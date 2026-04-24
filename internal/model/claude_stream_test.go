package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

func sseLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func TestClaudeStreamText(t *testing.T) {
	body := sseLines(
		"event: message_start",
		`data: {"type":"message_start","message":{"usage":{"input_tokens":25}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","content_block":{"type":"text"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop"}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","usage":{"output_tokens":10}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var events []agent.StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Expect: usage(input), text_delta("Hello"), text_delta(" world"), usage(output), message_stop
	if len(events) < 4 {
		t.Fatalf("got %d events, want >= 4", len(events))
	}

	// Check we got text deltas.
	var text string
	for _, e := range events {
		if e.Type == "text_delta" {
			text += e.Text
		}
	}
	if text != "Hello world" {
		t.Errorf("accumulated text = %q, want 'Hello world'", text)
	}

	// Check message_stop.
	last := events[len(events)-1]
	if last.Type != "message_stop" {
		t.Errorf("last event type = %q, want 'message_stop'", last.Type)
	}
}

func TestClaudeStreamToolUse(t *testing.T) {
	body := sseLines(
		"event: message_start",
		`data: {"type":"message_start","message":{"usage":{"input_tokens":15}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"call_1","name":"task__add"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"content\":"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"\"buy milk\"}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop"}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "Add buy milk"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var events []agent.StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// Check tool_use_start with unsanitized name.
	var gotToolStart bool
	var gotPartialJSON string
	for _, e := range events {
		if e.Type == "tool_use_start" {
			gotToolStart = true
			if e.ToolCallID != "call_1" {
				t.Errorf("tool_use_start ID = %q, want 'call_1'", e.ToolCallID)
			}
			if e.ToolName != "task.add" {
				t.Errorf("tool_use_start name = %q, want 'task.add'", e.ToolName)
			}
		}
		if e.Type == "input_json_delta" {
			gotPartialJSON += e.PartialJSON
		}
	}

	if !gotToolStart {
		t.Error("expected tool_use_start event")
	}
	if gotPartialJSON == "" {
		t.Error("expected input_json_delta events")
	}
}

func TestClaudeStreamMixedTextAndTool(t *testing.T) {
	body := sseLines(
		"event: message_start",
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","content_block":{"type":"text"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"I'll add that."}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop"}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"call_2","name":"task__add"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop"}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var gotText, gotTool bool
	for e := range ch {
		if e.Type == "text_delta" {
			gotText = true
		}
		if e.Type == "tool_use_start" {
			gotTool = true
		}
	}

	if !gotText {
		t.Error("expected text_delta event")
	}
	if !gotTool {
		t.Error("expected tool_use_start event")
	}
}

func TestClaudeStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	_, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err == nil {
		t.Fatal("expected error for 429 status")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want to contain '429'", err.Error())
	}
}

func TestClaudeStreamContextCancellation(t *testing.T) {
	// Server sends data slowly so we can cancel mid-stream.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		// Send initial event.
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n")
		flusher.Flush()

		// Block until client disconnects.
		<-r.Context().Done()
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := adapter.CompleteStream(ctx, []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	// Read first event.
	e := <-ch
	if e.Type != "usage" {
		t.Errorf("first event type = %q, want 'usage'", e.Type)
	}

	// Cancel context.
	cancel()

	// Drain remaining events — should get an error event.
	var gotError bool
	for e := range ch {
		if e.Type == "error" {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected error event after context cancellation")
	}
}
