package missionruntime_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/agent/missionruntime"
	"github.com/LumabyteCo/aibutler/internal/agent/supervisor"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// recordingModel captures every message slice it sees so the prompt-shape
// tests can assert without scanning the runtime logs. The Replanner
// calls Complete sequentially on one goroutine per Replan invocation,
// so plain slice append is race-free without a mutex.
type recordingModel struct {
	respond  func(attempt int, messages []agent.Message) agent.Response
	calls    atomic.Int32
	captured [][]agent.Message
}

func (m *recordingModel) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	attempt := int(m.calls.Add(1))
	// Capture a copy so callers comparing later see the snapshot at
	// call time, not after the slice has been reused.
	cp := make([]agent.Message, len(messages))
	copy(cp, messages)
	m.captured = append(m.captured, cp)
	resultCh := make(chan agent.Response, 1)
	go func() { resultCh <- m.respond(attempt, messages) }()
	select {
	case r := <-resultCh:
		return r, nil
	case <-ctx.Done():
		return agent.Response{}, ctx.Err()
	}
}

func sampleRequest() supervisor.ReplanRequest {
	return supervisor.ReplanRequest{
		MissionID: "mis_1",
		Goal:      "draft a release announcement",
		CompletedSteps: []mission.Step{
			{ID: "step_a", Task: "research recent product launches", Output: "found 3 examples"},
		},
		FailedStep: mission.Step{
			ID:    "step_b",
			Task:  "fetch metrics from grafana",
			State: mission.StateFailed,
			Error: "auth token expired",
		},
		FailureReason: "auth token expired",
		RemainingSteps: []mission.Step{
			{Task: "draft the body"},
			{Task: "send to reviewers"},
		},
		PriorReplans: 0,
	}
}

func TestNewLLMReplanner_RequiresModel(t *testing.T) {
	if _, err := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{}); err == nil {
		t.Error("expected error when Model is nil")
	}
}

