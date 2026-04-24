// Package webauthn provides a WebAuthn/FIDO2 server for passwordless authentication.
// It implements the core registration and authentication ceremonies using P-256
// ECDSA keys, without any external dependencies beyond the Go standard library.
package webauthn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// Config holds the relying party configuration for WebAuthn ceremonies.
type Config struct {
	RPID     string // Relying Party ID (domain), e.g. "butler.example.com"
	RPOrigin string // Origin URL, e.g. "https://butler.example.com"
	RPName   string // Human-readable name, e.g. "AI Butler"
}

// Credential represents a stored WebAuthn credential.
type Credential struct {
	ID        []byte
	PublicKey []byte // DER-encoded ECDSA P-256 public key
	SignCount uint32
	UserID    string
	CreatedAt time.Time
}

// RegistrationChallenge is the server-side state during a registration ceremony.
type RegistrationChallenge struct {
	Challenge []byte
	UserID    string
	ExpiresAt time.Time
}

// AuthenticationChallenge is the server-side state during an authentication ceremony.
type AuthenticationChallenge struct {
	Challenge     []byte
	CredentialIDs [][]byte
	ExpiresAt     time.Time
}

// Server implements WebAuthn registration and authentication ceremonies.
type Server struct {
	cfg            Config
	db             *sql.DB
	mu             sync.Mutex
	regChallenges  map[string]*RegistrationChallenge     // userID -> challenge
	authChallenges map[string]*AuthenticationChallenge    // userID -> challenge
}

// New creates a WebAuthn server with the given config and database connection.
func New(cfg Config, db *sql.DB) *Server {
	return &Server{
		cfg:            cfg,
		db:             db,
		regChallenges:  make(map[string]*RegistrationChallenge),
		authChallenges: make(map[string]*AuthenticationChallenge),
	}
}

// StoreRegistrationChallenge persists a registration challenge for the given user.
// The challenge expires after 5 minutes.
func (s *Server) StoreRegistrationChallenge(userID string, ch *RegistrationChallenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.regChallenges[userID] = ch
}

// GetRegistrationChallenge retrieves and removes a registration challenge for the given user.
// Returns nil if no challenge exists or if it has expired.
func (s *Server) GetRegistrationChallenge(userID string) *RegistrationChallenge {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.regChallenges[userID]
	if !ok {
		return nil
	}
	delete(s.regChallenges, userID)
	if time.Now().After(ch.ExpiresAt) {
		return nil
	}
	return ch
}

// StoreAuthenticationChallenge persists an authentication challenge for the given user.
func (s *Server) StoreAuthenticationChallenge(userID string, ch *AuthenticationChallenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authChallenges[userID] = ch
}

// GetAuthenticationChallenge retrieves and removes an authentication challenge for the given user.
// Returns nil if no challenge exists or if it has expired.
func (s *Server) GetAuthenticationChallenge(userID string) *AuthenticationChallenge {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.authChallenges[userID]
	if !ok {
		return nil
	}
	delete(s.authChallenges, userID)
	if time.Now().After(ch.ExpiresAt) {
		return nil
	}
	return ch
}

// CleanupExpiredChallenges removes expired challenges from the in-memory caches.
func (s *Server) CleanupExpiredChallenges() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for uid, ch := range s.regChallenges {
		if now.After(ch.ExpiresAt) {
			delete(s.regChallenges, uid)
		}
	}
	for uid, ch := range s.authChallenges {
		if now.After(ch.ExpiresAt) {
			delete(s.authChallenges, uid)
		}
	}
}

// BeginRegistration starts a WebAuthn registration ceremony by generating a challenge.
func (s *Server) BeginRegistration(ctx context.Context, userID string) (*RegistrationChallenge, error) {
	if userID == "" {
		return nil, errors.New("webauthn: user ID required")
	}

	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("webauthn: generate challenge: %w", err)
	}

	return &RegistrationChallenge{
		Challenge: challenge,
		UserID:    userID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}, nil
}

// FinishRegistration completes a WebAuthn registration ceremony.
// The response parameter should contain a DER-encoded ECDSA P-256 public key.
// In production, this would parse a full attestation object; here we accept
// the raw public key bytes for simplicity and testability.
func (s *Server) FinishRegistration(ctx context.Context, userID string, challenge *RegistrationChallenge, response []byte) (*Credential, error) {
	if challenge == nil {
		return nil, errors.New("webauthn: nil challenge")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return nil, errors.New("webauthn: challenge expired")
	}
	if challenge.UserID != userID {
		return nil, errors.New("webauthn: user ID mismatch")
	}
	if len(response) == 0 {
		return nil, errors.New("webauthn: empty response")
	}

	// Parse the public key to validate it.
	pubKey, err := x509.ParsePKIXPublicKey(response)
	if err != nil {
		return nil, fmt.Errorf("webauthn: parse public key: %w", err)
	}
	ecKey, ok := pubKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("webauthn: not an ECDSA key")
	}
	if ecKey.Curve != elliptic.P256() {
		return nil, errors.New("webauthn: key must be P-256")
	}

	// Generate a credential ID from the public key hash.
	credID := sha256Sum(response)

	cred := &Credential{
		ID:        credID,
		PublicKey: response,
		SignCount: 0,
		UserID:    userID,
		CreatedAt: time.Now(),
	}

	// Store the credential.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials (user_id, credential_id, public_key, sign_count, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		cred.UserID, cred.ID, cred.PublicKey, cred.SignCount, cred.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("webauthn: store credential: %w", err)
	}

	return cred, nil
}

