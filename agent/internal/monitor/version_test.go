package monitor

import "testing"

func TestParseCodexVersion(t *testing.T) {
	if got := ParseCodexVersion("codex-cli 0.144.5"); got != "0.144.5" {
		t.Fatalf("unexpected version %q", got)
	}
}
