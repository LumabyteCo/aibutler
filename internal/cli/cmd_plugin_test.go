package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin"
	"github.com/LumabyteCo/aibutler/internal/plugin/registry"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// mockToolRegistry for CLI tests.
type pluginTestToolReg struct {
	registered []string
}

func (m *pluginTestToolReg) Register(t registry.ToolLike) {
	m.registered = append(m.registered, t.Name())
}
func (m *pluginTestToolReg) UnregisterPrefix(_ string) {}

type pluginTestAuditor struct{}

func (m *pluginTestAuditor) WriteAudit(_ context.Context, _ int64, _, _, _ string) error {
	return nil
}

func writePluginFixture(t *testing.T, dir, name string) string {
	t.Helper()
	pluginDir := filepath.Join(dir, name)
	os.MkdirAll(pluginDir, 0755)

	toml := `name = "` + name + `"
version = "1.0.0"
wasm_path = "plugin.wasm"
capabilities = ["tool.call"]

[[tools]]
name = "greet"
description = "Say hello"
schema = '{"type":"object"}'
`
	os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte(toml), 0644)
	os.WriteFile(filepath.Join(pluginDir, "plugin.wasm"), []byte("stub"), 0644)
	return filepath.Join(pluginDir, "plugin.toml")
}

func setupPluginCLI(t *testing.T) (*registry.Registry, string) {
	t.Helper()
	database := testutil.TestDB(t)
	rt := plugin.NewMockRuntime()
	toolReg := &pluginTestToolReg{}
	pluginDir := t.TempDir()

	reg := registry.New(database.Conn(), rt, toolReg, nil, nil, &pluginTestAuditor{}, pluginDir, 0)
	return reg, pluginDir
}

func TestCmdPluginNoArgs(t *testing.T) {
	reg, _ := setupPluginCLI(t)
	var buf bytes.Buffer
	err := CmdPlugin(reg, nil, &buf)
	if err == nil {
		t.Error("expected usage error")
	}
}

func TestCmdPluginUnknownSubcommand(t *testing.T) {
	reg, _ := setupPluginCLI(t)
	var buf bytes.Buffer
	err := CmdPlugin(reg, []string{"bogus"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("err = %v, want unknown subcommand error", err)
	}
}

func TestCmdPluginInstall(t *testing.T) {
	reg, pluginDir := setupPluginCLI(t)
	manifestPath := writePluginFixture(t, pluginDir, "test-plugin")

	var buf bytes.Buffer
	if err := CmdPlugin(reg, []string{"install", manifestPath}, &buf); err != nil {
		t.Fatalf("install: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "test-plugin") {
		t.Errorf("output missing plugin name: %s", out)
	}
	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("output missing version: %s", out)
	}
}

func TestCmdPluginList(t *testing.T) {
	reg, pluginDir := setupPluginCLI(t)
	ctx := context.Background()

	// Install two plugins.
	path1 := writePluginFixture(t, pluginDir, "alpha")
	path2 := writePluginFixture(t, pluginDir, "beta")
	reg.Install(ctx, path1)
	reg.Install(ctx, path2)

	var buf bytes.Buffer
	if err := CmdPlugin(reg, []string{"list"}, &buf); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("list output: %s", out)
	}
}

func TestCmdPluginListEmpty(t *testing.T) {
	reg, _ := setupPluginCLI(t)
	var buf bytes.Buffer
	if err := CmdPlugin(reg, []string{"list"}, &buf); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(buf.String(), "No plugins") {
		t.Errorf("expected 'No plugins' message, got: %s", buf.String())
	}
}

func TestCmdPluginEnableDisable(t *testing.T) {
	reg, pluginDir := setupPluginCLI(t)
	ctx := context.Background()

	manifestPath := writePluginFixture(t, pluginDir, "toggle")
	reg.Install(ctx, manifestPath)

	var buf bytes.Buffer
	if err := CmdPlugin(reg, []string{"enable", "toggle"}, &buf); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !strings.Contains(buf.String(), "Enabled") {
		t.Errorf("output: %s", buf.String())
	}

	buf.Reset()
	if err := CmdPlugin(reg, []string{"disable", "toggle"}, &buf); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !strings.Contains(buf.String(), "Disabled") {
		t.Errorf("output: %s", buf.String())
	}
}

func TestCmdPluginRemove(t *testing.T) {
	reg, pluginDir := setupPluginCLI(t)
	ctx := context.Background()

	manifestPath := writePluginFixture(t, pluginDir, "removeme")
	reg.Install(ctx, manifestPath)

	var buf bytes.Buffer
	if err := CmdPlugin(reg, []string{"remove", "removeme"}, &buf); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed") {
		t.Errorf("output: %s", buf.String())
	}
}

func TestCmdPluginInfo(t *testing.T) {
	reg, pluginDir := setupPluginCLI(t)
	ctx := context.Background()

	manifestPath := writePluginFixture(t, pluginDir, "info-plugin")
	reg.Install(ctx, manifestPath)

	var buf bytes.Buffer
	if err := CmdPlugin(reg, []string{"info", "info-plugin"}, &buf); err != nil {
		t.Fatalf("info: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "info-plugin") {
		t.Errorf("output missing name: %s", out)
	}
	if !strings.Contains(out, "tool.call") {
		t.Errorf("output missing capabilities: %s", out)
	}
}

func TestCmdPluginInfoNotFound(t *testing.T) {
	reg, _ := setupPluginCLI(t)
	var buf bytes.Buffer
	err := CmdPlugin(reg, []string{"info", "nonexistent"}, &buf)
	if err == nil {
		t.Error("expected error for nonexistent plugin info")
	}
}

func TestCmdPluginLs(t *testing.T) {
	reg, _ := setupPluginCLI(t)
	var buf bytes.Buffer
	// "ls" should be an alias for "list".
	if err := CmdPlugin(reg, []string{"ls"}, &buf); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(buf.String(), "No plugins") {
		t.Errorf("expected 'No plugins' from ls alias: %s", buf.String())
	}
}

func TestToolCallerAdapterCallsRegisteredTool(t *testing.T) {
	reg := tool.NewRegistry()
	called := false
	reg.Register(&tool.FuncTool{
		ToolName:   "test.echo",
		ToolDesc:   "Echo",
		ToolSchema: `{}`,
		ToolCap:    "",
		Exec: func(_ context.Context, input string) (string, error) {
			called = true
			return "echo:" + input, nil
		},
	})

	adapter := &toolCallerAdapter{reg: reg}
	result, err := adapter.CallTool(context.Background(), "test.echo", "hello")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !called {
		t.Error("tool not called")
	}
	if result != "echo:hello" {
		t.Errorf("result = %q", result)
	}
}

func TestToolCallerAdapterRejectsUnknownTool(t *testing.T) {
	reg := tool.NewRegistry()
	adapter := &toolCallerAdapter{reg: reg}
	_, err := adapter.CallTool(context.Background(), "nope", "{}")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}
