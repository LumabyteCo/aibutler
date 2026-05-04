package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

func TestClaudeComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != claudeAPIVersion {
			t.Error("missing anthropic-version header")
		}

		// Verify request body.
		body, _ := io.ReadAll(r.Body)
		var req claudeRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("invalid request body: %v", err)
		}
		if req.System != "You are helpful." {
			t.Errorf("system = %q, want 'You are helpful.'", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content[0].Text != "Hello" {
			t.Error("expected 1 user message with 'Hello'")
		}

		// Return response.
		resp := claudeResponse{
			Content: []claudeContentBlock{
				{Type: "text", Text: "Hi there!"},
			},
			Usage: claudeUsage{InputTokens: 10, OutputTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	messages := []agent.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	}

	resp, err := adapter.Complete(context.Background(), messages)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "Hi there!" {
		t.Errorf("content = %q, want 'Hi there!'", resp.Content)
	}
	if resp.TokensIn != 10 {
		t.Errorf("TokensIn = %d, want 10", resp.TokensIn)
	}
	if resp.TokensOut != 5 {
		t.Errorf("TokensOut = %d, want 5", resp.TokensOut)
	}
}

func TestClaudeToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify tools are sent with sanitized names (dots → double underscores).
		body, _ := io.ReadAll(r.Body)
		var req claudeRequest
		json.Unmarshal(body, &req)
		if len(req.Tools) != 1 || req.Tools[0].Name != "task__add" {
			t.Errorf("expected 1 tool 'task__add' in request, got %q", req.Tools[0].Name)
		}

		// API returns tool call with sanitized name (as real APIs do).
		resp := claudeResponse{
			Content: []claudeContentBlock{
				{Type: "text", Text: "I'll add that task."},
				{
					Type:  "tool_use",
					ID:    "call_123",
					Name:  "task__add",
					Input: json.RawMessage(`{"content":"Buy groceries"}`),
				},
			},
			Usage: claudeUsage{InputTokens: 20, OutputTokens: 15},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL
	adapter.SetTools([]agent.ToolDef{
		{Name: "task.add", Description: "Add a task", Schema: `{"type":"object","properties":{"content":{"type":"string"}}}`},
	})

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "Add buy groceries to my list"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ToolCall ID = %q, want 'call_123'", tc.ID)
	}
	if tc.Name != "task.add" {
		t.Errorf("ToolCall Name = %q, want 'task.add'", tc.Name)
	}
	if !strings.Contains(tc.Input, "Buy groceries") {
		t.Errorf("ToolCall Input = %q, want to contain 'Buy groceries'", tc.Input)
	}
}

func TestClaudeRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		resp := claudeResponse{
			Content: []claudeContentBlock{{Type: "text", Text: "Success after retry"}},
			Usage:   claudeUsage{InputTokens: 5, OutputTokens: 3},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 2)
	adapter.baseURL = server.URL

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "Success after retry" {
		t.Errorf("content = %q, want 'Success after retry'", resp.Content)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestClaudeTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		json.NewEncoder(w).Encode(claudeResponse{})
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 100*time.Millisecond, 0)
	adapter.baseURL = server.URL

	_, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"task.add", "task__add"},
		{"expense.log", "expense__log"},
		{"cost.status", "cost__status"},
		{"simple", "simple"},                    // no dots → unchanged
		{"a.b.c", "a__b__c"},                    // multiple dots
		{"my_tool", "my_tool"},                   // underscores preserved
		{"my_tool.sub_cmd", "my_tool__sub_cmd"}, // mixed
	}
	for _, tc := range tests {
		got := sanitizeToolName(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestUnsanitizeToolName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"task__add", "task.add"},
		{"expense__log", "expense.log"},
		{"simple", "simple"},
		{"a__b__c", "a.b.c"},
		{"my_tool", "my_tool"},                   // single underscores preserved
		{"my_tool__sub_cmd", "my_tool.sub_cmd"},
	}
	for _, tc := range tests {
		got := unsanitizeToolName(tc.input)
		if got != tc.want {
			t.Errorf("unsanitizeToolName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestClaudeToolNameRoundTrip(t *testing.T) {
	// Verify that tool names with dots survive the sanitize → API → unsanitize round trip.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req claudeRequest
		json.Unmarshal(body, &req)

		// Verify ALL tool names are sanitized (no dots).
		for _, tool := range req.Tools {
			if strings.Contains(tool.Name, ".") {
				t.Errorf("tool name %q contains dots — API would reject this", tool.Name)
			}
		}

		// Echo back a tool_use with the sanitized name (as the real API does).
		resp := claudeResponse{
			Content: []claudeContentBlock{{
				Type:  "tool_use",
				ID:    "call_1",
				Name:  req.Tools[0].Name, // API echoes sanitized name
				Input: json.RawMessage(`{}`),
			}},
			Usage: claudeUsage{InputTokens: 10, OutputTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL
	adapter.SetTools([]agent.ToolDef{
		{Name: "task.add", Description: "Add task"},
		{Name: "expense.log", Description: "Log expense"},
		{Name: "cost.status", Description: "Cost status"},
	})

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	// The returned name should be unsanitized back to the original dotted form.
	if resp.ToolCalls[0].Name != "task.add" {
		t.Errorf("tool call name = %q, want 'task.add' (unsanitized)", resp.ToolCalls[0].Name)
	}
}

func TestClaudeToolResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req claudeRequest
		json.Unmarshal(body, &req)

		// Verify tool result is sent as user message with tool_result content.
		found := false
		for _, msg := range req.Messages {
			for _, block := range msg.Content {
				if block.Type == "tool_result" && block.ToolUseID == "call_1" {
					found = true
				}
			}
		}
		if !found {
			t.Error("expected tool_result block with tool_use_id 'call_1'")
		}

		resp := claudeResponse{
			Content: []claudeContentBlock{{Type: "text", Text: "Done!"}},
			Usage:   claudeUsage{InputTokens: 30, OutputTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-test", 5*time.Second, 0)
	adapter.baseURL = server.URL

	messages := []agent.Message{
		{Role: "user", Content: "Add task"},
		{Role: "assistant", Content: "I'll add that."},
		{Role: "tool", Content: "Task added (id: 1)", ToolID: "call_1"},
	}

	resp, err := adapter.Complete(context.Background(), messages)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "Done!" {
		t.Errorf("content = %q, want 'Done!'", resp.Content)
	}
}

// --- Multimodal / vision tests ---

func TestClaude_Multimodal_Base64Image(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(claudeResponse{
			Content: []claudeContentBlock{{Type: "text", Text: "ok"}},
			Usage:   claudeUsage{InputTokens: 10, OutputTokens: 2},
		})
	}))
	defer server.Close()

	adapter := NewClaude("test-key", "claude-sonnet-4-5", 5*time.Second, 0)
	adapter.baseURL = server.URL

	_, err := adapter.Complete(context.Background(), []agent.Message{
		{
			Role:    "user",
			Content: "describe",
			Images: []agent.Image{
				{Source: agent.ImageSourceBase64, Data: "iVBORw0KAAA=", MimeType: "image/png"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	rawMsgs := captured["messages"].([]interface{})
	msg := rawMsgs[0].(map[string]interface{})
	blocks := msg["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks (text + image), got %d", len(blocks))
	}

	textBlock := blocks[0].(map[string]interface{})
	if textBlock["type"] != "text" {
		t.Errorf("block[0].type = %v, want text", textBlock["type"])
	}

	imgBlock := blocks[1].(map[string]interface{})
	if imgBlock["type"] != "image" {
		t.Errorf("block[1].type = %v, want image", imgBlock["type"])
	}
	src := imgBlock["source"].(map[string]interface{})
	if src["type"] != "base64" {
		t.Errorf("source.type = %v, want base64", src["type"])
	}
	if src["media_type"] != "image/png" {
		t.Errorf("source.media_type = %v, want image/png", src["media_type"])
	}
	if src["data"] != "iVBORw0KAAA=" {
		t.Errorf("source.data did not match input")
	}
}

func TestClaude_Multimodal_URLImage(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(claudeResponse{
			Content: []claudeContentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer server.Close()

	adapter := NewClaude("k", "m", 5*time.Second, 0)
	adapter.baseURL = server.URL
	_, _ = adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "describe", Images: []agent.Image{
			{Source: agent.ImageSourceURL, Data: "https://example.com/cat.png"},
		}},
	})

	rawMsgs := captured["messages"].([]interface{})
	blocks := rawMsgs[0].(map[string]interface{})["content"].([]interface{})
	imgBlock := blocks[1].(map[string]interface{})
	src := imgBlock["source"].(map[string]interface{})
	if src["type"] != "url" {
		t.Errorf("source.type = %v, want url", src["type"])
	}
	if src["url"] != "https://example.com/cat.png" {
		t.Errorf("source.url did not match input, got %v", src["url"])
	}
}

func TestClaude_TextOnly_NoSourceBlock(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(claudeResponse{
			Content: []claudeContentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer server.Close()

	adapter := NewClaude("k", "m", 5*time.Second, 0)
	adapter.baseURL = server.URL
	_, _ = adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "plain text"},
	})

	rawMsgs := captured["messages"].([]interface{})
	blocks := rawMsgs[0].(map[string]interface{})["content"].([]interface{})
	if len(blocks) != 1 {
		t.Fatalf("text-only message should have 1 block, got %d", len(blocks))
	}
	if _, hasSource := blocks[0].(map[string]interface{})["source"]; hasSource {
		t.Errorf("text-only block should NOT have a source field (would be a regression)")
	}
}

func TestClaudeImageBlock_EmptyDataSkipped(t *testing.T) {
	if _, ok := claudeImageBlock(agent.Image{Source: agent.ImageSourceBase64}); ok {
		t.Error("empty Data should not produce an image block")
	}
	if _, ok := claudeImageBlock(agent.Image{Source: agent.ImageSourceURL}); ok {
		t.Error("empty URL should not produce an image block")
	}
}

func TestClaudeImageBlock_DefaultMediaType(t *testing.T) {
	block, ok := claudeImageBlock(agent.Image{Source: agent.ImageSourceBase64, Data: "abc"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if block.Source.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", block.Source.MediaType)
	}
}

func TestClaudeImageBlock_BackwardCompatNoSource(t *testing.T) {
	// Empty Source with non-empty Data falls back to base64.
	block, ok := claudeImageBlock(agent.Image{Data: "abc", MimeType: "image/jpeg"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if block.Source.Type != "base64" || block.Source.MediaType != "image/jpeg" || block.Source.Data != "abc" {
		t.Errorf("backward-compat path produced wrong source: %+v", block.Source)
	}
}
