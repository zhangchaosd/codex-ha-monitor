package hookrelay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestForwardPreservesNativePayload(t *testing.T) {
	want := `{"session_id":"abc","hook_event_name":"PreToolUse"}`
	received := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		received = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := Forward(context.Background(), strings.NewReader(want), server.URL); err != nil {
		t.Fatal(err)
	}
	if received != want {
		t.Fatalf("payload = %q, want %q", received, want)
	}
}

func TestConfigIncludesIdentityPreservingForwarder(t *testing.T) {
	config := Config("/Applications/CMA Agent/cma", DefaultURL)
	hooks := config["hooks"].(map[string]any)
	for _, event := range []string{"UserPromptSubmit", "PermissionRequest", "Stop"} {
		if hooks[event] == nil {
			t.Fatalf("missing %s hook", event)
		}
	}
	serialized := strings.Builder{}
	if err := PrintConfig(&serialized, "/Applications/CMA Agent/cma", DefaultURL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serialized.String(), "hook-forward") || !strings.Contains(serialized.String(), "commandWindows") {
		t.Fatalf("unexpected config: %s", serialized.String())
	}
}
