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

func TestOpenAIComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing Authorization header")
		}

		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)
		if req.Model != "gpt-4o" {
			t.Errorf("model = %q, want 'gpt-4o'", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Errorf("messages count = %d, want 2", len(req.Messages))
		}

		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMessage{Role: "assistant", Content: "Hello from GPT!"},
			}},
			Usage: openaiUsage{PromptTokens: 15, CompletionTokens: 8},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL

	messages := []agent.Message{
		{Role: "system", Content: "Be helpful."},
		{Role: "user", Content: "Hi"},
	}

	resp, err := adapter.Complete(context.Background(), messages)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "Hello from GPT!" {
		t.Errorf("content = %q, want 'Hello from GPT!'", resp.Content)
	}
	if resp.TokensIn != 15 {
		t.Errorf("TokensIn = %d, want 15", resp.TokensIn)
	}
	if resp.TokensOut != 8 {
		t.Errorf("TokensOut = %d, want 8", resp.TokensOut)
	}
}

func TestOpenAIToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)
		// Verify sanitized name sent to API (dots → double underscores).
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "expense__log" {
			t.Errorf("expected 1 tool 'expense__log', got %q", req.Tools[0].Function.Name)
		}

		// API returns tool call with sanitized name (as real APIs do).
		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMessage{
					Role:    "assistant",
					Content: "Logging that expense.",
					ToolCalls: []openaiToolCall{
						{
							ID:   "call_abc",
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{
								Name:      "expense__log",
								Arguments: `{"amount":42.50,"category":"food"}`,
							},
						},
					},
				},
			}},
			Usage: openaiUsage{PromptTokens: 20, CompletionTokens: 12},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL
	adapter.SetTools([]agent.ToolDef{
		{Name: "expense.log", Description: "Log expense", Schema: `{"type":"object"}`},
	})

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "I spent $42.50 on food"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "expense.log" {
		t.Errorf("tool name = %q, want 'expense.log'", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].ID != "call_abc" {
		t.Errorf("tool ID = %q, want 'call_abc'", resp.ToolCalls[0].ID)
	}
}

func TestOpenAIToolNameRoundTrip(t *testing.T) {
	// Verify that tool names with dots survive the sanitize → API → unsanitize round trip.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)

		// Verify ALL tool names are sanitized (no dots).
		for _, tool := range req.Tools {
			if strings.Contains(tool.Function.Name, ".") {
				t.Errorf("tool name %q contains dots — API would reject this", tool.Function.Name)
			}
		}

		// Echo back a tool call with the sanitized name (as the real API does).
		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMessage{
					Role: "assistant",
					ToolCalls: []openaiToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      req.Tools[0].Function.Name,
							Arguments: `{}`,
						},
					}},
				},
			}},
			Usage: openaiUsage{PromptTokens: 10, CompletionTokens: 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL
	adapter.SetTools([]agent.ToolDef{
		{Name: "task.add", Description: "Add task"},
		{Name: "expense.log", Description: "Log expense"},
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

func TestOpenAICompatOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No auth header for local models.
		if r.Header.Get("Authorization") != "" {
			t.Error("local model should not send Authorization header")
		}

		body, _ := io.ReadAll(r.Body)
		var req openaiRequest
		json.Unmarshal(body, &req)
		if req.Model != "llama3" {
			t.Errorf("model = %q, want 'llama3'", req.Model)
		}

		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMessage{Role: "assistant", Content: "Hello from Ollama!"},
			}},
			// Local models may not return usage.
			Usage: openaiUsage{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAICompat(server.URL, "", "llama3", 5*time.Second, 0)

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "Hello from Ollama!" {
		t.Errorf("content = %q, want 'Hello from Ollama!'", resp.Content)
	}
}

func TestOpenAICompatZeroCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMessage{Role: "assistant", Content: "Response"},
			}},
			// No usage returned by local model.
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAICompat(server.URL, "", "llama3", 5*time.Second, 0)

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.TokensIn != 0 || resp.TokensOut != 0 {
		t.Errorf("tokens = %d/%d, want 0/0 for local model", resp.TokensIn, resp.TokensOut)
	}

	// Verify cost calculation.
	cost := estimateCost("local", resp.TokensIn, resp.TokensOut)
	if cost != 0.0 {
		t.Errorf("cost = %f, want 0.0 for local model", cost)
	}
}

func TestOpenAIRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"internal error"}`))
			return
		}
		resp := openaiResponse{
			Choices: []openaiChoice{{
				Message: openaiMessage{Role: "assistant", Content: "OK"},
			}},
			Usage: openaiUsage{PromptTokens: 5, CompletionTokens: 2},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 3)
	adapter.baseURL = server.URL

	resp, err := adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "OK" {
		t.Errorf("content = %q, want 'OK'", resp.Content)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

// --- Multimodal / vision tests ---

// TestOpenAI_Multimodal_BuildsContentArray verifies that when a Message
// has Images, the outbound request renders Content as an array of typed
// parts (text + image_url) instead of a plain string.
func TestOpenAI_Multimodal_BuildsContentArray(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)

		resp := openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Role: "assistant", Content: "saw it"}}},
			Usage:   openaiUsage{PromptTokens: 5, CompletionTokens: 2},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewOpenAI("test-key", "gpt-4o", 5*time.Second, 0)
	adapter.baseURL = server.URL

	messages := []agent.Message{
		{
			Role:    "user",
			Content: "What's in this image?",
			Images: []agent.Image{
				{Source: agent.ImageSourceBase64, Data: "iVBORw0KAAA=", MimeType: "image/png"},
			},
		},
	}
	if _, err := adapter.Complete(context.Background(), messages); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	rawMsgs, _ := captured["messages"].([]interface{})
	if len(rawMsgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(rawMsgs))
	}
	msg, _ := rawMsgs[0].(map[string]interface{})
	parts, ok := msg["content"].([]interface{})
	if !ok {
		t.Fatalf("multimodal message content should be an array, got %T: %v", msg["content"], msg["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image_url), got %d: %v", len(parts), parts)
	}

	// Text part.
	textPart, _ := parts[0].(map[string]interface{})
	if textPart["type"] != "text" {
		t.Errorf("part[0].type = %v, want text", textPart["type"])
	}
	if textPart["text"] != "What's in this image?" {
		t.Errorf("part[0].text = %v", textPart["text"])
	}

	// Image part.
	imagePart, _ := parts[1].(map[string]interface{})
	if imagePart["type"] != "image_url" {
		t.Errorf("part[1].type = %v, want image_url", imagePart["type"])
	}
	urlObj, _ := imagePart["image_url"].(map[string]interface{})
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(urlObj["url"].(string), wantPrefix) {
		t.Errorf("image_url.url = %q, want prefix %q", urlObj["url"], wantPrefix)
	}
}

// TestOpenAI_Multimodal_URLImage verifies URL-source images are passed
// through verbatim without data: wrapping.
func TestOpenAI_Multimodal_URLImage(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer server.Close()

	adapter := NewOpenAI("k", "m", 5*time.Second, 0)
	adapter.baseURL = server.URL
	_, _ = adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "describe", Images: []agent.Image{
			{Source: agent.ImageSourceURL, Data: "https://example.com/cat.png"},
		}},
	})

	rawMsgs, _ := captured["messages"].([]interface{})
	parts := rawMsgs[0].(map[string]interface{})["content"].([]interface{})
	imgPart := parts[1].(map[string]interface{})
	urlObj := imgPart["image_url"].(map[string]interface{})
	if urlObj["url"] != "https://example.com/cat.png" {
		t.Errorf("URL image should pass through verbatim, got %v", urlObj["url"])
	}
}

// TestOpenAI_TextOnly_UnchangedShape verifies that messages without Images
// continue to render Content as a plain string (no regression for the
// existing text-only path).
func TestOpenAI_TextOnly_UnchangedShape(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		json.NewEncoder(w).Encode(openaiResponse{
			Choices: []openaiChoice{{Message: openaiMessage{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer server.Close()

	adapter := NewOpenAI("k", "m", 5*time.Second, 0)
	adapter.baseURL = server.URL
	_, _ = adapter.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "plain text only"},
	})

	rawMsgs, _ := captured["messages"].([]interface{})
	msg := rawMsgs[0].(map[string]interface{})
	if _, isString := msg["content"].(string); !isString {
		t.Errorf("text-only message content should still be a string, got %T", msg["content"])
	}
}

// TestImageToDataURL_DefaultMimeType verifies the helper handles missing
// MimeType gracefully (defaults to image/png).
func TestImageToDataURL_DefaultMimeType(t *testing.T) {
	got := imageToDataURL(agent.Image{Source: agent.ImageSourceBase64, Data: "abc"})
	want := "data:image/png;base64,abc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImageToDataURL_EmptyDataReturnsEmpty(t *testing.T) {
	if got := imageToDataURL(agent.Image{Source: agent.ImageSourceBase64, Data: ""}); got != "" {
		t.Errorf("empty Data should yield empty URL, got %q", got)
	}
}

func TestImageToDataURL_BackwardCompatNoSource(t *testing.T) {
	// Empty Source with non-empty Data falls back to base64 (helps callers
	// that build Image structs without setting Source explicitly).
	got := imageToDataURL(agent.Image{Data: "abc", MimeType: "image/jpeg"})
	want := "data:image/jpeg;base64,abc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
