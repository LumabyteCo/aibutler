package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
)

// retryableStatusCodes are HTTP status codes that trigger a retry.
var retryableStatusCodes = map[int]bool{
	408: true, // Request Timeout
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// Client is an A2A v2 HTTP client with retry and exponential backoff.
type Client struct {
	httpClient  *http.Client
	retries     int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// New creates a new A2A v2 client.
func New(retries int) *Client {
	if retries < 0 {
		retries = 0
	}
	return &Client{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		retries:     retries,
		baseBackoff: 200 * time.Millisecond,
		maxBackoff:  2 * time.Second,
	}
}

// Discover fetches the agent card from a peer agent.
func (c *Client) Discover(ctx context.Context, peerURL string) (*a2a.AgentCard, error) {
	url := strings.TrimRight(peerURL, "/") + "/.well-known/agent.json"

	resp, err := c.doWithRetry(ctx, http.MethodGet, url, "", nil)
	if err != nil {
		return nil, fmt.Errorf("a2a.client: discover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a.client: discover: status %d", resp.StatusCode)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("a2a.client: discover: decode: %w", err)
	}
	return &card, nil
}

// Delegate sends a synchronous task to a peer agent.
func (c *Client) Delegate(ctx context.Context, peerURL, token, task string) (*a2a.TaskResult, error) {
	if token != "" && !strings.HasPrefix(peerURL, "https://") {
		log.Printf("WARNING: A2A delegation to %s uses unencrypted HTTP — bearer token may be intercepted", peerURL)
	}
	url := strings.TrimRight(peerURL, "/") + "/a2a/tasks"

	reqBody := a2a.TaskRequest{
		ID:   fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Task: task,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("a2a.client: delegate: marshal: %w", err)
	}

	resp, err := c.doWithRetry(ctx, http.MethodPost, url, token, body)
	if err != nil {
		return nil, fmt.Errorf("a2a.client: delegate: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("a2a.client: delegate: read: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a.client: delegate: status %d: %s", resp.StatusCode, respBody)
	}

	var result a2a.TaskResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("a2a.client: delegate: decode: %w", err)
	}
	return &result, nil
}

// DelegateAsync sends a task and returns the task ID immediately.
func (c *Client) DelegateAsync(ctx context.Context, peerURL, token, task string) (string, error) {
	result, err := c.Delegate(ctx, peerURL, token, task)
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

// GetTask polls the status of a previously submitted task.
func (c *Client) GetTask(ctx context.Context, peerURL, taskID string) (*a2a.TaskStatusResponse, error) {
	url := strings.TrimRight(peerURL, "/") + "/a2a/tasks/" + taskID

	resp, err := c.doWithRetry(ctx, http.MethodGet, url, "", nil)
	if err != nil {
		return nil, fmt.Errorf("a2a.client: getTask: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("a2a.client: getTask: not found: %s", taskID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a.client: getTask: status %d", resp.StatusCode)
	}

	var status a2a.TaskStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("a2a.client: getTask: decode: %w", err)
	}
	return &status, nil
}

// CancelTask cancels a running task.
func (c *Client) CancelTask(ctx context.Context, peerURL, taskID string) error {
	url := strings.TrimRight(peerURL, "/") + "/a2a/tasks/" + taskID + "/cancel"

	resp, err := c.doWithRetry(ctx, http.MethodPost, url, "", nil)
	if err != nil {
		return fmt.Errorf("a2a.client: cancelTask: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("a2a.client: cancelTask: status %d", resp.StatusCode)
	}
	return nil
}

// StreamTask opens an SSE stream for a task and returns a channel of status updates.
func (c *Client) StreamTask(ctx context.Context, peerURL, taskID string) (<-chan a2a.TaskStatusResponse, error) {
	url := strings.TrimRight(peerURL, "/") + "/a2a/tasks/" + taskID + "/stream"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("a2a.client: streamTask: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a.client: streamTask: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("a2a.client: streamTask: status %d", resp.StatusCode)
	}

	ch := make(chan a2a.TaskStatusResponse, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var status a2a.TaskStatusResponse
			if err := json.Unmarshal([]byte(data), &status); err != nil {
				continue
			}
			select {
			case ch <- status:
			case <-ctx.Done():
				return
			}
			if status.LifecycleState == a2a.TaskCompleted ||
				status.LifecycleState == a2a.TaskFailed ||
				status.LifecycleState == a2a.TaskCanceled {
				return
			}
		}
	}()

	return ch, nil
}

// doWithRetry executes an HTTP request with exponential backoff + jitter.
func (c *Client) doWithRetry(ctx context.Context, method, url, token string, body []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			backoff := c.backoffDuration(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if !retryableStatusCodes[resp.StatusCode] {
			return resp, nil
		}

		// Retryable status code: consume body and retry.
		resp.Body.Close()
		lastErr = fmt.Errorf("retryable status %d", resp.StatusCode)
	}
	return nil, fmt.Errorf("a2a.client: exhausted %d retries: %w", c.retries, lastErr)
}

// backoffDuration returns the backoff duration for a given attempt with jitter.
func (c *Client) backoffDuration(attempt int) time.Duration {
	backoff := c.baseBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
			break
		}
	}
	// Add jitter: 50-100% of backoff.
	jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
	return backoff/2 + jitter
}
