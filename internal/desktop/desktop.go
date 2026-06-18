// Package desktop provides Tier 4 vision + input primitives — screen
// capture and synthetic mouse/keyboard control. It is the last-resort
// automation tier: use it only when no Tier 0-3 path (native API, MCP,
// scripting, accessibility) can accomplish the task.
//
// Risk posture. Synthetic input is the single most dangerous capability
// in the system: it can drive ANY application the user can, with no
// per-app scoping. It is therefore guarded by TWO independent gates:
//
//  1. The capability gate tool.input.control (opt-in grant), and
//  2. An explicit EnableInput flag on the Controller (default OFF), so
//     input is dead even if the capability is somehow granted, until an
//     operator deliberately turns it on in config.
//
// Screen capture is lower-risk (read-only) and guarded by the separate
// tool.screen.capture capability.
//
// Zero-CGO posture. Everything shells out to built-in / standard OS
// tools rather than linking native frameworks, preserving the single
// static binary. Per-OS backends (capture.go, input.go):
//
//   - macOS — screencapture (screenshot) and osascript / System Events
//     (mouse + keyboard). Both ship with the OS.
//   - Linux / FreeBSD — screenshot via the first available of grim
//     (Wayland), gnome-screenshot, spectacle, scrot, maim, or
//     ImageMagick's import; input via xdotool (X11). These are common
//     but not guaranteed present — the tools return a clear "install X"
//     error when a backend is missing.
//   - Windows — screenshot and input via PowerShell (.NET
//     System.Drawing for capture; SendKeys + user32 for input). No
//     third-party install.
//
// Validation note: the macOS backends are exercised by live tests. The
// Linux backends are validated end-to-end in CI (the "Desktop Tier 4
// (Linux live)" job runs the live tests against an Xvfb virtual display
// with scrot + xdotool installed — a real capture and real input
// injection). The Windows backend is unit-tested for command
// construction, key mapping, and escaping; its real on-device behaviour
// (SendKeys / CopyFromScreen need an interactive desktop session) cannot
// be validated in containers or standard CI and awaits a manual run on a
// real Windows desktop.
//
// The full validation story — the per-OS matrix, the Docker+Xvfb recipe
// for Linux, why Windows can't be containerized, and the manual Windows
// runbook — is documented in docs/computer-use/TIER4-VALIDATION.md.
package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/action"
)

const (
	maxOutputBytes = 1024
	defaultTimeout = 15 * time.Second
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Controller performs screen capture and (when enabled) synthetic input.
type Controller struct {
	timeout     time.Duration
	enableInput bool
	recorder    action.Recorder
}

// NewController creates a desktop controller. Screen capture is always
// available (subject to the capability gate); synthetic input is OFF
// until EnableInput is called — a deliberate second gate on the
// highest-risk capability.
func NewController() *Controller {
	return &Controller{timeout: defaultTimeout}
}

// SetTimeout overrides the default command timeout.
func (c *Controller) SetTimeout(d time.Duration) { c.timeout = d }

// EnableInput turns synthetic mouse/keyboard control on or off. Default
// off. This is intentionally separate from the capability gate so input
// stays dead unless an operator explicitly enables it.
func (c *Controller) EnableInput(on bool) { c.enableInput = on }

// SetRecorder attaches an action recorder.
func (c *Controller) SetRecorder(r action.Recorder) { c.recorder = r }

// InputEnabled reports whether synthetic input is currently enabled.
func (c *Controller) InputEnabled() bool { return c.enableInput }

// --- Screen capture ---

// Screenshot captures the full screen and returns PNG bytes.
func (c *Controller) Screenshot(ctx context.Context) ([]byte, error) {
	start := time.Now()
	png, err := c.screenshot(ctx)
	c.record(ctx, "screen.capture", "fullscreen", err, time.Since(start))
	return png, err
}

func (c *Controller) screenshot(ctx context.Context) ([]byte, error) {
	// Per-OS dispatch. All backends shell out to built-in / standard
	// tools (zero CGO) and write a PNG to a server-side temp file, which
	// is then read back — the temp path is never user input.
	switch runtime.GOOS {
	case "darwin":
		return c.screenshotDarwin(ctx)
	case "linux", "freebsd":
		return c.screenshotLinux(ctx)
	case "windows":
		return c.screenshotWindows(ctx)
	default:
		return nil, fmt.Errorf("screen.capture: unsupported OS %q", runtime.GOOS)
	}
}

// captureToTempPNG runs cmd (which must write a PNG to path), then reads
// and returns the bytes. Shared by the per-OS screenshot backends.
func (c *Controller) captureToTempPNG(ctx context.Context, label string, build func(path string) *exec.Cmd, permHint string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "aibutler-shot-*.png")
	if err != nil {
		return nil, fmt.Errorf("%s: temp file: %w", label, err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)

	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	cmd := build(path)
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}
	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return nil, fmt.Errorf("%s: timeout after %s", label, c.timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if permHint != "" && (strings.Contains(strings.ToLower(msg), "permission") || strings.Contains(msg, "not authorized")) {
			return nil, fmt.Errorf("%s: %s", label, permHint)
		}
		return nil, fmt.Errorf("%s: %s", label, msg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: read capture: %w", label, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s: empty capture (likely a permission denial)", label)
	}
	return data, nil
}

