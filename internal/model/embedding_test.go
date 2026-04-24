package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- OpenAI Embedding Tests ---

func TestEmbeddingOpenAI_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "text-embedding-3-small" {
			t.Errorf("expected model text-embedding-3-small, got %s", req.Model)
		}
		if len(req.Input) != 1 || req.Input[0] != "hello world" {
			t.Errorf("unexpected input: %v", req.Input)
		}

		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float64{0.1, 0.2, 0.3}, Index: 0},
			},
			Model: "text-embedding-3-small",
			Usage: embeddingUsage{PromptTokens: 2, TotalTokens: 2},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := &EmbeddingOpenAI{
		apiKey:  "test-key",
		model:   "text-embedding-3-small",
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: server.URL,
	}

	vec, err := adapter.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected values: %v", vec)
	}
	// Dimension should be cached after first call.
	if adapter.Dimension() != 3 {
		t.Errorf("expected dimension 3 after call, got %d", adapter.Dimension())
	}
}

func TestEmbeddingOpenAI_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)

		if len(req.Input) != 3 {
			t.Errorf("expected 3 inputs, got %d", len(req.Input))
		}

		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float64{1.0, 2.0}, Index: 0},
				{Embedding: []float64{3.0, 4.0}, Index: 1},
				{Embedding: []float64{5.0, 6.0}, Index: 2},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := &EmbeddingOpenAI{
		apiKey:  "test-key",
		model:   "test-model",
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: server.URL,
	}

	vecs, err := adapter.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(vecs))
	}
	if vecs[0][0] != 1.0 || vecs[1][0] != 3.0 || vecs[2][0] != 5.0 {
		t.Errorf("unexpected values: %v", vecs)
	}
}

func TestEmbeddingOpenAI_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	adapter := &EmbeddingOpenAI{
		apiKey:  "bad-key",
		model:   "test-model",
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: server.URL,
	}

	_, err := adapter.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
}

