package supervisor_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/manager"
	"github.com/LumabyteCo/aibutler/internal/agent/supervisor"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/capability"
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
	// :memory: SQLite is per-connection — pin to a single connection
	// so the supervisor + worker goroutines all see the same database.
	db.SetMaxOpenConns(1)
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

// --- Parallel dispatch tests ---
//
// Scope note: these tests verify supervisor-side DAG dispatch logic —
// the supervisor walks Step.DependsOn as a DAG and stops blocking on
// each step's result before dispatching the next ready step. Real
// wall-clock parallelism additionally requires either a competing-
// consumer bus or per-worker concurrent handling, both of which are
// follow-up changes. The tests here therefore check correctness of
// the DAG order and completion, not wall-clock concurrency.

// TestRun_Parallel_IndependentStepsAllComplete verifies that when a
// plan is set via SetPlanParallel and the steps have no DependsOn
// links between them, all of them are dispatched and the mission
// completes.
func TestRun_Parallel_IndependentStepsAllComplete(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "parallel", "", 0)
	if err := mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{Task: "A"}, {Task: "B"}, {Task: "C"}, // no DependsOn = all independent
	}); err != nil {
		t.Fatal(err)
	}

	b := bus.New()
	var dispatched atomic.Int32
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		dispatched.Add(1)
		return "ok", nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("state = %s, want completed", got.State)
	}
	if dispatched.Load() != 3 {
		t.Errorf("executor calls = %d, want 3", dispatched.Load())
	}
}

// TestRun_Parallel_RespectsDependsOnGraph verifies that a DAG like
//
//	A → B
//	  → C → D
//
// completes successfully and that A always runs before B/C, C runs
// before D.
func TestRun_Parallel_RespectsDependsOnGraph(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "dag", "", 0)
	// Assign explicit IDs so DependsOn can reference them.
	steps := []mission.Step{
		{ID: "a", Task: "A"},
		{ID: "b", Task: "B", DependsOn: []string{"a"}},
		{ID: "c", Task: "C", DependsOn: []string{"a"}},
		{ID: "d", Task: "D", DependsOn: []string{"c"}},
	}
	if err := mgr.SetPlanParallel(ctx, m.ID, steps); err != nil {
		t.Fatal(err)
	}

	b := bus.New()
	var mu sync.Mutex
	startedAt := map[string]time.Time{}
	executor := func(_ context.Context, task worker.Task) (string, error) {
		mu.Lock()
		startedAt[task.Task] = time.Now()
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		return "ok", nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("state = %s, want completed", got.State)
	}

	mu.Lock()
	defer mu.Unlock()

	// A must start before B, C, and D — they all transitively depend on A.
	for _, downstream := range []string{"B", "C", "D"} {
		if !startedAt["A"].Before(startedAt[downstream]) {
			t.Errorf("A should start before %s; A=%v %s=%v",
				downstream, startedAt["A"], downstream, startedAt[downstream])
		}
	}
	// D must start after C.
	if !startedAt["C"].Before(startedAt["D"]) {
		t.Errorf("D should start after C; C=%v D=%v",
			startedAt["C"], startedAt["D"])
	}
}

// TestRun_Parallel_FailedStep_FailsMission verifies that a failed
// step terminates the mission. In-flight peers may still run; the
// supervisor stops dispatching new work and reports the first failure.
func TestRun_Parallel_FailedStep_FailsMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "fails", "", 0)
	if err := mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{ID: "ok1", Task: "ok"},
		{ID: "bad", Task: "bad"},
		{ID: "downstream", Task: "downstream", DependsOn: []string{"bad"}},
	}); err != nil {
		t.Fatal(err)
	}

	b := bus.New()

	executor := func(_ context.Context, task worker.Task) (string, error) {
		if task.Task == "bad" {
			return "", errors.New("step deliberately fails")
		}
		return "ok", nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error from failed step")
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}

	// "downstream" should NOT have run (its dep failed).
	stepsAfter, _ := store.GetSteps(ctx, m.ID)
	for _, st := range stepsAfter {
		if st.ID == "downstream" && st.State == mission.StateCompleted {
			t.Error("downstream step ran even though its dep failed")
		}
	}
}