// --- Synthetic input (gated) ---

// Click moves the pointer to (x,y) and clicks. Requires input enabled.
func (c *Controller) Click(ctx context.Context, x, y int) error {
	start := time.Now()
	err := c.click(ctx, x, y)
	c.record(ctx, "input.click", fmt.Sprintf("%d,%d", x, y), err, time.Since(start))
	return err
}

func (c *Controller) click(ctx context.Context, x, y int) error {
	if err := c.inputGate(); err != nil {
		return err
	}
	if x < 0 || y < 0 {
		return fmt.Errorf("input.click: coordinates must be non-negative")
	}
	switch runtime.GOOS {
	case "darwin":
		return c.clickDarwin(ctx, x, y)
	case "linux", "freebsd":
		return c.clickLinux(ctx, x, y)
	case "windows":
		return c.clickWindows(ctx, x, y)
	default:
		return errInputUnsupported()
	}
}

// TypeText types the given text via synthetic keystrokes. Requires input
// enabled.
func (c *Controller) TypeText(ctx context.Context, text string) error {
	start := time.Now()
	err := c.typeText(ctx, text)
	c.record(ctx, "input.type", truncate(text, 40), err, time.Since(start))
	return err
}

func (c *Controller) typeText(ctx context.Context, text string) error {
	if err := c.inputGate(); err != nil {
		return err
	}
	if text == "" {
		return fmt.Errorf("input.type: text is required")
	}
	// Reject embedded newlines/CR — synthetic typing of a literal
	// newline is ambiguous; callers should use input.key "return".
	// Each per-OS backend does its own escaping for its tool.
	if strings.ContainsAny(text, "\n\r") {
		return fmt.Errorf("input.type: text must not contain newlines — use input.key \"return\" instead")
	}
	switch runtime.GOOS {
	case "darwin":
		return c.typeDarwin(ctx, text)
	case "linux", "freebsd":
		return c.typeLinux(ctx, text)
	case "windows":
		return c.typeWindows(ctx, text)
	default:
		return errInputUnsupported()
	}
}

// KeyPress presses a single named special key (return, tab, escape,
// space, delete, up, down, left, right). Requires input enabled.
func (c *Controller) KeyPress(ctx context.Context, key string) error {
	start := time.Now()
	err := c.keyPress(ctx, key)
	c.record(ctx, "input.key", key, err, time.Since(start))
	return err
}

