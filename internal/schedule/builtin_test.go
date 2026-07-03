package schedule_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/schedule"
)

// capsRunner records the capability list it was invoked with.
type capsRunner struct {
	mu       sync.Mutex
	called   bool
	capsGot  []string
	plainRun bool
}

func (f *capsRunner) Run(_ context.Context, sessionID, task, channel string) (*agent.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.plainRun = true
	return &agent.Result{Status: agent.StateCompleted}, nil
}

func (f *capsRunner) RunWithCapabilities(_ context.Context, sessionID, task, channel string, caps []string) (*agent.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.capsGot = append([]string(nil), caps...)
	return &agent.Result{Status: agent.StateCompleted}, nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func backdate(t *testing.T, env *testEnv, id string) {
	t.Helper()
	if _, err := env.conn.ExecContext(context.Background(),
		`UPDATE schedules SET created_at = datetime('now', '-10 minutes') WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
}

// A schedule that declares capabilities routes through the capability-scoped
// runner with exactly that list.
func TestTickUsesScopedCapabilities(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	sched := &schedule.Schedule{
		ID: "s-caps", Name: "ScopedJob", CronExpr: "*/5 * * * *",
		Task: "summarize the day", Channel: "internal", AccountID: "u1",
		Capabilities: []string{"memory.read", "memory.write"},
		Enabled:      true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatal(err)
	}
	backdate(t, env, sched.ID)

	// Round-trip: capabilities survive storage.
	loaded, err := env.store.Get(ctx, sched.ID)
	if err != nil || len(loaded.Capabilities) != 2 || loaded.Capabilities[0] != "memory.read" {
		t.Fatalf("capabilities round-trip failed: %+v (err %v)", loaded, err)
	}

	runner := &capsRunner{}
	s := schedule.NewScheduler(env.store, runner, time.Minute)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return runner.called
	})
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.plainRun {
		t.Fatal("scoped schedule must not use the default-capability run path")
	}
	if len(runner.capsGot) != 2 || runner.capsGot[0] != "memory.read" || runner.capsGot[1] != "memory.write" {
		t.Fatalf("runner got capabilities %v, want the schedule's list", runner.capsGot)
	}
}

// Builtin tasks dispatch to registered code — no model runner involved —
// and run even when no runner is configured at all.
func TestBuiltinDispatchWithoutRunner(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	s := schedule.NewScheduler(env.store, nil, time.Minute)
	ran := make(chan struct{}, 1)
	s.RegisterBuiltin("test.maintenance", func(ctx context.Context) (string, error) {
		ran <- struct{}{}
		return "ok", nil
	})
	if err := s.EnsureBuiltinSchedule(ctx, "test-maintenance", "*/5 * * * *", "test.maintenance",
		[]string{"memory.read"}); err != nil {
		t.Fatal(err)
	}
	backdate(t, env, "sched_builtin_test.maintenance")

	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("builtin never ran")
	}

	// The run is on the record like any scheduled job.
	waitFor(t, func() bool {
		last, err := env.store.LastRun(ctx, "sched_builtin_test.maintenance")
		return err == nil && last != nil && last.Status == "completed"
	})
}

// EnsureBuiltinSchedule is idempotent across restarts.
func TestEnsureBuiltinScheduleIdempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	s := schedule.NewScheduler(env.store, nil, time.Minute)

	for i := 0; i < 3; i++ {
		if err := s.EnsureBuiltinSchedule(ctx, "maint", "0 3 * * *", "m", nil); err != nil {
			t.Fatalf("ensure %d: %v", i, err)
		}
	}
	all, err := env.store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range all {
		if e.Name == "maint" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 schedule row, got %d", count)
	}
}

// An unknown builtin key fails the run visibly instead of silently passing.
func TestUnknownBuiltinFailsRun(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	s := schedule.NewScheduler(env.store, nil, time.Minute)

	if err := s.EnsureBuiltinSchedule(ctx, "ghost", "*/5 * * * *", "not.registered", nil); err != nil {
		t.Fatal(err)
	}
	backdate(t, env, "sched_builtin_not.registered")
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		last, err := env.store.LastRun(ctx, "sched_builtin_not.registered")
		return err == nil && last != nil && last.Status == "failed"
	})
}

// The builtin: reservation holds at the store level, so every creation
// surface inherits it.
func TestStoreRejectsReservedTaskPrefix(t *testing.T) {
	env := newTestEnv(t)
	err := env.store.Create(context.Background(), &schedule.Schedule{
		ID: "s-evil", Name: "Sneaky", CronExpr: "*/5 * * * *",
		Task: "builtin:memory.maintenance", Channel: "internal", AccountID: "u1", Enabled: true,
	})
	if err == nil {
		t.Fatal("store must reject reserved builtin task prefixes")
	}
}

// Downtime recovery uses the same dispatch as the tick loop: builtins run
// their registered code (no model), scoped schedules keep their profile.
func TestRecoverMissedDispatchesBuiltinsAndProfiles(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	runner := &capsRunner{}
	s := schedule.NewScheduler(env.store, runner, time.Minute)
	ran := make(chan struct{}, 1)
	s.RegisterBuiltin("recover.me", func(ctx context.Context) (string, error) {
		ran <- struct{}{}
		return "ok", nil
	})
	if err := s.EnsureBuiltinSchedule(ctx, "recover-builtin", "*/5 * * * *", "recover.me", nil); err != nil {
		t.Fatal(err)
	}
	backdate(t, env, "sched_builtin_recover.me")

	scoped := &schedule.Schedule{
		ID: "s-rec-caps", Name: "RecScoped", CronExpr: "*/5 * * * *",
		Task: "do it", Channel: "internal", AccountID: "u1",
		Capabilities: []string{"memory.read"}, Enabled: true,
	}
	if err := env.store.Create(ctx, scoped); err != nil {
		t.Fatal(err)
	}
	backdate(t, env, scoped.ID)

	n, err := s.RecoverMissed(ctx)
	if err != nil || n != 2 {
		t.Fatalf("recovered = %d (err %v), want 2", n, err)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("builtin was not dispatched to its registered code during recovery")
	}
	waitFor(t, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return runner.called && !runner.plainRun && len(runner.capsGot) == 1
	})
}

// A schedule that declares capabilities on a runner that cannot scope them
// fails closed instead of silently running with the full default set.
func TestScopedScheduleFailsClosedOnPlainRunner(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	sched := &schedule.Schedule{
		ID: "s-plain", Name: "PlainRunner", CronExpr: "*/5 * * * *",
		Task: "do it", Channel: "internal", AccountID: "u1",
		Capabilities: []string{"memory.read"}, Enabled: true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatal(err)
	}
	backdate(t, env, sched.ID)

	runner := &fakeRunner{result: &agent.Result{Status: agent.StateCompleted}}
	s := schedule.NewScheduler(env.store, runner, time.Minute)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		last, err := env.store.LastRun(ctx, sched.ID)
		return err == nil && last != nil && last.Status == "failed"
	})
	if called, _ := runner.wasCalled(); called {
		t.Fatal("plain runner must not execute a capability-scoped schedule")
	}
}
