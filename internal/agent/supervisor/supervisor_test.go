package supervisor_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/supervisor"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/mission"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const missionSchema = `
CREATE TABLE missions (
    id TEXT PRIMARY KEY, goal TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'created',
    plan_json TEXT, budget_usd REAL NOT NULL DEFAULT 0, cost_so_far_usd REAL NOT NULL DEFAULT 0,
    supervisor_agent_id TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME, completed_at DATETIME);
CREATE TABLE mission_steps (
    id TEXT PRIMARY KEY, mission_id TEXT NOT NULL, task TEXT NOT NULL,
    depends_on_json TEXT, assigned_worker_id TEXT, state TEXT NOT NULL DEFAULT 'created',
    output TEXT, error TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME, completed_at DATETIME);
CREATE TABLE mission_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT, mission_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, event_type TEXT NOT NULL,
    payload_json TEXT);`

func newTestStore(t *testing.T) (mission.Store, *mission.Manager) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(missionSchema); err != nil {
		t.Fatal(err)
	}
	store := mission.NewSQLiteStore(db)
	return store, mission.NewManager(store)
}

func TestRun_RequiresMissionID(t *testing.T) {
	store, mgr := newTestStore(t)
	s := supervisor.New(mgr, store, bus.New(), "sup-1")
	if err := s.Run(context.Background(), ""); err == nil {
		t.Error("expected error for empty missionID")
	}
}

func TestRun_RejectsNonPlannedMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "g", "", 0)
	// Mission is in `created` state, not `planned`.
	s := supervisor.New(mgr, store, bus.New(), "sup-1")
	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error when mission is not in planned state")
	}
	if !strings.Contains(err.Error(), "planned") {
		t.Errorf("error should mention required state, got %v", err)
	}
}

func TestRun_EmptyPlan_CompletesImmediately(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "g", "", 0)
	if err := mgr.SetPlan(ctx, m.ID, nil); err != nil {
		t.Fatal(err)
	}
	s := supervisor.New(mgr, store, bus.New(), "sup-1")
	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("Run with empty plan should succeed: %v", err)
	}
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("state = %s, want completed", got.State)
	}
}

// TestRun_HappyPath_TwoStepsTwoWorkers wires up a real bus + two workers
// and a supervisor against a real-schema in-memory SQLite store. Verifies
// the full dispatch → execute → result → step-update → mission-complete
// cycle works end-to-end.
func TestRun_HappyPath_TwoSteps(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "test mission", "sup-1", 0)
	if err := mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: "step A"},
		{Task: "step B"},
	}); err != nil {
		t.Fatal(err)
	}

	b := bus.New()

	var taskCount atomic.Int32
	var mu sync.Mutex
	var seen []string
	executor := func(_ context.Context, t worker.Task) (string, error) {
		taskCount.Add(1)
		mu.Lock()
		seen = append(seen, t.Task)
		mu.Unlock()
		return "did " + t.Task, nil
	}
	w := worker.New(b, "w-1", executor)

	// Start worker in the background.
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	workerDone := make(chan error, 1)
	go func() { workerDone <- w.Run(wctx, m.ID) }()

	// Give worker time to subscribe.
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second

	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("supervisor Run: %v", err)
	}

	// Stop worker.
	wcancel()
	<-workerDone

	// Mission should be complete.
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("mission state = %s, want completed", got.State)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}

	// Both steps should have completed with output set.
	steps, _ := store.GetSteps(ctx, m.ID)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	for i, st := range steps {
		if st.State != mission.StateCompleted {
			t.Errorf("step %d state = %s, want completed", i, st.State)
		}
		if !strings.Contains(st.Output, "did step") {
			t.Errorf("step %d output = %q, want contains 'did step'", i, st.Output)
		}
	}

	// Both tasks should have been dispatched in order.
	if taskCount.Load() != 2 {
		t.Errorf("expected 2 tasks executed, got %d", taskCount.Load())
	}
	mu.Lock()
	if len(seen) != 2 || seen[0] != "step A" || seen[1] != "step B" {
		t.Errorf("dispatch order wrong: %v", seen)
	}
	mu.Unlock()

	// Event log should contain mission-level events.
	events, _ := store.GetEvents(ctx, m.ID, 100)
	wantTypes := map[string]bool{
		"mission.created":       false,
		"mission.planned":       false,
		"mission.started":       false,
		"supervisor.step_done":  false,
		"mission.completed":     false,
	}
	for _, e := range events {
		if _, ok := wantTypes[e.Type]; ok {
			wantTypes[e.Type] = true
		}
	}
	for k, seen := range wantTypes {
		if !seen {
			t.Errorf("expected event type %q in log; got %d events", k, len(events))
		}
	}
}

func TestRun_FailedStep_FailsMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "doomed", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "doomed step"}})

	b := bus.New()

	executor := func(_ context.Context, _ worker.Task) (string, error) {
		return "", errFail
	}
	w := worker.New(b, "w-fail", executor)

	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error when step fails")
	}

	wcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
	steps, _ := store.GetSteps(ctx, m.ID)
	if steps[0].State != mission.StateFailed {
		t.Errorf("step state = %s, want failed", steps[0].State)
	}
}

func TestRun_StepTimeout_FailsMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "stuck", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "stuck"}})

	b := bus.New()

	// Worker that never completes — receives the dispatch but never replies.
	// (Subscribes but never reads from the channel.)
	_ = b.SubscribeReliable("mission." + m.ID + ".dispatch")

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 200 * time.Millisecond

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// errFail is reused so the failed-step test doesn't depend on errors.New each run.
var errFail = stringError("worker failed")

type stringError string

func (e stringError) Error() string { return string(e) }
