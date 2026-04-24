package agent

import (
	"context"
	"testing"
)

func TestStreamEventCreation(t *testing.T) {
	tests := []struct {
		name  string
		event StreamEvent
		want  string
	}{
		{
			name:  "text_delta",
			event: StreamEvent{Type: "text_delta", Text: "Hello"},
			want:  "text_delta",
		},
		{
			name:  "tool_use_start",
			event: StreamEvent{Type: "tool_use_start", ToolCallID: "call_1", ToolName: "task.add"},
			want:  "tool_use_start",
		},
		{
			name:  "input_json_delta",
			event: StreamEvent{Type: "input_json_delta", ToolCallID: "call_1", PartialJSON: `{"content"`},
			want:  "input_json_delta",
		},
		{
			name:  "thinking_delta",
			event: StreamEvent{Type: "thinking_delta", Text: "Let me think..."},
			want:  "thinking_delta",
		},
		{
			name:  "usage",
			event: StreamEvent{Type: "usage", TokensIn: 100, TokensOut: 50},
			want:  "usage",
		},
		{
			name:  "message_stop",
			event: StreamEvent{Type: "message_stop"},
			want:  "message_stop",
		},
		{
			name:  "error",
			event: StreamEvent{Type: "error", Error: context.Canceled},
			want:  "error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.event.Type != tc.want {
				t.Errorf("event.Type = %q, want %q", tc.event.Type, tc.want)
			}
		})
	}
}

func TestStreamingModelAdapterTypeAssertion(t *testing.T) {
	// Verify that a non-streaming adapter does not satisfy StreamingModelAdapter.
	var adapter ModelAdapter = &nonStreamingAdapter{}
	_, ok := adapter.(StreamingModelAdapter)
	if ok {
		t.Error("non-streaming adapter should not satisfy StreamingModelAdapter")
	}

	// Verify that a streaming adapter does satisfy StreamingModelAdapter.
	var streamAdapter ModelAdapter = &fakeStreamAdapter{}
	s, ok := streamAdapter.(StreamingModelAdapter)
	if !ok {
		t.Fatal("streaming adapter should satisfy StreamingModelAdapter")
	}

	ch, err := s.CompleteStream(context.Background(), nil)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	events := drain(ch)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != "text_delta" {
		t.Errorf("event[0].Type = %q, want 'text_delta'", events[0].Type)
	}
	if events[1].Type != "message_stop" {
		t.Errorf("event[1].Type = %q, want 'message_stop'", events[1].Type)
	}
}

// --- test helpers ---

type nonStreamingAdapter struct{}

func (a *nonStreamingAdapter) Complete(_ context.Context, _ []Message) (Response, error) {
	return Response{Content: "hello"}, nil
}

type fakeStreamAdapter struct{}

func (a *fakeStreamAdapter) Complete(_ context.Context, _ []Message) (Response, error) {
	return Response{Content: "hello"}, nil
}

func (a *fakeStreamAdapter) CompleteStream(_ context.Context, _ []Message) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 2)
	ch <- StreamEvent{Type: "text_delta", Text: "hello"}
	ch <- StreamEvent{Type: "message_stop"}
	close(ch)
	return ch, nil
}

func drain(ch <-chan StreamEvent) []StreamEvent {
	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	return events
}
