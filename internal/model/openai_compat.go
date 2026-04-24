package model

import (
	"net/http"
	"time"
)

// DefaultOllamaURL is the default base URL for Ollama's OpenAI-compatible endpoint.
const DefaultOllamaURL = "http://localhost:11434/v1/chat/completions"

// NewOpenAICompat creates an OpenAI-compatible model adapter for local LLM servers.
// Covers: Ollama, LM Studio, llama.cpp, LocalAI, vLLM, GPT4All, Jan, and any
// endpoint implementing the OpenAI Chat Completions API.
//
// apiKey can be empty for local models that don't require authentication.
// baseURL should point to the chat/completions endpoint.
func NewOpenAICompat(baseURL, apiKey, model string, timeout time.Duration, retries int) *OpenAIAdapter {
	if baseURL == "" {
		baseURL = DefaultOllamaURL
	}
	return &OpenAIAdapter{
		apiKey:      apiKey,
		model:       model,
		client:      &http.Client{Timeout: timeout},
		retries:     retries,
		baseURL:     baseURL,
		maxTokens:   8192,
		temperature: 0.7,
	}
}
