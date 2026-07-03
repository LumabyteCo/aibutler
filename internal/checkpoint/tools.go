package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterCheckpointTools exposes checkpoint listing and restore to the agent.
func RegisterCheckpointTools(registry *tool.Registry, store *Store) {
	registry.Register(&listTool{store: store})
	registry.Register(&restoreTool{store: store})
}

type listTool struct{ store *Store }

func (t *listTool) Name() string { return "checkpoint.list" }
func (t *listTool) Description() string {
	return "List recent file checkpoints — pre-images captured before agent file mutations. Each entry shows what file changed, which tool changed it, when, and whether it can be restored."
}
func (t *listTool) Capability() string { return "tool.checkpoint.read" }
func (t *listTool) Schema() string {
	return `{"type":"object","properties":{"limit":{"type":"integer","description":"Max entries (default 50)"}}}`
}

func (t *listTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal([]byte(input), &args)
	cps, err := t.store.List(ctx, args.Limit)
	if err != nil {
		return "", err
	}
	if len(cps) == 0 {
		return "No checkpoints recorded.", nil
	}
	data, _ := json.Marshal(cps)
	return string(data), nil
}

type restoreTool struct{ store *Store }

func (t *restoreTool) Name() string { return "checkpoint.restore" }
func (t *restoreTool) Description() string {
	return "Restore a file to its recorded pre-mutation state by checkpoint id (see checkpoint.list). The current state is checkpointed first, so a restore can itself be undone. Confirm with the user before calling."
}
func (t *restoreTool) Capability() string { return "tool.checkpoint.restore" }
func (t *restoreTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer","description":"Checkpoint id to restore"}},"required":["id"]}`
}

func (t *restoreTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil || args.ID == 0 {
		return "", fmt.Errorf("checkpoint.restore: id is required")
	}
	return t.store.Restore(ctx, args.ID)
}
