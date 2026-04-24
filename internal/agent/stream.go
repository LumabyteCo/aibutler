package agent

import "context"

// StreamEvent represents a single event in a streaming LLM response.
type StreamEvent struct {
	Type string // "text_delta", "tool_use_start", "input_json_delta", "thinking_delta", "usage", "message_stop", "error"

	// Text content (for text_delta, thinking_delta)
	Text string

	// Tool use fields (for tool_use_start, input_json_delta)
	ToolCallID  string
	ToolName    string
	PartialJSON string

	// Usage fields (for usage event)
	TokensIn  int
	TokensOut int

	// Error (for error event)
	Error error
}

// StreamingModelAdapter extends ModelAdapter with streaming capability.
// Adapters that support streaming implement this interface.
type StreamingModelAdapter interface {
	ModelAdapter
	CompleteStream(ctx context.Context, messages []Message) (<-chan StreamEvent, error)
}
