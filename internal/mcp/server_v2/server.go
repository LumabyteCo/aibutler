package server_v2

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/mcp"
)

// MemorySearcher is the narrow interface for memory search.
type MemorySearcher interface {
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// SearchResult is a single memory search result.
type SearchResult struct {
	Content string `json:"content"`
	Score   float64 `json:"score,omitempty"`
}

// ScheduleOps is the interface the server needs to both list schedules
// (for the `butler.schedule.*` tool family) and create them
// (for `butler.schedule.create`). Keeping read + write on one interface
// lets the CLI bootstrap wire a single adapter.
type ScheduleOps interface {
	ListTasks(ctx context.Context) ([]ScheduleEntry, error)
	CreateTask(ctx context.Context, req CreateScheduleRequest) (string, error)
}

// ScheduleLister is kept as a type alias for backward compatibility with
// callers that only consume the read side. New callers should use ScheduleOps.
type ScheduleLister = ScheduleOps

// ScheduleEntry is a scheduled task.
type ScheduleEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	CronExpr    string `json:"cron_expr"`
	Enabled     bool   `json:"enabled"`
}

// CreateScheduleRequest is the typed argument to CreateTask.
// Mirrors the input schema advertised to MCP clients at tools/list.
type CreateScheduleRequest struct {
	Description string `json:"description"`
	CronExpr    string `json:"cron"`
	Task        string `json:"task,omitempty"`
	Channel     string `json:"channel,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
}

// ChannelOps is the interface for listing channels and sending messages
// via a named channel. Used by `butler.channel.*` tools.
type ChannelOps interface {
	ListChannels() []string
	SendMessage(ctx context.Context, channel, to, message string) error
}

// ChannelLister is kept as an alias for backward compatibility.
type ChannelLister = ChannelOps

// AgentOps is the interface for listing registered agents and delegating
// tasks to them. Used by `butler.agent.*` tools.
type AgentOps interface {
	ListAgents(ctx context.Context) ([]AgentEntry, error)
	DelegateTask(ctx context.Context, req DelegateRequest) (string, error)
}

// AgentRegistry is kept as an alias for backward compatibility.
type AgentRegistry = AgentOps

// AgentEntry is a registered agent.
type AgentEntry struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
}

// DelegateRequest is the typed argument to DelegateTask. Either the
// agent name (for A2A peers) or an empty name (for local subagent)
// is accepted.
type DelegateRequest struct {
	Agent string `json:"agent,omitempty"`
	Task  string `json:"task"`
}

// SwarmRunner runs a multi-agent swarm task. Returns the synthesized
// answer. Used by `butler.swarm.run`.
type SwarmRunner interface {
	Run(ctx context.Context, goal string) (string, error)
}

// ResourceInfo describes an MCP resource.
type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceListResult is the response to resources/list.
type ResourceListResult struct {
	Resources []ResourceInfo `json:"resources"`
}

// Server is the MCP v2 server exposing Butler capabilities.
type Server struct {
	memory   MemorySearcher
	schedule ScheduleOps
	channels ChannelOps
	registry AgentOps
	swarm    SwarmRunner
	mu       sync.Mutex
}

// New creates an MCP v2 server. All subsystem adapters are required for
// their respective tools to work; pass nil for `swarm` if the build
// doesn't include swarm support, and `butler.swarm.run` will return a
// configured-out error instead of a fake success.
func New(memory MemorySearcher, schedule ScheduleOps, channels ChannelOps, registry AgentOps) *Server {
	return &Server{
		memory:   memory,
		schedule: schedule,
		channels: channels,
		registry: registry,
	}
}

// SetSwarmRunner wires the swarm runner after construction. Kept separate
// from New() so swarm is optional without breaking the existing
// four-argument call site and its tests.
func (s *Server) SetSwarmRunner(runner SwarmRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.swarm = runner
}

// HandleStdio serves JSON-RPC over stdio.
func (s *Server) HandleStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("mcp_v2: read: %w", err)
			}
			return nil
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req mcp.JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := mcp.JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &mcp.JSONRPCError{Code: -32700, Message: "parse error"},
			}
			s.writeResponse(w, resp)
			continue
		}

		resp := s.handleRequest(ctx, req)
		if resp == nil {
			continue
		}
		s.writeResponse(w, *resp)
	}
}

// HandleHTTP returns an HTTP handler for HTTP/SSE transport.
func (s *Server) HandleHTTP() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var req mcp.JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			resp := mcp.JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &mcp.JSONRPCError{Code: -32700, Message: "parse error"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := s.handleRequest(r.Context(), req)
		if resp == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
}

func (s *Server) writeResponse(w io.Writer, resp mcp.JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("mcp_v2: marshal response: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		log.Printf("mcp_v2: write response: %v", err)
	}
}

func (s *Server) handleRequest(ctx context.Context, req mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "ping":
		result, _ := json.Marshal(struct{}{})
		return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	default:
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcp.JSONRPCError{Code: -32601, Message: "method not found"},
		}
	}
}

func (s *Server) handleInitialize(req mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	type caps struct {
		Tools     *struct{} `json:"tools"`
		Resources *struct{} `json:"resources"`
	}
	type initResult struct {
		ProtocolVersion string       `json:"protocolVersion"`
		ServerInfo      mcp.ServerInfo `json:"serverInfo"`
		Capabilities    caps         `json:"capabilities"`
	}
	result := initResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      mcp.ServerInfo{Name: "ai-butler", Version: "2.0.0"},
		Capabilities:    caps{Tools: &struct{}{}, Resources: &struct{}{}},
	}
	data, _ := json.Marshal(result)
	return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) handleToolsList(req mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	tools := []mcp.ToolInfo{
		{
			Name:        "butler.memory.search",
			Description: "Search Butler's memory for relevant information",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`),
		},
		{
			Name:        "butler.schedule.create",
			Description: "Create a scheduled task that runs on a cron expression. When `task` is omitted the description is used as the task text. Returns the created schedule ID.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"description":{"type":"string","description":"Human-readable name of the schedule"},"cron":{"type":"string","description":"Standard 5-field cron expression (e.g. '0 9 * * *' for daily 9am)"},"task":{"type":"string","description":"Natural-language task to run on each tick (default: description)"},"channel":{"type":"string","description":"Optional delivery channel (e.g. 'webchat', 'telegram')"},"account_id":{"type":"string","description":"Optional channel account to deliver to"}},"required":["description","cron"]}`),
		},
		{
			Name:        "butler.channel.send",
			Description: "Send a text message through a registered messaging channel. Returns status + echoed channel/recipient.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"channel":{"type":"string","description":"Channel name (e.g. 'webchat', 'telegram', 'slack')"},"to":{"type":"string","description":"Channel-specific recipient ID"},"message":{"type":"string","description":"Message body"}},"required":["channel","to","message"]}`),
		},
		{
			Name:        "butler.swarm.run",
			Description: "Decompose a goal into subtasks, execute them with coordinated agents, and return the synthesized answer. Blocks until completion. The `agents` hint is advertised for forward-compat and currently ignored; the swarm auto-sizes based on the goal.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"task":{"type":"string","description":"High-level goal for the swarm"},"agents":{"type":"integer","description":"Optional agent count hint (currently ignored)"}},"required":["task"]}`),
		},
		{
			Name:        "butler.agent.delegate",
			Description: "Run a single task through the Butler agent loop (no swarm decomposition) and return the final answer. The `agent` parameter routes to a named A2A peer when set; unset routes to a local subagent.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"agent":{"type":"string","description":"Optional A2A peer name (empty = run locally)"},"task":{"type":"string","description":"Task description"}},"required":["task"]}`),
		},
	}
	result := mcp.ToolListResult{Tools: tools}
	data, _ := json.Marshal(result)
	return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) handleToolsCall(ctx context.Context, req mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcp.JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	var params mcp.ToolCallParams
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcp.JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	var output string
	var execErr error

	switch params.Name {
	case "butler.memory.search":
		output, execErr = s.execMemorySearch(ctx, params.Arguments)
	case "butler.schedule.create":
		output, execErr = s.execScheduleCreate(ctx, params.Arguments)
	case "butler.channel.send":
		output, execErr = s.execChannelSend(ctx, params.Arguments)
	case "butler.swarm.run":
		output, execErr = s.execSwarmRun(ctx, params.Arguments)
	case "butler.agent.delegate":
		output, execErr = s.execAgentDelegate(ctx, params.Arguments)
	default:
		result := mcp.ToolCallResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", params.Name)}},
			IsError: true,
		}
		data, _ := json.Marshal(result)
		return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
	}

	if execErr != nil {
		result := mcp.ToolCallResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: execErr.Error()}},
			IsError: true,
		}
		data, _ := json.Marshal(result)
		return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
	}

	result := mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: output}},
	}
	data, _ := json.Marshal(result)
	return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) execMemorySearch(ctx context.Context, args json.RawMessage) (string, error) {
	if s.memory == nil {
		return "[]", nil
	}

	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	results, err := s.memory.Search(ctx, input.Query)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(results)
	return string(data), nil
}

