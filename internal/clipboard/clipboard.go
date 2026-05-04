// Package clipboard provides cross-platform read/write access to the OS
// clipboard via native command-line tools — no CGO required.
//
// Per-OS commands:
//
//   - macOS:    pbcopy (write) / pbpaste (read)
//   - Linux:    wl-copy / wl-paste under Wayland (auto-detected via
//                WAYLAND_DISPLAY env var); falls back to xclip on X11
//   - Windows:  clip.exe (write) and PowerShell Get-Clipboard (read)
//
// Security:
//
//   - Read/write are gated by separate capabilities (tool.clipboard.read /
//     tool.clipboard.write) so callers can grant write-only or read-only.
//   - Clipboard contents are commonly sensitive (passwords from password
//     managers, 2FA codes). Audit logs are full by default.
//   - Read output is capped at 64 KiB to prevent agents from accidentally
//     pulling huge in-memory blobs. Larger reads are truncated with a
//     marker line.
package clipboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/action"
)

const (
	maxReadBytes   = 64 * 1024
	defaultTimeout = 10 * time.Second
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Client reads and writes the OS clipboard.
type Client struct {
	timeout time.Duration
	// commandFinder allows tests to override how the backend binary is discovered.
	commandFinder func(name string) (string, error)
	recorder      action.Recorder // optional — nil disables recording
}

// NewClient creates a clipboard client with the default timeout.
func NewClient() *Client {
	return &Client{
		timeout:       defaultTimeout,
		commandFinder: exec.LookPath,
	}
}

// SetTimeout overrides the default command timeout.
func (c *Client) SetTimeout(d time.Duration) { c.timeout = d }

// SetRecorder attaches an action recorder. Pass nil to disable.
//
// Privacy note: clipboard contents are commonly sensitive (passwords from
// password managers, 2FA codes). The recorder logs READ size only — never
// the actual content read. WRITE payloads ARE logged, redacted by the
// recorder's audit pipeline before storage; treat the agent's chosen
// write text as sensitive accordingly.
func (c *Client) SetRecorder(r action.Recorder) { c.recorder = r }

// Read returns the current clipboard contents (text only). Output is capped
// at maxReadBytes — anything beyond is replaced with a truncation marker.
func (c *Client) Read(ctx context.Context) (string, error) {
	start := time.Now()
	out, err := c.read(ctx)
	c.recordRead(ctx, len(out), err, time.Since(start))
	return out, err
}

func (c *Client) read(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	binary, args, err := c.readCommand()
	if err != nil {
		return "", err
	}

	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("clipboard.read: timeout after %s", c.timeout)
		}
		return "", fmt.Errorf("clipboard.read: %w", err)
	}

	out := stdout.String()
	if len(out) > maxReadBytes {
		out = out[:maxReadBytes] + "\n... [truncated: clipboard exceeds " + fmt.Sprintf("%d", maxReadBytes) + " bytes]"
	}
	return out, nil
}

// Write replaces the clipboard contents with the given text.
func (c *Client) Write(ctx context.Context, text string) error {
	start := time.Now()
	err := c.write(ctx, text)
	c.recordWrite(ctx, text, err, time.Since(start))
	return err
}

func (c *Client) write(ctx context.Context, text string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	binary, args, err := c.writeCommand()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec
	cmd.Stdin = strings.NewReader(text)

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("clipboard.write: timeout after %s", c.timeout)
		}
		return fmt.Errorf("clipboard.write: %w", err)
	}
	return nil
}

// recordRead logs a clipboard read. Privacy: only the byte count is
// recorded, never the actual content read (which may be a password).
func (c *Client) recordRead(ctx context.Context, bytesRead int, err error, dur time.Duration) {
	if c.recorder == nil {
		return
	}
	status := "success"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
	}
	_ = c.recorder.Record(ctx, action.Action{
		Type:           "clipboard.read",
		Target:         "os.clipboard",
		PayloadSummary: "read",
		PayloadFull:    `{"op":"read"}`,
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  fmt.Sprintf("%d bytes read", bytesRead),
		Error:          errStr,
	})
}

