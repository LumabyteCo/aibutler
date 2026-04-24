package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/mcp"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// helper: build an InMemoryTransport with standard initialize + tools/list responses.
func newTestTransport(tools []mcp.ToolInfo, extra ...mcp.JSONRPCResponse) *mcp.InMemoryTransport {
	initResult, _ := json.Marshal(mcp.InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      mcp.ServerInfo{Name: "test-server", Version: "1.0"},
	})
	toolsResult, _ := json.Marshal(mcp.ToolListResult{Tools: tools})

	responses := []mcp.JSONRPCResponse{
		{JSONRPC: "2.0", Result: initResult},
		{JSONRPC: "2.0", Result: toolsResult},
	}
	responses = append(responses, extra...)
	return mcp.NewInMemoryTransport(responses...)
}

func TestProtocolRequestSerialization(t *testing.T) {
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  mcp.InitializeParams{ProtocolVersion: "2024-11-05"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON")
	}
}

func TestProtocolResponseDeserialization(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`
	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 1 {
		t.Errorf("id = %d, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Error("expected no error")
	}
}

func TestProtocolErrorDeserialization(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"invalid request"}}`
	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("code = %d, want -32600", resp.Error.Code)
	}
	if resp.Error.Error() != "invalid request" {
		t.Errorf("message = %q", resp.Error.Error())
	}
}

func TestInMemoryTransport(t *testing.T) {
	resp := mcp.JSONRPCResponse{JSONRPC: "2.0", Result: json.RawMessage(`"ok"`)}
	tr := mcp.NewInMemoryTransport(resp)

	ctx := context.Background()
	got, err := tr.Send(ctx, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 42, Method: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 {
		t.Errorf("id = %d, want 42", got.ID)
	}

	calls := tr.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Method != "test" {
		t.Errorf("method = %q, want test", calls[0].Method)
	}
}

func TestInMemoryTransportExhausted(t *testing.T) {
	tr := mcp.NewInMemoryTransport() // no responses
	_, err := tr.Send(context.Background(), mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "test"})
	if err == nil {
		t.Error("expected error when responses exhausted")
	}
}

func TestClientConnect(t *testing.T) {
	tools := []mcp.ToolInfo{
		{Name: "echo", Description: "Echo input"},
	}
	tr := newTestTransport(tools)

	client := mcp.NewClient()
	ctx := context.Background()

	if err := client.ConnectWithTransport(ctx, "test", tr); err != nil {
		t.Fatal(err)
	}

	servers := client.Servers()
	if len(servers) != 1 || servers[0] != "test" {
		t.Errorf("servers = %v, want [test]", servers)
	}
}

func TestClientConnectError(t *testing.T) {
	errResult := mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		Error:   &mcp.JSONRPCError{Code: -1, Message: "connection refused"},
	}
	tr := mcp.NewInMemoryTransport(errResult)

	client := mcp.NewClient()
	err := client.ConnectWithTransport(context.Background(), "fail", tr)
	if err == nil {
		t.Error("expected connect error")
	}
}

func TestClientToolDiscovery(t *testing.T) {
	tools := []mcp.ToolInfo{
		{Name: "search", Description: "Search things", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "fetch", Description: "Fetch URL"},
	}
	tr := newTestTransport(tools)

	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "web", tr); err != nil {
		t.Fatal(err)
	}

	discovered, err := client.Tools("web")
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 2 {
		t.Fatalf("tools = %d, want 2", len(discovered))
	}
	if discovered[0].Name != "search" {
		t.Errorf("tool[0] = %q, want search", discovered[0].Name)
	}

	all := client.AllTools()
	if len(all) != 2 {
		t.Errorf("allTools = %d, want 2", len(all))
	}
}

