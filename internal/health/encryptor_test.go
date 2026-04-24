package health_test

import (
	"bytes"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/health"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := health.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	enc, err := health.NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("weight: 75.5 kg")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Ciphertext should differ from plaintext.
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext equals plaintext")
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := health.GenerateKey()
	key2, _ := health.GenerateKey()

	enc1, _ := health.NewEncryptor(key1)
	enc2, _ := health.NewEncryptor(key2)

	ciphertext, _ := enc1.Encrypt([]byte("secret data"))

	_, err := enc2.Decrypt(ciphertext)
	if err == nil {
		t.Error("expected decryption error with wrong key")
	}
}

func TestEncryptorInvalidKeySize(t *testing.T) {
	_, err := health.NewEncryptor([]byte("short"))
	if err == nil {
		t.Error("expected error for short key")
	}
}

func TestDecryptTooShort(t *testing.T) {
	key, _ := health.GenerateKey()
	enc, _ := health.NewEncryptor(key)

	_, err := enc.Decrypt([]byte("short"))
	if err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}

func TestEncryptDifferentNonces(t *testing.T) {
	key, _ := health.GenerateKey()
	enc, _ := health.NewEncryptor(key)

	plaintext := []byte("same data")
	c1, _ := enc.Encrypt(plaintext)
	c2, _ := enc.Encrypt(plaintext)

	// Same plaintext should produce different ciphertext (different nonces).
	if bytes.Equal(c1, c2) {
		t.Error("same plaintext produced identical ciphertext")
	}
}
