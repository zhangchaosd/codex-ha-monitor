package monitor

import (
	"testing"
	"time"

	"codex-monitor-agent/internal/model"
)

func TestRecordHookOverlaysMatchingSession(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	m.mu.Lock()
	m.fsThreads = []model.Thread{{
		ID: "session-1", Name: "Existing", State: model.StateIdle,
		StateSource: "filesystem_inference", StateConfidence: "inferred", UpdatedAt: time.Now().Add(-time.Minute),
	}}
	m.rebuildLocked(time.Now().UTC())
	m.mu.Unlock()

	result, err := m.RecordHook(HookEvent{
		SessionID: "session-1", TurnID: "turn-1", EventName: "PreToolUse", CWD: "/tmp/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Recorded || result.State != model.StateRunning {
		t.Fatalf("unexpected result: %+v", result)
	}
	snapshot := m.Snapshot()
	if len(snapshot.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(snapshot.Threads))
	}
	thread := snapshot.Threads[0]
	if thread.State != model.StateRunning || thread.StateSource != "codex_hook" {
		t.Fatalf("unexpected thread state: %+v", thread)
	}
	if thread.TurnID != "turn-1" || thread.LastHookEvent != "PreToolUse" {
		t.Fatalf("hook identity was not retained: %+v", thread)
	}
	if snapshot.Hooks.ReceivedEvents != 1 || snapshot.Hooks.ActiveSessions != 1 {
		t.Fatalf("unexpected hook summary: %+v", snapshot.Hooks)
	}
}

func TestRecordHookStateMapping(t *testing.T) {
	tests := []struct {
		event string
		want  string
	}{
		{"PermissionRequest", model.StateWaitingApproval},
		{"Elicitation", model.StateWaitingInput},
		{"UserPromptSubmit", model.StateRunning},
		{"PostToolUse", model.StateRunning},
		{"Stop", model.StateIdle},
	}
	for _, test := range tests {
		t.Run(test.event, func(t *testing.T) {
			state, recorded := mapHookEvent(HookEvent{EventName: test.event})
			if !recorded || state != test.want {
				t.Fatalf("mapHookEvent(%q) = %q, %v; want %q, true", test.event, state, recorded, test.want)
			}
		})
	}
}

func TestExpiredHookFallsBackToFilesystem(t *testing.T) {
	now := time.Now().UTC()
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test", HookRunningTTL: time.Second})
	m.mu.Lock()
	m.fsThreads = []model.Thread{{
		ID: "session-1", State: model.StateIdle, StateSource: "filesystem_inference",
		StateConfidence: "inferred", UpdatedAt: now,
	}}
	m.hookSessions["session-1"] = hookObservation{
		event: HookEvent{SessionID: "session-1", EventName: "PreToolUse"},
		state: model.StateRunning, receivedAt: now.Add(-2 * time.Second),
	}
	m.rebuildLocked(now)
	snapshot := m.snapshot
	m.mu.Unlock()

	if snapshot.Threads[0].State != model.StateIdle || snapshot.Threads[0].StateSource != "filesystem_inference" {
		t.Fatalf("expired hook still overrides thread: %+v", snapshot.Threads[0])
	}
	if snapshot.Hooks.ActiveSessions != 0 {
		t.Fatalf("active hook sessions = %d, want 0", snapshot.Hooks.ActiveSessions)
	}
}

func TestRecordHookRequiresNativeIdentity(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	if _, err := m.RecordHook(HookEvent{EventName: "PreToolUse"}); err == nil {
		t.Fatal("expected missing session_id error")
	}
}
