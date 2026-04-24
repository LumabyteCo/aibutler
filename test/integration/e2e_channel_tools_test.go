//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2EChannelSend verifies the channel.send tool dispatches a message to a named channel.
func TestE2EChannelSend(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithChannelTool: true,
		Responses: []agent.Response{
			toolCallResponse("Sending to slack.",
				tc("cs1", "channel.send", `{"channel":"slack","account_id":"U1","text":"hello"}`),
			),
			finalResponse("Message sent to slack."),
		},
	})

	// Register a second fake channel named "slack".
	slackCh := p.addFakeChannel("slack")

	p.sendMsg(t, "Send hello to U1 on slack")

	// Verify the slack channel received the message.
	sent := getSent(slackCh)
	if len(sent) != 1 {
		t.Fatalf("slack sent = %d, want 1", len(sent))
	}
	if sent[0].Text != "hello" {
		t.Errorf("slack message = %q, want %q", sent[0].Text, "hello")
	}

	// Verify the default webchat channel got the final response.
	resp := p.lastResponse(t)
	if resp != "Message sent to slack." {
		t.Errorf("webchat response = %q", resp)
	}
}

// TestE2EChannelRead verifies the channel.read tool returns session messages from the DB.
func TestE2EChannelRead(t *testing.T) {
	// Use a known session ID so we can reference it in the canned tool call.
	const seedSessID = "sess-channel-read-test"

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithChannelTool: true,
		Responses: []agent.Response{
			toolCallResponse("Reading session messages.",
				tc("cr1", "channel.read", `{"session_id":"`+seedSessID+`","limit":10}`),
			),
			finalResponse("Here are the messages from that session."),
		},
	})

	// Seed the session and messages directly in the DB.
	ctx := context.Background()
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope, created_at, updated_at) VALUES (?, 'webchat', 'user-e2e', 'default', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`,
		seedSessID)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := p.SM.AddMessage(ctx, seedSessID, agent.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("seed message 1: %v", err)
	}
	if err := p.SM.AddMessage(ctx, seedSessID, agent.Message{Role: "assistant", Content: "hi there"}); err != nil {
		t.Fatalf("seed message 2: %v", err)
	}

	p.sendMsg(t, "Read the session messages")

	// The channel.read tool should have been called and returned the seeded messages.
	// Verify the model was called twice (tool call + final).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify the second model call includes a tool result message containing "hello".
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "hello") {
			found = true
			break
		}
	}
	if !found {
		t.Error("channel.read tool result should contain seeded message 'hello'")
	}

	// Verify final response was sent.
	resp := p.lastResponse(t)
	if resp != "Here are the messages from that session." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EChannelRelay verifies the channel.relay tool resolves a contact and sends via their preferred channel.
func TestE2EChannelRelay(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithChannelTool: true,
		Responses: []agent.Response{
			toolCallResponse("Relaying to Bob.",
				tc("rl1", "channel.relay", `{"contact":"Bob","text":"hello"}`),
			),
			finalResponse("Message relayed to Bob via slack."),
		},
	})

	// Register a "slack" channel.
	slackCh := p.addFakeChannel("slack")

	// Seed a contact in the DB with preferred_channel = "slack" and channel_ids mapping.
	_, err := p.DB.ExecContext(context.Background(),
		`INSERT INTO user_contacts (name, preferred_channel, channel_ids) VALUES ('Bob', 'slack', '{"slack":"U123"}')`)
	if err != nil {
		t.Fatalf("seed contact: %v", err)
	}

	p.sendMsg(t, "Relay hello to Bob")

	// Verify the slack channel received the message to account "U123".
	sent := getSent(slackCh)
	if len(sent) != 1 {
		t.Fatalf("slack sent = %d, want 1", len(sent))
	}
	if sent[0].Text != "hello" {
		t.Errorf("slack message = %q, want %q", sent[0].Text, "hello")
	}

	// Verify the webchat got the final response.
	resp := p.lastResponse(t)
	if resp != "Message relayed to Bob via slack." {
		t.Errorf("response = %q", resp)
	}

	// Verify model was called exactly twice.
	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}
}
