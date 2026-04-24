package powershell_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/shell/powershell"
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

func TestAllowlist_Denied(t *testing.T) {
	exec := powershell.NewExecutor([]string{"Get-Date"})
	_, err := exec.Execute(context.Background(), "Remove-Item -Recurse /tmp/test")
	if err == nil {
		t.Fatal("expected error for non-allowlisted command")
	}
}

func TestAllowlist_Empty_DeniesAll(t *testing.T) {
	// Empty allowlist means no commands allowed.
	exec := powershell.NewExecutor([]string{})
	_, err := exec.Execute(context.Background(), "Write-Host hello")
	if err == nil {
		t.Fatal("expected error when allowlist is empty")
	}
}

func TestRegisterPowerShellTool(t *testing.T) {
	reg := newMockRegistry()
	exec := powershell.NewExecutor([]string{"Get-Date"})
	powershell.RegisterPowerShellTool(reg, exec)

	found := false
	for _, name := range reg.tools {
		if name == "shell.powershell" {
			found = true
		}
	}
	if !found {
		t.Error("shell.powershell tool was not registered")
	}
}

func TestExecuteTool_DeniedViaRegistry(t *testing.T) {
	reg := newMockRegistry()
	exec := powershell.NewExecutor([]string{"Get-Date"})
	powershell.RegisterPowerShellTool(reg, exec)

	psExec := reg.exec["shell.powershell"]
	if psExec == nil {
		t.Fatal("shell.powershell not registered")
	}

	_, err := psExec(context.Background(), `{"command":"Invoke-Expression evil"}`)
	if err == nil {
		t.Fatal("expected error for non-allowlisted command via tool exec")
	}
}
