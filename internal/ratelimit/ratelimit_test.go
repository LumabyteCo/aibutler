package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAllow_UnderLimit(t *testing.T) {
	l := New(5, time.Minute)
	for i := 0; i < 5; i++ {
		if !l.Allow("client-1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestAllow_OverLimit(t *testing.T) {
	l := New(3, time.Minute)
	for i := 0; i < 3; i++ {
		l.Allow("client-1")
	}
	if l.Allow("client-1") {
		t.Error("4th request should be denied")
	}
}

func TestAllow_SeparateKeys(t *testing.T) {
	l := New(1, time.Minute)
	if !l.Allow("a") {
		t.Error("first request for 'a' should be allowed")
	}
	if l.Allow("a") {
		t.Error("second request for 'a' should be denied")
	}
	if !l.Allow("b") {
		t.Error("first request for 'b' should be allowed (separate key)")
	}
}

func TestMiddleware(t *testing.T) {
	l := New(2, time.Minute)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := l.Middleware(inner)

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200", i+1, rec.Code)
		}
	}

	// Third request should be rate limited.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("3rd request: got %d, want 429", rec.Code)
	}
}

func TestPluginQuota(t *testing.T) {
	q := NewPluginQuota(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !q.Allow("my-plugin") {
			t.Errorf("call %d should be allowed", i+1)
		}
	}
	if q.Allow("my-plugin") {
		t.Error("4th call should be denied")
	}

	// Different plugin should still be allowed.
	if !q.Allow("other-plugin") {
		t.Error("other-plugin should be allowed")
	}

	// Reset should allow again.
	q.Reset()
	if !q.Allow("my-plugin") {
		t.Error("after reset, should be allowed")
	}
}
