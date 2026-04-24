//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/instruction"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/prompt"
)

// instructionAdapter bridges instruction.Store to prompt.InstructionProvider
// for integration tests (same pattern as cli/app.go).
type instructionAdapter struct {
	store *instruction.Store
}

func (a *instructionAdapter) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]prompt.InstructionEntry, error) {
	instructions, err := a.store.ActiveForPrompt(ctx, channel, sessionID)
	if err != nil {
		return nil, err
	}
	entries := make([]prompt.InstructionEntry, len(instructions))
	for i, inst := range instructions {
		entries[i] = prompt.InstructionEntry{
			Content:  inst.Content,
			Category: inst.Category,
			Priority: inst.Priority,
		}
	}
	return entries, nil
}

func (a *instructionAdapter) Count(ctx context.Context) (int, error) {
	return a.store.Count(ctx)
}

// TestE2EInstructionsInPrompt verifies that a learned instruction seeded in the
// database appears in the Tier 1 system message sent to the model.
func TestE2EInstructionsInPrompt(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Hola, como puedo ayudarte?"),
		},
	})

	ctx := context.Background()

	// Seed an instruction directly in the learned_instructions table.
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO learned_instructions (content, category, priority, scope, active, created_at, updated_at)
		 VALUES ('Always respond in Spanish', 'preference', 80, 'global', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed instruction: %v", err)
	}

	// Wire the instruction store into the composer so Tier 1 picks it up.
	instrStore := instruction.NewStore(p.DB)
	p.Composer.SetInstructionStore(&instructionAdapter{store: instrStore})

	p.sendMsg(t, "Hello there")

	// Verify the system message (first message in the first model call) contains the instruction.
	calls := p.Fake.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least one model call")
	}
	if len(calls[0]) == 0 {
		t.Fatal("expected at least one message in first model call")
	}

	systemMsg := calls[0][0].Content
	if calls[0][0].Role != "system" {
		t.Fatalf("first message role = %q, want 'system'", calls[0][0].Role)
	}
	if !strings.Contains(systemMsg, "Always respond in Spanish") {
		t.Errorf("system message should contain instruction text 'Always respond in Spanish', got: %s",
			systemMsg[:min(300, len(systemMsg))])
	}
}

// TestE2EKeyFactsInPrompt verifies that key facts seeded in the database
// appear in the Tier 1 system message as "Key facts: ...".
func TestE2EKeyFactsInPrompt(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Sure, I'll keep that in mind."),
		},
	})

	ctx := context.Background()

	// Seed a key fact directly in the key_facts table.
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO key_facts (fact, category, extracted_at)
		 VALUES ('User prefers dark mode', 'preference', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed key fact: %v", err)
	}

	p.sendMsg(t, "What do you know about me?")

	// Verify the system message contains the key fact.
	calls := p.Fake.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least one model call")
	}

	systemMsg := calls[0][0].Content
	if calls[0][0].Role != "system" {
		t.Fatalf("first message role = %q, want 'system'", calls[0][0].Role)
	}
	if !strings.Contains(systemMsg, "Key facts:") {
		t.Errorf("system message should contain 'Key facts:' header, got: %s",
			systemMsg[:min(300, len(systemMsg))])
	}
	if !strings.Contains(systemMsg, "dark mode") {
		t.Errorf("system message should contain fact about dark mode, got: %s",
			systemMsg[:min(300, len(systemMsg))])
	}
}

// TestE2EMemoryAwarenessInPrompt verifies that when a captured thought exists
// in the database, the Tier 1 system message includes a "Living Memory: N
// thoughts captured." awareness pointer.
func TestE2EMemoryAwarenessInPrompt(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("I have some memories stored."),
		},
	})

	ctx := context.Background()

	// Wire the memory store into the composer for the awareness pointer.
	memStore := memory.NewStore(p.DB)
	p.Composer.SetMemoryStore(memStore)

	// Seed a thought in the captured_thoughts table.
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO captured_thoughts (content, source, tags, created_at)
		 VALUES ('Test thought about preferences', 'webchat', '[]', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed thought: %v", err)
	}

	p.sendMsg(t, "Do you remember anything?")

	// Verify the system message contains the Living Memory awareness pointer.
	calls := p.Fake.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least one model call")
	}

	systemMsg := calls[0][0].Content
	if calls[0][0].Role != "system" {
		t.Fatalf("first message role = %q, want 'system'", calls[0][0].Role)
	}
	if !strings.Contains(systemMsg, "Living Memory") {
		t.Errorf("system message should contain 'Living Memory' pointer, got: %s",
			systemMsg[:min(300, len(systemMsg))])
	}
	if !strings.Contains(systemMsg, "1") {
		t.Errorf("system message should contain thought count '1', got: %s",
			systemMsg[:min(300, len(systemMsg))])
	}
}

// TestE2EPromptHistoryGrows verifies that each subsequent model call receives
// more messages than the previous one because conversation history (Tier 3)
// accumulates across turns.
func TestE2EPromptHistoryGrows(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Reply 1."),
			finalResponse("Reply 2."),
			finalResponse("Reply 3."),
		},
	})

	p.sendMsg(t, "Message 1")
	p.sendMsg(t, "Message 2")
	p.sendMsg(t, "Message 3")

	calls := p.Fake.Calls()
	if len(calls) < 3 {
		t.Fatalf("expected at least 3 model calls, got %d", len(calls))
	}

	// Each call should have more messages than the previous, because history grows.
	if len(calls[1]) <= len(calls[0]) {
		t.Errorf("call 2 should have more messages than call 1: call1=%d, call2=%d",
			len(calls[0]), len(calls[1]))
	}
	if len(calls[2]) <= len(calls[1]) {
		t.Errorf("call 3 should have more messages than call 2: call2=%d, call3=%d",
			len(calls[1]), len(calls[2]))
	}
}

// TestE2EPersonaInPrompt verifies that when a custom persona name is set in
// config, the Tier 1 system message includes that persona name.
func TestE2EPersonaInPrompt(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		ConfigOverride: func(cfg *config.Config) {
			cfg.Settings.PersonaName = "Jarvis"
		},
		Responses: []agent.Response{
			finalResponse("Hello! I am Jarvis, at your service."),
		},
	})

	p.sendMsg(t, "Hello")

	// Verify system message contains the custom persona name.
	calls := p.Fake.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least one model call")
	}
	if len(calls[0]) == 0 {
		t.Fatal("expected at least one message in first model call")
	}

	systemMsg := calls[0][0].Content
	if calls[0][0].Role != "system" {
		t.Fatalf("first message role = %q, want 'system'", calls[0][0].Role)
	}
	if !strings.Contains(systemMsg, "Jarvis") {
		t.Errorf("system message should contain persona name 'Jarvis', got: %s",
			systemMsg[:min(200, len(systemMsg))])
	}
}
