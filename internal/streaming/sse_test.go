package streaming

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSSEWriter_Send(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(rec)

	if err := w.Send("update", `{"status":"ok"}`); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := w.Send("update", `{"status":"done"}`); err != nil {
		t.Fatalf("send: %v", err)
	}

	body := rec.Body.String()
	if body == "" {
		t.Fatal("empty body")
	}

	// Check that event IDs are incrementing.
	if w.EventID() != 2 {
		t.Errorf("eventID = %d, want 2", w.EventID())
	}

	// Check headers.
	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestSSEWriter_SendJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(rec)

	data := map[string]string{"key": "value"}
	if err := w.SendJSON("data", data); err != nil {
		t.Fatalf("sendJSON: %v", err)
	}

	body := rec.Body.String()
	if body == "" {
		t.Fatal("empty body")
	}
	if w.EventID() != 1 {
		t.Errorf("eventID = %d, want 1", w.EventID())
	}
}

func TestSSEClient_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "id: %d\nevent: update\ndata: msg-%d\n\n", i+1, i+1)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	client := NewSSEClient(1)

	var mu sync.Mutex
	var events []string

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx, srv.URL, func(event, data string) {
		mu.Lock()
		events = append(events, data)
		mu.Unlock()
		if len(events) >= 3 {
			cancel()
		}
	})

	// Context cancellation is expected.
	if err != nil && ctx.Err() == nil {
		t.Fatalf("connect: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 3 {
		t.Errorf("got %d events, want >= 3", len(events))
	}
}

func TestSSEClient_Reconnect(t *testing.T) {
	var mu sync.Mutex
	connectionCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectionCount++
		count := connectionCount
		mu.Unlock()

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		// First connection: send one event then close.
		fmt.Fprintf(w, "id: %d\nevent: update\ndata: conn-%d\n\n", count, count)
		flusher.Flush()
		// Close immediately to trigger reconnect.
	}))
	defer srv.Close()

	client := NewSSEClient(3)
	client.retryDelay = 50 * time.Millisecond

	var eventMu sync.Mutex
	var events []string

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = client.Connect(ctx, srv.URL, func(event, data string) {
		eventMu.Lock()
		events = append(events, data)
		eventMu.Unlock()
		if len(events) >= 2 {
			cancel()
		}
	})

	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) < 2 {
		t.Errorf("got %d events, want >= 2 (indicating reconnection worked)", len(events))
	}
}
