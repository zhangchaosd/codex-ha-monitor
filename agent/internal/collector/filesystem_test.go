package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codex-monitor-agent/internal/model"
)

func TestFilesystemCollectorDetectsActiveTurnAfterTaskComplete(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions", "2026", "07", "16")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	content := fmt.Sprintf(
		"{\"timestamp\":%q,\"type\":\"session_meta\",\"payload\":{\"id\":\"thread-1\",\"cwd\":\"/tmp/repo\",\"source\":\"vscode\",\"cli_version\":\"0.144.5\"}}\n"+
			"{\"timestamp\":%q,\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"+
			"{\"timestamp\":%q,\"type\":\"response_item\",\"payload\":{\"type\":\"message\"}}\n",
		now.Add(-time.Minute).Format(time.RFC3339Nano), now.Add(-30*time.Second).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err := os.WriteFile(filepath.Join(sessions, "rollout.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte("{\"id\":\"thread-1\",\"thread_name\":\"Active thread\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	threads, err := (FilesystemCollector{Home: home, ActiveWindow: time.Minute, MaxThreads: 10}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].State != model.StateRunning || threads[0].Name != "Active thread" {
		t.Fatalf("unexpected threads: %+v", threads)
	}
}

func TestFilesystemCollectorExtractsSubagentHierarchy(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions", "2026", "07", "18")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	content := fmt.Sprintf(
		"{\"timestamp\":%q,\"type\":\"session_meta\",\"payload\":{"+
			"\"id\":\"child\",\"cwd\":\"/tmp/repo\",\"source\":{"+
			"\"subagent\":{\"thread_spawn\":{\"parent_thread_id\":\"root\","+
			"\"agent_nickname\":\"reviewer\",\"agent_role\":\"explorer\"}}}}}\n",
		now.Format(time.RFC3339Nano),
	)
	if err := os.WriteFile(filepath.Join(sessions, "rollout.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	threads, err := (FilesystemCollector{Home: home, MaxThreads: 10}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("unexpected threads: %+v", threads)
	}
	thread := threads[0]
	if thread.SessionID != "child" || thread.ParentThreadID != "root" || thread.AgentNickname != "reviewer" || thread.AgentRole != "explorer" {
		t.Fatalf("subagent metadata was not preserved: %+v", thread)
	}
}
