package appserver

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
)

func TestRespondPreservesServerRequestID(t *testing.T) {
	reader, writer := io.Pipe()
	client := &Client{stdin: writer}
	done := make(chan error, 1)
	go func() {
		done <- client.Respond(json.RawMessage(`"server-request"`), map[string]any{"decision": "accept"})
	}()
	line, err := bufio.NewReader(reader).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID     string         `json:"id"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "server-request" || response.Result["decision"] != "accept" {
		t.Fatalf("unexpected response: %+v", response)
	}
}
