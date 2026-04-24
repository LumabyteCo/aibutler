// Package powershell provides a PowerShell command executor.
package powershell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	maxOutputBytes = 4096
	defaultTimeout = 30 * time.Second
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Executor runs PowerShell commands with allowlist enforcement.
type Executor struct {
	allowlist []string
	timeout   time.Duration
}

// NewExecutor creates a PowerShell executor with the given command allowlist.
// An empty allowlist permits no commands.
func NewExecutor(allowlist []string) *Executor {
	return &Executor{
		allowlist: allowlist,
		timeout:   defaultTimeout,
	}
}

// SetTimeout overrides the default command timeout.
func (e *Executor) SetTimeout(d time.Duration) { e.timeout = d }

// Execute runs a PowerShell command after validating it against the allowlist.
// It tries pwsh first, then falls back to powershell.
//
// SECURITY: An empty allowlist means no commands are permitted. This matches
// the documented behavior of NewExecutor ("An empty allowlist permits no
// commands.") and the default safe-by-default posture of the shell sandbox.
// Do NOT wrap this check in `if len(allowlist) > 0` — that would silently
// allow ALL commands when the user leaves the allowlist empty, which is the
// opposite of what they'd expect.
func (e *Executor) Execute(ctx context.Context, command string) (string, error) {
	if !e.inAllowlist(command) {
		return "", fmt.Errorf("shell.powershell: command not in allowlist: %q", firstWord(command))
	}

	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	binary := resolvePowerShell()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, binary, "-NonInteractive", "-Command", command) //nolint:gosec
	cmd.Stdout = &limitWriter{w: &stdout, remaining: maxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return "", fmt.Errorf("shell.powershell: timeout after %s", e.timeout)
		}
		// Non-zero exit — return combined output.
		out := stdout.String()
		if errOut := stderr.String(); errOut != "" {
			out += "\n--- stderr ---\n" + errOut
		}
		return out, fmt.Errorf("shell.powershell: %w", err)
	}

	out := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		out += "\n--- stderr ---\n" + errOut
	}
	return out, nil
}

func (e *Executor) inAllowlist(command string) bool {
	cmd := firstWord(command)
	for _, allowed := range e.allowlist {
		if strings.EqualFold(allowed, cmd) {
			return true
		}
	}
	return false
}

// firstWord extracts the first word (command name) from a command string.
func firstWord(command string) string {
	command = strings.TrimSpace(command)
	for i, r := range command {
		if r == ' ' || r == '\t' || r == ';' || r == '|' {
			return command[:i]
		}
	}
	return command
}

// resolvePowerShell returns the path to the best available PowerShell binary.
func resolvePowerShell() string {
	// Prefer pwsh (PowerShell Core, cross-platform).
	if path, err := exec.LookPath("pwsh"); err == nil {
		return path
	}
	// Fall back to Windows PowerShell.
	if path, err := exec.LookPath("powershell"); err == nil {
		return path
	}
	// Return pwsh — Execute will fail with a clear error.
	return "pwsh"
}

// limitWriter wraps a bytes.Buffer with a size limit.
type limitWriter struct {
	w         *bytes.Buffer
	remaining int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil
	}
	if len(p) > lw.remaining {
		p = p[:lw.remaining]
	}
	n, err := lw.w.Write(p)
	lw.remaining -= n
	return n, err
}

// RegisterPowerShellTool registers the shell.powershell tool.
func RegisterPowerShellTool(registry toolRegistry, executor *Executor) {
	registry.Register(
		"shell.powershell",
		"Execute a PowerShell command. Only allowed commands (per allowlist) may be run.",
		`{"type":"object","properties":{"command":{"type":"string","description":"PowerShell command to execute"}},"required":["command"]}`,
		"tool.shell.exec",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("shell.powershell: invalid input: %w", err)
			}
			if args.Command == "" {
				return "", fmt.Errorf("shell.powershell: command is required")
			}
			return executor.Execute(ctx, args.Command)
		},
	)
}
