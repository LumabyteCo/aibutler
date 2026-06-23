package accessibility_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/accessibility"
	"github.com/godbus/dbus/v5"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestReadUI_EmptyApp(t *testing.T) {
	r := accessibility.NewReader([]string{"Mail"})
	if _, err := r.ReadUI(context.Background(), "", 1); err == nil {
		t.Error("expected error for empty app name")
	}
}

func TestReadUI_NotInAllowlist(t *testing.T) {
	r := accessibility.NewReader([]string{"Mail"})
	_, err := r.ReadUI(context.Background(), "Terminal", 1)
	if err == nil {
		t.Fatal("expected denial for app not in allowlist")
	}
	if !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("error = %v, want 'not in allowlist'", err)
	}
}

func TestReadUI_EmptyAllowlist_DeniesAll(t *testing.T) {
	r := accessibility.NewReader(nil)
	if _, err := r.ReadUI(context.Background(), "Mail", 1); err == nil {
		t.Error("empty allowlist should deny everything")
	}
}

// TestReadUI_InjectionRejected ensures an app name that could break out
// of the AppleScript string literal is refused — BEFORE the allowlist
// check, so even an allowlisted-looking name with quotes is blocked.
func TestReadUI_InjectionRejected(t *testing.T) {
	r := accessibility.NewReader([]string{`Mail" to do shell script "id`})
	for _, bad := range []string{
		`Mail" to do shell script "id`,
		"Mail\ntell",
		`Mail\`,
	} {
		_, err := r.ReadUI(context.Background(), bad, 1)
		if err == nil {
			t.Errorf("expected rejection for injection-y app name %q", bad)
		}
	}
}

func TestReadUI_AllowedApp_OSGate(t *testing.T) {
	r := accessibility.NewReader([]string{"Mail"})
	_, err := r.ReadUI(context.Background(), "Mail", 2)

	// macOS, Linux/FreeBSD, and Windows each have a real backend. The call
	// passes validation + allowlist and dispatches to it. It will usually
	// fail for environmental reasons (app not running, no a11y bus /
	// permission), but it must NEVER fail with an allowlist, validation, or
	// unsupported-OS error — those would mean the gate or dispatch is wrong.
	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "windows":
		if err == nil {
			return
		}
		for _, gate := range []string{"not in allowlist", "illegal characters", "unsupported OS"} {
			if strings.Contains(err.Error(), gate) {
				t.Errorf("allowed app on %s hit a gate error (%q): %v", runtime.GOOS, gate, err)
			}
		}
	default:
		// Genuinely unsupported OS: must be refused at the dispatch gate.
		if err == nil || !strings.Contains(err.Error(), "unsupported OS") {
			t.Errorf("expected unsupported-OS error on %s, got: %v", runtime.GOOS, err)
		}
	}
}

// TestBuildMacScript_InterpolatesSafely verifies the generated script
// embeds the app name and respects the depth bound, and that depth
// produces nested repeat loops.
func TestBuildMacScript_InterpolatesSafely(t *testing.T) {
	s := accessibility.BuildMacScript("Mail", 2)
	if !strings.Contains(s, `process "Mail"`) {
		t.Errorf("script should target process \"Mail\"; got:\n%s", s)
	}
	// Depth 2 → two nested repeat loops (level 0 and level 1).
	if got := strings.Count(s, "repeat with e0"); got != 1 {
		t.Errorf("expected 1 level-0 loop, got %d", got)
	}
	if got := strings.Count(s, "repeat with e1"); got != 1 {
		t.Errorf("expected 1 level-1 loop, got %d", got)
	}
	// Depth 1 → only the level-0 loop, no level-1.
	s1 := accessibility.BuildMacScript("Mail", 1)
	if strings.Contains(s1, "repeat with e1") {
		t.Error("depth=1 should not produce a level-1 loop")
	}
}

// TestWinUIAScript_Shape verifies the Windows UIAutomation script is
// well-formed: it loads the UIAutomation assemblies, takes the app name and
// depth as positional args (never interpolated — so there is no injection
// surface), and emits real tab characters via [char]9 rather than a
// PowerShell backtick-escape that a Go raw string can't carry.
func TestWinUIAScript_Shape(t *testing.T) {
	s := accessibility.WinUIAScript
	for _, want := range []string{
		"Add-Type -AssemblyName UIAutomationClient",
		"Add-Type -AssemblyName UIAutomationTypes",
		"$args[0]",      // app name passed positionally
		"$args[1]",      // depth passed positionally
		"[char]9",       // tab separator (no backtick-escape)
		"FromHandle",    // walks the main-window element tree
		"ProgrammaticName",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("winUIAScript missing %q", want)
		}
	}
	// The app name must never be baked into the script body.
	if strings.Contains(s, "Mail") {
		t.Error("winUIAScript should not interpolate any app name")
	}
	// A stray literal backtick would mean the raw-string/escape juggling
	// regressed (PowerShell backtick-escapes can't live in a Go raw string).
	if strings.Contains(s, "`") {
		t.Error("winUIAScript should contain no backticks (use [char]9 for tab)")
	}
}

func TestResolvePowerShell_NonEmpty(t *testing.T) {
	if got := accessibility.ResolvePowerShell(); got == "" {
		t.Error("resolvePowerShell returned empty string")
	}
}

// TestVariantString covers the Linux AT-SPI property-variant formatter:
// strings pass through, numbers drop trailing zeros, and a zero/absent
// value renders empty so the value column stays clean.
func TestVariantString(t *testing.T) {
	cases := []struct {
		name string
		in   dbus.Variant
		want string
	}{
		{"string", dbus.MakeVariant("hello"), "hello"},
		{"empty string", dbus.MakeVariant(""), ""},
		{"int float", dbus.MakeVariant(float64(42)), "42"},
		{"fractional", dbus.MakeVariant(float64(3.5)), "3.5"},
		{"zero float", dbus.MakeVariant(float64(0)), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := accessibility.VariantString(tc.in); got != tc.want {
				t.Errorf("VariantString(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRegisterAccessibilityTool(t *testing.T) {
	reg := newMockRegistry()
	accessibility.RegisterAccessibilityTool(reg, accessibility.NewReader([]string{"Mail"}))
	if _, ok := reg.exec["accessibility.read_ui"]; !ok {
		t.Error("accessibility.read_ui not registered")
	}
}

func TestTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	accessibility.RegisterAccessibilityTool(reg, accessibility.NewReader([]string{"Mail"}))
	_, err := reg.exec["accessibility.read_ui"](context.Background(), "{not json")
	if err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

func TestTool_DeniedViaRegistry(t *testing.T) {
	reg := newMockRegistry()
	accessibility.RegisterAccessibilityTool(reg, accessibility.NewReader([]string{"Mail"}))
	// "Terminal" not allowlisted → denial surfaces through the tool.
	_, err := reg.exec["accessibility.read_ui"](context.Background(), `{"app":"Terminal"}`)
	if err == nil {
		t.Error("expected denial for non-allowlisted app via registry")
	}
}
