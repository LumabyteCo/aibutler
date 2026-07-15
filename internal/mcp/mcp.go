package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// ClientVersion is reported to servers during the initialize handshake.
const ClientVersion = "0.1.0"

// ErrRemote marks an error the server reported in a JSON-RPC error response, as
// opposed to a transport failure. The distinction matters: a remote error means
// the connection is healthy and must not trigger a reconnect.
var ErrRemote = errors.New("mcp: remote error")

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

	// Negotiated during the initialize handshake.
	protocolVersion string
	serverInfo      ServerInfo
	serverCaps      ServerCapabilities
}

// Client manages connections to one or more MCP servers.
type Client struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
	nextID  int

	// registry is set by RegisterMCPTools so that Reconnect can re-register tools.
	registry    ToolRegistry
	registrySet bool

	// hmu guards the peer-traffic hooks. It is deliberately separate from mu:
	// hooks fire from transport goroutines and must not contend with an
	// in-flight connect/call holding mu.
	hmu      sync.RWMutex
	elicit   ElicitationHandler
	progress ProgressHandler
	// reRegister re-adds a server's tools to the registry after a reconnect.
	// It is a callback rather than a ToolRegistry method to keep the mcp → tool
	// import cycle broken.
	reRegister func(serverName string)
}

// ToolRegistry is the subset of tool.Registry that the MCP client needs.
// This avoids an import cycle (mcp → tool → mcp).
type ToolRegistry interface {
	UnregisterPrefix(prefix string)
}

// NewClient creates an MCP client.
//
// Elicitation defaults to DeclineElicitation: the client advertises that it can
// answer elicitation/create (so servers never hang waiting) but refuses to
// invent answers on the user's behalf until a policy is chosen.
func NewClient() *Client {
	return &Client{
		servers: make(map[string]*serverConn),
		elicit:  DeclineElicitation,
	}
}

// SetRegistry stores a reference to the tool registry for reconnect-time re-registration.
func (c *Client) SetRegistry(r ToolRegistry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registry = r
	c.registrySet = true
}

// SetElicitationHandler installs the policy used to answer server elicitation
// requests. Passing nil restores the safe default (decline).
func (c *Client) SetElicitationHandler(h ElicitationHandler) {
	c.hmu.Lock()
	defer c.hmu.Unlock()
	if h == nil {
		h = DeclineElicitation
	}
	c.elicit = h
}

// SetProgressHandler installs a sink for notifications/progress.
func (c *Client) SetProgressHandler(h ProgressHandler) {
	c.hmu.Lock()
	defer c.hmu.Unlock()
	c.progress = h
}

// SetReRegisterHook installs the callback Reconnect uses to re-add a server's
// tools to the tool registry.
func (c *Client) SetReRegisterHook(fn func(serverName string)) {
	c.hmu.Lock()
	defer c.hmu.Unlock()
	c.reRegister = fn
}

// ServerInfo returns the identity a server reported at initialize.
func (c *Client) ServerInfo(name string) (ServerInfo, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	conn, ok := c.servers[name]
	if !ok {
		return ServerInfo{}, "", fmt.Errorf("mcp: server %q not connected", name)
	}
	return conn.serverInfo, conn.protocolVersion, nil
}

// ServerCapabilities returns what a server declared at initialize.
func (c *Client) ServerCapabilities(name string) (ServerCapabilities, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	conn, ok := c.servers[name]
	if !ok {
		return ServerCapabilities{}, fmt.Errorf("mcp: server %q not connected", name)
	}
	return conn.serverCaps, nil
}

// ConnectWithTransport connects using a provided transport (useful for testing).
func (c *Client) ConnectWithTransport(ctx context.Context, name string, t Transport) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.connectLocked(ctx, name, t, ServerConfig{Name: name})
}

