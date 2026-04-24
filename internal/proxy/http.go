package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/security/ssrf"
	"github.com/LumabyteCo/aibutler/internal/vault"
)

// OfflineChecker validates URLs against offline mode.
type OfflineChecker interface {
	CheckURL(rawURL string) error
}

// HTTPExecutor makes HTTP requests with credential injection.
type HTTPExecutor struct {
	client    *http.Client
	offline   OfflineChecker
	skipSSRF  bool
}

// NewHTTPExecutor creates an executor with the given timeout.
func NewHTTPExecutor(timeout time.Duration) *HTTPExecutor {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPExecutor{
		client: &http.Client{Timeout: timeout},
	}
}

// SetOfflineGuard attaches an offline mode guard to the executor.
func (e *HTTPExecutor) SetOfflineGuard(g OfflineChecker) {
	e.offline = g
}

// SetClient allows injection of a custom HTTP client (for testing with httptest).
func (e *HTTPExecutor) SetClient(c *http.Client) {
	e.client = c
}

// SetSkipSSRF disables SSRF validation (for testing with localhost servers).
func (e *HTTPExecutor) SetSkipSSRF(skip bool) {
	e.skipSSRF = skip
}

// Do executes an HTTP request, injecting credentials from the service entry.
func (e *HTTPExecutor) Do(ctx context.Context, req AccessRequest, cred *vault.Credential, svc *vault.ServiceEntry) (*AccessResponse, error) {
	// Block requests to private/internal networks (SSRF protection).
	if !e.skipSSRF {
		if err := ssrf.ValidateURL(req.URL); err != nil {
			return nil, fmt.Errorf("proxy.http: %w", err)
		}
	}

	// Check offline guard before making external requests.
	if e.offline != nil {
		if err := e.offline.CheckURL(req.URL); err != nil {
			return nil, err
		}
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}

	method := req.Method
	if method == "" {
		method = "GET"
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return nil, fmt.Errorf("proxy.http: build request: %w", err)
	}

	// Set user-provided headers.
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Inject credential.
	if cred != nil && svc != nil {
		injectCredential(httpReq, cred, svc)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("proxy.http: execute: %w", err)
	}
	defer resp.Body.Close()

	// Read body with 1MB size limit.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("proxy.http: read body: %w", err)
	}

	respHeaders := make(map[string]string)
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	return &AccessResponse{
		StatusCode: resp.StatusCode,
		Headers:    respHeaders,
		Body:       respBody,
	}, nil
}

// injectCredential adds the appropriate auth header based on service config.
func injectCredential(req *http.Request, cred *vault.Credential, svc *vault.ServiceEntry) {
	if svc.Header == "" {
		// Default to Bearer token if no header template specified.
		req.Header.Set("Authorization", "Bearer "+string(cred.Value))
		return
	}

	// Parse header template: "Authorization: Bearer {key}" or "x-api-key: {key}"
	parts := strings.SplitN(svc.Header, ": ", 2)
	if len(parts) != 2 {
		return
	}
	headerName := parts[0]
	headerValue := strings.ReplaceAll(parts[1], "{key}", string(cred.Value))
	req.Header.Set(headerName, headerValue)
}
