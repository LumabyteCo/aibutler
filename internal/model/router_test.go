package model_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/model"
)

// recordingAdapter records calls and returns a fixed response.
type recordingAdapter struct {
	name  string
	calls int
	resp  agent.Response
	err   error
}

func (r *recordingAdapter) Complete(_ context.Context, _ []agent.Message) (agent.Response, error) {
	r.calls++
	return r.resp, r.err
}

func newRouter() (*model.Router, *recordingAdapter, *recordingAdapter) {
	local := &recordingAdapter{name: "local", resp: agent.Response{Content: "local-answer"}}
	primary := &recordingAdapter{name: "primary", resp: agent.Response{Content: "primary-answer"}}
	r := model.NewRouter(model.RouterConfig{
		Local:        local,
		LocalName:    "Local (test)",
		Primary:      primary,
		PrimaryName:  "Cloud (test)",
	})
	return r, local, primary
}

func msgs(text string) []agent.Message {
	return []agent.Message{
		{Role: "system", Content: "You are Butler."},
		{Role: "user", Content: text},
	}
}

// TestRouterFastIntentsGoLocal — the Phase 2 headline: short device-command
// utterances must hit the local model (sub-2s, offline-capable).
func TestRouterFastIntentsGoLocal(t *testing.T) {
	fast := []string{
		"turn off the kitchen light",
		"Turn on the living room light",
		"goodnight",
		"Good night!",
		"lock the front door",
		"lights off",
		"set the thermostat to 21",
		"arm the alarm",
	}
	for _, text := range fast {
		t.Run(text, func(t *testing.T) {
			r, local, primary := newRouter()
			resp, err := r.Complete(context.Background(), msgs(text))
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if local.calls != 1 {
				t.Errorf("fast intent should route local: local calls = %d", local.calls)
			}
			if primary.calls != 0 {
				t.Errorf("primary should not be called for fast intent: %d", primary.calls)
			}
			if resp.Content != "local-answer" {
				t.Errorf("expected local answer, got %q", resp.Content)
			}
		})
	}
}

// TestRouterReasoningGoesPrimary — everything that isn't a short command
// stays on the primary model: memory recall, questions, files, long text.
func TestRouterReasoningGoesPrimary(t *testing.T) {
	reasoning := []string{
		"What do you remember about me? Summarize my preferences and my current project.",
		"turn off the kitchen light and also check whether the front door is locked, then summarize today's weather and remind me what I asked you to remember about Project Apollo",
		"Please save these facts about me: my name is Sam, I live in Berlin, and I prefer Go for backend work",
	}
	for _, text := range reasoning {
		t.Run(text[:30], func(t *testing.T) {
			r, local, primary := newRouter()
			resp, err := r.Complete(context.Background(), msgs(text))
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if primary.calls != 1 {
				t.Errorf("reasoning should route primary: primary calls = %d", primary.calls)
			}
			if local.calls != 0 {
				t.Errorf("local should not be called for reasoning: %d", local.calls)
			}
			if resp.Content != "primary-answer" {
				t.Errorf("expected primary answer, got %q", resp.Content)
			}
		})
	}
}

// TestRouterToolLoopStaysPrimary — mid-agent-loop requests (tool results in
// flight) must never go to the tiny model: it would try to reason over
// tool schemas it can't handle.
func TestRouterToolLoopStaysPrimary(t *testing.T) {
	r, local, primary := newRouter()
	m := []agent.Message{
		{Role: "system", Content: "You are Butler."},
		{Role: "user", Content: "turn off the kitchen light"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "tc-1", Name: "iot.device.control", Input: `{}`}}},
		{Role: "tool", ToolID: "tc-1", Content: "Device light-kitchen: off executed."},
	}
	if _, err := r.Complete(context.Background(), m); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if local.calls != 0 {
		t.Errorf("tool-loop turn must route primary; local calls = %d", local.calls)
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls)
	}
}

// TestRouterLocalFailureFallsBack — offline/broken local model degrades to
// primary instead of failing the user's command.
func TestRouterLocalFailureFallsBack(t *testing.T) {
	local := &recordingAdapter{name: "local", err: context.DeadlineExceeded}
	primary := &recordingAdapter{name: "primary", resp: agent.Response{Content: "primary-answer"}}
	r := model.NewRouter(model.RouterConfig{Local: local, Primary: primary})

	resp, err := r.Complete(context.Background(), msgs("turn off the kitchen light"))
	if err != nil {
		t.Fatalf("expected graceful fallback, got: %v", err)
	}
	if primary.calls != 1 {
		t.Errorf("primary should take over on local failure: %d", primary.calls)
	}
	if resp.Content != "primary-answer" {
		t.Errorf("expected primary answer after local failure, got %q", resp.Content)
	}
}

