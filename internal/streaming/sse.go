package streaming

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// SSEWriter writes Server-Sent Events to an HTTP response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	eventID int64
}

// NewSSEWriter creates a new SSE writer and sets appropriate headers.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)
	return &SSEWriter{w: w, flusher: flusher}
}

// Send writes an SSE frame with an incrementing event ID.
func (s *SSEWriter) Send(event, data string) error {
	id := atomic.AddInt64(&s.eventID, 1)
	_, err := fmt.Fprintf(s.w, "id: %d\nevent: %s\ndata: %s\n\n", id, event, data)
	if err != nil {
		return fmt.Errorf("sse: write: %w", err)
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// SendJSON encodes v as JSON and sends it as an SSE frame.
func (s *SSEWriter) SendJSON(event string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sse: marshal: %w", err)
	}
	return s.Send(event, string(data))
}

// EventID returns the current event ID counter.
func (s *SSEWriter) EventID() int64 {
	return atomic.LoadInt64(&s.eventID)
}

// SSEClient connects to an SSE endpoint with automatic reconnection.
type SSEClient struct {
	httpClient *http.Client
	maxRetries int
	retryDelay time.Duration
}

// NewSSEClient creates an SSE client with reconnection support.
func NewSSEClient(maxRetries int) *SSEClient {
	return &SSEClient{
		httpClient: &http.Client{Timeout: 0}, // No timeout for streaming.
		maxRetries: maxRetries,
		retryDelay: 1 * time.Second,
	}
}

// Connect connects to an SSE endpoint and calls handler for each event.
// Automatically reconnects on disconnect, using Last-Event-ID for resumption.
func (c *SSEClient) Connect(ctx context.Context, url string, handler func(event, data string)) error {
	var lastEventID string
	retries := 0

	for {
		err := c.stream(ctx, url, lastEventID, func(id, event, data string) {
			if id != "" {
				lastEventID = id
			}
			handler(event, data)
			retries = 0 // Reset retries on successful event.
		})

		if ctx.Err() != nil {
			return ctx.Err()
		}

		retries++
		if retries > c.maxRetries {
			return fmt.Errorf("sse: max retries (%d) exceeded: %w", c.maxRetries, err)
		}

		log.Printf("sse: connection lost (attempt %d/%d), reconnecting in %s...",
			retries, c.maxRetries, c.retryDelay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.retryDelay):
		}
	}
}

func (c *SSEClient) stream(ctx context.Context, url, lastEventID string, handler func(id, event, data string)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("sse: create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sse: connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sse: server returned %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	var currentEvent, currentData, currentID string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = end of event.
			if currentData != "" {
				event := currentEvent
				if event == "" {
					event = "message"
				}
				handler(currentID, event, currentData)
			}
			currentEvent = ""
			currentData = ""
			currentID = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "id: ") {
			currentID = strings.TrimPrefix(line, "id: ")
		}
	}

	return scanner.Err()
}
