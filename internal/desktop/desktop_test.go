package desktop_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/desktop"
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

// TestInputDisabledByDefault: synthetic input must be OFF until
// explicitly enabled, even before the OS gate — the enable flag is the
// second, independent guard on the highest-risk capability.
func TestInputDisabledByDefault(t *testing.T) {
	c := desktop.NewController()
	if c.InputEnabled() {
		t.Fatal("input should be disabled by default")
	}
	// On macOS the OS gate passes, so the disabled-flag is what blocks.
	// On non-macOS the OS gate blocks first; either way it's an error.
	for _, call := range []func() error{
		func() error { return c.Click(context.Background(), 10, 10) },
		func() error { return c.TypeText(context.Background(), "hi") },
		func() error { return c.KeyPress(context.Background(), "return") },
	} {
		if err := call(); err == nil {
			t.Error("input action succeeded while input disabled")
		}
	}
}

// TestInputEnabled_OSGate: once enabled, non-macOS still blocks at the
// OS gate with a clear message; macOS proceeds to osascript.
func TestInputEnabled_OSGate(t *testing.T) {
	c := desktop.NewController()
	c.EnableInput(true)
	err := c.KeyPress(context.Background(), "return")
	if runtime.GOOS != "darwin" {
		if err == nil || !strings.Contains(err.Error(), "macOS") {
			t.Errorf("non-macOS should report a macOS-only error, got: %v", err)
		}
	}
	// On darwin this may succeed or fail on permissions; not asserted.
}

func TestKeyPress_UnknownKey(t *testing.T) {
	c := desktop.NewController()
	c.EnableInput(true)
	err := c.KeyPress(context.Background(), "f13-nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	// On macOS the unknown-key error; on non-macOS the OS gate fires
	// first. Accept either, but it must be an error.
}

func TestTypeText_RejectsNewlines(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("newline rejection happens after the macOS OS gate")
	}
	c := desktop.NewController()
	c.EnableInput(true)
	err := c.TypeText(context.Background(), "line1\nline2")
	if err == nil || !strings.Contains(err.Error(), "newline") {
		t.Errorf("expected newline-rejection error, got: %v", err)
	}
}

func TestScreenshot_OSGate(t *testing.T) {
	c := desktop.NewController()
	_, err := c.Screenshot(context.Background())
	if runtime.GOOS != "darwin" {
		if err == nil || !strings.Contains(err.Error(), "macOS") {
			t.Errorf("non-macOS screenshot should report macOS-only, got: %v", err)
		}
	}
	// On darwin it may capture or fail on permission; not asserted here.
}

func TestRegisterDesktopTools(t *testing.T) {
	reg := newMockRegistry()
	desktop.RegisterDesktopTools(reg, desktop.NewController())
	for _, want := range []string{"screen.capture", "input.click", "input.type", "input.key"} {
		if _, ok := reg.exec[want]; !ok {
			t.Errorf("tool %q not registered", want)
		}
	}
}

func TestInputTools_DeniedViaRegistry_WhenDisabled(t *testing.T) {
	reg := newMockRegistry()
	desktop.RegisterDesktopTools(reg, desktop.NewController()) // input disabled
	_, err := reg.exec["input.type"](context.Background(), `{"text":"x"}`)
	if err == nil {
		t.Error("input.type should fail through the registry while input is disabled")
	}
}
