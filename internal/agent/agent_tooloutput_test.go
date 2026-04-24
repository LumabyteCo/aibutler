package agent_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestToolOutputsPopulatedSequential(t *testing.T) {
	fake := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "c1", Name: "tool.alpha", Input: `{"x":1}`},
				{ID: "c2", Name: "tool.beta", Input: `{"y":2}`},
			},
		},
		agent.Response{Content: "done"},
	)

	executor := testutil.NewFakeToolExecutor(map[string]string{
		"tool.alpha": "result_alpha",
		"tool.beta":  "result_beta",
	})

	a := agent.New(agent.Config{
		ID:    "test-1",
		Task:  "do it",
		Model: fake,
		Tools: executor,
		Mode:  agent.ModeSingle, // sequential execution
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.ToolOutputs) != 2 {
		t.Fatalf("ToolOutputs = %d, want 2", len(result.ToolOutputs))
	}
	if result.ToolOutputs[0].ToolName != "tool.alpha" {
		t.Errorf("[0].ToolName = %q", result.ToolOutputs[0].ToolName)
	}
	if result.ToolOutputs[0].Output != "result_alpha" {
		t.Errorf("[0].Output = %q", result.ToolOutputs[0].Output)
	}
	if result.ToolOutputs[1].ToolName != "tool.beta" {
		t.Errorf("[1].ToolName = %q", result.ToolOutputs[1].ToolName)
	}
	if result.ToolOutputs[1].Output != "result_beta" {
		t.Errorf("[1].Output = %q", result.ToolOutputs[1].Output)
	}
}

func TestToolOutputsPopulatedParallel(t *testing.T) {
	fake := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "c1", Name: "tool.alpha", Input: `{}`},
				{ID: "c2", Name: "tool.beta", Input: `{}`},
			},
		},
		agent.Response{Content: "done"},
	)

	executor := testutil.NewFakeToolExecutor(map[string]string{
		"tool.alpha": "alpha_out",
		"tool.beta":  "beta_out",
	})

	a := agent.New(agent.Config{
		ID:    "test-2",
		Task:  "parallel test",
		Model: fake,
		Tools: executor,
		Mode:  agent.ModeMulti, // parallel execution for multiple calls
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.ToolOutputs) != 2 {
		t.Fatalf("ToolOutputs = %d, want 2", len(result.ToolOutputs))
	}
	// Results should be in order (not random).
	if result.ToolOutputs[0].ToolName != "tool.alpha" {
		t.Errorf("[0].ToolName = %q, want tool.alpha", result.ToolOutputs[0].ToolName)
	}
	if result.ToolOutputs[1].ToolName != "tool.beta" {
		t.Errorf("[1].ToolName = %q, want tool.beta", result.ToolOutputs[1].ToolName)
	}
}

func TestToolOutputsEmptyWhenNoToolCalls(t *testing.T) {
	fake := testutil.NewFakeModel(
		agent.Response{Content: "no tools needed"},
	)

	a := agent.New(agent.Config{
		ID:    "test-3",
		Task:  "simple question",
		Model: fake,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(result.ToolOutputs) != 0 {
		t.Errorf("ToolOutputs = %d, want 0", len(result.ToolOutputs))
	}
}

func TestToolOutputTruncation(t *testing.T) {
	// Create a tool output larger than the 10KB limit.
	var r agent.Result
	bigOutput := make([]byte, 20000)
	for i := range bigOutput {
		bigOutput[i] = 'A'
	}
	r.ToolOutputs = nil // ensure clean
	// Use the exported struct to test the append behavior indirectly.
	// Since appendToolOutput is unexported, we test via the agent.
	fake := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "c1", Name: "big.tool", Input: `{}`},
			},
		},
		agent.Response{Content: "done"},
	)
	executor := testutil.NewFakeToolExecutor(map[string]string{
		"big.tool": string(bigOutput),
	})
	a := agent.New(agent.Config{
		ID:    "trunc-test",
		Task:  "test",
		Model: fake,
		Tools: executor,
		Mode:  agent.ModeSingle,
	})
	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.ToolOutputs) != 1 {
		t.Fatalf("ToolOutputs = %d, want 1", len(result.ToolOutputs))
	}
	// Output should be capped at ~10KB + truncation marker.
	if len(result.ToolOutputs[0].Output) > 11000 {
		t.Errorf("output length = %d, should be capped around 10KB", len(result.ToolOutputs[0].Output))
	}
	if !stringContains(result.ToolOutputs[0].Output, "[truncated]") {
		t.Error("truncated output should contain [truncated] marker")
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
