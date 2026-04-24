package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"filippo.io/age"
)

// fileVault implements Vault using age-encrypted files on disk.
type fileVault struct {
	mu        sync.RWMutex
	dir       string
	identity  *age.ScryptIdentity
	recipient *age.ScryptRecipient
}

// newFileVault creates a new file-based vault.
func newFileVault(dir, passphrase string) (*fileVault, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("vault: create dir: %w", err)
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("vault: create recipient: %w", err)
	}

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("vault: create identity: %w", err)
	}

	return &fileVault{
		dir:       dir,
		identity:  identity,
		recipient: recipient,
	}, nil
}

func (v *fileVault) Store(_ context.Context, cred Credential) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	all, err := v.loadAll()
	if err != nil {
		all = make(map[string]Credential)
	}
	all[cred.Key] = cred
	return v.saveAll(all)
}

func (v *fileVault) Get(_ context.Context, key string) (Credential, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	all, err := v.loadAll()
	if err != nil {
		return Credential{}, err
	}
	cred, ok := all[key]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return cred, nil
}

func (v *fileVault) Delete(_ context.Context, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	all, err := v.loadAll()
	if err != nil {
		return err
	}
	delete(all, key)
	return v.saveAll(all)
}

func (v *fileVault) List(_ context.Context) ([]string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	all, err := v.loadAll()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	return keys, nil
}

func (v *fileVault) Has(_ context.Context, key string) (bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	all, err := v.loadAll()
	if err != nil {
		return false, err
	}
	_, ok := all[key]
	return ok, nil
}

func (v *fileVault) HealthCheck(_ context.Context) error {
	v.mu.RLock()
	defer v.mu.RUnlock()

	_, err := v.loadAll()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("vault: health check: %w", err)
	}
	return nil
}

func (v *fileVault) vaultFile() string {
	return filepath.Join(v.dir, "keys.age")
}

func (v *fileVault) loadAll() (map[string]Credential, error) {
	data, err := os.ReadFile(v.vaultFile())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]Credential), nil
		}
		return nil, fmt.Errorf("vault: read file: %w", err)
	}

	reader, err := age.Decrypt(bytes.NewReader(data), v.identity)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("vault: read decrypted: %w", err)
	}

	var all map[string]Credential
	if err := json.Unmarshal(plaintext, &all); err != nil {
		return nil, fmt.Errorf("vault: unmarshal: %w", err)
	}
	return all, nil
}

func (v *fileVault) saveAll(all map[string]Credential) error {
	plaintext, err := json.Marshal(all)
	if err != nil {
		return fmt.Errorf("vault: marshal: %w", err)
	}

	var buf bytes.Buffer
	writer, err := age.Encrypt(&buf, v.recipient)
	if err != nil {
		return fmt.Errorf("vault: encrypt: %w", err)
	}
	if _, err := writer.Write(plaintext); err != nil {
		return fmt.Errorf("vault: write encrypted: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("vault: close writer: %w", err)
	}

	if err := os.WriteFile(v.vaultFile(), buf.Bytes(), 0600); err != nil {
		return fmt.Errorf("vault: write file: %w", err)
	}
	return nil
}
