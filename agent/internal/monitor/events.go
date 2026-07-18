package monitor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"codex-monitor-agent/internal/model"
)

const eventHistoryLimit = 256

// StreamMessage is one message sent to an authenticated SSE subscriber.
// Snapshot messages describe current state; task_activity messages are durable,
// sequenced transitions that can be replayed after a reconnect.
type StreamMessage struct {
	Event     string
	Sequence  uint64
	Snapshot  *model.Snapshot
	TaskEvent *model.TaskEvent
}

func (m *Monitor) broadcastSnapshotLocked() {
	signature := snapshotStreamSignature(m.snapshot)
	if m.streamReady && signature == m.streamSignature {
		return
	}
	m.streamReady = true
	m.streamSignature = signature
	current := m.snapshot
	for ch := range m.subs {
		select {
		case ch <- StreamMessage{Event: "snapshot", Snapshot: &current}:
		default:
		}
	}
}

func snapshotStreamSignature(snapshot model.Snapshot) [32]byte {
	normalized := snapshot
	normalized.GeneratedAt = time.Time{}
	normalized.Agent.UptimeSeconds = 0
	normalized.Codex.LastSuccessAt = nil
	normalized.Threads = append([]model.Thread(nil), snapshot.Threads...)
	for i := range normalized.Threads {
		normalized.Threads[i].UpdatedAt = time.Time{}
	}
	normalized.Usage = compactUsage(snapshot.Usage)
	data, _ := json.Marshal(normalized)
	return sha256.Sum256(data)
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

func (m *Monitor) emitTaskEventLocked(event model.TaskEvent) {
	m.eventSequence++
	event.Sequence = m.eventSequence
	event.EventID = strconv.FormatUint(event.Sequence, 10)
	event.InstallationID = m.cfg.InstallationID
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	m.eventHistory = append(m.eventHistory, event)
	if len(m.eventHistory) > eventHistoryLimit {
		m.eventHistory = append([]model.TaskEvent(nil), m.eventHistory[len(m.eventHistory)-eventHistoryLimit:]...)
	}
	for ch := range m.subs {
		eventCopy := event
		select {
		case ch <- StreamMessage{Event: "task_activity", Sequence: event.Sequence, TaskEvent: &eventCopy}:
		default:
		}
	}
}

func (m *Monitor) prepareLiveEventLocked() {
	if m.eventsReady {
		return
	}
	m.lastThreads = make(map[string]model.Thread, len(m.snapshot.Threads))
	for _, thread := range m.snapshot.Threads {
		m.lastThreads[thread.ID] = thread
	}
	m.eventsReady = true
}

func (m *Monitor) emitTransitionEventsLocked(threads []model.Thread, now time.Time) {
	current := make(map[string]model.Thread, len(threads))
	for _, thread := range threads {
		current[thread.ID] = thread
	}
	if !m.eventsReady {
		m.lastThreads = current
		m.eventsReady = true
		return
	}
	for _, thread := range threads {
		previous, existed := m.lastThreads[thread.ID]
		if existed && previous.State == thread.State && previous.RequestID == thread.RequestID {
			continue
		}
		eventType := transitionEventType(previous, thread, existed)
		if eventType == "" {
			continue
		}
		m.emitTaskEventLocked(model.TaskEvent{
			Type: eventType, ThreadID: thread.ID, TurnID: thread.TurnID,
			ParentThreadID: thread.ParentThreadID, RootThreadID: thread.RootThreadID,
			ThreadRole: thread.ThreadRole, AgentNickname: thread.AgentNickname,
			TaskName: displayThreadName(thread), FromState: previous.State, ToState: thread.State,
			StateSource: thread.StateSource, StateConfidence: thread.StateConfidence,
			RequestID: thread.RequestID, Controllable: thread.Controllable, OccurredAt: now,
		})
	}
	m.lastThreads = current
}

func transitionEventType(previous, current model.Thread, existed bool) string {
	switch current.State {
	case model.StateWaitingApproval:
		return model.EventApprovalRequired
	case model.StateWaitingInput:
		return model.EventInputRequired
	case model.StateError:
		return model.EventTaskFailed
	case model.StateRunning:
		if existed && (previous.State == model.StateWaitingApproval || previous.State == model.StateWaitingInput || previous.State == model.StateIdle) {
			return model.EventTaskResumed
		}
		return model.EventTaskStarted
	case model.StateIdle:
		if !existed {
			return ""
		}
		switch current.LastTurnStatus {
		case "interrupted":
			return model.EventTaskInterrupted
		case "failed":
			return model.EventTaskFailed
		}
		if previous.State == model.StateRunning || previous.State == model.StateWaitingApproval || previous.State == model.StateWaitingInput {
			return model.EventTaskCompleted
		}
	}
	return ""
}

func displayThreadName(thread model.Thread) string {
	for _, value := range []string{thread.Name, thread.Preview, thread.ID} {
		if value != "" {
			return value
		}
	}
	return thread.ID
}

func assignThreadHierarchy(threads []model.Thread) {
	byID := make(map[string]model.Thread, len(threads))
	for _, thread := range threads {
		byID[thread.ID] = thread
	}
	for i := range threads {
		if threads[i].ParentThreadID == "" {
			threads[i].ThreadRole = "root"
			threads[i].RootThreadID = threads[i].ID
			continue
		}
		threads[i].ThreadRole = "subagent"
		rootID := threads[i].ParentThreadID
		seen := map[string]struct{}{threads[i].ID: {}}
		for rootID != "" {
			if _, duplicate := seen[rootID]; duplicate {
				break
			}
			seen[rootID] = struct{}{}
			parent, ok := byID[rootID]
			if !ok || parent.ParentThreadID == "" {
				break
			}
			rootID = parent.ParentThreadID
		}
		threads[i].RootThreadID = rootID
	}
}

func (m *Monitor) pendingRequestSnapshotLocked() []model.PendingRequest {
	requests := make([]model.PendingRequest, 0, len(m.pendingRequests))
	for _, pending := range m.pendingRequests {
		requests = append(requests, pending.request)
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].CreatedAt.Before(requests[j].CreatedAt) })
	return requests
}

func (m *Monitor) decoratePendingRequestsLocked(threads []model.Thread) []model.Thread {
	byID := make(map[string]int, len(threads))
	for i := range threads {
		byID[threads[i].ID] = i
	}
	for _, pending := range m.pendingRequests {
		index, ok := byID[pending.request.ThreadID]
		if !ok {
			threads = append(threads, model.Thread{ID: pending.request.ThreadID, RootThreadID: pending.request.ThreadID, ThreadRole: "root"})
			index = len(threads) - 1
			byID[pending.request.ThreadID] = index
		}
		thread := &threads[index]
		thread.TurnID = pending.request.TurnID
		thread.RequestID = pending.request.ID
		thread.AttentionType = pending.request.Type
		thread.Controllable = pending.request.Controllable
		thread.StateSource = "app_server_request"
		thread.StateConfidence = "exact"
		thread.UpdatedAt = pending.request.CreatedAt
		switch pending.request.Type {
		case "approval":
			thread.State = model.StateWaitingApproval
		case "input":
			thread.State = model.StateWaitingInput
		}
	}
	return threads
}

func nextRequestID(sequence uint64, now time.Time) string {
	return fmt.Sprintf("req-%d-%d", now.UnixMilli(), sequence)
}