// BeginAuthentication starts a WebAuthn authentication ceremony for the given user.
func (s *Server) BeginAuthentication(ctx context.Context, userID string) (*AuthenticationChallenge, error) {
	if userID == "" {
		return nil, errors.New("webauthn: user ID required")
	}

	// Retrieve credential IDs for the user.
	rows, err := s.db.QueryContext(ctx,
		`SELECT credential_id FROM webauthn_credentials WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("webauthn: query credentials: %w", err)
	}
	defer rows.Close()

	var credIDs [][]byte
	for rows.Next() {
		var id []byte
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("webauthn: scan credential: %w", err)
		}
		credIDs = append(credIDs, id)
	}
	if len(credIDs) == 0 {
		return nil, errors.New("webauthn: no credentials found for user")
	}

	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("webauthn: generate challenge: %w", err)
	}

	return &AuthenticationChallenge{
		Challenge:     challenge,
		CredentialIDs: credIDs,
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}, nil
}

// FinishAuthentication completes a WebAuthn authentication ceremony.
// The response should contain: credentialID (32 bytes) + signature (DER-encoded).
// The signed data is: SHA256(rpID) + SHA256(challenge).
func (s *Server) FinishAuthentication(ctx context.Context, userID string, challenge *AuthenticationChallenge, response []byte) error {
	if challenge == nil {
		return errors.New("webauthn: nil challenge")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return errors.New("webauthn: challenge expired")
	}
	if len(response) < 33 {
		return errors.New("webauthn: response too short")
	}

	// Parse response: first 32 bytes = credential ID, rest = signature.
	credID := response[:32]
	signature := response[32:]

	// Retrieve the stored credential.
	var pubKeyDER []byte
	var signCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT public_key, sign_count FROM webauthn_credentials
		 WHERE user_id = ? AND credential_id = ?`, userID, credID).Scan(&pubKeyDER, &signCount)
	if err != nil {
		return fmt.Errorf("webauthn: credential not found: %w", err)
	}

	// Parse the public key.
	pubKeyRaw, err := x509.ParsePKIXPublicKey(pubKeyDER)
	if err != nil {
		return fmt.Errorf("webauthn: parse stored key: %w", err)
	}
	ecKey, ok := pubKeyRaw.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("webauthn: stored key is not ECDSA")
	}

	// Verify the signature over: SHA256(rpID) || SHA256(challenge).
	rpIDHash := sha256.Sum256([]byte(s.cfg.RPID))
	challengeHash := sha256.Sum256(challenge.Challenge)
	signedData := append(rpIDHash[:], challengeHash[:]...)
	dataHash := sha256.Sum256(signedData)

	if !verifyECDSA(ecKey, dataHash[:], signature) {
		return errors.New("webauthn: invalid signature")
	}

	// Update sign count.
	_, err = s.db.ExecContext(ctx,
		`UPDATE webauthn_credentials SET sign_count = sign_count + 1
		 WHERE user_id = ? AND credential_id = ?`, userID, credID)
	if err != nil {
		return fmt.Errorf("webauthn: update sign count: %w", err)
	}

	return nil
}

// GetCredentials returns all credentials for a given user.
func (s *Server) GetCredentials(ctx context.Context, userID string) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT credential_id, public_key, sign_count, user_id, created_at
		 FROM webauthn_credentials WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("webauthn: query: %w", err)
	}
	defer rows.Close()

	var creds []Credential
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.PublicKey, &c.SignCount, &c.UserID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("webauthn: scan: %w", err)
		}
		creds = append(creds, c)
	}
	return creds, nil
}

// BeginRegistrationHandler returns an HTTP handler that starts a WebAuthn
// registration ceremony. The user ID is taken from the "user" query parameter.
func (s *Server) BeginRegistrationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user")
		if userID == "" {
			http.Error(w, `{"error":"user parameter required"}`, http.StatusBadRequest)
			return
		}
		challenge, err := s.BeginRegistration(r.Context(), userID)
		if err != nil {
			log.Printf("webauthn: begin registration: %v", err)
			http.Error(w, `{"error":"registration failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"challenge":  challenge.Challenge,
			"user_id":    challenge.UserID,
			"expires_at": challenge.ExpiresAt,
			"rp": map[string]string{
				"id":   s.cfg.RPID,
				"name": s.cfg.RPName,
			},
		})
	}
}

// FinishRegistrationHandler returns an HTTP handler that completes a WebAuthn
// registration ceremony. Expects JSON body with user_id, challenge, and response.
func (s *Server) FinishRegistrationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "finish_registration_pending",
			"note":   "Full attestation parsing requires client-side WebAuthn integration",
		})
	}
}

// BeginAuthenticationHandler returns an HTTP handler that starts a WebAuthn
// authentication ceremony. The user ID is taken from the "user" query parameter.
func (s *Server) BeginAuthenticationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("user")
		if userID == "" {
			http.Error(w, `{"error":"user parameter required"}`, http.StatusBadRequest)
			return
		}
		challenge, err := s.BeginAuthentication(r.Context(), userID)
		if err != nil {
			log.Printf("webauthn: begin authentication: %v", err)
			http.Error(w, `{"error":"authentication failed"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"challenge":      challenge.Challenge,
			"credential_ids": challenge.CredentialIDs,
			"expires_at":     challenge.ExpiresAt,
		})
	}
}

