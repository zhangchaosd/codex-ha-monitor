package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codex-monitor-agent/internal/appserver"
	"codex-monitor-agent/internal/model"
)

var (
	ErrRequestNotFound = errors.New("pending request not found")
	ErrRequestConflict = errors.New("pending request does not match thread or turn")
	ErrNotControllable = errors.New("request is visible but not controllable through this app-server connection")
	ErrNoAppServer     = errors.New("app-server is not connected")
)

type pendingControl struct {
	request     model.PendingRequest
	client      appServerController
	appServerID json.RawMessage
}

type appServerController interface {
	Request(context.Context, string, any, any) error
	Respond(json.RawMessage, any) error
}

type ActionResult struct {
	OK        bool   `json:"ok"`
	Action    string `json:"action"`
	RequestID string `json:"request_id,omitempty"`
	ThreadID  string `json:"thread_id"`
	TurnID    string `json:"turn_id,omitempty"`
}

func (m *Monitor) PendingRequests() []model.PendingRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pendingRequestSnapshotLocked()
}

func (m *Monitor) recordServerRequest(client *appserver.Client, appServerID json.RawMessage, method string, params json.RawMessage) {
	var base struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		CWD      string `json:"cwd"`
		Reason   string `json:"reason"`
		Command  string `json:"command"`
	}
	if len(appServerID) == 0 || json.Unmarshal(params, &base) != nil || base.ThreadID == "" {
		return
	}

	typeName := ""
	controllable := false
	summary := strings.TrimSpace(base.Reason)
	questions := []map[string]any(nil)
	decisions := []string(nil)
	switch method {
	case "item/commandExecution/requestApproval":
		typeName, controllable = "approval", true
		if base.Command != "" {
			summary = base.Command
		}
		var details struct {
			Available []any `json:"availableDecisions"`
		}
		if json.Unmarshal(params, &details) == nil {
			for _, decision := range details.Available {
				if value, ok := decision.(string); ok {
					decisions = append(decisions, value)
				}
			}
		}
	case "item/fileChange/requestApproval":
		typeName, controllable = "approval", true
		decisions = []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/tool/requestUserInput":
		typeName, controllable = "input", true
		var details struct {
			Questions []map[string]any `json:"questions"`
		}
		if json.Unmarshal(params, &details) == nil {
			questions = details.Questions
			if len(questions) > 0 {
				summary, _ = questions[0]["question"].(string)
			}
		}
	case "item/permissions/requestApproval":
		typeName = "approval"
	case "mcpServer/elicitation/request":
		typeName = "input"
	default:
		return
	}

	now := time.Now().UTC()
	m.mu.Lock()
	m.prepareLiveEventLocked()
	m.requestSequence++
	requestID := nextRequestID(m.requestSequence, now)
	pending := &pendingControl{
		request: model.PendingRequest{
			ID: requestID, Type: typeName, Method: method, ThreadID: base.ThreadID,
			TurnID: base.TurnID, ItemID: base.ItemID, Summary: summary, CWD: base.CWD,
			Questions: questions, AvailableDecisions: decisions, Controllable: controllable, CreatedAt: now,
		},
		client: client, appServerID: append(json.RawMessage(nil), appServerID...),
	}
	m.pendingRequests[requestID] = pending
	m.rebuildLocked(now)
	m.mu.Unlock()
}

func (m *Monitor) Approve(requestID, threadID, turnID string, forSession bool) (ActionResult, error) {
	decision := "accept"
	if forSession {
		decision = "acceptForSession"
	}
	return m.resolveApproval("approve", requestID, threadID, turnID, decision)
}

func (m *Monitor) Reject(requestID, threadID, turnID string, cancelTurn bool) (ActionResult, error) {
	decision := "decline"
	if cancelTurn {
		decision = "cancel"
	}
	return m.resolveApproval("reject", requestID, threadID, turnID, decision)
}

