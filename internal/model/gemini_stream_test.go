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

func TestGeminiStreamText(t *testing.T) {
	body := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}` + "\n\n" +
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]}}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":3}}` + "\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it hits the streaming endpoint.
		if !strings.Contains(r.URL.Path, "streamGenerateContent") {
			t.Errorf("expected streaming endpoint, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 5*time.Second, 0, server.URL)

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

func TestGeminiStreamFunctionCall(t *testing.T) {
	body := `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"task__add","args":{"content":"buy milk"}}}]}}],"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":8}}` + "\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	adapter := NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 5*time.Second, 0, server.URL)

	ch, err := adapter.CompleteStream(context.Background(), []agent.Message{
		{Role: "user", Content: "Add buy milk"},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var gotToolStart bool
	var gotJSON bool
	for e := range ch {
		if e.Type == "tool_use_start" {
			gotToolStart = true
			if e.ToolName != "task.add" {
				t.Errorf("tool_use_start name = %q, want 'task.add'", e.ToolName)
			}
		}
		if e.Type == "input_json_delta" {
			gotJSON = true
		}
	}

	if !gotToolStart {
		t.Error("expected tool_use_start event")
	}
	if !gotJSON {
		t.Error("expected input_json_delta event")
	}
}

func TestGeminiStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"internal error"}`)
	}))
	defer server.Close()

	adapter := NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 5*time.Second, 0, server.URL)

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
