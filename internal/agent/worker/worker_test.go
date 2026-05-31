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