// TestRun_Parallel_Deadlock_DanglingDependency verifies a clear error
// when a step references a DependsOn entry that doesn't exist (typo or
// model hallucination).
func TestRun_Parallel_Deadlock_DanglingDependency(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "dangling", "", 0)
	if err := mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{ID: "a", Task: "A", DependsOn: []string{"ghost"}}, // ghost doesn't exist
	}); err != nil {
		t.Fatal(err)
	}

	s := supervisor.New(mgr, store, bus.New(), "sup-1")
	s.StepTimeout = 1 * time.Second

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected deadlock error")
	}
	if !strings.Contains(err.Error(), "deadlock") {
		t.Errorf("error should mention deadlock, got: %v", err)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// TestRun_Parallel_ConcurrentWorkers_AchievesWallClockParallelism
// verifies that with competing-consumer dispatch and multiple workers
// in the pool, independent steps actually execute concurrently. With
// 3 workers and 3 independent steps × 200ms each, sequential
// execution would take ~600ms; concurrent execution should be much
// closer to ~200ms.
func TestRun_Parallel_ConcurrentWorkers_AchievesWallClockParallelism(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "real-parallel", "", 0)
	// Each step takes 500ms — long enough that the
	// per-dispatch shuffle + busy-peer fall-through overhead in the
	// competing-consumer bus (worst case ~50ms × 3 steps = 150ms) is
	// dwarfed by the work itself. Sequential floor: 1500ms. Parallel
	// best case: ~500ms. Parallel worst case (busy shuffle): ~650ms.
	// CI-safe bound at 1100ms leaves >400ms of headroom for slow
	// runners under -race while still proving wall-clock parallelism
	// vs the 1500ms sequential floor.
	const stepDelay = 500 * time.Millisecond
	if err := mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{Task: "A"}, {Task: "B"}, {Task: "C"},
	}); err != nil {
		t.Fatal(err)
	}

	b := bus.New()

	executor := func(ctx context.Context, _ worker.Task) (string, error) {
		select {
		case <-time.After(stepDelay):
		case <-ctx.Done():
		}
		return "ok", nil
	}

	// Spin up three workers, all competing for dispatched tasks.
	for i := 0; i < 3; i++ {
		w := worker.New(b, "w-"+string(rune('A'+i)), executor)
		wctx, wcancel := context.WithCancel(ctx)
		defer wcancel()
		go func() { _ = w.Run(wctx, m.ID) }()
	}
	// Let all three subscribe before the supervisor starts dispatching.
	time.Sleep(100 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second

	start := time.Now()
	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("state = %s, want completed", got.State)
	}

	// Sequential would take 3 × stepDelay = 1500ms. Real wall-clock
	// parallelism completes in ~stepDelay plus bus and scheduling
	// overhead — best case ~520ms, worst case ~650ms when the shuffle
	// picks busy peers first and falls through via SendTimeout.
	// Bound at 1100ms gives slow CI runners under -race plenty of
	// headroom while still proving parallelism vs the 1500ms
	// sequential floor.
	const bound = 1100 * time.Millisecond
	if elapsed >= bound {
		t.Errorf("expected parallel wall-clock < %s with 3 workers × %s each, got %s",
			bound, stepDelay, elapsed)
	}
	t.Logf("3 independent steps × %s with 3 workers: %s wall-clock (sequential floor = %s)",
		stepDelay, elapsed, 3*stepDelay)
}

// TestSetPlanParallel_PersistsFlag verifies the Plan.Parallel flag
// round-trips through the plan JSON.
func TestSetPlanParallel_PersistsFlag(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx := context.Background()

	mSeq, _ := mgr.Create(ctx, "seq", "", 0)
	_ = mgr.SetPlan(ctx, mSeq.ID, []mission.Step{{Task: "a"}})

	mPar, _ := mgr.Create(ctx, "par", "", 0)
	_ = mgr.SetPlanParallel(ctx, mPar.ID, []mission.Step{{Task: "a"}})

	gotSeq, _ := store.GetMission(ctx, mSeq.ID)
	gotPar, _ := store.GetMission(ctx, mPar.ID)

	planSeq := mission.PlanFromJSON(gotSeq.PlanJSON)
	planPar := mission.PlanFromJSON(gotPar.PlanJSON)

	if planSeq.Parallel {
		t.Error("SetPlan should leave Parallel=false")
	}
	if !planPar.Parallel {
		t.Error("SetPlanParallel should set Parallel=true")
	}
}

// --- Interrupt / external-state-change tests ---

