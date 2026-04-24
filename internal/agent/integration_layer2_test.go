package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestAutonomyBlocksToolExecution(t *testing.T) {
	// L2 with ask list: "shell.exec" requires confirmation.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "1", Name: "shell.exec", Input: `{"cmd":"ls"}`}},
		},
		agent.Response{Content: "blocked"},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{"shell.exec": "file1.txt"})

	a := agent.New(agent.Config{
		ID: "autonomy-1", Task: "List files", Type: agent.TypePrimary,
		Model: model, Tools: tools, Mode: agent.ModeSingle,
		Autonomy: agent.AutonomyConfig{
			Level:      agent.AutonomyL2,
			AskActions: []string{"shell.exec"},
		},
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s, want completed", result.Status)
	}
	// Tool should NOT have been executed (blocked by autonomy).
	if len(tools.ToolCalls()) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(tools.ToolCalls()))
	}
}

func TestAutonomyAllowsApprovedTools(t *testing.T) {
	// L2 with auto list: "web.search" is auto-approved.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "1", Name: "web.search", Input: `{"q":"test"}`}},
		},
		agent.Response{Content: "search done"},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{"web.search": "results"})

	a := agent.New(agent.Config{
		ID: "autonomy-2", Task: "Search", Type: agent.TypePrimary,
		Model: model, Tools: tools, Mode: agent.ModeSingle,
		Autonomy: agent.AutonomyConfig{
			Level:       agent.AutonomyL2,
			AutoActions: []string{"web.search"},
		},
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s, want completed", result.Status)
	}
	// Tool SHOULD have been executed.
	if len(tools.ToolCalls()) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(tools.ToolCalls()))
	}
}

func TestAutonomyL1AutoApprovesAll(t *testing.T) {
	// L1 (default) should auto-approve all tools.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "1", Name: "shell.exec", Input: `{"cmd":"rm -rf /"}`}},
		},
		agent.Response{Content: "done"},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{"shell.exec": "ok"})

	a := agent.New(agent.Config{
		ID: "autonomy-3", Task: "Dangerous", Type: agent.TypePrimary,
		Model: model, Tools: tools, Mode: agent.ModeSingle,
		Autonomy: agent.AutonomyConfig{Level: agent.AutonomyL1},
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}
	if len(tools.ToolCalls()) != 1 {
		t.Errorf("L1 should auto-approve: got %d calls, want 1", len(tools.ToolCalls()))
	}
}

func TestAutonomyBlocksInParallel(t *testing.T) {
	// L2 blocks "shell.exec" but allows "web.search", executed in parallel via ModeMulti.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "1", Name: "shell.exec", Input: `{"cmd":"ls"}`},
				{ID: "2", Name: "web.search", Input: `{"q":"test"}`},
			},
		},
		agent.Response{Content: "done"},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{
		"shell.exec": "files",
		"web.search": "results",
	})

	a := agent.New(agent.Config{
		ID: "autonomy-parallel", Task: "Mixed", Type: agent.TypePrimary,
		Model: model, Tools: tools, Mode: agent.ModeMulti,
		Autonomy: agent.AutonomyConfig{
			Level:      agent.AutonomyL2,
			AskActions: []string{"shell.exec"},
		},
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}
	// Only web.search should have been executed.
	calls := tools.ToolCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 tool call (web.search only), got %d", len(calls))
	}
	if len(calls) > 0 && calls[0].Name != "web.search" {
		t.Errorf("expected web.search, got %s", calls[0].Name)
	}
}

func TestModeCustomParallelExecution(t *testing.T) {
	// ModeCustom should execute multiple tool calls in parallel.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "1", Name: "tool_a", Input: "{}"},
				{ID: "2", Name: "tool_b", Input: "{}"},
			},
		},
		agent.Response{Content: "custom done"},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{
		"tool_a": "result_a",
		"tool_b": "result_b",
	})

	a := agent.New(agent.Config{
		ID: "custom-parallel", Task: "Custom parallel", Type: agent.TypePrimary,
		Model: model, Tools: tools, Mode: agent.ModeCustom,
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}
	// Both tools should have been executed.
	calls := tools.ToolCalls()
	if len(calls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(calls))
	}
}

func TestAutonomyBlockMessageContainsToolName(t *testing.T) {
	// Verify the block message includes the tool name for debugging.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "1", Name: "dangerous.tool", Input: "{}"}},
		},
		agent.Response{Content: "noted"},
	)
	tools := testutil.NewFakeToolExecutor(nil)

	a := agent.New(agent.Config{
		ID: "autonomy-msg", Task: "Test", Type: agent.TypePrimary,
		Model: model, Tools: tools, Mode: agent.ModeSingle,
		Autonomy: agent.AutonomyConfig{
			Level:      agent.AutonomyL2,
			AskActions: []string{"dangerous.tool"},
		},
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// The model should have received the blocked message containing the tool name.
	calls := model.Calls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 model calls, got %d", len(calls))
	}
	// Second call should contain the blocked tool result.
	lastCall := calls[1]
	found := false
	for _, msg := range lastCall {
		if msg.Role == "tool" && strings.Contains(msg.Content, "dangerous.tool") {
			found = true
			break
		}
	}
	if !found {
		t.Error("blocked tool message should contain tool name")
	}
}
