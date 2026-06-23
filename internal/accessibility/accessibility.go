// Package accessibility provides a Tier 3 accessibility-tree reader —
// a read-only view of an application's on-screen UI element hierarchy
// (buttons, text fields, labels, their roles / names / values).
//
// Tier 3 sits between native scripting (Tier 2: AppleScript, D-Bus,
// PowerShell) and vision-driven input (Tier 4). It lets an agent
// understand what's on screen in a structured, deterministic way —
// without screenshots or pixel reasoning — so it can decide what to act
// on. Acting itself is Tier 2 (scripting) or Tier 4 (synthetic input).
//
// Zero-CGO posture: this reader shells out to platform tools / talks D-Bus
// rather than linking native accessibility APIs (which would require CGO /
// Objective-C on macOS), preserving the single static binary. Per-OS
// backends (accessibility.go = macOS, windows.go, linux.go):
//
//   - macOS — System Events via `osascript`: dumps the UI element tree of
//     a named process's front window.
//   - Windows — PowerShell + .NET UIAutomation: walks the named process's
//     main-window AutomationElement tree.
//   - Linux / FreeBSD — AT-SPI2 over D-Bus (via godbus, already a dep):
//     walks the accessibility registry to the named application and dumps
//     its element tree. Requires an AT-SPI environment (at-spi2-core) and
//     an accessibility-exposing toolkit (GTK/Qt) — returns a clear error
//     when the a11y bus isn't available.
//
// Validation: the macOS backend is exercised live; Linux is validated in
// CI against a headless AT-SPI environment (Xvfb + at-spi2-core); the
// Windows backend is unit-tested for script/parse construction and awaits a
// real Windows desktop (UIAutomation needs an interactive session). See
// docs/computer-use/TIER3-VALIDATION.md for the validation matrix and
// per-OS runbooks.
//
// Security model mirrors the Tier 2 executors:
//
//   - Allowlist of inspectable application names — empty allowlist denies
//     everything. Reading another app's UI is information disclosure, so
//     it's gated even though it's read-only.
//   - Bounded execution timeout and capped output.
//   - Capability gating via tool.accessibility.read at the dispatcher layer.
package accessibility

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
	maxOutputBytes = 16384
	defaultTimeout = 15 * time.Second
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Reader reads accessibility (UI element) trees with allowlist enforcement.
type Reader struct {
	allowlist []string
	timeout   time.Duration
	recorder  action.Recorder // optional
}

// NewReader creates an accessibility reader with the given app-name
// allowlist. An empty allowlist permits inspecting no applications.
func NewReader(allowlist []string) *Reader {
	return &Reader{allowlist: allowlist, timeout: defaultTimeout}
}

// DefaultAllowlist is a conservative set of common, low-risk macOS
// applications whose UI an agent commonly needs to read. Used when the
// operator opts in to default allowlists; an empty allowlist (the
// hard default) inspects nothing.
func DefaultAllowlist() []string {
	return []string{
		"Finder", "System Events", "Notes", "Calendar", "Reminders",
		"Mail", "Safari", "Music", "TextEdit", "Preview",
	}
}

// SetTimeout overrides the default command timeout.
func (r *Reader) SetTimeout(d time.Duration) { r.timeout = d }

// SetRecorder attaches an action recorder. Pass nil to disable.
func (r *Reader) SetRecorder(rec action.Recorder) { r.recorder = rec }

// ReadUI returns a structured snapshot of the named application's UI
// element tree. depth bounds how many levels of the hierarchy are walked
// (1 = top-level elements of the front window; clamped to [1,5]).
func (r *Reader) ReadUI(ctx context.Context, app string, depth int) (string, error) {
	start := time.Now()
	result, err := r.readUI(ctx, app, depth)
	r.record(ctx, app, result, err, time.Since(start))
	return result, err
}

func (r *Reader) readUI(ctx context.Context, app string, depth int) (string, error) {
	app = strings.TrimSpace(app)
	if app == "" {
		return "", fmt.Errorf("accessibility.read: app name is required")
	}
	// Reject names that could break out of the AppleScript string
	// literal. Allowlisted names are config-controlled, but defend in
	// depth against quoting/injection regardless.
	if strings.ContainsAny(app, "\"\\\n\r") {
		return "", fmt.Errorf("accessibility.read: app name contains illegal characters")
	}
	if !r.inAllowlist(app) {
		return "", fmt.Errorf("accessibility.read: app not in allowlist: %q", app)
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}

	switch runtime.GOOS {
	case "darwin":
		return r.readMacOS(ctx, app, depth)
	case "linux", "freebsd":
		return r.readLinux(ctx, app, depth)
	case "windows":
		return r.readWindows(ctx, app, depth)
	default:
		return "", fmt.Errorf("accessibility.read: unsupported OS %q", runtime.GOOS)
	}
}

