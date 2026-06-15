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

// --- Unicode case-fold bypass regression test (v0.4.2 hardening) ---

// TestBypass_UnicodeCaseFold confirms that a Unicode confusable cannot
// spoof an allowlisted shortcut name. Go's strings.EqualFold folds
// U+212A (KELVIN SIGN) to ASCII 'k', so the old matcher accepted
// "Bac<U+212A>up" as "Backup" while `shortcuts run` would resolve a
// different shortcut. ASCII-only folding closes this. (Confirmed HIGH.)
func TestBypass_UnicodeCaseFold(t *testing.T) {
	r := shortcuts.NewRunner([]string{"Backup"})

	kelvin := "BacKup" // third-from-last rune is KELVIN SIGN, not ASCII 'k'
	if r.InAllowlist(kelvin) {
		t.Errorf("unicode case-fold bypass: %q (with U+212A) spoofed the allowlisted %q", kelvin, "Backup")
	}

	// Ordinary ASCII case-insensitivity must still work.
	for _, ok := range []string{"Backup", "backup", "BACKUP", "BackUp"} {
		if !r.InAllowlist(ok) {
			t.Errorf("ASCII case variant %q should match allowlisted 'Backup'", ok)
		}
	}
	// A genuinely different name is still denied.
	if r.InAllowlist("Restore") {
		t.Error("unrelated name 'Restore' should be denied")
	}
}
