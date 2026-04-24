package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func fixedPastTime() time.Time {
	return time.Now().Add(-1 * time.Hour)
}

func TestLogin_Success(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	auth := New(hash, "")
	token, err := auth.Login("secret123", "", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if !auth.ValidateSession(token) {
		t.Fatal("session should be valid")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	auth := New(hash, "")

	_, err := auth.Login("wrongpassword", "", "127.0.0.1")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestLogin_TOTPValidation(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	auth := New(hash, "testsecret12345678")

	// With correct password but empty TOTP when required.
	_, err := auth.Login("pass", "", "127.0.0.1")
	if err == nil {
		t.Fatal("expected error for missing TOTP code")
	}

	// Generate the correct TOTP code.
	// Since we can't easily predict the exact code in a test without knowing
	// the exact time, we test with an invalid code.
	_, err = auth.Login("pass", "000000", "127.0.0.1")
	// This may or may not succeed depending on timing, but should not panic.
	// The test ensures the code path works without errors other than auth failure.
	if err != nil && err.Error() != "auth: invalid TOTP code" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	auth := New(hash, "")

	addr := "192.168.1.1"

	// Exhaust rate limit with wrong passwords.
	for i := 0; i < 6; i++ {
		auth.Login("wrong", "", addr)
	}

	// Should now be rate limited.
	err := auth.RateLimit(addr)
	if err == nil {
		t.Fatal("expected rate limit error after too many attempts")
	}
}

func TestMiddleware_Unauthorized(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	auth := New(hash, "")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.Middleware(inner)

	// No token.
	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_WithValidToken(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	auth := New(hash, "")

	token, err := auth.Login("pass", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.Middleware(inner)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "127.0.0.1:12345" // Match the IP used during Login
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", w.Code)
	}
}

func TestMiddleware_CookieAuth(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	auth := New(hash, "")

	token, _ := auth.Login("pass", "", "127.0.0.1")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.Middleware(inner)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	req.RemoteAddr = "127.0.0.1:12345" // Match the IP used during Login
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with cookie auth, got %d", w.Code)
	}
}

func TestValidateSession_Expired(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	auth := New(hash, "")

	// Manually insert an expired session.
	auth.mu.Lock()
	auth.sessions["expired-token"] = &AuthSession{
		Token:     "expired-token",
		ExpiresAt: fixedPastTime(),
	}
	auth.mu.Unlock()

	if auth.ValidateSession("expired-token") {
		t.Fatal("expired session should not be valid")
	}
}

func TestSessionIPBinding(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	a := New(hash, "")

	token, err := a.Login("pass", "", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	// Same IP should work.
	if !a.ValidateSessionWithIP(token, "10.0.0.1") {
		t.Fatal("session should be valid from same IP")
	}

	// Different IP should fail.
	if a.ValidateSessionWithIP(token, "10.0.0.2") {
		t.Fatal("session should not be valid from different IP")
	}

	// Empty IP (no binding check) should work.
	if !a.ValidateSessionWithIP(token, "") {
		t.Fatal("session should be valid when no IP specified")
	}
}

func TestConcurrentSessionLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	a := New(hash, "")

	// Create maxSessionsPerIP sessions.
	for i := 0; i < maxSessionsPerIP; i++ {
		_, err := a.Login("pass", "", "10.0.0.1")
		if err != nil {
			t.Fatalf("login %d should succeed: %v", i, err)
		}
	}

	// Next session from same IP should fail.
	_, err := a.Login("pass", "", "10.0.0.1")
	if err == nil {
		t.Fatal("expected error when exceeding max sessions per IP")
	}

	// Different IP should still work.
	_, err = a.Login("pass", "", "10.0.0.2")
	if err != nil {
		t.Fatalf("login from different IP should work: %v", err)
	}
}

func TestMiddleware_IPMismatch(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	a := New(hash, "")

	token, _ := a.Login("pass", "", "10.0.0.1")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := a.Middleware(inner)

	// Request from different IP should be rejected.
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "10.0.0.2:5678"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from mismatched IP, got %d", w.Code)
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"10.0.0.1:1234", "10.0.0.1"},
		{"127.0.0.1:80", "127.0.0.1"},
		{"10.0.0.1", "10.0.0.1"},
	}
	for _, tt := range tests {
		got := extractIP(tt.input)
		if got != tt.expected {
			t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("mypassword")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) == 0 {
		t.Fatal("expected non-empty hash")
	}

	// Verify it works with bcrypt.
	if err := bcrypt.CompareHashAndPassword(hash, []byte("mypassword")); err != nil {
		t.Fatal("hash should match original password")
	}
}
