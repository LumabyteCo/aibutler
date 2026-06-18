package desktop_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/desktop"
)

// These tests exercise the REAL per-OS backends end-to-end against a
// live display. They're gated behind AIBUTLER_DESKTOP_TESTS=1 so they
// never run in normal CI (which is headless). They are the on-device
// validation for the Linux / Windows backends:
//
//   - macOS: run directly (needs Screen Recording + Accessibility perms).
//   - Linux: run inside a container with Xvfb + scrot + xdotool, e.g.
//       Xvfb :99 -screen 0 1024x768x24 & export DISPLAY=:99
//       AIBUTLER_DESKTOP_TESTS=1 AIBUTLER_ENABLE_SYNTHETIC_INPUT=1 \
//         go test -run TestLive_ -v ./internal/desktop/...
//   - Windows: run on a real desktop session (GitHub windows-latest).

func desktopLiveOrSkip(t *testing.T) {
	t.Helper()
	if os.Getenv("AIBUTLER_DESKTOP_TESTS") == "" {
		t.Skip("set AIBUTLER_DESKTOP_TESTS=1 (with a live display) to run desktop live tests")
	}
}

// TestLive_ScreenCapture captures the real screen and asserts the result
// is a non-trivial PNG. On Linux this validates the scrot/grim/… backend
// against the (virtual) display.
func TestLive_ScreenCapture(t *testing.T) {
	desktopLiveOrSkip(t)
	c := desktop.NewController()
	c.SetTimeout(20 * time.Second)

	png, err := c.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if len(png) < 100 {
		t.Fatalf("screenshot too small (%d bytes) — likely not a real capture", len(png))
	}
	// PNG magic: 0x89 'P' 'N' 'G' \r \n 0x1a \n.
	if png[0] != 0x89 || png[1] != 'P' || png[2] != 'N' || png[3] != 'G' {
		t.Errorf("capture is not a PNG (first bytes: % x)", png[:8])
	}
	t.Logf("captured a %d-byte PNG", len(png))
}

// TestLive_Input drives the real synthetic-input backend against the
// live display. With no focused target it can't assert a visible effect,
// but a clean exit proves command construction + tool invocation work
// (e.g. xdotool against Xvfb on Linux).
func TestLive_Input(t *testing.T) {
	desktopLiveOrSkip(t)
	if os.Getenv("AIBUTLER_ENABLE_SYNTHETIC_INPUT") == "" {
		t.Skip("set AIBUTLER_ENABLE_SYNTHETIC_INPUT=1 to run live input tests")
	}
	c := desktop.NewController()
	c.SetTimeout(20 * time.Second)
	c.EnableInput(true)
	ctx := context.Background()

	if err := c.Click(ctx, 5, 5); err != nil {
		t.Errorf("Click: %v", err)
	}
	if err := c.TypeText(ctx, "hello world 123"); err != nil {
		t.Errorf("TypeText: %v", err)
	}
	for _, k := range []string{"tab", "space", "return", "escape", "up", "down"} {
		if err := c.KeyPress(ctx, k); err != nil {
			t.Errorf("KeyPress(%q): %v", k, err)
		}
	}
}
