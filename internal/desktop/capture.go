package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// --- macOS ---

func (c *Controller) screenshotDarwin(ctx context.Context) ([]byte, error) {
	// -x: no capture sound. -t png: PNG output.
	return c.captureToTempPNG(ctx, "screen.capture",
		func(path string) *exec.Cmd {
			return exec.CommandContext(ctx, "screencapture", "-x", "-t", "png", path) //nolint:gosec
		},
		"AI Butler lacks Screen Recording permission — grant it in System Settings ▸ Privacy & Security ▸ Screen Recording")
}

// --- Linux / FreeBSD ---

// captureTool is one screenshot CLI: its binary name and a builder that
// produces the args to write a PNG to the given path.
type captureTool struct {
	bin     string
	argsFor func(path string) []string
}

// linuxCaptureTools lists screenshot CLIs in preference order. The first
// one found on PATH is used.
var linuxCaptureTools = []captureTool{
	{"grim", func(p string) []string { return []string{p} }},                        // Wayland (wlroots: sway, etc.)
	{"gnome-screenshot", func(p string) []string { return []string{"-f", p} }},      // GNOME
	{"spectacle", func(p string) []string { return []string{"-b", "-n", "-o", p} }}, // KDE: background, no-notify, output
	{"scrot", func(p string) []string { return []string{"-o", p} }},                 // generic X11 (overwrite)
	{"maim", func(p string) []string { return []string{p} }},                        // generic X11
	{"import", func(p string) []string { return []string{"-window", "root", p} }},   // ImageMagick
}

func (c *Controller) screenshotLinux(ctx context.Context) ([]byte, error) {
	for _, t := range linuxCaptureTools {
		if _, err := exec.LookPath(t.bin); err != nil {
			continue
		}
		t := t // capture loop var
		return c.captureToTempPNG(ctx, "screen.capture",
			func(path string) *exec.Cmd {
				return exec.CommandContext(ctx, t.bin, t.argsFor(path)...) //nolint:gosec
			}, "")
	}
	names := make([]string, len(linuxCaptureTools))
	for i, t := range linuxCaptureTools {
		names[i] = t.bin
	}
	return nil, fmt.Errorf(
		"screen.capture: no screenshot tool found on Linux (looked for %s) — install one (e.g. `apt install grim` on Wayland or `apt install scrot` on X11)",
		strings.Join(names, ", "))
}

// --- Windows ---

// winCaptureScript is a PowerShell script that captures the full virtual
// screen to the PNG path passed as the first positional arg ($args[0]).
const winCaptureScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$vs = [System.Windows.Forms.SystemInformation]::VirtualScreen
$bmp = New-Object System.Drawing.Bitmap $vs.Width, $vs.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($vs.Location, [System.Drawing.Point]::Empty, $vs.Size)
$bmp.Save($args[0], [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose(); $bmp.Dispose()
`

func (c *Controller) screenshotWindows(ctx context.Context) ([]byte, error) {
	ps := resolvePowerShell()
	return c.captureToTempPNG(ctx, "screen.capture",
		func(path string) *exec.Cmd {
			// Pass the temp path as a positional arg ($args[0]) rather
			// than interpolating it into the script body — no injection
			// surface (and the path is server-controlled anyway).
			return exec.CommandContext(ctx, ps, //nolint:gosec
				"-NoProfile", "-NonInteractive", "-Command", winCaptureScript, "--", path)
		}, "")
}

// resolvePowerShell returns the best available PowerShell binary: pwsh
// (cross-platform PowerShell 7+) if present, else Windows powershell.exe.
func resolvePowerShell() string {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	return "powershell"
}
