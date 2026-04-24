package db_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/db"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestHealthEncryptorRoundTrip(t *testing.T) {
	key := []byte("test-health-encryption-key-32b!!")
	enc, err := db.NewHealthEncryptor(key)
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}

	plaintext := []byte("75.5")

	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Ciphertext should not contain plaintext
	if bytes.Contains(ciphertext, plaintext) {
		t.Error("ciphertext contains plaintext")
	}

	// Decrypt should recover original
	got, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("decrypt = %q, want %q", got, plaintext)
	}
}

func TestHealthEncryptorWrongKey(t *testing.T) {
	key1 := []byte("test-health-encryption-key-32b!!")
	key2 := []byte("wrong-key-for-decryption-32byte!")

	enc1, _ := db.NewHealthEncryptor(key1)
	enc2, _ := db.NewHealthEncryptor(key2)

	ciphertext, err := enc1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = enc2.Decrypt(ciphertext)
	if err == nil {
		t.Error("expected decryption error with wrong key")
	}
}

func TestHealthEncryptorInvalidKeySize(t *testing.T) {
	_, err := db.NewHealthEncryptor([]byte("too-short"))
	if err == nil {
		t.Error("expected error for invalid key size")
	}
}

func TestHealthDataEncryptedAtRest(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	key := []byte("test-health-encryption-key-32b!!")
	enc, _ := db.NewHealthEncryptor(key)

	// Encrypt and store
	value := []byte("75.5")
	encrypted, _ := enc.Encrypt(value)

	_, err := conn.ExecContext(ctx,
		`INSERT INTO user_health (metric, value, unit) VALUES ('weight', ?, 'kg')`,
		encrypted)
	if err != nil {
		t.Fatalf("insert health: %v", err)
	}

	// Read raw bytes — should NOT be readable
	var rawValue []byte
	err = conn.QueryRowContext(ctx, "SELECT value FROM user_health LIMIT 1").Scan(&rawValue)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if bytes.Contains(rawValue, []byte("75.5")) {
		t.Error("raw value contains plaintext — encryption not working")
	}

	// Decrypt — should recover
	decrypted, err := enc.Decrypt(rawValue)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != "75.5" {
		t.Errorf("decrypted = %q, want '75.5'", decrypted)
	}
}
