package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/mcp"
	"github.com/LumabyteCo/aibutler/internal/mcp/server"
)

// --- test helpers ---

type mockExecutor struct {
	output string
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, _ string) (string, error) {
	return m.output, m.err
}

type mockLister struct {
	tools map[string]*mockExecutor
	infos []mcp.ToolInfo
}

func newMockLister(tools map[string]*mockExecutor) *mockLister {
	ml := &mockLister{tools: tools}
	for name := range tools {
		ml.infos = append(ml.infos, mcp.ToolInfo{
			Name:        name,
			Description: "Test tool " + name,
			InputSchema: json.RawMessage(`{"type":"object"}`),
		})
	}
	return ml
}

func (m *mockLister) List() []mcp.ToolInfo { return m.infos }
func (m *mockLister) Get(name string) (server.ToolExecutor, bool) {
	e, ok := m.tools[name]
	if !ok {
		return nil, false
	}
	return e, true
}

func sendRequest(t *testing.T, srv *server.Server, method string, id int, params interface{}) mcp.JSONRPCResponse {
	t.Helper()
	req := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, _ := json.Marshal(req)
	data = append(data, '\n')

	var out bytes.Buffer
	ctx := context.Background()
	in := bytes.NewReader(data)
	if err := srv.Serve(ctx, in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var resp mcp.JSONRPCResponse
	if out.Len() == 0 {
		return resp
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw: %s)", err, out.String())
	}
	return resp
}

func testServer() *server.Server {
	lister := newMockLister(map[string]*mockExecutor{
		"memory.search": {output: `[{"id":1,"content":"test thought"}]`},
		"memory.stats":  {output: `{"summary":"5 entities"}`},
		"error.tool":    {err: fmt.Errorf("something went wrong")},
	})
	return server.New(mcp.ServerInfo{Name: "aibutler", Version: "0.1.0"}, lister)
}

// --- tests ---

func TestNewServer(t *testing.T) {
	srv := server.New(mcp.ServerInfo{Name: "test", Version: "1.0"}, newMockLister(nil))
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestServeInitialize(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "initialize", 1, mcp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      mcp.ClientInfo{Name: "test-client", Version: "1.0"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
	if resp.ID != 1 {
		t.Errorf("id = %d, want 1", resp.ID)
	}

	var result struct {
		ProtocolVersion string         `json:"protocolVersion"`
		ServerInfo      mcp.ServerInfo `json:"serverInfo"`
	}
	json.Unmarshal(resp.Result, &result)
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("protocol = %q, want 2024-11-05", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "aibutler" {
		t.Errorf("server name = %q", result.ServerInfo.Name)
	}
}

func TestServeInitializedNotification(t *testing.T) {
	srv := testServer()
	req := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 0, Method: "notifications/initialized"}
	data, _ := json.Marshal(req)
	data = append(data, '\n')

	var out bytes.Buffer
	srv.Serve(context.Background(), bytes.NewReader(data), &out)

	if out.Len() > 0 {
		t.Errorf("expected no response for notification, got: %s", out.String())
	}
}

func TestServeToolsList(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "tools/list", 2, nil)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	var result mcp.ToolListResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Tools) != 3 {
		t.Errorf("tools count = %d, want 3", len(result.Tools))
	}
}

func TestServeToolsListEmpty(t *testing.T) {
	srv := server.New(mcp.ServerInfo{Name: "empty", Version: "1.0"}, newMockLister(nil))
	resp := sendRequest(t, srv, "tools/list", 1, nil)

	var result mcp.ToolListResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Tools) != 0 {
		t.Errorf("tools count = %d, want 0", len(result.Tools))
	}
}

func TestServeToolsCallSuccess(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "tools/call", 3, mcp.ToolCallParams{
		Name:      "memory.search",
		Arguments: json.RawMessage(`{"query":"test"}`),
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if result.IsError {
		t.Error("expected IsError=false")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	if !strings.Contains(result.Content[0].Text, "test thought") {
		t.Errorf("content = %q, missing 'test thought'", result.Content[0].Text)
	}
}

func TestServeToolsCallError(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "tools/call", 4, mcp.ToolCallParams{
		Name: "error.tool",
	})

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	if !strings.Contains(result.Content[0].Text, "something went wrong") {
		t.Errorf("error text = %q", result.Content[0].Text)
	}
}

func TestServeToolsCallUnknown(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "tools/call", 5, mcp.ToolCallParams{
		Name: "nonexistent",
	})

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if !result.IsError {
		t.Error("expected IsError=true for unknown tool")
	}
}

