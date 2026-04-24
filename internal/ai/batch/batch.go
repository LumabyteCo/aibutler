// Package batch provides batch AI tool execution (run one tool with many prompts).
package batch

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolRunner executes a named tool with the given input.
type ToolRunner interface {
	CallTool(ctx context.Context, name, input string) (string, error)
}

// BatchGenerate runs the same tool N times with different prompts and returns all results.
func BatchGenerate(ctx context.Context, runner ToolRunner, tool string, prompts []string) ([]string, error) {
	if len(prompts) == 0 {
		return []string{}, nil
	}

	results := make([]string, 0, len(prompts))
	for i, prompt := range prompts {
		output, err := runner.CallTool(ctx, tool, prompt)
		if err != nil {
			return results, fmt.Errorf("batch item %d: %w", i, err)
		}
		results = append(results, output)
	}
	return results, nil
}

// toolRegistry is the narrow interface used by registration functions.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterBatchTools registers the batch generation tool.
func RegisterBatchTools(registry toolRegistry, runner ToolRunner) {
	registry.Register(
		"ai.batch.generate",
		"Run an AI tool multiple times with different prompts and collect all results.",
		`{"type":"object","properties":{"tool":{"type":"string","description":"Tool name to run"},"prompts":{"type":"array","items":{"type":"string"},"description":"List of prompts to run"}},"required":["tool","prompts"]}`,
		"tool.ai.batch",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Tool    string   `json:"tool"`
				Prompts []string `json:"prompts"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			results, err := BatchGenerate(ctx, runner, args.Tool, args.Prompts)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"tool":    args.Tool,
				"count":   len(results),
				"results": results,
			})
			return string(out), nil
		},
	)
}