func TestLLMReplanner_HappyPath_ReturnsParsedSteps(t *testing.T) {
	model := &recordingModel{
		respond: func(_ int, _ []agent.Message) agent.Response {
			return agent.Response{
				Content: `{"steps":[{"task":"rotate the token"},{"task":"retry the fetch"}],"reason":"refresh the auth then retry"}`,
			}
		},
	}
	rp, err := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{
		Model:   model,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewLLMReplanner: %v", err)
	}

	steps, err := rp.Replan(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Replan: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if steps[0].Task != "rotate the token" || steps[1].Task != "retry the fetch" {
		t.Errorf("step tasks = %+v, want [rotate the token, retry the fetch]", steps)
	}
	if got := model.calls.Load(); got != 1 {
		t.Errorf("model calls = %d, want 1 (clean response, no retries)", got)
	}

	// Prompt should mention the failed step's task and the goal — that's
	// what tells the model what it's recovering from.
	if len(model.captured) == 0 {
		t.Fatal("no captured prompts")
	}
	systemMsg := model.captured[0][0]
	userMsg := model.captured[0][1]
	if systemMsg.Role != "system" {
		t.Errorf("first message role = %q, want system", systemMsg.Role)
	}
	if !strings.Contains(userMsg.Content, "draft a release announcement") {
		t.Errorf("user prompt should mention the goal; got:\n%s", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "fetch metrics from grafana") {
		t.Errorf("user prompt should mention the failed task; got:\n%s", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "auth token expired") {
		t.Errorf("user prompt should mention the failure reason; got:\n%s", userMsg.Content)
	}
}

func TestLLMReplanner_EmptyStepsArray_RejectsReplan(t *testing.T) {
	model := &recordingModel{
		respond: func(_ int, _ []agent.Message) agent.Response {
			return agent.Response{
				Content: `{"steps":[],"reason":"cannot recover, the source is offline"}`,
			}
		},
	}
	rp, _ := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{Model: model})

	_, err := rp.Replan(context.Background(), sampleRequest())
	if !errors.Is(err, supervisor.ErrReplanRejected) {
		t.Errorf("err = %v, want supervisor.ErrReplanRejected", err)
	}
}

func TestLLMReplanner_MalformedThenRecovers(t *testing.T) {
	model := &recordingModel{
		respond: func(attempt int, _ []agent.Message) agent.Response {
			if attempt == 1 {
				// First attempt: deliberate garbage.
				return agent.Response{Content: "lol here is my plan: just retry"}
			}
			// Second attempt: well-formed.
			return agent.Response{
				Content: `{"steps":[{"task":"rotate"}],"reason":"after corrective re-prompt"}`,
			}
		},
	}
	rp, _ := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{
		Model:      model,
		MaxRetries: 2,
	})

	steps, err := rp.Replan(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Replan: %v", err)
	}
	if len(steps) != 1 || steps[0].Task != "rotate" {
		t.Errorf("steps = %+v, want [rotate]", steps)
	}
	if got := model.calls.Load(); got != 2 {
		t.Errorf("model calls = %d, want 2 (initial + one retry)", got)
	}

	// The retry should have added a corrective system-style "you broke
	// the format" turn after the assistant's malformed reply.
	if len(model.captured) < 2 {
		t.Fatal("expected at least 2 captured prompt slices")
	}
	retryMessages := model.captured[1]
	var hasCorrection bool
	for _, m := range retryMessages {
		if m.Role == "user" && strings.Contains(m.Content, "not valid JSON") {
			hasCorrection = true
			break
		}
	}
	if !hasCorrection {
		t.Error("retry should include the corrective user turn telling the model its prior output was invalid JSON")
	}
}

func TestLLMReplanner_MarkdownFenced_ParsesAnyway(t *testing.T) {
	model := &recordingModel{
		respond: func(_ int, _ []agent.Message) agent.Response {
			return agent.Response{
				Content: "Sure, here's the plan:\n```json\n{\"steps\":[{\"task\":\"retry\"}],\"reason\":\"simple\"}\n```\nLet me know if you need changes.",
			}
		},
	}
	rp, _ := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{Model: model})

	steps, err := rp.Replan(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Replan with fenced JSON: %v", err)
	}
	if len(steps) != 1 || steps[0].Task != "retry" {
		t.Errorf("steps = %+v, want [retry]", steps)
	}
}

func TestLLMReplanner_RetriesExhausted_ReturnsError(t *testing.T) {
	model := &recordingModel{
		respond: func(_ int, _ []agent.Message) agent.Response {
			return agent.Response{Content: "no JSON here ever"}
		},
	}
	rp, _ := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{
		Model:      model,
		MaxRetries: 2,
	})

	_, err := rp.Replan(context.Background(), sampleRequest())
	if err == nil {
		t.Fatal("expected error when all attempts produce malformed JSON")
	}
	if errors.Is(err, supervisor.ErrReplanRejected) {
		t.Error("malformed JSON should be a parse error, not ErrReplanRejected (which is reserved for explicit rejection)")
	}
	// Should be (MaxRetries+1) calls = 3.
	if got := model.calls.Load(); got != 3 {
		t.Errorf("model calls = %d, want 3 (initial + 2 retries)", got)
	}
}

func TestLLMReplanner_ModelError_PropagatesAsImplError(t *testing.T) {
	model := errorModel{err: errors.New("model down")}
	rp, _ := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{
		Model: &model,
	})

	_, err := rp.Replan(context.Background(), sampleRequest())
	if err == nil {
		t.Fatal("expected error from model failure")
	}
	if errors.Is(err, supervisor.ErrReplanRejected) {
		t.Error("model error should NOT be reported as ErrReplanRejected")
	}
}

type errorModel struct {
	err   error
	calls atomic.Int32
}

func (m *errorModel) Complete(_ context.Context, _ []agent.Message) (agent.Response, error) {
	m.calls.Add(1)
	return agent.Response{}, m.err
}
