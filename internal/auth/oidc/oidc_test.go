package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/auth/oidc"
)

// mockIdP creates a test OIDC provider with RSA keys and all necessary endpoints.
type mockIdP struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	kid        string
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	m := &mockIdP{
		privateKey: key,
		kid:        "test-key-1",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.handleDiscovery)
	mux.HandleFunc("/jwks", m.handleJWKS)
	mux.HandleFunc("/token", m.handleToken)
	mux.HandleFunc("/userinfo", m.handleUserInfo)

	m.server = httptest.NewServer(mux)
	return m
}

func (m *mockIdP) close() {
	m.server.Close()
}

func (m *mockIdP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	info := map[string]string{
		"issuer":                 m.server.URL,
		"authorization_endpoint": m.server.URL + "/authorize",
		"token_endpoint":         m.server.URL + "/token",
		"userinfo_endpoint":      m.server.URL + "/userinfo",
		"jwks_uri":               m.server.URL + "/jwks",
	}
	json.NewEncoder(w).Encode(info)
}

func (m *mockIdP) handleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := m.privateKey.PublicKey
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()

	jwks := map[string]interface{}{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"kid": m.kid,
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}
	json.NewEncoder(w).Encode(jwks)
}

func (m *mockIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	grantType := r.FormValue("grant_type")
	idToken := m.signTestJWT()

	resp := map[string]interface{}{
		"access_token":  "mock-access-token",
		"id_token":      idToken,
		"refresh_token": "mock-refresh-token",
		"expires_in":    3600,
		"token_type":    "Bearer",
	}
	if grantType == "refresh_token" {
		resp["access_token"] = "mock-refreshed-access-token"
	}
	json.NewEncoder(w).Encode(resp)
}

func (m *mockIdP) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	info := map[string]interface{}{
		"sub":            "user-123",
		"email":          "test@example.com",
		"name":           "Test User",
		"picture":        "https://example.com/photo.jpg",
		"email_verified": true,
	}
	json.NewEncoder(w).Encode(info)
}

func (m *mockIdP) signTestJWT() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(
		fmt.Sprintf(`{"alg":"RS256","kid":"%s"}`, m.kid)))

	claims := map[string]interface{}{
		"iss":            m.server.URL,
		"aud":            "test-client-id",
		"exp":            time.Now().Add(1 * time.Hour).Unix(),
		"sub":            "user-123",
		"email":          "test@example.com",
		"name":           "Test User",
		"email_verified": true,
	}
	claimsJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, m.privateKey, 0x05, hash[:])
	signature := base64.RawURLEncoding.EncodeToString(sig)

	return signingInput + "." + signature
}

func TestDiscover(t *testing.T) {
	idp := newMockIdP(t)
	defer idp.close()

	client, err := oidc.New(oidc.Config{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		ClientID:     "test-client-id",
		RedirectURL:  "http://localhost/callback",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := client.Discover(context.Background()); err != nil {
		t.Fatalf("discover: %v", err)
	}
}

func TestAuthURLPKCE(t *testing.T) {
	idp := newMockIdP(t)
	defer idp.close()

	client, _ := oidc.New(oidc.Config{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		ClientID:     "test-client-id",
		RedirectURL:  "http://localhost/callback",
	})
	_ = client.Discover(context.Background())

	authURL := client.AuthURL("test-state", "test-verifier")
	if authURL == "" {
		t.Fatal("expected non-empty auth URL")
	}
	if !strings.Contains(authURL, "code_challenge=") {
		t.Error("auth URL missing code_challenge")
	}
	if !strings.Contains(authURL, "code_challenge_method=S256") {
		t.Error("auth URL missing S256 method")
	}
	if !strings.Contains(authURL, "state=test-state") {
		t.Error("auth URL missing state parameter")
	}
}

func TestExchange(t *testing.T) {
	idp := newMockIdP(t)
	defer idp.close()

	client, _ := oidc.New(oidc.Config{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		ClientID:     "test-client-id",
		RedirectURL:  "http://localhost/callback",
	})
	_ = client.Discover(context.Background())

	resp, err := client.Exchange(context.Background(), "auth-code", "verifier")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.AccessToken != "mock-access-token" {
		t.Errorf("access_token = %q, want %q", resp.AccessToken, "mock-access-token")
	}
	if resp.RefreshToken != "mock-refresh-token" {
		t.Errorf("refresh_token = %q, want %q", resp.RefreshToken, "mock-refresh-token")
	}
	if resp.IDToken == "" {
		t.Error("expected non-empty id_token")
	}
}

func TestValidateIDToken(t *testing.T) {
	idp := newMockIdP(t)
	defer idp.close()

	client, _ := oidc.New(oidc.Config{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		ClientID:     "test-client-id",
		RedirectURL:  "http://localhost/callback",
	})
	_ = client.Discover(context.Background())

	idToken := idp.signTestJWT()
	info, err := client.ValidateIDToken(context.Background(), idToken)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if info.Sub != "user-123" {
		t.Errorf("sub = %q, want %q", info.Sub, "user-123")
	}
	if info.Email != "test@example.com" {
		t.Errorf("email = %q, want %q", info.Email, "test@example.com")
	}
	if info.Name != "Test User" {
		t.Errorf("name = %q, want %q", info.Name, "Test User")
	}
}

func TestUserInfoEndpoint(t *testing.T) {
	idp := newMockIdP(t)
	defer idp.close()

	client, _ := oidc.New(oidc.Config{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		ClientID:     "test-client-id",
		RedirectURL:  "http://localhost/callback",
	})
	_ = client.Discover(context.Background())

	info, err := client.UserInfo(context.Background(), "some-access-token")
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	if info.Sub != "user-123" {
		t.Errorf("sub = %q, want %q", info.Sub, "user-123")
	}
	if !info.EmailVerified {
		t.Error("expected email_verified = true")
	}
}

func TestRefresh(t *testing.T) {
	idp := newMockIdP(t)
	defer idp.close()

	client, _ := oidc.New(oidc.Config{
		DiscoveryURL: idp.server.URL + "/.well-known/openid-configuration",
		ClientID:     "test-client-id",
		RedirectURL:  "http://localhost/callback",
	})
	_ = client.Discover(context.Background())

	resp, err := client.Refresh(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if resp.AccessToken != "mock-refreshed-access-token" {
		t.Errorf("access_token = %q, want %q", resp.AccessToken, "mock-refreshed-access-token")
	}
}
