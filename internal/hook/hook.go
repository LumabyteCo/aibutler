package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Event names for hook lifecycle.
type Event string

const (
	PreToolUse     Event = "PreToolUse"
	PostToolUse    Event = "PostToolUse"
	OnAgentSpawn   Event = "OnAgentSpawn"
	OnAgentComplete Event = "OnAgentComplete"
	OnAgentFail    Event = "OnAgentFail"
)

// HookConfig describes a single hook entry.
type HookConfig struct {
	Command string   `yaml:"command"` // shell command to execute
	Tools   []string `yaml:"tools"`   // tool name patterns (glob-like: "bash", "shell.*", "*")
}

// Payload is the JSON sent to hook commands on stdin.
type Payload struct {
	HookEventName    string      `json:"hook_event_name"`
	ToolName         string      `json:"tool_name"`
	ToolInput        interface{} `json:"tool_input"`
	ToolInputJSON    string      `json:"tool_input_json"`
	ToolOutput       *string     `json:"tool_output"`
	ToolResultIsError bool       `json:"tool_result_is_error"`
}

// RunResult holds the aggregated outcome of running hooks.
type RunResult struct {
	Denied   bool
	Messages []string
}

// Engine manages pre and post tool-use hooks.
type Engine struct {
	preHooks  []HookConfig
	postHooks []HookConfig
	agentHooks map[Event][]HookConfig
	timeout   time.Duration
}

// New creates a hook engine with the given hook configurations.
func New(preHooks, postHooks []HookConfig) *Engine {
	return &Engine{
		preHooks:   preHooks,
		postHooks:  postHooks,
		agentHooks: make(map[Event][]HookConfig),
		timeout:    10 * time.Second,
	}
}

// SetAgentHooks configures lifecycle hooks for agent events.
func (e *Engine) SetAgentHooks(event Event, hooks []HookConfig) {
	e.agentHooks[event] = hooks
}

// RunPreToolUse runs all matching pre-tool hooks. If any exits 2, the tool is denied.
func (e *Engine) RunPreToolUse(ctx context.Context, toolName, toolInput string) (*RunResult, error) {
	payload := Payload{
		HookEventName:    string(PreToolUse),
		ToolName:         toolName,
		ToolInputJSON:    toolInput,
		ToolResultIsError: false,
	}
	// Try to parse tool input as JSON for the structured field.
	var parsed interface{}
	if err := json.Unmarshal([]byte(toolInput), &parsed); err == nil {
		payload.ToolInput = parsed
	} else {
		payload.ToolInput = toolInput
	}

	return e.runHooks(ctx, e.preHooks, toolName, payload, map[string]string{
		"HOOK_EVENT":      string(PreToolUse),
		"HOOK_TOOL_NAME":  toolName,
		"HOOK_TOOL_INPUT": toolInput,
	})
}

// RunPostToolUse runs all matching post-tool hooks. Feedback is merged into RunResult.Messages.
func (e *Engine) RunPostToolUse(ctx context.Context, toolName, toolInput, toolOutput string, isError bool) (*RunResult, error) {
	payload := Payload{
		HookEventName:    string(PostToolUse),
		ToolName:         toolName,
		ToolInputJSON:    toolInput,
		ToolOutput:       &toolOutput,
		ToolResultIsError: isError,
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(toolInput), &parsed); err == nil {
		payload.ToolInput = parsed
	} else {
		payload.ToolInput = toolInput
	}

	isErrStr := "false"
	if isError {
		isErrStr = "true"
	}
	return e.runHooks(ctx, e.postHooks, toolName, payload, map[string]string{
		"HOOK_EVENT":       string(PostToolUse),
		"HOOK_TOOL_NAME":   toolName,
		"HOOK_TOOL_INPUT":  toolInput,
		"HOOK_TOOL_OUTPUT": toolOutput,
		"HOOK_TOOL_IS_ERROR": isErrStr,
	})
}

// OnAgentSpawnHook runs OnAgentSpawn lifecycle hooks.
func (e *Engine) OnAgentSpawnHook(ctx context.Context, agentName string) (*RunResult, error) {
	hooks := e.agentHooks[OnAgentSpawn]
	if len(hooks) == 0 {
		return &RunResult{}, nil
	}
	payload := Payload{
		HookEventName: string(OnAgentSpawn),
		ToolName:      agentName,
	}
	return e.runHooks(ctx, hooks, agentName, payload, map[string]string{
		"HOOK_EVENT":     string(OnAgentSpawn),
		"HOOK_TOOL_NAME": agentName,
	})
}

