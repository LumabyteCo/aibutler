package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// ServerConfig describes how to connect to an MCP server.
type ServerConfig struct {
	Name      string
	Command   string
	Args      []string
	Env       map[string]string
	Transport string // "stdio" (default)
}

type serverConn struct {
	name      string
	transport Transport
	tools     []ToolInfo
	cfg       ServerConfig // retained for reconnection
}

// Client manages connections to one or more MCP servers.
type Client struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
	nextID  int

	// registry is set by RegisterMCPTools so that Reconnect can re-register tools.
	registry    ToolRegistry
	registrySet bool
}

// ToolRegistry is the subset of tool.Registry that the MCP client needs.
// This avoids an import cycle (mcp → tool → mcp).
type ToolRegistry interface {
	UnregisterPrefix(prefix string)
}

// NewClient creates an MCP client.
func NewClient() *Client {
	return &Client{servers: make(map[string]*serverConn)}
}

// SetRegistry stores a reference to the tool registry for reconnect-time re-registration.
func (c *Client) SetRegistry(r ToolRegistry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registry = r
	c.registrySet = true
}

// ConnectWithTransport connects using a provided transport (useful for testing).
func (c *Client) ConnectWithTransport(ctx context.Context, name string, t Transport) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.connectLocked(ctx, name, t, ServerConfig{Name: name})
}

// connectLocked performs the handshake and tool discovery while holding the write lock.
func (c *Client) connectLocked(ctx context.Context, name string, t Transport, cfg ServerConfig) error {
	// 1. Initialize handshake.
	initResp, err := t.Send(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "initialize",
		Params: InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo:      ClientInfo{Name: "aibutler", Version: "0.1.0"},
			Capabilities:    &MCPCaps{},
		},
	})
	if err != nil {
		return fmt.Errorf("mcp: initialize %s: %w", name, err)
	}
	if initResp.Error != nil {
		return fmt.Errorf("mcp: initialize %s: %s", name, initResp.Error.Message)
	}

	// 2. Discover tools.
	toolsResp, err := t.Send(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/list",
	})
	if err != nil {
		return fmt.Errorf("mcp: tools/list %s: %w", name, err)
	}
	if toolsResp.Error != nil {
		return fmt.Errorf("mcp: tools/list %s: %s", name, toolsResp.Error.Message)
	}

	var toolList ToolListResult
	if err := json.Unmarshal(toolsResp.Result, &toolList); err != nil {
		return fmt.Errorf("mcp: parse tools %s: %w", name, err)
	}

	c.servers[name] = &serverConn{
		name:      name,
		transport: t,
		tools:     toolList.Tools,
		cfg:       cfg,
	}

	return nil
}

// Connect starts a stdio transport and connects to a server.
func (c *Client) Connect(ctx context.Context, cfg ServerConfig) error {
	var envSlice []string
	for k, v := range cfg.Env {
		envSlice = append(envSlice, k+"="+v)
	}

	t, err := NewStdioTransport(cfg.Command, cfg.Args, envSlice)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectLocked(ctx, cfg.Name, t, cfg)
}

// Reconnect tears down a server connection and re-establishes it using the stored config.
// If a tool registry is set, stale tools are unregistered before reconnecting.
func (c *Client) Reconnect(ctx context.Context, serverName string) error {
	c.mu.Lock()
	conn, ok := c.servers[serverName]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("mcp: server %q not connected", serverName)
	}

	// Capture config, tear down old connection.
	cfg := conn.cfg
	conn.transport.Close()
	delete(c.servers, serverName)

	// Unregister stale tools while still holding the lock.
	if c.registrySet && c.registry != nil {
		c.registry.UnregisterPrefix("mcp." + serverName + ".")
	}
	c.mu.Unlock()

	// Re-connect (Connect acquires its own lock).
	return c.Connect(ctx, cfg)
}

// RefreshTools re-sends tools/list to a connected server and updates the cached tool list.
func (c *Client) RefreshTools(ctx context.Context, serverName string) ([]ToolInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, ok := c.servers[serverName]
	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", serverName)
	}

	resp, err := conn.transport.Send(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/list",
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: refresh tools %s: %w", serverName, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: refresh tools %s: %s", serverName, resp.Error.Message)
	}

	var toolList ToolListResult
	if err := json.Unmarshal(resp.Result, &toolList); err != nil {
		return nil, fmt.Errorf("mcp: parse refreshed tools %s: %w", serverName, err)
	}

	conn.tools = toolList.Tools
	return toolList.Tools, nil
}

// Disconnect closes the connection to a named server.
func (c *Client) Disconnect(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, ok := c.servers[name]
	if !ok {
		return fmt.Errorf("mcp: server %q not connected", name)
	}

	err := conn.transport.Close()
	delete(c.servers, name)
	return err
}

// Tools returns the tool list for a specific server.
func (c *Client) Tools(serverName string) ([]ToolInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	conn, ok := c.servers[serverName]
	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", serverName)
	}

	out := make([]ToolInfo, len(conn.tools))
	copy(out, conn.tools)
	return out, nil
}

// AllTools returns tools from all connected servers, prefixed with server name.
func (c *Client) AllTools() []ToolInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var all []ToolInfo
	for _, conn := range c.servers {
		all = append(all, conn.tools...)
	}
	return all
}

// Servers returns the names of all connected servers.
func (c *Client) Servers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.servers))
	for name := range c.servers {
		names = append(names, name)
	}
	return names
}

// defaultCallTimeout is applied when the parent context has no deadline.
const defaultCallTimeout = 60 * time.Second

// Call invokes a tool on a specific server. If the transport returns an error
// (e.g. EOF from a crashed server), it attempts one reconnect + retry.
func (c *Client) Call(ctx context.Context, serverName, toolName string, args json.RawMessage) (*ToolCallResult, error) {
	// Apply a default timeout if the caller's context is open-ended.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallTimeout)
		defer cancel()
	}

	result, err := c.callOnce(ctx, serverName, toolName, args)
	if err == nil {
		return result, nil
	}

	// Transport error → try one reconnect.
	log.Printf("mcp: call %s.%s failed (%v), attempting reconnect", serverName, toolName, err)
	if reconnErr := c.Reconnect(ctx, serverName); reconnErr != nil {
		return nil, fmt.Errorf("mcp: call %s.%s: %w (reconnect failed: %v)", serverName, toolName, err, reconnErr)
	}
	log.Printf("mcp: reconnected to %s, retrying call", serverName)

	// Retry once after successful reconnect.
	return c.callOnce(ctx, serverName, toolName, args)
}

// callOnce performs a single tools/call RPC without retry.
func (c *Client) callOnce(ctx context.Context, serverName, toolName string, args json.RawMessage) (*ToolCallResult, error) {
	c.mu.RLock()
	conn, ok := c.servers[serverName]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", serverName)
	}

	c.mu.Lock()
	id := c.nextRequestID()
	c.mu.Unlock()

	resp, err := conn.transport.Send(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  ToolCallParams{Name: toolName, Arguments: args},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: call %s.%s: %w", serverName, toolName, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: call %s.%s: %s", serverName, toolName, resp.Error.Message)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("mcp: parse result %s.%s: %w", serverName, toolName, err)
	}

	return &result, nil
}

func (c *Client) nextRequestID() int {
	c.nextID++
	return c.nextID
}
