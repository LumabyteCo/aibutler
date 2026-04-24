package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Provider identifies an OAuth provider.
type Provider string

const (
	ProviderGmail          Provider = "gmail"
	ProviderGoogleCalendar Provider = "google_calendar"
	ProviderGitHub         Provider = "github"
)

// endpoints maps providers to their OAuth 2.0 authorization and token endpoints.
var endpoints = map[Provider]struct{ Auth, Token string }{
	ProviderGmail:          {"https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token"},
	ProviderGoogleCalendar: {"https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token"},
	ProviderGitHub:         {"https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token"},
}

// Config holds OAuth client credentials for a provider.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
}

// Token holds an OAuth access token and its metadata.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// IsExpired returns true if the token has expired (with a 30s leeway).
func (t *Token) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt.Add(-30 * time.Second))
}

// Store persists OAuth tokens to SQLite.
type Store struct {
	db *sql.DB
	mu sync.Mutex // protects concurrent Save and refresh operations
}

// NewStore creates an OAuth token store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save stores or updates a token for the given provider and account.
//
// Security: OAuth tokens are stored as plaintext in the oauth_tokens SQLite table.
// When the database file is encrypted at rest (via the vault encryption layer),
// tokens inherit that protection. For deployments without DB-level encryption,
// consider routing tokens through the vault package for field-level encryption.
func (s *Store) Save(ctx context.Context, provider Provider, accountID string, tok *Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes, _ := json.Marshal(tok.Scopes)
	var expiresAt interface{}
	if !tok.ExpiresAt.IsZero() {
		expiresAt = tok.ExpiresAt.UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_tokens (provider, account_id, access_token, refresh_token, token_type, expires_at, scopes, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider, account_id) DO UPDATE SET
		     access_token  = excluded.access_token,
		     refresh_token = excluded.refresh_token,
		     token_type    = excluded.token_type,
		     expires_at    = excluded.expires_at,
		     scopes        = excluded.scopes,
		     updated_at    = excluded.updated_at`,
		string(provider), accountID, tok.AccessToken, tok.RefreshToken,
		tok.TokenType, expiresAt, string(scopes), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("oauth: save: %w", err)
	}
	return nil
}

// Get retrieves a token for the given provider and account.
// See Save for the token-at-rest security model.
func (s *Store) Get(ctx context.Context, provider Provider, accountID string) (*Token, error) {
	var tok Token
	var expiresAt sql.NullString
	var scopesJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT access_token, COALESCE(refresh_token,''), token_type, expires_at, scopes
		 FROM oauth_tokens WHERE provider = ? AND account_id = ?`,
		string(provider), accountID).Scan(&tok.AccessToken, &tok.RefreshToken, &tok.TokenType, &expiresAt, &scopesJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth: no token for %s/%s", provider, accountID)
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: get: %w", err)
	}
	if expiresAt.Valid && expiresAt.String != "" {
		tok.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	json.Unmarshal([]byte(scopesJSON), &tok.Scopes)
	return &tok, nil
}

// Delete removes the token for the given provider and account.
func (s *Store) Delete(ctx context.Context, provider Provider, accountID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM oauth_tokens WHERE provider = ? AND account_id = ?`,
		string(provider), accountID)
	return err
}

// PKCEFlow manages the PKCE authorization flow for a provider.
type PKCEFlow struct {
	provider   Provider
	cfg        Config
	httpClient *http.Client
}

// NewPKCEFlow creates a PKCE flow.
func NewPKCEFlow(provider Provider, cfg Config) *PKCEFlow {
	return &PKCEFlow{
		provider:   provider,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// GenerateCodeVerifier generates a random PKCE code verifier and S256 challenge.
func GenerateCodeVerifier() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("oauth: generate verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// AuthURL returns the provider authorization URL.
func (f *PKCEFlow) AuthURL(state, codeChallenge string) string {
	ep := endpoints[f.provider]
	params := url.Values{
		"client_id":             {f.cfg.ClientID},
		"redirect_uri":          {f.cfg.RedirectURI},
		"response_type":         {"code"},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"scope":                 {strings.Join(f.cfg.Scopes, " ")},
	}
	return ep.Auth + "?" + params.Encode()
}

// Exchange exchanges an authorization code for tokens.
func (f *PKCEFlow) Exchange(ctx context.Context, code, codeVerifier string) (*Token, error) {
	ep := endpoints[f.provider]
	return f.doTokenRequest(ctx, ep.Token, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {f.cfg.ClientID},
		"client_secret": {f.cfg.ClientSecret},
		"redirect_uri":  {f.cfg.RedirectURI},
		"code":          {code},
		"code_verifier": {codeVerifier},
	})
}

// Refresh exchanges a refresh token for a new access token.
func (f *PKCEFlow) Refresh(ctx context.Context, tok *Token) (*Token, error) {
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("oauth: no refresh token")
	}
	ep := endpoints[f.provider]
	newTok, err := f.doTokenRequest(ctx, ep.Token, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {f.cfg.ClientID},
		"client_secret": {f.cfg.ClientSecret},
		"refresh_token": {tok.RefreshToken},
	})
	if err != nil {
		return nil, err
	}
	if newTok.RefreshToken == "" {
		newTok.RefreshToken = tok.RefreshToken
	}
	return newTok, nil
}

func (f *PKCEFlow) doTokenRequest(ctx context.Context, tokenURL string, params url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: token request: status %d: %s", resp.StatusCode, body)
	}

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("oauth: parse token: %w", err)
	}
	tok := &Token{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    raw.TokenType,
	}
	if raw.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	if raw.Scope != "" {
		tok.Scopes = strings.Fields(raw.Scope)
	}
	return tok, nil
}
