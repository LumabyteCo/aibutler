package missionruntime_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/missionruntime"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// stubModel is a tiny fake ModelAdapter for tests. It returns canned
// responses based on the last user message, captures call counts, and
// supports simulated tool calls.
//
// Complete honors ctx: if respond blocks past ctx's deadline (e.g. via
// a stub that sleeps), Complete returns ctx.Err() so timeout tests
// behave the way real network-backed adapters do.
type stubModel struct {
	respond func(messages []agent.Message) agent.Response
	calls   atomic.Int32
}

func (m *stubModel) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	m.calls.Add(1)
	resultCh := make(chan agent.Response, 1)
	go func() { resultCh <- m.respond(messages) }()
	select {
	case r := <-resultCh:
		return r, nil
	case <-ctx.Done():
		return agent.Response{}, ctx.Err()
	}
}

// stubTools is a no-op ToolExecutor — declares no tools, errors on Execute.
type stubTools struct{}

func (stubTools) Execute(_ context.Context, _ agent.ToolCall) (string, error) {
	return "", nil
}

func (stubTools) AvailableTools(_ context.Context, _ agent.Mode, _ *capability.CapabilitySet) []agent.ToolDef {
	return nil
}

func TestNewLLMExecutor_RequiresModelAndTools(t *testing.T) {
	if _, err := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{}); err == nil {
		t.Error("expected error when Model is nil")
	}
	if _, err := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{
		Model: &stubModel{},
	}); err == nil {
		t.Error("expected error when Tools is nil")
	}
}

func TestNewLLMExecutor_RunsTaskAndReturnsModelOutput(t *testing.T) {
	model := &stubModel{
		respond: func(_ []agent.Message) agent.Response {
			// No tool calls — terminate immediately with a final answer.
			return agent.Response{Content: "the answer is 42", TokensIn: 10, TokensOut: 5}
		},
	}
	exec, err := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{
		Model:       model,
		Tools:       stubTools{},
		StepTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLLMExecutor: %v", err)
	}

	out, err := exec(context.Background(), worker.Task{
		StepID:    "step-1",
		MissionID: "mis-1",
		Task:      "what is the answer to life?",
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("output = %q, want contains '42'", out)
	}
	if model.calls.Load() == 0 {
		t.Error("model adapter should have been called at least once")
	}
}

func TestLLMExecutor_PassesTaskToModel(t *testing.T) {
	var lastUserMsg string
	model := &stubModel{
		respond: func(messages []agent.Message) agent.Response {
			for _, m := range messages {
				if m.Role == "user" {
					lastUserMsg = m.Content
				}
			}
			return agent.Response{Content: "ok"}
		},
	}
	exec, _ := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{
		Model:       model,
		Tools:       stubTools{},
		StepTimeout: 2 * time.Second,
	})

	_, _ = exec(context.Background(), worker.Task{
		StepID:    "s",
		MissionID: "m",
		Task:      "the task body",
	})
	if lastUserMsg != "the task body" {
		t.Errorf("model received user message %q, want 'the task body'", lastUserMsg)
	}
}

func TestLLMExecutor_StepTimeout(t *testing.T) {
	// Model that hangs longer than the StepTimeout.
	model := &stubModel{
		respond: func(_ []agent.Message) agent.Response {
			// Sleep with no ctx awareness — the agent core should
			// still cancel via its own deadline.
			time.Sleep(300 * time.Millisecond)
			return agent.Response{Content: "late"}
		},
	}
	exec, _ := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{
		Model:       model,
		Tools:       stubTools{},
		StepTimeout: 100 * time.Millisecond,
	})

	start := time.Now()
	_, err := exec(context.Background(), worker.Task{
		StepID: "s", MissionID: "m", Task: "slow",
	})
	elapsed := time.Since(start)

	// On timeout the agent loop returns either an error or a cancelled
	// status — either way the executor should surface a non-nil err.
	if err == nil {
		t.Error("expected error on step timeout")
	}
	if elapsed > 700*time.Millisecond {
		t.Errorf("StepTimeout=100ms but executor took %v", elapsed)
	}
}

func TestLLMExecutor_DefaultsApplied(t *testing.T) {
	// Construct with a near-empty config — defaults should kick in
	// (StepTimeout, MaxToolCalls, BudgetCapPerStep, Caps).
	model := &stubModel{
		respond: func(_ []agent.Message) agent.Response { return agent.Response{Content: "ok"} },
	}
	exec, err := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{
		Model: model,
		Tools: stubTools{},
	})
	if err != nil {
		t.Fatalf("expected defaults to fill in, got: %v", err)
	}
	out, err := exec(context.Background(), worker.Task{StepID: "s", MissionID: "m", Task: "go"})
	if err != nil || out != "ok" {
		t.Errorf("default-config exec: out=%q err=%v", out, err)
	}
}

func TestLLMExecutor_PluggedIntoRuntime(t *testing.T) {
	// End-to-end-ish: create runtime with the LLM executor pointed at
	// a stub model, push a planned mission through, verify completion.
	store, mgr := newTestStore(t)

	model := &stubModel{
		respond: func(_ []agent.Message) agent.Response { return agent.Response{Content: "did it"} },
	}
	exec, err := missionruntime.NewLLMExecutor(missionruntime.LLMExecutorConfig{
		Model:       model,
		Tools:       stubTools{},
		StepTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	rt := missionruntime.New(mgr, store, bus.New(), missionruntime.Options{
		PollInterval: 50 * time.Millisecond,
		Executor:     exec,
	})

	ctx := context.Background()
	m, _ := mgr.Create(ctx, "test", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []mission.Step{{Task: "step 1"}})

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() { _ = rt.Start(runCtx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.GetMission(ctx, m.ID)
		if got.State == mission.StateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, _ := store.GetMission(ctx, m.ID)
	if got.State != mission.StateCompleted {
		t.Errorf("mission state = %s, want completed (model calls=%d)", got.State, model.calls.Load())
	}
	if model.calls.Load() == 0 {
		t.Error("LLM executor never called the model")
	}
	cancel()
	rt.Wait()
}
