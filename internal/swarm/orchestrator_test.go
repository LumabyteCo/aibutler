package swarm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	swarmws "github.com/LumabyteCo/aibutler/internal/memory/swarm"
	"github.com/LumabyteCo/aibutler/internal/swarm"
	"github.com/LumabyteCo/aibutler/testutil"
)

// mockRunner implements swarm.TaskRunner for tests.
type mockRunner struct {
	fn func(ctx context.Context, task string) (string, error)
}

func (r *mockRunner) RunTask(ctx context.Context, task string) (string, error) {
	if r.fn != nil {
		return r.fn(ctx, task)
	}
	return "result: " + task, nil
}

// mockModel implements agent.ModelAdapter for tests.
type mockModel struct {
	response string
	err      error
}

func (m *mockModel) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	if m.err != nil {
		return agent.Response{}, m.err
	}
	return agent.Response{Content: m.response}, nil
}

func TestNewOrchestrator(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{}
	orch := swarm.New(db.Conn(), nil, nil, runner)
	if orch == nil {
		t.Fatal("expected non-nil orchestrator")
	}
}

func TestDecomposeNilModel(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{}
	orch := swarm.New(db.Conn(), nil, nil, runner)
	ctx := context.Background()

	plan, err := orch.Decompose(ctx, "do something")
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if plan.Goal != "do something" {
		t.Errorf("expected goal 'do something', got %q", plan.Goal)
	}
	if len(plan.Subtasks) != 1 {
		t.Fatalf("expected 1 subtask (fallback), got %d", len(plan.Subtasks))
	}
	if plan.Subtasks[0].ID != "sub-1" {
		t.Errorf("expected sub-1, got %s", plan.Subtasks[0].ID)
	}
}

func TestDecomposeWithModel(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{}
	jsonResp := `{"subtasks":[{"id":"sub-1","task":"search web","depends_on":[]},{"id":"sub-2","task":"summarize","depends_on":["sub-1"]}]}`
	model := &mockModel{response: jsonResp}
	orch := swarm.New(db.Conn(), model, nil, runner)
	ctx := context.Background()

	plan, err := orch.Decompose(ctx, "research and summarize topic")
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(plan.Subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(plan.Subtasks))
	}
	if plan.Subtasks[0].ID != "sub-1" {
		t.Errorf("expected sub-1 first")
	}
	if plan.Subtasks[1].DependsOn[0] != "sub-1" {
		t.Errorf("expected sub-2 to depend on sub-1")
	}
}

func TestDecomposeInvalidJSON(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{}
	model := &mockModel{response: "not json at all"}
	orch := swarm.New(db.Conn(), model, nil, runner)
	ctx := context.Background()

	// Should fall back to single subtask on bad JSON.
	plan, err := orch.Decompose(ctx, "test goal")
	if err != nil {
		t.Fatalf("Decompose with invalid JSON should not error: %v", err)
	}
	if len(plan.Subtasks) != 1 {
		t.Errorf("expected 1 fallback subtask, got %d", len(plan.Subtasks))
	}
}

func TestRunSingleSubtask(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{fn: func(ctx context.Context, task string) (string, error) {
		return "done: " + task, nil
	}}
	orch := swarm.New(db.Conn(), nil, nil, runner)
	ctx := context.Background()

	result, err := orch.Run(ctx, "run-1", "single task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "done: single task" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestRunMultipleSubtasks(t *testing.T) {
	db := testutil.TestDB(t)
	jsonPlan := `{"subtasks":[{"id":"sub-1","task":"task A"},{"id":"sub-2","task":"task B"}]}`
	modelObj := &mockModel{response: jsonPlan}
	var executed []string
	var execMu sync.Mutex
	runner := &mockRunner{fn: func(ctx context.Context, task string) (string, error) {
		execMu.Lock()
		executed = append(executed, task)
		execMu.Unlock()
		return "ok: " + task, nil
	}}
	orch := swarm.New(db.Conn(), modelObj, nil, runner)
	ctx := context.Background()

	_, err := orch.Run(ctx, "run-parallel", "do A and B")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	execMu.Lock()
	execLen := len(executed)
	execMu.Unlock()
	if execLen != 2 {
		t.Errorf("expected 2 tasks executed, got %d", execLen)
	}
}

func TestRunWithDependencies(t *testing.T) {
	db := testutil.TestDB(t)
	// sub-2 depends on sub-1, so sub-1 must run first.
	jsonPlan := `{"subtasks":[{"id":"sub-1","task":"step1"},{"id":"sub-2","task":"step2","depends_on":["sub-1"]}]}`
	model := &mockModel{response: jsonPlan}
	var order []string
	runner := &mockRunner{fn: func(ctx context.Context, task string) (string, error) {
		order = append(order, task)
		return "done: " + task, nil
	}}
	orch := swarm.New(db.Conn(), model, nil, runner)
	ctx := context.Background()

	_, err := orch.Run(ctx, "run-deps", "sequential goal")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(order))
	}
	if order[0] != "step1" {
		t.Errorf("expected step1 first, got %q", order[0])
	}
}

