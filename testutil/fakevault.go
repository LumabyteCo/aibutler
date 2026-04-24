package testutil

import (
	"context"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/vault"
)

// FakeVault is an in-memory vault for testing.
type FakeVault struct {
	mu    sync.RWMutex
	creds map[string]vault.Credential
}

// NewFakeVault creates an empty in-memory vault.
func NewFakeVault() *FakeVault {
	return &FakeVault{creds: make(map[string]vault.Credential)}
}

func (v *FakeVault) Store(_ context.Context, cred vault.Credential) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.creds[cred.Key] = cred
	return nil
}

func (v *FakeVault) Get(_ context.Context, key string) (vault.Credential, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	cred, ok := v.creds[key]
	if !ok {
		return vault.Credential{}, vault.ErrNotFound
	}
	return cred, nil
}

func (v *FakeVault) Delete(_ context.Context, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.creds, key)
	return nil
}

func (v *FakeVault) List(_ context.Context) ([]string, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keys := make([]string, 0, len(v.creds))
	for k := range v.creds {
		keys = append(keys, k)
	}
	return keys, nil
}

func (v *FakeVault) Has(_ context.Context, key string) (bool, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, ok := v.creds[key]
	return ok, nil
}

func (v *FakeVault) HealthCheck(_ context.Context) error {
	return nil
}
