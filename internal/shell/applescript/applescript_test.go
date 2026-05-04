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
