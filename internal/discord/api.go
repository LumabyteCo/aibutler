package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://discord.com/api/v10"

// APIClient abstracts the Discord REST API for testing.
type APIClient interface {
	// SendMessage sends a message and returns its ID.
	SendMessage(ctx context.Context, channelID, content string) (string, error)
	// EditMessage edits an existing message (for streaming).
	EditMessage(ctx context.Context, channelID, messageID, content string) error
	// TriggerTyping sends a typing indicator to a channel.
	TriggerTyping(ctx context.Context, channelID string) error
	// GetGatewayURL returns the WebSocket gateway URL.
	GetGatewayURL(ctx context.Context) (string, error)
}

// httpAPIClient implements APIClient using Discord's REST API.
type httpAPIClient struct {
	token  string
	client *http.Client
}

// NewAPIClient creates a Discord API client.
func NewAPIClient(botToken string) APIClient {
	return &httpAPIClient{
		token:  botToken,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *httpAPIClient) SendMessage(ctx context.Context, channelID, content string) (string, error) {
	body := map[string]string{"content": content}
	data, _ := json.Marshal(body)

	resp, err := c.doRequest(ctx, "POST",
		fmt.Sprintf("/channels/%s/messages", channelID), data)
	if err != nil {
		return "", err
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("discord: decode: %w", err)
	}
	return result.ID, nil
}

func (c *httpAPIClient) EditMessage(ctx context.Context, channelID, messageID, content string) error {
	body := map[string]string{"content": content}
	data, _ := json.Marshal(body)

	_, err := c.doRequest(ctx, "PATCH",
		fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID), data)
	return err
}

func (c *httpAPIClient) TriggerTyping(ctx context.Context, channelID string) error {
	_, err := c.doRequest(ctx, "POST",
		fmt.Sprintf("/channels/%s/typing", channelID), nil)
	return err
}

func (c *httpAPIClient) GetGatewayURL(ctx context.Context) (string, error) {
	resp, err := c.doRequest(ctx, "GET", "/gateway/bot", nil)
	if err != nil {
		return "", err
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("discord: decode gateway: %w", err)
	}
	return result.URL, nil
}

func (c *httpAPIClient) doRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	defer resp.Body.Close()

	// Limit response body to 10MB to prevent OOM from oversized API responses.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("discord: API %d: %s", resp.StatusCode, respBody)
	}
	return respBody, nil
}
