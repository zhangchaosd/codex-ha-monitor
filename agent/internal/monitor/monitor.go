package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"codex-monitor-agent/internal/appserver"
	"codex-monitor-agent/internal/collector"
	"codex-monitor-agent/internal/model"
)

type Config struct {
	AgentVersion           string
	InstallationID         string
	CodexBinary            string
	CodexHome              string
	Endpoint               string
	PollInterval           time.Duration
	FilesystemInterval     time.Duration
	StaleAfter             time.Duration
	FilesystemActiveWindow time.Duration
	HookRunningTTL         time.Duration
	HookIdleTTL            time.Duration
	HookAttentionTTL       time.Duration
	MaxThreads             int
}

type Monitor struct {
	cfg          Config
	startedAt    time.Time
	mu           sync.RWMutex
	snapshot     model.Snapshot
	appThreads   []model.Thread
	fsThreads    []model.Thread
	usage        map[string]any
	rateLimits   map[string]any
	hookSessions map[string]hookObservation
	subs         map[chan model.Snapshot]struct{}
}

func New(cfg Config) *Monitor {
	hostname, _ := os.Hostname()
	cli := detectCodexVersion(cfg.CodexBinary)
	now := time.Now().UTC()
	return &Monitor{
		cfg: cfg, startedAt: now, subs: map[chan model.Snapshot]struct{}{},
		hookSessions: map[string]hookObservation{},
		snapshot: model.Snapshot{
			SchemaVersion: model.SchemaVersion, GeneratedAt: now,
			InstallationID: cfg.InstallationID,
			Host:           model.HostInfo{Name: hostname, OS: runtime.GOOS, Arch: runtime.GOARCH},
			Agent:          model.AgentInfo{Version: cfg.AgentVersion, GoVersion: runtime.Version()},
			CodexCLI:       cli,
			Codex:          model.CodexInfo{ConnectionState: "connecting", Visibility: "unavailable"},
			Summary:        model.Summary{WorkloadState: model.StateUnknown, StateSource: "none", StateConfidence: "unknown"},
		},
	}
}

func (m *Monitor) Run(ctx context.Context) {
	go m.runFilesystem(ctx)
	m.runAppServer(ctx)
}

func (m *Monitor) Snapshot() model.Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

func (m *Monitor) Subscribe() (<-chan model.Snapshot, func()) {
	ch := make(chan model.Snapshot, 4)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	current := m.snapshot
	m.mu.Unlock()
	ch <- current
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		close(ch)
		m.mu.Unlock()
	}
}

func (m *Monitor) runFilesystem(ctx context.Context) {
	interval := m.cfg.FilesystemInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		m.collectFilesystem(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Monitor) collectFilesystem(ctx context.Context) {
	home := m.cfg.CodexHome
	m.mu.RLock()
	if m.snapshot.AppServer.CodexHome != "" {
		home = m.snapshot.AppServer.CodexHome
	}
	m.mu.RUnlock()
	if home == "" {
		if value := os.Getenv("CODEX_HOME"); value != "" {
			home = value
		} else if userHome, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(userHome, ".codex")
		}
	}
	threads, err := (collector.FilesystemCollector{
		Home: home, ActiveWindow: m.cfg.FilesystemActiveWindow, MaxThreads: m.cfg.MaxThreads,
	}).Collect(ctx)
	if err != nil {
		return
	}
	m.mu.Lock()
	m.fsThreads = threads
	m.rebuildLocked(time.Now().UTC())
	m.mu.Unlock()
}

