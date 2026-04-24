package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
)

func TestDiscover(t *testing.T) {
	card := a2a.AgentCard{
		Name:         "test-agent",
		Description:  "A test agent",
		URL:          "http://localhost:8081",
		Capabilities: []string{"memory.search"},
		Version:      "1.0.0",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent.json" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()

	c := New(0)
	got, err := c.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: unexpected error: %v", err)
	}
	if got.Name != card.Name {
		t.Errorf("Name = %q, want %q", got.Name, card.Name)
	}
	if got.Version != card.Version {
		t.Errorf("Version = %q, want %q", got.Version, card.Version)
	}
}

func TestDelegate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a/tasks" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req a2a.TaskRequest
		json.NewDecoder(r.Body).Decode(&req)

		result := a2a.TaskResult{
			ID:     req.ID,
			Status: "completed",
			Output: "task done: " + req.Task,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer srv.Close()

	c := New(0)
	result, err := c.Delegate(context.Background(), srv.URL, "test-token", "do something")
	if err != nil {
		t.Fatalf("Delegate: unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.Output != "task done: do something" {
		t.Errorf("Output = %q, want %q", result.Output, "task done: do something")
	}
}

func TestDelegateAsync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req a2a.TaskRequest
		json.NewDecoder(r.Body).Decode(&req)
		result := a2a.TaskResult{
			ID:     req.ID,
			Status: "pending",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer srv.Close()

	c := New(0)
	taskID, err := c.DelegateAsync(context.Background(), srv.URL, "tok", "async task")
	if err != nil {
		t.Fatalf("DelegateAsync: unexpected error: %v", err)
	}
	if taskID == "" {
		t.Error("DelegateAsync: expected non-empty task ID")
	}
}

func TestGetTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a/tasks/task-123" {
			http.NotFound(w, r)
			return
		}
		resp := a2a.TaskStatusResponse{
			ID:             "task-123",
			LifecycleState: a2a.TaskCompleted,
			Output:         "done",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(0)
	status, err := c.GetTask(context.Background(), srv.URL, "task-123")
	if err != nil {
		t.Fatalf("GetTask: unexpected error: %v", err)
	}
	if status.LifecycleState != a2a.TaskCompleted {
		t.Errorf("LifecycleState = %q, want %q", status.LifecycleState, a2a.TaskCompleted)
	}
}

func TestRetryBackoff(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		card := a2a.AgentCard{Name: "recovered"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()

	c := New(3)
	c.baseBackoff = 10 * time.Millisecond
	c.maxBackoff = 50 * time.Millisecond

	card, err := c.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("RetryBackoff: unexpected error: %v", err)
	}
	if card.Name != "recovered" {
		t.Errorf("Name = %q, want %q", card.Name, "recovered")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}
