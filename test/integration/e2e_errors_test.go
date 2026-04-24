//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/config"
)

// TestE2EModelError verifies that a model error is handled gracefully.
// The router should not crash and should still send a response.
func TestE2EModelError(t *testing.T) {
	p := setupE2E(t) // No canned responses.
	p.Fake.SetError(errors.New("API timeout"))

	p.sendMsg(t, "Hello")

	// The agent's failWith returns result with empty Output.
	// Router sends a response (possibly empty or fallback).
	if p.responseCount() < 1 {
		t.Error("expected at least 1 response sent despite model error")
	}
}

// TestE2EUnknownToolCall verifies that an unknown tool call is handled gracefully.
// The dispatcher returns an error string as the tool result, and the model returns a final response.
func TestE2EUnknownToolCall(t *testing.T) {
	p := setupE2E(t,
		// Model requests a non-existent tool.
		toolCallResponse("Let me try this tool.",
			tc("unk1", "nonexistent.tool", `{}`),
		),
		// After receiving the error result, model responds gracefully.
		finalResponse("Sorry, that tool is not available."),
	)

	p.sendMsg(t, "Do something special")

	// Verify 2 model calls.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// The second call should include the tool error result.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "unknown tool") {
			found = true
			break
		}
	}
	if !found {
		t.Error("second model call should contain tool result with 'unknown tool' error")
	}

	// Verify the final response was sent.
	resp := p.lastResponse(t)
	if resp != "Sorry, that tool is not available." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EEmptyModelResponse verifies that an empty model response triggers the fallback message.
func TestE2EEmptyModelResponse(t *testing.T) {
	p := setupE2E(t,
		agent.Response{Content: "", TokensIn: 10, TokensOut: 5},
	)

	p.sendMsg(t, "Hello")

	resp := p.lastResponse(t)
	if resp != "I processed your request but have no response to share." {
		t.Errorf("response = %q, want fallback message", resp)
	}
}

// TestE2EMaxToolCallsLimit verifies that the agent stops after reaching the configured tool call limit.
func TestE2EMaxToolCallsLimit(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		ConfigOverride: func(cfg *config.Config) {
			cfg.Options.Agents.MaxToolCalls = 2
		},
		Responses: []agent.Response{
			// Turn 1, iteration 1: tool call
			toolCallResponse("Adding task 1.",
				tc("lim1", "task.add", `{"content":"Task 1"}`),
			),
			// Turn 1, iteration 2: another tool call (this is the 2nd tool call)
			toolCallResponse("Adding task 2.",
				tc("lim2", "task.add", `{"content":"Task 2"}`),
			),
			// After 2 tool calls, agent should stop with "complexity limit reached".
			// This response should NOT be consumed because the agent exits the loop.
			finalResponse("This should not be reached."),
		},
	})

	p.sendMsg(t, "Add many tasks")

	// The agent should have stopped after 2 tool calls with "complexity limit reached".
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "complexity limit reached") {
		t.Errorf("response = %q, want 'complexity limit reached'", resp)
	}

	// Verify exactly 2 tasks were added.
	count := p.countRows(t, "user_tasks")
	if count != 2 {
		t.Errorf("user_tasks = %d, want 2", count)
	}
}

// TestE2EToolInputValidationError verifies that tool input validation errors are fed back to the model.
func TestE2EToolInputValidationError(t *testing.T) {
	p := setupE2E(t,
		// Model sends empty content (validation error).
		toolCallResponse("Adding empty task.",
			tc("val1", "task.add", `{"content":""}`),
		),
		// After receiving error feedback, model responds gracefully.
		finalResponse("I need a task description to add a task."),
	)

	p.sendMsg(t, "Add a task")

	// Verify 2 model calls.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// The second call should include the validation error as a tool result.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "content is required") {
			found = true
			break
		}
	}
	if !found {
		t.Error("second model call should contain tool result with 'content is required' error")
	}

	// Verify no tasks were added.
	count := p.countRows(t, "user_tasks")
	if count != 0 {
		t.Errorf("user_tasks = %d, want 0", count)
	}
}

// TestE2EToolResultFeedback verifies that tool results are fed back to the model in subsequent calls.
func TestE2EToolResultFeedback(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Adding task.",
			tc("fb1", "task.add", `{"content":"Feedback test"}`),
		),
		finalResponse("Task added successfully."),
	)

	p.sendMsg(t, "Add task: Feedback test")

	// Verify 2 model calls.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// The second model call should contain a "tool" role message with the task.add result.
	calls := p.Fake.Calls()
	foundToolResult := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" {
			foundToolResult = true
			// The tool result should contain "Task added" from task.add.
			if !strings.Contains(msg.Content, "Task added") {
				t.Errorf("tool result = %q, expected 'Task added'", msg.Content)
			}
			break
		}
	}
	if !foundToolResult {
		t.Error("second model call should contain a 'tool' role message")
	}
}

// TestE2EEmptyUserMessage verifies that an empty user message is handled without crashing.
func TestE2EEmptyUserMessage(t *testing.T) {
	p := setupE2E(t,
		finalResponse("I received an empty message."),
	)

	env := channel.Envelope{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Channel:   "webchat",
		AccountID: "user-e2e",
		Type:      channel.TypeText,
		Text:      "",
		Timestamp: time.Now(),
	}

	err := p.sendEnvelope(t, env)
	if err != nil {
		t.Fatalf("sendEnvelope: %v", err)
	}

	// Verify a response was sent (no crash).
	if p.responseCount() < 1 {
		t.Error("expected at least 1 response for empty message")
	}
}

