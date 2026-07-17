package collector

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codex-monitor-agent/internal/model"
)

const tailBytes int64 = 512 * 1024

type FilesystemCollector struct {
	Home         string
	ActiveWindow time.Duration
	MaxThreads   int
}

type sessionIndexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
}

type sessionRecord struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		CWD        string `json:"cwd"`
		Originator string `json:"originator"`
		CLIVersion string `json:"cli_version"`
		Source     any    `json:"source"`
	} `json:"payload"`
}

type sessionFile struct {
	path    string
	modTime time.Time
	size    int64
}

func (c FilesystemCollector) Collect(ctx context.Context) ([]model.Thread, error) {
	if c.Home == "" {
		return nil, errors.New("CODEX_HOME is empty")
	}
	maxThreads := c.MaxThreads
	if maxThreads <= 0 {
		maxThreads = 100
	}
	titles := c.readTitles()
	files, err := c.recentSessionFiles(ctx, maxThreads)
	if err != nil {
		return nil, err
	}
	threads := make([]model.Thread, 0, len(files))
	for _, file := range files {
		thread, parseErr := c.parseSession(file, titles)
		if parseErr == nil && thread.ID != "" {
			threads = append(threads, thread)
		}
	}
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
	})
	return threads, nil
}

func (c FilesystemCollector) readTitles() map[string]string {
	result := map[string]string{}
	f, err := os.Open(filepath.Join(c.Home, "session_index.jsonl"))
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var entry sessionIndexEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.ID != "" {
			result[entry.ID] = entry.ThreadName
		}
	}
	return result
}

func (c FilesystemCollector) recentSessionFiles(ctx context.Context, max int) ([]sessionFile, error) {
	root := filepath.Join(c.Home, "sessions")
	files := make([]sessionFile, 0, max)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			files = append(files, sessionFile{path: path, modTime: info.ModTime(), size: info.Size()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	if len(files) > max {
		files = files[:max]
	}
	return files, nil
}

func (c FilesystemCollector) parseSession(file sessionFile, titles map[string]string) (model.Thread, error) {
	f, err := os.Open(file.path)
	if err != nil {
		return model.Thread{}, err
	}
	defer f.Close()

	var thread model.Thread
	if err := scanRecords(io.LimitReader(f, tailBytes), func(record sessionRecord) {
		if record.Type == "session_meta" && thread.ID == "" {
			thread.ID = record.Payload.ID
			thread.CWD = record.Payload.CWD
			thread.Source = stringifySource(record.Payload.Source)
			thread.CLIVersion = record.Payload.CLIVersion
		}
	}); err != nil {
		return model.Thread{}, err
	}

	if file.size > tailBytes {
		if _, err := f.Seek(file.size-tailBytes, io.SeekStart); err != nil {
			return model.Thread{}, err
		}
		reader := bufio.NewReader(f)
		_, _ = reader.ReadString('\n')
		if err := c.scanTail(reader, &thread); err != nil {
			return model.Thread{}, err
		}
	} else {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return model.Thread{}, err
		}
		if err := c.scanTail(f, &thread); err != nil {
			return model.Thread{}, err
		}
	}

	if thread.ID == "" {
		return model.Thread{}, errors.New("session_meta id not found")
	}
	thread.Name = titles[thread.ID]
	thread.StateSource = "filesystem_inference"
	thread.StateConfidence = "inferred"
	thread.Loaded = false
	if thread.UpdatedAt.IsZero() {
		thread.UpdatedAt = file.modTime.UTC()
	}
	return thread, nil
}

func (c FilesystemCollector) scanTail(reader io.Reader, thread *model.Thread) error {
	var lastAt time.Time
	var completedAt time.Time
	err := scanRecords(reader, func(record sessionRecord) {
		at, err := time.Parse(time.RFC3339Nano, record.Timestamp)
		if err == nil && at.After(lastAt) {
			lastAt = at
		}
		if record.Type == "event_msg" && record.Payload.Type == "task_complete" && at.After(completedAt) {
			completedAt = at
		}
	})
	if err != nil {
		return err
	}
	thread.UpdatedAt = lastAt.UTC()
	activeWindow := c.ActiveWindow
	if activeWindow <= 0 {
		activeWindow = 60 * time.Second
	}
	switch {
	case !lastAt.IsZero() && lastAt.After(completedAt) && time.Since(lastAt) <= activeWindow:
		thread.State = model.StateRunning
	case !completedAt.IsZero() && !lastAt.After(completedAt):
		thread.State = model.StateIdle
	default:
		thread.State = model.StateUnknown
	}
	return nil
}

func scanRecords(reader io.Reader, fn func(sessionRecord)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record sessionRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err == nil {
			fn(record)
		}
	}
	return scanner.Err()
}

func stringifySource(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}
