package model

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/prompt"
)

func TestFactoryCompactorWiring(t *testing.T) {
	// Test that the factory correctly receives and stores a compactor.
	compactor := prompt.NewCompactor(prompt.CompactorConfig{
		MaxEstimatedTokens:  1000,
		PreserveRecentCount: 2,
	})

	factory := NewFactory(FactoryConfig{
		Compactor: compactor,
	})

	if factory.compactor == nil {
		t.Fatal("compactor was not wired into factory")
	}

	// Verify ShouldCompact works through the factory's compactor.
	// Build messages that exceed the 1000 token threshold.
	var messages []agent.Message
	for i := 0; i < 50; i++ {
		messages = append(messages, agent.Message{
			Role:    "user",
			Content: "This is a reasonably long message that should contribute to the token count estimation when we accumulate enough of these messages together in the conversation history",
		})
	}

	if !factory.compactor.ShouldCompact(messages) {
		t.Error("expected ShouldCompact to return true for large message set")
	}

	// Verify compact works.
	compacted, meta, err := factory.compactor.Compact(messages)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}
	if meta == nil {
		t.Fatal("Compact metadata is nil")
	}
	if len(compacted) >= len(messages) {
		t.Errorf("compacted should have fewer messages: got %d, original %d", len(compacted), len(messages))
	}
}
