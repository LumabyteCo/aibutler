package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/testutil"
)

// TestAgentFailurePersistsFailedState guards the v0.2.1 fix (B1): a failed
// model call must leave the DB row in state "failed", not "running". The
// original bug: failWith() transitioned in memory but never persisted, so
// every 401/410 run became a permanent zombie in `aibutler agent list`.
func TestAgentFailurePersistsFailedState(t *testing.T) {
	database := testutil.TestDB(t)
	db := database.Conn()
	model := testutil.NewFakeModel(agent.Response{Content: "unused"})

	// Seed the session row (FK target of agents.session_id).
	if _, err := db.Exec(`INSERT INTO sessions (id, channel, account_id, scope) VALUES ('sess-fail', 'terminal', 'user1', 'default')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	model.SetError(errors.New("openai: stream status 401"))

	a := agent.New(agent.Config{
		ID:        "agent-fail-persist",
		SessionID: "sess-fail",
		Task:      "anything",
		Type:      agent.TypePrimary,
		Model:     model,
		Mode:      agent.ModeSingle,
		DB:        db,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run returned err: %v", err)
	}
	if result.Status != agent.StateFailed {
		t.Fatalf("result.Status = %s, want failed", result.Status)
	}

	var state string
	if err := db.QueryRow(`SELECT state FROM agents WHERE id = ?`, "agent-fail-persist").Scan(&state); err != nil {
		t.Fatalf("query persisted state: %v", err)
	}
	if state != string(agent.StateFailed) {
		t.Errorf("persisted state = %q, want %q — failed agents must not be left 'running' in the DB", state, agent.StateFailed)
	}

	// The error text must also be persisted for post-mortem.
	var errText string
	if err := db.QueryRow(`SELECT error FROM agents WHERE id = ?`, "agent-fail-persist").Scan(&errText); err != nil {
		t.Fatalf("query persisted error: %v", err)
	}
	if errText == "" {
		t.Error("persisted error text is empty — failure cause must be stored")
	}
}

// TestRecoverAgentsMarksZombies verifies the crash-recovery janitor: agents
// in spawned/running/waiting from a previous (crashed) run are marked failed
// on next boot.
func TestRecoverAgentsMarksZombies(t *testing.T) {
	database := testutil.TestDB(t)
	db := database.Conn()
	ctx := context.Background()

	// Seed the session row (FK target of agents.session_id).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope) VALUES ('s', 'terminal', 'user1', 'default')`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Seed three zombies in different non-terminal states.
	for _, row := range []struct{ id, state string }{
		{"z1", "spawned"},
		{"z2", "running"},
		{"z3", "waiting"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO agents (id, session_id, type, state, task, capabilities, model, created_at, updated_at)
			 VALUES (?, 's', 'primary', ?, 't', '[]', 'default', datetime('now'), datetime('now'))`,
			row.id, row.state); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}

	n, err := agent.RecoverAgents(ctx, db)
	if err != nil {
		t.Fatalf("RecoverAgents: %v", err)
	}
	if n != 3 {
		t.Errorf("recovered = %d, want 3", n)
	}

	for _, id := range []string{"z1", "z2", "z3"} {
		var state string
		if err := db.QueryRow(`SELECT state FROM agents WHERE id = ?`, id).Scan(&state); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if state != string(agent.StateFailed) {
			t.Errorf("%s state = %q, want failed", id, state)
		}
	}
}