// Package shortcuts provides a macOS Shortcuts (shortcuts CLI) runner.
//
// Apple Shortcuts let users author multi-step automations in the GUI and
// invoke them by name from the CLI. This is a higher-level scripting mechanism
// than raw AppleScript: users build the workflow once in Shortcuts.app, then
// AI Butler triggers it. Inputs and outputs flow through stdin/stdout.
//
// Security model:
//
//   - Allowlist by exact shortcut name — empty allowlist denies everything.
//   - Bounded execution timeout.
//   - Capped output size.
//   - Capability gating via tool.shortcuts.run at the dispatcher layer.
package shortcuts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
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

// Runner runs macOS Shortcuts by name with allowlist enforcement.
type Runner struct {
	allowlist []string
	timeout   time.Duration
	recorder  action.Recorder // optional — nil disables recording
}

// NewRunner creates a Shortcuts runner with the given allowlist of shortcut names.
// Names are matched case-insensitively (Shortcuts itself is case-insensitive on lookup).
// An empty allowlist permits no shortcuts.
func NewRunner(allowlist []string) *Runner {
	return &Runner{
		allowlist: allowlist,
		timeout:   defaultTimeout,
	}
}

// SetTimeout overrides the default execution timeout.
func (r *Runner) SetTimeout(d time.Duration) { r.timeout = d }

// SetRecorder attaches an action recorder. Pass nil to disable.
// Each Run emits one Action row when a recorder is set.
func (r *Runner) SetRecorder(rec action.Recorder) { r.recorder = rec }

// Run invokes the named Shortcut with optional stdin input.
// Returns the shortcut's stdout (and stderr appended on non-zero exit).
//
// SECURITY: An empty allowlist denies everything. Mirrors the powershell
// executor's posture — do NOT silently allow ALL shortcuts when allowlist is empty.
func (r *Runner) Run(ctx context.Context, name, input string) (string, error) {
	start := time.Now()
	result, err := r.run(ctx, name, input)
	r.recordAction(ctx, name, input, result, err, time.Since(start))
	return result, err
}

func (r *Runner) run(ctx context.Context, name, input string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf(
			"shell.shortcuts: macOS-only (Apple Shortcuts CLI) — you're on %s. "+
				"For other OSes try shell.dbus (linux), shell.powershell (windows), "+
				"or shell.script for cross-OS routing.", runtime.GOOS)
	}
	if name == "" {
		return "", fmt.Errorf("shell.shortcuts: shortcut name is required")
	}
	if !r.inAllowlist(name) {
		return "", fmt.Errorf("shell.shortcuts: shortcut not in allowlist: %q", name)
	}

	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := []string{"run", name}
	if input != "" {
		// `shortcuts run <name> --input-path -` reads input from stdin.
		args = append(args, "--input-path", "-")
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, "shortcuts", args...) //nolint:gosec
	cmd.Stdout = &limitWriter{w: &stdout, remaining: maxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return "", fmt.Errorf("shell.shortcuts: timeout after %s", r.timeout)
		}
		out := stdout.String()
		if errOut := stderr.String(); errOut != "" {
			out += "\n--- stderr ---\n" + errOut
		}
		return out, fmt.Errorf("shell.shortcuts: %w", err)
	}

	out := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		out += "\n--- stderr ---\n" + errOut
	}
	return out, nil
}

func (r *Runner) inAllowlist(name string) bool {
	for _, allowed := range r.allowlist {
		if strings.EqualFold(allowed, name) {
			return true
		}
	}
	return false
}

// recordAction emits one Action row for the just-completed Run when a
// recorder is attached.
func (r *Runner) recordAction(ctx context.Context, name, input, result string, err error, dur time.Duration) {
	if r.recorder == nil {
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
	payloadJSON, _ := json.Marshal(struct {
		Name  string `json:"name"`
		Input string `json:"input,omitempty"`
	}{name, input})
	resSnippet := result
	if len(resSnippet) > 200 {
		resSnippet = resSnippet[:200]
	}
	_ = r.recorder.Record(ctx, action.Action{
		Type:           "shortcuts.run",
		Target:         name,
		PayloadSummary: "run:" + name,
		PayloadFull:    string(payloadJSON),
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  resSnippet,
		Error:          errStr,
	})
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

// DefaultAllowlist returns the default shortcut-name allowlist.
//
// Unlike AppleScript / D-Bus, Apple Shortcuts have no shared, well-known
// system shortcuts — every shortcut is authored by the user in Shortcuts.app
// and named arbitrarily. There are no sensible defaults to ship; callers
// must populate the allowlist with the names of shortcuts they've created.
//
// Returned for API symmetry with the other executors so callers can
// uniformly merge defaults from each package.
func DefaultAllowlist() []string {
	return nil
}

// RegisterShortcutsTool registers the shell.shortcuts tool.
func RegisterShortcutsTool(registry toolRegistry, runner *Runner) {
	registry.Register(
		"shell.shortcuts",
		"Invoke a macOS Shortcut by name. Only allowed shortcuts (per allowlist) may run. Optionally pipe stdin via the input field.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Shortcut name as it appears in Shortcuts.app"},"input":{"type":"string","description":"Optional stdin input piped to the shortcut"}},"required":["name"]}`,
		"tool.shortcuts.run",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Name  string `json:"name"`
				Input string `json:"input"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("shell.shortcuts: invalid input: %w", err)
			}
			return runner.Run(ctx, args.Name, args.Input)
		},
	)
}
