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

// DefaultOllamaEmbedURL is the default Ollama native embedding endpoint.
const DefaultOllamaEmbedURL = "http://localhost:11434/api/embed"

// EmbeddingOllama implements vector.Embedder for the Ollama native embedding API.
// Uses the /api/embed endpoint which supports batch input.
type EmbeddingOllama struct {
	model   string
	client  *http.Client
	baseURL string
	dim     int
}

// NewEmbeddingOllama creates an Ollama embedding adapter with default URL (localhost:11434).
func NewEmbeddingOllama(model string, timeout time.Duration) *EmbeddingOllama {
	dim := knownEmbedDimensions[model]
	return &EmbeddingOllama{
		model:   model,
		client:  &http.Client{Timeout: timeout},
		baseURL: DefaultOllamaEmbedURL,
		dim:     dim,
	}
}

// NewEmbeddingOllamaWithURL creates an Ollama embedding adapter with a custom base URL.
// baseURL should point to the /api/embed endpoint, e.g. "http://host:11434/api/embed".
func NewEmbeddingOllamaWithURL(baseURL, model string, timeout time.Duration) *EmbeddingOllama {
	if baseURL == "" {
		baseURL = DefaultOllamaEmbedURL
	}
	dim := knownEmbedDimensions[model]
	return &EmbeddingOllama{
		model:   model,
		client:  &http.Client{Timeout: timeout},
		baseURL: baseURL,
		dim:     dim,
	}
}

// Embed generates a single embedding vector.
func (e *EmbeddingOllama) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.doEmbed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("ollama embedding: empty response")
	}
	return results[0], nil
}

// EmbedBatch generates embedding vectors for multiple texts in one API call.
func (e *EmbeddingOllama) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return e.doEmbed(ctx, texts)
}

// Dimension returns the embedding vector dimension.
func (e *EmbeddingOllama) Dimension() int { return e.dim }

func (e *EmbeddingOllama) doEmbed(ctx context.Context, inputs []string) ([][]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model: e.model,
		Input: inputs,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama embedding: status %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaEmbedResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("ollama embedding: parse response: %w", err)
	}

	embeddings := make([][]float32, len(result.Embeddings))
	for i, emb := range result.Embeddings {
		embeddings[i] = float64sToFloat32s(emb)
	}

	// Cache dimension from first successful response.
	if e.dim == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
		e.dim = len(embeddings[0])
	}

	return embeddings, nil
}

// --- Ollama Embed API types ---

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
}
