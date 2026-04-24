package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const openaiEmbeddingURL = "https://api.openai.com/v1/embeddings"

// knownEmbedDimensions maps well-known embedding model names to their output dimensions.
// Used for Dimension() before the first API call; updated from actual responses.
var knownEmbedDimensions = map[string]int{
	// OpenAI
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
	// Ollama / open-source
	"nomic-embed-text":          768,
	"nomic-embed-text:v1.5":     768,
	"nomic-embed-text:latest":   768,
	"mxbai-embed-large":         1024,
	"mxbai-embed-large:latest":  1024,
	"all-minilm":                384,
	"all-minilm:latest":         384,
	"snowflake-arctic-embed":    1024,
	"bge-m3":                    1024,
	"bge-m3:latest":             1024,
}

// EmbeddingOpenAI implements vector.Embedder for the OpenAI Embeddings API.
// Also serves as the base for OpenAI-compatible endpoints (Ollama /v1, LM Studio, vLLM).
type EmbeddingOpenAI struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
	dim     int
}

// NewEmbeddingOpenAI creates an OpenAI embedding adapter.
func NewEmbeddingOpenAI(apiKey, model string, timeout time.Duration) *EmbeddingOpenAI {
	dim := knownEmbedDimensions[model]
	return &EmbeddingOpenAI{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: timeout},
		baseURL: openaiEmbeddingURL,
		dim:     dim,
	}
}

// Embed generates a single embedding vector.
func (e *EmbeddingOpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("openai embedding: empty response")
	}
	return results[0], nil
}

// EmbedBatch generates embedding vectors for multiple texts in one API call.
func (e *EmbeddingOpenAI) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embedBatch(ctx, texts)
}

// Dimension returns the embedding vector dimension.
// Returns a known value for well-known models, or the dimension from the first API response.
func (e *EmbeddingOpenAI) Dimension() int { return e.dim }

func (e *EmbeddingOpenAI) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Model: e.model,
		Input: inputs,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai embedding: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embedding: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai embedding: read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai embedding: status %d: %s", resp.StatusCode, string(body))
	}

	var result embeddingResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("openai embedding: parse response: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		if d.Index < len(embeddings) {
			embeddings[d.Index] = float64sToFloat32s(d.Embedding)
		}
	}

	// Cache dimension from first successful response.
	if e.dim == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
		e.dim = len(embeddings[0])
	}

	return embeddings, nil
}

// --- OpenAI Embedding API types ---

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Model string          `json:"model"`
	Usage embeddingUsage  `json:"usage"`
}

type embeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// float64sToFloat32s converts a float64 slice (from JSON) to float32 slice (for storage).
func float64sToFloat32s(f64 []float64) []float32 {
	f32 := make([]float32, len(f64))
	for i, v := range f64 {
		f32[i] = float32(v)
	}
	return f32
}
