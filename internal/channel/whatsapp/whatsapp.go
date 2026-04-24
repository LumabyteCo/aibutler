// Package whatsapp provides a WhatsApp channel adapter using the Meta Cloud API.
package whatsapp

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

// Client sends and receives WhatsApp messages via the Meta Cloud API.
type Client struct {
	phoneNumberID string
	accessToken   string
	httpClient    *http.Client
	baseURL       string
}

// NewClient creates a WhatsApp client for the given phone number ID and access token.
func NewClient(phoneNumberID, accessToken string) *Client {
	return &Client{
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		baseURL:       "https://graph.facebook.com/v19.0",
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient overrides the HTTP client (for testing).
func (c *Client) SetHTTPClient(h *http.Client) { c.httpClient = h }

// Send sends a plain-text message to the recipient.
func (c *Client) Send(ctx context.Context, to, text string) error {
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": text},
	}
	return c.post(ctx, fmt.Sprintf("/%s/messages", c.phoneNumberID), payload)
}

// SendTemplate sends a WhatsApp template message.
func (c *Client) SendTemplate(ctx context.Context, to, templateName string, components []map[string]any) error {
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]interface{}{
			"name":       templateName,
			"language":   map[string]string{"code": "en_US"},
			"components": components,
		},
	}
	return c.post(ctx, fmt.Sprintf("/%s/messages", c.phoneNumberID), payload)
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("whatsapp: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp: API error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// InboundMessage represents a parsed incoming WhatsApp message.
type InboundMessage struct {
	From      string
	Body      string
	MessageID string
	Type      string
}

// webhookPayload mirrors the Meta Cloud API webhook structure.
type webhookPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		Changes []struct {
			Value struct {
				Messages []struct {
					ID   string `json:"id"`
					From string `json:"from"`
					Type string `json:"type"`
					Text struct {
						Body string `json:"body"`
					} `json:"text,omitempty"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// ParseWebhook parses an incoming WhatsApp webhook request.
// Returns nil, nil when the payload contains no messages.
func ParseWebhook(r *http.Request) (*InboundMessage, error) {
	if r == nil {
		return nil, fmt.Errorf("whatsapp: nil request")
	}
	// Limit request body to 1MB to prevent OOM from oversized payloads.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("whatsapp: read body: %w", err)
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("whatsapp: unmarshal webhook: %w", err)
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				return &InboundMessage{
					From:      msg.From,
					Body:      msg.Text.Body,
					MessageID: msg.ID,
					Type:      msg.Type,
				}, nil
			}
		}
	}
	return nil, nil
}

// RegisterWhatsAppTools registers whatsapp.send and whatsapp.send_template tools.
func RegisterWhatsAppTools(registry toolRegistry, client *Client) {
	registry.Register(
		"whatsapp.send",
		"Send a WhatsApp text message to a phone number.",
		`{"type":"object","properties":{"to":{"type":"string","description":"Recipient phone number (E.164 format, e.g. 15551234567)"},"text":{"type":"string","description":"Message body"}},"required":["to","text"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				To   string `json:"to"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("whatsapp.send: invalid input: %w", err)
			}
			if args.To == "" || args.Text == "" {
				return "", fmt.Errorf("whatsapp.send: to and text are required")
			}
			if err := client.Send(ctx, args.To, args.Text); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)

	registry.Register(
		"whatsapp.send_template",
		"Send a WhatsApp template message to a phone number.",
		`{"type":"object","properties":{"to":{"type":"string","description":"Recipient phone number (E.164 format)"},"template_name":{"type":"string","description":"Approved template name"},"components":{"type":"array","items":{"type":"object"},"description":"Template component parameters"}},"required":["to","template_name"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				To           string           `json:"to"`
				TemplateName string           `json:"template_name"`
				Components   []map[string]any `json:"components"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("whatsapp.send_template: invalid input: %w", err)
			}
			if args.To == "" || args.TemplateName == "" {
				return "", fmt.Errorf("whatsapp.send_template: to and template_name are required")
			}
			if err := client.SendTemplate(ctx, args.To, args.TemplateName, args.Components); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
