package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/capability"
)

func TestEchoExecutor_DefaultsToOK(t *testing.T) {
	out, err := EchoExecutor(context.Background(), Task{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Errorf("EchoExecutor() = %q, want ok", out)
	}
}

func TestEchoExecutor_PassesInputThrough(t *testing.T) {
	out, _ := EchoExecutor(context.Background(), Task{Input: "hello"})
	if out != "hello" {
		t.Errorf("EchoExecutor(input=hello) = %q, want hello", out)
	}
}

func TestNew_NilExecutorFallsBackToEcho(t *testing.T) {
	w := New(bus.New(), "w-1", nil)
	if w.executor == nil {
		t.Fatal("executor should fall back to EchoExecutor when nil")
	}
}

func TestRun_RequiresMissionID(t *testing.T) {
	w := New(bus.New(), "w-1", EchoExecutor)
	if err := w.Run(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty missionID")
	}
}

func TestRun_ProcessesTaskAndPublishesResult(t *testing.T) {
	b := bus.New()

	// Subscribe to events BEFORE worker runs so we don't miss the result.
	events := b.SubscribeReliable("mission.M1.events")

	var capturedTask Task
	var executorCalls atomic.Int32
	executor := func(_ context.Context, t Task) (string, error) {
		executorCalls.Add(1)
		capturedTask = t
		return "task done: " + t.Task, nil
	}
	w := New(b, "w-1", executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "M1") }()

	// Give the worker a moment to subscribe.
	time.Sleep(50 * time.Millisecond)

	// Dispatch a task.
	taskJSON, _ := json.Marshal(Task{StepID: "step-1", MissionID: "M1", Task: "do thing"})
	if err := b.PublishCompeting(ctx, "mission.M1.dispatch", "supervisor", string(taskJSON), bus.ReliableOpts{
		Timeout: 1 * time.Second,
	}); err != nil {
		t.Fatalf("dispatch publish: %v", err)
	}

	// Receive the worker's result event.
	var result Result
	select {
	case msg := <-events:
		if err := json.Unmarshal([]byte(msg.Payload), &result); err != nil {
			t.Fatalf("result not JSON: %v", err)
		}
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result")
	}

	cancel()
	<-done

	if executorCalls.Load() != 1 {
		t.Errorf("executor should run once, got %d", executorCalls.Load())
	}
	if capturedTask.StepID != "step-1" || capturedTask.Task != "do thing" {
		t.Errorf("task not passed through correctly: %+v", capturedTask)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got %+v", result)
	}
	if !strings.Contains(result.Output, "task done") {
		t.Errorf("Output = %q, want contains 'task done'", result.Output)
	}
	if result.WorkerID != "w-1" {
		t.Errorf("WorkerID = %q, want w-1", result.WorkerID)
	}
}

func TestRun_ExecutorErrorBecomesFailedResult(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.M2.events")

	executor := func(_ context.Context, _ Task) (string, error) {
		return "", errors.New("kaboom")
	}
	w := New(b, "w-2", executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "M2") }()
	time.Sleep(50 * time.Millisecond)

	taskJSON, _ := json.Marshal(Task{StepID: "step-1", MissionID: "M2", Task: "x"})
	_ = b.PublishCompeting(ctx, "mission.M2.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 1 * time.Second})

	var result Result
	select {
	case msg := <-events:
		_ = json.Unmarshal([]byte(msg.Payload), &result)
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	cancel()
	<-done

	if result.Success {
		t.Error("expected Success=false on executor error")
	}
	if !strings.Contains(result.Error, "kaboom") {
		t.Errorf("Error = %q, want contains 'kaboom'", result.Error)
	}
}

func TestRun_MalformedPayload_AcksAndPublishesError(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.M3.events")

	w := New(b, "w-3", EchoExecutor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "M3") }()
	time.Sleep(50 * time.Millisecond)

	// Send NOT JSON.
	if err := b.PublishCompeting(ctx, "mission.M3.dispatch", "sup", "not json", bus.ReliableOpts{Timeout: 1 * time.Second}); err != nil {
		t.Fatalf("dispatch (malformed): %v", err)
	}

	var result Result
	select {
	case msg := <-events:
		_ = json.Unmarshal([]byte(msg.Payload), &result)
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — worker should still publish an error event for malformed payload")
	}

	cancel()
	<-done

	if result.Success {
		t.Error("malformed payload should produce Success=false")
	}
	if !strings.Contains(result.Error, "malformed") {
		t.Errorf("Error = %q, want contains 'malformed'", result.Error)
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	b := bus.New()
	w := New(b, "w-c", EchoExecutor)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MC") }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not exit on context cancellation")
	}
}