func TestRun_ExternalCancel_ExitsBetweenSteps(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "long mission", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: "step 1"},
		{Task: "step 2"},
		{Task: "step 3"},
	})

	b := bus.New()

	// Worker that cancels the mission externally after the first step
	// completes — proves the supervisor's between-steps state recheck
	// notices and exits cleanly. Synchronous Cancel before returning
	// guarantees the supervisor sees the cancelled state on its next
	// step-loop iteration (no goroutine race).
	var stepsCompleted atomic.Int32
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		n := stepsCompleted.Add(1)
		if n == 1 {
			_ = mgr.Cancel(context.Background(), m.ID, "external cancel")
		}
		return "ok", nil
	}
	w := worker.New(b, "w-1", executor)

	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 1 * time.Second

	err := s.Run(ctx, m.ID)
	if err == nil || !strings.Contains(err.Error(), "cancelled externally") {
		t.Fatalf("expected cancelled-externally error, got %v", err)
	}

	// Mission should still be in cancelled state (the supervisor saw it
	// already terminal and didn't transition further).
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCancelled {
		t.Errorf("state = %s, want cancelled", got.State)
	}

	// Should have completed at most 1-2 steps before noticing the
	// external cancel.
	if stepsCompleted.Load() > 2 {
		t.Errorf("expected ≤2 steps before external cancel detected, got %d", stepsCompleted.Load())
	}
}

func TestRun_ExternalPause_ReturnsErrMissionPaused(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "pausable", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: "a"},
		{Task: "b"},
		{Task: "c"},
	})

	b := bus.New()

	executor := func(_ context.Context, t worker.Task) (string, error) {
		// Synchronously pause after step "a" so the supervisor's
		// between-steps state recheck sees waiting_user immediately.
		if t.Task == "a" {
			_ = mgr.Pause(context.Background(), m.ID, "user away")
		}
		return "ok", nil
	}
	w := worker.New(b, "w-pause", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 1 * time.Second

	err := s.Run(ctx, m.ID)
	if !errors.Is(err, supervisor.ErrMissionPaused) {
		t.Fatalf("expected ErrMissionPaused, got %v", err)
	}

	// State should remain waiting_user — the supervisor exited cleanly,
	// did not transition to terminal, and the caller can resume + re-run.
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateWaitingUser {
		t.Errorf("state = %s, want waiting_user", got.State)
	}
}

func TestRun_ResumeAfterPause_PicksUpRemainingSteps(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "pause-resume", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: "first"},
		{Task: "second"},
		{Task: "third"},
	})

	b := bus.New()

	var pauseFired atomic.Bool
	var executed atomic.Int32
	executor := func(_ context.Context, t worker.Task) (string, error) {
		executed.Add(1)
		// Pause after first execution only — synchronously, so the
		// supervisor sees waiting_user on the very next loop iteration.
		// The resumed run shouldn't re-trigger the pause (pauseFired guard).
		if !pauseFired.Load() && t.Task == "first" {
			pauseFired.Store(true)
			_ = mgr.Pause(context.Background(), m.ID, "checkpoint")
		}
		return "did " + t.Task, nil
	}
	w := worker.New(b, "w-pr", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 1 * time.Second

	// First Run: should pause partway.
	if err := s.Run(ctx, m.ID); !errors.Is(err, supervisor.ErrMissionPaused) {
		t.Fatalf("first Run: want ErrMissionPaused, got %v", err)
	}

	// External resume.
	if err := mgr.Resume(ctx, m.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// Second Run: should pick up the remaining steps and complete.
	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("second Run after resume: %v", err)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("state = %s, want completed", got.State)
	}

	// Skipped-completed-steps logic: should NOT re-execute already-done
	// steps. Total executions should be 3 (one per step).
	if got := executed.Load(); got != 3 {
		t.Errorf("executor calls = %d, want 3 (one per step, no re-runs)", got)
	}
}

// --- Replan-on-failure tests ---
//
// A test Replanner is a closure-backed implementation of
// supervisor.Replanner. Wraps a single mission and surfaces what the
// supervisor passed in so assertions can check both the trigger and
// the args.

type stubReplanner struct {
	mu       sync.Mutex
	calls    []supervisor.ReplanRequest
	respond  func(req supervisor.ReplanRequest) ([]mission.Step, error)
}

func (r *stubReplanner) Replan(_ context.Context, req supervisor.ReplanRequest) ([]mission.Step, error) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	return r.respond(req)
}

func (r *stubReplanner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *stubReplanner) lastCall() supervisor.ReplanRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return supervisor.ReplanRequest{}
	}
	return r.calls[len(r.calls)-1]
}

