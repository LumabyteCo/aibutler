package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/crypto/nacl/secretbox"
)

// CreateBackup copies the SQLite database to a backup file.
func CreateBackup(db *sql.DB, destPath string) error {
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return fmt.Errorf("backup: mkdir: %w", err)
	}

	// Use SQLite's VACUUM INTO for a clean, consistent backup.
	execCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := db.ExecContext(execCtx, "VACUUM INTO ?", destPath)
	if err != nil {
		return fmt.Errorf("backup: vacuum into: %w", err)
	}
	return nil
}

// RotateBackups keeps only the newest N backups in a directory.
func RotateBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("backup: readdir: %w", err)
	}

	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e)
		}
	}

	if len(files) <= keep {
		return nil
	}

	// Sort by modification time (newest first).
	sort.Slice(files, func(i, j int) bool {
		fi, _ := files[i].Info()
		fj, _ := files[j].Info()
		return fi.ModTime().After(fj.ModTime())
	})

	// Remove oldest.
	for _, f := range files[keep:] {
		os.Remove(filepath.Join(dir, f.Name()))
	}
	return nil
}

const (
	encKeySize   = 32
	encNonceSize = 24
)

// EncryptBackup encrypts a backup file in-place using NaCl secretbox.
// The key must be 32 bytes. The output format is: nonce (24 bytes) + ciphertext.
func EncryptBackup(path string, key []byte) error {
	if len(key) != encKeySize {
		return fmt.Errorf("backup: encryption key must be %d bytes", encKeySize)
	}

	plaintext, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backup: read for encryption: %w", err)
	}

	var nonce [encNonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return fmt.Errorf("backup: generate nonce: %w", err)
	}

	var k [encKeySize]byte
	copy(k[:], key)

	ciphertext := secretbox.Seal(nonce[:], plaintext, &nonce, &k)

	if err := os.WriteFile(path, ciphertext, 0600); err != nil {
		return fmt.Errorf("backup: write encrypted: %w", err)
	}
	return nil
}

// DecryptBackup decrypts an encrypted backup file in-place.
func DecryptBackup(path string, key []byte) error {
	if len(key) != encKeySize {
		return fmt.Errorf("backup: decryption key must be %d bytes", encKeySize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("backup: read for decryption: %w", err)
	}

	if len(data) < encNonceSize {
		return errors.New("backup: encrypted file too short")
	}

	var nonce [encNonceSize]byte
	copy(nonce[:], data[:encNonceSize])

	var k [encKeySize]byte
	copy(k[:], key)

	plaintext, ok := secretbox.Open(nil, data[encNonceSize:], &nonce, &k)
	if !ok {
		return errors.New("backup: decryption failed (wrong key or corrupted)")
	}

	if err := os.WriteFile(path, plaintext, 0600); err != nil {
		return fmt.Errorf("backup: write decrypted: %w", err)
	}
	return nil
}

// Export creates an encrypted backup bundle. If key is nil, the backup is plaintext.
func Export(db *sql.DB, destPath string, key []byte) error {
	if err := CreateBackup(db, destPath); err != nil {
		return err
	}
	if len(key) == encKeySize {
		return EncryptBackup(destPath, key)
	}
	return nil
}

// Import restores a database from a backup file. If key is non-nil, decrypts first.
func Import(srcPath, destPath string, key []byte) error {
	// Copy source to destination first.
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("backup.import: open source: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("backup.import: create dest: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("backup.import: copy: %w", err)
	}
	dst.Close()

	// Decrypt in-place if key provided.
	if len(key) == encKeySize {
		return DecryptBackup(destPath, key)
	}
	return nil
}

// IntegrityCheck runs SQLite integrity check.
func IntegrityCheck(db *sql.DB) error {
	var result string
	err := db.QueryRow("PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("backup: integrity check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("backup: integrity check failed: %s", result)
	}
	return nil
}

// BackupFilename generates a timestamped backup filename.
func BackupFilename() string {
	return fmt.Sprintf("aibutler-backup-%s.db", time.Now().UTC().Format("20060102-150405"))
}
