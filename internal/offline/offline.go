package offline

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Guard controls offline mode. When enabled, it blocks non-localhost HTTP access.
type Guard struct {
	enabled bool
}

// NewGuard creates an offline guard.
func NewGuard(enabled bool) *Guard {
	return &Guard{enabled: enabled}
}

// IsEnabled returns whether offline mode is active.
func (g *Guard) IsEnabled() bool {
	return g.enabled
}

// CheckURL verifies a URL is allowed under the current mode.
// In offline mode, only localhost URLs are permitted.
func (g *Guard) CheckURL(rawURL string) error {
	if !g.enabled {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("offline: invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}

	// Check if it's a loopback IP.
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}

	return fmt.Errorf("offline: blocked request to %q (offline mode enabled)", host)
}

// Transport wraps an http.RoundTripper with offline guard checking.
// Use this to create HTTP clients that respect offline mode.
type Transport struct {
	guard *Guard
	base  http.RoundTripper
}

// NewTransport creates an offline-guarded transport wrapping the given base.
// If base is nil, http.DefaultTransport is used.
func NewTransport(g *Guard, base http.RoundTripper) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{guard: g, base: base}
}

// RoundTrip checks the offline guard before forwarding the request.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.guard != nil {
		if err := t.guard.CheckURL(req.URL.String()); err != nil {
			return nil, err
		}
	}
	return t.base.RoundTrip(req)
}

// NewGuardedClient creates an http.Client that blocks non-localhost when offline mode is enabled.
func NewGuardedClient(g *Guard, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: NewTransport(g, nil),
		Timeout:   timeout,
	}
}
