package proxy

import (
	"context"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/vault"
)

// CredentialResolver maps domains to credentials via the service registry and vault.
type CredentialResolver struct {
	registry *vault.ServiceRegistry
	vault    vault.Vault
}

// NewCredentialResolver creates a resolver.
func NewCredentialResolver(registry *vault.ServiceRegistry, v vault.Vault) *CredentialResolver {
	return &CredentialResolver{registry: registry, vault: v}
}

// Resolve looks up the credential for a given domain.
// Returns (nil, nil, nil) if the domain has no registered service.
// Returns (nil, &svc, ErrNotFound) if the service is registered but no credential is stored.
func (r *CredentialResolver) Resolve(ctx context.Context, domain string) (*vault.Credential, *vault.ServiceEntry, error) {
	svc, ok := r.registry.Resolve(domain)
	if !ok {
		return nil, nil, nil // Unknown domain, proceed without credentials.
	}

	cred, err := r.vault.Get(ctx, svc.CredentialKey)
	if err != nil {
		return nil, &svc, fmt.Errorf("proxy.resolve: credential %q not configured: %w", svc.CredentialKey, err)
	}

	return &cred, &svc, nil
}