// recordWrite logs a clipboard write. The full text is included in the
// payload — the recorder's audit-redact pipeline catches credentials
// before storage. Treat agent-chosen write text as sensitive.
func (c *Client) recordWrite(ctx context.Context, text string, err error, dur time.Duration) {
	if c.recorder == nil {
		return
	}
	status := "success"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
	}
	preview := text
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	payloadJSON, _ := json.Marshal(struct {
		Op   string `json:"op"`
		Text string `json:"text"`
	}{"write", text})
	_ = c.recorder.Record(ctx, action.Action{
		Type:           "clipboard.write",
		Target:         "os.clipboard",
		PayloadSummary: fmt.Sprintf("write %d bytes", len(text)),
		PayloadFull:    string(payloadJSON),
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  preview,
		Error:          errStr,
	})
}

// readCommand resolves the backend command for reading.
func (c *Client) readCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		path, err := c.commandFinder("pbpaste")
		if err != nil {
			return "", nil, fmt.Errorf("clipboard.read: pbpaste not found in PATH (macOS built-in)")
		}
		return path, nil, nil
	case "linux":
		// Prefer Wayland when WAYLAND_DISPLAY is set.
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if path, err := c.commandFinder("wl-paste"); err == nil {
				return path, nil, nil
			}
		}
		if path, err := c.commandFinder("xclip"); err == nil {
			return path, []string{"-selection", "clipboard", "-o"}, nil
		}
		return "", nil, fmt.Errorf("clipboard.read: install wl-clipboard (Wayland) or xclip (X11) — neither found in PATH")
	case "windows":
		path, err := c.commandFinder("powershell")
		if err != nil {
			return "", nil, fmt.Errorf("clipboard.read: powershell.exe not found in PATH (Windows built-in)")
		}
		return path, []string{"-NoProfile", "-Command", "Get-Clipboard"}, nil
	default:
		return "", nil, fmt.Errorf("clipboard.read: unsupported OS %s", runtime.GOOS)
	}
}

// writeCommand resolves the backend command for writing.
func (c *Client) writeCommand() (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		path, err := c.commandFinder("pbcopy")
		if err != nil {
			return "", nil, fmt.Errorf("clipboard.write: pbcopy not found in PATH (macOS built-in)")
		}
		return path, nil, nil
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if path, err := c.commandFinder("wl-copy"); err == nil {
				return path, nil, nil
			}
		}
		if path, err := c.commandFinder("xclip"); err == nil {
			return path, []string{"-selection", "clipboard"}, nil
		}
		return "", nil, fmt.Errorf("clipboard.write: install wl-clipboard (Wayland) or xclip (X11) — neither found in PATH")
	case "windows":
		path, err := c.commandFinder("clip")
		if err != nil {
			return "", nil, fmt.Errorf("clipboard.write: clip.exe not found in PATH (Windows built-in)")
		}
		return path, nil, nil
	default:
		return "", nil, fmt.Errorf("clipboard.write: unsupported OS %s", runtime.GOOS)
	}
}

// RegisterTools registers clipboard.read and clipboard.write.
func RegisterTools(registry toolRegistry, client *Client) {
	registry.Register(
		"clipboard.read",
		"Return the current OS clipboard contents (text). Output is capped at 64 KiB.",
		`{"type":"object","properties":{},"additionalProperties":false}`,
		"tool.clipboard.read",
		func(ctx context.Context, _ string) (string, error) {
			return client.Read(ctx)
		},
	)
	registry.Register(
		"clipboard.write",
		"Replace the OS clipboard contents with the given text.",
		`{"type":"object","properties":{"text":{"type":"string","description":"Text to place on the clipboard"}},"required":["text"]}`,
		"tool.clipboard.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("clipboard.write: invalid input: %w", err)
			}
			if err := client.Write(ctx, args.Text); err != nil {
				return "", err
			}
			return "ok", nil
		},
	)
}
