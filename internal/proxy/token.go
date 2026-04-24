package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/vault"
)

// TokenRefresher handles OAuth2 token refresh via HTTP POST.
type TokenRefresher struct {
	vault  vault.Vault
	client *http.Client
}

// NewTokenRefresher creates a token refresher.
func NewTokenRefresher(v vault.Vault, client *http.Client) *TokenRefresher {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &TokenRefresher{vault: v, client: client}
}

// tokenResponse is the standard OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// Refresh exchanges a refresh token for a new access token.
func (r *TokenRefresher) Refresh(ctx context.Context, cred vault.Credential, svc vault.ServiceEntry) (vault.Credential, error) {
	if svc.OAuth == nil || svc.OAuth.TokenURL == "" {
		return cred, fmt.Errorf("proxy.token: no token_url configured for %s", svc.Name)
	}
	if len(cred.RefreshToken) == 0 {
		return cred, fmt.Errorf("proxy.token: no refresh_token for %s", svc.Name)
	}

	// Build form-encoded POST body.
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {string(cred.RefreshToken)},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", svc.OAuth.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return cred, fmt.Errorf("proxy.token: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.client.Do(req)
	if err != nil {
		return cred, fmt.Errorf("proxy.token: request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != http.StatusOK {
		return cred, fmt.Errorf("proxy.token: refresh failed (HTTP %d): %s", resp.StatusCode, body)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return cred, fmt.Errorf("proxy.token: parse response: %w", err)
	}

	// Update credential.
	newCred := cred
	newCred.Value = []byte(tokenResp.AccessToken)
	if tokenResp.RefreshToken != "" {
		newCred.RefreshToken = []byte(tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		newCred.ExpiresAt = &exp
	}

	// Persist to vault.
	if err := r.vault.Store(ctx, newCred); err != nil {
		return cred, fmt.Errorf("proxy.token: persist: %w", err)
	}

	return newCred, nil
}
