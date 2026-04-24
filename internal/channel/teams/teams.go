// Package teams provides a Microsoft Teams channel adapter using the Bot Framework REST API.
package teams

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

// Client sends and receives Microsoft Teams messages via the Bot Framework REST API.
type Client struct {
	appID       string
	appPassword string
	serviceURL  string
	httpClient  *http.Client
	baseURL     string
}

// NewClient creates a Teams client for the given app ID and password.
func NewClient(appID, appPassword string) *Client {
	return &Client{
		appID:       appID,
		appPassword: appPassword,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		baseURL:     "https://smba.trafficmanager.net/teams",
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient overrides the HTTP client (for testing).
func (c *Client) SetHTTPClient(h *http.Client) { c.httpClient = h }

// Send sends a plain-text message to a conversation.
func (c *Client) Send(ctx context.Context, conversationID, text string) error {
	payload := map[string]interface{}{
		"type": "message",
		"text": text,
	}
	return c.post(ctx, fmt.Sprintf("/v3/conversations/%s/activities", conversationID), payload)
}

// SendCard sends an Adaptive Card to a conversation.
func (c *Client) SendCard(ctx context.Context, conversationID string, card map[string]interface{}) error {
	payload := map[string]interface{}{
		"type": "message",
		"attachments": []map[string]interface{}{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}
	return c.post(ctx, fmt.Sprintf("/v3/conversations/%s/activities", conversationID), payload)
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("teams: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.appPassword)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("teams: API error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// InboundMessage represents a parsed incoming Teams message.
type InboundMessage struct {
	From           string
	Text           string
	ConversationID string
	MessageID      string
	Type           string
}

// webhookPayload mirrors the Bot Framework activity structure.
type webhookPayload struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Text         string `json:"text"`
	Conversation struct {
		ID string `json:"id"`
	} `json:"conversation"`
	From struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"from"`
}

// ParseWebhook parses an incoming Teams webhook request.
// Returns nil, nil when the payload is not a message activity.
func (c *Client) ParseWebhook(r *http.Request) (*InboundMessage, error) {
	if r == nil {
		return nil, fmt.Errorf("teams: nil request")
	}
	// Limit request body to 1MB to prevent OOM from oversized payloads.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("teams: read body: %w", err)
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("teams: unmarshal webhook: %w", err)
	}

	if payload.Type != "message" {
		return nil, nil
	}

	return &InboundMessage{
		From:           payload.From.ID,
		Text:           payload.Text,
		ConversationID: payload.Conversation.ID,
		MessageID:      payload.ID,
		Type:           payload.Type,
	}, nil
}

// RegisterTeamsTools registers teams.send and teams.send_card tools.
func RegisterTeamsTools(registry toolRegistry, client *Client) {
	registry.Register(
		"teams.send",
		"Send a plain-text message to a Microsoft Teams conversation.",
		`{"type":"object","properties":{"conversation_id":{"type":"string","description":"Teams conversation ID"},"text":{"type":"string","description":"Message body"}},"required":["conversation_id","text"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				ConversationID string `json:"conversation_id"`
				Text           string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("teams.send: invalid input: %w", err)
			}
			if args.ConversationID == "" || args.Text == "" {
				return "", fmt.Errorf("teams.send: conversation_id and text are required")
			}
			if err := client.Send(ctx, args.ConversationID, args.Text); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)

	registry.Register(
		"teams.send_card",
		"Send an Adaptive Card to a Microsoft Teams conversation.",
		`{"type":"object","properties":{"conversation_id":{"type":"string","description":"Teams conversation ID"},"card":{"type":"object","description":"Adaptive Card JSON payload"}},"required":["conversation_id","card"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				ConversationID string                 `json:"conversation_id"`
				Card           map[string]interface{} `json:"card"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("teams.send_card: invalid input: %w", err)
			}
			if args.ConversationID == "" || args.Card == nil {
				return "", fmt.Errorf("teams.send_card: conversation_id and card are required")
			}
			if err := client.SendCard(ctx, args.ConversationID, args.Card); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
