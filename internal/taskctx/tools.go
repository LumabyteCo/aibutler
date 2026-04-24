package taskctx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterTaskContextTools registers task context save/load tools.
func RegisterTaskContextTools(registry *tool.Registry, store *Store) {
	registry.Register(&taskContextSaveTool{store: store})
	registry.Register(&taskContextLoadTool{store: store})
}

// --- task.context.save ---

type taskContextSaveTool struct{ store *Store }

func (t *taskContextSaveTool) Name() string        { return "task.context.save" }
func (t *taskContextSaveTool) Description() string  { return "Save multi-step task state for continuation" }
func (t *taskContextSaveTool) Capability() string   { return "data.tasks.write" }
func (t *taskContextSaveTool) Schema() string {
	return `{"type":"object","properties":{"session_id":{"type":"string"},"task_type":{"type":"string"},"state":{"type":"string"},"context":{"type":"object"}},"required":["session_id","task_type","context"]}`
}

func (t *taskContextSaveTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		SessionID string                 `json:"session_id"`
		TaskType  string                 `json:"task_type"`
		State     string                 `json:"state"`
		Context   map[string]interface{} `json:"context"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("task.context.save: invalid input: %w", err)
	}
	if args.State == "" {
		args.State = "gathering"
	}

	id, err := t.store.Save(ctx, &TaskContext{
		SessionID: args.SessionID,
		TaskType:  args.TaskType,
		State:     args.State,
		Context:   args.Context,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Task context saved (id: %d)", id), nil
}

// --- task.context.load ---

type taskContextLoadTool struct{ store *Store }

func (t *taskContextLoadTool) Name() string        { return "task.context.load" }
func (t *taskContextLoadTool) Description() string  { return "Load the active task context for a session" }
func (t *taskContextLoadTool) Capability() string   { return "data.tasks.read" }
func (t *taskContextLoadTool) Schema() string {
	return `{"type":"object","properties":{"session_id":{"type":"string"}},"required":["session_id"]}`
}

func (t *taskContextLoadTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("task.context.load: invalid input: %w", err)
	}

	tc, err := t.store.Load(ctx, args.SessionID)
	if err != nil {
		return "", err
	}
	if tc == nil {
		return "No active task context", nil
	}

	type result struct {
		ID       int                    `json:"id"`
		TaskType string                 `json:"task_type"`
		State    string                 `json:"state"`
		Context  map[string]interface{} `json:"context"`
	}

	out, _ := json.Marshal(result{
		ID:       tc.ID,
		TaskType: tc.TaskType,
		State:    tc.State,
		Context:  tc.Context,
	})
	return string(out), nil
}
