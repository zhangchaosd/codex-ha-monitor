package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codex-monitor-agent/internal/model"
	"codex-monitor-agent/internal/monitor"
)

type Server struct {
	address string
	monitor *monitor.Monitor
	token   string
	http    *http.Server
}

func New(address string, m *monitor.Monitor, token string) *Server {
	s := &Server{address: address, monitor: m, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.HandleFunc("/api/v1/version", s.handleVersion)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/threads", s.handleThreads)
	mux.HandleFunc("/api/v1/usage", s.handleUsage)
	mux.HandleFunc("/api/v1/rate-limits", s.handleRateLimits)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/requests", s.handleRequests)
	mux.HandleFunc("/api/v1/actions/approve", s.handleApprove)
	mux.HandleFunc("/api/v1/actions/reject", s.handleReject)
	mux.HandleFunc("/api/v1/actions/submit-input", s.handleSubmitInput)
	mux.HandleFunc("/api/v1/actions/interrupt", s.handleInterrupt)
	mux.HandleFunc("/api/v1/hooks/codex", s.handleCodexHook)
	s.http = &http.Server{Addr: address, Handler: cors(authenticate(token, mux)), ReadHeaderTimeout: 5 * time.Second}
	return s
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) Handler() http.Handler { return s.http.Handler }

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(statusPage))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.monitor.Snapshot()
	writeJSON(w, map[string]any{
		"status": "ok", "agent_version": snapshot.Agent.Version, "uptime_seconds": snapshot.Agent.UptimeSeconds,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.monitor.Snapshot()
	writeJSON(w, map[string]any{
		"ready":                 true,
		"snapshot_available":    len(snapshot.Threads) > 0 || snapshot.Codex.ConnectionState == "connected",
		"app_server_connection": snapshot.Codex.ConnectionState,
		"filesystem_available":  len(snapshot.Threads) > 0,
		"hook_events_received":  snapshot.Hooks.ReceivedEvents,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.monitor.Snapshot()
	writeJSON(w, model.VersionResponse{
		SchemaVersion: snapshot.SchemaVersion, InstallationID: snapshot.InstallationID,
		Agent: snapshot.Agent, CodexCLI: snapshot.CodexCLI, AppServer: snapshot.AppServer,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.monitor.Snapshot()
	snapshot.Threads = nil
	snapshot.Usage = compactUsage(snapshot.Usage)
	writeJSON(w, snapshot)
}

func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	snapshot := s.monitor.Snapshot()
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= 200 {
			limit = parsed
		}
	}
	threads := snapshot.Threads
	if len(threads) > limit {
		threads = threads[:limit]
	}
	writeJSON(w, map[string]any{
		"schema_version": snapshot.SchemaVersion, "generated_at": snapshot.GeneratedAt, "threads": threads,
	})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	days := -1
	if value := r.URL.Query().Get("days"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 365 {
			http.Error(w, "days must be an integer between 0 and 365", http.StatusBadRequest)
			return
		}
		days = parsed
	}
	usage := s.monitor.Usage(days)
	if usage == nil {
		writeJSON(w, map[string]any{"availability": "unavailable", "summary": nil, "dailyUsageBuckets": nil})
		return
	}
	writeJSON(w, usage)
}

func (s *Server) handleRateLimits(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.monitor.Snapshot()
	if snapshot.RateLimits == nil {
		writeJSON(w, map[string]any{"availability": "unavailable", "rateLimits": nil})
		return
	}
	writeJSON(w, snapshot.RateLimits)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	afterSequence := uint64(0)
	if value := r.Header.Get("Last-Event-ID"); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			afterSequence = parsed
		}
	}
	updates, unsubscribe := s.monitor.Subscribe(afterSequence)
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case message, ok := <-updates:
			if !ok {
				return
			}
			var value any
			switch message.Event {
			case "task_activity":
				value = message.TaskEvent
				_, _ = fmt.Fprintf(w, "id: %d\n", message.Sequence)
			default:
				snapshot := *message.Snapshot
				snapshot.Usage = compactUsage(snapshot.Usage)
				value = snapshot
			}
			data, _ := json.Marshal(value)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", message.Event, data)
			flusher.Flush()
		}
	}
}

func compactUsage(usage map[string]any) map[string]any {
	if usage == nil {
		return nil
	}
	result := make(map[string]any, len(usage))
	for key, value := range usage {
		if key != "dailyUsageBuckets" {
			result[key] = value
		}
	}
	return result
}

func (s *Server) handleRequests(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"requests": s.monitor.PendingRequests()})
}

