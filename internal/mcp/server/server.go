package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/mcp"
)

// ToolExecutor is the narrow interface the MCP server needs to call tools.
type ToolExecutor interface {
	Execute(ctx context.Context, input string) (string, error)
}

// ToolLister provides tools to expose via MCP.
type ToolLister interface {
	List() []mcp.ToolInfo
	Get(name string) (ToolExecutor, bool)
}

// Server handles MCP JSON-RPC 2.0 over stdio.
type Server struct {
	info  mcp.ServerInfo
	tools ToolLister
	mu    sync.Mutex
}

// New creates an MCP server.
func New(info mcp.ServerInfo, tools ToolLister) *Server {
	return &Server{info: info, tools: tools}
}

// Tools returns the list of tools exposed by this server.
func (s *Server) Tools() []mcp.ToolInfo {
	return s.tools.List()
}

// Serve reads JSON-RPC requests from in and writes responses to out.
// Returns when ctx is cancelled or EOF is reached.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("mcp server: read: %w", err)
			}
			return nil // EOF
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
			s.writeResponse(out, resp)
			continue
		}

		resp := s.handleRequest(ctx, req)
		if resp == nil {
			continue // notification, no response
		}
		s.writeResponse(out, *resp)
	}
}

func (s *Server) writeResponse(out io.Writer, resp mcp.JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("mcp server: marshal response: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := out.Write(data); err != nil {
		log.Printf("mcp server: write response: %v", err)
	}
}

func (s *Server) handleRequest(ctx context.Context, req mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	switch req.Method {
	case "initialize":
		resp := s.handleInitialize(req)
		return &resp
	case "initialized", "notifications/initialized":
		return nil // notification
	case "tools/list":
		resp := s.handleToolsList(req)
		return &resp
	case "tools/call":
		resp := s.handleToolsCall(ctx, req)
		return &resp
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

type initializeResultWithCaps struct {
	ProtocolVersion string            `json:"protocolVersion"`
	ServerInfo      mcp.ServerInfo    `json:"serverInfo"`
	Capabilities    serverCapabilities `json:"capabilities"`
}

type serverCapabilities struct {
	Tools *struct{} `json:"tools"`
}

func (s *Server) handleInitialize(req mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	result := initializeResultWithCaps{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      s.info,
		Capabilities:    serverCapabilities{Tools: &struct{}{}},
	}
	data, _ := json.Marshal(result)
	return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) handleToolsList(req mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	tools := s.tools.List()
	if tools == nil {
		tools = []mcp.ToolInfo{}
	}
	result := mcp.ToolListResult{Tools: tools}
	data, _ := json.Marshal(result)
	return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

func (s *Server) handleToolsCall(ctx context.Context, req mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	// Parse params from the request.
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcp.JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	var params mcp.ToolCallParams
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return mcp.JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcp.JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	executor, ok := s.tools.Get(params.Name)
	if !ok {
		result := mcp.ToolCallResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", params.Name)}},
			IsError: true,
		}
		data, _ := json.Marshal(result)
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
	}

	input := "{}"
	if len(params.Arguments) > 0 {
		input = string(params.Arguments)
	}

	output, err := executor.Execute(ctx, input)
	if err != nil {
		result := mcp.ToolCallResult{
			Content: []mcp.ContentBlock{{Type: "text", Text: err.Error()}},
			IsError: true,
		}
		data, _ := json.Marshal(result)
		return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
	}

	result := mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: output}},
	}
	data, _ := json.Marshal(result)
	return mcp.JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: data}
}

// ToolEntry describes a tool from a provider.
type ToolEntry struct {
	Name        string
	Description string
	Schema      string
	Capability  string
	Executor    ToolExecutor
}

// ToolProvider is the narrow interface for listing tools.
type ToolProvider interface {
	All() []ToolEntry
}

// RegistryLister wraps a ToolProvider, filtering by allowed capabilities.
type RegistryLister struct {
	tools map[string]registeredTool
	infos []mcp.ToolInfo
}

type registeredTool struct {
	info     mcp.ToolInfo
	executor ToolExecutor
}

// NewRegistryLister creates a lister that exposes tools matching allowedCaps.
func NewRegistryLister(provider ToolProvider, allowedCaps []string) *RegistryLister {
	allowed := make(map[string]bool, len(allowedCaps))
	for _, c := range allowedCaps {
		allowed[c] = true
	}

	rl := &RegistryLister{tools: make(map[string]registeredTool)}
	for _, entry := range provider.All() {
		if !allowed[entry.Capability] {
			continue
		}
		info := mcp.ToolInfo{
			Name:        entry.Name,
			Description: entry.Description,
			InputSchema: json.RawMessage(entry.Schema),
		}
		rl.tools[entry.Name] = registeredTool{info: info, executor: entry.Executor}
		rl.infos = append(rl.infos, info)
	}
	return rl
}

func (r *RegistryLister) List() []mcp.ToolInfo { return r.infos }

func (r *RegistryLister) Get(name string) (ToolExecutor, bool) {
	t, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	return t.executor, true
}
