package workflow_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/ai/workflow"
)

// mockRunner implements workflow.ToolRunner for testing.
type mockRunner struct {
	results map[string]string
	err     error
}

func (m *mockRunner) CallTool(ctx context.Context, name, input string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if result, ok := m.results[name]; ok {
		return result, nil
	}
	return fmt.Sprintf("output from %s", name), nil
}

func TestExecuteSequentialWorkflow(t *testing.T) {
	runner := &mockRunner{
		results: map[string]string{
			"tool.a": "result-a",
			"tool.b": "result-b",
		},
	}

	wf := workflow.Workflow{
		Name: "test-workflow",
		Steps: []workflow.Step{
			{Tool: "tool.a", Input: `{"prompt":"hello"}`},
			{Tool: "tool.b", Input: `{"prompt":"{prev_output}"}`},
		},
	}

	result, err := workflow.ExecuteWorkflow(context.Background(), runner, wf)
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if result != "result-b" {
		t.Errorf("result = %q, want result-b", result)
	}
}

func TestWorkflowStepFailure(t *testing.T) {
	runner := &mockRunner{
		err: fmt.Errorf("provider unavailable"),
	}

	wf := workflow.Workflow{
		Name: "failing-workflow",
		Steps: []workflow.Step{
			{Tool: "tool.a", Input: `{"prompt":"hello"}`},
		},
	}

	_, err := workflow.ExecuteWorkflow(context.Background(), runner, wf)
	if err == nil {
		t.Fatal("expected error from failing workflow step")
	}
}
