// Package webhook provides a universal HTTP webhook channel adapter.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// toolRegistry is the interface for registering tools. Using a local narrow interface
// avoids import cycles with the tool package.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Config holds webhook adapter configuration.
type Config struct {
	OutboundURL string            // POST target for outgoing messages
	InboundPath string            // default "/webhook/inbound"
	Headers     map[string]string // custom headers for outbound
	AuthType    string            // "bearer", "apikey", "hmac", "none"
	AuthSecret  string            // token/key/HMAC secret
	HMACHeader  string            // header for HMAC signature (default "X-Signature")
}

// Adapter is the universal webhook channel adapter.
type Adapter struct {
	cfg        Config
	httpClient *http.Client
}

// New creates a webhook adapter with the given configuration.
func New(cfg Config) *Adapter {
	if cfg.InboundPath == "" {
		cfg.InboundPath = "/webhook/inbound"
	}
	if cfg.HMACHeader == "" {
		cfg.HMACHeader = "X-Signature"
	}
	return &Adapter{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// SetHTTPClient overrides the HTTP client (for testing).
func (a *Adapter) SetHTTPClient(h *http.Client) { a.httpClient = h }

// Send sends a plain-text message to the configured outbound URL.
func (a *Adapter) Send(ctx context.Context, recipient, text string) error {
	payload := map[string]interface{}{
		"recipient": recipient,
		"text":      text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.OutboundURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply auth.
	switch a.cfg.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+a.cfg.AuthSecret)
	case "apikey":
		req.Header.Set("X-API-Key", a.cfg.AuthSecret)
	case "hmac":
		sig := computeHMAC(body, a.cfg.AuthSecret)
		req.Header.Set(a.cfg.HMACHeader, sig)
	}

	// Apply custom headers.
	for k, v := range a.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook: API error %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// InboundMessage represents a parsed incoming webhook message.
type InboundMessage struct {
	Text      string `json:"text"`
	Sender    string `json:"sender"`
	ChannelID string `json:"channel_id"`
}

// InboundHandler returns an http.HandlerFunc that handles inbound webhook POSTs.
func (a *Adapter) InboundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Limit request body to 1MB to prevent OOM from oversized payloads.
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		// Verify HMAC if configured.
		if a.cfg.AuthType == "hmac" && a.cfg.AuthSecret != "" {
			sig := r.Header.Get(a.cfg.HMACHeader)
			if !a.VerifyHMAC(body, sig) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		// Verify bearer if configured (constant-time comparison to prevent timing attacks).
		if a.cfg.AuthType == "bearer" && a.cfg.AuthSecret != "" {
			auth := r.Header.Get("Authorization")
			expected := "Bearer " + a.cfg.AuthSecret
			if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		var msg InboundMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"received"}`))
	}
}

// VerifyHMAC verifies the HMAC-SHA256 signature of a payload.
func (a *Adapter) VerifyHMAC(payload []byte, signature string) bool {
	expected := computeHMAC(payload, a.cfg.AuthSecret)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// RegisterWebhookTools registers the webhook.send tool.
func RegisterWebhookTools(registry toolRegistry, adapter *Adapter) {
	registry.Register(
		"webhook.send",
		"Send a message via the custom webhook adapter.",
		`{"type":"object","properties":{"recipient":{"type":"string","description":"Recipient identifier"},"text":{"type":"string","description":"Message body"}},"required":["recipient","text"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Recipient string `json:"recipient"`
				Text      string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("webhook.send: invalid input: %w", err)
			}
			if args.Recipient == "" || args.Text == "" {
				return "", fmt.Errorf("webhook.send: recipient and text are required")
			}
			if err := adapter.Send(ctx, args.Recipient, args.Text); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
