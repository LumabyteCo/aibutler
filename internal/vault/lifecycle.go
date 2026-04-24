package vault

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// redactKey returns a redacted version of a key name for safe logging.
// Shows first 4 and last 4 characters, e.g. "anth..._key".
func redactKey(key string) string {
	if len(key) <= 10 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// RefreshFunc is the function signature for performing an OAuth2 token refresh.
// Provided by the proxy package's TokenRefresher.
type RefreshFunc func(ctx context.Context, cred Credential, svc ServiceEntry) (Credential, error)

// TokenLifecycleManager monitors stored OAuth tokens and refreshes them before expiry.
type TokenLifecycleManager struct {
	vault         Vault
	registry      *ServiceRegistry
	refreshFn     RefreshFunc
	scanInterval  time.Duration
	refreshBuffer time.Duration
	maxRetries    int
	clock         func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// LifecycleOption configures the TokenLifecycleManager.
type LifecycleOption func(*TokenLifecycleManager)

// WithScanInterval sets how often the manager checks for expiring tokens.
func WithScanInterval(d time.Duration) LifecycleOption {
	return func(m *TokenLifecycleManager) { m.scanInterval = d }
}

// WithRefreshBuffer sets how far before expiry a refresh is attempted.
func WithRefreshBuffer(d time.Duration) LifecycleOption {
	return func(m *TokenLifecycleManager) { m.refreshBuffer = d }
}

// WithLifecycleClock overrides the clock (for testing).
func WithLifecycleClock(fn func() time.Time) LifecycleOption {
	return func(m *TokenLifecycleManager) { m.clock = fn }
}

// WithRefreshFunc sets the token refresh implementation.
func WithRefreshFunc(fn RefreshFunc) LifecycleOption {
	return func(m *TokenLifecycleManager) { m.refreshFn = fn }
}

// NewTokenLifecycleManager creates a lifecycle manager.
func NewTokenLifecycleManager(v Vault, reg *ServiceRegistry, opts ...LifecycleOption) *TokenLifecycleManager {
	m := &TokenLifecycleManager{
		vault:         v,
		registry:      reg,
		scanInterval:  15 * time.Minute,
		refreshBuffer: 10 * time.Minute,
		maxRetries:    3,
		clock:         time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start begins the background scan loop. Safe to call once.
func (m *TokenLifecycleManager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return
	}
	m.running = true
	ctx, m.cancel = context.WithCancel(ctx)
	go m.loop(ctx)
}

// Stop halts the background loop.
func (m *TokenLifecycleManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.running = false
	}
}

func (m *TokenLifecycleManager) loop(ctx context.Context) {
	ticker := time.NewTicker(m.scanInterval)
	defer ticker.Stop()

	// Run once immediately on start.
	m.scan(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scan(ctx)
		}
	}
}

func (m *TokenLifecycleManager) scan(ctx context.Context) {
	keys, err := m.vault.List(ctx)
	if err != nil {
		log.Printf("lifecycle: list credentials: %v", err)
		return
	}

	now := m.clock()
	for _, key := range keys {
		cred, err := m.vault.Get(ctx, key)
		if err != nil {
			continue
		}
		// Only check OAuth2 tokens with an expiry.
		if cred.Type != CredOAuth2 || cred.ExpiresAt == nil {
			continue
		}
		// Refresh if within the buffer window.
		if cred.ExpiresAt.Sub(now) <= m.refreshBuffer {
			if err := m.refreshToken(ctx, cred); err != nil {
				log.Printf("lifecycle: refresh %s: %v", redactKey(key), err)
			}
		}
	}
}

// refreshToken attempts to refresh an OAuth2 token with retries and exponential backoff.
func (m *TokenLifecycleManager) refreshToken(ctx context.Context, cred Credential) error {
	if m.refreshFn == nil {
		log.Printf("lifecycle: no refresh function configured for %s", redactKey(cred.Key))
		return nil
	}

	// Look up service entry for this credential.
	svc := m.registry.FindByCredentialKey(cred.Key)
	if svc == nil {
		return fmt.Errorf("lifecycle: no service entry for credential %s", redactKey(cred.Key))
	}

	_, err := m.refreshFn(ctx, cred, *svc)
	return err
}
