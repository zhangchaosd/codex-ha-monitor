package model

import "time"

const SchemaVersion = "1.1"

const (
	StateRunning         = "RUNNING"
	StateWaitingApproval = "WAITING_APPROVAL"
	StateWaitingInput    = "WAITING_INPUT"
	StateIdle            = "IDLE"
	StateError           = "ERROR"
	StateUnknown         = "UNKNOWN"
)

type Thread struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id,omitempty"`
	TurnID          string    `json:"turn_id,omitempty"`
	ParentThreadID  string    `json:"parent_thread_id,omitempty"`
	RootThreadID    string    `json:"root_thread_id,omitempty"`
	ThreadRole      string    `json:"thread_role,omitempty"`
	AgentNickname   string    `json:"agent_nickname,omitempty"`
	AgentRole       string    `json:"agent_role,omitempty"`
	Name            string    `json:"name,omitempty"`
	Preview         string    `json:"preview,omitempty"`
	CWD             string    `json:"cwd,omitempty"`
	Source          string    `json:"source,omitempty"`
	CLIVersion      string    `json:"cli_version,omitempty"`
	State           string    `json:"state"`
	StateSource     string    `json:"state_source"`
	StateConfidence string    `json:"state_confidence"`
	Loaded          bool      `json:"loaded"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastTurnStatus  string    `json:"last_turn_status,omitempty"`
	LastHookEvent   string    `json:"last_hook_event,omitempty"`
	AttentionType   string    `json:"attention_type,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	Controllable    bool      `json:"controllable"`
}

type StateCounts struct {
	Running         int `json:"running"`
	WaitingApproval int `json:"waiting_approval"`
	WaitingInput    int `json:"waiting_input"`
	Idle            int `json:"idle"`
	Error           int `json:"error"`
	Unknown         int `json:"unknown"`
}

type Summary struct {
	WorkloadState   string      `json:"workload_state"`
	StateSource     string      `json:"state_source"`
	StateConfidence string      `json:"state_confidence"`
	KnownThreads    int         `json:"known_threads"`
	ActiveThreads   int         `json:"active_threads"`
	ActiveWorkflows int         `json:"active_workflows"`
	ActiveWorkers   int         `json:"active_workers"`
	States          StateCounts `json:"states"`
}

const (
	EventTaskStarted      = "task_started"
	EventTaskCompleted    = "task_completed"
	EventApprovalRequired = "approval_required"
	EventInputRequired    = "input_required"
	EventTaskFailed       = "task_failed"
	EventTaskInterrupted  = "task_interrupted"
	EventTaskResumed      = "task_resumed"
	EventAgentRecovered   = "agent_recovered"
)

