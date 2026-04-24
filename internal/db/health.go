package db

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// HealthEncryptor handles the double-encryption layer for health data.
// The entire database is encrypted via Adiantum VFS. Health data gets
// an additional application-level encryption using ChaCha20-Poly1305.
type HealthEncryptor struct {
	key []byte // 32-byte key
}

// NewHealthEncryptor creates a new encryptor with the given 32-byte key.
func NewHealthEncryptor(key []byte) (*HealthEncryptor, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("health encryptor: key must be %d bytes, got %d",
			chacha20poly1305.KeySize, len(key))
	}
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	return &HealthEncryptor{key: keyCopy}, nil
}

// Encrypt encrypts plaintext for storage in health tables.
func (h *HealthEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(h.key)
	if err != nil {
		return nil, fmt.Errorf("health encrypt: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("health encrypt: nonce: %w", err)
	}

	// nonce is prepended to the ciphertext
	ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts a value read from health tables.
func (h *HealthEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(h.key)
	if err != nil {
		return nil, fmt.Errorf("health decrypt: %w", err)
	}

	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("health decrypt: ciphertext too short")
	}

	nonce := ciphertext[:aead.NonceSize()]
	encrypted := ciphertext[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("health decrypt: %w", err)
	}
	return plaintext, nil
}
