package accessibility

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// winUIAScript walks the named process's main-window UIAutomation tree to
// a depth, emitting one tab-delimited "<indent><role>\t<name>\t<value>"
// line per element — the same shape as the macOS backend.
//
// $args[0] = process name (no .exe), $args[1] = max depth. Both are
// passed as positional args, never interpolated into the script body, so
// there is no PowerShell injection surface (and the app name is already
// validated + allowlisted by the caller).
const winUIAScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$procName = $args[0]
$maxDepth = [int]$args[1]
$tab = [char]9
$dot = [string]([char]0x00B7) + ' '
$proc = Get-Process -Name $procName -ErrorAction SilentlyContinue |
        Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if (-not $proc) { return }
$root = [System.Windows.Automation.AutomationElement]::FromHandle($proc.MainWindowHandle)
if (-not $root) { return }
function Walk($el, $level, $max) {
  if ($level -ge $max) { return }
  $children = $el.FindAll(
    [System.Windows.Automation.TreeScope]::Children,
    [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($c in $children) {
    $role = ($c.Current.ControlType.ProgrammaticName) -replace 'ControlType\.', ''
    $name = $c.Current.Name
    $val = ''
    try {
      $vp = $c.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern)
      if ($vp) { $val = $vp.Current.Value }
    } catch {}
    $indent = $dot * $level
    Write-Output ($indent + $role + $tab + $name + $tab + $val)
    Walk $c ($level + 1) $max
  }
}
Walk $root 0 $maxDepth
`

func (r *Reader) readWindows(ctx context.Context, app string, depth int) (string, error) {
	ps := resolvePowerShell()

	execCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(execCtx, ps, //nolint:gosec
		"-NoProfile", "-NonInteractive", "-Command", winUIAScript, "--", app, strconv.Itoa(depth))
	cmd.Stdout = &limitWriter{w: &stdout, remaining: maxOutputBytes}
	cmd.Stderr = &limitWriter{w: &stderr, remaining: maxOutputBytes}

	if err := cmd.Run(); err != nil {
		if execCtx.Err() != nil {
			return "", fmt.Errorf("accessibility.read: timeout after %s", r.timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("accessibility.read: %s", msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("accessibility.read: no UI elements found for %q (is it running with a visible window?)", app)
	}
	return out, nil
}

// resolvePowerShell returns the best available PowerShell binary: pwsh
// (PowerShell 7+) if present, else Windows powershell.exe.
func resolvePowerShell() string {
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	return "powershell"
}
