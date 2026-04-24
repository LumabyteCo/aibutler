package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Authenticator provides password + optional TOTP authentication with session management.
type Authenticator struct {
	passwordHash []byte // bcrypt hash
	totpSecret   string // base32-decoded TOTP secret (optional, empty = no TOTP)
	sessions     map[string]*AuthSession
	mu           sync.RWMutex
	maxAttempts  int // per minute (default 5)
	attempts     map[string]*attemptTracker
}

// AuthSession represents an active authenticated session.
type AuthSession struct {
	Token     string
	ExpiresAt time.Time
	ClientIP  string // IP address that created this session (for binding)
}

// maxSessionsPerIP limits concurrent sessions from a single IP address.
const maxSessionsPerIP = 10

// attemptTracker tracks login attempts per IP for rate limiting.
type attemptTracker struct {
	count     int
	windowEnd time.Time
	backoff   time.Duration
}

// New creates an Authenticator with the given bcrypt password hash and optional TOTP secret.
// Pass empty totpSecret to disable TOTP.
func New(passwordHash []byte, totpSecret string) *Authenticator {
	return &Authenticator{
		passwordHash: passwordHash,
		totpSecret:   totpSecret,
		sessions:     make(map[string]*AuthSession),
		maxAttempts:  5,
		attempts:     make(map[string]*attemptTracker),
	}
}

// Login authenticates with password and optional TOTP code.
// Returns a session token on success.
func (a *Authenticator) Login(password, totpCode string, remoteAddr string) (string, error) {
	if err := a.RateLimit(remoteAddr); err != nil {
		return "", err
	}

	if err := bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password)); err != nil {
		a.recordAttempt(remoteAddr)
		return "", fmt.Errorf("auth: invalid credentials")
	}

	// Validate TOTP if configured.
	if a.totpSecret != "" {
		if !a.validateTOTP(totpCode) {
			a.recordAttempt(remoteAddr)
			return "", fmt.Errorf("auth: invalid TOTP code")
		}
	}

	// Enforce concurrent session limit per IP.
	a.mu.RLock()
	ipCount := 0
	for _, s := range a.sessions {
		if s.ClientIP == remoteAddr && time.Now().Before(s.ExpiresAt) {
			ipCount++
		}
	}
	a.mu.RUnlock()
	if ipCount >= maxSessionsPerIP {
		return "", fmt.Errorf("auth: too many active sessions from this address")
	}

	// Generate session token.
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}

	session := &AuthSession{
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		ClientIP:  remoteAddr,
	}

	a.mu.Lock()
	a.sessions[token] = session
	a.mu.Unlock()

	return token, nil
}

// ValidateSession checks whether the given token refers to an active, non-expired session.
func (a *Authenticator) ValidateSession(token string) bool {
	return a.ValidateSessionWithIP(token, "")
}

// ValidateSessionWithIP checks whether the token is valid and optionally verifies
// that the request comes from the same IP that created the session.
func (a *Authenticator) ValidateSessionWithIP(token, remoteAddr string) bool {
	a.mu.RLock()
	session, ok := a.sessions[token]
	a.mu.RUnlock()

	if !ok {
		return false
	}
	if time.Now().After(session.ExpiresAt) {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return false
	}
	// If remoteAddr is provided, verify IP binding.
	if remoteAddr != "" && session.ClientIP != "" && session.ClientIP != remoteAddr {
		return false
	}
	return true
}

// SetSessionCookie writes a secure session cookie to the response.
// CSRF strategy: SameSite=Strict prevents cross-origin cookie sending,
// which provides implicit CSRF protection in modern browsers. This is
// sufficient for a self-hosted tool and simpler than token-based CSRF.
func SetSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// Middleware returns HTTP middleware that checks for a valid session token
// in the Authorization header or "session" cookie. Sessions are bound to the
// client IP that created them to prevent session hijacking.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""

		// Check Authorization: Bearer <token>
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// Fall back to cookie.
		if token == "" {
			if cookie, err := r.Cookie("session"); err == nil {
				token = cookie.Value
			}
		}

		// Extract client IP for session binding verification.
		clientIP := extractIP(r.RemoteAddr)

		if token == "" || !a.ValidateSessionWithIP(token, clientIP) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractIP strips the port from a RemoteAddr string (e.g., "1.2.3.4:5678" -> "1.2.3.4").
func extractIP(remoteAddr string) string {
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		return remoteAddr[:idx]
	}
	return remoteAddr
}

// RateLimit checks whether the given remote address has exceeded the allowed
// login attempts per minute. Returns an error if rate limited.
func (a *Authenticator) RateLimit(remoteAddr string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	tracker, ok := a.attempts[remoteAddr]
	if !ok {
		return nil
	}

	now := time.Now()

	// Check if in backoff period.
	if tracker.backoff > 0 && now.Before(tracker.windowEnd.Add(tracker.backoff)) {
		return fmt.Errorf("auth: too many attempts, try again later")
	}

	// Reset window if expired.
	if now.After(tracker.windowEnd) {
		tracker.count = 0
		tracker.windowEnd = now.Add(1 * time.Minute)
		tracker.backoff = 0
		return nil
	}

	if tracker.count >= a.maxAttempts {
		return fmt.Errorf("auth: too many attempts, try again later")
	}

	return nil
}

// recordAttempt increments the attempt counter and applies exponential backoff.
func (a *Authenticator) recordAttempt(remoteAddr string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	tracker, ok := a.attempts[remoteAddr]
	if !ok {
		tracker = &attemptTracker{
			windowEnd: time.Now().Add(1 * time.Minute),
		}
		a.attempts[remoteAddr] = tracker
	}

	tracker.count++
	if tracker.count >= a.maxAttempts {
		// Exponential backoff: 1s, 2s, 4s, 8s, ...
		backoffSec := math.Pow(2, float64(tracker.count-a.maxAttempts))
		if backoffSec > 300 {
			backoffSec = 300 // cap at 5 minutes
		}
		tracker.backoff = time.Duration(backoffSec) * time.Second
	}
}

// validateTOTP validates a 6-digit TOTP code using RFC 6238 (30-second period).
func (a *Authenticator) validateTOTP(code string) bool {
	if len(code) != 6 {
		return false
	}

	now := time.Now().Unix()
	period := int64(30)

	// Check current window and +/- 1 window for clock skew.
	for _, offset := range []int64{-1, 0, 1} {
		counter := (now / period) + offset
		expected := generateTOTPCode([]byte(a.totpSecret), counter)
		if code == expected {
			return true
		}
	}
	return false
}

// generateTOTPCode generates a 6-digit TOTP code per RFC 6238 / RFC 4226.
func generateTOTPCode(secret []byte, counter int64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	sum := mac.Sum(nil)

	// Dynamic truncation per RFC 4226.
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	otp := code % 1000000

	return fmt.Sprintf("%06d", otp)
}

// generateToken creates a cryptographically random hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CleanupExpired removes all expired sessions from memory.
func (a *Authenticator) CleanupExpired() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for token, session := range a.sessions {
		if now.After(session.ExpiresAt) {
			delete(a.sessions, token)
		}
	}
}

// StartCleanup starts a background goroutine that periodically removes expired sessions.
// It stops when the provided context is cancelled.
func (a *Authenticator) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.CleanupExpired()
			}
		}
	}()
}

// HashPassword creates a bcrypt hash of the given password.
// Utility function for setting up the initial password.
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}