func (c *Controller) keyPress(ctx context.Context, key string) error {
	if err := c.inputGate(); err != nil {
		return err
	}
	name := strings.ToLower(strings.TrimSpace(key))
	if _, ok := keyAliases[name]; !ok {
		return fmt.Errorf("input.key: unknown key %q (supported: %s)", key, supportedKeyList)
	}
	switch runtime.GOOS {
	case "darwin":
		return c.keyDarwin(ctx, name)
	case "linux", "freebsd":
		return c.keyLinux(ctx, name)
	case "windows":
		return c.keyWindows(ctx, name)
	default:
		return errInputUnsupported()
	}
}

// inputGate enforces the enable-flag precondition shared by every
// synthetic-input action — the second, operator-controlled gate beyond
// the tool.input.control capability (which is checked at the dispatcher
// layer). OS support is handled per-action in the dispatch switches.
func (c *Controller) inputGate() error {
	if !c.enableInput {
		return fmt.Errorf("synthetic input is disabled — an operator must explicitly enable it (highest-risk capability); it is off by default even when the capability is granted")
	}
	return nil
}

// errInputUnsupported is returned for synthetic input on an OS with no
// backend.
func errInputUnsupported() error {
	return fmt.Errorf("synthetic input: unsupported OS %q", runtime.GOOS)
}

func (c *Controller) record(ctx context.Context, typ, target string, err error, dur time.Duration) {
	if c.recorder == nil {
		return
	}
	status := "success"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
		if strings.Contains(errStr, "disabled") {
			status = "denied"
		}
	}
	_ = c.recorder.Record(ctx, action.Action{
		Type:           typ,
		Target:         target,
		PayloadSummary: target,
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		Error:          errStr,
	})
}

// RegisterDesktopTools registers screen.capture and the (gated) input
// tools.
func RegisterDesktopTools(registry toolRegistry, c *Controller) {
	registry.Register(
		"screen.capture",
		"Capture the full screen and return a base64 PNG data URI. Works on macOS (screencapture), Linux (grim/gnome-screenshot/scrot/ImageMagick), and Windows (PowerShell .NET). macOS requires Screen Recording permission.",
		`{"type":"object","properties":{}}`,
		"tool.screen.capture",
		func(ctx context.Context, _ string) (string, error) {
			png, err := c.Screenshot(ctx)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{
				"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
			})
			return string(out), nil
		},
	)

	registry.Register(
		"input.click",
		"Move the pointer to screen coordinates (x,y) and click. HIGHEST-RISK: synthetic input drives any app. Disabled unless an operator explicitly enables it. macOS (System Events), Linux (xdotool), Windows (PowerShell).",
		`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"}},"required":["x","y"]}`,
		"tool.input.control",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				X int `json:"x"`
				Y int `json:"y"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("input.click: invalid input: %w", err)
			}
			if err := c.Click(ctx, args.X, args.Y); err != nil {
				return "", err
			}
			return `{"result":"clicked at ` + strconv.Itoa(args.X) + `,` + strconv.Itoa(args.Y) + `"}`, nil
		},
	)

	registry.Register(
		"input.type",
		"Type text via synthetic keystrokes into the focused field. HIGHEST-RISK. Disabled unless explicitly enabled. macOS / Linux (xdotool) / Windows (PowerShell SendKeys).",
		`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`,
		"tool.input.control",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("input.type: invalid input: %w", err)
			}
			if err := c.TypeText(ctx, args.Text); err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"result": "typed " + strconv.Itoa(len(args.Text)) + " chars"})
			return string(out), nil
		},
	)

	registry.Register(
		"input.key",
		"Press a single named special key (return, tab, escape, space, delete, up, down, left, right, home, end, pageup, pagedown). HIGHEST-RISK. Disabled unless explicitly enabled. macOS / Linux / Windows.",
		`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`,
		"tool.input.control",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("input.key: invalid input: %w", err)
			}
			if err := c.KeyPress(ctx, args.Key); err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"result": "pressed " + args.Key})
			return string(out), nil
		},
	)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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
		lw.w.Write(p[:lw.remaining])
		lw.remaining = 0
		return len(p), nil
	}
	lw.w.Write(p)
	lw.remaining -= len(p)
	return len(p), nil
}