// TestRouterPrimaryFailureFallsToLocal — cloud down → local answers, and
// the response is marked reduced-reasoning so the caller can surface it.
func TestRouterPrimaryFailureFallsToLocal(t *testing.T) {
	local := &recordingAdapter{name: "local", resp: agent.Response{Content: "local-answer"}}
	primary := &recordingAdapter{name: "primary", err: context.DeadlineExceeded}
	r := model.NewRouter(model.RouterConfig{Local: local, Primary: primary})

	// A reasoning request (routes primary by shape) whose primary fails:
	resp, err := r.Complete(context.Background(), msgs("What do you remember about my projects and preferences?"))
	if err != nil {
		t.Fatalf("expected graceful fallback, got: %v", err)
	}
	if local.calls != 1 {
		t.Errorf("local should take over on primary failure: %d", local.calls)
	}
	if resp.Content != "local-answer" {
		t.Errorf("expected local answer, got %q", resp.Content)
	}
	if !strings.Contains(resp.Note, "reduced reasoning") {
		t.Errorf("fallback answer should carry the reduced-reasoning note, got %q", resp.Note)
	}
}

// TestRouterNilSides — a router with one side disabled behaves like that
// side directly (backwards compatibility for configs without local).
func TestRouterNilSides(t *testing.T) {
	primary := &recordingAdapter{name: "primary", resp: agent.Response{Content: "p"}}
	r := model.NewRouter(model.RouterConfig{Primary: primary})
	if _, err := r.Complete(context.Background(), msgs("turn off the kitchen light")); err != nil {
		t.Fatalf("nil-local router: %v", err)
	}
	if primary.calls != 1 {
		t.Errorf("nil-local router should route primary: %d", primary.calls)
	}

	local := &recordingAdapter{name: "local", resp: agent.Response{Content: "l"}}
	r2 := model.NewRouter(model.RouterConfig{Local: local})
	if _, err := r2.Complete(context.Background(), msgs("what is the meaning of life?")); err != nil {
		t.Fatalf("nil-primary router: %v", err)
	}
	if local.calls != 1 {
		t.Errorf("nil-primary router should route local: %d", local.calls)
	}
}
// TestRouterFastIntentWithHistory — regression for the live-found bug: a
// short device command must route local even when the session carries
// PRIOR conversation history (the sliding window grows with the chat, so
// "message count <= 3" was the wrong gate — a fast intent after any
// conversation silently degraded to the primary model).
func TestRouterFastIntentWithHistory(t *testing.T) {
	r, local, primary := newRouter()
	m := []agent.Message{
		{Role: "system", Content: "You are Butler."},
		{Role: "user", Content: "What do you remember about my projects?"},
		{Role: "assistant", Content: "Project Apollo, the internal API gateway."},
		{Role: "user", Content: "Interesting."},
		{Role: "assistant", Content: "It's a big one."},
		{Role: "user", Content: "turn off the kitchen light"}, // the fast intent
	}
	if _, err := r.Complete(context.Background(), m); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if local.calls != 1 {
		t.Errorf("fast intent with prior history must still route local: local=%d", local.calls)
	}
	if primary.calls != 0 {
		t.Errorf("primary should not be called: %d", primary.calls)
	}
}

// TestRouterMidFlightToolLoopNotLocal — the inverse guard: a short command
// sitting AFTER a dangling tool result (agent loop mid-flight) must NOT
// route local even if it looks like a fast intent.
func TestRouterMidFlightToolLoopNotLocal(t *testing.T) {
	r, local, primary := newRouter()
	m := []agent.Message{
		{Role: "system", Content: "You are Butler."},
		{Role: "user", Content: "list devices"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "tc1", Name: "iot.device.list", Input: "{}"}}},
		{Role: "tool", ToolID: "tc1", Content: "[...devices...]"},
		// the "final user turn" here is actually the model being asked to
		// continue after tool output — represented by a trailing user msg:
		{Role: "user", Content: "turn off the kitchen light"},
	}
	if _, err := r.Complete(context.Background(), m); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if local.calls != 0 {
		t.Errorf("mid-flight tool loop must route primary; local=%d", local.calls)
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls)
	}
}
