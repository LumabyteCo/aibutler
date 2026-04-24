package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/prompt"
)

func TestHandleSlashCommand_Help(t *testing.T) {
	var buf bytes.Buffer
	compactor := prompt.NewCompactor(prompt.DefaultCompactorConfig())

	quit := handleSlashCommand(nil, "/help", &buf, "test-session", nil, compactor)

	if quit {
		t.Error("help should not cause quit")
	}

	output := buf.String()
	if !strings.Contains(output, "Available commands") {
		t.Errorf("help output missing 'Available commands': %s", output)
	}
	if !strings.Contains(output, "/quit") {
		t.Errorf("help output missing '/quit': %s", output)
	}
	if !strings.Contains(output, "/status") {
		t.Errorf("help output missing '/status': %s", output)
	}
}

func TestHandleSlashCommand_Status(t *testing.T) {
	var buf bytes.Buffer
	compactor := prompt.NewCompactor(prompt.DefaultCompactorConfig())

	messages := []agent.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	quit := handleSlashCommand(nil, "/status", &buf, "repl-123", messages, compactor)

	if quit {
		t.Error("status should not cause quit")
	}

	output := buf.String()
	if !strings.Contains(output, "repl-123") {
		t.Errorf("status output missing session ID: %s", output)
	}
	if !strings.Contains(output, "Messages: 3") {
		t.Errorf("status output missing message count: %s", output)
	}
}

func TestHandleSlashCommand_Quit(t *testing.T) {
	var buf bytes.Buffer
	compactor := prompt.NewCompactor(prompt.DefaultCompactorConfig())

	quit := handleSlashCommand(nil, "/quit", &buf, "test", nil, compactor)

	if !quit {
		t.Error("quit should return true")
	}

	output := buf.String()
	if !strings.Contains(output, "Goodbye") {
		t.Errorf("quit output missing 'Goodbye': %s", output)
	}
}

func TestHandleSlashCommand_Diff(t *testing.T) {
	var buf bytes.Buffer
	compactor := prompt.NewCompactor(prompt.DefaultCompactorConfig())

	messages := []agent.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Question 1"},
		{Role: "assistant", Content: "Answer 1"},
		{Role: "user", Content: "Question 2"},
		{Role: "assistant", Content: "Answer 2"},
	}

	quit := handleSlashCommand(nil, "/diff", &buf, "test", messages, compactor)

	if quit {
		t.Error("diff should not cause quit")
	}

	output := buf.String()
	if !strings.Contains(output, "User messages:      2") {
		t.Errorf("diff output missing correct user count: %s", output)
	}
	if !strings.Contains(output, "Assistant messages:  2") {
		t.Errorf("diff output missing correct assistant count: %s", output)
	}
	if !strings.Contains(output, "Total messages:     5") {
		t.Errorf("diff output missing correct total: %s", output)
	}
}

func TestResolveProviderName(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"claude-sonnet-4-6", "anthropic"},
		{"gpt-4o", "openai"},
		{"gemini-2.0-flash", "gemini"},
		{"grok-2", "xai"},
		{"llama3", "local"},
		{"", "local"},
	}

	for _, tt := range tests {
		got := resolveProviderName(tt.model)
		if got != tt.expected {
			t.Errorf("resolveProviderName(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}
