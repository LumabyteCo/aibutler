package vault

import (
	"context"
	"errors"
	"time"
)

// CredentialType identifies the kind of credential.
type CredentialType string

const (
	CredAPIKey         CredentialType = "api_key"
	CredOAuth2         CredentialType = "oauth2"
	CredBotToken       CredentialType = "bot_token"
	CredAppPassword    CredentialType = "app_password"
	CredPlatformToken  CredentialType = "platform_token"
	CredWebhookSecret  CredentialType = "webhook_secret"
	CredDeviceIdentity CredentialType = "device_identity"
	CredSessionKey     CredentialType = "session_key"
	CredHealthKey      CredentialType = "health_key"
	CredIoTPIN         CredentialType = "iot_pin"
)

// Credential represents a stored credential.
type Credential struct {
	Key          string            // Service key: "openai", "telegram", "gmail.access"
	Type         CredentialType
	Value        []byte            // The secret material
	ExpiresAt    *time.Time        // nil = never expires
	RefreshToken []byte            // For OAuth2 only
	Metadata     map[string]string // Optional metadata (scopes, token URL, etc.)
}

// ErrNotFound is returned when a credential key does not exist.
var ErrNotFound = errors.New("vault: credential not found")

// ErrDecryptionFailed is returned when decryption fails (wrong key).
var ErrDecryptionFailed = errors.New("vault: decryption failed")

// Vault is the interface for credential storage.
type Vault interface {
	Store(ctx context.Context, cred Credential) error
	Get(ctx context.Context, key string) (Credential, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]string, error)
	Has(ctx context.Context, key string) (bool, error)
	HealthCheck(ctx context.Context) error
}

// Config holds vault initialization parameters.
type Config struct {
	VaultDir   string // Path to ~/.aibutler/vault/
	Passphrase string // For age file encryption (if keychain unavailable)
	ForceFile  bool   // Force file backend even if keychain available (testing)
	ForceEnv   bool   // Force env var backend (CI/containers)
}

// New creates a Vault with automatic backend selection.
// Priority: env (if ForceEnv) → file (if ForceFile or passphrase provided) → keychain → error.
func New(cfg Config) (Vault, error) {
	if cfg.ForceEnv {
		return newEnvVault(), nil
	}
	if cfg.ForceFile {
		if cfg.Passphrase == "" {
			return nil, errors.New("vault: file backend requires passphrase")
		}
		return newFileVault(cfg.VaultDir, cfg.Passphrase)
	}
	// Try keychain first.
	kc := newKeychainVault()
	if err := kc.HealthCheck(context.Background()); err == nil {
		return kc, nil
	}
	// Fall back to file.
	if cfg.Passphrase != "" && cfg.VaultDir != "" {
		return newFileVault(cfg.VaultDir, cfg.Passphrase)
	}
	// Last resort: env.
	return newEnvVault(), nil
}

// ZeroBytes overwrites a byte slice with zeros.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
