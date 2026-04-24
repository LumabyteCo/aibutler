package piper_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/voice/piper"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestAvailable_False_WhenBinaryMissing(t *testing.T) {
	e := piper.NewExecutor("/nonexistent/path/to/piper", "model.onnx")
	if e.Available() {
		t.Error("expected Available() to be false when binary is missing")
	}
}

func TestSynthesize_Error_WhenUnavailable(t *testing.T) {
	e := piper.NewExecutor("/nonexistent/path/to/piper", "model.onnx")
	_, err := e.Synthesize(context.Background(), "Hello")
	if err == nil {
		t.Fatal("expected error when binary is unavailable")
	}
}

func TestRegisterPiperTool(t *testing.T) {
	reg := newMockRegistry()
	e := piper.NewExecutor("/nonexistent/piper", "model.onnx")
	piper.RegisterPiperTool(reg, e)

	found := false
	for _, name := range reg.tools {
		if name == "voice.piper.synthesize" {
			found = true
		}
	}
	if !found {
		t.Error("voice.piper.synthesize was not registered")
	}
}
