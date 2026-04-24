//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/mcp"
)

// newMCPTransport builds an InMemoryTransport with initialize + tools/list + extra responses.
func newMCPTransport(tools []mcp.ToolInfo, callResponses ...mcp.JSONRPCResponse) *mcp.InMemoryTransport {
	// Response 1: initialize handshake.
	initResult, _ := json.Marshal(mcp.InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      mcp.ServerInfo{Name: "test-server", Version: "1.0"},
	})

	// Response 2: tools/list.
	toolListResult, _ := json.Marshal(mcp.ToolListResult{Tools: tools})

	responses := []mcp.JSONRPCResponse{
		{JSONRPC: "2.0", Result: initResult},
		{JSONRPC: "2.0", Result: toolListResult},
	}
	responses = append(responses, callResponses...)

	return mcp.NewInMemoryTransport(responses...)
}

// mcpCallResult builds a JSONRPCResponse wrapping a successful tool call result.
func mcpCallResult(text string) mcp.JSONRPCResponse {
	result, _ := json.Marshal(mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: text}},
	})
	return mcp.JSONRPCResponse{JSONRPC: "2.0", Result: result}
}

// mcpErrorResult builds a JSONRPCResponse wrapping an error tool call result.
func mcpErrorResult(text string) mcp.JSONRPCResponse {
	result, _ := json.Marshal(mcp.ToolCallResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: text}},
		IsError: true,
	})
	return mcp.JSONRPCResponse{JSONRPC: "2.0", Result: result}
}

// TestE2EMCPToolCall verifies that an MCP tool registered as mcp.<server>.<tool>
// can be called through the full pipeline.
func TestE2EMCPToolCall(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			toolCallResponse("Calling MCP tool.",
				tc("mcp1", "mcp.demo.echo", `{"text":"hello world"}`),
			),
			finalResponse("MCP tool returned: hello world"),
		},
	})

	// Create MCP client with fake transport.
	tools := []mcp.ToolInfo{
		{Name: "echo", Description: "Echo input back", InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)},
	}
	tr := newMCPTransport(tools, mcpCallResult("hello world"))

	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "demo", tr); err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	mcp.RegisterMCPTools(p.Registry, client)

	p.sendMsg(t, "Echo hello world via MCP")

	// Verify model was called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result fed back.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "hello world") {
			found = true
			break
		}
	}
	if !found {
		t.Error("MCP tool result should contain 'hello world'")
	}

	resp := p.lastResponse(t)
	if resp != "MCP tool returned: hello world" {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EMCPDynamicCall verifies the mcp.call generic tool works.
func TestE2EMCPDynamicCall(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			toolCallResponse("Calling MCP dynamically.",
				tc("mcp2", "mcp.call", `{"server":"tools","tool":"calculate","args":{"expression":"2+2"}}`),
			),
			finalResponse("Result is 4."),
		},
	})

	tools := []mcp.ToolInfo{
		{Name: "calculate", Description: "Evaluate math", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	tr := newMCPTransport(tools, mcpCallResult("4"))

	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "tools", tr); err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	mcp.RegisterMCPTools(p.Registry, client)

	p.sendMsg(t, "Calculate 2+2 via MCP")

	// Verify tool result.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "4") {
			found = true
			break
		}
	}
	if !found {
		t.Error("mcp.call result should contain '4'")
	}

	resp := p.lastResponse(t)
	if resp != "Result is 4." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EMCPToolDiscovery verifies that MCP tools appear in the registry
// with the correct mcp.<server>.<tool> naming.
func TestE2EMCPToolDiscovery(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Verified."),
		},
	})

	tools := []mcp.ToolInfo{
		{Name: "greet", Description: "Say hello", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "farewell", Description: "Say goodbye", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	tr := newMCPTransport(tools) // No call responses needed.

	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "social", tr); err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	mcp.RegisterMCPTools(p.Registry, client)

	// Verify tools are registered with correct names.
	if _, ok := p.Registry.Get("mcp.social.greet"); !ok {
		t.Error("mcp.social.greet should be registered")
	}
	if _, ok := p.Registry.Get("mcp.social.farewell"); !ok {
		t.Error("mcp.social.farewell should be registered")
	}
	// mcp.call (generic) should also be registered.
	if _, ok := p.Registry.Get("mcp.call"); !ok {
		t.Error("mcp.call should be registered")
	}
}

// TestE2EMCPToolError verifies that an MCP tool error is handled gracefully.
func TestE2EMCPToolError(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			toolCallResponse("Calling failing tool.",
				tc("mcp3", "mcp.demo.fail", `{}`),
			),
			finalResponse("The MCP tool encountered an error."),
		},
	})

	tools := []mcp.ToolInfo{
		{Name: "fail", Description: "Always fails", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	tr := newMCPTransport(tools, mcpErrorResult("something went wrong"))

	client := mcp.NewClient()
	if err := client.ConnectWithTransport(context.Background(), "demo", tr); err != nil {
		t.Fatalf("connect MCP: %v", err)
	}
	mcp.RegisterMCPTools(p.Registry, client)

	p.sendMsg(t, "Call the failing MCP tool")

	// The tool error should be fed back to the model.
	calls := p.Fake.Calls()
	if len(calls) < 2 {
		t.Fatalf("model calls = %d, want >= 2", len(calls))
	}

	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "something went wrong") {
			found = true
			break
		}
	}
	if !found {
		t.Error("MCP tool error should be fed back to model")
	}

	resp := p.lastResponse(t)
	if resp != "The MCP tool encountered an error." {
		t.Errorf("response = %q", resp)
	}
}
