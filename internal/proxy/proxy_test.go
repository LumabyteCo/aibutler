package proxy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/proxy"
	"github.com/LumabyteCo/aibutler/internal/vault"
	"github.com/LumabyteCo/aibutler/testutil"
)

func makeTestProxy(t *testing.T, fv *testutil.FakeVault, auditor *testutil.FakeAuditor) (*proxy.Proxy, *vault.ServiceRegistry) {
	t.Helper()
	reg, err := vault.NewServiceRegistry("")
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	resolver := proxy.NewCredentialResolver(reg, fv)
	executor := proxy.NewHTTPExecutor(10 * time.Second)
	executor.SetSkipSSRF(true) // tests use localhost httptest servers
	refresher := proxy.NewTokenRefresher(fv, nil)
	engine := capability.NewEngine(auditor)
	p := proxy.NewProxy(engine, resolver, executor, refresher, auditor)
	return p, reg
}

func TestPipelineCapAllowedCredFound(t *testing.T) {
	// Test the HTTP executor with credential injection (the core of the pipeline).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sk-test-key" {
			t.Errorf("auth = %q, want Bearer sk-test-key", auth)
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"result":"ok"}`)
	}))
	defer server.Close()

	executor := proxy.NewHTTPExecutor(5 * time.Second)
	executor.SetClient(server.Client())
	executor.SetSkipSSRF(true)

	svc := vault.ServiceEntry{
		Name:          "openai",
		CredentialKey: "openai",
		Header:        "Authorization: Bearer {key}",
	}
	cred := vault.Credential{Key: "openai", Type: vault.CredAPIKey, Value: []byte("sk-test-key")}

	resp, err := executor.Do(context.Background(), proxy.AccessRequest{
		Method: "GET",
		URL:    server.URL + "/v1/models",
	}, &cred, &svc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPipelineCapDenied(t *testing.T) {
	fv := testutil.NewFakeVault()
	auditor := testutil.NewFakeAuditor()
	p, _ := makeTestProxy(t, fv, auditor)

	// No capabilities.
	caps := capability.NewCapabilitySet(nil)

	_, err := p.AccessResource(context.Background(), caps, proxy.AccessRequest{
		URL: "https://api.openai.com/v1/models",
	})
	if err == nil {
		t.Fatal("expected error for denied capability")
	}

	entries := auditor.Entries()
	if len(entries) == 0 {
		t.Fatal("expected audit entry for denial")
	}
	// The engine logs "no_capability", the proxy logs "denied". Check for either.
	found := false
	for _, e := range entries {
		if e.Status == "denied" || e.Status == "no_capability" {
			found = true
		}
	}
	if !found {
		t.Error("expected denial-related audit entry")
	}
}

func TestPipelineCredNotFound(t *testing.T) {
	fv := testutil.NewFakeVault()
	auditor := testutil.NewFakeAuditor()
	p, _ := makeTestProxy(t, fv, auditor)

	// Allow the capability but don't store credentials.
	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.fetch", Domains: []string{"api.openai.com"}},
	})

	_, err := p.AccessResource(context.Background(), caps, proxy.AccessRequest{
		URL: "https://api.openai.com/v1/models",
	})
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
}

func TestPipelineUnknownDomain(t *testing.T) {
	fv := testutil.NewFakeVault()
	auditor := testutil.NewFakeAuditor()
	p, _ := makeTestProxy(t, fv, auditor)

	// Allow the capability with wildcard domain.
	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.fetch", Domains: []string{"*"}},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "public data")
	}))
	defer server.Close()

	// Unknown domain — no credentials needed, should proceed.
	resp, err := p.AccessResource(context.Background(), caps, proxy.AccessRequest{
		URL: server.URL + "/data",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestOAuthTokenRefreshExpired(t *testing.T) {
	// Set up a mock token endpoint.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "new-refresh-token",
		})
	}))
	defer tokenServer.Close()

	fv := testutil.NewFakeVault()
	expired := time.Now().Add(-1 * time.Hour)
	fv.Store(context.Background(), vault.Credential{
		Key:          "github",
		Type:         vault.CredOAuth2,
		Value:        []byte("old-access-token"),
		ExpiresAt:    &expired,
		RefreshToken: []byte("old-refresh-token"),
	})

	refresher := proxy.NewTokenRefresher(fv, tokenServer.Client())

	svc := vault.ServiceEntry{
		Name:          "github",
		CredentialKey: "github",
		OAuth: &vault.OAuthConfig{
			TokenURL: tokenServer.URL + "/token",
		},
	}
	cred, _ := fv.Get(context.Background(), "github")

	newCred, err := refresher.Refresh(context.Background(), cred, svc)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if string(newCred.Value) != "new-access-token" {
		t.Errorf("access_token = %q, want new-access-token", newCred.Value)
	}
	if string(newCred.RefreshToken) != "new-refresh-token" {
		t.Errorf("refresh_token = %q, want new-refresh-token", newCred.RefreshToken)
	}
	if newCred.ExpiresAt == nil || time.Until(*newCred.ExpiresAt) < 30*time.Minute {
		t.Error("expected expiry ~1 hour from now")
	}

	// Verify stored in vault.
	stored, _ := fv.Get(context.Background(), "github")
	if string(stored.Value) != "new-access-token" {
		t.Errorf("stored access_token = %q", stored.Value)
	}
}

func TestOAuthTokenRefreshFails(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	defer tokenServer.Close()

	fv := testutil.NewFakeVault()
	refresher := proxy.NewTokenRefresher(fv, tokenServer.Client())

	svc := vault.ServiceEntry{
		Name:          "github",
		CredentialKey: "github",
		OAuth: &vault.OAuthConfig{
			TokenURL: tokenServer.URL + "/token",
		},
	}
	cred := vault.Credential{
		Key:          "github",
		Type:         vault.CredOAuth2,
		Value:        []byte("expired-token"),
		RefreshToken: []byte("bad-refresh"),
	}

	_, err := refresher.Refresh(context.Background(), cred, svc)
	if err == nil {
		t.Fatal("expected error for failed refresh")
	}
}

func TestHTTPExecutorBearerHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("auth = %q, want 'Bearer test-key'", auth)
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	executor := proxy.NewHTTPExecutor(5 * time.Second)
	executor.SetSkipSSRF(true)
	svc := vault.ServiceEntry{Header: "Authorization: Bearer {key}"}
	cred := vault.Credential{Value: []byte("test-key")}

	resp, err := executor.Do(context.Background(), proxy.AccessRequest{
		URL: server.URL,
	}, &cred, &svc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestHTTPExecutorCustomHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("x-api-key")
		if apiKey != "my-api-key" {
			t.Errorf("x-api-key = %q, want 'my-api-key'", apiKey)
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	executor := proxy.NewHTTPExecutor(5 * time.Second)
	executor.SetSkipSSRF(true)
	svc := vault.ServiceEntry{Header: "x-api-key: {key}"}
	cred := vault.Credential{Value: []byte("my-api-key")}

	resp, err := executor.Do(context.Background(), proxy.AccessRequest{URL: server.URL}, &cred, &svc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestCredentialResolverUnknownDomain(t *testing.T) {
	fv := testutil.NewFakeVault()
	reg, _ := vault.NewServiceRegistry("")
	resolver := proxy.NewCredentialResolver(reg, fv)

	cred, svc, err := resolver.Resolve(context.Background(), "unknown.example.com")
	if cred != nil || svc != nil || err != nil {
		t.Errorf("expected nil,nil,nil for unknown domain; got cred=%v svc=%v err=%v", cred, svc, err)
	}
}

func TestCredentialResolverKnownDomainNoCred(t *testing.T) {
	fv := testutil.NewFakeVault()
	reg, _ := vault.NewServiceRegistry("")
	resolver := proxy.NewCredentialResolver(reg, fv)

	// api.openai.com is in defaults but no credential stored.
	cred, svc, err := resolver.Resolve(context.Background(), "api.openai.com")
	if cred != nil {
		t.Error("expected nil cred")
	}
	if svc == nil {
		t.Fatal("expected non-nil svc for known domain")
	}
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
}

func TestCredentialResolverKnownDomainWithCred(t *testing.T) {
	fv := testutil.NewFakeVault()
	fv.Store(context.Background(), vault.Credential{Key: "openai", Type: vault.CredAPIKey, Value: []byte("sk-123")})

	reg, _ := vault.NewServiceRegistry("")
	resolver := proxy.NewCredentialResolver(reg, fv)

	cred, svc, err := resolver.Resolve(context.Background(), "api.openai.com")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil {
		t.Fatal("expected non-nil cred")
	}
	if string(cred.Value) != "sk-123" {
		t.Errorf("value = %q", cred.Value)
	}
	if svc.Name != "openai" {
		t.Errorf("service name = %q", svc.Name)
	}
}

func TestAuditTrailSuccess(t *testing.T) {
	fv := testutil.NewFakeVault()
	auditor := testutil.NewFakeAuditor()
	p, _ := makeTestProxy(t, fv, auditor)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.fetch", Domains: []string{"*"}},
	})

	_, err := p.AccessResource(context.Background(), caps, proxy.AccessRequest{URL: server.URL})
	if err != nil {
		t.Fatalf("access: %v", err)
	}

	entries := auditor.Entries()
	found := false
	for _, e := range entries {
		if e.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Error("expected success audit entry")
	}
}

func TestAuditTrailDenied(t *testing.T) {
	fv := testutil.NewFakeVault()
	auditor := testutil.NewFakeAuditor()
	p, _ := makeTestProxy(t, fv, auditor)

	caps := capability.NewCapabilitySet(nil)

	p.AccessResource(context.Background(), caps, proxy.AccessRequest{URL: "https://api.example.com/data"})

	entries := auditor.Entries()
	found := false
	for _, e := range entries {
		if e.Status == "denied" {
			found = true
		}
	}
	if !found {
		t.Error("expected denied audit entry")
	}
}

func TestExtractDomain(t *testing.T) {
	// Test via the pipeline — domain extraction is internal.
	// We verify indirectly by checking that the correct service is matched.
	fv := testutil.NewFakeVault()
	fv.Store(context.Background(), vault.Credential{Key: "anthropic", Type: vault.CredAPIKey, Value: []byte("sk-ant")})

	reg, _ := vault.NewServiceRegistry("")
	resolver := proxy.NewCredentialResolver(reg, fv)

	cred, svc, err := resolver.Resolve(context.Background(), "api.anthropic.com")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil || svc == nil {
		t.Fatal("expected cred and svc")
	}
	if svc.Name != "anthropic" {
		t.Errorf("service = %q, want anthropic", svc.Name)
	}
}

func TestTokenRefresherNoTokenURL(t *testing.T) {
	fv := testutil.NewFakeVault()
	refresher := proxy.NewTokenRefresher(fv, nil)

	svc := vault.ServiceEntry{Name: "test"}
	cred := vault.Credential{RefreshToken: []byte("token")}

	_, err := refresher.Refresh(context.Background(), cred, svc)
	if err == nil {
		t.Fatal("expected error for missing token_url")
	}
}

func TestTokenRefresherNoRefreshToken(t *testing.T) {
	fv := testutil.NewFakeVault()
	refresher := proxy.NewTokenRefresher(fv, nil)

	svc := vault.ServiceEntry{
		Name:  "test",
		OAuth: &vault.OAuthConfig{TokenURL: "https://example.com/token"},
	}
	cred := vault.Credential{} // No refresh token.

	_, err := refresher.Refresh(context.Background(), cred, svc)
	if err == nil {
		t.Fatal("expected error for missing refresh_token")
	}
}

func TestHTTPExecutorDefaultMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	executor := proxy.NewHTTPExecutor(5 * time.Second)
	executor.SetSkipSSRF(true)
	resp, err := executor.Do(context.Background(), proxy.AccessRequest{URL: server.URL}, nil, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
