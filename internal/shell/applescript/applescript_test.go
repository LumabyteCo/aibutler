package applescript_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/shell/applescript"
)

// mockRegistry implements the narrow toolRegistry interface used by
// the applescript package — same pattern as powershell_test.go.
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

func TestAllowlist_Denied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// On non-Darwin we'd hit the GOOS-gate before the allowlist gate.
		// Test that a Darwin-targeted run rejects via allowlist.
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{"tell"})
	_, err := exec.Execute(context.Background(), `display dialog "hi"`, "")
	if err == nil {
		t.Fatal("expected error for non-allowlisted first-word")
	}
}

func TestAllowlist_Empty_DeniesAll(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{})
	_, err := exec.Execute(context.Background(), `tell application "Mail" to get count of messages in inbox`, "")
	if err == nil {
		t.Fatal("expected error when allowlist is empty")
	}
}

func TestExecute_NonDarwin_FailsLoudly(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin gate only fires off macOS")
	}
	exec := applescript.NewExecutor([]string{"tell"})
	_, err := exec.Execute(context.Background(), `tell application "Mail" to get count`, "")
	if err == nil {
		t.Fatal("expected error on non-darwin")
	}
}

func TestExecute_UnsupportedLanguage(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("language gate is checked after GOOS gate")
	}
	exec := applescript.NewExecutor([]string{"tell"})
	_, err := exec.Execute(context.Background(), `tell application "Mail" to get count`, "Python")
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
}

func TestRegisterAppleScriptTool(t *testing.T) {
	reg := newMockRegistry()
	exec := applescript.NewExecutor([]string{"tell"})
	applescript.RegisterAppleScriptTool(reg, exec)

	found := false
	for _, name := range reg.tools {
		if name == "shell.applescript" {
			found = true
		}
	}
	if !found {
		t.Error("shell.applescript tool was not registered")
	}
}

func TestExecuteTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	exec := applescript.NewExecutor([]string{"tell"})
	applescript.RegisterAppleScriptTool(reg, exec)

	asExec := reg.exec["shell.applescript"]
	if asExec == nil {
		t.Fatal("shell.applescript not registered")
	}
	_, err := asExec(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON input")
	}
}

func TestExecuteTool_MissingScript(t *testing.T) {
	reg := newMockRegistry()
	exec := applescript.NewExecutor([]string{"tell"})
	applescript.RegisterAppleScriptTool(reg, exec)

	asExec := reg.exec["shell.applescript"]
	_, err := asExec(context.Background(), `{"language":"AppleScript"}`)
	if err == nil {
		t.Fatal("expected error when script field is missing")
	}
}

func TestDefaultAllowlist_NotEmpty(t *testing.T) {
	defs := applescript.DefaultAllowlist()
	if len(defs) == 0 {
		t.Fatal("expected non-empty default allowlist for AppleScript")
	}
	// `tell` is the universal entry point — must be in defaults.
	found := false
	for _, d := range defs {
		if d == "tell" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'tell' in AppleScript defaults (it's the primary verb)")
	}
}

func TestDefaultAllowlist_PermitsCommonScripts(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("default-allowlist effect tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor(applescript.DefaultAllowlist())
	// A default-allowlisted script must pass the allowlist gate. The test
	// doesn't assert success of the actual osascript invocation (that's
	// environmental) — it only verifies the allowlist check is satisfied.
	_, err := exec.Execute(context.Background(), `display notification "hello"`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("default allowlist rejected a 'display' script: %v", err)
	}
}

func TestExecuteTool_DeniedViaRegistry(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	reg := newMockRegistry()
	exec := applescript.NewExecutor([]string{"tell"})
	applescript.RegisterAppleScriptTool(reg, exec)

	asExec := reg.exec["shell.applescript"]
	_, err := asExec(context.Background(), `{"script":"display dialog \"evil\""}`)
	if err == nil {
		t.Fatal("expected error for non-allowlisted command via tool exec")
	}
}

// --- Target-app allowlist tests ---

func TestAllowlist_TargetApp_ExactMatch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{"tell:Mail"})
	// In-allowlist: tell Mail.
	_, err := exec.Execute(context.Background(), `tell application "Mail" to get count of messages`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("tell:Mail entry rejected a Mail-targeted script: %v", err)
	}
	// Out-of-allowlist: tell Music.
	_, err = exec.Execute(context.Background(), `tell application "Music" to playpause`, "")
	if err == nil || !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("tell:Mail entry should have rejected a Music-targeted script, got: %v", err)
	}
}

func TestAllowlist_TargetApp_PrefixWildcard(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{"tell:Music*"})
	// In-allowlist: prefix matches.
	_, err := exec.Execute(context.Background(), `tell application "Music" to playpause`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("prefix wildcard rejected exact-prefix match: %v", err)
	}
	_, err = exec.Execute(context.Background(), `tell application "Music Pro" to playpause`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("prefix wildcard rejected prefix-wildcard match: %v", err)
	}
	// Out-of-allowlist: prefix doesn't match.
	_, err = exec.Execute(context.Background(), `tell application "Mail" to get count of messages`, "")
	if err == nil || !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("prefix wildcard should have rejected non-matching prefix, got: %v", err)
	}
}

func TestAllowlist_TargetApp_FullWildcard(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{"tell:*"})
	_, err := exec.Execute(context.Background(), `tell application "Anything" to do whatever`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("tell:* should permit any target, got: %v", err)
	}
}

func TestAllowlist_BareTell_PermitsAnyTarget(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{"tell"})
	// Bare `tell` keeps the original broad behaviour.
	_, err := exec.Execute(context.Background(), `tell application "Music" to playpause`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("bare tell entry rejected a tell-style script: %v", err)
	}
}

func TestAllowlist_TargetApp_ProcessForm(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	exec := applescript.NewExecutor([]string{"tell:System Events"})
	// `tell process "X"` — process variant.
	_, err := exec.Execute(context.Background(), `tell process "System Events" to get name`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("tell:System Events should permit `tell process \"System Events\"`, got: %v", err)
	}
}

func TestAllowlist_TargetApp_NoTargetExtractable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	// Allowlist requires target Mail. A tell script with no parseable target
	// (e.g. `tell me to ...`) does NOT match `tell:Mail`.
	exec := applescript.NewExecutor([]string{"tell:Mail"})
	_, err := exec.Execute(context.Background(), `tell me to do nothing`, "")
	if err == nil || !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("expected rejection when target can't be extracted, got: %v", err)
	}
}

func TestAllowlist_NonTellVerb_IgnoresTarget(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist-only path tested on darwin; non-darwin gates earlier")
	}
	// `display:foo` is a nonsense entry but the matcher should not
	// silently accept a `display`-prefixed script under it — non-tell
	// verbs don't carry a target, so the target component must be "*"
	// or empty for the bare-verb match to apply.
	exec := applescript.NewExecutor([]string{"display"})
	_, err := exec.Execute(context.Background(), `display notification "ok"`, "")
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("bare `display` should permit display scripts, got: %v", err)
	}
}
