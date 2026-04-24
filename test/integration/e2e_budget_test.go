//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
)

// TestE2ECostRecording sends 2 messages (2 simple responses) and verifies
// that the token_usage table has 2 rows recorded by the Tracker.
func TestE2ECostRecording(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("First reply."),
			finalResponse("Second reply."),
		},
	})

	p.sendMsg(t, "Hello")
	p.sendMsg(t, "Hello again")

	count := p.countRows(t, "token_usage")
	if count != 2 {
		t.Fatalf("token_usage rows = %d, want 2", count)
	}

	// Verify responses were delivered.
	if p.responseCount() != 2 {
		t.Fatalf("responses = %d, want 2", p.responseCount())
	}
}

// TestE2EBudgetPause configures a tiny budget ($0.001) with OnBudgetReached="pause",
// pre-seeds token_usage to push past the budget, then sends a message.
// The agent's BudgetChecker should trigger a pause and the router should send a
// budget-paused message to the user.
func TestE2EBudgetPause(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			// This response should never be delivered because the budget check
			// fires before or during agent execution and causes a pause.
			finalResponse("This should not arrive."),
		},
		ConfigOverride: func(cfg *config.Config) {
			cfg.Settings.Cost.MonthlyBudget = 0.001
			cfg.Configurations.Cost.OnBudgetReached = "pause"
		},
	})

	// Pre-seed token_usage with a row that already exceeds the budget.
	ctx := context.Background()
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO token_usage (timestamp, session_id, model, provider, input_tokens, output_tokens, cost_usd)
		 VALUES (datetime('now'), 'sess-1', 'test', 'test', 1000, 1000, 1.00)`)
	if err != nil {
		t.Fatalf("pre-seed token_usage: %v", err)
	}

	p.sendMsg(t, "What is 2+2?")

	resp := p.lastResponse(t)
	lower := strings.ToLower(resp)
	if !strings.Contains(lower, "budget") && !strings.Contains(lower, "paused") {
		t.Errorf("expected budget/paused in response, got %q", resp)
	}
}

// TestE2EBudgetWarnInResponse sets budget=$10, pre-seeds $8 of usage,
// then sends a message. The response should have a budget alert appended
// because we've crossed the 75% threshold.
func TestE2EBudgetWarnInResponse(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Here is your answer."),
		},
		ConfigOverride: func(cfg *config.Config) {
			cfg.Settings.Cost.MonthlyBudget = 10.0
		},
	})

	// Pre-seed $8 of usage (80% of $10 budget).
	ctx := context.Background()
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO token_usage (timestamp, session_id, model, provider, input_tokens, output_tokens, cost_usd)
		 VALUES (datetime('now'), 'sess-pre', 'test', 'test', 5000, 5000, 8.0)`)
	if err != nil {
		t.Fatalf("pre-seed token_usage: %v", err)
	}

	p.sendMsg(t, "Tell me something.")

	resp := p.lastResponse(t)
	// The response should contain the original text AND a budget alert.
	if !strings.Contains(resp, "Here is your answer.") {
		t.Errorf("expected agent response in output, got %q", resp)
	}
	if !strings.Contains(resp, "$") && !strings.Contains(strings.ToLower(resp), "budget") {
		t.Errorf("expected budget alert in response, got %q", resp)
	}
}

// TestE2ECostStatusTool pre-seeds token_usage, then has the model call
// cost.status({}) to retrieve month and cost info. Verifies the tool result
// makes it through to the final model response.
func TestE2ECostStatusTool(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			// Turn 1: model calls cost.status
			toolCallResponse("Checking cost status.",
				tc("tc1", "cost.status", `{}`),
			),
			// Turn 2: model sees tool result, sends final reply
			finalResponse("Your current month's cost is $0.50."),
		},
	})

	// Pre-seed token_usage so cost.status returns meaningful data.
	ctx := context.Background()
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO token_usage (timestamp, session_id, model, provider, input_tokens, output_tokens, cost_usd)
		 VALUES (datetime('now'), 'sess-cost', 'test', 'test', 2000, 1000, 0.50)`)
	if err != nil {
		t.Fatalf("pre-seed token_usage: %v", err)
	}

	p.sendMsg(t, "How much have I spent this month?")

	// Verify model was called twice (tool call + final answer).
	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify response reached the channel.
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "$0.50") {
		t.Errorf("response = %q, expected mention of $0.50", resp)
	}
}

// TestE2EBudgetUnderLimit sets budget=$100 with no pre-seeded usage.
// After sending a message, the response should NOT contain any budget alert.
func TestE2EBudgetUnderLimit(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Everything is fine."),
		},
		ConfigOverride: func(cfg *config.Config) {
			cfg.Settings.Cost.MonthlyBudget = 100.0
		},
	})

	p.sendMsg(t, "How are you?")

	resp := p.lastResponse(t)
	if resp != "Everything is fine." {
		t.Errorf("response = %q, want exact 'Everything is fine.' (no budget alert appended)", resp)
	}
}