// TestRun_FailedStep_ReplanRecovers verifies that a Replanner-returned
// replacement step is dispatched after a failure and the mission
// completes via the replacement path.
func TestRun_FailedStep_ReplanRecovers(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "recoverable mission", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: "FAIL-ME"},
		{Task: "would-have-been-skipped"},
	})

	b := bus.New()

	// Executor that fails the first task ("FAIL-ME") and succeeds the
	// replacement ("RECOVERED").
	var seen []string
	var seenMu sync.Mutex
	executor := func(_ context.Context, task worker.Task) (string, error) {
		seenMu.Lock()
		seen = append(seen, task.Task)
		seenMu.Unlock()
		if task.Task == "FAIL-ME" {
			return "", errFail
		}
		return "did " + task.Task, nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	replanner := &stubReplanner{
		respond: func(req supervisor.ReplanRequest) ([]mission.Step, error) {
			return []mission.Step{{Task: "RECOVERED"}}, nil
		},
	}

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second
	s.Replanner = replanner

	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("supervisor Run after replan: %v", err)
	}
	wcancel()

	// Replanner consulted exactly once.
	if got := replanner.callCount(); got != 1 {
		t.Fatalf("Replanner called %d times, want 1", got)
	}
	call := replanner.lastCall()
	if call.FailedStep.Task != "FAIL-ME" {
		t.Errorf("FailedStep.Task = %q, want FAIL-ME", call.FailedStep.Task)
	}
	if !strings.Contains(call.FailureReason, "worker failed") {
		t.Errorf("FailureReason = %q, want it to mention worker failed", call.FailureReason)
	}
	if call.Goal != "recoverable mission" {
		t.Errorf("Goal = %q, want 'recoverable mission'", call.Goal)
	}
	if call.PriorReplans != 0 {
		t.Errorf("PriorReplans = %d, want 0", call.PriorReplans)
	}

	// Mission should be completed via the replacement step.
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("mission state = %s, want completed", got.State)
	}

	// Steps should be: FAIL-ME (failed), would-have-been-skipped
	// (created), RECOVERED (completed). The supervisor stops at the
	// failure point and the replan replaces what comes after, so the
	// "would-have-been-skipped" step stays untouched in state=created.
	// We don't assert that here — what matters is the failed step is
	// failed and at least one completed replacement exists.
	steps, _ := store.GetSteps(ctx, m.ID)
	var failedCount, completedCount int
	for _, st := range steps {
		switch st.State {
		case mission.StateFailed:
			failedCount++
		case mission.StateCompleted:
			completedCount++
		}
	}
	if failedCount != 1 {
		t.Errorf("failed step count = %d, want 1", failedCount)
	}
	if completedCount < 1 {
		t.Errorf("completed step count = %d, want at least 1 (the replacement)", completedCount)
	}

	// Dispatch order should be FAIL-ME first, then RECOVERED. The
	// "would-have-been-skipped" task should NOT have been dispatched
	// since the failure short-circuited that point in the original plan
	// and the replan superseded the remaining sequence.
	seenMu.Lock()
	defer seenMu.Unlock()
	if len(seen) != 2 || seen[0] != "FAIL-ME" || seen[1] != "RECOVERED" {
		t.Errorf("dispatch order = %v, want [FAIL-ME RECOVERED]", seen)
	}

	// mission.replanned event recorded.
	events, _ := store.GetEvents(ctx, m.ID, 100)
	var sawReplan bool
	for _, e := range events {
		if e.Type == "mission.replanned" {
			sawReplan = true
			break
		}
	}
	if !sawReplan {
		t.Error("expected mission.replanned event in log")
	}
}

// TestRun_FailedStep_ReplanRejected_FailsFast verifies that a Replanner
// returning ErrReplanRejected causes the mission to fail immediately
// without burning a replan attempt or issuing another dispatch.
func TestRun_FailedStep_ReplanRejected_FailsFast(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "unrecoverable", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "FAIL-ME"}})

	b := bus.New()
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		return "", errFail
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	replanner := &stubReplanner{
		respond: func(req supervisor.ReplanRequest) ([]mission.Step, error) {
			return nil, supervisor.ErrReplanRejected
		},
	}

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second
	s.Replanner = replanner

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error after ErrReplanRejected")
	}
	wcancel()

	if got := replanner.callCount(); got != 1 {
		t.Errorf("Replanner called %d times, want 1 (one consult, no retries on rejection)", got)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// TestRun_FailedStep_MaxReplansExhausted verifies that after MaxReplans
// attempts the mission fails.
func TestRun_FailedStep_MaxReplansExhausted(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "persistently broken", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "FAIL-ALWAYS"}})

	b := bus.New()

	// Executor that fails every single task no matter what name it has.
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		return "", errFail
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	// Replanner that always proposes one new step. Every replacement
	// will also fail because the executor fails everything.
	replanner := &stubReplanner{
		respond: func(req supervisor.ReplanRequest) ([]mission.Step, error) {
			return []mission.Step{{Task: "FAIL-ALSO"}}, nil
		},
	}

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second
	s.Replanner = replanner
	s.MaxReplans = 2

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error after replan attempts exhausted")
	}
	wcancel()

	// Replanner consulted MaxReplans times — once per failure within the
	// cap. After the cap, the (cap+1)th failure does not consult.
	if got := replanner.callCount(); got != 2 {
		t.Errorf("Replanner called %d times, want 2 (MaxReplans)", got)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// TestRun_NoReplanner_PreservesOldFailFastBehavior verifies that
// leaving Replanner nil keeps the v0.2.x behavior: one step failure
// terminates the mission immediately.
func TestRun_NoReplanner_PreservesOldFailFastBehavior(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "no-replanner-mission", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "FAIL-ME"}})

	b := bus.New()
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		return "", errFail
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second
	// Replanner intentionally nil.

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error when step fails")
	}
	wcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// TestRun_NoopReplanner_BehavesLikeNoReplanner verifies that the
