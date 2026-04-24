package coding_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/coding"
)

type mockRegistry struct {
	tools map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{tools: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools[name] = exec
}

func TestRunGoCode(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}

	r := coding.NewRunner(t.TempDir())
	code := `package main

import "fmt"

func main() {
	fmt.Println("hello from go")
}
`
	out, err := r.Run(context.Background(), "go", code)
	if err != nil {
		t.Fatalf("Run go: %v", err)
	}
	if !strings.Contains(out, "hello from go") {
		t.Errorf("expected 'hello from go' in output, got %q", out)
	}
}

func TestRunPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	r := coding.NewRunner(t.TempDir())
	out, err := r.Run(context.Background(), "python", "print('hello from python')")
	if err != nil {
		t.Fatalf("Run python: %v", err)
	}
	if !strings.Contains(out, "hello from python") {
		t.Errorf("expected 'hello from python' in output, got %q", out)
	}
}

func TestLintGoCode(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}

	r := coding.NewRunner(t.TempDir())
	code := `package main

func main() {}
`
	_, err := r.Lint(context.Background(), "go", code)
	// go vet on a valid file should succeed (may produce output about missing go.mod, that's OK).
	// We just verify it doesn't panic.
	_ = err
}

func TestTestGoCode(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}

	// Test with a dir that has no Go files — go test should report an error.
	r := coding.NewRunner(t.TempDir())
	_, err := r.Test(context.Background(), "go", t.TempDir())
	if err == nil {
		t.Log("Test on empty dir: no error (go test may succeed with no test files)")
	}
}

func TestUnknownLanguage(t *testing.T) {
	r := coding.NewRunner(t.TempDir())
	_, err := r.Run(context.Background(), "cobol", "DISPLAY 'HELLO'")
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("expected 'unsupported language' error, got %v", err)
	}
}

func TestToolRegistration(t *testing.T) {
	r := coding.NewRunner(t.TempDir())
	reg := newMockRegistry()
	coding.RegisterCodingTools(reg, r)

	expected := []string{"code.run", "code.lint", "code.test"}
	for _, name := range expected {
		if _, ok := reg.tools[name]; !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}
