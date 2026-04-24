// Package nostr provides a lightweight Nostr channel adapter for encrypted DMs.
package nostr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// toolRegistry is the interface for registering tools. Using a local narrow interface
// avoids import cycles with the tool package.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Client is a lightweight Nostr client for sending encrypted DMs via relay.
type Client struct {
	relayURL   string
	privateKey string // hex-encoded secp256k1 private key
	sendFn     func(ctx context.Context, pubkey, text string) error
}

// NewClient creates a Nostr client configured for the given relay and private key.
func NewClient(relayURL, privateKey string) *Client {
	return &Client{
		relayURL:   relayURL,
		privateKey: privateKey,
	}
}

// SetSendFunc sets the underlying send function (for testing or custom transport).
func (c *Client) SetSendFunc(fn func(ctx context.Context, pubkey, text string) error) {
	c.sendFn = fn
}

// Send sends a NIP-04 encrypted DM to the given public key.
func (c *Client) Send(ctx context.Context, pubkey, text string) error {
	if pubkey == "" {
		return fmt.Errorf("nostr: pubkey is required")
	}
	if text == "" {
		return fmt.Errorf("nostr: text is required")
	}
	if c.sendFn != nil {
		return c.sendFn(ctx, pubkey, text)
	}
	// No real WebSocket relay — this is a capability framework.
	return fmt.Errorf("nostr: real relay connection not available (no external dependencies); use SetSendFunc for testing")
}

// Subscribe returns a channel of events mentioning the given pubkey.
// This is a placeholder — real implementation requires WebSocket connection.
func (c *Client) Subscribe(_ context.Context, _ string) (<-chan Event, error) {
	return nil, fmt.Errorf("nostr: real relay subscription not available (no external dependencies)")
}

// Event represents a Nostr event (NIP-01).
type Event struct {
	ID        string    `json:"id"`
	PubKey    string    `json:"pubkey"`
	Content   string    `json:"content"`
	Kind      int       `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

// ParseEvent parses a raw JSON Nostr event.
func (c *Client) ParseEvent(data []byte) (*Event, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("nostr: empty event data")
	}

	var raw struct {
		ID        string `json:"id"`
		PubKey    string `json:"pubkey"`
		Content   string `json:"content"`
		Kind      int    `json:"kind"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("nostr: unmarshal event: %w", err)
	}

	return &Event{
		ID:        raw.ID,
		PubKey:    raw.PubKey,
		Content:   raw.Content,
		Kind:      raw.Kind,
		CreatedAt: time.Unix(raw.CreatedAt, 0),
	}, nil
}

// RegisterNostrTools registers the nostr.send tool.
func RegisterNostrTools(registry toolRegistry, client *Client) {
	registry.Register(
		"nostr.send",
		"Send a NIP-04 encrypted DM via Nostr relay.",
		`{"type":"object","properties":{"pubkey":{"type":"string","description":"Recipient hex public key"},"text":{"type":"string","description":"Message body"}},"required":["pubkey","text"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				PubKey string `json:"pubkey"`
				Text   string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("nostr.send: invalid input: %w", err)
			}
			if args.PubKey == "" || args.Text == "" {
				return "", fmt.Errorf("nostr.send: pubkey and text are required")
			}
			if err := client.Send(ctx, args.PubKey, args.Text); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