func TestEmbeddingOpenAI_NoAuthKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header for empty key")
		}
		resp := embeddingResponse{
			Data: []embeddingData{{Embedding: []float64{1.0}, Index: 0}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := &EmbeddingOpenAI{
		model:   "local-model",
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: server.URL,
	}

	vec, err := adapter.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 1 {
		t.Errorf("expected 1 dimension, got %d", len(vec))
	}
}

func TestEmbeddingOpenAI_KnownDimension(t *testing.T) {
	adapter := NewEmbeddingOpenAI("key", "text-embedding-3-small", 30*time.Second)
	if adapter.Dimension() != 1536 {
		t.Errorf("expected known dimension 1536 for text-embedding-3-small, got %d", adapter.Dimension())
	}
}

func TestEmbeddingOpenAI_UnknownDimensionZero(t *testing.T) {
	adapter := NewEmbeddingOpenAI("key", "custom-model", 30*time.Second)
	if adapter.Dimension() != 0 {
		t.Errorf("expected dimension 0 for unknown model, got %d", adapter.Dimension())
	}
}

// --- Ollama Embedding Tests ---

func TestEmbeddingOllama_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ollamaEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "nomic-embed-text:v1.5" {
			t.Errorf("expected model nomic-embed-text:v1.5, got %s", req.Model)
		}
		if len(req.Input) != 1 || req.Input[0] != "test input" {
			t.Errorf("unexpected input: %v", req.Input)
		}

		resp := ollamaEmbedResponse{
			Model:      "nomic-embed-text:v1.5",
			Embeddings: [][]float64{{0.5, 0.6, 0.7, 0.8}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewEmbeddingOllamaWithURL(server.URL, "nomic-embed-text:v1.5", 5*time.Second)

	vec, err := adapter.Embed(context.Background(), "test input")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 4 {
		t.Fatalf("expected 4 dimensions, got %d", len(vec))
	}
	if vec[0] != 0.5 || vec[3] != 0.8 {
		t.Errorf("unexpected values: %v", vec)
	}
	// Known dimension (768) takes precedence over mock response (4 dims).
	if adapter.Dimension() != 768 {
		t.Errorf("expected known dimension 768, got %d", adapter.Dimension())
	}
}

func TestEmbeddingOllama_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := ollamaEmbedResponse{
			Model: "nomic-embed-text",
			Embeddings: [][]float64{
				{1.0, 2.0},
				{3.0, 4.0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewEmbeddingOllamaWithURL(server.URL, "nomic-embed-text", 5*time.Second)

	vecs, err := adapter.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(vecs))
	}
	if vecs[0][0] != 1.0 || vecs[1][0] != 3.0 {
		t.Errorf("unexpected values: %v", vecs)
	}
}

func TestEmbeddingOllama_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	adapter := NewEmbeddingOllamaWithURL(server.URL, "nonexistent-model", 5*time.Second)

	_, err := adapter.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
}

func TestEmbeddingOllama_DefaultURL(t *testing.T) {
	adapter := NewEmbeddingOllama("nomic-embed-text", 30*time.Second)
	if adapter.baseURL != DefaultOllamaEmbedURL {
		t.Errorf("expected default URL %s, got %s", DefaultOllamaEmbedURL, adapter.baseURL)
	}
}

func TestEmbeddingOllama_KnownDimension(t *testing.T) {
	adapter := NewEmbeddingOllama("nomic-embed-text:v1.5", 30*time.Second)
	if adapter.Dimension() != 768 {
		t.Errorf("expected known dimension 768 for nomic-embed-text:v1.5, got %d", adapter.Dimension())
	}
}

// --- OpenAI-Compatible Embedding Tests ---

func TestEmbeddingCompat_UsesCustomBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResponse{
			Data: []embeddingData{{Embedding: []float64{0.1, 0.2}, Index: 0}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewEmbeddingCompat(server.URL, "", "nomic-embed-text", 5*time.Second)

	vec, err := adapter.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 2 {
		t.Fatalf("expected 2 dimensions, got %d", len(vec))
	}
}

func TestEmbeddingCompat_DefaultURL(t *testing.T) {
	adapter := NewEmbeddingCompat("", "", "test-model", 30*time.Second)
	if adapter.baseURL != DefaultOllamaEmbeddingCompatURL {
		t.Errorf("expected default URL %s, got %s", DefaultOllamaEmbeddingCompatURL, adapter.baseURL)
	}
}

func TestEmbeddingCompat_WithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-key" {
			t.Error("expected Authorization header with API key")
		}
		resp := embeddingResponse{
			Data: []embeddingData{{Embedding: []float64{1.0}, Index: 0}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewEmbeddingCompat(server.URL, "my-key", "model", 5*time.Second)

	_, err := adapter.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
}

// --- Shared Helper Tests ---

func TestFloat64sToFloat32s(t *testing.T) {
	input := []float64{1.5, 2.5, 3.5}
	output := float64sToFloat32s(input)
	if len(output) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(output))
	}
	if output[0] != 1.5 || output[1] != 2.5 || output[2] != 3.5 {
		t.Errorf("unexpected values: %v", output)
	}
}

func TestFloat64sToFloat32s_Empty(t *testing.T) {
	output := float64sToFloat32s(nil)
	if len(output) != 0 {
		t.Errorf("expected empty slice, got %v", output)
	}
}

func TestKnownEmbedDimensions(t *testing.T) {
	tests := map[string]int{
		"text-embedding-3-small": 1536,
		"text-embedding-3-large": 3072,
		"nomic-embed-text":       768,
		"nomic-embed-text:v1.5":  768,
		"all-minilm":             384,
		"mxbai-embed-large":      1024,
	}
	for model, want := range tests {
		got := knownEmbedDimensions[model]
		if got != want {
			t.Errorf("knownEmbedDimensions[%q] = %d, want %d", model, got, want)
		}
	}
}

// --- Embedder Interface Compliance ---

func TestEmbeddingOpenAI_ImplementsEmbedder(t *testing.T) {
	var _ interface {
		Embed(ctx context.Context, text string) ([]float32, error)
		EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
		Dimension() int
	} = (*EmbeddingOpenAI)(nil)
}

func TestEmbeddingOllama_ImplementsEmbedder(t *testing.T) {
	var _ interface {
		Embed(ctx context.Context, text string) ([]float32, error)
		EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
		Dimension() int
	} = (*EmbeddingOllama)(nil)
}
