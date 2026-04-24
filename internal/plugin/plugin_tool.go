package plugin

import (
	"context"

	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

// PluginTool wraps a WASM plugin export as a tool.Tool-compatible implementation.
// Registered as "plugin.<pluginName>.<toolName>" in the tool registry.
type PluginTool struct {
	runtime    Runtime
	pluginName string
	toolDef    manifest.ToolDef
}

// NewPluginTool creates a tool wrapper for a plugin export.
func NewPluginTool(runtime Runtime, pluginName string, def manifest.ToolDef) *PluginTool {
	return &PluginTool{
		runtime:    runtime,
		pluginName: pluginName,
		toolDef:    def,
	}
}

// Name returns the fully-qualified tool name: plugin.<pluginName>.<toolName>.
func (t *PluginTool) Name() string {
	return "plugin." + t.pluginName + "." + t.toolDef.Name
}

// Description returns the tool description from the manifest.
func (t *PluginTool) Description() string {
	return t.toolDef.Description
}

// Schema returns the JSON Schema for the tool input.
func (t *PluginTool) Schema() string {
	return t.toolDef.Schema
}

// Capability returns the required capability resource.
func (t *PluginTool) Capability() string {
	return "plugin." + t.pluginName + ".call"
}

// Execute invokes the WASM function and returns the result.
func (t *PluginTool) Execute(ctx context.Context, input string) (string, error) {
	fn := t.toolDef.Function
	if fn == "" {
		fn = t.toolDef.Name
	}
	out, err := t.runtime.Call(ctx, t.pluginName, fn, []byte(input))
	if err != nil {
		return "", err
	}
	return string(out), nil
}
