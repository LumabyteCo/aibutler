package capability

import (
	"strings"
	"testing"
)

func TestRuleAllow(t *testing.T) {
	rules := []PermissionRule{
		{ToolPattern: "bash(git:*)", Action: "allow"},
	}
	p := NewInteractivePrompter(nil, nil, rules)
	p.SetMode(ModeFullAccess)

	allowed, err := p.ShouldAllow("bash", `{"command":"git status"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allow for bash with git input")
	}
}

func TestRuleDeny(t *testing.T) {
	rules := []PermissionRule{
		{ToolPattern: "shell.exec(rm:*)", Action: "deny"},
	}
	p := NewInteractivePrompter(nil, nil, rules)
	p.SetMode(ModeFullAccess)

	allowed, err := p.ShouldAllow("shell.exec", `{"command":"rm -rf /"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected deny for shell.exec with rm input")
	}
}

func TestInteractivePrompt(t *testing.T) {
	// Simulate user typing "y\n".
	input := strings.NewReader("y\n")
	var output strings.Builder

	p := NewInteractivePrompter(input, &output, nil)
	p.SetMode(ModeFullAccess)

	allowed, err := p.ShouldAllow("file.write", `{"path":"/tmp/test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allow after user typed 'y'")
	}
	if !strings.Contains(output.String(), "Allow tool") {
		t.Errorf("expected prompt message, got: %s", output.String())
	}
}

func TestModeEnforcement(t *testing.T) {
	p := NewInteractivePrompter(nil, nil, nil)

	// Read-only mode should deny write tools.
	p.SetMode(ModeReadOnly)
	allowed, err := p.ShouldAllow("task.add", `{"name":"test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("read-only mode should deny task.add")
	}

	// Read-only mode should allow read tools.
	allowed, err = p.ShouldAllow("task.list", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("read-only mode should allow task.list")
	}
}