// TestE2EStopPhraseCancel verifies that "cancel" triggers the stop phrase handler
// and the model is NOT called.
func TestE2EStopPhraseCancel(t *testing.T) {
	p := setupE2E(t) // No model responses needed.

	p.sendMsg(t, "cancel")

	// Model should NOT have been called.
	if p.Fake.CallCount() != 0 {
		t.Errorf("model calls = %d, want 0 (cancel is a stop phrase)", p.Fake.CallCount())
	}

	// A response should have been sent (the i18n cancel message).
	if p.responseCount() != 1 {
		t.Fatalf("responses = %d, want 1", p.responseCount())
	}

	resp := p.lastResponse(t)
	if resp == "" {
		t.Error("stop phrase response should not be empty")
	}
}

// TestE2ELongConversation verifies that 5 sequential turns all produce responses
// and the model is called once per turn.
func TestE2ELongConversation(t *testing.T) {
	p := setupE2E(t,
		finalResponse("Reply 1."),
		finalResponse("Reply 2."),
		finalResponse("Reply 3."),
		finalResponse("Reply 4."),
		finalResponse("Reply 5."),
	)

	for i := 1; i <= 5; i++ {
		p.sendMsg(t, fmt.Sprintf("Message %d", i))
	}

	// Verify 5 responses sent.
	if p.responseCount() != 5 {
		t.Errorf("responses = %d, want 5", p.responseCount())
	}

	// Verify model called 5 times.
	if p.Fake.CallCount() != 5 {
		t.Errorf("model calls = %d, want 5", p.Fake.CallCount())
	}

	// Verify the last response.
	resp := p.lastResponse(t)
	if resp != "Reply 5." {
		t.Errorf("last response = %q", resp)
	}
}

// TestE2EConcurrentMessages verifies that 2 different users can send messages
// and both receive responses. Uses separate channels to avoid session ID collisions.
func TestE2EConcurrentMessages(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Hello user A."),
			finalResponse("Hello user B."),
		},
	})

	telCh := p.addFakeChannel("telegram")

	// Send sequentially from different channels to avoid session ID collision.
	p.sendMsgAs(t, "webchat", "user-A", "Hello from A")
	p.sendMsgAs(t, "telegram", "user-B", "Hello from B")

	// Both should have gotten responses.
	webchatCount := sentCount(p.Channel)
	telegramCount := sentCount(telCh)

	if webchatCount != 1 {
		t.Errorf("webchat responses = %d, want 1", webchatCount)
	}
	if telegramCount != 1 {
		t.Errorf("telegram responses = %d, want 1", telegramCount)
	}

	// Verify model was called twice.
	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}
}

// TestE2EUnicodeContent verifies that emoji and CJK characters round-trip correctly through tools.
func TestE2EUnicodeContent(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Adding unicode task.",
			tc("uni1", "task.add", `{"content":"买菜 🛒"}`),
		),
		finalResponse("Task with unicode added."),
	)

	p.sendMsg(t, "Add task with emoji and CJK")

	// Verify the task was added with correct unicode content.
	count := p.countRows(t, "user_tasks")
	if count != 1 {
		t.Fatalf("user_tasks = %d, want 1", count)
	}

	content := p.querySingleString(t, "SELECT content FROM user_tasks WHERE id = 1")
	if content != "买菜 🛒" {
		t.Errorf("task content = %q, want %q", content, "买菜 🛒")
	}
}

// TestE2ELargeToolResult verifies that a large tool result (50 tasks) is processed correctly.
func TestE2ELargeToolResult(t *testing.T) {
	p := setupE2E(t,
		// Model calls task.list which returns 50 tasks.
		toolCallResponse("Listing all tasks.",
			tc("big1", "task.list", `{}`),
		),
		finalResponse("You have 50 tasks."),
	)

	// Pre-seed 50 tasks directly in the DB.
	ctx := context.Background()
	for i := 1; i <= 50; i++ {
		_, err := p.DB.ExecContext(ctx,
			`INSERT INTO user_tasks (list_name, content, status, priority) VALUES ('default', ?, 'pending', 0)`,
			fmt.Sprintf("Task %d", i))
		if err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}

	// Verify 50 tasks seeded.
	count := p.countRows(t, "user_tasks")
	if count != 50 {
		t.Fatalf("seeded tasks = %d, want 50", count)
	}

	p.sendMsg(t, "List all my tasks")

	// Verify model was called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// The second call should contain a tool result with many tasks.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" {
			// The result should contain "Task 1" and "Task 50".
			if strings.Contains(msg.Content, "Task 1") && strings.Contains(msg.Content, "Task 50") {
				found = true
			}
			// Verify the result is reasonably large (50 tasks as JSON).
			if len(msg.Content) < 500 {
				t.Errorf("tool result too short (%d bytes) for 50 tasks", len(msg.Content))
			}
			break
		}
	}
	if !found {
		t.Error("task.list tool result should contain 'Task 1' and 'Task 50'")
	}

	// Verify the final response was sent.
	resp := p.lastResponse(t)
	if resp != "You have 50 tasks." {
		t.Errorf("response = %q", resp)
	}
}
