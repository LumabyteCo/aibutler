package clipboard

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
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

func TestRegisterTools(t *testing.T) {
	reg := newMockRegistry()
	RegisterTools(reg, NewClient())

	wantTools := []string{"clipboard.read", "clipboard.write"}
	for _, want := range wantTools {
		found := false
		for _, name := range reg.tools {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q to be registered", want)
		}
	}
}

func TestWriteTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	RegisterTools(reg, NewClient())

	w := reg.exec["clipboard.write"]
	if w == nil {
		t.Fatal("clipboard.write not registered")
	}
	_, err := w(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON input")
	}
}

func TestReadCommand_BackendMissing_ClearError(t *testing.T) {
	c := NewClient()
	// Force LookPath to fail for every binary.
	c.commandFinder = func(_ string) (string, error) {
		return "", errors.New("not found")
	}

	_, _, err := c.readCommand()
	if err == nil {
		t.Fatal("expected error when backend command not found")
	}
	// Error must mention how to fix it (e.g. install wl-clipboard / xclip on Linux,
	// or that pbpaste/Get-Clipboard is the macOS/Windows built-in).
	msg := err.Error()
	if runtime.GOOS == "linux" && !strings.Contains(msg, "wl-clipboard") && !strings.Contains(msg, "xclip") {
		t.Errorf("Linux read-error should mention wl-clipboard or xclip, got: %s", msg)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(msg, "pbpaste") {
		t.Errorf("macOS read-error should mention pbpaste, got: %s", msg)
	}
}

func TestWriteCommand_BackendMissing_ClearError(t *testing.T) {
	c := NewClient()
	c.commandFinder = func(_ string) (string, error) {
		return "", errors.New("not found")
	}
	_, _, err := c.writeCommand()
	if err == nil {
		t.Fatal("expected error when backend command not found")
	}
	if runtime.GOOS == "linux" && !strings.Contains(err.Error(), "wl-clipboard") && !strings.Contains(err.Error(), "xclip") {
		t.Errorf("Linux write-error should mention wl-clipboard or xclip, got: %s", err)
	}
}

func TestReadCommand_ReturnsBackend(t *testing.T) {
	// On macOS this test exercises the real PATH lookup.
	if runtime.GOOS != "darwin" {
		t.Skip("real backend lookup tested on darwin (always has pbpaste)")
	}
	c := NewClient()
	binary, _, err := c.readCommand()
	if err != nil {
		t.Fatalf("expected pbpaste to be discoverable on macOS: %v", err)
	}
	if !strings.Contains(binary, "pbpaste") {
		t.Errorf("expected binary path to contain 'pbpaste', got %q", binary)
	}
}

func TestRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("clipboard round-trip test requires real backends; gated to darwin")
	}
	c := NewClient()
	const probe = "ai-butler-clipboard-roundtrip-probe-9f3a"
	if err := c.Write(context.Background(), probe); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := c.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, probe) {
		t.Errorf("expected clipboard to contain %q, got %q", probe, out)
	}
}

func TestReadTruncation(t *testing.T) {
	// We can't easily test truncation without a real clipboard. This test
	// just documents the constant — if anyone changes it, this must update.
	if maxReadBytes != 64*1024 {
		t.Errorf("maxReadBytes changed unexpectedly: %d", maxReadBytes)
	}
}
