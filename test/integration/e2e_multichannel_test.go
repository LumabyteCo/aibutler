//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/channel"
)

// TestE2EMultiChannelIsolation registers "telegram" as an additional fakeChannel,
// sends messages from "webchat/user-1" and "telegram/user-1", and verifies that
// 2 separate sessions are created (channel + account forms the session key).
func TestE2EMultiChannelIsolation(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Reply from webchat."),
			finalResponse("Reply from telegram."),
		},
	})

	telegram := p.addFakeChannel("telegram")

	// Send from webchat.
	p.sendMsgAs(t, "webchat", "user-1", "Hello from webchat")

	// Send from telegram.
	p.sendMsgAs(t, "telegram", "user-1", "Hello from telegram")

	// Verify 2 sessions were created (different channels = different sessions).
	sessionCount := p.countRows(t, "sessions")
	if sessionCount != 2 {
		t.Fatalf("sessions = %d, want 2 (webchat + telegram)", sessionCount)
	}

	// Verify both channels received their respective responses.
	webchatCount := sentCount(p.Channel)
	telegramCount := sentCount(telegram)
	if webchatCount != 1 {
		t.Errorf("webchat sent = %d, want 1", webchatCount)
	}
	if telegramCount != 1 {
		t.Errorf("telegram sent = %d, want 1", telegramCount)
	}
}

// TestE2EChannelThreadIsolation sends messages from "webchat/user-1/thread-A"
// and "webchat/user-1/thread-B" using sendMsgWithThread. Verifies 2 separate
// sessions are created because different threads form different session keys.
func TestE2EChannelThreadIsolation(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Reply to thread A."),
			finalResponse("Reply to thread B."),
		},
	})

	p.sendMsgWithThread(t, "webchat", "user-1", "thread-A", "Message in thread A")
	p.sendMsgWithThread(t, "webchat", "user-1", "thread-B", "Message in thread B")

	// Verify 2 sessions were created (different threads = different session keys).
	sessionCount := p.countRows(t, "sessions")
	if sessionCount != 2 {
		t.Fatalf("sessions = %d, want 2 (thread-A + thread-B)", sessionCount)
	}

	// Verify both responses were sent.
	if p.responseCount() != 2 {
		t.Fatalf("responses = %d, want 2", p.responseCount())
	}
}

// TestE2EChannelSpecificRouting registers "telegram" as an additional fakeChannel,
// sends a message from "telegram/user-1", and verifies the telegram channel
// received the response while webchat did NOT.
func TestE2EChannelSpecificRouting(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Telegram reply."),
		},
	})

	telegram := p.addFakeChannel("telegram")

	p.sendMsgAs(t, "telegram", "user-1", "Hello from telegram")

	// Telegram should have received the response.
	telegramSent := getSent(telegram)
	if len(telegramSent) != 1 {
		t.Fatalf("telegram sent = %d, want 1", len(telegramSent))
	}
	if telegramSent[0].Text != "Telegram reply." {
		t.Errorf("telegram response = %q", telegramSent[0].Text)
	}

	// Webchat should NOT have received any response.
	webchatSent := getSent(p.Channel)
	if len(webchatSent) != 0 {
		t.Errorf("webchat sent = %d, want 0 (message was from telegram)", len(webchatSent))
	}
}

// TestE2EUnknownChannelError sends an envelope with channel="unknown" (not registered).
// HandleMessage should return an error since the channel cannot be found.
func TestE2EUnknownChannelError(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Should not arrive."),
		},
	})

	env := channel.Envelope{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Channel:   "unknown",
		AccountID: "user-1",
		Type:      channel.TypeText,
		Text:      "Hello from unknown channel",
		Timestamp: time.Now(),
	}

	err := p.sendEnvelope(t, env)
	if err == nil {
		t.Fatal("expected error for unknown channel, got nil")
	}

	// The model should not have been called.
	if p.Fake.CallCount() != 0 {
		t.Errorf("model calls = %d, want 0", p.Fake.CallCount())
	}
}

// TestE2EMultiUserSameChannel sends messages from "webchat/alice" and "webchat/bob".
// Verifies 2 responses are sent and 2 sessions are created (different accounts).
func TestE2EMultiUserSameChannel(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Hello Alice!"),
			finalResponse("Hello Bob!"),
		},
	})

	p.sendMsgAs(t, "webchat", "alice", "Hi, I'm Alice")
	p.sendMsgAs(t, "webchat", "bob", "Hi, I'm Bob")

	// Verify 2 sessions (alice + bob).
	sessionCount := p.countRows(t, "sessions")
	if sessionCount != 2 {
		t.Fatalf("sessions = %d, want 2", sessionCount)
	}

	// Verify 2 responses sent.
	if p.responseCount() != 2 {
		t.Fatalf("responses = %d, want 2", p.responseCount())
	}

	// Verify both responses reached the webchat channel.
	sent := getSent(p.Channel)
	if len(sent) != 2 {
		t.Fatalf("webchat sent = %d, want 2", len(sent))
	}
	if sent[0].Text != "Hello Alice!" {
		t.Errorf("first response = %q, want 'Hello Alice!'", sent[0].Text)
	}
	if sent[1].Text != "Hello Bob!" {
		t.Errorf("second response = %q, want 'Hello Bob!'", sent[1].Text)
	}
}
