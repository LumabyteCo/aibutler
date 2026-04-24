// Package line provides a LINE Messaging API channel adapter.
package line

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

// Client sends and receives LINE messages via the Messaging API.
type Client struct {
	channelSecret string
	accessToken   string
	httpClient    *http.Client
	baseURL       string
}

// NewClient creates a LINE client for the given channel secret and access token.
func NewClient(channelSecret, accessToken string) *Client {
	return &Client{
		channelSecret: channelSecret,
		accessToken:   accessToken,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		baseURL:       "https://api.line.me/v2/bot",
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient overrides the HTTP client (for testing).
func (c *Client) SetHTTPClient(h *http.Client) { c.httpClient = h }

// Send sends a plain-text push message to the recipient.
func (c *Client) Send(ctx context.Context, to, text string) error {
	payload := map[string]interface{}{
		"to": to,
		"messages": []map[string]string{
			{"type": "text", "text": text},
		},
	}
	return c.post(ctx, "/message/push", payload)
}

// FlexContainer represents a LINE Flex Message container.
type FlexContainer struct {
	Type     string      `json:"type"`
	AltText  string      `json:"altText"`
	Contents interface{} `json:"contents"`
}

// SendFlexMessage sends a Flex Message to the recipient.
func (c *Client) SendFlexMessage(ctx context.Context, to string, altText string, contents interface{}) error {
	payload := map[string]interface{}{
		"to": to,
		"messages": []map[string]interface{}{
			{
				"type":    "flex",
				"altText": altText,
				"contents": contents,
			},
		},
	}
	return c.post(ctx, "/message/push", payload)
}

// QuickReplyItem represents a quick reply button.
type QuickReplyItem struct {
	Type   string      `json:"type"`
	Action interface{} `json:"action"`
}

// SendQuickReply sends a text message with quick reply buttons.
func (c *Client) SendQuickReply(ctx context.Context, to, text string, items []QuickReplyItem) error {
	payload := map[string]interface{}{
		"to": to,
		"messages": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
				"quickReply": map[string]interface{}{
					"items": items,
				},
			},
		},
	}
	return c.post(ctx, "/message/push", payload)
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("line: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("line: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("line: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("line: API error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// InboundMessage represents a parsed incoming LINE webhook event.
type InboundMessage struct {
	ReplyToken string
	UserID     string
	Text       string
	MessageID  string
	Type       string
}

// webhookPayload mirrors the LINE webhook event structure.
type webhookPayload struct {
	Events []struct {
		Type       string `json:"type"`
		ReplyToken string `json:"replyToken"`
		Source     struct {
			Type   string `json:"type"`
			UserID string `json:"userId"`
		} `json:"source"`
		Message struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"message"`
	} `json:"events"`
}

// ParseWebhook parses an incoming LINE webhook request.
// Returns nil, nil when the payload contains no message events.
func ParseWebhook(r *http.Request) (*InboundMessage, error) {
	if r == nil {
		return nil, fmt.Errorf("line: nil request")
	}
	// Limit request body to 1MB to prevent OOM from oversized payloads.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("line: read body: %w", err)
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("line: unmarshal webhook: %w", err)
	}

	for _, event := range payload.Events {
		if event.Type == "message" {
			return &InboundMessage{
				ReplyToken: event.ReplyToken,
				UserID:     event.Source.UserID,
				Text:       event.Message.Text,
				MessageID:  event.Message.ID,
				Type:       event.Message.Type,
			}, nil
		}
	}
	return nil, nil
}

// RegisterLINETools registers line.send, line.send_flex, and line.send_quick_reply tools.
func RegisterLINETools(registry toolRegistry, client *Client) {
	registry.Register(
		"line.send",
		"Send a LINE text message to a user.",
		`{"type":"object","properties":{"to":{"type":"string","description":"Recipient user ID"},"text":{"type":"string","description":"Message body"}},"required":["to","text"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				To   string `json:"to"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("line.send: invalid input: %w", err)
			}
			if args.To == "" || args.Text == "" {
				return "", fmt.Errorf("line.send: to and text are required")
			}
			if err := client.Send(ctx, args.To, args.Text); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)

	registry.Register(
		"line.send_flex",
		"Send a LINE Flex Message to a user.",
		`{"type":"object","properties":{"to":{"type":"string","description":"Recipient user ID"},"alt_text":{"type":"string","description":"Alt text for notification"},"contents":{"type":"object","description":"Flex Message container object"}},"required":["to","alt_text","contents"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				To       string      `json:"to"`
				AltText  string      `json:"alt_text"`
				Contents interface{} `json:"contents"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("line.send_flex: invalid input: %w", err)
			}
			if args.To == "" || args.AltText == "" {
				return "", fmt.Errorf("line.send_flex: to and alt_text are required")
			}
			if err := client.SendFlexMessage(ctx, args.To, args.AltText, args.Contents); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)

	registry.Register(
		"line.send_quick_reply",
		"Send a LINE text message with quick reply buttons.",
		`{"type":"object","properties":{"to":{"type":"string","description":"Recipient user ID"},"text":{"type":"string","description":"Message body"},"items":{"type":"array","items":{"type":"object"},"description":"Quick reply items"}},"required":["to","text","items"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				To    string           `json:"to"`
				Text  string           `json:"text"`
				Items []QuickReplyItem `json:"items"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("line.send_quick_reply: invalid input: %w", err)
			}
			if args.To == "" || args.Text == "" {
				return "", fmt.Errorf("line.send_quick_reply: to and text are required")
			}
			if err := client.SendQuickReply(ctx, args.To, args.Text, args.Items); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