// NoopReplanner (which always returns ErrReplanRejected) gives the
// same fail-fast behavior as leaving Replanner nil.
func TestRun_NoopReplanner_BehavesLikeNoReplanner(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "noop-mission", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "FAIL-ME"}})

	b := bus.New()
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		return "", errFail
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second
	s.Replanner = supervisor.NoopReplanner{}

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error when step fails")
	}
	wcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// --- Mid-mission auto-pause tests ---

// confirmStepTask is the task body recognised by confirmRequiringExecutor
// as the trigger to return a ConfirmationRequiredError. Any other task
// runs through to completion.
const confirmStepTask = "CONFIRM-ME"

// confirmRequiringExecutor returns a structured ConfirmationRequiredError
// for tasks matching confirmStepTask, and succeeds otherwise.
func confirmRequiringExecutor(_ context.Context, t worker.Task) (string, error) {
	if t.Task == confirmStepTask {
		return "", &capability.ConfirmationRequiredError{
			Capability: "tool.shell.exec",
			Reason:     "granted",
		}
	}
	return "did " + t.Task, nil
}

// TestRun_FailedStep_NeedsConfirmation_PausesMission verifies that
// when the worker reports NeedsConfirmation=true the supervisor
// transitions the mission to waiting_user, emits the
// mission.confirmation_required event, leaves the step in
// state=waiting_user (NOT failed), and exits with ErrMissionPaused
// rather than failing or replanning.
func TestRun_FailedStep_NeedsConfirmation_PausesMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "needs-confirmation mission", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: confirmStepTask}})

	b := bus.New()
	w := worker.New(b, "w-1", confirmRequiringExecutor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second

	err := s.Run(ctx, m.ID)
	if !errors.Is(err, supervisor.ErrMissionPaused) {
		t.Fatalf("err = %v, want supervisor.ErrMissionPaused", err)
	}
	wcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateWaitingUser {
		t.Errorf("mission state = %s, want waiting_user", got.State)
	}

	steps, _ := store.GetSteps(ctx, m.ID)
	if len(steps) != 1 {
		t.Fatalf("step count = %d, want 1", len(steps))
	}
	if steps[0].State != mission.StateWaitingUser {
		t.Errorf("step state = %s, want waiting_user (NOT failed)", steps[0].State)
	}
	if steps[0].CompletedAt != nil {
		t.Error("step CompletedAt should be nil — the step is paused, not finished")
	}
	if !strings.Contains(steps[0].Error, "tool.shell.exec") {
		t.Errorf("step.Error = %q, want it to mention the capability", steps[0].Error)
	}

	events, _ := store.GetEvents(ctx, m.ID, 100)
	var sawConfirm bool
	for _, e := range events {
		if e.Type == "mission.confirmation_required" {
			sawConfirm = true
			if !strings.Contains(e.PayloadJSON, steps[0].ID) {
				t.Errorf("confirmation event payload should mention step ID; got %q", e.PayloadJSON)
			}
		}
	}
	if !sawConfirm {
		t.Error("expected mission.confirmation_required event in log")
	}
}

