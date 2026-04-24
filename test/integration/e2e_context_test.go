//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2ETaskContextSave verifies that task.context.save persists a task
// context into the task_contexts table.
func TestE2ETaskContextSave(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithTaskCtx: true,
		Responses: []agent.Response{
			// Turn 1: model calls task.context.save
			toolCallResponse("Saving task context.",
				tc("tc1", "task.context.save", `{"session_id":"sess-test","task_type":"booking","context":{"step":"gathering"}}`),
			),
			// Turn 1 continued: final reply
			finalResponse("Task context saved. I'm tracking your booking flow."),
		},
	})

	p.sendMsg(t, "Start a booking")

	// Verify final response.
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "booking") {
		t.Errorf("response = %q, want mention of booking", resp)
	}

	// Verify task context was persisted.
	count := p.countRows(t, "task_contexts")
	if count != 1 {
		t.Fatalf("task_contexts rows = %d, want 1", count)
	}

	taskType := p.querySingleString(t, "SELECT task_type FROM task_contexts LIMIT 1")
	if taskType != "booking" {
		t.Errorf("task_type = %q, want 'booking'", taskType)
	}

	state := p.querySingleString(t, "SELECT state FROM task_contexts LIMIT 1")
	if state != "gathering" {
		t.Errorf("state = %q, want 'gathering'", state)
	}

	// Verify context JSON contains the step field.
	ctxJSON := p.querySingleString(t, "SELECT context FROM task_contexts LIMIT 1")
	if !strings.Contains(ctxJSON, "gathering") {
		t.Errorf("context JSON = %q, want it to contain 'gathering'", ctxJSON)
	}
}

// TestE2ETaskContextLoadRoundTrip saves a context in turn 1, then loads
// it in turn 2 to verify the round-trip.
func TestE2ETaskContextLoadRoundTrip(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithTaskCtx: true,
		Responses: []agent.Response{
			// Turn 1: save context
			toolCallResponse("Saving context.",
				tc("tc1", "task.context.save", `{"session_id":"sess-roundtrip","task_type":"booking","context":{"step":"gathering","destination":"Paris"}}`),
			),
			finalResponse("Context saved for your booking."),

			// Turn 2: load context
			toolCallResponse("Loading context.",
				tc("tc2", "task.context.load", `{"session_id":"sess-roundtrip"}`),
			),
			finalResponse("I found your booking context. You're booking a trip to Paris and we're in the gathering step."),
		},
	})

	// Turn 1: save context.
	p.sendMsg(t, "I want to book a trip to Paris")
	if p.countRows(t, "task_contexts") != 1 {
		t.Fatal("expected 1 task context after save")
	}

	// Turn 2: load context.
	p.sendMsg(t, "Where was I with my booking?")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "Paris") {
		t.Errorf("load response = %q, want mention of Paris", resp)
	}
	if !strings.Contains(resp, "gathering") {
		t.Errorf("load response = %q, want mention of gathering step", resp)
	}

	// 4 model calls total (2 per turn).
	if p.Fake.CallCount() != 4 {
		t.Errorf("model calls = %d, want 4", p.Fake.CallCount())
	}
}
