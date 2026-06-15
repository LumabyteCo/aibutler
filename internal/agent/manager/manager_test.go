package manager_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/manager"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// publishGroup dispatches a group task to the manager_dispatch topic
// with the sub-step list JSON-encoded in Task.Input — exactly the
// shape the supervisor's runStep produces for a step carrying
// SubSteps.
func publishGroup(t *testing.T, ctx context.Context, b *bus.Bus, missionID, parentStepID, parentTask string, subSteps []mission.Step) {
	t.Helper()
	subJSON, err := json.Marshal(subSteps)
	if err != nil {
		t.Fatalf("marshal sub-steps: %v", err)
	}
	taskJSON, err := json.Marshal(worker.Task{
		StepID:    parentStepID,
		MissionID: missionID,
		Task:      parentTask,
		Input:     string(subJSON),
	})
	if err != nil {
		t.Fatalf("marshal group task: %v", err)
	}
	if err := b.PublishCompeting(ctx, "mission."+missionID+".manager_dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 2 * time.Second}); err != nil {
		t.Fatalf("publish group: %v", err)
	}
}

// TestManager_DelegatesSubStepsAndAggregates verifies the core
// happy path: a group task with three sub-steps is decomposed,
// each sub-step runs on the worker pool, and the manager publishes
// a single aggregated parent Result on the events topic.
func TestManager_DelegatesSubStepsAndAggregates(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A real worker handling leaf sub-steps. Echoes "done:<task>".
	var seen []string
	var mu sync.Mutex
	executor := func(_ context.Context, task worker.Task) (string, error) {
		mu.Lock()
		seen = append(seen, task.Task)
		mu.Unlock()
		return "done:" + task.Task, nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, "MG") }()

	// The manager under test.
	m := manager.New(b, "mgr-1")
	mctx, mcancel := context.WithCancel(ctx)
	defer mcancel()
	go func() { _ = m.Run(mctx, "MG") }()

	// Supervisor stand-in: subscribe to events to receive the
	// aggregated parent Result.
	events := b.SubscribeReliable("mission.MG.events")
	time.Sleep(80 * time.Millisecond) // let worker + manager subscribe

	publishGroup(t, ctx, b, "MG", "parent-1", "do the group", []mission.Step{
		{ID: "sub-a", Task: "alpha"},
		{ID: "sub-b", Task: "beta"},
		{ID: "sub-c", Task: "gamma"},
	})

	// Drain events until the parent Result lands. Sub-step results also
	// flow on this topic; filter for the parent step ID.
	var parent worker.Result
	deadline := time.After(3 * time.Second)
	for parent.StepID == "" {
		select {
		case msg := <-events:
			msg.Ack()
			var res worker.Result
			if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
				continue
			}
			if res.StepID == "parent-1" {
				parent = res
			}
		case <-deadline:
			t.Fatal("timed out waiting for aggregated parent result")
		}
	}

	if !parent.Success {
		t.Errorf("parent result Success = false, want true (error: %s)", parent.Error)
	}
	if parent.WorkerID != "mgr-1" {
		t.Errorf("parent result WorkerID = %q, want mgr-1 (the manager)", parent.WorkerID)
	}
	// Aggregated output should mention every sub-step's id + output.
	for _, want := range []string{"sub-a: done:alpha", "sub-b: done:beta", "sub-c: done:gamma"} {
		if !strings.Contains(parent.Output, want) {
			t.Errorf("aggregated output missing %q; got:\n%s", want, parent.Output)
		}
	}

	mu.Lock()
	if len(seen) != 3 {
		t.Errorf("worker ran %d sub-steps, want 3", len(seen))
	}
	mu.Unlock()
}

// TestManager_SubStepFailure_FailsGroup verifies that one failing
// sub-step terminates the group with a parent-level error rather than
// a partial success.
func TestManager_SubStepFailure_FailsGroup(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	executor := func(_ context.Context, task worker.Task) (string, error) {
		if task.Task == "BOOM" {
			return "", errBoom
		}
		return "ok", nil
	}
	w := worker.New(b, "w-1", executor)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = w.Run(wctx, "MF") }()

	m := manager.New(b, "mgr-1")
	mctx, mcancel := context.WithCancel(ctx)
	defer mcancel()
	go func() { _ = m.Run(mctx, "MF") }()

	events := b.SubscribeReliable("mission.MF.events")
	time.Sleep(80 * time.Millisecond)

	publishGroup(t, ctx, b, "MF", "parent-x", "group", []mission.Step{
		{ID: "ok-1", Task: "fine"},
		{ID: "boom-1", Task: "BOOM"},
		{ID: "never", Task: "should-not-run"},
	})

	var parent worker.Result
	deadline := time.After(3 * time.Second)
	for parent.StepID == "" {
		select {
		case msg := <-events:
			msg.Ack()
			var res worker.Result
			if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
				continue
			}
			if res.StepID == "parent-x" {
				parent = res
			}
		case <-deadline:
			t.Fatal("timed out waiting for parent result")
		}
	}

	if parent.Success {
		t.Error("parent result Success = true, want false (a sub-step failed)")
	}
	if !strings.Contains(parent.Error, "boom-1") {
		t.Errorf("parent error = %q, want it to mention the failed sub-step boom-1", parent.Error)
	}
}

