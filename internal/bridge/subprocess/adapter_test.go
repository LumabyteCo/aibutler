package subprocess

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	adapter := New(Config{
		Command: "echo",
		Args:    []string{"{task}"},
		Timeout: 5 * time.Second,
	})

	output, err := adapter.Execute(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	got := strings.TrimSpace(output)
	if got != "hello world" {
		t.Errorf("output = %q, want %q", got, "hello world")
	}
}

func TestExecuteTimeout(t *testing.T) {
	adapter := New(Config{
		Command: "sleep",
		Args:    []string{"10"},
		Timeout: 100 * time.Millisecond,
	})

	_, err := adapter.Execute(context.Background(), "")
	if err == nil {
		t.Fatal("Execute: expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "signal") {
		t.Errorf("error = %v, want timeout-related error", err)
	}
}

func TestAvailable(t *testing.T) {
	// "echo" should be available on all platforms.
	adapter := New(Config{Command: "echo"})
	if !adapter.Available() {
		t.Error("Available: 'echo' should be available")
	}

	// Non-existent command should not be available.
	adapter2 := New(Config{Command: "nonexistent-command-xyz-abc-123"})
	if adapter2.Available() {
		t.Error("Available: non-existent command should not be available")
	}
}
