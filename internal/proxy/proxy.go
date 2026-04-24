package proxy

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/vault"
)

// AccessRequest describes what the agent wants to access.
type AccessRequest struct {
	Method    string
	URL       string
	Headers   map[string]string
	Body      []byte
	AgentID   string
	SessionID string
}

// AccessResponse holds the result of a proxied access.
type AccessResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// Proxy orchestrates the full resource access pipeline:
// cap check → credential resolve → token refresh → execute → audit.
type Proxy struct {
	engine    *capability.Engine
	resolver  *CredentialResolver
	executor  *HTTPExecutor
	refresher *TokenRefresher
	auditor   capability.Auditor
}

// NewProxy creates a resource access proxy.
func NewProxy(
	engine *capability.Engine,
	resolver *CredentialResolver,
	executor *HTTPExecutor,
	refresher *TokenRefresher,
	auditor capability.Auditor,
) *Proxy {
	return &Proxy{
		engine:    engine,
		resolver:  resolver,
		executor:  executor,
		refresher: refresher,
		auditor:   auditor,
	}
}

// AccessResource executes the full pipeline.
func (p *Proxy) AccessResource(ctx context.Context, caps *capability.CapabilitySet, req AccessRequest) (*AccessResponse, error) {
	domain := extractDomain(req.URL)

	// 1. Capability check.
	checkReq := capability.CheckRequest{
		Resource: "tool.web.fetch",
		Domain:   domain,
	}
	result := p.engine.Check(ctx, caps, checkReq)
	if !result.Allowed {
		p.audit(ctx, req, domain, "", "denied", result.Reason)
		return nil, fmt.Errorf("proxy: capability denied for %s: %s", domain, result.Reason)
	}

	// 2. Credential resolution.
	cred, svc, err := p.resolver.Resolve(ctx, domain)
	credKey := ""
	if svc != nil {
		credKey = svc.CredentialKey
	}
	if err != nil {
		p.audit(ctx, req, domain, credKey, "error", err.Error())
		return nil, err
	}

	// 3. Token refresh if OAuth2 and near expiry.
	if cred != nil && cred.Type == vault.CredOAuth2 && cred.ExpiresAt != nil {
		if time.Until(*cred.ExpiresAt) < 5*time.Minute {
			refreshed, refreshErr := p.refresher.Refresh(ctx, *cred, *svc)
			if refreshErr != nil {
				p.audit(ctx, req, domain, credKey, "error", "re-authorization required")
				return nil, fmt.Errorf("proxy: token refresh failed for %s: %w", domain, refreshErr)
			}
			cred = &refreshed
		}
	}

	// 4. Execute HTTP request.
	resp, execErr := p.executor.Do(ctx, req, cred, svc)

	// 5. Audit.
	status := "success"
	errMsg := ""
	if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
	}
	p.audit(ctx, req, domain, credKey, status, errMsg)

	return resp, execErr
}

func (p *Proxy) audit(ctx context.Context, req AccessRequest, domain, credKey, status, errMsg string) {
	if p.auditor == nil {
		return
	}
	_ = p.auditor.LogAccess(ctx, capability.AuditEntry{
		Timestamp:      time.Now(),
		AgentID:        req.AgentID,
		SessionID:      req.SessionID,
		ResourceType:   "http",
		Service:        domain,
		Action:         req.Method,
		Target:         req.URL,
		CapabilityUsed: "tool.web.fetch",
		CredentialKey:  credKey,
		Status:         status,
		Error:          errMsg,
	})
}

// extractDomain parses the host from a URL string.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
}
