# Tier 4 (vision + input) — validation

Tier 4 is the screen-capture + synthetic mouse/keyboard layer
(`internal/desktop`). Every backend shells out to standard OS tools
(zero CGO), so "does the code work" splits into two questions: *does it
construct the right command* (unit-testable anywhere) and *does that
command actually capture/inject on a real display* (only verifiable on
that OS with a live session).

This page records how each platform is validated, and — importantly —
**why Windows can't be validated in containers or standard CI**, plus the
manual runbook to validate it on a real desktop.

## Validation matrix

| Platform | Backend | Unit tests | End-to-end validation |
|----------|---------|-----------|------------------------|
| macOS | `screencapture`, `osascript`/System Events | ✅ everywhere | ✅ live (needs Screen Recording + Accessibility permissions) |
| Linux / FreeBSD | `scrot`/`grim`/… , `xdotool` | ✅ everywhere | ✅ **automated in CI** via Xvfb (`Desktop Tier 4 (Linux live)` job) |
| Windows | PowerShell (.NET `System.Drawing`, `SendKeys`, `user32`) | ✅ everywhere | ⏳ **awaits a real interactive desktop** — see below |

"Unit tests everywhere" = command construction, the cross-OS key table,
SendKeys escaping, and per-OS dispatch. These compile and run on every
platform (the backends are runtime-dispatched, not build-tagged) and are
covered by `internal/desktop/*_test.go`.

The live, end-to-end tests live in `internal/desktop/live_test.go` and
are gated behind `AIBUTLER_DESKTOP_TESTS=1` so they never run in normal
headless CI.

## Linux — automated (Xvfb)

A headless container/runner has no display, but **Xvfb** (X virtual
framebuffer) provides a real X11 display in software, so `scrot` actually
captures a PNG and `xdotool` actually injects input.

This runs on every push in the `Desktop Tier 4 (Linux live)` CI job
(`.github/workflows/ci.yml`). To reproduce locally with Docker:

```bash
docker run --rm -v "$PWD":/src -w /src \
  -e GOTOOLCHAIN=auto -e CGO_ENABLED=0 \
  -e AIBUTLER_DESKTOP_TESTS=1 -e AIBUTLER_ENABLE_SYNTHETIC_INPUT=1 \
  golang:1.26-bookworm bash -c '
    apt-get update -qq && apt-get install -y -qq xvfb scrot xdotool
    Xvfb :99 -screen 0 1280x1024x24 >/tmp/xvfb.log 2>&1 &
    export DISPLAY=:99 && sleep 2
    go test -count=1 -run TestLive_ -v ./internal/desktop/...
  '
```

Expected: `screen.capture` returns a multi-KB PNG; `xdotool` performs a
click, a type, and several named-key presses without error.

> CI note: on GitHub-hosted runners, prefix apt with
> `-o DPkg::Lock::Timeout=120` — the runner's background
> `unattended-upgrades` holds the dpkg lock at job start and apt will
> otherwise hang until the job times out.

## Windows — manual only (and why)

**Windows Tier 4 cannot be validated in Docker, and not reliably in
standard CI.** Two independent blockers:

1. **No Windows containers on non-Windows hosts.** Windows containers
   need a Windows host kernel. Docker Desktop on macOS/Linux cannot run
   them at all — there is no Xvfb-style escape hatch.
2. **No interactive desktop in containers.** Even *on* a Windows host,
   Server Core / Nano containers run with no interactive window station
   (`winsta0`). `SendKeys`, `SetCursorPos`/`mouse_event`, and
   `Graphics.CopyFromScreen` all require an interactive desktop session,
   so they fail or no-op in a container regardless of host.

GitHub-hosted `windows-latest` runners are also borderline: they execute
as a service, so synthetic GUI input and screen capture are unreliable
there. That's why this repo deliberately does **not** add a Windows live
CI job — a flaky job is worse than an honest "validate manually" note.

### Manual Windows validation runbook

On a real Windows machine **with an interactive desktop session logged
in** (not RDP-disconnected, not a service):

```powershell
# 1. Build/checkout the repo, then from its root:
$env:AIBUTLER_DESKTOP_TESTS = "1"
$env:AIBUTLER_ENABLE_SYNTHETIC_INPUT = "1"
$env:CGO_ENABLED = "0"

# 2. Run the live Tier 4 tests:
go test -count=1 -run TestLive_ -v ./internal/desktop/...
```

Expected: `TestLive_ScreenCapture` saves and reads back a PNG of the
desktop; `TestLive_Input` moves the pointer, types text, and presses
named keys. For a visual sanity check, open Notepad and focus it first —
the typed text should appear.

PowerShell availability: the backend prefers `pwsh` (PowerShell 7+) and
falls back to Windows `powershell.exe`; either is fine.

## See also

- `internal/desktop/desktop.go` — package doc + the two-gate security
  model (`tool.input.control` capability **and**
  `AIBUTLER_ENABLE_SYNTHETIC_INPUT=1`).
- `internal/desktop/capture.go`, `input.go` — the per-OS backends.
- `internal/desktop/live_test.go` — the env-gated live tests.