func TestServePing(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "ping", 6, nil)
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.ID != 6 {
		t.Errorf("id = %d, want 6", resp.ID)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	srv := testServer()
	resp := sendRequest(t, srv, "resources/list", 7, nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestServeMalformedJSON(t *testing.T) {
	srv := testServer()
	var out bytes.Buffer
	in := strings.NewReader("this is not json\n")
	srv.Serve(context.Background(), in, &out)

	var resp mcp.JSONRPCResponse
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestServeMultipleRequests(t *testing.T) {
	srv := testServer()

	var input bytes.Buffer
	for i := 1; i <= 3; i++ {
		req := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: i, Method: "ping"}
		data, _ := json.Marshal(req)
		input.Write(data)
		input.WriteByte('\n')
	}

	var out bytes.Buffer
	srv.Serve(context.Background(), &input, &out)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("response lines = %d, want 3", len(lines))
	}
	for i, line := range lines {
		var resp mcp.JSONRPCResponse
		json.Unmarshal([]byte(line), &resp)
		if resp.ID != i+1 {
			t.Errorf("line %d: id = %d, want %d", i, resp.ID, i+1)
		}
	}
}

func TestServeContextCancellation(t *testing.T) {
	srv := testServer()
	ctx, cancel := context.WithCancel(context.Background())

	pr, pw := io.Pipe()
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, pr, &out)
	}()

	cancel()
	pw.Close()

	err := <-done
	if err != nil && err != context.Canceled {
		// EOF is also acceptable
	}
}

func TestRegistryListerFilters(t *testing.T) {
	provider := &mockProvider{
		entries: []server.ToolEntry{
			{Name: "memory.search", Capability: "memory.read", Schema: `{"type":"object"}`, Executor: &mockExecutor{output: "ok"}},
			{Name: "shell.exec", Capability: "shell.execute", Schema: `{"type":"object"}`, Executor: &mockExecutor{output: "ok"}},
			{Name: "data.query", Capability: "data.read", Schema: `{"type":"object"}`, Executor: &mockExecutor{output: "ok"}},
		},
	}
	lister := server.NewRegistryLister(provider, []string{"memory.read", "data.read"})

	tools := lister.List()
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	names := map[string]bool{}
	for _, ti := range tools {
		names[ti.Name] = true
	}
	if !names["memory.search"] {
		t.Error("missing memory.search")
	}
	if !names["data.query"] {
		t.Error("missing data.query")
	}
	if names["shell.exec"] {
		t.Error("shell.exec should be filtered out")
	}
}

func TestRegistryListerGetTool(t *testing.T) {
	provider := &mockProvider{
		entries: []server.ToolEntry{
			{Name: "memory.search", Capability: "memory.read", Schema: `{"type":"object"}`, Executor: &mockExecutor{output: "found"}},
		},
	}
	lister := server.NewRegistryLister(provider, []string{"memory.read"})

	exec, ok := lister.Get("memory.search")
	if !ok {
		t.Fatal("expected tool to be found")
	}
	out, err := exec.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "found" {
		t.Errorf("output = %q", out)
	}
}

func TestRegistryListerRejectsDisallowed(t *testing.T) {
	provider := &mockProvider{
		entries: []server.ToolEntry{
			{Name: "shell.exec", Capability: "shell.execute", Schema: `{"type":"object"}`, Executor: &mockExecutor{}},
		},
	}
	lister := server.NewRegistryLister(provider, []string{"memory.read"})

	_, ok := lister.Get("shell.exec")
	if ok {
		t.Error("shell.exec should not be accessible")
	}
}

type mockProvider struct {
	entries []server.ToolEntry
}

func (m *mockProvider) All() []server.ToolEntry { return m.entries }
