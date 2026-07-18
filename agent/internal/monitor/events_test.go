package monitor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"codex-monitor-agent/internal/appserver"
	"codex-monitor-agent/internal/model"
)

type fakeController struct {
	requestMethod string
	requestParams any
	responseID    string
	response      map[string]any
}

func (f *fakeController) Request(_ context.Context, method string, params any, _ any) error {
	f.requestMethod = method
	f.requestParams = params
	return nil
}

func (f *fakeController) Respond(id json.RawMessage, result any) error {
	f.responseID = string(id)
	f.response, _ = result.(map[string]any)
	return nil
}

func TestTaskEventsAreSequencedAndReplayed(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	_, _ = m.RecordHook(HookEvent{SessionID: "thread-1", TurnID: "turn-1", EventName: "SessionStart"})
	_, _ = m.RecordHook(HookEvent{SessionID: "thread-1", TurnID: "turn-1", EventName: "UserPromptSubmit"})
	_, _ = m.RecordHook(HookEvent{SessionID: "thread-1", TurnID: "turn-1", EventName: "PermissionRequest"})

	stream, unsubscribe := m.Subscribe(1)
	defer unsubscribe()
	message := <-stream
	if message.Event != "task_activity" || message.TaskEvent == nil {
		t.Fatalf("first replay message = %+v", message)
	}
	if message.Sequence != 2 || message.TaskEvent.Type != model.EventApprovalRequired {
		t.Fatalf("replayed event = %+v", message.TaskEvent)
	}
	if snapshot := <-stream; snapshot.Event != "snapshot" || snapshot.Snapshot == nil {
		t.Fatalf("snapshot message = %+v", snapshot)
	}
}

