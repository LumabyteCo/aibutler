//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// ============================================================================
// Shell Execution Tools (4 tests)
// ============================================================================

func TestE2EShellExec(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithShell: true,
		Responses: []agent.Response{
			toolCallResponse("Running command.",
				tc("sh1", "shell.exec", `{"command":"echo hello"}`),
			),
			finalResponse("Command executed successfully."),
		},
	})

	p.sendMsg(t, "Run echo hello")

	// Verify model was called twice (tool call + final response).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains "hello".
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "hello") {
			found = true
			break
		}
	}
	if !found {
		t.Error("shell.exec tool result should contain 'hello'")
	}

	// Verify final response.
	resp := p.lastResponse(t)
	if resp != "Command executed successfully." {
		t.Errorf("response = %q, want %q", resp, "Command executed successfully.")
	}
}

func TestE2EShellExecDenied(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithShell: true,
		Responses: []agent.Response{
			toolCallResponse("Let me try.",
				tc("sh1", "shell.exec", `{"command":"rm -rf /"}`),
			),
			finalResponse("That command is not allowed."),
		},
	})

	p.sendMsg(t, "Delete everything")

	// Verify model was called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains the allowlist error.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "not in allowlist") {
			found = true
			break
		}
	}
	if !found {
		t.Error("shell.exec tool result should contain 'not in allowlist' error")
	}

	// Verify the model responded gracefully.
	resp := p.lastResponse(t)
	if resp != "That command is not allowed." {
		t.Errorf("response = %q, want %q", resp, "That command is not allowed.")
	}
}

func TestE2EShellExecEmptyCommand(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithShell: true,
		Responses: []agent.Response{
			toolCallResponse("Running.",
				tc("sh1", "shell.exec", `{"command":""}`),
			),
			finalResponse("Need a command."),
		},
	})

	p.sendMsg(t, "Run nothing")

	// Verify model was called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains the empty-command error.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "command is required") {
			found = true
			break
		}
	}
	if !found {
		t.Error("shell.exec tool result should contain 'command is required' error")
	}

	// Verify final response.
	resp := p.lastResponse(t)
	if resp != "Need a command." {
		t.Errorf("response = %q, want %q", resp, "Need a command.")
	}
}

func TestE2EShellExecPrintf(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithShell: true,
		Responses: []agent.Response{
			toolCallResponse("Printing lines.",
				tc("sh1", "shell.exec", `{"command":"printf 'line1\nline2'"}`),
			),
			finalResponse("Lines printed."),
		},
	})

	p.sendMsg(t, "Print two lines")

	// Verify model was called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains both lines.
	calls := p.Fake.Calls()
	hasLine1 := false
	hasLine2 := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" {
			if strings.Contains(msg.Content, "line1") {
				hasLine1 = true
			}
			if strings.Contains(msg.Content, "line2") {
				hasLine2 = true
			}
		}
	}
	if !hasLine1 {
		t.Error("shell.exec tool result should contain 'line1'")
	}
	if !hasLine2 {
		t.Error("shell.exec tool result should contain 'line2'")
	}

	// Verify final response.
	resp := p.lastResponse(t)
	if resp != "Lines printed." {
		t.Errorf("response = %q, want %q", resp, "Lines printed.")
	}
}