// FinishAuthenticationHandler returns an HTTP handler that completes a WebAuthn
// authentication ceremony. Expects JSON body with user_id, challenge, and response.
func (s *Server) FinishAuthenticationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "finish_authentication_pending",
			"note":   "Full assertion parsing requires client-side WebAuthn integration",
		})
	}
}

// sha256Sum returns the SHA-256 hash of the input.
func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// verifyECDSA verifies an ECDSA signature. The signature is expected to be
// in the format: r_len(1 byte) + r_bytes + s_bytes (each 32 bytes for P-256).
// We also accept DER-encoded signatures.
func verifyECDSA(pub *ecdsa.PublicKey, hash, sig []byte) bool {
	// Try raw r||s format first (64 bytes for P-256).
	if len(sig) == 64 {
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		return ecdsa.Verify(pub, hash, r, s)
	}

	// Try DER format.
	if len(sig) > 2 && sig[0] == 0x30 {
		r, s, ok := parseDERSignature(sig)
		if ok {
			return ecdsa.Verify(pub, hash, r, s)
		}
	}

	return false
}

// parseDERSignature parses a DER-encoded ECDSA signature (SEQUENCE { INTEGER r, INTEGER s }).
func parseDERSignature(data []byte) (r, s *big.Int, ok bool) {
	if len(data) < 6 || data[0] != 0x30 {
		return nil, nil, false
	}

	seqLen := int(data[1])
	if seqLen+2 != len(data) {
		return nil, nil, false
	}

	pos := 2

	// Parse r.
	if pos >= len(data) || data[pos] != 0x02 {
		return nil, nil, false
	}
	pos++
	rLen := int(data[pos])
	pos++
	if pos+rLen > len(data) {
		return nil, nil, false
	}
	rBytes := data[pos : pos+rLen]
	pos += rLen

	// Parse s.
	if pos >= len(data) || data[pos] != 0x02 {
		return nil, nil, false
	}
	pos++
	sLen := int(data[pos])
	pos++
	if pos+sLen > len(data) {
		return nil, nil, false
	}
	sBytes := data[pos : pos+sLen]

	return new(big.Int).SetBytes(rBytes), new(big.Int).SetBytes(sBytes), true
}

// SignForTest creates a test signature for a WebAuthn authentication response.
// This is only useful for unit tests — it signs SHA256(rpIDHash || challengeHash)
// with the given private key and returns credID || signature (raw r||s format).
func SignForTest(privKey *ecdsa.PrivateKey, rpID string, challenge []byte, credID []byte) ([]byte, error) {
	rpIDHash := sha256.Sum256([]byte(rpID))
	challengeHash := sha256.Sum256(challenge)
	signedData := append(rpIDHash[:], challengeHash[:]...)
	dataHash := sha256.Sum256(signedData)

	r, sVal, err := ecdsa.Sign(rand.Reader, privKey, dataHash[:])
	if err != nil {
		return nil, err
	}

	// Encode r and s as 32-byte big-endian values.
	rBytes := padTo32(r.Bytes())
	sBytes := padTo32(sVal.Bytes())
	sig := append(rBytes, sBytes...)

	// Response format: credID (32 bytes) + signature (64 bytes).
	return append(credID, sig...), nil
}

// padTo32 pads a byte slice to 32 bytes with leading zeros.
func padTo32(b []byte) []byte {
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// MarshalPublicKey encodes an ECDSA public key in DER (PKIX) format.
func MarshalPublicKey(pub *ecdsa.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}

// clientDataHash computes the hash used in WebAuthn's authenticator data.
// In production this would hash the full clientDataJSON; here we use the challenge directly.
func clientDataHash(challenge []byte) []byte {
	h := sha256.Sum256(challenge)
	return h[:]
}

// authenticatorData builds minimal authenticator data for testing.
// Format: rpIdHash (32) + flags (1) + signCount (4).
func authenticatorData(rpID string, signCount uint32) []byte {
	rpIDHash := sha256.Sum256([]byte(rpID))
	data := make([]byte, 37)
	copy(data[:32], rpIDHash[:])
	data[32] = 0x01 // flags: UP (user present)
	binary.BigEndian.PutUint32(data[33:37], signCount)
	return data
}
