// Package gchat provides a Google Chat channel adapter using the Google Chat API.
package gchat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// toolRegistry is the interface for registering tools. Using a local narrow interface
// avoids import cycles with the tool package.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Client sends and receives Google Chat messages via the Chat API.
type Client struct {
	serviceAccountKey []byte
	httpClient        *http.Client
	baseURL           string
}

// NewClient creates a Google Chat client for the given service account key JSON.
func NewClient(serviceAccountKeyJSON []byte) *Client {
	return &Client{
		serviceAccountKey: serviceAccountKeyJSON,
		httpClient:        &http.Client{Timeout: 15 * time.Second},
		baseURL:           "https://chat.googleapis.com/v1",
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient overrides the HTTP client (for testing).
func (c *Client) SetHTTPClient(h *http.Client) { c.httpClient = h }

// Send sends a plain-text message to a Google Chat space.
func (c *Client) Send(ctx context.Context, spaceName, text string) error {
	payload := map[string]interface{}{
		"text": text,
	}
	return c.post(ctx, fmt.Sprintf("/%s/messages", spaceName), payload)
}

// SendCard sends a Cards v2 message to a Google Chat space.
func (c *Client) SendCard(ctx context.Context, spaceName string, card map[string]interface{}) error {
	payload := map[string]interface{}{
		"cardsV2": []map[string]interface{}{
			{
				"cardId": "card_1",
				"card":   card,
			},
		},
	}
	return c.post(ctx, fmt.Sprintf("/%s/messages", spaceName), payload)
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("gchat: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("gchat: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// In production, use service account JWT for auth.
	// For now, the key is stored for future OAuth2 token exchange.
	req.Header.Set("Authorization", "Bearer service-account-token")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gchat: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gchat: API error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// InboundMessage represents a parsed incoming Google Chat event.
type InboundMessage struct {
	From        string
	Text        string
	SpaceName   string
	MessageName string
	Type        string
}

// webhookPayload mirrors the Google Chat event structure.
type webhookPayload struct {
	Type    string `json:"type"`
	Message struct {
		Name   string `json:"name"`
		Sender struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"sender"`
		Text string `json:"text"`
	} `json:"message"`
	Space struct {
		Name string `json:"name"`
	} `json:"space"`
}

// ParseWebhook parses an incoming Google Chat webhook request.
// Returns nil, nil when the payload is not a MESSAGE event.
func (c *Client) ParseWebhook(r *http.Request) (*InboundMessage, error) {
	if r == nil {
		return nil, fmt.Errorf("gchat: nil request")
	}
	// Limit request body to 1MB to prevent OOM from oversized payloads.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("gchat: read body: %w", err)
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("gchat: unmarshal webhook: %w", err)
	}

	if payload.Type != "MESSAGE" {
		return nil, nil
	}

	return &InboundMessage{
		From:        payload.Message.Sender.Name,
		Text:        payload.Message.Text,
		SpaceName:   payload.Space.Name,
		MessageName: payload.Message.Name,
		Type:        payload.Type,
	}, nil
}

// RegisterGChatTools registers gchat.send and gchat.send_card tools.
func RegisterGChatTools(registry toolRegistry, client *Client) {
	registry.Register(
		"gchat.send",
		"Send a plain-text message to a Google Chat space.",
		`{"type":"object","properties":{"space_name":{"type":"string","description":"Google Chat space name (e.g. spaces/AAAA)"},"text":{"type":"string","description":"Message body"}},"required":["space_name","text"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				SpaceName string `json:"space_name"`
				Text      string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("gchat.send: invalid input: %w", err)
			}
			if args.SpaceName == "" || args.Text == "" {
				return "", fmt.Errorf("gchat.send: space_name and text are required")
			}
			if err := client.Send(ctx, args.SpaceName, args.Text); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)

	registry.Register(
		"gchat.send_card",
		"Send a Cards v2 message to a Google Chat space.",
		`{"type":"object","properties":{"space_name":{"type":"string","description":"Google Chat space name (e.g. spaces/AAAA)"},"card":{"type":"object","description":"Cards v2 JSON payload"}},"required":["space_name","card"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				SpaceName string                 `json:"space_name"`
				Card      map[string]interface{} `json:"card"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("gchat.send_card: invalid input: %w", err)
			}
			if args.SpaceName == "" || args.Card == nil {
				return "", fmt.Errorf("gchat.send_card: space_name and card are required")
			}
			if err := client.SendCard(ctx, args.SpaceName, args.Card); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
