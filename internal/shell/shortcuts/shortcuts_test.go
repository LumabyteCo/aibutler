package shortcuts_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/shell/shortcuts"
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

func TestAllowlist_Denied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist gate tested on darwin; non-darwin gates earlier")
	}
	r := shortcuts.NewRunner([]string{"Open Project"})
	_, err := r.Run(context.Background(), "Drain Bank Account", "")
	if err == nil {
		t.Fatal("expected error for non-allowlisted shortcut name")
	}
}

func TestAllowlist_Empty_DeniesAll(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("allowlist gate tested on darwin; non-darwin gates earlier")
	}
	r := shortcuts.NewRunner([]string{})
	_, err := r.Run(context.Background(), "Anything", "")
	if err == nil {
		t.Fatal("expected error when allowlist is empty")
	}
}

func TestRun_NonDarwin_FailsLoudly(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin gate only fires off macOS")
	}
	r := shortcuts.NewRunner([]string{"X"})
	_, err := r.Run(context.Background(), "X", "")
	if err == nil {
		t.Fatal("expected error on non-darwin")
	}
}

func TestRun_EmptyName(t *testing.T) {
	r := shortcuts.NewRunner([]string{"X"})
	_, err := r.Run(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error when shortcut name is empty")
	}
}

func TestRegisterShortcutsTool(t *testing.T) {
	reg := newMockRegistry()
	r := shortcuts.NewRunner([]string{"X"})
	shortcuts.RegisterShortcutsTool(reg, r)

	found := false
	for _, name := range reg.tools {
		if name == "shell.shortcuts" {
			found = true
		}
	}
	if !found {
		t.Error("shell.shortcuts tool was not registered")
	}
}

func TestDefaultAllowlist_Empty(t *testing.T) {
	// Shortcuts have no shared system-wide defaults — every shortcut is
	// authored by the user. The function exists for API symmetry but
	// returns nil.
	defs := shortcuts.DefaultAllowlist()
	if len(defs) != 0 {
		t.Errorf("expected empty default allowlist for Shortcuts, got %v", defs)
	}
}

func TestExecuteTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	r := shortcuts.NewRunner([]string{"X"})
	shortcuts.RegisterShortcutsTool(reg, r)

	scExec := reg.exec["shell.shortcuts"]
	if scExec == nil {
		t.Fatal("shell.shortcuts not registered")
	}
	_, err := scExec(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON input")
	}
}
