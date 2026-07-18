package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codex-monitor-agent/internal/model"
	"codex-monitor-agent/internal/monitor"
)

func TestCodexHookEndpoint(t *testing.T) {
	m := monitor.New(monitor.Config{CodexBinary: "codex-does-not-exist-for-test"})
	server := New("127.0.0.1:0", m, "secret")
	payload := `{"session_id":"session-http","turn_id":"turn-http","cwd":"/tmp/project","hook_event_name":"PermissionRequest","tool_name":"Bash"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/hooks/codex", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var result monitor.HookResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != model.StateWaitingApproval || !result.Recorded {
		t.Fatalf("unexpected hook result: %+v", result)
	}
	snapshot := m.Snapshot()
	if len(snapshot.Threads) != 1 || snapshot.Threads[0].State != model.StateWaitingApproval {
		t.Fatalf("hook was not reflected in snapshot: %+v", snapshot.Threads)
	}
}

func TestCodexHookEndpointRejectsMissingSession(t *testing.T) {
	m := monitor.New(monitor.Config{CodexBinary: "codex-does-not-exist-for-test"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/hooks/codex", strings.NewReader(`{"hook_event_name":"Stop"}`))
	recorder := httptest.NewRecorder()
	request.Header.Set("Authorization", "Bearer secret")
	New("127.0.0.1:0", m, "secret").Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAPIRequiresBearerToken(t *testing.T) {
	m := monitor.New(monitor.Config{CodexBinary: "codex-does-not-exist-for-test"})
	server := New("127.0.0.1:0", m, "secret")
	for _, header := range []string{"", "Bearer wrong", "secret"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("header %q returned %d, want %d", header, recorder.Code, http.StatusUnauthorized)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestUsageEndpointRejectsInvalidDays(t *testing.T) {
	m := monitor.New(monitor.Config{CodexBinary: "codex-does-not-exist-for-test"})
	server := New("127.0.0.1:0", m, "secret")
	for _, days := range []string{"-1", "366", "invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/usage?days="+days, nil)
		request.Header.Set("Authorization", "Bearer secret")
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("days %q returned %d, want %d", days, recorder.Code, http.StatusBadRequest)
		}
	}
}
