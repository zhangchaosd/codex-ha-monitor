package monitor

import (
	"errors"
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

func TestAccountFailuresTriggerRecoveryAtThreshold(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test", AppServerFailureThreshold: 2})
	now := time.Now().UTC()
	if restart := m.recordAccountResult(now, []error{errors.New("usage timed out")}); restart {
		t.Fatal("first failure should not restart App Server")
	}
	if got := m.Snapshot().Codex.ConsecutiveFailures; got != 1 {
		t.Fatalf("consecutive failures = %d, want 1", got)
	}
	if restart := m.recordAccountResult(now.Add(time.Second), []error{errors.New("rate limits timed out")}); !restart {
		t.Fatal("second failure should restart App Server")
	}
	snapshot := m.Snapshot()
	if snapshot.Codex.ConnectionState != "recovering" {
		t.Fatalf("connection state = %q, want recovering", snapshot.Codex.ConnectionState)
	}
	if snapshot.Codex.LastRecoveryAt == nil {
		t.Fatal("expected recovery timestamp")
	}
}

func TestAccountSuccessResetsFailureCount(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	now := time.Now().UTC()
	_ = m.recordAccountResult(now, []error{errors.New("usage timed out")})
	if restart := m.recordAccountResult(now.Add(time.Second), nil); restart {
		t.Fatal("success should not restart App Server")
	}
	if got := m.Snapshot().Codex.ConsecutiveFailures; got != 0 {
		t.Fatalf("consecutive failures = %d, want 0", got)
	}
}

func TestTrimUsageHistoryRetainsNewestBuckets(t *testing.T) {
	usage := map[string]any{
		"summary": map[string]any{"lifetimeTokens": 123},
		"dailyUsageBuckets": []any{
			map[string]any{"startDate": "2026-07-01"},
			map[string]any{"startDate": "2026-07-02"},
			map[string]any{"startDate": "2026-07-03"},
		},
	}
	trimmed := trimUsageHistory(usage, 2)
	buckets := trimmed["dailyUsageBuckets"].([]any)
	if len(buckets) != 2 {
		t.Fatalf("bucket count = %d, want 2", len(buckets))
	}
	if got := buckets[0].(map[string]any)["startDate"]; got != "2026-07-02" {
		t.Fatalf("first retained bucket = %v, want 2026-07-02", got)
	}
	if usage["dailyUsageBuckets"].([]any)[0].(map[string]any)["startDate"] != "2026-07-01" {
		t.Fatal("trim mutated the original usage payload")
	}
}

func TestTrimUsageHistoryCanDisableBuckets(t *testing.T) {
	trimmed := trimUsageHistory(map[string]any{"dailyUsageBuckets": []any{map[string]any{"startDate": "2026-07-01"}}}, 0)
	if len(trimmed["dailyUsageBuckets"].([]any)) != 0 {
		t.Fatal("days=0 should remove all daily buckets")
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
