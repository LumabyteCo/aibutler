package model

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

func TestOpenAICompatSatisfiesStreamingModelAdapter(t *testing.T) {
	adapter := NewOpenAICompat("http://localhost:11434/v1/chat/completions", "", "llama3", 0, 0)

	// Verify that the OpenAI compat adapter satisfies StreamingModelAdapter.
	var _ agent.StreamingModelAdapter = adapter

	// Additional check: the adapter should also satisfy ModelAdapter.
	var _ agent.ModelAdapter = adapter
}