// TestRun_ResumeAfterConfirmation_RunsPausedStep verifies that after
// the mission is resumed (state goes back to running via Manager.Resume)
// and the underlying capability no longer needs confirmation, a fresh
// Supervisor.Run picks up the previously-paused step from state=
// waiting_user, dispatches it, and completes the mission. Simulates
// the user granting the capability between Run #1 and Run #2.
func TestRun_ResumeAfterConfirmation_RunsPausedStep(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "resumable", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{Task: confirmStepTask},
		{Task: "do followup"},
	})

	b := bus.New()

	// Phase 1 executor: confirmation required for FIRST call only.
	var calls atomic.Int32
	phase1 := func(_ context.Context, t worker.Task) (string, error) {
		calls.Add(1)
		if t.Task == confirmStepTask {
			return "", &capability.ConfirmationRequiredError{
				Capability: "tool.shell.exec",
				Reason:     "granted",
			}
		}
		return "did " + t.Task, nil
	}
	w := worker.New(b, "w-1", phase1)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second

	// First Run: hits the pause path.
	if err := s.Run(ctx, m.ID); !errors.Is(err, supervisor.ErrMissionPaused) {
		t.Fatalf("first Run err = %v, want ErrMissionPaused", err)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateWaitingUser {
		t.Fatalf("post-pause mission state = %s, want waiting_user", got.State)
	}

	// Simulate the user resuming. In production this is invoked via
	// the mission.interrupt tool with action=resume.
	if err := mgr.Resume(ctx, m.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// Swap the worker's executor for one that succeeds (simulating the
	// user having granted the confirmation between Run calls). The
	// worker is the same goroutine — we restart it with a new executor.
	wcancel()
	b2 := bus.New()
	phase2 := func(_ context.Context, t worker.Task) (string, error) {
		return "did " + t.Task, nil
	}
	w2 := worker.New(b2, "w-2", phase2)
	wctx2, wcancel2 := context.WithCancel(ctx)
	defer wcancel2()
	go func() { _ = w2.Run(wctx2, m.ID) }()
	time.Sleep(50 * time.Millisecond)

	s2 := supervisor.New(mgr, store, b2, "sup-2")
	s2.StepTimeout = 2 * time.Second
	if err := s2.Run(ctx, m.ID); err != nil {
		t.Fatalf("second Run after resume: %v", err)
	}
	wcancel2()

	got, _ = store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("post-resume mission state = %s, want completed", got.State)
	}

	steps, _ := store.GetSteps(ctx, m.ID)
	if len(steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(steps))
	}
	for i, st := range steps {
		if st.State != mission.StateCompleted {
			t.Errorf("step %d state = %s, want completed", i, st.State)
		}
	}
}

// --- Manager tier (3-level hierarchy) tests ---

// TestRun_ManagerTier_GroupStepDelegatesAndAggregates verifies the
// full 3-tier delegation cycle: a plan with one grouped step (carrying
// SubSteps) plus one leaf step. The supervisor routes the grouped step
// to a manager, the manager decomposes it across the worker pool and
// aggregates, and the mission completes with the grouped step's output
// holding every sub-step result.
func TestRun_ManagerTier_GroupStepDelegatesAndAggregates(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "3-tier mission", "sup-1", 0)
	// One grouped step (group-1, with three sub-steps) followed by a
	// plain leaf step (leaf-1). Explicit IDs keep the assertions
	// readable; SetPlan would otherwise allocate them.
	if err := mgr.SetPlan(ctx, m.ID, []mission.Step{
		{
			ID:   "group-1",
			Task: "do the group",
			SubSteps: []mission.Step{
				{ID: "sub-a", Task: "alpha"},
				{ID: "sub-b", Task: "beta"},
				{ID: "sub-c", Task: "gamma"},
			},
		},
		{ID: "leaf-1", Task: "final leaf"},
	}); err != nil {
		t.Fatal(err)
	}

	b := bus.New()

	executor := func(_ context.Context, task worker.Task) (string, error) {
		return "done:" + task.Task, nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()

	mgrAgent := manager.New(b, "mgr-1")
	mgctx, mgcancel := context.WithCancel(ctx)
	defer mgcancel()
	go func() { _ = mgrAgent.Run(mgctx, m.ID) }()

	time.Sleep(80 * time.Millisecond) // let worker + manager subscribe

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second

	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("supervisor Run: %v", err)
	}
	wcancel()
	mgcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("mission state = %s, want completed", got.State)
	}

	steps, _ := store.GetSteps(ctx, m.ID)
	if len(steps) != 2 {
		t.Fatalf("step count = %d, want 2 (group-1, leaf-1 — sub-steps don't get rows)", len(steps))
	}
	byID := map[string]mission.Step{}
	for _, st := range steps {
		byID[st.ID] = st
	}
	group := byID["group-1"]
	if group.State != mission.StateCompleted {
		t.Errorf("group step state = %s, want completed", group.State)
	}
	for _, want := range []string{"sub-a: done:alpha", "sub-b: done:beta", "sub-c: done:gamma"} {
		if !strings.Contains(group.Output, want) {
			t.Errorf("group output missing %q; got:\n%s", want, group.Output)
		}
	}
	if byID["leaf-1"].State != mission.StateCompleted {
		t.Errorf("leaf step state = %s, want completed", byID["leaf-1"].State)
	}
	if !strings.Contains(byID["leaf-1"].Output, "done:final leaf") {
		t.Errorf("leaf output = %q, want 'done:final leaf'", byID["leaf-1"].Output)
	}
}

