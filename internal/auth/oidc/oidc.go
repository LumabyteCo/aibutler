// Package oidc provides an OpenID Connect client with PKCE support for AI Butler SSO.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds OIDC client configuration.
type Config struct {
	DiscoveryURL string   // e.g., "https://accounts.google.com/.well-known/openid-configuration"
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string // default: ["openid", "profile", "email"]
}

// ProviderInfo holds the discovered OIDC provider metadata.
type ProviderInfo struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// TokenResponse holds the tokens returned from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// UserInfo holds the claims from the userinfo endpoint or ID token.
type UserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	EmailVerified bool   `json:"email_verified"`
}

// Client is the OIDC client that handles discovery, authorization, and token exchange.
type Client struct {
	cfg          Config
	httpClient   *http.Client
	providerInfo *ProviderInfo
}

// New creates a new OIDC client with the given configuration.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc: client_id is required")
	}
	if cfg.DiscoveryURL == "" {
		return nil, fmt.Errorf("oidc: discovery_url is required")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// SetHTTPClient replaces the default HTTP client (useful for testing).
func (c *Client) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

// Discover fetches the OIDC provider metadata from the discovery URL.
func (c *Client) Discover(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.DiscoveryURL, nil)
	if err != nil {
		return fmt.Errorf("oidc: create discover request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: discover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: discover returned status %d", resp.StatusCode)
	}

	var info ProviderInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("oidc: decode discover response: %w", err)
	}
	c.providerInfo = &info
	return nil
}

// AuthURL builds the authorization URL with PKCE challenge.
func (c *Client) AuthURL(state, codeVerifier string) string {
	if c.providerInfo == nil {
		return ""
	}

	challenge := pkceChallenge(codeVerifier)

	params := url.Values{
		"client_id":             {c.cfg.ClientID},
		"response_type":        {"code"},
		"redirect_uri":         {c.cfg.RedirectURL},
		"scope":                {strings.Join(c.cfg.Scopes, " ")},
		"state":                {state},
		"code_challenge":       {challenge},
		"code_challenge_method": {"S256"},
	}

	return c.providerInfo.AuthorizationEndpoint + "?" + params.Encode()
}

// Exchange trades an authorization code for tokens using PKCE.
func (c *Client) Exchange(ctx context.Context, code, codeVerifier string) (*TokenResponse, error) {
	if c.providerInfo == nil {
		return nil, fmt.Errorf("oidc: provider not discovered")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {c.cfg.RedirectURL},
		"client_id":     {c.cfg.ClientID},
		"code_verifier": {codeVerifier},
	}
	if c.cfg.ClientSecret != "" {
		data.Set("client_secret", c.cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.providerInfo.TokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oidc: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("oidc: token exchange returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("oidc: decode token response: %w", err)
	}
	return &tokenResp, nil
}

// ValidateIDToken verifies the JWT signature and standard claims of an ID token.
// It fetches JWKS from the provider to verify the RS256 signature.
func (c *Client) ValidateIDToken(ctx context.Context, idToken string) (*UserInfo, error) {
	if c.providerInfo == nil {
		return nil, fmt.Errorf("oidc: provider not discovered")
	}

	// Parse JWT parts.
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("oidc: invalid JWT format")
	}

	// Decode header to get kid.
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("oidc: decode JWT header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("oidc: parse JWT header: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("oidc: unsupported algorithm %q", header.Alg)
	}

	// Fetch JWKS.
	pubKey, err := c.fetchJWK(ctx, header.Kid)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetch JWK: %w", err)
	}

	// Verify signature.
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("oidc: decode signature: %w", err)
	}

	hash := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pubKey, 0x05, hash[:], signature); err != nil {
		return nil, fmt.Errorf("oidc: invalid signature: %w", err)
	}

	// Decode payload.
	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("oidc: decode payload: %w", err)
	}

	var claims struct {
		Iss           string `json:"iss"`
		Aud           string `json:"aud"`
		Exp           int64  `json:"exp"`
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}

	// Verify standard claims.
	if claims.Iss != c.providerInfo.Issuer {
		return nil, fmt.Errorf("oidc: issuer mismatch: got %q, want %q", claims.Iss, c.providerInfo.Issuer)
	}
	if claims.Aud != c.cfg.ClientID {
		return nil, fmt.Errorf("oidc: audience mismatch: got %q, want %q", claims.Aud, c.cfg.ClientID)
	}
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("oidc: token expired")
	}

	return &UserInfo{
		Sub:           claims.Sub,
		Email:         claims.Email,
		Name:          claims.Name,
		Picture:       claims.Picture,
		EmailVerified: claims.EmailVerified,
	}, nil
}

