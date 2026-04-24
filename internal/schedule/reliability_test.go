package schedule_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/schedule"
	"github.com/LumabyteCo/aibutler/testutil"
)

// reliabilityRunner is a test runner that counts calls and can be configured to fail.
type reliabilityRunner struct {
	calls    atomic.Int32
	failN    int32 // fail the first N calls
	resultID string
}

func (r *reliabilityRunner) Run(_ context.Context, sessionID, task, ch string) (*agent.Result, error) {
	n := r.calls.Add(1)
	if n <= r.failN {
		return nil, fmt.Errorf("deliberate failure %d", n)
	}
	return &agent.Result{ID: r.resultID, Output: "ok"}, nil
}

func TestRunWithRetry_Success(t *testing.T) {
	db := testutil.TestDB(t)
	store := schedule.NewStore(db.Conn())
	runner := &reliabilityRunner{resultID: "agent-1"}

	sched := schedule.NewScheduler(store, runner, time.Minute)

	s := schedule.Schedule{
		ID:       "test-retry-ok",
		Name:     "test-retry-ok",
		CronExpr: "0 * * * *",
		Task:     "say hello",
		Channel:  "test",
		Enabled:  true,
	}

	ctx := context.Background()
	err := sched.RunWithRetry(ctx, s, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.calls.Load() != 1 {
		t.Errorf("expected 1 call, got %d", runner.calls.Load())
	}
}

func TestRunWithRetry_FailThenSucceed(t *testing.T) {
	db := testutil.TestDB(t)
	store := schedule.NewStore(db.Conn())
	runner := &reliabilityRunner{failN: 2, resultID: "agent-2"}

	sched := schedule.NewScheduler(store, runner, time.Minute)

	s := schedule.Schedule{
		ID:       "test-retry-fail",
		Name:     "test-retry-fail",
		CronExpr: "0 * * * *",
		Task:     "say hello",
		Channel:  "test",
		Enabled:  true,
	}

	ctx := context.Background()
	err := sched.RunWithRetry(ctx, s, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have taken 3 calls (2 failures + 1 success).
	if runner.calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", runner.calls.Load())
	}
}

func TestRecoverMissed(t *testing.T) {
	db := testutil.TestDB(t)
	store := schedule.NewStore(db.Conn())
	ctx := context.Background()

	// Create a schedule.
	s := &schedule.Schedule{
		ID:       "test-recover",
		Name:     "test-recover",
		CronExpr: "*/5 * * * *", // every 5 minutes (minimum allowed)
		Task:     "recover task",
		Channel:  "test",
		Enabled:  true,
	}
	if err := store.Create(ctx, s); err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	// Record a run from 10 minutes ago.
	past := time.Now().UTC().Add(-10 * time.Minute)
	run := &schedule.Run{
		ScheduleID: s.ID,
		Status:     "completed",
		StartedAt:  past,
	}
	if err := store.RecordRun(ctx, run); err != nil {
		t.Fatalf("record run: %v", err)
	}

	runner := &reliabilityRunner{resultID: "agent-recover"}
	sched := schedule.NewScheduler(store, runner, time.Minute)
	sched.SetRunner(runner)

	count, err := sched.RecoverMissed(ctx)
	if err != nil {
		t.Fatalf("recover missed: %v", err)
	}
	if count != 1 {
		t.Errorf("recovered %d, want 1", count)
	}

	// Give goroutine a moment to run.
	time.Sleep(100 * time.Millisecond)

	if runner.calls.Load() < 1 {
		t.Error("expected at least 1 runner call from recovery")
	}
}
