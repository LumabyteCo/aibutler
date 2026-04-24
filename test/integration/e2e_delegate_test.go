//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// ============================================================================
// Agent Delegation Tools (4 tests)
// ============================================================================

func TestE2EDelegateSync(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithDelegate: true,
		Responses: []agent.Response{
			// Response 1 (parent): delegate to sub-agent.
			toolCallResponse("Delegating task.",
				tc("d1", "agent.delegate", `{"task":"What is 2+2?","timeout_seconds":10}`),
			),
			// Response 2 (sub-agent): sub-agent answers.
			finalResponse("4"),
			// Response 3 (parent): parent uses delegate result in final answer.
			finalResponse("The answer is 4."),
		},
	})

	p.sendMsg(t, "Delegate: what is 2+2?")

	// Verify model was called exactly 3 times: parent->sub-agent->parent.
	if got := p.Fake.CallCount(); got != 3 {
		t.Errorf("FakeModel call count = %d, want 3", got)
	}

	// The delegate tool result (returned to parent as tool output) should
	// contain the sub-agent's completion status and output.
	calls := p.Fake.Calls()
	if len(calls) < 3 {
		t.Fatalf("expected at least 3 model calls, got %d", len(calls))
	}
	// The third call is the parent's second turn. Its messages should include
	// the tool result containing the delegate output.
	thirdCallMsgs := calls[2]
	found := false
	for _, msg := range thirdCallMsgs {
		if strings.Contains(msg.Content, "completed") && strings.Contains(msg.Content, "4") {
			found = true
			break
		}
	}
	if !found {
		t.Error("parent's second turn messages should contain delegate result with \"completed\" and \"4\"")
	}

	resp := p.lastResponse(t)
	if resp != "The answer is 4." {
		t.Errorf("response = %q, want %q", resp, "The answer is 4.")
	}
}

func TestE2EDelegateMaxDepth(t *testing.T) {
	// The sub-agent (depth 1) tries to delegate again. With MaxDepth=3 this
	// succeeds (depth 1 < 3). The inner sub-agent (depth 2) returns a result.
	// This proves multi-level delegation works within the depth limit.
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithDelegate: true,
		Responses: []agent.Response{
			// Response 1 (parent, depth 0): delegates to sub-agent.
			toolCallResponse("Delegating outer.",
				tc("d1", "agent.delegate", `{"task":"Delegate further: compute 3+3","timeout_seconds":10}`),
			),
			// Response 2 (sub-agent, depth 1): tries to delegate again.
			toolCallResponse("Delegating inner.",
				tc("d2", "agent.delegate", `{"task":"What is 3+3?","timeout_seconds":10}`),
			),
			// Response 3 (inner sub-agent, depth 2): answers.
			finalResponse("6"),
			// Response 4 (sub-agent, depth 1): returns inner result.
			finalResponse("Inner agent said 6."),
			// Response 5 (parent, depth 0): uses final result.
			finalResponse("The nested delegation returned 6."),
		},
	})

	p.sendMsg(t, "Delegate: compute 3+3 through nesting")

	// 5 model calls: parent(1) -> sub(2) -> inner-sub(3) -> sub(4) -> parent(5).
	if got := p.Fake.CallCount(); got != 5 {
		t.Errorf("FakeModel call count = %d, want 5", got)
	}

	resp := p.lastResponse(t)
	if resp != "The nested delegation returned 6." {
		t.Errorf("response = %q, want %q", resp, "The nested delegation returned 6.")
	}
}

func TestE2ESpawnAsync(t *testing.T) {
	// The spawn tool starts a background goroutine AND returns immediately.
	// Both the parent and the background goroutine call FakeModel.Complete,
	// creating a race for response ordering. We provide identical responses
	// so the result is deterministic regardless of which goroutine runs first.
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithDelegate: true,
		Responses: []agent.Response{
			// Response 1 (parent): calls agent.spawn.
			toolCallResponse("Spawning background agent.",
				tc("s1", "agent.spawn", `{"task":"analyze data"}`),
			),
			// Responses 2 & 3: identical to handle the race between parent and
			// background goroutine. Either goroutine may consume either response.
			finalResponse("Background agent spawned."),
			finalResponse("Background agent spawned."),
		},
	})

	p.sendMsg(t, "Spawn a background agent to analyze data")

	// The spawn tool returns {"agent_id":"bg-...","status":"spawned","task":"..."}
	// immediately. This is added as a tool result to the parent's messages.
	// Verify the spawn result is present in the model calls.
	calls := p.Fake.Calls()
	foundSpawned := false
	foundAgentID := false
	for _, call := range calls {
		for _, msg := range call {
			if msg.Role == "tool" && strings.Contains(msg.Content, "spawned") {
				foundSpawned = true
			}
			if msg.Role == "tool" && strings.Contains(msg.Content, "bg-") {
				foundAgentID = true
			}
		}
	}
	if !foundSpawned {
		t.Error("spawn tool result should contain \"spawned\"")
	}
	if !foundAgentID {
		t.Error("spawn tool result should contain agent_id starting with \"bg-\"")
	}

	// Verify a response was sent to the channel.
	if p.responseCount() < 1 {
		t.Fatal("expected at least 1 response")
	}

	// Wait for the background goroutine to complete.
	time.Sleep(200 * time.Millisecond)
}

func TestE2EDelegateWithToolExecution(t *testing.T) {
	// The sub-agent calls the task.add tool (data tools are always registered).
	// Response ordering: parent -> sub-agent(tool call) -> sub-agent(final) -> parent(final).
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithDelegate: true,
		Responses: []agent.Response{
			// Response 1 (parent): delegates task creation.
			toolCallResponse("Delegating task creation.",
				tc("d1", "agent.delegate", `{"task":"Add a task: Buy groceries"}`),
			),
			// Response 2 (sub-agent): calls task.add tool.
			toolCallResponse("Adding task.",
				tc("sub1", "task.add", `{"content":"Buy groceries"}`),
			),
			// Response 3 (sub-agent): after tool result, returns final answer.
			finalResponse("Task added."),
			// Response 4 (parent): uses delegate result in final answer.
			finalResponse("Task has been created via delegation."),
		},
	})

	p.sendMsg(t, "Delegate: add a task for buying groceries")

	// 4 model calls: parent(1) -> sub-agent tool call(2) -> sub-agent final(3) -> parent final(4).
	if got := p.Fake.CallCount(); got != 4 {
		t.Errorf("FakeModel call count = %d, want 4", got)
	}

	// Verify the task was actually created in the database.
	count := p.countRows(t, "user_tasks")
	if count != 1 {
		t.Fatalf("user_tasks rows = %d, want 1", count)
	}

	content := p.querySingleString(t, "SELECT content FROM user_tasks WHERE id = 1")
	if content != "Buy groceries" {
		t.Errorf("task content = %q, want %q", content, "Buy groceries")
	}

	resp := p.lastResponse(t)
	if resp != "Task has been created via delegation." {
		t.Errorf("response = %q, want %q", resp, "Task has been created via delegation.")
	}
}
