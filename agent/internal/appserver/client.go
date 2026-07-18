package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type InitializeResult struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type incomingMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	result json.RawMessage
	err    error
}

type Client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	writeMu   sync.Mutex
	pending   map[string]chan response
	pendingMu sync.Mutex
	nextID    atomic.Int64
	done      chan error
	closeOnce sync.Once
	onMessage func(client *Client, id json.RawMessage, method string, params json.RawMessage)
}

func ConnectStdio(ctx context.Context, binary, clientVersion string, onMessage func(*Client, json.RawMessage, string, json.RawMessage)) (*Client, InitializeResult, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, InitializeResult{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, InitializeResult{}, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, InitializeResult{}, err
	}
	client := &Client{
		cmd: cmd, stdin: stdin, stdout: stdout,
		pending: map[string]chan response{}, done: make(chan error, 1), onMessage: onMessage,
	}
	go client.readLoop()

	var initResult InitializeResult
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Request(requestCtx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name": "codex_monitor_agent", "title": "Codex Monitor Agent", "version": clientVersion,
		},
		"capabilities": map[string]any{
			"experimentalApi": true, "mcpServerOpenaiFormElicitation": true,
		},
	}, &initResult); err != nil {
		_ = client.Close()
		return nil, InitializeResult{}, err
	}
	if err := client.Notify("initialized", map[string]any{}); err != nil {
		_ = client.Close()
		return nil, InitializeResult{}, err
	}
	return client, initResult, nil
}

func (c *Client) Done() <-chan error { return c.done }

func (c *Client) Request(ctx context.Context, method string, params any, target any) error {
	id := c.nextID.Add(1)
	key := fmt.Sprint(id)
	ch := make(chan response, 1)
	c.pendingMu.Lock()
	c.pending[key] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}()
	if err := c.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case reply := <-ch:
		if reply.err != nil {
			return reply.err
		}
		if target == nil || len(reply.result) == 0 {
			return nil
		}
		return json.Unmarshal(reply.result, target)
	}
}

func (c *Client) Notify(method string, params any) error {
	return c.write(map[string]any{"method": method, "params": params})
}

// Respond resolves a request initiated by app-server. The id must be copied
// verbatim from the incoming request so both numeric and string ids work.
func (c *Client) Respond(id json.RawMessage, result any) error {
	if len(id) == 0 {
		return fmt.Errorf("app-server response id is empty")
	}
	return c.write(struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result"`
	}{ID: id, Result: result})
}

func (c *Client) write(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var message incomingMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if len(message.ID) > 0 && message.Method == "" {
			key := string(message.ID)
			c.pendingMu.Lock()
			ch := c.pending[key]
			c.pendingMu.Unlock()
			if ch != nil {
				if message.Error != nil {
					ch <- response{err: fmt.Errorf("app-server error %d: %s", message.Error.Code, message.Error.Message)}
				} else {
					ch <- response{result: message.Result}
				}
			}
			continue
		}
		if message.Method != "" && c.onMessage != nil {
			c.onMessage(c, message.ID, message.Method, message.Params)
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.failPending(err)
	select {
	case c.done <- err:
	default:
	}
}

func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for _, ch := range c.pending {
		select {
		case ch <- response{err: err}:
		default:
		}
	}
}

func (c *Client) Close() error {
	var result error
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			result = c.cmd.Wait()
		}
	})
	return result
}
