//go:build darwin

package permissions

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const probeTimeout = 5 * time.Second

// Settings panel URL prefixes — opened with `open <url>` from a terminal,
// or via "Open URL" in any browser. macOS resolves these to the matching
// pane in System Settings.
const (
	urlAutomation     = "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation"
	urlScreenCapture  = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
	urlAccessibility  = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
)

// Check runs the macOS permission probes and returns a Report.
func Check(ctx context.Context) Report {
	perms := []Permission{
		checkAutomationApp(ctx, "System Events"),
		checkAutomationApp(ctx, "Finder"),
		checkScreenRecording(ctx),
	}
	return Report{
		Platform:    "darwin",
		Permissions: perms,
		Summary:     summarize(perms),
	}
}

// checkAutomationApp probes whether the current process has Automation
// permission to send AppleEvents to the named app.
func checkAutomationApp(ctx context.Context, app string) Permission {
	p := Permission{
		Name:        "Automation: " + app,
		Why:         fmt.Sprintf("Required for AppleScript / shell.applescript / shell.script control of %s.", app),
		SettingsURL: urlAutomation,
		HowToGrant: fmt.Sprintf(
			"System Settings → Privacy & Security → Automation. Find the entry for the process running Butler "+
				"(Terminal, your IDE, or the aibutler binary) and enable %q.", app,
		),
	}

	// Run a no-op AppleScript that returns "ok". If Automation is denied
	// for this target, osascript exits non-zero with TCC error -1743.
	out, err := runOsascript(ctx, fmt.Sprintf(`tell application %q to return "ok"`, app))
	switch {
	case err == nil && strings.Contains(out, "ok"):
		p.Status = StatusGranted
	case err != nil && (strings.Contains(err.Error(), "1743") || strings.Contains(err.Error(), "not authorized") || strings.Contains(err.Error(), "not allowed")):
		p.Status = StatusDenied
		p.LastError = err.Error()
	default:
		p.Status = StatusUnknown
		if err != nil {
			p.LastError = err.Error()
		}
	}
	return p
}

// checkScreenRecording captures a small screenshot to /tmp and inspects
// the size — TCC denial silently writes near-empty files.
func checkScreenRecording(ctx context.Context) Permission {
	p := Permission{
		Name:        "Screen Recording",
		Why:         "Required for screen capture, vision-based UI work, and screenshot tools.",
		SettingsURL: urlScreenCapture,
		HowToGrant: "System Settings → Privacy & Security → Screen Recording. Enable for the process running " +
			"Butler (Terminal, your IDE, or the aibutler binary). You may need to restart the process after enabling.",
	}

	tmp, err := os.CreateTemp("", "aibutler-screenprobe-*.png")
	if err != nil {
		p.Status = StatusUnknown
		p.LastError = "create temp file: " + err.Error()
		return p
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// -x: silent (no shutter sound), -t png: format, -o: omit shadow.
	cmd := exec.CommandContext(probeCtx, "screencapture", "-x", "-t", "png", "-o", tmpPath)
	if err := cmd.Run(); err != nil {
		p.Status = StatusUnknown
		p.LastError = "screencapture: " + err.Error()
		return p
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		// File missing — typically means screencapture refused to write.
		p.Status = StatusDenied
		p.LastError = "screencapture produced no file (likely permission denial)"
		return p
	}

	// A real PNG of even a tiny region is at least a few KB. Empty or
	// near-empty output indicates Screen Recording is denied.
	const minRealPNGSize = 1024
	if info.Size() < minRealPNGSize {
		p.Status = StatusDenied
		p.LastError = fmt.Sprintf("screencapture wrote %d bytes (expected >= %d) — likely permission denial",
			info.Size(), minRealPNGSize)
		return p
	}

	p.Status = StatusGranted
	return p
}

// runOsascript runs a small AppleScript probe and combines stderr into the
// returned error so callers can detect TCC error codes (-1743 etc).
func runOsascript(ctx context.Context, script string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(probeCtx, "osascript", "-e", script)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, msg)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}