// UserInfo fetches user information from the provider's userinfo endpoint.
func (c *Client) UserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	if c.providerInfo == nil {
		return nil, fmt.Errorf("oidc: provider not discovered")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.providerInfo.UserinfoEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: userinfo returned status %d", resp.StatusCode)
	}

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("oidc: decode userinfo: %w", err)
	}
	return &info, nil
}

// Refresh exchanges a refresh token for new tokens.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if c.providerInfo == nil {
		return nil, fmt.Errorf("oidc: provider not discovered")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.cfg.ClientID},
	}
	if c.cfg.ClientSecret != "" {
		data.Set("client_secret", c.cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.providerInfo.TokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oidc: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("oidc: refresh returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("oidc: decode refresh response: %w", err)
	}
	return &tokenResp, nil
}

// fetchJWK fetches the JWKS from the provider and returns the RSA public key matching kid.
func (c *Client) fetchJWK(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.providerInfo.JwksURI, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}

	for _, key := range jwks.Keys {
		if key.Kid == kid && key.Kty == "RSA" {
			nBytes, err := base64URLDecode(key.N)
			if err != nil {
				return nil, fmt.Errorf("decode n: %w", err)
			}
			eBytes, err := base64URLDecode(key.E)
			if err != nil {
				return nil, fmt.Errorf("decode e: %w", err)
			}

			n := new(big.Int).SetBytes(nBytes)
			e := 0
			for _, b := range eBytes {
				e = e<<8 | int(b)
			}

			return &rsa.PublicKey{N: n, E: e}, nil
		}
	}
	return nil, fmt.Errorf("key %q not found in JWKS", kid)
}

// pkceChallenge computes the S256 PKCE code challenge from a verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// LoginHandler returns an http.HandlerFunc that redirects the user to the OIDC
// authorization endpoint. It generates a random state and code verifier,
// stores them in a cookie, and redirects.
func (c *Client) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c.providerInfo == nil {
			// Attempt discovery on first login request.
			if err := c.Discover(r.Context()); err != nil {
				log.Printf("oidc: discovery failed: %v", err)
				http.Error(w, "oidc: discovery failed", http.StatusInternalServerError)
				return
			}
		}
		// Generate random state and verifier.
		stateBytes := make([]byte, 16)
		verifierBytes := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, stateBytes); err != nil {
			http.Error(w, "oidc: random generation failed", http.StatusInternalServerError)
			return
		}
		if _, err := io.ReadFull(rand.Reader, verifierBytes); err != nil {
			http.Error(w, "oidc: random generation failed", http.StatusInternalServerError)
			return
		}
		state := base64.RawURLEncoding.EncodeToString(stateBytes)
		verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

		// Store state+verifier in a cookie for callback validation.
		// Secure: OIDC flow requires HTTPS; SameSite=Lax allows the OAuth redirect.
		http.SetCookie(w, &http.Cookie{
			Name:     "oidc_state",
			Value:    state + ":" + verifier,
			Path:     "/",
			MaxAge:   600,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		authURL := c.AuthURL(state, verifier)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// CallbackHandler returns an http.HandlerFunc that handles the OIDC redirect
// callback. It exchanges the authorization code for tokens and returns the
// user info as JSON.
func (c *Client) CallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" || state == "" {
			http.Error(w, "oidc: missing code or state", http.StatusBadRequest)
			return
		}

		// Retrieve and validate state+verifier from cookie.
		cookie, err := r.Cookie("oidc_state")
		if err != nil {
			http.Error(w, "oidc: missing state cookie", http.StatusBadRequest)
			return
		}
		parts := strings.SplitN(cookie.Value, ":", 2)
		if len(parts) != 2 || parts[0] != state {
			http.Error(w, "oidc: state mismatch", http.StatusBadRequest)
			return
		}
		verifier := parts[1]

		// Clear the state cookie.
		http.SetCookie(w, &http.Cookie{
			Name:     "oidc_state",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		// Exchange code for tokens.
		tokenResp, err := c.Exchange(r.Context(), code, verifier)
		if err != nil {
			log.Printf("oidc: token exchange failed: %v", err)
			http.Error(w, "oidc: token exchange failed", http.StatusInternalServerError)
			return
		}

		// Return token info as JSON (the ID token contains user claims).
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": tokenResp.AccessToken,
			"id_token":     tokenResp.IDToken,
			"token_type":   tokenResp.TokenType,
			"expires_in":   tokenResp.ExpiresIn,
		})
	}
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed.
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
