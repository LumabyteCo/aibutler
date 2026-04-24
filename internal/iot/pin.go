package iot

import (
	"context"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/vault"
	"golang.org/x/crypto/bcrypt"
)

const iotPINKey = "iot_pin"

// PINVerifier handles IoT PIN setting and verification.
type PINVerifier struct {
	vault vault.Vault
}

// NewPINVerifier creates a new PIN verifier.
func NewPINVerifier(v vault.Vault) *PINVerifier {
	return &PINVerifier{vault: v}
}

// SetPIN stores a bcrypt-hashed PIN in the vault.
func (p *PINVerifier) SetPIN(ctx context.Context, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("iot.pin: hash: %w", err)
	}
	return p.vault.Store(ctx, vault.Credential{
		Key:   iotPINKey,
		Type:  vault.CredIoTPIN,
		Value: hash,
	})
}

// Verify checks a PIN against the stored hash.
func (p *PINVerifier) Verify(ctx context.Context, pin string) (bool, error) {
	cred, err := p.vault.Get(ctx, iotPINKey)
	if err != nil {
		return false, fmt.Errorf("iot.pin: get: %w", err)
	}
	err = bcrypt.CompareHashAndPassword(cred.Value, []byte(pin))
	if err != nil {
		return false, nil
	}
	return true, nil
}