// TestRun_ManagerTier_NoGroupSteps_NoManagerNeeded verifies backwards
// compatibility: a plan with only leaf steps completes normally even
// when no manager is running, because the supervisor never routes to
// the manager topic without SubSteps.
func TestRun_ManagerTier_NoGroupSteps_NoManagerNeeded(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "leaf-only", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{ID: "a", Task: "alpha"},
		{ID: "b", Task: "beta"},
	})

	b := bus.New()
	executor := func(_ context.Context, task worker.Task) (string, error) {
		return "done:" + task.Task, nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	// Note: NO manager spawned. Leaf-only plans must not need one.
	time.Sleep(50 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 2 * time.Second
	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("supervisor Run: %v", err)
	}
	wcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("mission state = %s, want completed (leaf-only plan, no manager)", got.State)
	}
}

// TestRun_ManagerTier_GroupSubStepFailure_FailsMission verifies that a
// failing sub-step inside a group propagates up: the manager reports a
// failed parent Result, the supervisor marks the group step failed,
// and the mission fails.
func TestRun_ManagerTier_GroupSubStepFailure_FailsMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "group-failure", "sup-1", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{
		{
			ID:   "group-1",
			Task: "doomed group",
			SubSteps: []mission.Step{
				{ID: "sub-ok", Task: "fine"},
				{ID: "sub-boom", Task: "BOOM"},
			},
		},
	})

	b := bus.New()
	executor := func(_ context.Context, task worker.Task) (string, error) {
		if task.Task == "BOOM" {
			return "", errFail
		}
		return "ok", nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()

	mgrAgent := manager.New(b, "mgr-1")
	mgctx, mgcancel := context.WithCancel(ctx)
	defer mgcancel()
	go func() { _ = mgrAgent.Run(mgctx, m.ID) }()
	time.Sleep(80 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error when a group sub-step fails")
	}
	wcancel()
	mgcancel()

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
	steps, _ := store.GetSteps(ctx, m.ID)
	if steps[0].State != mission.StateFailed {
		t.Errorf("group step state = %s, want failed", steps[0].State)
	}
}

// --- Parallel-mode replanning tests (v0.4.1) ---

// TestRun_Parallel_ReplanRecovers verifies that a failing step in a
// parallel mission triggers a replan (after in-flight peers drain) and
// the replacement step lets the mission complete.
func TestRun_Parallel_ReplanRecovers(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "parallel recover", "sup-1", 0)
	// Three independent steps. "bad" fails on first attempt; the replan
	// replaces it (and any remaining unstarted steps) with "RECOVER".
	if err := mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{ID: "ok1", Task: "ok1"},
		{ID: "bad", Task: "FAIL-ME"},
		{ID: "ok2", Task: "ok2"},
	}); err != nil {
		t.Fatal(err)
	}

	b := bus.New()
	var failCount atomic.Int32
	executor := func(_ context.Context, task worker.Task) (string, error) {
		if task.Task == "FAIL-ME" {
			failCount.Add(1)
			return "", errFail
		}
		return "did:" + task.Task, nil
	}
	// Three workers so the independent steps genuinely run in parallel.
	for i := 0; i < 3; i++ {
		w := worker.New(b, "w", executor)
		wctx, wcancel := context.WithCancel(ctx)
		defer wcancel()
		go func() { _ = w.Run(wctx, m.ID) }()
	}
	time.Sleep(80 * time.Millisecond)

	replanner := &stubReplanner{
		respond: func(req supervisor.ReplanRequest) ([]mission.Step, error) {
			return []mission.Step{{Task: "RECOVER"}}, nil
		},
	}

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	s.Replanner = replanner

	if err := s.Run(ctx, m.ID); err != nil {
		t.Fatalf("parallel Run with replan: %v", err)
	}

	if got := replanner.callCount(); got != 1 {
		t.Fatalf("Replanner called %d times, want 1", got)
	}
	// The replan request should have seen the failed step and the
	// completed peers (which drained before the replan).
	call := replanner.lastCall()
	if call.FailedStep.ID != "bad" {
		t.Errorf("FailedStep.ID = %q, want bad", call.FailedStep.ID)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("mission state = %s, want completed", got.State)
	}

	// The "bad" step stays failed (audit trail); a RECOVER step
	// completed; ok1/ok2 completed.
	steps, _ := store.GetSteps(ctx, m.ID)
	var failedCount, completedCount, recovered int
	for _, st := range steps {
		switch st.State {
		case mission.StateFailed:
			failedCount++
		case mission.StateCompleted:
			completedCount++
			if st.Task == "RECOVER" {
				recovered++
			}
		}
	}
	if failedCount != 1 {
		t.Errorf("failed step count = %d, want 1 (the original 'bad')", failedCount)
	}
	if recovered != 1 {
		t.Errorf("RECOVER step completed count = %d, want 1", recovered)
	}
}

