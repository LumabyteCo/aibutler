# Tier 3 (accessibility tree) — validation

Tier 3 is the read-only accessibility-tree reader (`internal/accessibility`):
a structured, deterministic view of an application's on-screen UI element
hierarchy — roles, names, values — so an agent can decide what to act on
*without* screenshots or pixel reasoning. Acting itself is Tier 2
(scripting) or Tier 4 (synthetic input).

Like Tier 4, every backend is **zero-CGO**: it shells out to platform tools
or talks D-Bus rather than linking native accessibility APIs (which would
drag in CGO / Objective-C). So "does the code work" splits into two
questions: *does it construct the right query* (unit-testable anywhere) and
*does that query actually walk a live UI tree* (only verifiable on that OS
with a real accessibility environment).

## Validation matrix

| Platform | Backend | Unit tests | End-to-end validation |
|----------|---------|-----------|------------------------|
| macOS | System Events via `osascript` | ✅ everywhere | ✅ live (needs Accessibility permission) |
| Linux / FreeBSD | AT-SPI2 over D-Bus (`godbus`) | ✅ everywhere | ✅ **automated in CI** via Xvfb + at-spi2-core (`Accessibility Tier 3 (Linux AT-SPI live)` job) |
| Windows | PowerShell + .NET UIAutomation | ✅ everywhere | ⏳ **awaits a real interactive desktop** — see below |

"Unit tests everywhere" = the macOS AppleScript builder, the Windows
UIAutomation script shape (positional-arg passing, no app-name
interpolation, `[char]9` tab separators), the Linux property-variant
formatter, allowlist + injection enforcement, and per-OS dispatch. These
compile and run on every platform (the backends are runtime-dispatched, not
build-tagged) and are covered by `internal/accessibility/*_test.go`.

The live, end-to-end test lives in `internal/accessibility/live_test.go` and
is gated behind `AIBUTLER_ACCESSIBILITY_TESTS=1` so it never runs in normal
headless CI.

## Linux — automated (Xvfb + at-spi2-core)

A headless container/runner has no display and no accessibility bus. Two
pieces fix that:

- **Xvfb** (X virtual framebuffer) provides a real X11 display in software.
- A **private session D-Bus** (`dbus-run-session`) lets at-spi2-core
  auto-activate the a11y bus on first `org.a11y.Bus.GetAddress`.

With a GTK app (zenity) on the virtual display, the AT-SPI backend connects
to the a11y bus, finds the application in the registry, and walks its real
UI element tree over godbus.

This runs on every push in the `Accessibility Tier 3 (Linux AT-SPI live)` CI
job (`.github/workflows/ci.yml`). To reproduce locally with Docker:

```bash
docker run --rm -v "$PWD":/src -w /src \
  -e GOTOOLCHAIN=auto -e CGO_ENABLED=0 \
  golang:1.26-bookworm bash -c '
    apt-get update -qq && apt-get install -y -qq xvfb dbus-x11 at-spi2-core zenity
    dbus-run-session -- bash -c "
      Xvfb :99 -screen 0 1024x768x24 >/tmp/xvfb.log 2>&1 &
      export DISPLAY=:99 && sleep 2
      export NO_AT_BRIDGE=0
      zenity --info --title=ProbeWindow --text=probe >/tmp/zenity.log 2>&1 &
      sleep 4
      AIBUTLER_ACCESSIBILITY_TESTS=1 AIBUTLER_A11Y_APP=zenity \
        go test -count=1 -run TestLive_ReadUI -v ./internal/accessibility/...
    "
  '
```

Expected: `TestLive_ReadUI` returns a tab-delimited, indented UI tree whose
first line is the dialog (`dialog\tProbeWindow`) followed by its nested
child elements — proving the godbus registry walk, `GetChildren` recursion,
`GetRoleName`, and `Name`-property reads all work against a live a11y bus.

> CI note: on GitHub-hosted runners, prefix apt with
> `-o DPkg::Lock::Timeout=120` — the runner's background
> `unattended-upgrades` holds the dpkg lock at job start and apt will
> otherwise hang until the job times out.

## Windows — manual only (and why)

**Windows Tier 3 cannot be validated in Docker, and not reliably in standard
CI** — the same blockers as Tier 4:

1. **No Windows containers on non-Windows hosts.** Windows containers need a
   Windows host kernel; Docker Desktop on macOS/Linux cannot run them.
2. **No interactive desktop in containers.** UIAutomation reads the live
   element tree of an on-screen window. `AutomationElement.FromHandle`
   needs a real window on an interactive window station (`winsta0`), which
   Server Core / Nano containers don't provide.

GitHub-hosted `windows-latest` runners execute as a service with no
interactive desktop, so there's typically no visible top-level window to
walk. That's why this repo deliberately does **not** add a Windows live CI
job — a flaky job is worse than an honest "validate manually" note.

### Manual Windows validation runbook

On a real Windows machine **with an interactive desktop session logged in**
(not RDP-disconnected, not a service):

```powershell
# 1. Build/checkout the repo, then from its root:
$env:AIBUTLER_ACCESSIBILITY_TESTS = "1"
$env:AIBUTLER_A11Y_APP = "notepad"   # any running app with a visible window
$env:CGO_ENABLED = "0"

# 2. Open the target app so it has a visible window:
notepad

# 3. Run the live Tier 3 test:
go test -count=1 -run TestLive_ReadUI -v ./internal/accessibility/...
```

Expected: a tab-delimited UI tree of Notepad's window (menu bar, edit
control, etc.). Note the app name is the **process name without `.exe`**
(`notepad`, not `Notepad.exe`) and must be in the reader's allowlist.

PowerShell availability: the backend prefers `pwsh` (PowerShell 7+) and
falls back to Windows `powershell.exe`; either is fine.

## See also

- `internal/accessibility/accessibility.go` — package doc, security model
  (allowlist + injection guard + `tool.accessibility.read` capability), and
  the macOS backend.
- `internal/accessibility/windows.go`, `linux.go` — the Windows UIAutomation
  and Linux AT-SPI backends.
- `internal/accessibility/live_test.go` — the env-gated live test.
- `docs/computer-use/TIER4-VALIDATION.md` — the sibling Tier 4 runbook.