// installHandlers wires peer-initiated traffic for one server's transport.
func (c *Client) installHandlers(name string, t Transport) {
	t.SetHandlers(Handlers{
		OnRequest: func(method string, params json.RawMessage) (interface{}, *JSONRPCError) {
			switch method {
			case "elicitation/create":
				var p ElicitRequestParams
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, &JSONRPCError{Code: ErrCodeInternalError, Message: "bad elicitation params: " + err.Error()}
				}
				c.hmu.RLock()
				h := c.elicit
				c.hmu.RUnlock()
				if h == nil {
					h = DeclineElicitation
				}
				res := h(name, p)
				log.Printf("mcp: %s elicitation/create → %s", name, res.Action)
				return res, nil

			case "ping":
				return struct{}{}, nil

			default:
				return nil, &JSONRPCError{Code: ErrCodeMethodNotFound, Message: "unsupported: " + method}
			}
		},
		OnNotification: func(method string, params json.RawMessage) {
			switch method {
			case "notifications/progress":
				var p ProgressParams
				if err := json.Unmarshal(params, &p); err != nil {
					return
				}
				c.hmu.RLock()
				h := c.progress
				c.hmu.RUnlock()
				if h != nil {
					h(name, p)
				}
			case "notifications/tools/list_changed":
				log.Printf("mcp: %s reports tools/list_changed", name)
			}
		},
	})
}

// connectLocked performs the handshake and tool discovery while holding the write lock.
func (c *Client) connectLocked(ctx context.Context, name string, t Transport, cfg ServerConfig) error {
	// Bound the handshake here rather than trusting the caller: Bootstrap
	// connects with context.Background(), so a server that starts but never
	// answers would otherwise wedge startup forever — and it holds c.mu while
	// it hangs, blocking every other server too.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, connectTimeout)
		defer cancel()
	}

	// 0. Route peer-initiated traffic before any request goes out, so an
	//    elicitation or progress message can never arrive unhandled.
	c.installHandlers(name, t)

	// 1. Initialize handshake.
	initResp, err := t.Send(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "initialize",
		Params: InitializeParams{
			ProtocolVersion: PreferredProtocolVersion,
			ClientInfo:      ClientInfo{Name: "aibutler", Version: ClientVersion},
			Capabilities: &ClientCapabilities{
				// Declared because OnRequest above always answers
				// elicitation/create. Advertising a capability we could not
				// answer would leave servers waiting forever.
				Elicitation: &EmptyCapability{},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("mcp: initialize %s: %w", name, err)
	}
	if initResp.Error != nil {
		return fmt.Errorf("mcp: initialize %s: %s", name, initResp.Error.Message)
	}

	// 2. Read back what the server negotiated. The server picks the version;
	//    if it names one we cannot speak, the spec says to disconnect.
	var initResult InitializeResult
	if len(initResp.Result) > 0 {
		if err := json.Unmarshal(initResp.Result, &initResult); err != nil {
			return fmt.Errorf("mcp: parse initialize result %s: %w", name, err)
		}
	}
	negotiated := initResult.ProtocolVersion
	if negotiated == "" {
		// Pre-spec server that echoes nothing: assume the oldest we offered.
		negotiated = ProtocolVersion20241105
	}
	if !IsSupportedProtocolVersion(negotiated) {
		t.Close()
		return fmt.Errorf("mcp: %s requested unsupported protocol version %q (client supports %v)",
			name, negotiated, SupportedProtocolVersions)
	}

	// 3. Tell the server the handshake is complete. Required by the MCP
	//    lifecycle before normal requests; notifications carry no id and get
	//    no response.
	if err := t.Notify(ctx, "notifications/initialized", struct{}{}); err != nil {
		return fmt.Errorf("mcp: initialized notification %s: %w", name, err)
	}

	// 4. Discover tools.
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
		name:            name,
		transport:       t,
		tools:           toolList.Tools,
		cfg:             cfg,
		protocolVersion: negotiated,
		serverInfo:      initResult.ServerInfo,
		serverCaps:      initResult.Capabilities,
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
	if err := c.connectLocked(ctx, cfg.Name, t, cfg); err != nil {
		// The handshake failed, so nothing owns this transport. Without an
		// explicit close the subprocess and its read-loop goroutine leak for
		// the life of the process — once per failed connect attempt.
		t.Close()
		return err
	}
	return nil
}

// Reconnect tears down a server connection and re-establishes it using the stored config.
// Stale tools are unregistered first and re-registered on success, so the agent
// keeps a working tool set across a restart.
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
	if err := c.Connect(ctx, cfg); err != nil {
		return err
	}

	// Re-register the fresh tool set. Unregistering without this leaves the
	// agent permanently unable to call the server it just reconnected to.
	c.hmu.RLock()
	hook := c.reRegister
	c.hmu.RUnlock()
	if hook != nil {
		hook(serverName)
	}
	return nil
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

// ListResources returns the resources a server exposes.
func (c *Client) ListResources(ctx context.Context, serverName string) ([]ResourceInfo, error) {
	resp, err := c.request(ctx, serverName, "resources/list", nil)
	if err != nil {
		return nil, err
	}
	var out ResourceListResult
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, fmt.Errorf("mcp: parse resources %s: %w", serverName, err)
	}
	return out.Resources, nil
}

