package prompt

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

func TestShouldCompactFalse(t *testing.T) {
	c := NewCompactor(CompactorConfig{MaxEstimatedTokens: 80000})
	msgs := []agent.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	if c.ShouldCompact(msgs) {
		t.Error("ShouldCompact = true, want false for small conversation")
	}
}

func TestShouldCompactTrue(t *testing.T) {
	c := NewCompactor(CompactorConfig{MaxEstimatedTokens: 10})
	// Create messages that exceed 10 tokens.
	msgs := []agent.Message{
		{Role: "user", Content: "This is a fairly long message that should exceed ten tokens easily when estimated."},
		{Role: "assistant", Content: "And this is a response that also has some reasonable length to it for testing."},
	}
	if !c.ShouldCompact(msgs) {
		t.Error("ShouldCompact = false, want true for large conversation")
	}
}

func TestCompactBasic(t *testing.T) {
	c := NewCompactor(CompactorConfig{
		MaxEstimatedTokens:  10,
		PreserveRecentCount: 2,
	})

	msgs := []agent.Message{
		{Role: "user", Content: "First message"},
		{Role: "assistant", Content: "First reply"},
		{Role: "user", Content: "Second message"},
		{Role: "assistant", Content: "Second reply"},
		{Role: "user", Content: "Third message"},
		{Role: "assistant", Content: "Third reply"},
	}

	result, meta, err := c.Compact(msgs)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}

	// Should have 1 summary + 2 preserved = 3 messages.
	if len(result) != 3 {
		t.Fatalf("compacted len = %d, want 3", len(result))
	}

	// Summary should start with the continuation preamble.
	if !strings.HasPrefix(result[0].Content, CompactContinuationPreamble) {
		t.Error("summary should start with continuation preamble")
	}

	// Last two messages should be preserved.
	if result[1].Content != "Third message" {
		t.Errorf("preserved[0] = %q, want 'Third message'", result[1].Content)
	}
	if result[2].Content != "Third reply" {
		t.Errorf("preserved[1] = %q, want 'Third reply'", result[2].Content)
	}

	// Metadata.
	if meta.OriginalCount != 6 {
		t.Errorf("OriginalCount = %d, want 6", meta.OriginalCount)
	}
	if meta.CompactedCount != 3 {
		t.Errorf("CompactedCount = %d, want 3", meta.CompactedCount)
	}
	if meta.RemovedCount != 4 {
		t.Errorf("RemovedCount = %d, want 4", meta.RemovedCount)
	}
	if meta.PreservedCount != 2 {
		t.Errorf("PreservedCount = %d, want 2", meta.PreservedCount)
	}

	// Should contain resume instruction.
	if !strings.Contains(result[0].Content, CompactResumeInstruction) {
		t.Error("summary should contain resume instruction")
	}
}

func TestCompactPreservesRecent(t *testing.T) {
	c := NewCompactor(CompactorConfig{
		MaxEstimatedTokens:  10,
		PreserveRecentCount: 4,
	})

	msgs := []agent.Message{
		{Role: "user", Content: "Old message 1"},
		{Role: "assistant", Content: "Old reply 1"},
		{Role: "user", Content: "Recent 1"},
		{Role: "assistant", Content: "Recent reply 1"},
		{Role: "user", Content: "Recent 2"},
		{Role: "assistant", Content: "Recent reply 2"},
	}

	result, meta, err := c.Compact(msgs)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}

	// 1 summary + 4 preserved = 5.
	if len(result) != 5 {
		t.Fatalf("compacted len = %d, want 5", len(result))
	}
	if meta.PreservedCount != 4 {
		t.Errorf("PreservedCount = %d, want 4", meta.PreservedCount)
	}

	// Preserved messages should be the last 4.
	if result[1].Content != "Recent 1" {
		t.Errorf("preserved[0] = %q, want 'Recent 1'", result[1].Content)
	}
}