func (m *Monitor) resolveApproval(action, requestID, threadID, turnID, decision string) (ActionResult, error) {
	pending, err := m.lookupPending(requestID, threadID, turnID, "approval")
	if err != nil {
		return ActionResult{}, err
	}
	if len(pending.request.AvailableDecisions) > 0 && !contains(pending.request.AvailableDecisions, decision) {
		return ActionResult{}, fmt.Errorf("%w: decision %q is not offered", ErrRequestConflict, decision)
	}
	if err := pending.client.Respond(pending.appServerID, map[string]any{"decision": decision}); err != nil {
		return ActionResult{}, err
	}
	m.finishPending(requestID, pending)
	return ActionResult{OK: true, Action: action, RequestID: requestID, ThreadID: threadID, TurnID: turnID}, nil
}

func (m *Monitor) SubmitInput(requestID, threadID, turnID string, answers map[string][]string) (ActionResult, error) {
	pending, err := m.lookupPending(requestID, threadID, turnID, "input")
	if err != nil {
		return ActionResult{}, err
	}
	if len(answers) == 0 {
		return ActionResult{}, fmt.Errorf("%w: answers are required", ErrRequestConflict)
	}
	responseAnswers := make(map[string]any, len(answers))
	for key, values := range answers {
		if key == "" || len(values) == 0 {
			return ActionResult{}, fmt.Errorf("%w: each question needs at least one answer", ErrRequestConflict)
		}
		responseAnswers[key] = map[string]any{"answers": values}
	}
	if err := pending.client.Respond(pending.appServerID, map[string]any{"answers": responseAnswers}); err != nil {
		return ActionResult{}, err
	}
	m.finishPending(requestID, pending)
	return ActionResult{OK: true, Action: "submit_input", RequestID: requestID, ThreadID: threadID, TurnID: turnID}, nil
}

func (m *Monitor) Interrupt(ctx context.Context, threadID, turnID string) (ActionResult, error) {
	if threadID == "" || turnID == "" {
		return ActionResult{}, fmt.Errorf("%w: thread_id and turn_id are required", ErrRequestConflict)
	}
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()
	if client == nil {
		return ActionResult{}, ErrNoAppServer
	}
	requestCtx, cancel := m.appServerRequestContext(ctx)
	defer cancel()
	if err := client.Request(requestCtx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}, nil); err != nil {
		return ActionResult{}, err
	}
	now := time.Now().UTC()
	m.mu.Lock()
	for i := range m.appThreads {
		if m.appThreads[i].ID == threadID {
			m.appThreads[i].TurnID = turnID
			m.appThreads[i].LastTurnStatus = "interrupted"
			m.appThreads[i].State = model.StateIdle
			m.appThreads[i].StateSource = "app_server_control"
			m.appThreads[i].StateConfidence = "exact"
			m.appThreads[i].UpdatedAt = now
		}
	}
	for id, pending := range m.pendingRequests {
		if pending.request.ThreadID == threadID && pending.request.TurnID == turnID {
			delete(m.pendingRequests, id)
		}
	}
	m.rebuildLocked(now)
	m.mu.Unlock()
	return ActionResult{OK: true, Action: "interrupt", ThreadID: threadID, TurnID: turnID}, nil
}

func (m *Monitor) lookupPending(requestID, threadID, turnID, requestType string) (*pendingControl, error) {
	m.mu.RLock()
	pending := m.pendingRequests[requestID]
	m.mu.RUnlock()
	if pending == nil {
		return nil, ErrRequestNotFound
	}
	if pending.request.ThreadID != threadID || pending.request.TurnID != turnID || pending.request.Type != requestType {
		return nil, ErrRequestConflict
	}
	if !pending.request.Controllable {
		return nil, ErrNotControllable
	}
	return pending, nil
}

func (m *Monitor) finishPending(requestID string, expected *pendingControl) {
	now := time.Now().UTC()
	m.mu.Lock()
	if m.pendingRequests[requestID] == expected {
		delete(m.pendingRequests, requestID)
	}
	m.rebuildLocked(now)
	m.mu.Unlock()
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
