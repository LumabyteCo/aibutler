//go:build integration

package integration

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2ESessionCreation sends 1 message and verifies the sessions table has 1 row.
func TestE2ESessionCreation(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Hello!"),
		},
	})

	p.sendMsg(t, "Hi there")

	sessionCount := p.countRows(t, "sessions")
	if sessionCount != 1 {
		t.Fatalf("sessions = %d, want 1", sessionCount)
	}
}

// TestE2ESessionReuse sends 2 messages from the same user and verifies
// the sessions table still has 1 row (the session was reused).
func TestE2ESessionReuse(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("First."),
			finalResponse("Second."),
		},
	})

	p.sendMsg(t, "Message one")
	p.sendMsg(t, "Message two")

	sessionCount := p.countRows(t, "sessions")
	if sessionCount != 1 {
		t.Fatalf("sessions = %d, want 1 (session should be reused)", sessionCount)
	}

	// Both responses should have arrived.
	if p.responseCount() != 2 {
		t.Fatalf("responses = %d, want 2", p.responseCount())
	}
}

// TestE2ESessionIsolation sends messages from "webchat/alice" and "webchat/bob"
// and verifies the sessions table has 2 rows (separate sessions for different users).
func TestE2ESessionIsolation(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Hi Alice."),
			finalResponse("Hi Bob."),
		},
	})

	p.sendMsgAs(t, "webchat", "alice", "I am Alice")
	p.sendMsgAs(t, "webchat", "bob", "I am Bob")

	sessionCount := p.countRows(t, "sessions")
	if sessionCount != 2 {
		t.Fatalf("sessions = %d, want 2 (alice + bob)", sessionCount)
	}
}

// TestE2ESessionMessagePersistence sends 1 message and verifies that the
// messages table has 2 rows: the user message and the assistant response.
func TestE2ESessionMessagePersistence(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Got your message."),
		},
	})

	p.sendMsg(t, "Please remember this")

	msgCount := p.countRows(t, "messages")
	if msgCount != 2 {
		t.Fatalf("messages = %d, want 2 (user + assistant)", msgCount)
	}

	// Verify the roles are correct: one "user" and one "assistant".
	userCount := p.querySingleInt(t, "SELECT COUNT(*) FROM messages WHERE role = 'user'")
	assistantCount := p.querySingleInt(t, "SELECT COUNT(*) FROM messages WHERE role = 'assistant'")

	if userCount != 1 {
		t.Errorf("user messages = %d, want 1", userCount)
	}
	if assistantCount != 1 {
		t.Errorf("assistant messages = %d, want 1", assistantCount)
	}
}

// TestE2ESessionHistoryInPrompt sends 2 messages and verifies the second model
// call received more messages than the first (history from the first turn is
// included via the Composer).
func TestE2ESessionHistoryInPrompt(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("First reply."),
			finalResponse("Second reply."),
		},
	})

	p.sendMsg(t, "First message")
	p.sendMsg(t, "Second message")

	calls := p.Fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(calls))
	}

	firstCallMsgCount := len(calls[0])
	secondCallMsgCount := len(calls[1])

	// The second call should have more messages because it includes history
	// from the first turn (user message + assistant response).
	if secondCallMsgCount <= firstCallMsgCount {
		t.Errorf("second call messages (%d) should be > first call messages (%d); history not included",
			secondCallMsgCount, firstCallMsgCount)
	}
}
