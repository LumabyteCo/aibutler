package webauthn_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/auth/webauthn"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestBeginRegistration(t *testing.T) {
	db := testutil.TestDB(t)
	srv := webauthn.New(webauthn.Config{
		RPID:     "butler.example.com",
		RPOrigin: "https://butler.example.com",
		RPName:   "AI Butler",
	}, db.Conn())

	ctx := context.Background()

	// Valid registration start.
	challenge, err := srv.BeginRegistration(ctx, "user-1")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	if len(challenge.Challenge) != 32 {
		t.Errorf("challenge length = %d, want 32", len(challenge.Challenge))
	}
	if challenge.UserID != "user-1" {
		t.Errorf("user ID = %q, want 'user-1'", challenge.UserID)
	}

	// Empty user ID should fail.
	_, err = srv.BeginRegistration(ctx, "")
	if err == nil {
		t.Error("expected error for empty user ID")
	}
}

func TestFinishRegistrationWithMock(t *testing.T) {
	db := testutil.TestDB(t)
	srv := webauthn.New(webauthn.Config{
		RPID:     "butler.example.com",
		RPOrigin: "https://butler.example.com",
		RPName:   "AI Butler",
	}, db.Conn())

	ctx := context.Background()

	// Generate a mock P-256 keypair.
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubKeyDER, err := webauthn.MarshalPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	// Begin registration.
	challenge, err := srv.BeginRegistration(ctx, "user-1")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}

	// Finish registration with the mock public key.
	cred, err := srv.FinishRegistration(ctx, "user-1", challenge, pubKeyDER)
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	if cred.UserID != "user-1" {
		t.Errorf("credential user ID = %q, want 'user-1'", cred.UserID)
	}
	if len(cred.ID) == 0 {
		t.Error("credential ID is empty")
	}
	if cred.SignCount != 0 {
		t.Errorf("sign count = %d, want 0", cred.SignCount)
	}
}

func TestBeginAuthentication(t *testing.T) {
	db := testutil.TestDB(t)
	srv := webauthn.New(webauthn.Config{
		RPID:     "butler.example.com",
		RPOrigin: "https://butler.example.com",
		RPName:   "AI Butler",
	}, db.Conn())

	ctx := context.Background()

	// Register a credential first.
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubKeyDER, _ := webauthn.MarshalPublicKey(&privKey.PublicKey)
	challenge, _ := srv.BeginRegistration(ctx, "user-1")
	_, err := srv.FinishRegistration(ctx, "user-1", challenge, pubKeyDER)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Begin authentication should succeed.
	authChallenge, err := srv.BeginAuthentication(ctx, "user-1")
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}
	if len(authChallenge.Challenge) != 32 {
		t.Errorf("challenge length = %d, want 32", len(authChallenge.Challenge))
	}
	if len(authChallenge.CredentialIDs) != 1 {
		t.Errorf("credential IDs count = %d, want 1", len(authChallenge.CredentialIDs))
	}

	// Begin authentication for unknown user should fail.
	_, err = srv.BeginAuthentication(ctx, "unknown-user")
	if err == nil {
		t.Error("expected error for unknown user")
	}
}

func TestFinishAuthenticationWithMock(t *testing.T) {
	db := testutil.TestDB(t)
	cfg := webauthn.Config{
		RPID:     "butler.example.com",
		RPOrigin: "https://butler.example.com",
		RPName:   "AI Butler",
	}
	srv := webauthn.New(cfg, db.Conn())

	ctx := context.Background()

	// Register a credential.
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubKeyDER, _ := webauthn.MarshalPublicKey(&privKey.PublicKey)
	regChallenge, _ := srv.BeginRegistration(ctx, "user-1")
	cred, err := srv.FinishRegistration(ctx, "user-1", regChallenge, pubKeyDER)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Begin authentication.
	authChallenge, err := srv.BeginAuthentication(ctx, "user-1")
	if err != nil {
		t.Fatalf("begin auth: %v", err)
	}

	// Create a signed assertion using the test helper.
	response, err := webauthn.SignForTest(privKey, cfg.RPID, authChallenge.Challenge, cred.ID)
	if err != nil {
		t.Fatalf("sign for test: %v", err)
	}

	// Finish authentication.
	if err := srv.FinishAuthentication(ctx, "user-1", authChallenge, response); err != nil {
		t.Fatalf("finish auth: %v", err)
	}
}

func TestCredentialStorageRoundTrip(t *testing.T) {
	db := testutil.TestDB(t)
	srv := webauthn.New(webauthn.Config{
		RPID:     "butler.example.com",
		RPOrigin: "https://butler.example.com",
		RPName:   "AI Butler",
	}, db.Conn())

	ctx := context.Background()

	// Register two credentials for the same user.
	for i := 0; i < 2; i++ {
		privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pubKeyDER, _ := webauthn.MarshalPublicKey(&privKey.PublicKey)
		challenge, _ := srv.BeginRegistration(ctx, "user-1")
		if _, err := srv.FinishRegistration(ctx, "user-1", challenge, pubKeyDER); err != nil {
			t.Fatalf("register credential %d: %v", i, err)
		}
	}

	// Retrieve credentials.
	creds, err := srv.GetCredentials(ctx, "user-1")
	if err != nil {
		t.Fatalf("get credentials: %v", err)
	}
	if len(creds) != 2 {
		t.Errorf("credentials count = %d, want 2", len(creds))
	}
	for _, c := range creds {
		if c.UserID != "user-1" {
			t.Errorf("credential user ID = %q, want 'user-1'", c.UserID)
		}
		if len(c.PublicKey) == 0 {
			t.Error("credential public key is empty")
		}
	}

	// No credentials for unknown user.
	creds, err = srv.GetCredentials(ctx, "unknown")
	if err != nil {
		t.Fatalf("get credentials unknown: %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected 0 credentials for unknown user, got %d", len(creds))
	}
}
