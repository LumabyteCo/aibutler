package dispatch

import (
	"context"
	"runtime"
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

func TestDispatch_RoutesToCurrentGOOS(t *testing.T) {
	d := New()
	captured := ""
	d.SetHandler(runtime.GOOS, func(_ context.Context, input string) (string, error) {
		captured = input
		return "ok", nil
	})

	// Build a JSON object that includes the current GOOS plus a foreign one.
	payload := `{"` + runtime.GOOS + `":{"x":1},"freebsd":{"y":2}}`
	out, err := d.Dispatch(context.Background(), payload)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if out != "ok" {
		t.Fatalf("expected handler output 'ok', got %q", out)
	}
	if captured != `{"x":1}` {
		t.Errorf("expected handler to receive only its OS payload, got %q", captured)
	}
}

func TestDispatch_NoHandlerForCurrentGOOS(t *testing.T) {
	d := New()
	d.SetHandler("freebsd", func(_ context.Context, _ string) (string, error) {
		return "ok", nil
	})

	// Payload is for the current GOOS, but no handler is registered for it.
	payload := `{"` + runtime.GOOS + `":{"x":1}}`
	_, err := d.Dispatch(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error when no handler is registered for current GOOS")
	}
}

func TestDispatch_NoPayloadForCurrentGOOS(t *testing.T) {
	d := New()
	d.SetHandler(runtime.GOOS, func(_ context.Context, _ string) (string, error) {
		return "ok", nil
	})

	// Payload only includes a foreign OS — current GOOS missing.
	other := "freebsd"
	if runtime.GOOS == "freebsd" {
		other = "darwin"
	}
	payload := `{"` + other + `":{"x":1}}`
	_, err := d.Dispatch(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error when payload doesn't include current GOOS")
	}
}

func TestDispatch_EmptyPayload(t *testing.T) {
	d := New()
	d.SetHandler(runtime.GOOS, func(_ context.Context, _ string) (string, error) {
		return "ok", nil
	})
	_, err := d.Dispatch(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestDispatch_InvalidJSON(t *testing.T) {
	d := New()
	_, err := d.Dispatch(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSetHandler_NilRemoves(t *testing.T) {
	d := New()
	d.SetHandler("darwin", func(_ context.Context, _ string) (string, error) { return "", nil })
	d.SetHandler("darwin", nil)

	got := d.AvailableHandlers()
	for _, k := range got {
		if k == "darwin" {
			t.Fatal("expected darwin handler to be removed when set to nil")
		}
	}
}

func TestAvailableHandlers_Sorted(t *testing.T) {
	d := New()
	d.SetHandler("windows", func(_ context.Context, _ string) (string, error) { return "", nil })
	d.SetHandler("darwin", func(_ context.Context, _ string) (string, error) { return "", nil })
	d.SetHandler("linux", func(_ context.Context, _ string) (string, error) { return "", nil })

	got := d.AvailableHandlers()
	want := []string{"darwin", "linux", "windows"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegisterDispatchTool(t *testing.T) {
	reg := newMockRegistry()
	d := New()
	d.SetHandler(runtime.GOOS, func(_ context.Context, _ string) (string, error) { return "ok", nil })
	RegisterDispatchTool(reg, d)

	found := false
	for _, name := range reg.tools {
		if name == "shell.script" {
			found = true
		}
	}
	if !found {
		t.Error("shell.script tool was not registered")
	}
}

func TestRegisteredTool_DispatchesCorrectly(t *testing.T) {
	reg := newMockRegistry()
	d := New()
	d.SetHandler(runtime.GOOS, func(_ context.Context, input string) (string, error) {
		return "got:" + input, nil
	})
	RegisterDispatchTool(reg, d)

	tool := reg.exec["shell.script"]
	if tool == nil {
		t.Fatal("shell.script not registered")
	}

	payload := `{"` + runtime.GOOS + `":{"k":"v"}}`
	out, err := tool(context.Background(), payload)
	if err != nil {
		t.Fatalf("tool exec failed: %v", err)
	}
	if out != `got:{"k":"v"}` {
		t.Errorf("unexpected output: %q", out)
	}
}
