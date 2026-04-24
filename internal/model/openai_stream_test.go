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

func TestOpenAIStreamText(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var text string
	var gotStop bool
	for e := range ch {
		if e.Type == "text_delta" {
			text += e.Text
		}
		if e.Type == "message_stop" {
			gotStop = true
		}
	}

	if text != "Hello world" {
		t.Errorf("accumulated text = %q, want 'Hello world'", text)
	}
	if !gotStop {
		t.Error("expected message_stop event")
	}
}

func TestOpenAIStreamToolCallAccumulation(t *testing.T) {
	// Tool call arrives in chunks: first ID+name, then arguments in pieces.
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_abc\",\"type\":\"function\",\"function\":{\"name\":\"task__add\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"content\\\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\":\\\"buy milk\\\"}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
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
			if e.ToolCallID != "call_abc" {
				t.Errorf("tool_use_start ID = %q, want 'call_abc'", e.ToolCallID)
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

func TestOpenAIStreamDeferredToolStart(t *testing.T) {
	// Name arrives in first chunk but arguments are empty — tool_use_start should
	// be deferred until the first non-empty arguments chunk.
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_x\",\"type\":\"function\",\"function\":{\"name\":\"expense__log\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"amount\\\":42}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var events []agent.StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	// tool_use_start should come BEFORE input_json_delta.
	var toolStartIdx, jsonDeltaIdx int
	for i, e := range events {
		if e.Type == "tool_use_start" {
			toolStartIdx = i
		}
		if e.Type == "input_json_delta" && jsonDeltaIdx == 0 {
			jsonDeltaIdx = i
		}
	}

	if toolStartIdx == 0 && jsonDeltaIdx == 0 {
		t.Fatal("expected both tool_use_start and input_json_delta events")
	}

	if toolStartIdx >= jsonDeltaIdx {
		t.Errorf("tool_use_start (index %d) should come before input_json_delta (index %d)",
			toolStartIdx, jsonDeltaIdx)
	}
}

func TestOpenAIStreamFinishReasonMapping(t *testing.T) {
	// The stream with finish_reason "stop" should still emit message_stop.
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"Done\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var gotStop bool
	for e := range ch {
		if e.Type == "message_stop" {
			gotStop = true
		}
	}
	if !gotStop {
		t.Error("expected message_stop after [DONE]")
	}
}

func TestOpenAIStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"internal error"}`)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL

	_, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want to contain '500'", err.Error())
	}
}
