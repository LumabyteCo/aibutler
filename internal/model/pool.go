package model

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// PoolConfig holds connection pool tuning parameters.
type PoolConfig struct {
	MaxIdleConns    int           // default 10
	MaxConnsPerHost int           // default 10
	IdleTimeout     time.Duration // default 90s
}

// DefaultPoolConfig returns sensible defaults for API connection pooling.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleConns:    10,
		MaxConnsPerHost: 10,
		IdleTimeout:     90 * time.Second,
	}
}

// PoolMetrics tracks connection pool usage statistics.
type PoolMetrics struct {
	TotalRequests  atomic.Int64
	ActiveRequests atomic.Int64
	Errors         atomic.Int64
}

// Stats returns a snapshot of pool metrics.
func (m *PoolMetrics) Stats() PoolStats {
	return PoolStats{
		TotalRequests:  m.TotalRequests.Load(),
		ActiveRequests: m.ActiveRequests.Load(),
		Errors:         m.Errors.Load(),
	}
}

// PoolStats is a point-in-time snapshot of pool metrics.
type PoolStats struct {
	TotalRequests  int64
	ActiveRequests int64
	Errors         int64
}

// metricsTransport wraps http.Transport to track request metrics.
type metricsTransport struct {
	base    http.RoundTripper
	metrics *PoolMetrics
}

func (t *metricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.metrics.TotalRequests.Add(1)
	t.metrics.ActiveRequests.Add(1)
	defer t.metrics.ActiveRequests.Add(-1)

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.metrics.Errors.Add(1)
		return nil, err
	}
	if resp.StatusCode >= 500 {
		t.metrics.Errors.Add(1)
	}
	return resp, nil
}

// NewPooledClient returns an http.Client with Transport configured for
// connection reuse across multiple API provider adapters, with request
// metrics tracking.
//
// Go's http.Transport is the connection pool — it manages idle connections,
// keep-alive, and per-host limits internally. This function configures
// those settings and wraps the transport with metrics collection.
func NewPooledClient(cfg PoolConfig, timeout time.Duration) (*http.Client, *PoolMetrics) {
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.MaxConnsPerHost <= 0 {
		cfg.MaxConnsPerHost = 10
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 90 * time.Second
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxConnsPerHost,
		MaxConnsPerHost:     cfg.MaxConnsPerHost,
		IdleConnTimeout:     cfg.IdleTimeout,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableCompression:  false,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	metrics := &PoolMetrics{}

	return &http.Client{
		Timeout:   timeout,
		Transport: &metricsTransport{base: transport, metrics: metrics},
	}, metrics
}

// NewPooledClientSimple returns an http.Client without metrics (backward compatible).
func NewPooledClientSimple(cfg PoolConfig, timeout time.Duration) *http.Client {
	client, _ := NewPooledClient(cfg, timeout)
	return client
}
