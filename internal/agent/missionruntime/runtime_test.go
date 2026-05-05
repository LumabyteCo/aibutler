package missionruntime_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/missionruntime"
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

func TestRuntime_PicksUpPlannedMissionAndDrivesToCompletion(t *testing.T) {
	store, mgr := newTestStore(t)
	b := bus.New()

	var executed atomic.Int32
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		executed.Add(1)
		return "ok", nil
	}

	rt := missionruntime.New(mgr, store, b, missionruntime.Options{
		PollInterval: 50 * time.Millisecond,
		Executor:     executor,
	})

	// Pre-create a planned mission BEFORE starting the runtime — verifies
	// the initial-scan path picks it up immediately rather than waiting
	// for the first tick.
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "test mission", "", 0)
	if err := mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: "step 1"},
		{Task: "step 2"},
	}); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	go func() { _ = rt.Start(runCtx) }()

	// Wait for the mission to reach completed.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.GetMission(ctx, m.ID)
		if got.State == mission.StateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Fatalf("mission state = %s, want completed (executed=%d)", got.State, executed.Load())
	}
	if executed.Load() != 2 {
		t.Errorf("executor calls = %d, want 2", executed.Load())
	}

	// Shut down the runtime and confirm the running map drains.
	cancel()
	rt.Wait()
	if got := rt.RunningCount(); got != 0 {
		t.Errorf("RunningCount after shutdown = %d, want 0", got)
	}
}

func TestRuntime_MaxConcurrent_CapsConcurrentMissions(t *testing.T) {
	store, mgr := newTestStore(t)
	b := bus.New()

	// Slow executor — holds the worker for a while so concurrent missions
	// have to wait when the cap is hit.
	executor := func(ctx context.Context, _ worker.Task) (string, error) {
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
		}
		return "ok", nil
	}

	rt := missionruntime.New(mgr, store, b, missionruntime.Options{
		PollInterval:  20 * time.Millisecond,
		Executor:      executor,
		MaxConcurrent: 2,
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		m, _ := mgr.Create(ctx, "m", "", 0)
		_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "x"}})
	}

	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go func() { _ = rt.Start(runCtx) }()

	// Sample running count over 200ms — should never exceed MaxConcurrent.
	maxSeen := 0
	for i := 0; i < 10; i++ {
		time.Sleep(20 * time.Millisecond)
		if got := rt.RunningCount(); got > maxSeen {
			maxSeen = got
		}
	}
	if maxSeen > 2 {
		t.Errorf("MaxConcurrent=2 violated; saw %d running", maxSeen)
	}
	if maxSeen == 0 {
		t.Error("expected at least one mission to be running at some point")
	}

	cancel()
	rt.Wait()
}

func TestRuntime_DoesNotReSpawnRunningMission(t *testing.T) {
	store, mgr := newTestStore(t)
	b := bus.New()

	// Slow executor so the mission stays running across polls.
	executor := func(ctx context.Context, _ worker.Task) (string, error) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
		}
		return "ok", nil
	}

	rt := missionruntime.New(mgr, store, b, missionruntime.Options{
		PollInterval: 30 * time.Millisecond,
		Executor:     executor,
	})

	ctx := context.Background()
	m, _ := mgr.Create(ctx, "m", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "x"}})

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	go func() { _ = rt.Start(runCtx) }()

	// During the slow execution there should be exactly one supervisor
	// goroutine — re-polling shouldn't re-spawn for the same mission.
	time.Sleep(150 * time.Millisecond)
	if got := rt.RunningCount(); got != 1 {
		t.Errorf("RunningCount during slow exec = %d, want 1", got)
	}

	cancel()
	rt.Wait()
}

func TestRuntime_StopAll_OnContextCancel(t *testing.T) {
	store, mgr := newTestStore(t)
	b := bus.New()

	executor := func(ctx context.Context, _ worker.Task) (string, error) {
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
		}
		return "ok", ctx.Err()
	}

	rt := missionruntime.New(mgr, store, b, missionruntime.Options{
		PollInterval: 30 * time.Millisecond,
		Executor:     executor,
	})

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		m, _ := mgr.Create(ctx, "m", "", 0)
		_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "x"}})
	}

	runCtx, cancel := context.WithCancel(ctx)
	go func() { _ = rt.Start(runCtx) }()

	// Wait for at least one supervisor to be running.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && rt.RunningCount() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if rt.RunningCount() == 0 {
		t.Fatal("no supervisors started before cancel")
	}

	// Cancel and confirm the runtime drains within a reasonable bound.
	cancel()
	rt.Wait()

	if got := rt.RunningCount(); got != 0 {
		t.Errorf("RunningCount after Stop = %d, want 0", got)
	}
}

func TestRuntime_DefaultExecutor_IsEcho(t *testing.T) {
	store, mgr := newTestStore(t)
	b := bus.New()

	rt := missionruntime.New(mgr, store, b, missionruntime.Options{
		PollInterval: 30 * time.Millisecond,
	})

	ctx := context.Background()
	m, _ := mgr.Create(ctx, "echo-default", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "anything"}})

	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	go func() { _ = rt.Start(runCtx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.GetMission(ctx, m.ID)
		if got.State == mission.StateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Fatalf("default executor should drive mission to completed, got %s", got.State)
	}

	cancel()
	rt.Wait()
}
