package model

import "testing"

func TestSummarizePrefersWaitingApproval(t *testing.T) {
	threads := []Thread{
		{State: StateRunning, StateSource: "filesystem_inference", StateConfidence: "inferred"},
		{State: StateWaitingApproval, StateSource: "app_server_event", StateConfidence: "exact"},
	}
	summary := Summarize(threads, true)
	if summary.WorkloadState != StateWaitingApproval || summary.ActiveThreads != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestSummarizeDoesNotClaimIdleWithoutSharedVisibility(t *testing.T) {
	summary := Summarize([]Thread{{State: StateIdle, StateSource: "filesystem_inference", StateConfidence: "inferred"}}, false)
	if summary.WorkloadState != StateUnknown {
		t.Fatalf("expected UNKNOWN, got %s", summary.WorkloadState)
	}
}