// execScheduleCreate handles `butler.schedule.create`. Dispatches to the
// real scheduler when wired; returns a configured-out error when the
// server was constructed without a ScheduleOps adapter (tests, minimal
// builds).
func (s *Server) execScheduleCreate(ctx context.Context, args json.RawMessage) (string, error) {
	if s.schedule == nil {
		return "", fmt.Errorf("butler.schedule.create: scheduler not configured")
	}
	var req CreateScheduleRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("butler.schedule.create: invalid arguments: %w", err)
	}
	if req.Description == "" {
		return "", fmt.Errorf("butler.schedule.create: 'description' is required")
	}
	if req.CronExpr == "" {
		return "", fmt.Errorf("butler.schedule.create: 'cron' is required")
	}
	if req.Task == "" {
		// Many callers will pass description-as-task. Default the task to
		// the description so simple callers don't need to send both.
		req.Task = req.Description
	}
	id, err := s.schedule.CreateTask(ctx, req)
	if err != nil {
		return "", fmt.Errorf("butler.schedule.create: %w", err)
	}
	out, _ := json.Marshal(map[string]string{"status": "created", "id": id})
	return string(out), nil
}

// execChannelSend handles `butler.channel.send`. Fails cleanly when the
// channel isn't registered (rather than swallowing the miss silently).
func (s *Server) execChannelSend(ctx context.Context, args json.RawMessage) (string, error) {
	if s.channels == nil {
		return "", fmt.Errorf("butler.channel.send: channel registry not configured")
	}
	var input struct {
		Channel string `json:"channel"`
		To      string `json:"to"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("butler.channel.send: invalid arguments: %w", err)
	}
	if input.Channel == "" {
		return "", fmt.Errorf("butler.channel.send: 'channel' is required")
	}
	if input.To == "" {
		return "", fmt.Errorf("butler.channel.send: 'to' is required")
	}
	if input.Message == "" {
		return "", fmt.Errorf("butler.channel.send: 'message' is required")
	}
	if err := s.channels.SendMessage(ctx, input.Channel, input.To, input.Message); err != nil {
		return "", fmt.Errorf("butler.channel.send: %w", err)
	}
	out, _ := json.Marshal(map[string]string{"status": "sent", "channel": input.Channel, "to": input.To})
	return string(out), nil
}

// execSwarmRun handles `butler.swarm.run`. Blocks until the swarm
// decomposes the goal, runs the subtasks, and returns a synthesized
// answer. MCP callers should expect multi-second latency on real goals.
func (s *Server) execSwarmRun(ctx context.Context, args json.RawMessage) (string, error) {
	if s.swarm == nil {
		return "", fmt.Errorf("butler.swarm.run: swarm orchestrator not configured in this build")
	}
	var input struct {
		Task string `json:"task"`
		// Agents field is advertised in the schema for forward-compat but
		// ignored for now — swarm auto-decomposes based on the goal.
		Agents int `json:"agents,omitempty"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("butler.swarm.run: invalid arguments: %w", err)
	}
	if input.Task == "" {
		return "", fmt.Errorf("butler.swarm.run: 'task' is required")
	}
	answer, err := s.swarm.Run(ctx, input.Task)
	if err != nil {
		return "", fmt.Errorf("butler.swarm.run: %w", err)
	}
	out, _ := json.Marshal(map[string]string{"status": "completed", "result": answer})
	return string(out), nil
}

