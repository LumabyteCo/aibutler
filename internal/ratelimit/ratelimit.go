package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// window tracks requests within a time window.
type window struct {
	count    int
	resetAt  time.Time
}

// Limiter is a sliding-window rate limiter.
type Limiter struct {
	mu         sync.Mutex
	windows    map[string]*window
	limit      int
	windowDur  time.Duration
	trustProxy bool // when true, use X-Forwarded-For to extract client IP
}

// New creates a rate limiter that allows limit requests per windowDuration.
func New(limit int, windowDuration time.Duration) *Limiter {
	return &Limiter{
		windows:   make(map[string]*window),
		limit:     limit,
		windowDur: windowDuration,
	}
}

// Allow returns true if the request for the given key is within the rate limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, ok := l.windows[key]
	if !ok || now.After(w.resetAt) {
		l.windows[key] = &window{count: 1, resetAt: now.Add(l.windowDur)}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

// Reset clears the rate limit for the given key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, key)
}

// SetTrustProxy controls whether X-Forwarded-For is used to extract client IPs.
// Only enable this when the service is behind a trusted reverse proxy.
func (l *Limiter) SetTrustProxy(trust bool) {
	l.trustProxy = trust
}

// Middleware wraps an http.Handler with rate limiting based on remote IP.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := extractIP(r, l.trustProxy)
		if !l.Allow(key) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractIP extracts the remote IP from the request.
// Only trusts X-Forwarded-For when trustProxy is true, to prevent spoofing.
func extractIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Use first IP (the original client, not intermediate proxies).
			if idx := strings.Index(xff, ","); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}
	// Fall back to RemoteAddr (strip port).
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// PluginQuota tracks per-plugin call quotas with periodic resets.
type PluginQuota struct {
	mu            sync.Mutex
	counts        map[string]int
	limit         int
	resetInterval time.Duration
	lastReset     time.Time
}

// NewPluginQuota creates a plugin quota limiter.
func NewPluginQuota(limit int, resetInterval time.Duration) *PluginQuota {
	return &PluginQuota{
		counts:        make(map[string]int),
		limit:         limit,
		resetInterval: resetInterval,
		lastReset:     time.Now(),
	}
}

// Allow returns true if the plugin has not exceeded its quota.
func (q *PluginQuota) Allow(pluginName string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	if now.Sub(q.lastReset) >= q.resetInterval {
		q.counts = make(map[string]int)
		q.lastReset = now
	}

	if q.counts[pluginName] >= q.limit {
		return false
	}
	q.counts[pluginName]++
	return true
}

// Reset clears all plugin quotas.
func (q *PluginQuota) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.counts = make(map[string]int)
	q.lastReset = time.Now()
}