func (m *Monitor) runAppServer(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		m.setConnection("connecting", "")
		if m.cfg.Endpoint != "auto" && m.cfg.Endpoint != "stdio" {
			m.setConnection("error", "only auto and stdio are implemented in this MVP")
			return
		}
		client, initResult, err := appserver.ConnectStdio(ctx, m.cfg.CodexBinary, m.cfg.AgentVersion, m.handleAppServerMessage)
		if err != nil {
			m.setConnection("error", err.Error())
			if !waitContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		m.mu.Lock()
		m.snapshot.AppServer = model.AppServerInfo{UserAgent: initResult.UserAgent, CodexHome: initResult.CodexHome}
		m.snapshot.Codex.ConnectionState = "connected"
		m.snapshot.Codex.Visibility = "agent_owned_with_filesystem_fallback"
		m.snapshot.Codex.LastError = ""
		m.rebuildLocked(time.Now().UTC())
		m.mu.Unlock()

		poll := m.cfg.PollInterval
		if poll <= 0 {
			poll = 10 * time.Second
		}
		ticker := time.NewTicker(poll)
		m.refreshAppServer(ctx, client)
		connected := true
		for connected {
			select {
			case <-ctx.Done():
				ticker.Stop()
				_ = client.Close()
				return
			case err := <-client.Done():
				ticker.Stop()
				_ = client.Close()
				m.setConnection("disconnected", err.Error())
				connected = false
			case <-ticker.C:
				m.refreshAppServer(ctx, client)
			}
		}
		if !waitContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func (m *Monitor) refreshAppServer(parent context.Context, client *appserver.Client) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	var threadsResponse struct {
		Data []struct {
			ID         string          `json:"id"`
			Name       string          `json:"name"`
			Preview    string          `json:"preview"`
			CWD        string          `json:"cwd"`
			Source     any             `json:"source"`
			CLIVersion string          `json:"cliVersion"`
			UpdatedAt  int64           `json:"updatedAt"`
			Status     json.RawMessage `json:"status"`
		} `json:"data"`
	}
	if err := client.Request(ctx, "thread/list", map[string]any{
		"limit": m.cfg.MaxThreads, "sortKey": "updated_at", "sortDirection": "desc",
	}, &threadsResponse); err != nil {
		m.setConnection("disconnected", err.Error())
		return
	}
	var loadedResponse struct {
		Data []string `json:"data"`
	}
	_ = client.Request(ctx, "thread/loaded/list", map[string]any{"limit": m.cfg.MaxThreads}, &loadedResponse)
	loaded := map[string]bool{}
	for _, id := range loadedResponse.Data {
		loaded[id] = true
	}
	threads := make([]model.Thread, 0, len(threadsResponse.Data))
	for _, item := range threadsResponse.Data {
		state, confidence := mapAppServerState(item.Status)
		threads = append(threads, model.Thread{
			ID: item.ID, Name: item.Name, Preview: item.Preview, CWD: item.CWD,
			Source: stringifySource(item.Source), CLIVersion: item.CLIVersion,
			State: state, StateSource: "app_server_reconcile", StateConfidence: confidence,
			Loaded: loaded[item.ID], UpdatedAt: time.Unix(item.UpdatedAt, 0).UTC(),
		})
	}
	var usage map[string]any
	if err := client.Request(ctx, "account/usage/read", nil, &usage); err != nil {
		usage = map[string]any{"availability": "unavailable", "error": err.Error()}
	} else {
		usage["availability"] = "available"
	}
	var rateLimits map[string]any
	if err := client.Request(ctx, "account/rateLimits/read", nil, &rateLimits); err != nil {
		rateLimits = map[string]any{"availability": "unavailable", "error": err.Error()}
	} else {
		rateLimits["availability"] = "available"
	}
	now := time.Now().UTC()
	m.mu.Lock()
	m.appThreads = threads
	m.usage = usage
	m.rateLimits = rateLimits
	m.snapshot.Codex.ConnectionState = "connected"
	m.snapshot.Codex.LastSuccessAt = &now
	m.snapshot.Codex.LastError = ""
	m.rebuildLocked(now)
	m.mu.Unlock()
}

func (m *Monitor) handleAppServerMessage(method string, params json.RawMessage, serverRequest bool) {
	if method == "thread/status/changed" {
		var event struct {
			ThreadID string          `json:"threadId"`
			Status   json.RawMessage `json:"status"`
		}
		if json.Unmarshal(params, &event) == nil {
			state, confidence := mapAppServerState(event.Status)
			m.mu.Lock()
			for i := range m.appThreads {
				if m.appThreads[i].ID == event.ThreadID {
					m.appThreads[i].State = state
					m.appThreads[i].StateSource = "app_server_event"
					m.appThreads[i].StateConfidence = confidence
					m.appThreads[i].UpdatedAt = time.Now().UTC()
				}
			}
			m.rebuildLocked(time.Now().UTC())
			m.mu.Unlock()
		}
	}
	_ = serverRequest
}

func (m *Monitor) rebuildLocked(now time.Time) {
	merged := mergeThreads(m.appThreads, m.fsThreads)
	merged = m.applyHooksLocked(merged, now)
	visibility := m.snapshot.Codex.Visibility
	if visibility == "unavailable" && len(m.fsThreads) > 0 {
		visibility = "filesystem_fallback"
		m.snapshot.Codex.Visibility = visibility
	}
	shared := visibility == "shared_app_server"
	m.snapshot.SchemaVersion = model.SchemaVersion
	m.snapshot.GeneratedAt = now
	m.snapshot.Agent.UptimeSeconds = int64(time.Since(m.startedAt).Seconds())
	m.snapshot.Threads = merged
	m.snapshot.Summary = model.Summarize(merged, shared)
	if m.snapshot.Codex.ConnectionState == "connected" && len(m.fsThreads) > 0 && !shared {
		m.snapshot.Codex.Visibility = "agent_owned_with_filesystem_fallback"
	}
	m.snapshot.Usage = m.usage
	m.snapshot.RateLimits = m.rateLimits
	m.snapshot.Stale = m.isStaleLocked(now)
	current := m.snapshot
	for ch := range m.subs {
		select {
		case ch <- current:
		default:
		}
	}
}

func (m *Monitor) isStaleLocked(now time.Time) bool {
	if m.snapshot.Codex.ConnectionState == "connected" {
		return false
	}
	if len(m.fsThreads) > 0 && now.Sub(m.fsThreads[0].UpdatedAt) <= m.cfg.StaleAfter {
		return false
	}
	return true
}

func (m *Monitor) setConnection(state, lastError string) {
	m.mu.Lock()
	m.snapshot.Codex.ConnectionState = state
	m.snapshot.Codex.LastError = lastError
	m.rebuildLocked(time.Now().UTC())
	m.mu.Unlock()
}

func mergeThreads(app, fs []model.Thread) []model.Thread {
	byID := map[string]model.Thread{}
	for _, thread := range fs {
		byID[thread.ID] = thread
	}
	for _, thread := range app {
		current, exists := byID[thread.ID]
		if exists {
			if thread.Name == "" {
				thread.Name = current.Name
			}
			if thread.CWD == "" {
				thread.CWD = current.CWD
			}
			if thread.Source == "" {
				thread.Source = current.Source
			}
			if thread.CLIVersion == "" {
				thread.CLIVersion = current.CLIVersion
			}
			if thread.State == model.StateUnknown && current.State != model.StateUnknown {
				thread.State = current.State
				thread.StateSource = current.StateSource
				thread.StateConfidence = current.StateConfidence
				thread.UpdatedAt = current.UpdatedAt
			}
		}
		byID[thread.ID] = thread
	}
	result := make([]model.Thread, 0, len(byID))
	for _, thread := range byID {
		result = append(result, thread)
	}
	sortThreads(result)
	return result
}

func sortThreads(threads []model.Thread) {
	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
	})
}

func mapAppServerState(raw json.RawMessage) (string, string) {
	var status struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	}
	if json.Unmarshal(raw, &status) != nil {
		return model.StateUnknown, "unknown"
	}
	if status.Type == "systemError" {
		return model.StateError, "exact"
	}
	if status.Type == "idle" {
		return model.StateIdle, "exact"
	}
	if status.Type != "active" {
		return model.StateUnknown, "unknown"
	}
	for _, flag := range status.ActiveFlags {
		if flag == "waitingOnApproval" {
			return model.StateWaitingApproval, "exact"
		}
	}
	for _, flag := range status.ActiveFlags {
		if flag == "waitingOnUserInput" {
			return model.StateWaitingInput, "exact"
		}
	}
	return model.StateRunning, "exact"
}

func detectCodexVersion(binary string) model.CodexCLIInfo {
	info := model.CodexCLIInfo{}
	path, err := exec.LookPath(binary)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Binary = path
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	info.Raw = strings.TrimSpace(string(output))
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Version = ParseCodexVersion(info.Raw)
	return info
}

func ParseCodexVersion(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[len(fields)-1], "v")
}

func stringifySource(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return next
}