// execAgentDelegate handles `butler.agent.delegate`. Runs the task as a
// single AI Butler agent (no swarm decomposition) and returns the final
// answer. If `agent` is provided and matches a known A2A peer, the task
// is forwarded to that peer; otherwise it runs locally.
func (s *Server) execAgentDelegate(ctx context.Context, args json.RawMessage) (string, error) {
	if s.registry == nil {
		return "", fmt.Errorf("butler.agent.delegate: agent runtime not configured")
	}
	var req DelegateRequest
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("butler.agent.delegate: invalid arguments: %w", err)
	}
	if req.Task == "" {
		return "", fmt.Errorf("butler.agent.delegate: 'task' is required")
	}
	answer, err := s.registry.DelegateTask(ctx, req)
	if err != nil {
		return "", fmt.Errorf("butler.agent.delegate: %w", err)
	}
	out, _ := json.Marshal(map[string]string{"status": "completed", "result": answer})
	return string(out), nil
}

func (s *Server) handleResourcesList(req mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	resources := []ResourceInfo{
		{URI: "memory://search", Name: "Memory Search", Description: "Search Butler's memory store", MimeType: "application/json"},
		{URI: "memory://entities", Name: "Memory Entities", Description: "List known entities", MimeType: "application/json"},
		{URI: "schedule://tasks", Name: "Scheduled Tasks", Description: "List scheduled tasks", MimeType: "application/json"},
		{URI: "channels://list", Name: "Channels", Description: "List active channels", MimeType: "application/json"},
		{URI: "agent://registry", Name: "Agent Registry", Description: "List registered agents", MimeType: "application/json"},
	}
	result := ResourceListResult{Resources: resources}
	data, _ := json.Marshal(result)
	return &mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}
