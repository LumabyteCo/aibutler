package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"nhooyr.io/websocket"
)

// Gateway opcodes.
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opPresenceUpdate = 3
	opHello          = 10
	opHeartbeatAck   = 11
)

// Intents.
const (
	intentGuildMessages  = 1 << 9
	intentMessageContent = 1 << 15
	intentDMMessages     = 1 << 12
)

// Adapter implements channel.Channel for Discord via Gateway API.
type Adapter struct {
	api             APIClient
	token           string
	handler         channel.MessageHandler
	conn            *websocket.Conn
	cancel          context.CancelFunc
	heartbeatCancel context.CancelFunc // Cancels current heartbeat loop
	wg              sync.WaitGroup
	mu              sync.RWMutex
	sessionID       string
	seq             *int64 // Last sequence number
	botUserID       string
}

// New creates a new Discord adapter.
func New(api APIClient) *Adapter {
	return &Adapter{
		api: api,
	}
}

// NewWithToken creates a new Discord adapter with a bot token for Identify.
func NewWithToken(api APIClient, botToken string) *Adapter {
	return &Adapter{
		api:   api,
		token: botToken,
	}
}

func (a *Adapter) Name() string { return "discord" }

// Start connects to the Gateway and begins processing events.
func (a *Adapter) Start(ctx context.Context, handler channel.MessageHandler) error {
	a.handler = handler
	ctx, a.cancel = context.WithCancel(ctx)

	if err := a.connect(ctx); err != nil {
		return err
	}

	// Start read loop.
	a.wg.Add(1)
	go a.readLoop(ctx)

	return nil
}

// connect performs the full Gateway handshake: dial, hello, identify, heartbeat.
func (a *Adapter) connect(ctx context.Context) error {
	gwURL, err := a.api.GetGatewayURL(ctx)
	if err != nil {
		return fmt.Errorf("discord: get gateway: %w", err)
	}

	conn, _, err := websocket.Dial(ctx, gwURL+"?v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("discord: ws connect: %w", err)
	}
	conn.SetReadLimit(1 << 20)

	// Read Hello to get heartbeat interval.
	_, data, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("discord: read hello: %w", err)
	}

	var hello gatewayEvent
	if err := json.Unmarshal(data, &hello); err != nil {
		return fmt.Errorf("discord: parse hello: %w", err)
	}
	if hello.Op != opHello {
		return fmt.Errorf("discord: expected hello (op 10), got op %d", hello.Op)
	}

	var helloData struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	json.Unmarshal(hello.D, &helloData)

	// Send Identify.
	identify := gatewayEvent{
		Op: opIdentify,
	}
	identifyData := map[string]interface{}{
		"token":   a.token,
		"intents": intentGuildMessages | intentMessageContent | intentDMMessages,
		"properties": map[string]string{
			"os":      "linux",
			"browser": "aibutler",
			"device":  "aibutler",
		},
	}
	identify.D, _ = json.Marshal(identifyData)

	identifyBytes, _ := json.Marshal(identify)
	if err := conn.Write(ctx, websocket.MessageText, identifyBytes); err != nil {
		return fmt.Errorf("discord: send identify: %w", err)
	}

	// Start heartbeat loop.
	interval := time.Duration(helloData.HeartbeatInterval) * time.Millisecond
	if interval == 0 {
		interval = 41250 * time.Millisecond // Default fallback.
	}

	// Cancel old heartbeat loop before starting new one.
	a.mu.Lock()
	if a.heartbeatCancel != nil {
		a.heartbeatCancel()
	}
	a.conn = conn
	hbCtx, hbCancel := context.WithCancel(ctx)
	a.heartbeatCancel = hbCancel
	a.mu.Unlock()

	a.wg.Add(1)
	go a.heartbeatLoop(hbCtx, interval)

	return nil
}

// reconnect attempts to re-establish the Gateway connection with exponential backoff.
func (a *Adapter) reconnect(ctx context.Context) bool {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}

		log.Printf("discord: attempting reconnect...")
		if err := a.connect(ctx); err != nil {
			log.Printf("discord: reconnect failed: %v", err)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		log.Printf("discord: reconnected successfully")
		return true
	}
}

// Stop gracefully closes the Gateway connection.
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

// Send sends or edits a message in a Discord channel.
func (a *Adapter) Send(ctx context.Context, accountID string, msg channel.OutgoingMessage) error {
	// accountID is the channel ID for Discord.
	if msg.EditID != "" {
		return a.api.EditMessage(ctx, accountID, msg.EditID, msg.Text)
	}
	_, err := a.api.SendMessage(ctx, accountID, msg.Text)
	return err
}

// SendTyping sends a typing indicator to a channel.
func (a *Adapter) SendTyping(ctx context.Context, accountID string) error {
	return a.api.TriggerTyping(ctx, accountID)
}

func (a *Adapter) heartbeatLoop(ctx context.Context, interval time.Duration) {
	defer a.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hb := gatewayEvent{Op: opHeartbeat}
			a.mu.RLock()
			if a.seq != nil {
				hb.D, _ = json.Marshal(*a.seq)
			} else {
				hb.D = json.RawMessage("null")
			}
			conn := a.conn
			a.mu.RUnlock()

			data, _ := json.Marshal(hb)
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				return
			}
		}
	}
}

func (a *Adapter) readLoop(ctx context.Context) {
	defer a.wg.Done()
	for {
		a.mu.RLock()
		conn := a.conn
		a.mu.RUnlock()
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("discord: read error: %v, attempting reconnect", err)
			if !a.reconnect(ctx) {
				return
			}
			continue
		}

		var event gatewayEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}

		// Track sequence.
		if event.S != nil {
			a.mu.Lock()
			seq := *event.S
			a.seq = &seq
			a.mu.Unlock()
		}

		if event.Op == opHeartbeatAck {
			continue
		}

		if event.Op != opDispatch || event.T != "MESSAGE_CREATE" {
			continue
		}

		var msg discordMessage
		if err := json.Unmarshal(event.D, &msg); err != nil {
			continue
		}

		// Skip bot messages.
		if msg.Author.Bot {
			continue
		}

		env := channel.Envelope{
			ID:        msg.ID,
			Channel:   "discord",
			AccountID: msg.ChannelID,
			ThreadID:  msg.ChannelID,
			Type:      channel.TypeText,
			Text:      msg.Content,
		}

		go func() {
			if err := a.handler(ctx, env); err != nil {
				log.Printf("discord: handler error: %v", err)
			}
		}()
	}
}

// gatewayEvent is a Discord Gateway WebSocket event.
type gatewayEvent struct {
	Op int              `json:"op"`
	D  json.RawMessage  `json:"d,omitempty"`
	S  *int64           `json:"s,omitempty"`
	T  string           `json:"t,omitempty"`
}

// discordMessage is a Discord MESSAGE_CREATE payload.
type discordMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
	Author    struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
}

// VerifyDiscordSignature validates the Ed25519 signature on Discord webhook requests.
// If publicKey is empty, verification is skipped (backward compatible).
func VerifyDiscordSignature(body []byte, signature, timestamp, publicKey string) bool {
	if publicKey == "" {
		return true // no verification configured
	}
	pubKeyBytes, err := hex.DecodeString(publicKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return false
	}
	sigBytes, err := hex.DecodeString(signature)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return false
	}
	message := []byte(timestamp + string(body))
	return ed25519.Verify(pubKeyBytes, message, sigBytes)
}

// Verify interface compliance.
var _ channel.Channel = (*Adapter)(nil)
