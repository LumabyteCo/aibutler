package model

import (
	"net/http"
	"testing"
	"time"
)

// TestSetHTTPClientAllAdapters verifies that SetHTTPClient works on all adapter types.
func TestSetHTTPClientAllAdapters(t *testing.T) {
	pooledClient, _ := NewPooledClient(DefaultPoolConfig(), 30*time.Second)

	t.Run("ClaudeAdapter", func(t *testing.T) {
		adapter := NewClaude("test-key", "test-model", 10*time.Second, 1)
		original := adapter.client
		adapter.SetHTTPClient(pooledClient)
		if adapter.client == original {
			t.Error("SetHTTPClient did not replace the client")
		}
		if adapter.client != pooledClient {
			t.Error("SetHTTPClient did not set the pooled client")
		}
	})

	t.Run("ClaudeAdapter_nil", func(t *testing.T) {
		adapter := NewClaude("test-key", "test-model", 10*time.Second, 1)
		original := adapter.client
		adapter.SetHTTPClient(nil)
		if adapter.client != original {
			t.Error("SetHTTPClient(nil) should not replace the client")
		}
	})

	t.Run("OpenAIAdapter", func(t *testing.T) {
		adapter := NewOpenAI("test-key", "test-model", 10*time.Second, 1)
		original := adapter.client
		adapter.SetHTTPClient(pooledClient)
		if adapter.client == original {
			t.Error("SetHTTPClient did not replace the client")
		}
		if adapter.client != pooledClient {
			t.Error("SetHTTPClient did not set the pooled client")
		}
	})

	t.Run("GeminiAdapter", func(t *testing.T) {
		adapter := NewGemini("test-key", "test-model", 10*time.Second, 1)
		original := adapter.client
		adapter.SetHTTPClient(pooledClient)
		if adapter.client == original {
			t.Error("SetHTTPClient did not replace the client")
		}
		if adapter.client != pooledClient {
			t.Error("SetHTTPClient did not set the pooled client")
		}
	})

	t.Run("OpenAICompat", func(t *testing.T) {
		adapter := NewOpenAICompat("http://localhost:11434", "", "test-model", 10*time.Second, 1)
		adapter.SetHTTPClient(pooledClient)
		if adapter.client != pooledClient {
			t.Error("SetHTTPClient did not set the pooled client on compat adapter")
		}
	})

	t.Run("PooledClientReusesConnections", func(t *testing.T) {
		client, _ := NewPooledClient(DefaultPoolConfig(), 30*time.Second)
		mt, ok := client.Transport.(*metricsTransport)
		if !ok {
			t.Fatal("expected *metricsTransport")
		}
		transport, ok := mt.base.(*http.Transport)
		if !ok {
			t.Fatal("expected *http.Transport as base")
		}
		// Verify the transport is properly configured for connection reuse.
		if transport.MaxIdleConns == 0 {
			t.Error("MaxIdleConns should be > 0 for connection reuse")
		}
		if transport.MaxIdleConnsPerHost == 0 {
			t.Error("MaxIdleConnsPerHost should be > 0 for connection reuse")
		}
	})
}
