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
	authorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		received = string(data)
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := Forward(context.Background(), strings.NewReader(want), server.URL, "secret"); err != nil {
		t.Fatal(err)
	}
	if received != want {
		t.Fatalf("payload = %q, want %q", received, want)
	}
	if authorization != "Bearer secret" {
		t.Fatalf("Authorization = %q, want bearer token", authorization)
	}
}

func TestConfigIncludesIdentityPreservingForwarder(t *testing.T) {
	config := Config("/Applications/CMA Agent/cma", DefaultURL, "secret")
	hooks := config["hooks"].(map[string]any)
	for _, event := range []string{"UserPromptSubmit", "PermissionRequest", "Stop"} {
		if hooks[event] == nil {
			t.Fatalf("missing %s hook", event)
		}
	}
	serialized := strings.Builder{}
	if err := PrintConfig(&serialized, "/Applications/CMA Agent/cma", DefaultURL, "secret"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serialized.String(), "hook-forward --token") || !strings.Contains(serialized.String(), "commandWindows") {
		t.Fatalf("unexpected config: %s", serialized.String())
	}
}