type TaskEvent struct {
	Sequence        uint64    `json:"sequence"`
	EventID         string    `json:"event_id"`
	Type            string    `json:"type"`
	InstallationID  string    `json:"installation_id"`
	ThreadID        string    `json:"thread_id,omitempty"`
	TurnID          string    `json:"turn_id,omitempty"`
	ParentThreadID  string    `json:"parent_thread_id,omitempty"`
	RootThreadID    string    `json:"root_thread_id,omitempty"`
	ThreadRole      string    `json:"thread_role,omitempty"`
	AgentNickname   string    `json:"agent_nickname,omitempty"`
	TaskName        string    `json:"task_name,omitempty"`
	FromState       string    `json:"from_state,omitempty"`
	ToState         string    `json:"to_state,omitempty"`
	StateSource     string    `json:"state_source,omitempty"`
	StateConfidence string    `json:"state_confidence,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	Controllable    bool      `json:"controllable"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type PendingRequest struct {
	ID                 string           `json:"id"`
	Type               string           `json:"type"`
	Method             string           `json:"method"`
	ThreadID           string           `json:"thread_id"`
	TurnID             string           `json:"turn_id,omitempty"`
	ItemID             string           `json:"item_id,omitempty"`
	Summary            string           `json:"summary,omitempty"`
	CWD                string           `json:"cwd,omitempty"`
	Questions          []map[string]any `json:"questions,omitempty"`
	AvailableDecisions []string         `json:"available_decisions,omitempty"`
	Controllable       bool             `json:"controllable"`
	CreatedAt          time.Time        `json:"created_at"`
}

type HostInfo struct {
	Name string `json:"name"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type AgentInfo struct {
	Version       string `json:"version"`
	GoVersion     string `json:"go_version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

type CodexCLIInfo struct {
	Binary  string `json:"binary,omitempty"`
	Raw     string `json:"raw,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

type AppServerInfo struct {
	UserAgent string `json:"user_agent,omitempty"`
	CodexHome string `json:"codex_home,omitempty"`
}

type HookInfo struct {
	ReceivedEvents int64      `json:"received_events"`
	ActiveSessions int        `json:"active_sessions"`
	LastEventAt    *time.Time `json:"last_event_at"`
}

type CodexInfo struct {
	ConnectionState     string     `json:"connection_state"`
	Visibility          string     `json:"visibility"`
	LastSuccessAt       *time.Time `json:"last_success_at"`
	LastError           string     `json:"last_error,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastRecoveryAt      *time.Time `json:"last_recovery_at,omitempty"`
}

type Snapshot struct {
	SchemaVersion   string           `json:"schema_version"`
	GeneratedAt     time.Time        `json:"generated_at"`
	Stale           bool             `json:"stale"`
	InstallationID  string           `json:"installation_id"`
	Host            HostInfo         `json:"host"`
	Agent           AgentInfo        `json:"agent"`
	CodexCLI        CodexCLIInfo     `json:"codex_cli"`
	AppServer       AppServerInfo    `json:"app_server"`
	Hooks           HookInfo         `json:"hooks"`
	Codex           CodexInfo        `json:"codex"`
	Summary         Summary          `json:"summary"`
	Threads         []Thread         `json:"threads,omitempty"`
	PendingRequests []PendingRequest `json:"pending_requests,omitempty"`
	Usage           map[string]any   `json:"usage,omitempty"`
	RateLimits      map[string]any   `json:"rate_limits,omitempty"`
}

type VersionResponse struct {
	SchemaVersion  string        `json:"schema_version"`
	InstallationID string        `json:"installation_id"`
	Agent          AgentInfo     `json:"agent"`
	CodexCLI       CodexCLIInfo  `json:"codex_cli"`
	AppServer      AppServerInfo `json:"app_server"`
}

func Summarize(threads []Thread, sharedVisibility bool) Summary {
	s := Summary{
		WorkloadState:   StateUnknown,
		StateSource:     "none",
		StateConfidence: "unknown",
		KnownThreads:    len(threads),
	}

	bestRank := 0
	activeWorkflows := map[string]struct{}{}
	for _, thread := range threads {
		rank := 0
		switch thread.State {
		case StateWaitingApproval:
			s.States.WaitingApproval++
			s.ActiveThreads++
			rank = 6
		case StateWaitingInput:
			s.States.WaitingInput++
			s.ActiveThreads++
			rank = 5
		case StateError:
			s.States.Error++
			rank = 4
		case StateRunning:
			s.States.Running++
			s.ActiveThreads++
			rank = 3
		case StateIdle:
			s.States.Idle++
			rank = 2
		default:
			s.States.Unknown++
			rank = 1
		}
		if thread.State == StateRunning || thread.State == StateWaitingApproval || thread.State == StateWaitingInput {
			rootID := thread.RootThreadID
			if rootID == "" {
				rootID = thread.ID
			}
			activeWorkflows[rootID] = struct{}{}
		}
		if rank > bestRank {
			bestRank = rank
			s.WorkloadState = thread.State
			s.StateSource = thread.StateSource
			s.StateConfidence = thread.StateConfidence
		}
	}
	s.ActiveWorkers = s.ActiveThreads
	s.ActiveWorkflows = len(activeWorkflows)

	if len(threads) == 0 || (!sharedVisibility && s.ActiveThreads == 0 && s.States.Error == 0) {
		s.WorkloadState = StateUnknown
		s.StateSource = "none"
		s.StateConfidence = "unknown"
	}
	return s
}
