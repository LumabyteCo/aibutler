package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"nhooyr.io/websocket"
)

// Adapter implements channel.Channel for Slack via Socket Mode.
type Adapter struct {
	api     APIClient
	handler channel.MessageHandler
	conn    *websocket.Conn
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	// Track channel IDs for sending responses back.
	sessions map[string]string // accountID -> channelID
}

// New creates a new Slack adapter.
func New(api APIClient) *Adapter {
	return &Adapter{
		api:      api,
		sessions: make(map[string]string),
	}
}

func (a *Adapter) Name() string { return "slack" }

func (a *Adapter) Start(ctx context.Context, handler channel.MessageHandler) error {
	a.handler = handler

	wsURL, err := a.api.GetWSURL(ctx)
	if err != nil {
		return fmt.Errorf("slack: get ws url: %w", err)
	}

	ctx, a.cancel = context.WithCancel(ctx)

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("slack: ws connect: %w", err)
	}
	a.conn = conn
	conn.SetReadLimit(1 << 20) // 1MB

	a.wg.Add(1)
	go a.readLoop(ctx)

	return nil
}

func (a *Adapter) Stop(_ context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.conn != nil {
		a.conn.Close(websocket.StatusNormalClosure, "shutdown")
	}
	a.wg.Wait()
	return nil
}

func (a *Adapter) Send(ctx context.Context, accountID string, msg channel.OutgoingMessage) error {
	a.mu.RLock()
	channelID, ok := a.sessions[accountID]
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("slack: no channel for account %q", accountID)
	}

	if msg.EditID != "" {
		return a.api.UpdateMessage(ctx, channelID, msg.EditID, msg.Text)
	}

	_, err := a.api.PostMessage(ctx, channelID, msg.Text, msg.ReplyTo)
	return err
}

func (a *Adapter) SendTyping(_ context.Context, _ string) error {
	// Slack doesn't have a public typing indicator API for bots.
	// This is a no-op. Users see "Bot is typing..." only via Socket Mode events.
	return nil
}

// reconnect attempts to re-establish the WebSocket connection with exponential backoff.
func (a *Adapter) reconnect(ctx context.Context) bool {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}

		log.Printf("slack: attempting reconnect...")
		wsURL, err := a.api.GetWSURL(ctx)
		if err != nil {
			log.Printf("slack: reconnect get ws url: %v", err)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			log.Printf("slack: reconnect dial: %v", err)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		a.mu.Lock()
		a.conn = conn
		a.mu.Unlock()
		conn.SetReadLimit(1 << 20)
		log.Printf("slack: reconnected successfully")
		return true
	}
}

// readLoop reads Socket Mode events from the WebSocket.
func (a *Adapter) readLoop(ctx context.Context) {
	defer a.wg.Done()
	for {
		_, data, err := a.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("slack: read error: %v, attempting reconnect", err)
			if !a.reconnect(ctx) {
				return
			}
			continue
		}

		var envelope socketModeEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		// Acknowledge the envelope.
		if envelope.EnvelopeID != "" {
			ack, _ := json.Marshal(map[string]string{"envelope_id": envelope.EnvelopeID})
			_ = a.conn.Write(ctx, websocket.MessageText, ack)
		}

		if envelope.Type != "events_api" {
			continue
		}

		// Parse the inner event.
		var eventPayload struct {
			Event struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				User     string `json:"user"`
				Channel  string `json:"channel"`
				ThreadTS string `json:"thread_ts"`
				TS       string `json:"ts"`
				BotID    string `json:"bot_id"`
			} `json:"event"`
		}
		if err := json.Unmarshal(envelope.Payload, &eventPayload); err != nil {
			continue
		}

		evt := eventPayload.Event
		// Skip bot messages (prevent loops).
		if evt.BotID != "" || evt.Type != "message" {
			continue
		}

		// Track channel for responses.
		a.mu.Lock()
		a.sessions[evt.User] = evt.Channel
		a.mu.Unlock()

		threadID := evt.ThreadTS
		if threadID == "" {
			threadID = evt.TS
		}

		env := channel.Envelope{
			ID:        evt.TS,
			Channel:   "slack",
			AccountID: evt.User,
			ThreadID:  threadID,
			Type:      channel.TypeText,
			Text:      evt.Text,
		}

		go func() {
			if err := a.handler(ctx, env); err != nil {
				log.Printf("slack: handler error: %v", err)
			}
		}()
	}
}

// socketModeEnvelope represents a Socket Mode WebSocket frame.
type socketModeEnvelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

// VerifySlackSignature validates the X-Slack-Signature header using HMAC-SHA256.
// If signingSecret is empty, verification is skipped (backward compatible).
func VerifySlackSignature(body []byte, timestamp, signature, signingSecret string) bool {
	if signingSecret == "" {
		return true // no verification configured
	}
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// Verify interface compliance.
var _ channel.Channel = (*Adapter)(nil)