func TestCompactExtractsTools(t *testing.T) {
	c := NewCompactor(CompactorConfig{
		MaxEstimatedTokens:  10,
		PreserveRecentCount: 1,
	})

	msgs := []agent.Message{
		{Role: "user", Content: "Add a task"},
		{Role: "assistant", Content: "Adding...", ToolCalls: []agent.ToolCall{
			{ID: "c1", Name: "task.add", Input: `{"content":"test"}`},
		}},
		{Role: "tool", Content: "done", ToolID: "c1"},
		{Role: "user", Content: "Log expense"},
		{Role: "assistant", Content: "Logging...", ToolCalls: []agent.ToolCall{
			{ID: "c2", Name: "expense.log", Input: `{"amount":50}`},
		}},
		{Role: "tool", Content: "done", ToolID: "c2"},
		{Role: "user", Content: "What next?"},
	}

	result, meta, err := c.Compact(msgs)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}

	// Should extract tool names.
	if len(meta.ToolsUsed) != 2 {
		t.Fatalf("ToolsUsed = %v, want 2 tools", meta.ToolsUsed)
	}
	// Should be sorted.
	if meta.ToolsUsed[0] != "expense.log" || meta.ToolsUsed[1] != "task.add" {
		t.Errorf("ToolsUsed = %v, want [expense.log, task.add]", meta.ToolsUsed)
	}

	// Summary should mention tools.
	if !strings.Contains(result[0].Content, "expense.log") {
		t.Error("summary should mention extracted tools")
	}
}

func TestCompactExtractsFiles(t *testing.T) {
	c := NewCompactor(CompactorConfig{
		MaxEstimatedTokens:  10,
		PreserveRecentCount: 1,
		FileExtensions:      []string{"go", "ts", "md"},
	})

	msgs := []agent.Message{
		{Role: "user", Content: "Edit internal/agent/agent.go and internal/model/claude.go"},
		{Role: "assistant", Content: "Done editing those files."},
		{Role: "user", Content: "Now check README.md please"},
		{Role: "assistant", Content: "README.md looks good."},
		{Role: "user", Content: "Thanks, what next?"},
	}

	_, meta, err := c.Compact(msgs)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}

	if len(meta.KeyFiles) == 0 {
		t.Fatal("expected key files to be extracted")
	}

	// Should find .go and .md files.
	foundGo := false
	foundMd := false
	for _, f := range meta.KeyFiles {
		if strings.HasSuffix(f, ".go") {
			foundGo = true
		}
		if strings.HasSuffix(f, ".md") {
			foundMd = true
		}
	}
	if !foundGo {
		t.Error("expected .go file in KeyFiles")
	}
	if !foundMd {
		t.Error("expected .md file in KeyFiles")
	}
}

func TestCompactIterativeMerge(t *testing.T) {
	c := NewCompactor(CompactorConfig{
		MaxEstimatedTokens:  10,
		PreserveRecentCount: 1,
	})

	// Simulate a prior compaction.
	priorSummary := CompactContinuationPreamble + "\n\nMessage counts: user=5, assistant=5, tool=3\nTools used: task.add"
	msgs := []agent.Message{
		{Role: "user", Content: priorSummary},
		{Role: "assistant", Content: "Continuing from before."},
		{Role: "user", Content: "New work here."},
		{Role: "assistant", Content: "Done with new work."},
		{Role: "user", Content: "Latest message."},
	}

	result, _, err := c.Compact(msgs)
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}

	summary := result[0].Content
	if !strings.Contains(summary, "Previously compacted:") {
		t.Error("iterative merge should include 'Previously compacted:' section")
	}
	if !strings.Contains(summary, "Newly compacted:") {
		t.Error("iterative merge should include 'Newly compacted:' section")
	}
}

func TestEstimateTokens(t *testing.T) {
	c := NewCompactor(DefaultCompactorConfig())

	// Empty.
	if got := c.EstimateTokens(nil); got != 0 {
		t.Errorf("EstimateTokens(nil) = %d, want 0", got)
	}

	// Small conversation.
	msgs := []agent.Message{
		{Role: "user", Content: "Hello world"},
		{Role: "assistant", Content: "Hi there, how can I help?"},
	}
	est := c.EstimateTokens(msgs)
	if est <= 0 {
		t.Errorf("EstimateTokens = %d, want > 0", est)
	}
	// Should be reasonable (not wildly off).
	if est > 100 {
		t.Errorf("EstimateTokens = %d, seems too high for simple conversation", est)
	}
}
