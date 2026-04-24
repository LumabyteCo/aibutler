package taskctx_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/taskctx"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestTaskContextSaveAndLoad(t *testing.T) {
	db := testutil.TestDB(t)
	store := taskctx.NewStore(db.Conn())
	ctx := context.Background()

	// Save.
	id, err := store.Save(ctx, &taskctx.TaskContext{
		SessionID: "sess-1",
		TaskType:  "booking",
		State:     "gathering",
		Context:   map[string]interface{}{"step": "destination"},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// Load.
	tc, err := store.Load(ctx, "sess-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tc == nil {
		t.Fatal("expected non-nil task context")
	}
	if tc.TaskType != "booking" {
		t.Errorf("task_type = %q, want booking", tc.TaskType)
	}
	if tc.Context["step"] != "destination" {
		t.Errorf("context[step] = %v", tc.Context["step"])
	}
}

func TestTaskContextOneActivePerSession(t *testing.T) {
	db := testutil.TestDB(t)
	store := taskctx.NewStore(db.Conn())
	ctx := context.Background()

	// Save first context.
	store.Save(ctx, &taskctx.TaskContext{
		SessionID: "sess-1",
		TaskType:  "task-A",
		State:     "gathering",
		Context:   map[string]interface{}{"a": true},
	})

	// Save second — should abandon first.
	store.Save(ctx, &taskctx.TaskContext{
		SessionID: "sess-1",
		TaskType:  "task-B",
		State:     "processing",
		Context:   map[string]interface{}{"b": true},
	})

	// Load should return task-B.
	tc, err := store.Load(ctx, "sess-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tc.TaskType != "task-B" {
		t.Errorf("task_type = %q, want task-B (old should be abandoned)", tc.TaskType)
	}
}

func TestTaskContextExpiry(t *testing.T) {
	db := testutil.TestDB(t)
	store := taskctx.NewStore(db.Conn())
	ctx := context.Background()

	// Save with already-expired time.
	expired := time.Now().UTC().Add(-1 * time.Hour)
	store.Save(ctx, &taskctx.TaskContext{
		SessionID: "sess-1",
		TaskType:  "expired-task",
		State:     "gathering",
		Context:   map[string]interface{}{},
		ExpiresAt: &expired,
	})

	// Load should return nil (expired).
	tc, err := store.Load(ctx, "sess-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if tc != nil {
		t.Errorf("expected nil for expired context, got %v", tc)
	}
}

func TestTaskContextComplete(t *testing.T) {
	db := testutil.TestDB(t)
	store := taskctx.NewStore(db.Conn())
	ctx := context.Background()

	id, _ := store.Save(ctx, &taskctx.TaskContext{
		SessionID: "sess-1",
		TaskType:  "task",
		State:     "gathering",
		Context:   map[string]interface{}{},
	})

	store.Complete(ctx, id)

	tc, _ := store.Load(ctx, "sess-1")
	if tc != nil {
		t.Error("expected nil after completion")
	}
}

func TestTaskContextMultiStepUpdate(t *testing.T) {
	db := testutil.TestDB(t)
	store := taskctx.NewStore(db.Conn())
	ctx := context.Background()

	// Step 1.
	id, _ := store.Save(ctx, &taskctx.TaskContext{
		SessionID: "sess-1",
		TaskType:  "flight",
		State:     "gathering",
		Context:   map[string]interface{}{"from": "LAX"},
	})

	// Step 2: update same context.
	store.Save(ctx, &taskctx.TaskContext{
		ID:        id,
		SessionID: "sess-1",
		TaskType:  "flight",
		State:     "processing",
		Context:   map[string]interface{}{"from": "LAX", "to": "JFK"},
	})

	// Load should have updated state.
	tc, _ := store.Load(ctx, "sess-1")
	if tc.State != "processing" {
		t.Errorf("state = %q, want processing", tc.State)
	}
	if tc.Context["to"] != "JFK" {
		t.Errorf("context[to] = %v, want JFK", tc.Context["to"])
	}
}

// --- Tool Tests ---

func TestTaskContextTools(t *testing.T) {
	db := testutil.TestDB(t)
	store := taskctx.NewStore(db.Conn())

	reg := tool.NewRegistry()
	taskctx.RegisterTaskContextTools(reg, store)
	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)
	ctx := context.Background()

	// Save via tool.
	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "task.context.save",
		Input: `{"session_id":"s1","task_type":"order","context":{"item":"pizza"}}`,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Load via tool.
	result, err = disp.Execute(ctx, agent.ToolCall{
		Name:  "task.context.load",
		Input: `{"session_id":"s1"}`,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var loaded struct {
		TaskType string                 `json:"task_type"`
		Context  map[string]interface{} `json:"context"`
	}
	json.Unmarshal([]byte(result), &loaded)
	if loaded.TaskType != "order" {
		t.Errorf("task_type = %q, want order", loaded.TaskType)
	}
}