func TestAggregateNilModel(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{fn: func(ctx context.Context, task string) (string, error) {
		return task + "-result", nil
	}}
	// Use model that returns multiple subtasks.
	jsonPlan := `{"subtasks":[{"id":"sub-1","task":"t1"},{"id":"sub-2","task":"t2"}]}`
	model := &mockModel{response: jsonPlan}
	// Aggregation model is nil.
	orch := swarm.New(db.Conn(), model, nil, runner)
	ctx := context.Background()

	result, err := orch.Run(ctx, "run-agg", "aggregate test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// With nil model for aggregation, results should be joined.
	if result == "" {
		t.Error("expected non-empty aggregated result")
	}
}

func TestAggregateWithModel(t *testing.T) {
	db := testutil.TestDB(t)
	var countingModelObj = &countingModel{
		responses: []string{
			`{"subtasks":[{"id":"sub-1","task":"t1"},{"id":"sub-2","task":"t2"}]}`,
			"Final synthesized answer",
		},
	}
	runner := &mockRunner{fn: func(ctx context.Context, task string) (string, error) {
		return "result for " + task, nil
	}}
	orch := swarm.New(db.Conn(), countingModelObj, nil, runner)
	ctx := context.Background()

	result, err := orch.Run(ctx, "run-synth", "synthesis goal")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "Final synthesized answer" {
		t.Errorf("expected synthesized answer, got %q", result)
	}
}

type countingModel struct {
	responses []string
	callIdx   int
	mu        sync.Mutex
}

func (m *countingModel) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIdx >= len(m.responses) {
		return agent.Response{Content: "fallback"}, nil
	}
	resp := agent.Response{Content: m.responses[m.callIdx]}
	m.callIdx++
	return resp, nil
}

func TestWithTraceID(t *testing.T) {
	ctx := context.Background()
	ctx = swarm.WithTraceID(ctx, "trace-123")

	db := testutil.TestDB(t)
	runner := &mockRunner{}
	orch := swarm.New(db.Conn(), nil, nil, runner)

	// Run with trace ID in context.
	_, err := orch.Run(ctx, "run-trace", "traced goal")
	if err != nil {
		t.Fatalf("Run with trace: %v", err)
	}

	// Verify the run was recorded.
	var traceID string
	_ = db.Conn().QueryRowContext(ctx, `SELECT COALESCE(trace_id,'') FROM swarm_runs WHERE run_id = ?`, "run-trace").Scan(&traceID)
	if traceID != "trace-123" {
		t.Errorf("expected trace_id 'trace-123', got %q", traceID)
	}
}

func TestSwarmTool(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{}
	orch := swarm.New(db.Conn(), nil, nil, runner)
	ws := swarmws.NewWorkspace(db.Conn())

	name, desc, schema, cap, exec := swarm.NewSwarmTool(orch, ws)

	if name != "agent.swarm" {
		t.Errorf("expected name 'agent.swarm', got %q", name)
	}
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if cap != "agent.swarm" {
		t.Errorf("expected cap 'agent.swarm', got %q", cap)
	}

	// Validate schema is valid JSON.
	var schemaObj map[string]interface{}
	if err := json.Unmarshal([]byte(schema), &schemaObj); err != nil {
		t.Errorf("schema is not valid JSON: %v", err)
	}

	// Test execution.
	input := `{"goal":"test goal","run_id":"test-run"}`
	result, err := exec(context.Background(), input)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
	_ = fmt.Sprintf("result: %s", result)
	_ = strings.Contains(result, "")
}

func TestSwarmToolMissingGoal(t *testing.T) {
	db := testutil.TestDB(t)
	runner := &mockRunner{}
	orch := swarm.New(db.Conn(), nil, nil, runner)
	ws := swarmws.NewWorkspace(db.Conn())

	_, _, _, _, exec := swarm.NewSwarmTool(orch, ws)

	_, err := exec(context.Background(), `{"run_id":"test"}`)
	if err == nil {
		t.Fatal("expected error for missing goal")
	}
	if !strings.Contains(err.Error(), "goal is required") {
		t.Errorf("expected 'goal is required' error, got: %v", err)
	}
}
