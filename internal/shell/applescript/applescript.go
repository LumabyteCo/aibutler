// Package applescript provides a macOS AppleScript / JXA executor.
//
// AppleScript is the native scripting language for macOS apps that expose a
// scripting dictionary (Mail, Calendar, Music, Finder, Safari, and many
// third-party apps). JXA (JavaScript for Automation) is the JavaScript-based
// alternative that uses the same scripting bridges.
//
// This package mirrors the security model of internal/shell/powershell:
//
//   - Allowlist-by-first-word — empty allowlist denies everything.
//   - Bounded execution timeout.
//   - Capped output size.
//   - Capability gating via tool.applescript.exec at the dispatcher layer.
//
// AppleScript first-word grammar covers most real scripts: tell, set, get,
// display, do, run, return, on, of, repeat, if. Allowlisting these keywords
// is coarse but matches the existing PowerShell executor's posture. A
// target-application allowlist (e.g. `tell:Mail` to grant only Mail-targeted
// scripts) is a natural follow-up if finer grain is needed.
package applescript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	maxOutputBytes = 4096
	defaultTimeout = 30 * time.Second

	// LanguageAppleScript runs the script as AppleScript (default).
	LanguageAppleScript = "AppleScript"
	// LanguageJavaScript runs the script as JXA via osascript -l JavaScript.
	LanguageJavaScript = "JavaScript"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Executor runs AppleScript / JXA commands with allowlist enforcement.
type Executor struct {
	allowlist []string
	timeout   time.Duration
}

// NewExecutor creates an AppleScript executor with the given first-word allowlist.
// An empty allowlist permits no commands.
func NewExecutor(allowlist []string) *Executor {
	return &Executor{
		allowlist: allowlist,
		timeout:   defaultTimeout,
	}
}

// SetTimeout overrides the default command timeout.
func (e *Executor) SetTimeout(d time.Duration) { e.timeout = d }

// Execute runs an AppleScript (default) or JXA (when language=JavaScript)
// script after validating the first word against the allowlist.
//
// SECURITY: An empty allowlist means no commands are permitted. Mirrors the
// powershell executor's posture — do NOT wrap the allowlist check in
// `if len(allowlist) > 0`, which would silently allow ALL commands.
func (e *Executor) Execute(ctx context.Context, script, language string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf(
			"shell.applescript: macOS-only — you're on %s. "+
				"Try shell.dbus for native Linux automation, shell.powershell on Windows, "+
				"or shell.script for cross-OS routing.", runtime.GOOS)
	}
	if !e.inAllowlist(script) {
		return "", fmt.Errorf("shell.applescript: command not in allowlist: %q", firstWord(script))
	}

	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	args := []string{}
	switch language {
	case "", LanguageAppleScript:
		// default — no -l flag
	case LanguageJavaScript:
		args = append(args, "-l", "JavaScript")
	default:
		return "", fmt.Errorf("shell.applescript: unsupported language %q (use %q or %q)", language, LanguageAppleScript, LanguageJavaScript)
	}
	args = append(args, "-e", script)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, "osascript", args...) //nolint:gosec
	cmd.Stdout = &limitWriter{w: &stdout, remaining: maxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return "", fmt.Errorf("shell.applescript: timeout after %s", e.timeout)
		}
		out := stdout.String()
		if errOut := stderr.String(); errOut != "" {
			out += "\n--- stderr ---\n" + errOut
		}
		return out, fmt.Errorf("shell.applescript: %w", err)
	}

	out := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		out += "\n--- stderr ---\n" + errOut
	}
	return out, nil
}

func (e *Executor) inAllowlist(script string) bool {
	cmd := firstWord(script)
	for _, allowed := range e.allowlist {
		if strings.EqualFold(allowed, cmd) {
			return true
		}
	}
	return false
}

// firstWord extracts the first word (keyword) from an AppleScript / JXA script.
// AppleScript scripts typically begin with: tell, set, get, display, do, run,
// on, of, repeat, if, return.
func firstWord(script string) string {
	script = strings.TrimSpace(script)
	for i, r := range script {
		if r == ' ' || r == '\t' || r == '\n' || r == '(' {
			return script[:i]
		}
	}
	return script
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

// DefaultAllowlist returns a small, curated set of AppleScript first-words
// that cover the common cases of "talk to a Mac app, display a notification,
// or speak some text" — useful as an opt-in starter for users who don't want
// to assemble an allowlist from scratch.
//
// These are intentionally permissive enough to be useful: `tell` is the
// universal entry point for app interaction, `display` covers dialogs and
// notifications, `say` covers TTS, and `get` covers read operations. A
// future target-application allowlist could narrow `tell` to specific apps
// (e.g. tell:Music, tell:Notification Center) — until then, callers who
// opt into defaults accept the broader surface.
//
// Defaults are NOT applied automatically. Callers must merge them into the
// allowlist explicitly (typically gated by a config flag) so secure-by-default
// (empty allowlist denies everything) still holds for fresh installs.
func DefaultAllowlist() []string {
	return []string{
		"tell",    // tell application "X" to ... — primary verb
		"display", // display notification / display dialog — UI output
		"say",     // say "..." — text-to-speech output
		"get",     // get count of messages, get name of, ... — read
	}
}

// RegisterAppleScriptTool registers the shell.applescript tool.
func RegisterAppleScriptTool(registry toolRegistry, executor *Executor) {
	registry.Register(
		"shell.applescript",
		"Execute an AppleScript or JXA script on macOS via osascript. Only allowed first-words (per allowlist) may run. Use language=JavaScript for JXA.",
		`{"type":"object","properties":{"script":{"type":"string","description":"AppleScript or JXA source (e.g. 'tell application \"Mail\" to get count of messages in inbox')"},"language":{"type":"string","enum":["AppleScript","JavaScript"],"description":"Script language. Defaults to AppleScript.","default":"AppleScript"}},"required":["script"]}`,
		"tool.applescript.exec",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Script   string `json:"script"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("shell.applescript: invalid input: %w", err)
			}
			if args.Script == "" {
				return "", fmt.Errorf("shell.applescript: script is required")
			}
			return executor.Execute(ctx, args.Script, args.Language)
		},
	)
}