// readMacOS dumps the UI element tree via System Events / osascript.
func (r *Reader) readMacOS(ctx context.Context, app string, depth int) (string, error) {
	script := buildMacScript(app, depth)

	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, "osascript", "-e", script) //nolint:gosec
	cmd.Stdout = &limitWriter{w: &stdout, remaining: maxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return "", fmt.Errorf("accessibility.read: timeout after %s", r.timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		// System Events reports a missing process / no-accessibility-
		// permission with a recognizable error; surface it helpfully.
		if strings.Contains(msg, "not allowed assistive") || strings.Contains(msg, "osascript is not allowed") {
			return "", fmt.Errorf("accessibility.read: AI Butler lacks Accessibility permission — grant it in System Settings ▸ Privacy & Security ▸ Accessibility")
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("accessibility.read: %s", msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("accessibility.read: no UI elements found for %q (is it running and frontmost?)", app)
	}
	return out, nil
}

// buildMacScript returns an AppleScript that walks the front window's UI
// element tree of `process app` to `depth` levels and emits one
// tab-delimited line per element: "<indent><role>\t<name>\t<value>".
// The app name is already validated to contain no quote/backslash/newline.
func buildMacScript(app string, depth int) string {
	// AppleScript has no easy recursion over UI elements without handlers;
	// build a fixed-depth nested walk. Keep it readable and bounded.
	var b strings.Builder
	b.WriteString(`set out to ""` + "\n")
	b.WriteString(`tell application "System Events"` + "\n")
	b.WriteString(`  if not (exists process "` + app + `") then return ""` + "\n")
	b.WriteString(`  tell process "` + app + `"` + "\n")
	b.WriteString(`    if (count of windows) is 0 then return ""` + "\n")
	b.WriteString(`    set w to front window` + "\n")
	b.WriteString(emitLevel("w", 0, depth))
	b.WriteString(`  end tell` + "\n")
	b.WriteString(`end tell` + "\n")
	b.WriteString(`return out` + "\n")
	return b.String()
}

// emitLevel generates AppleScript that iterates UI elements of `container`
// at the given indent level, emitting role/name/value and recursing up to
// maxDepth.
func emitLevel(container string, level, maxDepth int) string {
	if level >= maxDepth {
		return ""
	}
	elemVar := fmt.Sprintf("e%d", level)
	indent := strings.Repeat("  ", level)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%srepeat with %s in (UI elements of %s)\n", indent, elemVar, container))
	b.WriteString(fmt.Sprintf("%s  set r to \"\"\n", indent))
	b.WriteString(fmt.Sprintf("%s  try\n%s    set r to (role of %s) as string\n%s  end try\n", indent, indent, elemVar, indent))
	b.WriteString(fmt.Sprintf("%s  set nm to \"\"\n", indent))
	b.WriteString(fmt.Sprintf("%s  try\n%s    set nm to (name of %s) as string\n%s  end try\n", indent, indent, elemVar, indent))
	b.WriteString(fmt.Sprintf("%s  set vl to \"\"\n", indent))
	b.WriteString(fmt.Sprintf("%s  try\n%s    set vl to (value of %s) as string\n%s  end try\n", indent, indent, elemVar, indent))
	// Indent marker proportional to depth so the agent sees structure.
	b.WriteString(fmt.Sprintf("%s  set out to out & \"%s\" & r & tab & nm & tab & vl & linefeed\n",
		indent, strings.Repeat("· ", level)))
	b.WriteString(emitLevel(elemVar, level+1, maxDepth))
	b.WriteString(fmt.Sprintf("%send repeat\n", indent))
	return b.String()
}

func (r *Reader) inAllowlist(app string) bool {
	for _, allowed := range r.allowlist {
		if strings.EqualFold(allowed, app) {
			return true
		}
	}
	return false
}

func (r *Reader) record(ctx context.Context, app, result string, err error, dur time.Duration) {
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
	resSnippet := result
	if len(resSnippet) > 200 {
		resSnippet = resSnippet[:200]
	}
	_ = r.recorder.Record(ctx, action.Action{
		Type:           "accessibility.read",
		Target:         app,
		PayloadSummary: app,
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  resSnippet,
		Error:          errStr,
	})
}

// RegisterAccessibilityTool registers accessibility.read_ui.
func RegisterAccessibilityTool(registry toolRegistry, reader *Reader) {
	registry.Register(
		"accessibility.read_ui",
		"Read the on-screen UI element tree (roles, names, values) of a running application's front window. Works on macOS (System Events), Windows (UIAutomation), and Linux (AT-SPI). Returns a tab-delimited, indented snapshot for deciding what to act on.",
		`{"type":"object","properties":{"app":{"type":"string","description":"Application/process name (e.g. \"Mail\")"},"depth":{"type":"integer","description":"Tree depth to walk, 1-5 (default 2)"}},"required":["app"]}`,
		"tool.accessibility.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				App   string `json:"app"`
				Depth int    `json:"depth"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("accessibility.read_ui: invalid input: %w", err)
			}
			if args.Depth == 0 {
				args.Depth = 2
			}
			result, err := reader.ReadUI(ctx, args.App, args.Depth)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"app": args.App, "ui_tree": result})
			return string(out), nil
		},
	)
}

// limitWriter wraps a bytes.Buffer with a size limit.
type limitWriter struct {
	w         *bytes.Buffer
	remaining int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil // discard overflow, report success
	}
	if len(p) > lw.remaining {
		lw.w.Write(p[:lw.remaining])
		lw.remaining = 0
		return len(p), nil
	}
	lw.w.Write(p)
	lw.remaining -= len(p)
	return len(p), nil
}
