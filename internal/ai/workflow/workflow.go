// Package workflow provides sequential AI tool workflow execution.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolRunner executes a named tool with the given input.
type ToolRunner interface {
	CallTool(ctx context.Context, name, input string) (string, error)
}

// Step represents a single step in a workflow.
type Step struct {
	Tool  string `json:"tool"`  // tool name to call
	Input string `json:"input"` // input template with {prev_output} placeholder
}

// Workflow represents a named sequence of tool steps.
type Workflow struct {
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

// ExecuteWorkflow runs a workflow's steps sequentially, passing each output to the next step.
func ExecuteWorkflow(ctx context.Context, runner ToolRunner, wf Workflow) (string, error) {
	if len(wf.Steps) == 0 {
		return "", fmt.Errorf("workflow %q has no steps", wf.Name)
	}

	var prevOutput string
	for i, step := range wf.Steps {
		input := step.Input
		if prevOutput != "" {
			input = strings.ReplaceAll(input, "{prev_output}", prevOutput)
		}

		output, err := runner.CallTool(ctx, step.Tool, input)
		if err != nil {
			return "", fmt.Errorf("workflow %q step %d (%s): %w", wf.Name, i+1, step.Tool, err)
		}
		prevOutput = output
	}

	return prevOutput, nil
}

// toolRegistry is the narrow interface used by registration functions.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterWorkflowTools registers the workflow execution tool.
func RegisterWorkflowTools(registry toolRegistry, runner ToolRunner) {
	registry.Register(
		"ai.workflow.run",
		"Execute a multi-step AI workflow, passing output from each step to the next.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Workflow name"},"steps":{"type":"array","items":{"type":"object","properties":{"tool":{"type":"string"},"input":{"type":"string"}},"required":["tool","input"]}}},"required":["name","steps"]}`,
		"tool.ai.workflow",
		func(ctx context.Context, input string) (string, error) {
			var wf Workflow
			if err := json.Unmarshal([]byte(input), &wf); err != nil {
				return "", fmt.Errorf("invalid workflow input: %w", err)
			}
			return ExecuteWorkflow(ctx, runner, wf)
		},
	)
}
