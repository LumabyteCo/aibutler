//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2EInstructionSave verifies that instruction.save persists a learned
// instruction into the learned_instructions table.
func TestE2EInstructionSave(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithInstruction: true,
		Responses: []agent.Response{
			// Turn 1: model calls instruction.save
			toolCallResponse("Saving instruction.",
				tc("tc1", "instruction.save", `{"content":"Always use metric units","category":"preference"}`),
			),
			// Turn 1 continued: final reply
			finalResponse("Done! I'll always use metric units from now on."),
		},
	})

	p.sendMsg(t, "Always use metric units")

	// Verify final response.
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "metric") {
		t.Errorf("response = %q, want mention of metric", resp)
	}

	// Verify instruction was persisted.
	count := p.countRows(t, "learned_instructions")
	if count != 1 {
		t.Fatalf("learned_instructions rows = %d, want 1", count)
	}

	content := p.querySingleString(t, "SELECT content FROM learned_instructions LIMIT 1")
	if content != "Always use metric units" {
		t.Errorf("instruction content = %q, want 'Always use metric units'", content)
	}

	category := p.querySingleString(t, "SELECT category FROM learned_instructions LIMIT 1")
	if category != "preference" {
		t.Errorf("instruction category = %q, want 'preference'", category)
	}
}

// TestE2EInstructionList saves 2 instructions, then lists them.
func TestE2EInstructionList(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithInstruction: true,
		Responses: []agent.Response{
			// Turn 1: save first instruction
			toolCallResponse("Saving instruction 1.",
				tc("tc1", "instruction.save", `{"content":"Always use metric units","category":"preference"}`),
			),
			finalResponse("Noted: metric units."),

			// Turn 2: save second instruction
			toolCallResponse("Saving instruction 2.",
				tc("tc2", "instruction.save", `{"content":"Respond in formal English","category":"style"}`),
			),
			finalResponse("Noted: formal English."),

			// Turn 3: list instructions
			toolCallResponse("Listing instructions.",
				tc("tc3", "instruction.list", `{}`),
			),
			finalResponse("You have 2 active instructions: use metric units and respond in formal English."),
		},
	})

	// Turn 1: save first instruction.
	p.sendMsg(t, "Always use metric units")
	if p.countRows(t, "learned_instructions") != 1 {
		t.Fatal("expected 1 instruction after first save")
	}

	// Turn 2: save second instruction.
	p.sendMsg(t, "Respond in formal English")
	if p.countRows(t, "learned_instructions") != 2 {
		t.Fatal("expected 2 instructions after second save")
	}

	// Turn 3: list.
	p.sendMsg(t, "What instructions do you have?")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "2 active instructions") {
		t.Errorf("list response = %q, want mention of 2 active instructions", resp)
	}

	if p.responseCount() != 3 {
		t.Errorf("response count = %d, want 3", p.responseCount())
	}
}

// TestE2EInstructionUpdate saves an instruction, then updates its content.
func TestE2EInstructionUpdate(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithInstruction: true,
		Responses: []agent.Response{
			// Turn 1: save instruction
			toolCallResponse("Saving instruction.",
				tc("tc1", "instruction.save", `{"content":"Always use metric units","category":"preference"}`),
			),
			finalResponse("Instruction saved."),

			// Turn 2: update instruction (ID=1 since it's the first row)
			toolCallResponse("Updating instruction.",
				tc("tc2", "instruction.update", `{"id":1,"content":"Always use imperial units"}`),
			),
			finalResponse("Updated to imperial units."),
		},
	})

	// Turn 1: save.
	p.sendMsg(t, "Always use metric units")
	if p.countRows(t, "learned_instructions") != 1 {
		t.Fatal("expected 1 instruction after save")
	}

	// Verify original content.
	original := p.querySingleString(t, "SELECT content FROM learned_instructions WHERE id = 1")
	if original != "Always use metric units" {
		t.Fatalf("original content = %q", original)
	}

	// Turn 2: update.
	p.sendMsg(t, "Actually, use imperial units instead")

	// Verify updated content.
	updated := p.querySingleString(t, "SELECT content FROM learned_instructions WHERE id = 1")
	if updated != "Always use imperial units" {
		t.Errorf("updated content = %q, want 'Always use imperial units'", updated)
	}

	if p.Fake.CallCount() != 4 {
		t.Errorf("model calls = %d, want 4", p.Fake.CallCount())
	}
}

// TestE2EInstructionRemove saves an instruction, then removes it.
func TestE2EInstructionRemove(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithInstruction: true,
		Responses: []agent.Response{
			// Turn 1: save instruction
			toolCallResponse("Saving instruction.",
				tc("tc1", "instruction.save", `{"content":"Always use metric units","category":"preference"}`),
			),
			finalResponse("Instruction saved."),

			// Turn 2: remove instruction (ID=1)
			toolCallResponse("Removing instruction.",
				tc("tc2", "instruction.remove", `{"id":1}`),
			),
			finalResponse("Instruction removed."),
		},
	})

	// Turn 1: save.
	p.sendMsg(t, "Always use metric units")
	if p.countRows(t, "learned_instructions") != 1 {
		t.Fatal("expected 1 instruction after save")
	}

	// Turn 2: remove.
	p.sendMsg(t, "Forget that instruction")

	// Verify row is deleted.
	count := p.countRows(t, "learned_instructions")
	if count != 0 {
		t.Errorf("learned_instructions rows = %d, want 0 after removal", count)
	}

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "removed") {
		t.Errorf("response = %q, want mention of removed", resp)
	}
}
