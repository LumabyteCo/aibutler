package health

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/secretbox"
)

const (
	keySize   = 32
	nonceSize = 24
)

// Encryptor encrypts and decrypts health data values.
type Encryptor struct {
	key [keySize]byte
}

// NewEncryptor creates a health data encryptor from a 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("health: key must be %d bytes, got %d", keySize, len(key))
	}
	e := &Encryptor{}
	copy(e.key[:], key)
	return e, nil
}

// GenerateKey creates a random 32-byte encryption key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("health: generate key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext health data.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	var nonce [nonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("health: generate nonce: %w", err)
	}

	// Prepend nonce to the ciphertext.
	encrypted := secretbox.Seal(nonce[:], plaintext, &nonce, &e.key)
	return encrypted, nil
}

// Decrypt decrypts encrypted health data.
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < nonceSize {
		return nil, errors.New("health: ciphertext too short")
	}

	var nonce [nonceSize]byte
	copy(nonce[:], ciphertext[:nonceSize])

	plaintext, ok := secretbox.Open(nil, ciphertext[nonceSize:], &nonce, &e.key)
	if !ok {
		return nil, errors.New("health: decryption failed (wrong key or corrupted data)")
	}
	return plaintext, nil
}
