package telegram_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/telegram"
)

// mockAPI is a test double for the Telegram Bot API.
type mockAPI struct {
	mu       sync.Mutex
	updates  []telegram.Update
	sent     []sentMsg
	edited   []editedMsg
	actions  []chatAction
	offset   int
	pollOnce bool // If true, return updates once then empty
}

type sentMsg struct {
	chatID  int64
	text    string
	replyTo int
}

type editedMsg struct {
	chatID int64
	msgID  int
	text   string
}

type chatAction struct {
	chatID int64
	action string
}

func (m *mockAPI) GetUpdates(_ context.Context, offset, _ int) ([]telegram.Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pollOnce && m.offset > 0 {
		// Block briefly to simulate long poll, then return empty.
		return nil, nil
	}

	var result []telegram.Update
	for _, u := range m.updates {
		if u.UpdateID >= offset {
			result = append(result, u)
		}
	}
	m.offset = offset
	return result, nil
}

func (m *mockAPI) SendMessage(_ context.Context, chatID int64, text string, replyTo int) (*telegram.SentMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMsg{chatID: chatID, text: text, replyTo: replyTo})
	return &telegram.SentMessage{MessageID: 100 + len(m.sent), Chat: telegram.Chat{ID: chatID}}, nil
}

func (m *mockAPI) EditMessage(_ context.Context, chatID int64, msgID int, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.edited = append(m.edited, editedMsg{chatID: chatID, msgID: msgID, text: text})
	return nil
}

func (m *mockAPI) SendChatAction(_ context.Context, chatID int64, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, chatAction{chatID: chatID, action: action})
	return nil
}

func (m *mockAPI) GetFile(_ context.Context, _ string) ([]byte, error) {
	return []byte("fake-file-data"), nil
}

func TestTelegramName(t *testing.T) {
	a := telegram.New(&mockAPI{})
	if a.Name() != "telegram" {
		t.Errorf("name = %q, want telegram", a.Name())
	}
}

func TestNormalizeTextMessage(t *testing.T) {
	api := &mockAPI{
		updates: []telegram.Update{{
			UpdateID: 1,
			Message: &telegram.Message{
				MessageID: 10,
				Chat:      telegram.Chat{ID: 12345},
				Text:      "hello",
			},
		}},
		pollOnce: true,
	}

	a := telegram.New(api)
	received := make(chan channel.Envelope, 1)

	handler := func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	select {
	case env := <-received:
		if env.Type != channel.TypeText {
			t.Errorf("type = %q, want text", env.Type)
		}
		if env.Text != "hello" {
			t.Errorf("text = %q, want hello", env.Text)
		}
		if env.Channel != "telegram" {
			t.Errorf("channel = %q, want telegram", env.Channel)
		}
		if env.AccountID != "12345" {
			t.Errorf("accountID = %q, want 12345", env.AccountID)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestNormalizePhotoMessage(t *testing.T) {
	api := &mockAPI{
		updates: []telegram.Update{{
			UpdateID: 1,
			Message: &telegram.Message{
				MessageID: 20,
				Chat:      telegram.Chat{ID: 999},
				Photo: []telegram.PhotoSize{
					{FileID: "small", Width: 100, Height: 100},
					{FileID: "large", Width: 800, Height: 800, FileSize: 50000},
				},
			},
		}},
		pollOnce: true,
	}

	a := telegram.New(api)
	received := make(chan channel.Envelope, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a.Start(ctx, func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	})
	defer a.Stop(ctx)

	select {
	case env := <-received:
		if env.Type != channel.TypeImage {
			t.Errorf("type = %q, want image", env.Type)
		}
		if len(env.Attachments) != 1 {
			t.Fatalf("attachments = %d, want 1", len(env.Attachments))
		}
		if env.Attachments[0].URL != "large" {
			t.Errorf("fileID = %q, want large (largest photo)", env.Attachments[0].URL)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout")
	}
}

func TestNormalizeVoiceMessage(t *testing.T) {
	api := &mockAPI{
		updates: []telegram.Update{{
			UpdateID: 1,
			Message: &telegram.Message{
				MessageID: 30,
				Chat:      telegram.Chat{ID: 777},
				Voice:     &telegram.Voice{FileID: "voice-1", Duration: 5, MimeType: "audio/ogg"},
			},
		}},
		pollOnce: true,
	}

	a := telegram.New(api)
	received := make(chan channel.Envelope, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a.Start(ctx, func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	})
	defer a.Stop(ctx)

	select {
	case env := <-received:
		if env.Type != channel.TypeVoice {
			t.Errorf("type = %q, want voice", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout")
	}
}

func TestSendMessage(t *testing.T) {
	api := &mockAPI{}
	a := telegram.New(api)

	ctx := context.Background()
	err := a.Send(ctx, "12345", channel.OutgoingMessage{Text: "hi there"})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(api.sent))
	}
	if api.sent[0].text != "hi there" {
		t.Errorf("text = %q, want 'hi there'", api.sent[0].text)
	}
	if api.sent[0].chatID != 12345 {
		t.Errorf("chatID = %d, want 12345", api.sent[0].chatID)
	}
}

func TestSendTyping(t *testing.T) {
	api := &mockAPI{}
	a := telegram.New(api)

	ctx := context.Background()
	if err := a.SendTyping(ctx, "12345"); err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(api.actions))
	}
	if api.actions[0].action != "typing" {
		t.Errorf("action = %q, want typing", api.actions[0].action)
	}
}

func TestLongPollingOffset(t *testing.T) {
	api := &mockAPI{
		updates: []telegram.Update{
			{UpdateID: 100, Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 1}, Text: "a"}},
			{UpdateID: 101, Message: &telegram.Message{MessageID: 2, Chat: telegram.Chat{ID: 1}, Text: "b"}},
		},
		pollOnce: true,
	}

	a := telegram.New(api)
	count := 0
	mu := sync.Mutex{}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a.Start(ctx, func(_ context.Context, env channel.Envelope) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})
	defer a.Stop(ctx)

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 2 {
		t.Errorf("received %d messages, want 2", count)
	}
}