type actionRequest struct {
	RequestID  string              `json:"request_id"`
	ThreadID   string              `json:"thread_id"`
	TurnID     string              `json:"turn_id"`
	ForSession bool                `json:"for_session"`
	CancelTurn bool                `json:"cancel_turn"`
	Text       string              `json:"text"`
	Answers    map[string][]string `json:"answers"`
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.handleAction(w, r, func(input actionRequest) (monitor.ActionResult, error) {
		return s.monitor.Approve(input.RequestID, input.ThreadID, input.TurnID, input.ForSession)
	})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	s.handleAction(w, r, func(input actionRequest) (monitor.ActionResult, error) {
		return s.monitor.Reject(input.RequestID, input.ThreadID, input.TurnID, input.CancelTurn)
	})
}

func (s *Server) handleSubmitInput(w http.ResponseWriter, r *http.Request) {
	s.handleAction(w, r, func(input actionRequest) (monitor.ActionResult, error) {
		answers := input.Answers
		if len(answers) == 0 && input.Text != "" {
			for _, pending := range s.monitor.PendingRequests() {
				if pending.ID == input.RequestID && len(pending.Questions) == 1 {
					if questionID, ok := pending.Questions[0]["id"].(string); ok && questionID != "" {
						answers = map[string][]string{questionID: {input.Text}}
					}
				}
			}
		}
		return s.monitor.SubmitInput(input.RequestID, input.ThreadID, input.TurnID, answers)
	})
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	s.handleAction(w, r, func(input actionRequest) (monitor.ActionResult, error) {
		return s.monitor.Interrupt(r.Context(), input.ThreadID, input.TurnID)
	})
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request, action func(actionRequest) (monitor.ActionResult, error)) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var input actionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid action JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := action(input)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, monitor.ErrRequestNotFound):
			status = http.StatusNotFound
		case errors.Is(err, monitor.ErrRequestConflict), errors.Is(err, monitor.ErrNotControllable):
			status = http.StatusConflict
		case errors.Is(err, monitor.ErrNoAppServer):
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleCodexHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var event monitor.HookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid hook JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.monitor.RecordHook(event)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, result)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/hooks/codex" || strings.HasPrefix(r.URL.Path, "/api/v1/actions/")) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authenticate(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			next.ServeHTTP(w, r)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if provided == r.Header.Get("Authorization") || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="codex-monitor-agent"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const statusPage = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Codex Monitor Agent</title><style>
body{font-family:system-ui,sans-serif;margin:2rem;background:#f5f7fb;color:#172033}main{max-width:960px;margin:auto}
.card{background:white;border-radius:14px;padding:1rem 1.25rem;margin:.8rem 0;box-shadow:0 3px 16px #17203312}
.state{font-size:2rem;font-weight:700}pre{white-space:pre-wrap;word-break:break-word}table{width:100%;border-collapse:collapse}td,th{padding:.5rem;border-bottom:1px solid #dde3ee;text-align:left}
</style></head><body><main><h1>Codex Monitor Agent</h1><div class="card"><div id="state" class="state">Loading…</div><div id="meta"></div></div><div class="card"><h2>最近线程</h2><table><thead><tr><th>名称</th><th>状态</th><th>来源</th><th>更新时间</th></tr></thead><tbody id="threads"></tbody></table></div><div class="card"><h2>原始状态</h2><pre id="raw"></pre></div></main><script>
const token=sessionStorage.getItem('cmaToken')||prompt('Codex Monitor API token');if(token)sessionStorage.setItem('cmaToken',token);const api=p=>fetch(p,{headers:{Authorization:'Bearer '+token}}).then(r=>{if(!r.ok)throw new Error('Request failed: '+r.status);return r.json()});
async function refresh(){const [s,t]=await Promise.all([api('/api/v1/status'),api('/api/v1/threads?limit=10')]);
document.querySelector('#state').textContent=s.summary.workload_state;document.querySelector('#meta').textContent=s.host.name+' · '+s.codex.connection_state+' · '+s.codex.visibility+' · hooks '+s.hooks.active_sessions+' · Codex '+(s.codex_cli.version||'unknown');document.querySelector('#raw').textContent=JSON.stringify(s,null,2);
document.querySelector('#threads').innerHTML=(t.threads||[]).map(x=>'<tr><td>'+escapeHTML(x.name||x.id)+'</td><td>'+x.state+'</td><td>'+escapeHTML(x.source||'')+'</td><td>'+x.updated_at+'</td></tr>').join('')}
function escapeHTML(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}if(token){refresh();setInterval(refresh,5000)}
</script></body></html>`
