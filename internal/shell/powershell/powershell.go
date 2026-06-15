// Package powershell provides a PowerShell command executor.
//
// Security model: allowlist-by-first-cmdlet (empty allowlist denies all),
// bounded timeout, capped output, capability gating via
// tool.powershell.exec at the dispatcher layer.
//
// Because `pwsh -Command` runs the entire submitted string, the executor
// rejects statement chaining and sub-expressions (`;`, `|`, `&`, backtick,
// `$(...)`, `@(...)`, newlines) BEFORE the allowlist check — hardened in
// v0.4.2. The allowlist validates only the first cmdlet, so this enforces
// one-cmdlet-per-call and closes the chaining bypass where an allowlisted
// producer cmdlet smuggles arbitrary downstream stages. The guard is
// deliberately conservative (it also rejects those metacharacters inside
// quoted strings) — it fails closed.
package powershell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/action"
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
	recorder  action.Recorder // optional — nil disables recording
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

// SetRecorder attaches an action recorder. Pass nil to disable.
// Each Execute call emits one Action row when a recorder is set.
func (e *Executor) SetRecorder(r action.Recorder) { e.recorder = r }

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
	start := time.Now()
	result, err := e.execute(ctx, command)
	e.recordAction(ctx, command, result, err, time.Since(start))
	return result, err
}

func (e *Executor) execute(ctx context.Context, command string) (string, error) {
	// Reject statement chaining and sub-expressions BEFORE the allowlist
	// check. The allowlist validates only the first cmdlet (firstWord),
	// but `pwsh -Command` runs the ENTIRE string — so a single allowlisted
	// producer cmdlet could otherwise smuggle arbitrary downstream stages
	// via `;`, `|`, `&`, backtick, `$(...)`, `@(...)`, or a newline. The
	// allowlist model is one-cmdlet-per-call; enforce that here. This is
	// deliberately conservative: a `;`/`|`/`&` that happens to sit inside
	// a quoted string is also rejected (fail closed) rather than risk a
	// parser-confusion bypass.
	if sep := statementChainingChar(command); sep != "" {
		return "", fmt.Errorf(
			"shell.powershell: statement chaining/sub-expressions are not permitted "+
				"(found %q) — submit a single cmdlet invocation", sep)
	}
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

// recordAction emits one Action row for the just-completed Execute when
// a recorder is attached.
func (e *Executor) recordAction(ctx context.Context, command, result string, err error, dur time.Duration) {
	if e.recorder == nil {
		return
	}
	status := "success"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
		if strings.Contains(errStr, "not in allowlist") {
			status = "denied"
		}
	}
	verb := firstWord(command)
	payloadJSON, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{command})
	resSnippet := result
	if len(resSnippet) > 200 {
		resSnippet = resSnippet[:200]
	}
	_ = e.recorder.Record(ctx, action.Action{
		Type:           "powershell.exec",
		Target:         verb,
		PayloadSummary: verb,
		PayloadFull:    string(payloadJSON),
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  resSnippet,
		Error:          errStr,
	})
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

// statementChainingChars are the PowerShell metacharacters that begin a
// new statement, pipeline stage, sub-expression, call, or line — any of
// which lets a command reach beyond the single allowlisted first cmdlet.
var statementChainingChars = []string{
	";",   // statement separator
	"|",   // pipeline
	"&",   // call operator / background (covers && too)
	"`",   // backtick: line continuation / escape, can hide separators
	"$(",  // sub-expression
	"@(",  // array sub-expression
	"\n",  // newline statement separator
	"\r",  // carriage-return separator
	"\x00", // NUL — defensive
}

// statementChainingChar returns the first chaining metacharacter found
// in the command, or "" if the command is a single simple statement.
func statementChainingChar(command string) string {
	for _, c := range statementChainingChars {
		if strings.Contains(command, c) {
			return c
		}
	}
	return ""
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
