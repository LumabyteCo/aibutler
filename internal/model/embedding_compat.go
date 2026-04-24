package model

import (
	"net/http"
	"time"
)

// DefaultOllamaEmbeddingCompatURL is the default Ollama OpenAI-compatible embedding endpoint.
const DefaultOllamaEmbeddingCompatURL = "http://localhost:11434/v1/embeddings"

// NewEmbeddingCompat creates an OpenAI-compatible embedding adapter for local LLM servers.
// Covers: Ollama (/v1/embeddings), LM Studio, llama.cpp, LocalAI, vLLM, GPT4All, Jan,
// and any endpoint implementing the OpenAI Embeddings API format.
//
// apiKey can be empty for local models that don't require authentication.
// baseURL should point to the /v1/embeddings endpoint.
func NewEmbeddingCompat(baseURL, apiKey, model string, timeout time.Duration) *EmbeddingOpenAI {
	if baseURL == "" {
		baseURL = DefaultOllamaEmbeddingCompatURL
	}
	dim := knownEmbedDimensions[model]
	return &EmbeddingOpenAI{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: timeout},
		baseURL: baseURL,
		dim:     dim,
	}
}
