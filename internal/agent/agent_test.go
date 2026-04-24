package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestAgentHappyPath(t *testing.T) {
	model := testutil.NewFakeModel(agent.Response{
		Content:   "Hello! I'm your AI Butler.",
		TokensIn:  10,
		TokensOut: 20,
	})

	a := agent.New(agent.Config{
		ID:        "agent-1",
		SessionID: "sess-1",
		Task:      "Say hello",
		Type:      agent.TypePrimary,
		Model:     model,
		Mode:      agent.ModeSingle,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Errorf("status = %s, want completed", result.Status)
	}
	if result.Output != "Hello! I'm your AI Butler." {
		t.Errorf("output = %q, want 'Hello! I'm your AI Butler.'", result.Output)
	}
	if result.TokensIn != 10 || result.TokensOut != 20 {
		t.Errorf("tokens = %d/%d, want 10/20", result.TokensIn, result.TokensOut)
	}
	if model.CallCount() != 1 {
		t.Errorf("model calls = %d, want 1", model.CallCount())
	}
}

func TestAgentWithToolCalls(t *testing.T) {
	model := testutil.NewFakeModel(
		// First response: call a tool.
		agent.Response{
			Content: "Let me check the weather.",
			ToolCalls: []agent.ToolCall{
				{ID: "tc-1", Name: "weather", Input: `{"city":"London"}`},
			},
			TokensIn: 10, TokensOut: 15,
		},
		// Second response: final answer using tool result.
		agent.Response{
			Content:   "It's 15°C in London.",
			TokensIn:  20,
			TokensOut: 10,
		},
	)

	tools := testutil.NewFakeToolExecutor(map[string]string{
		"weather": "15°C, partly cloudy",
	})

	a := agent.New(agent.Config{
		ID:    "agent-2",
		Task:  "What's the weather in London?",
		Type:  agent.TypePrimary,
		Model: model,
		Tools: tools,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Errorf("status = %s, want completed", result.Status)
	}
	if result.ToolCalls != 1 {
		t.Errorf("tool calls = %d, want 1", result.ToolCalls)
	}
	if result.Output != "It's 15°C in London." {
		t.Errorf("output = %q", result.Output)
	}

	// Verify the tool was called.
	tc := tools.ToolCalls()
	if len(tc) != 1 || tc[0].Name != "weather" {
		t.Errorf("tool calls = %v", tc)
	}
}

func TestAgentMultiRound(t *testing.T) {
	model := testutil.NewFakeModel(
		agent.Response{ToolCalls: []agent.ToolCall{{ID: "1", Name: "search"}}},
		agent.Response{ToolCalls: []agent.ToolCall{{ID: "2", Name: "fetch"}}},
		agent.Response{Content: "Done."},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{
		"search": "found: example.com",
		"fetch":  "page content",
	})

	a := agent.New(agent.Config{
		ID: "agent-3", Task: "Research topic", Type: agent.TypePrimary,
		Model: model, Tools: tools,
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Errorf("status = %s, want completed", result.Status)
	}
	if result.ToolCalls != 2 {
		t.Errorf("tool calls = %d, want 2", result.ToolCalls)
	}
	if model.CallCount() != 3 {
		t.Errorf("model calls = %d, want 3", model.CallCount())
	}
}

func TestAgentTimeout(t *testing.T) {
	// Model that blocks until context is cancelled.
	model := &blockingModel{}

	a := agent.New(agent.Config{
		ID: "agent-timeout", Task: "Slow task", Type: agent.TypePrimary,
		Model:   model,
		Timeout: 50 * time.Millisecond,
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateFailed {
		// Model error because ctx expired during Complete.
		if result.Status != agent.StateCancelled {
			t.Errorf("status = %s, want cancelled or failed", result.Status)
		}
	}
}

type blockingModel struct{}

func (m *blockingModel) Complete(ctx context.Context, _ []agent.Message) (agent.Response, error) {
	<-ctx.Done()
	return agent.Response{}, ctx.Err()
}

func TestAgentMaxToolCalls(t *testing.T) {
	// Model always returns a tool call.
	responses := make([]agent.Response, 6)
	for i := range responses {
		responses[i] = agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "tc", Name: "repeat"}},
		}
	}
	model := testutil.NewFakeModel(responses...)
	tools := testutil.NewFakeToolExecutor(map[string]string{"repeat": "ok"})

	a := agent.New(agent.Config{
		ID: "agent-max", Task: "Loop", Type: agent.TypePrimary,
		Model: model, Tools: tools,
		MaxToolCalls: 3,
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Errorf("status = %s, want completed", result.Status)
	}
	if result.ToolCalls != 3 {
		t.Errorf("tool calls = %d, want 3", result.ToolCalls)
	}
}

func TestAgentContextCancel(t *testing.T) {
	model := &blockingModel{}
	ctx, cancel := context.WithCancel(context.Background())

	a := agent.New(agent.Config{
		ID: "agent-cancel", Task: "Cancel me", Type: agent.TypePrimary,
		Model:   model,
		Timeout: 10 * time.Second,
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, _ := a.Run(ctx)
	// Should end as failed (model error from ctx cancel) or cancelled.
	if result.Status != agent.StateFailed && result.Status != agent.StateCancelled {
		t.Errorf("status = %s, want failed or cancelled", result.Status)
	}
}

func TestAgentModelError(t *testing.T) {
	model := testutil.NewFakeModel()
	model.SetError(errors.New("API rate limit exceeded"))

	a := agent.New(agent.Config{
		ID: "agent-err", Task: "Fail", Type: agent.TypePrimary,
		Model: model,
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateFailed {
		t.Errorf("status = %s, want failed", result.Status)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestAgentBudgetExceeded(t *testing.T) {
	// Each response costs tokens. With a very low budget, should stop.
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "1", Name: "expensive"}},
			TokensIn:  5000,
			TokensOut: 5000,
		},
		agent.Response{Content: "Done"},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{"expensive": "ok"})

	a := agent.New(agent.Config{
		ID: "agent-budget", Task: "Expensive task", Type: agent.TypePrimary,
		Model: model, Tools: tools,
		BudgetCap: 0.05, // $0.05 max
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCancelled {
		t.Errorf("status = %s, want cancelled (budget exceeded)", result.Status)
	}
}

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		from, to agent.State
		valid    bool
	}{
		{agent.StateSpawned, agent.StateRunning, true},
		{agent.StateSpawned, agent.StateCompleted, false},
		{agent.StateRunning, agent.StateWaiting, true},
		{agent.StateRunning, agent.StateCompleted, true},
		{agent.StateRunning, agent.StateFailed, true},
		{agent.StateRunning, agent.StateCancelled, true},
		{agent.StateWaiting, agent.StateRunning, true},
		{agent.StateCompleted, agent.StateRunning, false},
		{agent.StateFailed, agent.StateRunning, false},
		{agent.StateCancelled, agent.StateRunning, false},
	}

	for _, tt := range tests {
		got := agent.CanTransition(tt.from, tt.to)
		if got != tt.valid {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.valid)
		}
	}
}

func TestAgentPersistsState(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert required session (FK constraint).
	_, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope) VALUES ('sess-1', 'terminal', 'user1', 'default')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	model := testutil.NewFakeModel(agent.Response{Content: "Done"})

	a := agent.New(agent.Config{
		ID:        "persist-1",
		SessionID: "sess-1",
		Task:      "Test persistence",
		Type:      agent.TypePrimary,
		Model:     model,
		DB:        conn,
	})

	result, err := a.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// Verify agent row exists.
	var state string
	err = conn.QueryRowContext(ctx,
		"SELECT state FROM agents WHERE id = ?", "persist-1").Scan(&state)
	if err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if state != "completed" {
		t.Errorf("DB state = %q, want 'completed'", state)
	}
}

func TestRecoverAgents(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert a session first (FK constraint).
	_, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope) VALUES ('sess-1', 'terminal', 'user1', 'default')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	now := "2026-03-06T12:00:00Z"
	// Insert agents stuck in non-terminal states.
	stucks := []struct {
		id, state string
	}{
		{"stuck-1", "running"},
		{"stuck-2", "waiting"},
		{"stuck-3", "spawned"},
		{"ok-1", "completed"},
	}
	for _, s := range stucks {
		_, err := conn.ExecContext(ctx,
			`INSERT INTO agents (id, session_id, type, state, task, capabilities, model, created_at, updated_at)
			 VALUES (?, 'sess-1', 'primary', ?, 'test', '[]', 'default', ?, ?)`,
			s.id, s.state, now, now)
		if err != nil {
			t.Fatalf("insert %s: %v", s.id, err)
		}
	}

	count, err := agent.RecoverAgents(ctx, conn)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if count != 3 {
		t.Errorf("recovered = %d, want 3", count)
	}

	// Verify all stuck agents are now failed.
	for _, id := range []string{"stuck-1", "stuck-2", "stuck-3"} {
		var state string
		if err := conn.QueryRowContext(ctx, "SELECT state FROM agents WHERE id = ?", id).Scan(&state); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if state != "failed" {
			t.Errorf("%s state = %q, want 'failed'", id, state)
		}
	}

	// Verify completed agent is still completed.
	var state string
	conn.QueryRowContext(ctx, "SELECT state FROM agents WHERE id = 'ok-1'").Scan(&state)
	if state != "completed" {
		t.Errorf("ok-1 state = %q, want 'completed'", state)
	}
}

func TestSemaphore(t *testing.T) {
	sem := agent.NewSemaphore(2)
	ctx := context.Background()

	// Acquire 2 slots.
	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := sem.Acquire(ctx); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	// Third acquire should block. Use a timeout context to verify.
	ctx2, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := sem.Acquire(ctx2); err == nil {
		t.Error("expected blocked acquire to fail")
	}

	// Release one and try again.
	sem.Release()
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestSemaphoreDefaultCapacity(t *testing.T) {
	sem := agent.NewSemaphore(0) // 0 should default to 5
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := sem.Acquire(ctx); err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
	}
	// 6th should block.
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := sem.Acquire(ctx2); err == nil {
		t.Error("expected 6th acquire to block")
	}
}

func TestUserSemaphorePerUserLimit(t *testing.T) {
	global := agent.NewSemaphore(10) // High global limit
	us := agent.NewUserSemaphore(global, 2)
	ctx := context.Background()

	// User A can acquire 2 slots.
	if err := us.Acquire(ctx, "userA"); err != nil {
		t.Fatalf("userA acquire 1: %v", err)
	}
	if err := us.Acquire(ctx, "userA"); err != nil {
		t.Fatalf("userA acquire 2: %v", err)
	}
	// User A's 3rd should fail immediately (per-user limit).
	if err := us.Acquire(ctx, "userA"); err == nil {
		t.Error("expected userA 3rd acquire to fail per-user limit")
	}

	// User B should still succeed (different user).
	if err := us.Acquire(ctx, "userB"); err != nil {
		t.Fatalf("userB acquire: %v", err)
	}

	// Release one from A, should work again.
	us.Release("userA")
	if err := us.Acquire(ctx, "userA"); err != nil {
		t.Fatalf("userA acquire after release: %v", err)
	}
}

func TestUserSemaphoreGlobalLimit(t *testing.T) {
	global := agent.NewSemaphore(3) // Low global limit
	us := agent.NewUserSemaphore(global, 5)
	ctx := context.Background()

	// 3 different users each acquire 1 — fills global.
	if err := us.Acquire(ctx, "userA"); err != nil {
		t.Fatalf("userA: %v", err)
	}
	if err := us.Acquire(ctx, "userB"); err != nil {
		t.Fatalf("userB: %v", err)
	}
	if err := us.Acquire(ctx, "userC"); err != nil {
		t.Fatalf("userC: %v", err)
	}

	// 4th user should block on global semaphore.
	ctx2, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := us.Acquire(ctx2, "userD"); err == nil {
		t.Error("expected userD to block on global limit")
	}
}

func TestUserSemaphoreCountTracking(t *testing.T) {
	global := agent.NewSemaphore(10)
	us := agent.NewUserSemaphore(global, 5)
	ctx := context.Background()

	if c := us.UserCount("userA"); c != 0 {
		t.Errorf("initial count = %d, want 0", c)
	}
	_ = us.Acquire(ctx, "userA")
	_ = us.Acquire(ctx, "userA")
	if c := us.UserCount("userA"); c != 2 {
		t.Errorf("after 2 acquires count = %d, want 2", c)
	}
	us.Release("userA")
	if c := us.UserCount("userA"); c != 1 {
		t.Errorf("after release count = %d, want 1", c)
	}
}

func TestUserSemaphoreDefaultPerUser(t *testing.T) {
	global := agent.NewSemaphore(10)
	us := agent.NewUserSemaphore(global, 0) // 0 defaults to 3
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := us.Acquire(ctx, "user1"); err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
	}
	if err := us.Acquire(ctx, "user1"); err == nil {
		t.Error("expected 4th acquire to fail with default per-user limit of 3")
	}
}

func TestModeAuto(t *testing.T) {
	// Auto mode should work just like single when no delegation is configured.
	model := testutil.NewFakeModel(agent.Response{Content: "auto works"})

	a := agent.New(agent.Config{
		ID: "auto-1", Task: "Test auto", Type: agent.TypePrimary,
		Model: model,
		Mode:  agent.ModeAuto,
	})

	result, _ := a.Run(context.Background())
	if result.Status != agent.StateCompleted {
		t.Errorf("status = %s, want completed", result.Status)
	}
}
