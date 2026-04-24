package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestDelegateToolCapabilitySubset(t *testing.T) {
	parentCaps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.shell.exec"},
		{Resource: "tool.web.search"},
	})

	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "done"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    parentCaps,
		Timeout: 10 * time.Second,
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	// Valid subset — should succeed.
	result, err := exec(context.Background(), `{"task":"sub task","capabilities":["tool.shell.exec"]}`)
	if err != nil {
		t.Fatalf("valid subset: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	// Invalid subset — "data.health.read" not in parent.
	_, err = exec(context.Background(), `{"task":"sub task","capabilities":["data.health.read"]}`)
	if err == nil {
		t.Error("expected error for capability not in parent")
	}
}

func TestDelegateToolBudgetCap(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:             testutil.NewFakeModel(agent.Response{Content: "done"}),
		Tools:             testutil.NewFakeToolExecutor(nil),
		Caps:              capability.NewCapabilitySet(nil),
		Timeout:           10 * time.Second,
		PerSubagentBudget: 0.50,
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	result, err := exec(context.Background(), `{"task":"budgeted task"}`)
	if err != nil {
		t.Fatalf("budget cap: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed", out["status"])
	}
}

func TestDelegateToolMaxCostParam(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "done"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    capability.NewCapabilitySet(nil),
		Timeout: 10 * time.Second,
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	// Pass explicit max_cost in parameters.
	result, err := exec(context.Background(), `{"task":"costly task","max_cost":1.00}`)
	if err != nil {
		t.Fatalf("max_cost: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestDelegateToolSemaphore(t *testing.T) {
	sem := agent.NewSemaphore(1)

	cfg := agent.DelegateConfig{
		Model:     testutil.NewFakeModel(agent.Response{Content: "done"}),
		Tools:     testutil.NewFakeToolExecutor(nil),
		Caps:      capability.NewCapabilitySet(nil),
		Timeout:   10 * time.Second,
		Semaphore: sem,
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	result, err := exec(context.Background(), `{"task":"semaphore task"}`)
	if err != nil {
		t.Fatalf("semaphore: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestDelegateToolNestingDepthLimit(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:        testutil.NewFakeModel(agent.Response{Content: "done"}),
		Tools:        testutil.NewFakeToolExecutor(nil),
		Caps:         capability.NewCapabilitySet(nil),
		Timeout:      10 * time.Second,
		MaxDepth:     3,
		CurrentDepth: 3, // At max depth
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	_, err := exec(context.Background(), `{"task":"too deep"}`)
	if err == nil {
		t.Error("expected error for max nesting depth")
	}
}

func TestSpawnToolSemaphore(t *testing.T) {
	sem := agent.NewSemaphore(2)

	cfg := agent.DelegateConfig{
		Model:     testutil.NewFakeModel(agent.Response{Content: "bg done"}),
		Tools:     testutil.NewFakeToolExecutor(nil),
		Caps:      capability.NewCapabilitySet(nil),
		Timeout:   5 * time.Second,
		Semaphore: sem,
	}
	_, _, _, _, exec := agent.NewSpawnTool(cfg)

	result, err := exec(context.Background(), `{"task":"bg with semaphore"}`)
	if err != nil {
		t.Fatalf("spawn with semaphore: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out["status"] != "spawned" {
		t.Errorf("status = %v, want spawned", out["status"])
	}
	// Let background goroutine finish.
	time.Sleep(100 * time.Millisecond)
}

func TestSpawnToolNestingDepthLimit(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:        testutil.NewFakeModel(),
		Tools:        testutil.NewFakeToolExecutor(nil),
		MaxDepth:     2,
		CurrentDepth: 2,
	}
	_, _, _, _, exec := agent.NewSpawnTool(cfg)

	_, err := exec(context.Background(), `{"task":"too deep spawn"}`)
	if err == nil {
		t.Error("expected error for max nesting depth in spawn")
	}
}

func TestStatusToolNoDB(t *testing.T) {
	_, _, _, _, exec := agent.NewStatusTool(nil)

	result, err := exec(context.Background(), `{"agent_id":"test-123"}`)
	if err != nil {
		t.Fatalf("status no db: %v", err)
	}
	if result != `{"error":"no database configured"}` {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestStatusToolEmptyID(t *testing.T) {
	_, _, _, _, exec := agent.NewStatusTool(nil)

	_, err := exec(context.Background(), `{"agent_id":""}`)
	if err == nil {
		t.Error("expected error for empty agent_id")
	}
}

func TestStatusToolInvalidJSON(t *testing.T) {
	_, _, _, _, exec := agent.NewStatusTool(nil)

	_, err := exec(context.Background(), `not json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCancelToolNoDB(t *testing.T) {
	_, _, _, _, exec := agent.NewCancelTool(nil)

	result, err := exec(context.Background(), `{"agent_id":"test-123"}`)
	if err != nil {
		t.Fatalf("cancel no db: %v", err)
	}
	if result != `{"error":"no database configured"}` {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestCancelToolEmptyID(t *testing.T) {
	_, _, _, _, exec := agent.NewCancelTool(nil)

	_, err := exec(context.Background(), `{"agent_id":""}`)
	if err == nil {
		t.Error("expected error for empty agent_id")
	}
}

func TestListBackgroundToolNoDB(t *testing.T) {
	_, _, _, _, exec := agent.NewListBackgroundTool(nil)

	result, err := exec(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("list no db: %v", err)
	}
	if result != `{"error":"no database configured"}` {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestStatusToolWithDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Insert a session for FK.
	conn.ExecContext(ctx, `INSERT INTO sessions (id, channel, account_id) VALUES ('sess-1', 'terminal', 'user-1')`)

	// Insert an agent record.
	_, err := conn.ExecContext(ctx,
		`INSERT INTO agents (id, session_id, type, task, state, model, capabilities, created_at, updated_at)
		 VALUES ('test-agent-1', 'sess-1', 'subagent', 'analyze data', 'completed', 'test-model', '[]', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	_, _, _, _, exec := agent.NewStatusTool(conn)
	result, err := exec(ctx, `{"agent_id":"test-agent-1"}`)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed", out["status"])
	}
	if out["task"] != "analyze data" {
		t.Errorf("task = %v, want 'analyze data'", out["task"])
	}
}

func TestStatusToolNotFound(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()

	_, _, _, _, exec := agent.NewStatusTool(conn)
	result, err := exec(context.Background(), `{"agent_id":"nonexistent"}`)
	if err != nil {
		t.Fatalf("status not found: %v", err)
	}
	if result != `{"error":"agent not found"}` {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestCancelToolWithDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Insert a session for FK.
	conn.ExecContext(ctx, `INSERT INTO sessions (id, channel, account_id) VALUES ('sess-1', 'terminal', 'user-1')`)

	// Insert a running agent.
	_, err := conn.ExecContext(ctx,
		`INSERT INTO agents (id, session_id, type, task, state, model, capabilities, created_at, updated_at)
		 VALUES ('cancel-me', 'sess-1', 'background', 'long task', 'running', 'test-model', '[]', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, _, _, _, exec := agent.NewCancelTool(conn)
	result, err := exec(ctx, `{"agent_id":"cancel-me"}`)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	var out map[string]interface{}
	json.Unmarshal([]byte(result), &out)
	if out["status"] != "cancelled" {
		t.Errorf("status = %v, want cancelled", out["status"])
	}

	// Verify the agent state changed.
	var state string
	conn.QueryRowContext(ctx, "SELECT state FROM agents WHERE id = 'cancel-me'").Scan(&state)
	if state != "cancelled" {
		t.Errorf("db state = %q, want cancelled", state)
	}
}

func TestCancelToolAlreadyTerminal(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Insert a session for FK.
	conn.ExecContext(ctx, `INSERT INTO sessions (id, channel, account_id) VALUES ('sess-1', 'terminal', 'user-1')`)

	// Insert a completed agent.
	_, err := conn.ExecContext(ctx,
		`INSERT INTO agents (id, session_id, type, task, state, model, capabilities, created_at, updated_at)
		 VALUES ('done-agent', 'sess-1', 'subagent', 'task', 'completed', 'test-model', '[]', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, _, _, _, exec := agent.NewCancelTool(conn)
	result, err := exec(ctx, `{"agent_id":"done-agent"}`)
	if err != nil {
		t.Fatalf("cancel completed: %v", err)
	}
	if result != `{"status":"not_found_or_already_terminal"}` {
		t.Errorf("unexpected: %s", result)
	}
}

func TestListBackgroundToolWithDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Insert background agents.
	conn.ExecContext(ctx,
		`INSERT INTO background_agents (agent_id, owner_session, task, status, created_at)
		 VALUES ('bg-1', 'sess-1', 'task 1', 'running', datetime('now'))`)
	conn.ExecContext(ctx,
		`INSERT INTO background_agents (agent_id, owner_session, task, status, created_at)
		 VALUES ('bg-2', 'sess-1', 'task 2', 'completed', datetime('now'))`)

	_, _, _, _, exec := agent.NewListBackgroundTool(conn)

	// List all.
	result, err := exec(ctx, `{}`)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	var out map[string]interface{}
	json.Unmarshal([]byte(result), &out)
	count := int(out["count"].(float64))
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// List filtered by status.
	result, err = exec(ctx, `{"status":"running"}`)
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	json.Unmarshal([]byte(result), &out)
	count = int(out["count"].(float64))
	if count != 1 {
		t.Errorf("filtered count = %d, want 1", count)
	}
}

func TestCleanupExpiredBackgroundAgents(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Insert an expired background agent.
	conn.ExecContext(ctx,
		`INSERT INTO background_agents (agent_id, owner_session, task, status, created_at, expires_at)
		 VALUES ('expired-1', 'sess-1', 'old task', 'running', datetime('now', '-1 hour'), datetime('now', '-30 minutes'))`)

	// Insert a non-expired one.
	conn.ExecContext(ctx,
		`INSERT INTO background_agents (agent_id, owner_session, task, status, created_at, expires_at)
		 VALUES ('active-1', 'sess-1', 'active task', 'running', datetime('now'), datetime('now', '+1 hour'))`)

	count, err := agent.CleanupExpiredBackgroundAgents(ctx, conn)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if count != 1 {
		t.Errorf("cleaned up %d, want 1", count)
	}

	// Verify statuses.
	var status string
	conn.QueryRowContext(ctx, "SELECT status FROM background_agents WHERE agent_id = 'expired-1'").Scan(&status)
	if status != "expired" {
		t.Errorf("expired-1 status = %q, want expired", status)
	}
	conn.QueryRowContext(ctx, "SELECT status FROM background_agents WHERE agent_id = 'active-1'").Scan(&status)
	if status != "running" {
		t.Errorf("active-1 status = %q, want running", status)
	}
}

func TestCleanupNilDB(t *testing.T) {
	count, err := agent.CleanupExpiredBackgroundAgents(context.Background(), nil)
	if err != nil {
		t.Fatalf("cleanup nil db: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestDelegateToolWithDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "result"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    capability.NewCapabilitySet(nil),
		Timeout: 10 * time.Second,
		DB:      conn,
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	_, err := exec(ctx, `{"task":"tracked delegation"}`)
	if err != nil {
		t.Fatalf("delegate with db: %v", err)
	}

	// Verify delegation was recorded.
	var count int
	conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_delegations WHERE delegation_type = 'delegate'").Scan(&count)
	if count != 1 {
		t.Errorf("delegation count = %d, want 1", count)
	}
}

func TestSpawnToolWithDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "bg result"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    capability.NewCapabilitySet(nil),
		Timeout: 5 * time.Second,
		DB:      conn,
	}
	_, _, _, _, exec := agent.NewSpawnTool(cfg)

	_, err := exec(ctx, `{"task":"tracked spawn"}`)
	if err != nil {
		t.Fatalf("spawn with db: %v", err)
	}

	// Verify delegation and background agent were recorded.
	var dCount, bCount int
	conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_delegations WHERE delegation_type = 'spawn'").Scan(&dCount)
	conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM background_agents").Scan(&bCount)
	if dCount != 1 {
		t.Errorf("delegation count = %d, want 1", dCount)
	}
	if bCount != 1 {
		t.Errorf("background agent count = %d, want 1", bCount)
	}

	// Let background goroutine finish.
	time.Sleep(200 * time.Millisecond)
}
