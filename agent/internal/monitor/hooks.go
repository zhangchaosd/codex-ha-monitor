package monitor

import (
	"errors"
	"strings"
	"time"

	"codex-monitor-agent/internal/model"
)

type HookEvent struct {
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	EventName      string `json:"hook_event_name"`
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`
	ToolName       string `json:"tool_name"`
	Message        string `json:"message"`

	// LegacyEvent keeps the receiver compatible with simple forwarders that use
	// {"agent":"codex","event":"PreToolUse"}. Native Codex hook payloads
	// should always use hook_event_name and session_id.
	LegacyEvent string `json:"event"`
}

type HookResult struct {
	SessionID string    `json:"session_id"`
	TurnID    string    `json:"turn_id,omitempty"`
	EventName string    `json:"hook_event_name"`
	State     string    `json:"state"`
	Recorded  bool      `json:"recorded"`
	Received  time.Time `json:"received_at"`
}

type hookObservation struct {
	event      HookEvent
	state      string
	receivedAt time.Time
}

func (m *Monitor) RecordHook(event HookEvent) (HookResult, error) {
	event.EventName = strings.TrimSpace(event.EventName)
	if event.EventName == "" {
		event.EventName = strings.TrimSpace(event.LegacyEvent)
	}
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.TurnID = strings.TrimSpace(event.TurnID)
	if event.EventName == "" {
		return HookResult{}, errors.New("hook_event_name is required")
	}
	if event.SessionID == "" {
		return HookResult{}, errors.New("session_id is required")
	}

	state, recorded := mapHookEvent(event)
	now := time.Now().UTC()
	result := HookResult{
		SessionID: event.SessionID, TurnID: event.TurnID, EventName: event.EventName,
		State: state, Recorded: recorded, Received: now,
	}

	m.mu.Lock()
	m.prepareLiveEventLocked()
	m.snapshot.Hooks.ReceivedEvents++
	m.snapshot.Hooks.LastEventAt = &now
	if recorded {
		m.hookSessions[event.SessionID] = hookObservation{event: event, state: state, receivedAt: now}
	}
	m.rebuildLocked(now)
	m.mu.Unlock()
	return result, nil
}

func mapHookEvent(event HookEvent) (string, bool) {
	switch event.EventName {
	case "PermissionRequest":
		return model.StateWaitingApproval, true
	case "Elicitation":
		return model.StateWaitingInput, true
	case "Notification":
		message := strings.ToLower(event.Message)
		if strings.Contains(message, "permission") || strings.Contains(message, "approve") || strings.Contains(message, "approval") {
			return model.StateWaitingApproval, true
		}
		return model.StateWaitingInput, true
	case "Stop", "SessionEnd", "SessionStart":
		return model.StateIdle, true
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PreCompact", "PostCompact",
		"SubagentStart", "SubagentStop", "WorktreeCreate":
		return model.StateRunning, true
	default:
		return model.StateUnknown, false
	}
}

func (m *Monitor) hookTTL(state string) time.Duration {
	switch state {
	case model.StateIdle:
		if m.cfg.HookIdleTTL > 0 {
			return m.cfg.HookIdleTTL
		}
		return time.Minute
	case model.StateWaitingApproval, model.StateWaitingInput:
		if m.cfg.HookAttentionTTL > 0 {
			return m.cfg.HookAttentionTTL
		}
		return 5 * time.Minute
	default:
		if m.cfg.HookRunningTTL > 0 {
			return m.cfg.HookRunningTTL
		}
		return 10 * time.Minute
	}
}

func (m *Monitor) applyHooksLocked(threads []model.Thread, now time.Time) []model.Thread {
	byID := make(map[string]int, len(threads))
	for i := range threads {
		byID[threads[i].ID] = i
	}
	active := 0
	for sessionID, observation := range m.hookSessions {
		if now.Sub(observation.receivedAt) >= m.hookTTL(observation.state) {
			delete(m.hookSessions, sessionID)
			continue
		}
		active++
		index, exists := byID[sessionID]
		if !exists {
			threads = append(threads, model.Thread{
				ID: sessionID, CWD: observation.event.CWD, Source: "codex_hook",
			})
			index = len(threads) - 1
			byID[sessionID] = index
		}
		thread := &threads[index]
		thread.TurnID = observation.event.TurnID
		if thread.CWD == "" {
			thread.CWD = observation.event.CWD
		}
		thread.State = observation.state
		thread.StateSource = "codex_hook"
		thread.StateConfidence = "event_derived"
		thread.LastHookEvent = observation.event.EventName
		if observation.receivedAt.After(thread.UpdatedAt) {
			thread.UpdatedAt = observation.receivedAt
		}
	}
	m.snapshot.Hooks.ActiveSessions = active
	sortThreads(threads)
	return threads
}