func TestSendEditMessage(t *testing.T) {
	api := &mockAPI{}
	a := telegram.New(api)

	ctx := context.Background()
	err := a.Send(ctx, "12345", channel.OutgoingMessage{
		Text:   "updated text",
		EditID: "42",
	})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.edited) != 1 {
		t.Fatalf("edited = %d, want 1", len(api.edited))
	}
	if api.edited[0].msgID != 42 {
		t.Errorf("msgID = %d, want 42", api.edited[0].msgID)
	}
	if api.edited[0].text != "updated text" {
		t.Errorf("text = %q, want 'updated text'", api.edited[0].text)
	}
}

func TestStreamWriter(t *testing.T) {
	api := &mockAPI{}
	ctx := context.Background()

	sw, err := telegram.NewStreamWriter(ctx, api, 12345, "thinking...")
	if err != nil {
		t.Fatal(err)
	}

	if sw.MessageID() == "" {
		t.Error("expected non-empty message ID")
	}

	if err := sw.Update(ctx, "done!"); err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.edited) != 1 {
		t.Fatalf("edited = %d, want 1", len(api.edited))
	}
	if api.edited[0].text != "done!" {
		t.Errorf("text = %q, want 'done!'", api.edited[0].text)
	}
}

func TestSendInvalidChatID(t *testing.T) {
	api := &mockAPI{}
	a := telegram.New(api)

	err := a.Send(context.Background(), "not-a-number", channel.OutgoingMessage{Text: "hi"})
	if err == nil {
		t.Error("expected error for invalid chat ID")
	}
}

func TestVerifyWebhookSecret(t *testing.T) {
	a := telegram.New(&mockAPI{})

	// Empty secret = always passes (backward compatible).
	req, _ := http.NewRequest(http.MethodPost, "/webhook", nil)
	if !a.VerifyWebhookSecret(req, "") {
		t.Error("empty secret should always pass")
	}

	// Correct secret.
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret")
	if !a.VerifyWebhookSecret(req, "my-secret") {
		t.Error("expected verification to pass with correct secret")
	}

	// Wrong secret.
	if a.VerifyWebhookSecret(req, "wrong-secret") {
		t.Error("expected verification to fail with wrong secret")
	}

	// Missing header.
	req2, _ := http.NewRequest(http.MethodPost, "/webhook", nil)
	if a.VerifyWebhookSecret(req2, "my-secret") {
		t.Error("expected verification to fail with missing header")
	}
}

// Ensure the fmt import is used.
var _ = fmt.Sprint