// ReadResource fetches the contents of a resource by URI.
func (c *Client) ReadResource(ctx context.Context, serverName, uri string) ([]EmbeddedResource, error) {
	resp, err := c.request(ctx, serverName, "resources/read", ResourceReadParams{URI: uri})
	if err != nil {
		return nil, err
	}
	var out ResourceReadResult
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, fmt.Errorf("mcp: parse resource %s: %w", serverName, err)
	}
	return out.Contents, nil
}

// request performs a generic RPC against a connected server.
func (c *Client) request(ctx context.Context, serverName, method string, params interface{}) (json.RawMessage, error) {
	c.mu.RLock()
	conn, ok := c.servers[serverName]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", serverName)
	}

	c.mu.Lock()
	id := c.nextRequestID()
	c.mu.Unlock()

	resp, err := conn.transport.Send(ctx, JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("mcp: %s %s: %w", method, serverName, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: %s %s: %s: %w", method, serverName, resp.Error.Message, ErrRemote)
	}
	return resp.Result, nil
}

// defaultCallTimeout is applied when the parent context has no deadline.
const defaultCallTimeout = 60 * time.Second

// reconnectTimeout bounds a reconnect attempt. It is independent of the
// caller's context so a timed-out call can still restore a dead server.
const reconnectTimeout = 30 * time.Second

// connectTimeout bounds the initialize + tools/list handshake when the caller
// supplies no deadline of its own.
const connectTimeout = 30 * time.Second

// Call invokes a tool on a specific server. If the transport fails (e.g. the
// server crashed), it attempts one reconnect + retry. A JSON-RPC error from a
// healthy server is returned as-is — reconnecting would not help and would
// needlessly restart the subprocess.
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
	if errors.Is(err, ErrRemote) {
		// The server answered; the connection is fine.
		return nil, err
	}

	// The caller gave up (deadline or cancellation), which says nothing about
	// the server's health. Tearing it down here would kill a working
	// subprocess mid-work, and any tool slow enough to outrun the caller's
	// deadline would trigger it on every timeout.
	if ctx.Err() != nil {
		return nil, err
	}

	// Transport error → try one reconnect. Use a fresh context: the caller's
	// may be nearly exhausted, and a reconnect that inherits a dead deadline
	// would fail instantly and leave the server permanently unregistered.
	log.Printf("mcp: call %s.%s failed (%v), attempting reconnect", serverName, toolName, err)
	rctx, cancel := context.WithTimeout(context.Background(), reconnectTimeout)
	defer cancel()
	if reconnErr := c.Reconnect(rctx, serverName); reconnErr != nil {
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

	// Opt into live progress: servers only stream notifications/progress for
	// calls that carry a token. The request id is unique per connection, so it
	// doubles as the token.
	params := ToolCallParams{
		Name:      toolName,
		Arguments: args,
		Meta:      &RequestMeta{ProgressToken: id},
	}

	resp, err := conn.transport.Send(ctx, JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  params,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp: call %s.%s: %w", serverName, toolName, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: call %s.%s: %s: %w", serverName, toolName, resp.Error.Message, ErrRemote)
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
