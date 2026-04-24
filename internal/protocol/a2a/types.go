package a2a

import (
	"context"
	"encoding/json"
)

// AgentCard is the A2A agent discovery descriptor.
type AgentCard struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	URL          string       `json:"url"`
	Capabilities []string     `json:"capabilities"`
	Version      string       `json:"version"`
	Skills       []AgentSkill `json:"skills,omitempty"`
	AuthSchemes  []string     `json:"auth_schemes,omitempty"`
	Streaming    bool         `json:"streaming"`
}

// TaskRequest is an inbound delegation request.
type TaskRequest struct {
	ID       string          `json:"id"`
	Task     string          `json:"task"`
	Context  json.RawMessage `json:"context,omitempty"`
	Messages []A2AMessage    `json:"messages,omitempty"`
	TraceID  string          `json:"trace_id,omitempty"`
}

// TaskResult is the response to a delegation.
type TaskResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // completed | failed | pending
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// A2AMessagePart is a single part in an A2A v2 multi-part message.
type A2AMessagePart struct {
	Text    string          `json:"text,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	FileURL string          `json:"file_url,omitempty"`
}

// A2AMessage is a single turn in an A2A v2 multi-turn conversation.
type A2AMessage struct {
	Role  string           `json:"role"` // "user" | "assistant"
	Parts []A2AMessagePart `json:"parts"`
}

// TaskLifecycleState describes the lifecycle state of a delegated task.
type TaskLifecycleState string

const (
	TaskSubmitted     TaskLifecycleState = "submitted"
	TaskWorking       TaskLifecycleState = "working"
	TaskInputRequired TaskLifecycleState = "input_required"
	TaskCompleted     TaskLifecycleState = "completed"
	TaskFailed        TaskLifecycleState = "failed"
	TaskCanceled      TaskLifecycleState = "canceled"
)

// TaskStatusResponse is returned by GET /a2a/tasks/{id}.
type TaskStatusResponse struct {
	ID             string             `json:"id"`
	LifecycleState TaskLifecycleState `json:"lifecycle_state"`
	Output         string             `json:"output,omitempty"`
	Error          string             `json:"error,omitempty"`
	CreatedAt      string             `json:"created_at,omitempty"`
	CompletedAt    string             `json:"completed_at,omitempty"`
}

// AgentSkill describes a named skill that an agent offers.
type AgentSkill struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// traceIDKey is the context key for swarm trace IDs.
type traceIDKey struct{}

// WithTraceID attaches a trace ID to the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext returns the trace ID from the context, or "".
func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}