func TestRun_HandlesMultipleTasks_Sequentially(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.MX.events")

	var calls atomic.Int32
	var mu sync.Mutex
	var seen []string
	executor := func(_ context.Context, t Task) (string, error) {
		calls.Add(1)
		mu.Lock()
		seen = append(seen, t.StepID)
		mu.Unlock()
		return "ok", nil
	}
	w := New(b, "w-multi", executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MX") }()
	time.Sleep(50 * time.Millisecond)

	for _, stepID := range []string{"a", "b", "c"} {
		taskJSON, _ := json.Marshal(Task{StepID: stepID, MissionID: "MX", Task: "x"})
		_ = b.PublishCompeting(ctx, "mission.MX.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 1 * time.Second})
	}

	// Drain 3 result events.
	for i := 0; i < 3; i++ {
		select {
		case msg := <-events:
			msg.Ack()
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d/3 results", i)
		}
	}

	cancel()
	<-done

	if calls.Load() != 3 {
		t.Errorf("executor should run 3 times, got %d", calls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 || seen[0] != "a" || seen[1] != "b" || seen[2] != "c" {
		t.Errorf("step order wrong: %v", seen)
	}
}

// --- Per-worker concurrent handling tests ---

// TestRun_MaxConcurrent_GreaterThanOne_RunsTasksInParallel verifies
// that a single worker with MaxConcurrent=3 completes 3 concurrent
// tasks in roughly one task's wall-clock time, not three.
func TestRun_MaxConcurrent_GreaterThanOne_RunsTasksInParallel(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.MP.events")

	stepDelay := 200 * time.Millisecond
	executor := func(ctx context.Context, _ Task) (string, error) {
		select {
		case <-time.After(stepDelay):
			return "ok", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	w := New(b, "w-par", executor)
	w.MaxConcurrent = 3

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MP") }()
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	for _, stepID := range []string{"a", "b", "c"} {
		taskJSON, _ := json.Marshal(Task{StepID: stepID, MissionID: "MP", Task: "x"})
		if err := b.PublishCompeting(ctx, "mission.MP.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 1 * time.Second}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		select {
		case msg := <-events:
			msg.Ack()
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d/3 results", i)
		}
	}
	elapsed := time.Since(start)
	t.Logf("3 tasks × %s with MaxConcurrent=3 on one worker: %s wall-clock", stepDelay, elapsed)

	// Sequential would be ~600ms; parallel within one worker should
	// be roughly 200-350ms (one step + scheduling jitter). Bound at
	// 500ms for CI safety — well under the sequential floor.
	if elapsed >= 500*time.Millisecond {
		t.Errorf("wall-clock = %s, want < 500ms (sequential would be ~600ms)", elapsed)
	}

	cancel()
	<-done
}

// TestRun_MaxConcurrent_RespectsCap verifies that with MaxConcurrent=2
// and 4 tasks queued, no more than 2 are ever in flight at once.
func TestRun_MaxConcurrent_RespectsCap(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.MC.events")

	var inFlight atomic.Int32
	var maxObserved atomic.Int32
	executor := func(_ context.Context, _ Task) (string, error) {
		cur := inFlight.Add(1)
		// atomic max update — only if cur > maxObserved.
		for {
			prev := maxObserved.Load()
			if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		inFlight.Add(-1)
		return "ok", nil
	}
	w := New(b, "w-cap", executor)
	w.MaxConcurrent = 2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MC") }()
	time.Sleep(50 * time.Millisecond)

	for _, stepID := range []string{"a", "b", "c", "d"} {
		taskJSON, _ := json.Marshal(Task{StepID: stepID, MissionID: "MC", Task: "x"})
		if err := b.PublishCompeting(ctx, "mission.MC.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 2 * time.Second}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	for i := 0; i < 4; i++ {
		select {
		case msg := <-events:
			msg.Ack()
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out after %d/4 results", i)
		}
	}

	cancel()
	<-done

	if got := maxObserved.Load(); got > 2 {
		t.Errorf("max in-flight = %d, want <= MaxConcurrent (2)", got)
	}
	if got := maxObserved.Load(); got < 2 {
		t.Errorf("max in-flight = %d, want exactly 2 (cap should be reached, not just respected)", got)
	}
}

// TestRun_MaxConcurrent_DrainsInFlightBeforeReturning verifies that
// Run waits for any in-flight handler goroutines to complete before
// returning on cancellation — no leaked goroutines, no spurious
// missed results.
func TestRun_MaxConcurrent_DrainsInFlightBeforeReturning(t *testing.T) {
	b := bus.New()

	var inFlight atomic.Int32
	executor := func(ctx context.Context, _ Task) (string, error) {
		inFlight.Add(1)
		defer inFlight.Add(-1)
		// Long-ish sleep that ctx cancellation should be able to
		// interrupt. The handler must finish (one way or another)
		// before Run returns.
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
		}
		return "ok", nil
	}
	w := New(b, "w-drain", executor)
	w.MaxConcurrent = 3

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MD") }()
	time.Sleep(50 * time.Millisecond)

	for _, stepID := range []string{"a", "b", "c"} {
		taskJSON, _ := json.Marshal(Task{StepID: stepID, MissionID: "MD", Task: "x"})
		_ = b.PublishCompeting(ctx, "mission.MD.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 1 * time.Second})
	}

	// Give time for all 3 to be in flight.
	time.Sleep(100 * time.Millisecond)
	if got := inFlight.Load(); got != 3 {
		t.Errorf("inFlight before cancel = %d, want 3", got)
	}

	cancel()
	select {
	case <-done:
		// Run returned — verify no handlers are still in flight.
		if got := inFlight.Load(); got != 0 {
			t.Errorf("inFlight after Run returned = %d, want 0 (handlers should drain before Run exits)", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s after cancellation")
	}
}

// TestRun_MaxConcurrent_Default1_PreservesSequentialBehavior verifies
// that an unset (zero) MaxConcurrent gives identical semantics to
// MaxConcurrent=1: only one task in flight at any moment.
func TestRun_MaxConcurrent_Default1_PreservesSequentialBehavior(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.MS.events")

	var inFlight atomic.Int32
	var maxObserved atomic.Int32
	executor := func(_ context.Context, _ Task) (string, error) {
		cur := inFlight.Add(1)
		for {
			prev := maxObserved.Load()
			if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
				break
			}
		}
		time.Sleep(75 * time.Millisecond)
		inFlight.Add(-1)
		return "ok", nil
	}
	w := New(b, "w-seq", executor) // MaxConcurrent unset → zero → default 1

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MS") }()
	time.Sleep(50 * time.Millisecond)

	for _, stepID := range []string{"a", "b", "c"} {
		taskJSON, _ := json.Marshal(Task{StepID: stepID, MissionID: "MS", Task: "x"})
		_ = b.PublishCompeting(ctx, "mission.MS.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 1 * time.Second})
	}

	for i := 0; i < 3; i++ {
		select {
		case msg := <-events:
			msg.Ack()
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d/3 results", i)
		}
	}
	cancel()
	<-done

	if got := maxObserved.Load(); got != 1 {
		t.Errorf("max in-flight = %d, want 1 (default MaxConcurrent should be sequential)", got)
	}
}

// TestRun_ConfirmationRequiredError_PublishedAsNeedsConfirmation verifies
// the worker recognises capability.ConfirmationRequiredError and
// publishes the result with NeedsConfirmation=true + the reason populated.
// This is the layer immediately below the supervisor's auto-pause
// branch — locking it down here keeps the contract honest in isolation.
func TestRun_ConfirmationRequiredError_PublishedAsNeedsConfirmation(t *testing.T) {
	b := bus.New()
	events := b.SubscribeReliable("mission.MNC.events")

	executor := func(_ context.Context, _ Task) (string, error) {
		return "", &capability.ConfirmationRequiredError{
			Capability: "tool.shell.exec",
			Reason:     "granted",
		}
	}
	w := New(b, "w-nc", executor)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, "MNC") }()
	time.Sleep(50 * time.Millisecond)

	taskJSON, _ := json.Marshal(Task{StepID: "s-nc", MissionID: "MNC", Task: "x"})
	if err := b.PublishCompeting(ctx, "mission.MNC.dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 1 * time.Second}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case msg := <-events:
		msg.Ack()
		var res Result
		if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if !res.NeedsConfirmation {
			t.Error("Result.NeedsConfirmation = false, want true")
		}
		if res.Success {
			t.Error("Result.Success = true, want false (step did not complete)")
		}
		if !strings.Contains(res.ConfirmationReason, "tool.shell.exec") {
			t.Errorf("Result.ConfirmationReason = %q, want it to mention the capability", res.ConfirmationReason)
		}
		if !strings.Contains(res.Error, "tool.shell.exec") {
			t.Errorf("Result.Error = %q, want it to mention the capability", res.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result event")
	}

	cancel()
	<-done
}