// OnAgentCompleteHook runs OnAgentComplete lifecycle hooks.
func (e *Engine) OnAgentCompleteHook(ctx context.Context, agentName, output string) (*RunResult, error) {
	hooks := e.agentHooks[OnAgentComplete]
	if len(hooks) == 0 {
		return &RunResult{}, nil
	}
	payload := Payload{
		HookEventName: string(OnAgentComplete),
		ToolName:      agentName,
		ToolOutput:    &output,
	}
	return e.runHooks(ctx, hooks, agentName, payload, map[string]string{
		"HOOK_EVENT":       string(OnAgentComplete),
		"HOOK_TOOL_NAME":   agentName,
		"HOOK_TOOL_OUTPUT": output,
	})
}

// OnAgentFailHook runs OnAgentFail lifecycle hooks.
func (e *Engine) OnAgentFailHook(ctx context.Context, agentName, errMsg string) (*RunResult, error) {
	hooks := e.agentHooks[OnAgentFail]
	if len(hooks) == 0 {
		return &RunResult{}, nil
	}
	payload := Payload{
		HookEventName:    string(OnAgentFail),
		ToolName:         agentName,
		ToolOutput:       &errMsg,
		ToolResultIsError: true,
	}
	return e.runHooks(ctx, hooks, agentName, payload, map[string]string{
		"HOOK_EVENT":         string(OnAgentFail),
		"HOOK_TOOL_NAME":     agentName,
		"HOOK_TOOL_OUTPUT":   errMsg,
		"HOOK_TOOL_IS_ERROR": "true",
	})
}

// runHooks executes a list of hooks that match the tool name pattern.
func (e *Engine) runHooks(ctx context.Context, hooks []HookConfig, toolName string, payload Payload, env map[string]string) (*RunResult, error) {
	result := &RunResult{}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hook: marshal payload: %w", err)
	}

	for _, h := range hooks {
		if !matchesToolFilter(h.Tools, toolName) {
			continue
		}

		stdout, exitCode, err := e.execCommand(ctx, h.Command, payloadBytes, env)
		if err != nil {
			// Command execution error (not an exit code) — warn and continue.
			result.Messages = append(result.Messages, fmt.Sprintf("[hook warning] %s: %v", h.Command, err))
			continue
		}

		msg := strings.TrimSpace(stdout)

		switch exitCode {
		case 0:
			// Allow — stdout is feedback.
			if msg != "" {
				result.Messages = append(result.Messages, msg)
			}
		case 2:
			// Deny — stdout is reason, halt chain.
			result.Denied = true
			if msg != "" {
				result.Messages = append(result.Messages, msg)
			}
			return result, nil
		default:
			// Other exit codes — warn and continue.
			if msg != "" {
				result.Messages = append(result.Messages, fmt.Sprintf("[hook warning] %s (exit %d): %s", h.Command, exitCode, msg))
			}
		}
	}

	return result, nil
}

// execCommand runs a shell command with stdin payload and environment variables.
func (e *Engine) execCommand(ctx context.Context, command string, stdin []byte, env map[string]string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(stdin)

	// Set environment variables.
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), exitErr.ExitCode(), nil
		}
		return "", -1, err
	}

	return stdout.String(), 0, nil
}

// HookRunnerAdapter adapts Engine to the tool.HookRunner interface (narrow interface to avoid import cycles).
type HookRunnerAdapter struct {
	engine *Engine
}

// NewHookRunnerAdapter creates an adapter.
func NewHookRunnerAdapter(e *Engine) *HookRunnerAdapter {
	return &HookRunnerAdapter{engine: e}
}

// RunPreToolUse implements tool.HookRunner.
func (a *HookRunnerAdapter) RunPreToolUse(ctx context.Context, toolName, toolInput string) (bool, []string, error) {
	result, err := a.engine.RunPreToolUse(ctx, toolName, toolInput)
	if err != nil {
		return false, nil, err
	}
	return result.Denied, result.Messages, nil
}

// RunPostToolUse implements tool.HookRunner.
func (a *HookRunnerAdapter) RunPostToolUse(ctx context.Context, toolName, toolInput, toolOutput string, isError bool) (bool, []string, error) {
	result, err := a.engine.RunPostToolUse(ctx, toolName, toolInput, toolOutput, isError)
	if err != nil {
		return false, nil, err
	}
	return result.Denied, result.Messages, nil
}

// matchesToolFilter checks if a tool name matches any of the filter patterns.
// If patterns is empty, matches all tools.
func matchesToolFilter(patterns []string, toolName string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == "*" {
			return true
		}
		if p == toolName {
			return true
		}
		// Prefix match with "*" suffix: "shell.*" matches "shell.exec", "shell.powershell".
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(toolName, prefix) {
				return true
			}
		}
	}
	return false
}
