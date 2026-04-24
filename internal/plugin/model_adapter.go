package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// PluginModelAdapter wraps a WASM plugin's complete() export as an agent.ModelAdapter.
type PluginModelAdapter struct {
	runtime    Runtime
	pluginName string
	function   string // WASM export name (default: "complete")
	models     []string
}

// NewPluginModelAdapter creates a model adapter backed by a WASM plugin.
func NewPluginModelAdapter(runtime Runtime, pluginName, function string, models []string) *PluginModelAdapter {
	if function == "" {
		function = "complete"
	}
	return &PluginModelAdapter{
		runtime:    runtime,
		pluginName: pluginName,
		function:   function,
		models:     models,
	}
}

// Models returns the model names this adapter handles.
func (a *PluginModelAdapter) Models() []string {
	return a.models
}

// Complete sends messages to the plugin's complete function and returns the response.
func (a *PluginModelAdapter) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	input, err := json.Marshal(messages)
	if err != nil {
		return agent.Response{}, fmt.Errorf("marshal messages: %w", err)
	}

	output, err := a.runtime.Call(ctx, a.pluginName, a.function, input)
	if err != nil {
		return agent.Response{}, fmt.Errorf("plugin %s.%s: %w", a.pluginName, a.function, err)
	}

	var resp agent.Response
	if err := json.Unmarshal(output, &resp); err != nil {
		return agent.Response{}, fmt.Errorf("unmarshal response from %s.%s: %w", a.pluginName, a.function, err)
	}
	return resp, nil
}
