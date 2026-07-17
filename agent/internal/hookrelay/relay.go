package hookrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const DefaultURL = "http://127.0.0.1:8765/api/v1/hooks/codex"

var events = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest",
	"PostToolUse", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop", "Stop",
}

func Forward(ctx context.Context, reader io.Reader, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, io.LimitReader(reader, 1024*1024))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("hook endpoint returned %s", response.Status)
	}
	return nil
}

func Config(executable, endpoint string) map[string]any {
	unixCommand := shellQuote(executable) + " hook-forward " + shellQuote(endpoint)
	windowsCommand := windowsQuote(executable) + " hook-forward " + windowsQuote(endpoint)
	hooks := make(map[string]any, len(events))
	for _, event := range events {
		hooks[event] = []any{map[string]any{
			"hooks": []any{map[string]any{
				"type": "command", "command": unixCommand, "commandWindows": windowsCommand,
				"timeout": 2,
			}},
		}}
	}
	return map[string]any{"hooks": hooks}
}

func PrintConfig(writer io.Writer, executable, endpoint string) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(Config(executable, endpoint))
}

func Executable() string {
	path, err := os.Executable()
	if err != nil {
		return "codex-monitor-agent"
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func windowsQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
