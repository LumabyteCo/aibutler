package finance

import (
	"time"
)

// SetProviderURL overrides the URL template for testing with httptest servers.
func SetProviderURL(p *AlphaVantageProvider, urlTemplate string) {
	p.urlTemplate = urlTemplate
}

// TestRateLimiter wraps rateLimiter for external test access.
type TestRateLimiter struct {
	rl *rateLimiter
}

// NewTestRateLimiter creates a rate limiter accessible from tests.
func NewTestRateLimiter(max int, window time.Duration) *TestRateLimiter {
	return &TestRateLimiter{rl: newRateLimiter(max, window)}
}

// Allow delegates to the underlying rate limiter.
func (t *TestRateLimiter) Allow() bool {
	return t.rl.Allow()
}

// NewPriceTool creates a priceTool for external testing.
func NewPriceTool(provider Provider) *priceTool {
	return &priceTool{provider: provider}
}