// TestRun_Parallel_ReplanRejected_FailsMission verifies that
// ErrReplanRejected in parallel mode fails the mission without
// consuming retries.
func TestRun_Parallel_ReplanRejected_FailsMission(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "parallel reject", "sup-1", 0)
	_ = mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{ID: "ok1", Task: "ok1"},
		{ID: "bad", Task: "FAIL-ME"},
	})

	b := bus.New()
	executor := func(_ context.Context, task worker.Task) (string, error) {
		if task.Task == "FAIL-ME" {
			return "", errFail
		}
		return "ok", nil
	}
	for i := 0; i < 2; i++ {
		w := worker.New(b, "w", executor)
		wctx, wcancel := context.WithCancel(ctx)
		defer wcancel()
		go func() { _ = w.Run(wctx, m.ID) }()
	}
	time.Sleep(80 * time.Millisecond)

	replanner := &stubReplanner{
		respond: func(req supervisor.ReplanRequest) ([]mission.Step, error) {
			return nil, supervisor.ErrReplanRejected
		},
	}

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	s.Replanner = replanner

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error after ErrReplanRejected in parallel mode")
	}
	if got := replanner.callCount(); got != 1 {
		t.Errorf("Replanner called %d times, want 1 (rejection, no retry)", got)
	}
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// TestRun_Parallel_MaxReplansExhausted verifies the per-mission replan
// cap applies in parallel mode: a step that keeps failing across
// replans eventually fails the mission after MaxReplans attempts.
func TestRun_Parallel_MaxReplansExhausted(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "parallel exhaust", "sup-1", 0)
	_ = mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{ID: "bad", Task: "FAIL-ALWAYS"},
	})

	b := bus.New()
	// Every task fails — including the replan replacements.
	executor := func(_ context.Context, _ worker.Task) (string, error) {
		return "", errFail
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, m.ID) }()
	time.Sleep(80 * time.Millisecond)

	replanner := &stubReplanner{
		respond: func(req supervisor.ReplanRequest) ([]mission.Step, error) {
			return []mission.Step{{Task: "FAIL-ALSO"}}, nil
		},
	}

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	s.Replanner = replanner
	s.MaxReplans = 2

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error after parallel replan attempts exhausted")
	}
	if got := replanner.callCount(); got != 2 {
		t.Errorf("Replanner called %d times, want 2 (MaxReplans)", got)
	}
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}

// TestRun_Parallel_NoReplanner_StillFailsFast verifies backwards
// compatibility: with no Replanner, a parallel step failure fails the
// mission exactly as in v0.3.x/v0.4.0.
func TestRun_Parallel_NoReplanner_StillFailsFast(t *testing.T) {
	store, mgr := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	m, _ := mgr.Create(ctx, "parallel no-replanner", "sup-1", 0)
	_ = mgr.SetPlanParallel(ctx, m.ID, []mission.Step{
		{ID: "ok1", Task: "ok1"},
		{ID: "bad", Task: "FAIL-ME"},
	})

	b := bus.New()
	executor := func(_ context.Context, task worker.Task) (string, error) {
		if task.Task == "FAIL-ME" {
			return "", errFail
		}
		return "ok", nil
	}
	for i := 0; i < 2; i++ {
		w := worker.New(b, "w", executor)
		wctx, wcancel := context.WithCancel(ctx)
		defer wcancel()
		go func() { _ = w.Run(wctx, m.ID) }()
	}
	time.Sleep(80 * time.Millisecond)

	s := supervisor.New(mgr, store, b, "sup-1")
	s.StepTimeout = 3 * time.Second
	// Replanner intentionally nil.

	err := s.Run(ctx, m.ID)
	if err == nil {
		t.Fatal("expected error from failed parallel step (no replanner)")
	}
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateFailed {
		t.Errorf("mission state = %s, want failed", got.State)
	}
}
