package model

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

func TestCollectStreamResponse_TextOnly(t *testing.T) {
	ch := make(chan agent.StreamEvent, 10)
	ch <- agent.StreamEvent{Type: "text_delta", Text: "Hello"}
	ch <- agent.StreamEvent{Type: "text_delta", Text: " world"}
	ch <- agent.StreamEvent{Type: "usage", TokensIn: 10, TokensOut: 5}
	ch <- agent.StreamEvent{Type: "message_stop"}
	close(ch)

	resp := CollectStreamResponse(ch)

	if resp.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello world")
	}
	if resp.TokensIn != 10 {
		t.Errorf("TokensIn = %d, want 10", resp.TokensIn)
	}
	if resp.TokensOut != 5 {
		t.Errorf("TokensOut = %d, want 5", resp.TokensOut)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestCollectStreamResponse_WithToolCalls(t *testing.T) {
	ch := make(chan agent.StreamEvent, 10)
	ch <- agent.StreamEvent{Type: "tool_use_start", ToolCallID: "tc1", ToolName: "search"}
	ch <- agent.StreamEvent{Type: "input_json_delta", ToolCallID: "tc1", PartialJSON: `{"query"`}
	ch <- agent.StreamEvent{Type: "input_json_delta", ToolCallID: "tc1", PartialJSON: `:"test"}`}
	ch <- agent.StreamEvent{Type: "usage", TokensIn: 20, TokensOut: 15}
	ch <- agent.StreamEvent{Type: "message_stop"}
	close(ch)

	resp := CollectStreamResponse(ch)

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "tc1" {
		t.Errorf("ToolCall ID = %q, want %q", tc.ID, "tc1")
	}
	if tc.Name != "search" {
		t.Errorf("ToolCall Name = %q, want %q", tc.Name, "search")
	}
	if tc.Input != `{"query":"test"}` {
		t.Errorf("ToolCall Input = %q, want %q", tc.Input, `{"query":"test"}`)
	}
}

func TestCollectStreamResponse_MultipleUsageEvents(t *testing.T) {
	ch := make(chan agent.StreamEvent, 10)
	ch <- agent.StreamEvent{Type: "usage", TokensIn: 5, TokensOut: 3}
	ch <- agent.StreamEvent{Type: "usage", TokensIn: 5, TokensOut: 7}
	ch <- agent.StreamEvent{Type: "message_stop"}
	close(ch)

	resp := CollectStreamResponse(ch)

	if resp.TokensIn != 10 {
		t.Errorf("TokensIn = %d, want 10", resp.TokensIn)
	}
	if resp.TokensOut != 10 {
		t.Errorf("TokensOut = %d, want 10", resp.TokensOut)
	}
}
