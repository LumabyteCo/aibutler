package desktop

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// keyMapping holds a special key's representation on each backend.
type keyMapping struct {
	macCode int    // macOS System Events virtual key code
	x11     string // xdotool X11 keysym name
	winSend string // Windows SendKeys token
}

// keyAliases maps friendly key names to their per-OS representations.
var keyAliases = map[string]keyMapping{
	"return":    {36, "Return", "{ENTER}"},
	"enter":     {36, "Return", "{ENTER}"},
	"tab":       {48, "Tab", "{TAB}"},
	"space":     {49, "space", " "},
	"delete":    {51, "BackSpace", "{BACKSPACE}"},
	"backspace": {51, "BackSpace", "{BACKSPACE}"},
	"escape":    {53, "Escape", "{ESC}"},
	"esc":       {53, "Escape", "{ESC}"},
	"left":      {123, "Left", "{LEFT}"},
	"right":     {124, "Right", "{RIGHT}"},
	"down":      {125, "Down", "{DOWN}"},
	"up":        {126, "Up", "{UP}"},
	"home":      {115, "Home", "{HOME}"},
	"end":       {119, "End", "{END}"},
	"pageup":    {116, "Prior", "{PGUP}"},
	"pagedown":  {121, "Next", "{PGDN}"},
}

const supportedKeyList = "return, tab, space, delete, escape, up, down, left, right, home, end, pageup, pagedown"

// runInputCmd runs a synthetic-input command, capturing stderr and
// returning a cleaned error. permHint, if non-empty, is substituted when
// the error looks like a permission denial.
func (c *Controller) runInputCmd(ctx context.Context, label string, cmd *exec.Cmd, permHint string) error {
	var stderr bytes.Buffer
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: timeout after %s", label, c.timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if permHint != "" && looksLikePermissionDenial(msg) {
			return fmt.Errorf("%s: %s", label, permHint)
		}
		return fmt.Errorf("%s: %s", label, msg)
	}
	return nil
}

func looksLikePermissionDenial(msg string) bool {
	l := strings.ToLower(msg)
	return strings.Contains(l, "not allowed assistive") ||
		strings.Contains(l, "is not allowed") ||
		strings.Contains(l, "permission") ||
		strings.Contains(l, "not authorized")
}

// withTimeout derives a per-action timeout context.
func (c *Controller) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

// --- macOS (osascript / System Events) ---

const macAXHint = "AI Butler lacks Accessibility permission — grant it in System Settings ▸ Privacy & Security ▸ Accessibility"

func (c *Controller) clickDarwin(ctx context.Context, x, y int) error {
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	script := fmt.Sprintf(`tell application "System Events" to click at {%d, %d}`, x, y)
	return c.runInputCmd(ec, "input.click", exec.CommandContext(ec, "osascript", "-e", script), macAXHint) //nolint:gosec
}

func (c *Controller) typeDarwin(ctx context.Context, text string) error {
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	// Escape for the AppleScript string literal: backslash then quote.
	esc := strings.ReplaceAll(text, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, esc)
	return c.runInputCmd(ec, "input.type", exec.CommandContext(ec, "osascript", "-e", script), macAXHint) //nolint:gosec
}

func (c *Controller) keyDarwin(ctx context.Context, name string) error {
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	script := fmt.Sprintf(`tell application "System Events" to key code %d`, keyAliases[name].macCode)
	return c.runInputCmd(ec, "input.key", exec.CommandContext(ec, "osascript", "-e", script), macAXHint) //nolint:gosec
}

// --- Linux / FreeBSD (xdotool) ---

func (c *Controller) requireXdotool() (string, error) {
	p, err := exec.LookPath("xdotool")
	if err != nil {
		return "", fmt.Errorf("synthetic input: xdotool not found — install it (e.g. `apt install xdotool`). Note: xdotool targets X11; Wayland sessions need a compositor-specific tool")
	}
	return p, nil
}

func (c *Controller) clickLinux(ctx context.Context, x, y int) error {
	bin, err := c.requireXdotool()
	if err != nil {
		return err
	}
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	// Args are exec arguments (no shell), and coords are validated ints.
	cmd := exec.CommandContext(ec, bin, "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", "1") //nolint:gosec
	return c.runInputCmd(ec, "input.click", cmd, "")
}

func (c *Controller) typeLinux(ctx context.Context, text string) error {
	bin, err := c.requireXdotool()
	if err != nil {
		return err
	}
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	// `--` ends option parsing so text starting with '-' isn't treated
	// as a flag; the text is a single exec arg, so no shell injection.
	cmd := exec.CommandContext(ec, bin, "type", "--clearmodifiers", "--", text) //nolint:gosec
	return c.runInputCmd(ec, "input.type", cmd, "")
}

func (c *Controller) keyLinux(ctx context.Context, name string) error {
	bin, err := c.requireXdotool()
	if err != nil {
		return err
	}
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	cmd := exec.CommandContext(ec, bin, "key", keyAliases[name].x11) //nolint:gosec
	return c.runInputCmd(ec, "input.key", cmd, "")
}

// --- Windows (PowerShell) ---

const winClickScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$x = [int]$args[0]; $y = [int]$args[1]
[System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point($x, $y)
Add-Type -MemberDefinition '[DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint dx, uint dy, uint data, int extra);' -Name U -Namespace Win
[Win.U]::mouse_event(0x0002, 0, 0, 0, 0) # left down
[Win.U]::mouse_event(0x0004, 0, 0, 0, 0) # left up
`

const winSendKeysScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.SendKeys]::SendWait($args[0])
`

func (c *Controller) clickWindows(ctx context.Context, x, y int) error {
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	ps := resolvePowerShell()
	cmd := exec.CommandContext(ec, ps, //nolint:gosec
		"-NoProfile", "-NonInteractive", "-Command", winClickScript, "--",
		strconv.Itoa(x), strconv.Itoa(y))
	return c.runInputCmd(ec, "input.click", cmd, "")
}

func (c *Controller) typeWindows(ctx context.Context, text string) error {
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	ps := resolvePowerShell()
	// Escape SendKeys metacharacters so the text types literally, then
	// pass as a positional arg ($args[0]) — never interpolated into the
	// script body, so there's no PowerShell injection surface.
	cmd := exec.CommandContext(ec, ps, //nolint:gosec
		"-NoProfile", "-NonInteractive", "-Command", winSendKeysScript, "--", escapeSendKeys(text))
	return c.runInputCmd(ec, "input.type", cmd, "")
}

func (c *Controller) keyWindows(ctx context.Context, name string) error {
	ec, cancel := c.withTimeout(ctx)
	defer cancel()
	ps := resolvePowerShell()
	cmd := exec.CommandContext(ec, ps, //nolint:gosec
		"-NoProfile", "-NonInteractive", "-Command", winSendKeysScript, "--", keyAliases[name].winSend)
	return c.runInputCmd(ec, "input.key", cmd, "")
}

// escapeSendKeys escapes the characters that SendKeys treats specially
// (+ ^ % ~ ( ) { } [ ]) by enclosing each in braces, so the text is
// typed literally rather than interpreted as key commands.
func escapeSendKeys(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '+', '^', '%', '~', '(', ')', '{', '}', '[', ']':
			b.WriteByte('{')
			b.WriteRune(r)
			b.WriteByte('}')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
