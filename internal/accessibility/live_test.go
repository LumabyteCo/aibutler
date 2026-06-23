package accessibility_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/accessibility"
)

// This test exercises the REAL per-OS accessibility backend end-to-end
// against a live UI. It is gated behind AIBUTLER_ACCESSIBILITY_TESTS=1 so it
// never runs in normal (headless) CI. It is the on-device validation for the
// Linux AT-SPI backend:
//
//   - Linux: run inside a container with a private session D-Bus + Xvfb +
//     at-spi2-core, with an accessible GTK app (e.g. zenity) on screen:
//       dbus-run-session -- bash -c '
//         Xvfb :99 -screen 0 1024x768x24 & export DISPLAY=:99; sleep 2
//         zenity --info --title=ProbeWindow --text=hi &  sleep 4
//         AIBUTLER_ACCESSIBILITY_TESTS=1 AIBUTLER_A11Y_APP=zenity \
//           go test -run TestLive_ReadUI -v ./internal/accessibility/...'
//   - macOS: run directly with Accessibility permission granted, pointing
//     AIBUTLER_A11Y_APP at a running app (e.g. Finder).
//
// AIBUTLER_A11Y_APP names the on-screen application to inspect (default
// "zenity", what the Linux CI harness launches).

func TestLive_ReadUI(t *testing.T) {
	if os.Getenv("AIBUTLER_ACCESSIBILITY_TESTS") == "" {
		t.Skip("set AIBUTLER_ACCESSIBILITY_TESTS=1 (with a live a11y environment) to run accessibility live tests")
	}
	app := os.Getenv("AIBUTLER_A11Y_APP")
	if app == "" {
		app = "zenity"
	}

	r := accessibility.NewReader([]string{app})
	r.SetTimeout(20 * time.Second)

	tree, err := r.ReadUI(context.Background(), app, 4)
	if err != nil {
		t.Fatalf("ReadUI(%q): %v", app, err)
	}
	if strings.TrimSpace(tree) == "" {
		t.Fatalf("ReadUI(%q) returned an empty tree", app)
	}
	// The backend emits tab-delimited "<indent>role\tname\tvalue" lines; a
	// real walk produces at least one such row.
	if !strings.Contains(tree, "\t") {
		t.Errorf("tree is not tab-delimited — backend may not have walked anything:\n%s", tree)
	}
	t.Logf("ReadUI(%q) produced:\n%s", app, tree)
}
