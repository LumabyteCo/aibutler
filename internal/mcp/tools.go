package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterMCPTools bridges all discovered MCP tools into the local tool registry.
// It also stores the registry reference in the client so that Reconnect can
// unregister stale tools and re-register fresh ones.
func RegisterMCPTools(registry *tool.Registry, client *Client) {
	// Store registry reference for reconnect-time re-registration.
	client.SetRegistry(registry)

	// Reconnect drops the server's stale tools; this puts the fresh set back.
	client.SetReRegisterHook(func(serverName string) {
		registerServerTools(registry, client, serverName)
	})

	// Register individual server tools as mcp.<server>.<tool>.
	for _, serverName := range client.Servers() {
		registerServerTools(registry, client, serverName)
	}

	// Register the dynamic call tool.
	registry.Register(&mcpCallTool{client: client})
}

// registerServerTools registers all tools from a single MCP server.
func registerServerTools(registry *tool.Registry, client *Client, serverName string) {
	tools, err := client.Tools(serverName)
	if err != nil {
		return
	}
	for _, ti := range tools {
		registry.Register(&mcpTool{
			client:     client,
			serverName: serverName,
			info:       ti,
		})
	}
}

// ReRegisterServerTools refreshes tool schemas from a server and re-registers them.
// Call this after Reconnect to pick up added/removed/renamed tools.
func ReRegisterServerTools(registry *tool.Registry, client *Client, serverName string) error {
	// Remove old tools for this server.
	registry.UnregisterPrefix("mcp." + serverName + ".")

	// Re-register with fresh tool list.
	registerServerTools(registry, client, serverName)
	return nil
}

// mcpTool wraps a remote MCP tool as a local tool.Tool.
type mcpTool struct {
	client     *Client
	serverName string
	info       ToolInfo
}

func (t *mcpTool) Name() string {
	return fmt.Sprintf("mcp.%s.%s", t.serverName, t.info.Name)
}

func (t *mcpTool) Description() string { return t.info.Description }
func (t *mcpTool) Capability() string  { return "mcp.call" }

func (t *mcpTool) Schema() string {
	if len(t.info.InputSchema) > 0 {
		return string(t.info.InputSchema)
	}
	return `{"type": "object"}`
}

func (t *mcpTool) Execute(ctx context.Context, input string) (string, error) {
	result, err := t.client.Call(ctx, t.serverName, t.info.Name, json.RawMessage(input))
	if err != nil {
		return "", err
	}
	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", result.AgentText())
	}
	return result.AgentText(), nil
}

// mcpCallTool is a generic dynamic invocation tool.
type mcpCallTool struct {
	client *Client
}

type mcpCallInput struct {
	Server string          `json:"server"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
}

func (t *mcpCallTool) Name() string        { return "mcp.call" }
func (t *mcpCallTool) Description() string { return "Call any tool on a connected MCP server." }
func (t *mcpCallTool) Capability() string  { return "mcp.call" }

func (t *mcpCallTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"server": {"type": "string", "description": "MCP server name"},
			"tool":   {"type": "string", "description": "Tool name on the server"},
			"args":   {"type": "object", "description": "Tool arguments"}
		},
		"required": ["server", "tool"]
	}`
}

func (t *mcpCallTool) Execute(ctx context.Context, input string) (string, error) {
	var in mcpCallInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("mcp.call: %w", err)
	}

	args := in.Args
	if args == nil {
		args = json.RawMessage("{}")
	}

	result, err := t.client.Call(ctx, in.Server, in.Tool, args)
	if err != nil {
		return "", err
	}
	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", result.AgentText())
	}
	return result.AgentText(), nil
}
