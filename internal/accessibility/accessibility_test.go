package accessibility_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/accessibility"
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
	if runtime.GOOS == "darwin" {
		// On macOS the call proceeds to osascript; it may fail for
		// environmental reasons (app not running, no a11y permission),
		// but it must NOT fail with allowlist/validation errors.
		if err != nil && strings.Contains(err.Error(), "not in allowlist") {
			t.Errorf("allowed app wrongly denied: %v", err)
		}
		return
	}
	// Non-darwin: passes validation, then hits the OS gate.
	if err == nil {
		t.Fatal("expected macOS-only error on non-darwin")
	}
	if !strings.Contains(err.Error(), "macOS") {
		t.Errorf("error = %v, want a macOS-only message", err)
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
