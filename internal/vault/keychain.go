package vault

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zalando/go-keyring"
)

const keychainService = "aibutler"

// keychainVault implements Vault using the OS keychain (macOS Keychain, libsecret, WinCred).
type keychainVault struct{}

func newKeychainVault() *keychainVault {
	return &keychainVault{}
}

func (v *keychainVault) Store(_ context.Context, cred Credential) error {
	data, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("vault: marshal credential: %w", err)
	}
	if err := keyring.Set(keychainService, cred.Key, string(data)); err != nil {
		return fmt.Errorf("vault: keychain set: %w", err)
	}
	return v.updateKeyIndex(cred.Key, true)
}

func (v *keychainVault) Get(_ context.Context, key string) (Credential, error) {
	data, err := keyring.Get(keychainService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return Credential{}, ErrNotFound
		}
		return Credential{}, fmt.Errorf("vault: keychain get: %w", err)
	}
	var cred Credential
	if err := json.Unmarshal([]byte(data), &cred); err != nil {
		return Credential{}, fmt.Errorf("vault: unmarshal credential: %w", err)
	}
	return cred, nil
}

func (v *keychainVault) Delete(_ context.Context, key string) error {
	err := keyring.Delete(keychainService, key)
	if err != nil && err != keyring.ErrNotFound {
		return fmt.Errorf("vault: keychain delete: %w", err)
	}
	return v.updateKeyIndex(key, false)
}

// List is not natively supported by most keychains; we maintain a key index.
func (v *keychainVault) List(_ context.Context) ([]string, error) {
	data, err := keyring.Get(keychainService, "_key_index")
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("vault: keychain list: %w", err)
	}
	var keys []string
	if err := json.Unmarshal([]byte(data), &keys); err != nil {
		return nil, fmt.Errorf("vault: unmarshal key index: %w", err)
	}
	return keys, nil
}

func (v *keychainVault) Has(_ context.Context, key string) (bool, error) {
	_, err := keyring.Get(keychainService, key)
	if err == keyring.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("vault: keychain has: %w", err)
	}
	return true, nil
}

func (v *keychainVault) HealthCheck(_ context.Context) error {
	// Try a no-op read to verify keychain access.
	_, err := keyring.Get(keychainService, "_health_check_probe")
	if err == keyring.ErrNotFound {
		return nil // Expected — keychain is accessible.
	}
	return err
}

// updateKeyIndex adds or removes a key from the stored key index.
func (v *keychainVault) updateKeyIndex(key string, add bool) error {
	data, err := keyring.Get(keychainService, "_key_index")
	var keys []string
	if err == nil {
		_ = json.Unmarshal([]byte(data), &keys)
	}

	if add {
		for _, k := range keys {
			if k == key {
				return nil
			}
		}
		keys = append(keys, key)
	} else {
		filtered := keys[:0]
		for _, k := range keys {
			if k != key {
				filtered = append(filtered, k)
			}
		}
		keys = filtered
	}

	idx, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return keyring.Set(keychainService, "_key_index", string(idx))
}
