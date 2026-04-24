package model

import (
	"net/http"
	"testing"
	"time"
)

func TestDefaultPoolConfig(t *testing.T) {
	cfg := DefaultPoolConfig()
	if cfg.MaxIdleConns != 10 {
		t.Errorf("MaxIdleConns = %d, want 10", cfg.MaxIdleConns)
	}
	if cfg.MaxConnsPerHost != 10 {
		t.Errorf("MaxConnsPerHost = %d, want 10", cfg.MaxConnsPerHost)
	}
	if cfg.IdleTimeout != 90*time.Second {
		t.Errorf("IdleTimeout = %v, want 90s", cfg.IdleTimeout)
	}
}

func TestNewPooledClient(t *testing.T) {
	client, metrics := NewPooledClient(DefaultPoolConfig(), 30*time.Second)
	if client == nil {
		t.Fatal("NewPooledClient returned nil")
	}
	if metrics == nil {
		t.Fatal("NewPooledClient returned nil metrics")
	}
	if client.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", client.Timeout)
	}

	// Transport should be metricsTransport wrapping http.Transport
	mt, ok := client.Transport.(*metricsTransport)
	if !ok {
		t.Fatal("Transport is not *metricsTransport")
	}
	transport, ok := mt.base.(*http.Transport)
	if !ok {
		t.Fatal("base transport is not *http.Transport")
	}
	if transport.MaxIdleConns != 10 {
		t.Errorf("MaxIdleConns = %d, want 10", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 10 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 10", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 10 {
		t.Errorf("MaxConnsPerHost = %d, want 10", transport.MaxConnsPerHost)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", transport.IdleConnTimeout)
	}
	if transport.DisableCompression {
		t.Error("DisableCompression should be false")
	}

	// Verify metrics start at zero
	stats := metrics.Stats()
	if stats.TotalRequests != 0 || stats.ActiveRequests != 0 || stats.Errors != 0 {
		t.Errorf("initial stats should be zero, got %+v", stats)
	}
}

func TestNewPooledClientCustomConfig(t *testing.T) {
	cfg := PoolConfig{
		MaxIdleConns:    20,
		MaxConnsPerHost: 15,
		IdleTimeout:     120 * time.Second,
	}
	client, _ := NewPooledClient(cfg, 60*time.Second)
	if client.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", client.Timeout)
	}

	mt := client.Transport.(*metricsTransport)
	transport := mt.base.(*http.Transport)
	if transport.MaxIdleConns != 20 {
		t.Errorf("MaxIdleConns = %d, want 20", transport.MaxIdleConns)
	}
	if transport.MaxConnsPerHost != 15 {
		t.Errorf("MaxConnsPerHost = %d, want 15", transport.MaxConnsPerHost)
	}
	if transport.IdleConnTimeout != 120*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 120s", transport.IdleConnTimeout)
	}
}