// TestManager_MalformedSubStepList_FailsGroup verifies a malformed
// Input (not a JSON step array) surfaces as a parent-level failure
// rather than a hang or panic.
func TestManager_MalformedSubStepList_FailsGroup(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m := manager.New(b, "mgr-1")
	mctx, mcancel := context.WithCancel(ctx)
	defer mcancel()
	go func() { _ = m.Run(mctx, "MM") }()

	events := b.SubscribeReliable("mission.MM.events")
	time.Sleep(80 * time.Millisecond)

	// Publish a group task whose Input is NOT a valid step-array.
	taskJSON, _ := json.Marshal(worker.Task{
		StepID:    "parent-bad",
		MissionID: "MM",
		Task:      "group",
		Input:     "this is not json",
	})
	if err := b.PublishCompeting(ctx, "mission.MM.manager_dispatch", "sup", string(taskJSON), bus.ReliableOpts{Timeout: 2 * time.Second}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case msg := <-events:
		msg.Ack()
		var res worker.Result
		if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if res.StepID != "parent-bad" {
			t.Errorf("result StepID = %q, want parent-bad", res.StepID)
		}
		if res.Success {
			t.Error("result Success = true, want false (malformed sub-step list)")
		}
		if !strings.Contains(res.Error, "sub-step list") {
			t.Errorf("error = %q, want it to mention the malformed sub-step list", res.Error)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failure result")
	}
}

// TestManager_EmptySubStepList_FailsGroup verifies an empty sub-step
// array is rejected (a group with no sub-steps is a plan bug).
func TestManager_EmptySubStepList_FailsGroup(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m := manager.New(b, "mgr-1")
	mctx, mcancel := context.WithCancel(ctx)
	defer mcancel()
	go func() { _ = m.Run(mctx, "ME") }()

	events := b.SubscribeReliable("mission.ME.events")
	time.Sleep(80 * time.Millisecond)

	publishGroup(t, ctx, b, "ME", "parent-empty", "group", []mission.Step{})

	select {
	case msg := <-events:
		msg.Ack()
		var res worker.Result
		_ = json.Unmarshal([]byte(msg.Payload), &res)
		if res.Success {
			t.Error("result Success = true, want false (empty sub-step list)")
		}
		if !strings.Contains(res.Error, "empty sub-step list") {
			t.Errorf("error = %q, want 'empty sub-step list'", res.Error)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failure result")
	}
}

// TestManager_ConcurrentGroups_DistinctResults verifies two managers
// competing on the same manager_dispatch topic each handle exactly
// one of two group tasks (competing-consumer semantics), and both
// parent results land.
func TestManager_ConcurrentGroups_DistinctResults(t *testing.T) {
	b := bus.New()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var workerCalls atomic.Int32
	executor := func(_ context.Context, task worker.Task) (string, error) {
		workerCalls.Add(1)
		return "done:" + task.Task, nil
	}
	// Two workers so sub-steps from both groups can run.
	for i := 0; i < 2; i++ {
		w := worker.New(b, "w", executor)
		wctx, wcancel := context.WithCancel(ctx)
		defer wcancel()
		go func() { _ = w.Run(wctx, "MC") }()
	}
	// Two managers competing for group dispatches.
	for i := 0; i < 2; i++ {
		m := manager.New(b, "mgr")
		mctx, mcancel := context.WithCancel(ctx)
		defer mcancel()
		go func() { _ = m.Run(mctx, "MC") }()
	}

	events := b.SubscribeReliable("mission.MC.events")
	time.Sleep(100 * time.Millisecond)

	publishGroup(t, ctx, b, "MC", "g1", "group one", []mission.Step{{ID: "g1-a", Task: "a1"}})
	publishGroup(t, ctx, b, "MC", "g2", "group two", []mission.Step{{ID: "g2-a", Task: "a2"}})

	got := map[string]bool{}
	deadline := time.After(4 * time.Second)
	for len(got) < 2 {
		select {
		case msg := <-events:
			msg.Ack()
			var res worker.Result
			if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
				continue
			}
			if res.StepID == "g1" || res.StepID == "g2" {
				if !res.Success {
					t.Errorf("group %s failed: %s", res.StepID, res.Error)
				}
				got[res.StepID] = true
			}
		case <-deadline:
			t.Fatalf("timed out; got parent results for %v", got)
		}
	}

	if !got["g1"] || !got["g2"] {
		t.Errorf("missing parent results: got %v, want both g1 and g2", got)
	}
}

// errBoom is a reusable sub-step failure error.
var errBoom = stringErr("sub-step exploded")

type stringErr string

func (e stringErr) Error() string { return string(e) }
