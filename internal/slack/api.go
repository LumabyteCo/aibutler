package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIClient abstracts the Slack Web API for testing.
type APIClient interface {
	// PostMessage sends a message and returns the timestamp (message ID).
	PostMessage(ctx context.Context, channel, text, threadTS string) (string, error)
	// UpdateMessage edits an existing message (for streaming).
	UpdateMessage(ctx context.Context, channel, ts, text string) error
	// GetWSURL calls apps.connections.open to get a Socket Mode WebSocket URL.
	GetWSURL(ctx context.Context) (string, error)
}

// httpAPIClient implements APIClient using Slack's Web API.
type httpAPIClient struct {
	botToken string
	appToken string
	client   *http.Client
}

// NewAPIClient creates a Slack API client.
func NewAPIClient(botToken, appToken string) APIClient {
	return &httpAPIClient{
		botToken: botToken,
		appToken: appToken,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *httpAPIClient) PostMessage(ctx context.Context, channel, text, threadTS string) (string, error) {
	body := map[string]interface{}{
		"channel": channel,
		"text":    text,
	}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		TS    string `json:"ts"`
	}
	if err := c.apiCall(ctx, "chat.postMessage", c.botToken, body, &result); err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("slack: postMessage: %s", result.Error)
	}
	return result.TS, nil
}

func (c *httpAPIClient) UpdateMessage(ctx context.Context, channel, ts, text string) error {
	body := map[string]interface{}{
		"channel": channel,
		"text":    text,
		"ts":      ts,
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.apiCall(ctx, "chat.update", c.botToken, body, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("slack: update: %s", result.Error)
	}
	return nil
}

func (c *httpAPIClient) GetWSURL(ctx context.Context) (string, error) {
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		URL   string `json:"url"`
	}
	if err := c.apiCall(ctx, "apps.connections.open", c.appToken, nil, &result); err != nil {
		return "", err
	}
	if !result.OK {
		return "", fmt.Errorf("slack: connections.open: %s", result.Error)
	}
	return result.URL, nil
}

func (c *httpAPIClient) apiCall(ctx context.Context, method, token string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://slack.com/api/"+method, reqBody)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(out)
}
