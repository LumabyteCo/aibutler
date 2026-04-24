package vault_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/vault"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestFakeVaultRoundTrip(t *testing.T) {
	v := testutil.NewFakeVault()
	ctx := context.Background()

	cred := vault.Credential{
		Key:   "openai",
		Type:  vault.CredAPIKey,
		Value: []byte("sk-test-123"),
	}

	if err := v.Store(ctx, cred); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := v.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got.Value, cred.Value) {
		t.Errorf("value = %q, want %q", got.Value, cred.Value)
	}

	has, err := v.Has(ctx, "openai")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has {
		t.Error("has = false, want true")
	}

	keys, err := v.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0] != "openai" {
		t.Errorf("list = %v, want [openai]", keys)
	}

	if err := v.Delete(ctx, "openai"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = v.Get(ctx, "openai")
	if err != vault.ErrNotFound {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestFileVaultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(vault.Config{
		VaultDir:   dir,
		Passphrase: "test-passphrase-for-file-vault",
		ForceFile:  true,
	})
	if err != nil {
		t.Fatalf("new file vault: %v", err)
	}

	ctx := context.Background()
	cred := vault.Credential{
		Key:   "anthropic",
		Type:  vault.CredAPIKey,
		Value: []byte("sk-ant-test-456"),
	}

	if err := v.Store(ctx, cred); err != nil {
		t.Fatalf("store: %v", err)
	}

	// Read raw file — should NOT contain plaintext.
	raw, err := os.ReadFile(dir + "/keys.age")
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if bytes.Contains(raw, []byte("sk-ant-test-456")) {
		t.Error("raw file contains plaintext — encryption not working")
	}

	// Get should recover the value.
	got, err := v.Get(ctx, "anthropic")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got.Value, cred.Value) {
		t.Errorf("value = %q, want %q", got.Value, cred.Value)
	}

	// Delete and verify gone.
	if err := v.Delete(ctx, "anthropic"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = v.Get(ctx, "anthropic")
	if err != vault.ErrNotFound {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestFileVaultWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	v1, err := vault.New(vault.Config{VaultDir: dir, Passphrase: "correct-pass", ForceFile: true})
	if err != nil {
		t.Fatalf("new v1: %v", err)
	}

	ctx := context.Background()
	if err := v1.Store(ctx, vault.Credential{Key: "secret", Value: []byte("value")}); err != nil {
		t.Fatalf("store: %v", err)
	}

	v2, err := vault.New(vault.Config{VaultDir: dir, Passphrase: "wrong-pass", ForceFile: true})
	if err != nil {
		t.Fatalf("new v2: %v", err)
	}

	_, err = v2.Get(ctx, "secret")
	if err == nil {
		t.Error("expected error with wrong passphrase")
	}
}

func TestEnvVaultRoundTrip(t *testing.T) {
	v, err := vault.New(vault.Config{ForceEnv: true})
	if err != nil {
		t.Fatalf("new env vault: %v", err)
	}

	ctx := context.Background()
	cred := vault.Credential{Key: "telegram", Type: vault.CredBotToken, Value: []byte("bot-token-xyz")}
	if err := v.Store(ctx, cred); err != nil {
		t.Fatalf("store: %v", err)
	}
	defer os.Unsetenv("AIBUTLER_TELEGRAM")

	got, err := v.Get(ctx, "telegram")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Value) != "bot-token-xyz" {
		t.Errorf("value = %q, want 'bot-token-xyz'", got.Value)
	}

	has, err := v.Has(ctx, "telegram")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has {
		t.Error("has = false, want true")
	}

	if err := v.Delete(ctx, "telegram"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = v.Get(ctx, "telegram")
	if err != vault.ErrNotFound {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestFileVaultHealthCheck(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.New(vault.Config{VaultDir: dir, Passphrase: "test", ForceFile: true})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := v.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health check: %v", err)
	}
}

func TestServiceRegistryDefaults(t *testing.T) {
	reg, err := vault.NewServiceRegistry("")
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	tests := []struct {
		domain string
		name   string
	}{
		{"api.openai.com", "openai"},
		{"api.anthropic.com", "anthropic"},
		{"api.github.com", "github"},
		{"api.telegram.org", "telegram"},
		{"api.slack.com", "slack"},
		{"api.deepgram.com", "deepgram"},
	}

	for _, tt := range tests {
		svc, ok := reg.Resolve(tt.domain)
		if !ok {
			t.Errorf("Resolve(%q): not found", tt.domain)
			continue
		}
		if svc.Name != tt.name {
			t.Errorf("Resolve(%q).Name = %q, want %q", tt.domain, svc.Name, tt.name)
		}
	}

	// Verify total count.
	svcs := reg.Services()
	if len(svcs) < 18 {
		t.Errorf("expected >= 18 services, got %d", len(svcs))
	}
}

func TestServiceRegistryUnknownDomain(t *testing.T) {
	reg, err := vault.NewServiceRegistry("")
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	_, ok := reg.Resolve("unknown.example.com")
	if ok {
		t.Error("expected not found for unknown domain")
	}
}

func TestZeroBytes(t *testing.T) {
	secret := []byte("super-secret-key")
	vault.ZeroBytes(secret)
	for i, b := range secret {
		if b != 0 {
			t.Errorf("byte[%d] = %d, want 0", i, b)
		}
	}
}

func TestLifecycleManagerStartStop(t *testing.T) {
	v := testutil.NewFakeVault()
	reg, _ := vault.NewServiceRegistry("")

	// Store an OAuth2 credential that expires soon.
	exp := time.Now().Add(5 * time.Minute)
	ctx := context.Background()
	_ = v.Store(ctx, vault.Credential{
		Key:       "github",
		Type:      vault.CredOAuth2,
		Value:     []byte("access-token"),
		ExpiresAt: &exp,
	})

	mgr := vault.NewTokenLifecycleManager(v, reg,
		vault.WithScanInterval(50*time.Millisecond),
		vault.WithRefreshBuffer(10*time.Minute),
	)
	mgr.Start(ctx)
	// Give it a moment to run at least one scan.
	time.Sleep(100 * time.Millisecond)
	mgr.Stop()
	// No crash = pass. Full refresh testing happens at higher layers.
}

func TestFileVaultForceFileRequiresPassphrase(t *testing.T) {
	_, err := vault.New(vault.Config{ForceFile: true})
	if err == nil {
		t.Error("expected error when ForceFile without passphrase")
	}
}