func TestSnapshotsAreNotBroadcastForTimestampOnlyChanges(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	m.mu.Lock()
	m.rebuildLocked(time.Now().UTC())
	m.mu.Unlock()
	stream, unsubscribe := m.Subscribe(0)
	defer unsubscribe()
	if message := <-stream; message.Event != "snapshot" {
		t.Fatalf("initial message = %+v", message)
	}

	m.mu.Lock()
	m.rebuildLocked(time.Now().UTC().Add(time.Minute))
	m.mu.Unlock()
	select {
	case message := <-stream:
		t.Fatalf("timestamp-only rebuild emitted %+v", message)
	case <-time.After(20 * time.Millisecond):
	}

	m.setConnection("connected", "")
	select {
	case message := <-stream:
		if message.Event != "snapshot" {
			t.Fatalf("state change emitted %+v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("semantic state change did not emit a snapshot")
	}
}

func TestSnapshotSignatureIgnoresUsageHistoryButKeepsSummary(t *testing.T) {
	snapshot := model.Snapshot{Usage: map[string]any{
		"availability":      "available",
		"summary":           map[string]any{"lifetimeTokens": 10},
		"dailyUsageBuckets": []any{map[string]any{"tokens": 1}},
	}}
	first := snapshotStreamSignature(snapshot)
	snapshot.Usage["dailyUsageBuckets"] = []any{map[string]any{"tokens": 2}}
	if first != snapshotStreamSignature(snapshot) {
		t.Fatal("daily usage history changed the live entity signature")
	}
	snapshot.Usage["summary"] = map[string]any{"lifetimeTokens": 11}
	if first == snapshotStreamSignature(snapshot) {
		t.Fatal("usage summary change was not detected")
	}
}

func TestServerRequestDecoratesExactThread(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	params := json.RawMessage(`{
		"threadId":"thread-control","turnId":"turn-control","itemId":"item-1",
		"command":"git status","startedAtMs":1,
		"availableDecisions":["accept","decline"]
	}`)
	m.recordServerRequest(&appserver.Client{}, json.RawMessage(`12`), "item/commandExecution/requestApproval", params)
	requests := m.PendingRequests()
	if len(requests) != 1 || !requests[0].Controllable || requests[0].Summary != "git status" {
		t.Fatalf("pending requests = %+v", requests)
	}
	threads := m.Snapshot().Threads
	if len(threads) != 1 || threads[0].State != model.StateWaitingApproval || threads[0].RequestID == "" {
		t.Fatalf("decorated threads = %+v", threads)
	}
}

func TestApprovalResolvesExactServerRequest(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	params := json.RawMessage(`{
		"threadId":"thread-control","turnId":"turn-control","itemId":"item-1",
		"command":"go test ./...","startedAtMs":1,
		"availableDecisions":["accept","decline"]
	}`)
	m.recordServerRequest(&appserver.Client{}, json.RawMessage(`21`), "item/commandExecution/requestApproval", params)
	requestID := m.PendingRequests()[0].ID
	controller := &fakeController{}
	m.mu.Lock()
	m.pendingRequests[requestID].client = controller
	m.mu.Unlock()

	result, err := m.Approve(requestID, "thread-control", "turn-control", false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || controller.responseID != "21" || controller.response["decision"] != "accept" {
		t.Fatalf("result = %+v, controller = %+v", result, controller)
	}
	if len(m.PendingRequests()) != 0 {
		t.Fatal("resolved request was not removed")
	}
}

func TestInterruptTargetsExactTurn(t *testing.T) {
	m := New(Config{CodexBinary: "codex-does-not-exist-for-test"})
	controller := &fakeController{}
	m.mu.Lock()
	m.client = controller
	m.appThreads = []model.Thread{{ID: "thread", TurnID: "turn", State: model.StateRunning}}
	m.rebuildLocked(time.Now().UTC())
	m.mu.Unlock()

	result, err := m.Interrupt(context.Background(), "thread", "turn")
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || controller.requestMethod != "turn/interrupt" {
		t.Fatalf("result = %+v, controller = %+v", result, controller)
	}
	thread := m.Snapshot().Threads[0]
	if thread.State != model.StateIdle || thread.LastTurnStatus != "interrupted" {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestHierarchyCountsWorkflowsAndWorkers(t *testing.T) {
	threads := []model.Thread{
		{ID: "root", State: model.StateRunning},
		{ID: "child", ParentThreadID: "root", State: model.StateRunning},
		{ID: "approval", State: model.StateWaitingApproval},
		{ID: "error", State: model.StateError},
	}
	assignThreadHierarchy(threads)
	summary := model.Summarize(threads, true)
	if summary.ActiveWorkers != 3 || summary.ActiveWorkflows != 2 {
		t.Fatalf("summary counts = %+v", summary)
	}
	if summary.WorkloadState != model.StateWaitingApproval {
		t.Fatalf("workload state = %s", summary.WorkloadState)
	}
}

func TestMergeThreadsPreservesFilesystemHierarchy(t *testing.T) {
	merged := mergeThreads(
		[]model.Thread{{ID: "child", State: model.StateUnknown}},
		[]model.Thread{{
			ID: "child", SessionID: "child", ParentThreadID: "root",
			AgentNickname: "reviewer", AgentRole: "explorer", State: model.StateIdle,
		}},
	)
	if len(merged) != 1 {
		t.Fatalf("unexpected threads: %+v", merged)
	}
	thread := merged[0]
	if thread.SessionID != "child" || thread.ParentThreadID != "root" || thread.AgentNickname != "reviewer" || thread.AgentRole != "explorer" {
		t.Fatalf("filesystem hierarchy was lost: %+v", thread)
	}
}

func TestTransitionToIdlePreservesInterruptedMeaning(t *testing.T) {
	previous := model.Thread{ID: "thread", State: model.StateRunning}
	current := model.Thread{ID: "thread", State: model.StateIdle, LastTurnStatus: "interrupted", UpdatedAt: time.Now()}
	if got := transitionEventType(previous, current, true); got != model.EventTaskInterrupted {
		t.Fatalf("event = %s", got)
	}
}