func TestClientToolCall(t *testing.T) {
	callResult, _ := json.Marshal(mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: "hello world"}},
	})
	tools := []mcp.ToolInfo{{Name: "echo", Description: "Echo"}}
	tr := newTestTransport(tools, mcp.JSONRPCResponse{JSONRPC: "2.0", Result: callResult})

	client := mcp.NewClient()
	ctx := context.Background()
	if err := client.ConnectWithTransport(ctx, "srv", tr); err != nil {
		t.Fatal(err)
	}

	result, err := client.Call(ctx, "srv", "echo", json.RawMessage(`{"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.TextContent() != "hello world" {
		t.Errorf("result = %q, want 'hello world'", result.TextContent())
	}
}

func TestClientToolCallError(t *testing.T) {
	errResp := mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		Error:   &mcp.JSONRPCError{Code: -1, Message: "tool failed"},
	}
	tools := []mcp.ToolInfo{{Name: "fail", Description: "Fails"}}
	tr := newTestTransport(tools, errResp)

	client := mcp.NewClient()
	ctx := context.Background()
	if err := client.ConnectWithTransport(ctx, "srv", tr); err != nil {
		t.Fatal(err)
	}

	_, err := client.Call(ctx, "srv", "fail", nil)
	if err == nil {
		t.Error("expected error from tool call")
	}
}

func TestClientDisconnect(t *testing.T) {
	tr := newTestTransport(nil)
	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "srv", tr); err != nil {
		t.Fatal(err)
	}

	if err := client.Disconnect("srv"); err != nil {
		t.Fatal(err)
	}

	if len(client.Servers()) != 0 {
		t.Error("expected no servers after disconnect")
	}

	// Disconnect again should error.
	if err := client.Disconnect("srv"); err == nil {
		t.Error("expected error for unknown server")
	}
}

func TestClientMultipleServers(t *testing.T) {
	tools1 := []mcp.ToolInfo{{Name: "a", Description: "A"}}
	tools2 := []mcp.ToolInfo{{Name: "b", Description: "B"}, {Name: "c", Description: "C"}}

	client := mcp.NewClient()
	ctx := context.Background()

	if err := client.ConnectWithTransport(ctx, "s1", newTestTransport(tools1)); err != nil {
		t.Fatal(err)
	}
	if err := client.ConnectWithTransport(ctx, "s2", newTestTransport(tools2)); err != nil {
		t.Fatal(err)
	}

	if len(client.Servers()) != 2 {
		t.Errorf("servers = %d, want 2", len(client.Servers()))
	}

	all := client.AllTools()
	if len(all) != 3 {
		t.Errorf("allTools = %d, want 3", len(all))
	}
}

func TestMCPToolBridge(t *testing.T) {
	callResult, _ := json.Marshal(mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: "bridged result"}},
	})
	tools := []mcp.ToolInfo{
		{Name: "greet", Description: "Say hello", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	tr := newTestTransport(tools, mcp.JSONRPCResponse{JSONRPC: "2.0", Result: callResult})

	client := mcp.NewClient()
	ctx := context.Background()
	if err := client.ConnectWithTransport(ctx, "demo", tr); err != nil {
		t.Fatal(err)
	}

	registry := tool.NewRegistry()
	mcp.RegisterMCPTools(registry, client)

	// Should have mcp.demo.greet + mcp.call.
	toolObj, ok := registry.Get("mcp.demo.greet")
	if !ok {
		t.Fatal("expected mcp.demo.greet to be registered")
	}
	if toolObj.Capability() != "mcp.call" {
		t.Errorf("capability = %q, want mcp.call", toolObj.Capability())
	}

	result, err := toolObj.Execute(ctx, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "bridged result" {
		t.Errorf("result = %q, want 'bridged result'", result)
	}
}

func TestMCPCallToolDynamic(t *testing.T) {
	callResult, _ := json.Marshal(mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: "dynamic call ok"}},
	})
	tools := []mcp.ToolInfo{{Name: "action", Description: "Do action"}}
	tr := newTestTransport(tools, mcp.JSONRPCResponse{JSONRPC: "2.0", Result: callResult})

	client := mcp.NewClient()
	ctx := context.Background()
	if err := client.ConnectWithTransport(ctx, "api", tr); err != nil {
		t.Fatal(err)
	}

	registry := tool.NewRegistry()
	mcp.RegisterMCPTools(registry, client)

	callTool, ok := registry.Get("mcp.call")
	if !ok {
		t.Fatal("expected mcp.call to be registered")
	}

	result, err := callTool.Execute(ctx, `{"server":"api","tool":"action","args":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "dynamic call ok" {
		t.Errorf("result = %q, want 'dynamic call ok'", result)
	}
}

func TestToolCallResultTextContent(t *testing.T) {
	r := mcp.ToolCallResult{
		Content: []mcp.ContentBlock{
			{Type: "text", Text: "part1"},
			{Type: "image", Text: ""},
			{Type: "text", Text: "part2"},
		},
	}
	got := r.TextContent()
	if got != "part1\npart2" {
		t.Errorf("TextContent = %q, want 'part1\\npart2'", got)
	}

	empty := mcp.ToolCallResult{}
	if empty.TextContent() != "" {
		t.Error("expected empty TextContent for empty result")
	}
}

func TestToolsNotConnected(t *testing.T) {
	client := mcp.NewClient()
	_, err := client.Tools("nonexistent")
	if err == nil {
		t.Error("expected error for not-connected server")
	}
}

func TestCallNotConnected(t *testing.T) {
	client := mcp.NewClient()
	_, err := client.Call(context.Background(), "nonexistent", "tool", nil)
	if err == nil {
		t.Error("expected error for not-connected server")
	}
}
