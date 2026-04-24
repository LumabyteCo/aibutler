package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Quote holds a stock/crypto price quote.
type Quote struct {
	Symbol        string    `json:"symbol"`
	Name          string    `json:"name"`
	Price         float64   `json:"price"`
	Currency      string    `json:"currency"`
	Change        float64   `json:"change"`
	ChangePercent float64   `json:"change_percent"`
	Timestamp     time.Time `json:"timestamp"`
}

// Provider fetches price quotes.
type Provider interface {
	Quote(ctx context.Context, symbol string) (*Quote, error)
}

// AlphaVantageProvider fetches quotes from Alpha Vantage.
type AlphaVantageProvider struct {
	apiKey      string
	client      *http.Client
	limiter     *rateLimiter
	urlTemplate string // fmt template with %s for symbol and apikey
}

// NewAlphaVantageProvider creates a provider with the given API key.
// If client is nil, a default client with 15s timeout is used.
func NewAlphaVantageProvider(apiKey string, client *http.Client) *AlphaVantageProvider {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &AlphaVantageProvider{
		apiKey:      apiKey,
		client:      client,
		limiter:     newRateLimiter(25, 24*time.Hour), // 25 req/day free tier
		urlTemplate: "https://www.alphavantage.co/query?function=GLOBAL_QUOTE&symbol=%s&apikey=%s",
	}
}

// Quote fetches a global quote from Alpha Vantage.
func (a *AlphaVantageProvider) Quote(ctx context.Context, symbol string) (*Quote, error) {
	if !a.limiter.Allow() {
		return nil, fmt.Errorf("finance: rate limit exceeded (25 requests/day)")
	}

	url := fmt.Sprintf(a.urlTemplate, symbol, a.apiKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("finance: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("finance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("finance: API returned %d", resp.StatusCode)
	}

	var result struct {
		GlobalQuote struct {
			Symbol        string `json:"01. symbol"`
			Price         string `json:"05. price"`
			Change        string `json:"09. change"`
			ChangePercent string `json:"10. change percent"`
		} `json:"Global Quote"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("finance: decode: %w", err)
	}

	gq := result.GlobalQuote
	if gq.Symbol == "" {
		return nil, fmt.Errorf("finance: no data for symbol %q", symbol)
	}

	return &Quote{
		Symbol:        gq.Symbol,
		Price:         parseFloat(gq.Price),
		Currency:      "USD",
		Change:        parseFloat(gq.Change),
		ChangePercent: parseFloat(gq.ChangePercent),
		Timestamp:     time.Now().UTC(),
	}, nil
}

func parseFloat(s string) float64 {
	// Remove trailing % if present
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// rateLimiter is a simple token-bucket rate limiter.
type rateLimiter struct {
	mu        sync.Mutex
	tokens    int
	max       int
	window    time.Duration
	lastReset time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:    max,
		max:       max,
		window:    window,
		lastReset: time.Now(),
	}
}

func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset if window has elapsed
	if time.Since(r.lastReset) >= r.window {
		r.tokens = r.max
		r.lastReset = time.Now()
	}

	if r.tokens <= 0 {
		return false
	}
	r.tokens--
	return true
}
