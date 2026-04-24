package model_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/model"
)

func geminiResponse(text string) map[string]interface{} {
	return map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"role": "model",
					"parts": []map[string]interface{}{
						{"text": text},
					},
				},
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     10,
			"candidatesTokenCount": 20,
		},
	}
}

func TestNewGemini(t *testing.T) {
	g := model.NewGemini("test-key", "gemini-2.0-flash", 30*time.Second, 3)
	if g == nil {
		t.Fatal("expected non-nil GeminiAdapter")
	}
}

func TestGeminiDefaultModel(t *testing.T) {
	// Empty model should default to gemini-2.0-flash.
	g := model.NewGemini("key", "", 10*time.Second, 0)
	if g == nil {
		t.Fatal("expected non-nil adapter")
	}
	// Just check it doesn't panic.
}

func TestGeminiComplete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Verify Content-Type.
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			http.Error(w, "bad content type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResponse("Hello from Gemini"))
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	resp, err := g.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "Hello from Gemini" {
		t.Errorf("expected 'Hello from Gemini', got %q", resp.Content)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 20 {
		t.Errorf("unexpected token counts: in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
}

func TestGeminiCompleteWithToolCall(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"role": "model",
						"parts": []map[string]interface{}{
							{
								"functionCall": map[string]interface{}{
									"name": "weather__current",
									"args": map[string]string{"location": "London"},
								},
							},
						},
					},
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     5,
				"candidatesTokenCount": 15,
			},
		})
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	resp, err := g.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "What's the weather in London?"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	// Tool name should be unsanitized back to dot notation.
	if resp.ToolCalls[0].Name != "weather.current" {
		t.Errorf("expected tool name 'weather.current', got %q", resp.ToolCalls[0].Name)
	}
}

func TestGeminiCompleteErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	_, err := g.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error on 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error, got: %v", err)
	}
}

func TestGeminiCompleteRetryOn500(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResponse("Retry success"))
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 3, ts.URL)
	resp, err := g.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("expected retry success: %v", err)
	}
	if resp.Content != "Retry success" {
		t.Errorf("expected 'Retry success', got %q", resp.Content)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 calls for retries, got %d", callCount)
	}
}

func TestGeminiCompleteEmptyCandidates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"candidates":    []interface{}{},
			"usageMetadata": map[string]interface{}{"promptTokenCount": 5, "candidatesTokenCount": 0},
		})
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	resp, err := g.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete with empty candidates: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content for no candidates, got %q", resp.Content)
	}
}

func TestGeminiSystemMessage(t *testing.T) {
	var receivedBody map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResponse("ok"))
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	_, err := g.Complete(context.Background(), []agent.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Verify systemInstruction was set.
	if receivedBody["systemInstruction"] == nil {
		t.Error("expected systemInstruction to be set")
	}
}

func TestGeminiSetTools(t *testing.T) {
	var receivedBody map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(geminiResponse("ok"))
	}))
	defer ts.Close()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	g.SetTools([]agent.ToolDef{
		{Name: "weather.current", Description: "Get weather", Schema: `{"type":"object"}`},
	})

	_, err := g.Complete(context.Background(), []agent.Message{
		{Role: "user", Content: "What's the weather?"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Verify tools were sent.
	if receivedBody["tools"] == nil {
		t.Error("expected tools to be set in request")
	}
}

func TestGeminiContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay response.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	g := model.NewGeminiWithBaseURL("test-key", "gemini-2.0-flash", 10*time.Second, 0, ts.URL)
	_, err := g.Complete(ctx, []agent.Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}
