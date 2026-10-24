package iot_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/iot"
)

// scriptedHA returns statuses in order (the last one repeats) and counts
// requests, so retry behavior is observable rather than inferred.
type scriptedHA struct {
	mu        sync.Mutex
	attempts  int
	responses []int
	retryAft  map[int]string // attempt index (0-based) → Retry-After value
}

func (s *scriptedHA) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		idx := s.attempts
		s.attempts++
		status := http.StatusOK
		if len(s.responses) > 0 {
			if idx < len(s.responses) {
				status = s.responses[idx]
			} else {
				status = s.responses[len(s.responses)-1]
			}
		}
		ra := s.retryAft[idx]
		s.mu.Unlock()

		if ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("scripted failure"))
			return
		}
		if r.URL.Path == "/api/states" {
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{{
				"entity_id":    "light.kitchen",
				"state":        "on",
				"last_changed": "2026-10-24T10:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Kitchen Light"},
			}})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{})
	})
}

func (s *scriptedHA) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func newScriptedAdapter(t *testing.T, s *scriptedHA) *iot.HomeAssistantAdapter {
	t.Helper()
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return iot.NewHomeAssistantAdapter(srv.URL, "test-token", nil)
}

// TestHARetriesTransientGet: an HA restart (502/503) right before a state
// read must not surface as a failure.
func TestHARetriesTransientGet(t *testing.T) {
	s := &scriptedHA{responses: []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusOK}}
	ha := newScriptedAdapter(t, s)

	devices, err := ha.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover should survive two transient failures: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	if got := s.count(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

// TestHARetryAbortsOnAuthError: a revoked token is not transient; retrying
// just delays an actionable error.
func TestHARetryAbortsOnAuthError(t *testing.T) {
	s := &scriptedHA{responses: []int{http.StatusUnauthorized}}
	ha := newScriptedAdapter(t, s)

	_, err := ha.Discover(context.Background())
	if err == nil {
		t.Fatal("expected 401 error")
	}
	if !strings.Contains(err.Error(), "token rejected") {
		t.Errorf("error should be actionable, got: %v", err)
	}
	if got := s.count(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 401)", got)
	}
}

// TestHAToggleIsNeverReplayed: toggle is non-idempotent. A 5xx may mean the
// toggle already ran; replaying it would flip the light back.
func TestHAToggleIsNeverReplayed(t *testing.T) {
	s := &scriptedHA{responses: []int{http.StatusInternalServerError}}
	ha := newScriptedAdapter(t, s)

	err := ha.Execute(context.Background(), iot.Command{
		DeviceID: "light.kitchen", Action: "toggle",
	})
	if err == nil {
		t.Fatal("expected 500 error")
	}
	if got := s.count(); got != 1 {
		t.Errorf("toggle attempts = %d, want 1 (never replay a toggle)", got)
	}
}

// TestHAIdempotentCommandRetried: turn_on can safely be replayed, so the
// same 5xx is retried.
func TestHAIdempotentCommandRetried(t *testing.T) {
	s := &scriptedHA{responses: []int{http.StatusInternalServerError}}
	ha := newScriptedAdapter(t, s)

	err := ha.Execute(context.Background(), iot.Command{
		DeviceID: "light.kitchen", Action: "off",
	})
	if err == nil {
		t.Fatal("expected 500 error after retries")
	}
	if got := s.count(); got != 3 {
		t.Errorf("turn_off attempts = %d, want 3", got)
	}
}

// TestHAToggleRetriedWhenNotProcessed: 503 (and 429) explicitly mean HA did
// not run the service, so even a toggle is safe to retry.
func TestHAToggleRetriedWhenNotProcessed(t *testing.T) {
	s := &scriptedHA{responses: []int{http.StatusServiceUnavailable, http.StatusOK}}
	ha := newScriptedAdapter(t, s)

	if err := ha.Execute(context.Background(), iot.Command{
		DeviceID: "light.kitchen", Action: "toggle",
	}); err != nil {
		t.Fatalf("toggle should retry after explicit 503: %v", err)
	}
	if got := s.count(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

// TestHARespectsRetryAfter: HA's Retry-After wins over the default backoff.
func TestHARespectsRetryAfter(t *testing.T) {
	s := &scriptedHA{
		responses: []int{http.StatusTooManyRequests, http.StatusOK},
		retryAft:  map[int]string{0: "1"},
	}
	ha := newScriptedAdapter(t, s)

	start := time.Now()
	if _, err := ha.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 800*time.Millisecond {
		t.Errorf("retry did not respect Retry-After: elapsed %v", elapsed)
	}
	if got := s.count(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

// TestHARetryStopsOnContextCancel: a slow HA must not pin a request past
// the caller's deadline.
func TestHARetryStopsOnContextCancel(t *testing.T) {
	s := &scriptedHA{responses: []int{http.StatusServiceUnavailable}}
	ha := newScriptedAdapter(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ha.Discover(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("cancellation took %v; retry loop ignored the context", elapsed)
	}
}
