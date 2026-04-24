package shell

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterShellTools registers all shell tools in the tool registry.
func RegisterShellTools(registry *tool.Registry, executor *Executor) {
	registry.Register(&execTool{executor: executor})
}

type execTool struct {
	executor *Executor
}

func (t *execTool) Name() string        { return "shell.exec" }
func (t *execTool) Description() string { return "Execute a shell command in a sandboxed POSIX emulator" }
func (t *execTool) Capability() string  { return "tool.shell.exec" }
func (t *execTool) Schema() string {
	return `{"type":"object","properties":{"command":{"type":"string","description":"The command to execute"}},"required":["command"]}`
}

func (t *execTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("shell.exec: invalid input: %w", err)
	}
	if args.Command == "" {
		return "", fmt.Errorf("shell.exec: command is required")
	}

	result, err := t.executor.Exec(ctx, args.Command)
	if err != nil {
		return "", err
	}

	output := result.Stdout
	if result.Stderr != "" {
		output += "\n--- stderr ---\n" + result.Stderr
	}
	if result.ExitCode != 0 {
		output += fmt.Sprintf("\n(exit code: %d)", result.ExitCode)
	}
	return output, nil
}
