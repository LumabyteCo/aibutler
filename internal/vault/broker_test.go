package vault

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// fakeVault is a minimal in-memory Vault for tests.
type fakeVault struct {
	mu    sync.Mutex
	creds map[string]Credential
}

func newFakeVault() *fakeVault { return &fakeVault{creds: map[string]Credential{}} }

func (f *fakeVault) Store(_ context.Context, c Credential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creds[c.Key] = c
	return nil
}
func (f *fakeVault) Get(_ context.Context, key string) (Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.creds[key]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return c, nil
}
func (f *fakeVault) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.creds, key)
	return nil
}
func (f *fakeVault) List(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.creds))
	for k := range f.creds {
		out = append(out, k)
	}
	return out, nil
}
func (f *fakeVault) Has(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.creds[key]
	return ok, nil
}
func (f *fakeVault) HealthCheck(_ context.Context) error { return nil }

// fakeAuditor records audit entries for inspection.
type fakeAuditor struct {
	mu      sync.Mutex
	Entries []capability.AuditEntry
}

func (a *fakeAuditor) LogAccess(_ context.Context, e capability.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Entries = append(a.Entries, e)
	return nil
}

func newTestBroker(t *testing.T, rules BrokerRules) (*Broker, *fakeVault, *fakeAuditor) {
	t.Helper()
	v := newFakeVault()
	a := &fakeAuditor{}
	return NewBroker(v, rules, a), v, a
}

// --- Decision tests ---

func TestRequest_AutoApproved_Granted(t *testing.T) {
	b, v, a := newTestBroker(t, BrokerRules{AutoApprovedKeys: []string{"github_token"}})
	_ = v.Store(context.Background(), Credential{Key: "github_token", Value: []byte("ghp_xyz")})

	r := b.Request(context.Background(), "github_token", "create PR for issue #42", "agent-1")
	if !r.Granted {
		t.Fatalf("expected granted=true, got %+v", r)
	}
	if r.Policy != PolicyAuto {
		t.Errorf("Policy = %q, want %q", r.Policy, PolicyAuto)
	}
	if string(r.Value) != "ghp_xyz" {
		t.Errorf("Value = %q, want ghp_xyz", string(r.Value))
	}

	if len(a.Entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(a.Entries))
	}
	if a.Entries[0].Status != "granted" {
		t.Errorf("audit Status = %q, want granted", a.Entries[0].Status)
	}
	if a.Entries[0].CredentialKey != "github_token" {
		t.Errorf("audit CredentialKey = %q, want github_token", a.Entries[0].CredentialKey)
	}
	if a.Entries[0].AgentID != "agent-1" {
		t.Errorf("audit AgentID = %q, want agent-1", a.Entries[0].AgentID)
	}
}

func TestRequest_AutoApproved_CaseInsensitive(t *testing.T) {
	b, v, _ := newTestBroker(t, BrokerRules{AutoApprovedKeys: []string{"GitHub_Token"}})
	_ = v.Store(context.Background(), Credential{Key: "github_token", Value: []byte("ghp_xyz")})

	r := b.Request(context.Background(), "github_token", "ci", "agent-1")
	if !r.Granted {
		t.Errorf("expected case-insensitive match, got %+v", r)
	}
}

func TestRequest_NotApproved_DeniedWithGuidance(t *testing.T) {
	b, v, a := newTestBroker(t, BrokerRules{}) // no auto-approval
	_ = v.Store(context.Background(), Credential{Key: "github_token", Value: []byte("ghp_xyz")})

	r := b.Request(context.Background(), "github_token", "?", "agent-1")
	if r.Granted {
		t.Fatalf("expected granted=false, got %+v", r)
	}
	if r.Policy != PolicyPrompt {
		t.Errorf("Policy = %q, want %q (default-deny falls into prompt path)", r.Policy, PolicyPrompt)
	}
	if !strings.Contains(r.Reason, "auto_approved_keys") && !strings.Contains(r.Reason, "AutoApprovedKeys") {
		t.Errorf("Reason should suggest the config-based remediation, got %q", r.Reason)
	}
	if len(r.Value) != 0 {
		t.Error("Value must be empty when not granted")
	}
	if len(a.Entries) != 1 || a.Entries[0].Status != "denied" {
		t.Errorf("expected one denied audit entry, got %+v", a.Entries)
	}
}

func TestRequest_DenyList_AlwaysWins(t *testing.T) {
	b, v, _ := newTestBroker(t, BrokerRules{
		AutoApprovedKeys: []string{"github_token"},
		DeniedKeys:       []string{"github_token"},
	})
	_ = v.Store(context.Background(), Credential{Key: "github_token", Value: []byte("ghp_xyz")})

	r := b.Request(context.Background(), "github_token", "?", "agent-1")
	if r.Granted {
		t.Fatalf("deny list should win over auto-approval, got granted=true: %+v", r)
	}
	if r.Policy != PolicyDeny {
		t.Errorf("Policy = %q, want %q", r.Policy, PolicyDeny)
	}
	if !strings.Contains(r.Reason, "deny list") {
		t.Errorf("Reason should mention deny list, got %q", r.Reason)
	}
}

func TestRequest_AutoApproved_VaultMiss_Denies(t *testing.T) {
	b, _, a := newTestBroker(t, BrokerRules{AutoApprovedKeys: []string{"missing_key"}})
	// Don't store anything in the vault.

	r := b.Request(context.Background(), "missing_key", "?", "agent-1")
	if r.Granted {
		t.Fatalf("expected granted=false when vault has no credential, got %+v", r)
	}
	if r.Policy != PolicyDeny {
		t.Errorf("Policy = %q, want %q (vault-miss is a denial, not a prompt)", r.Policy, PolicyDeny)
	}
	if !strings.Contains(r.Reason, "not stored") {
		t.Errorf("Reason should explain vault miss, got %q", r.Reason)
	}
	if len(a.Entries) != 1 || a.Entries[0].Status != "denied" {
		t.Errorf("expected denied audit entry, got %+v", a.Entries)
	}
}

func TestRequest_EmptyKey_Denies(t *testing.T) {
	b, _, _ := newTestBroker(t, BrokerRules{})
	r := b.Request(context.Background(), "  ", "?", "agent-1")
	if r.Granted {
		t.Fatal("expected denial for empty key")
	}
	if !strings.Contains(r.Reason, "key is required") {
		t.Errorf("Reason should mention required key, got %q", r.Reason)
	}
}

func TestRequest_NilAuditor_NoCrash(t *testing.T) {
	v := newFakeVault()
	_ = v.Store(context.Background(), Credential{Key: "k", Value: []byte("v")})
	b := NewBroker(v, BrokerRules{AutoApprovedKeys: []string{"k"}}, nil)

	r := b.Request(context.Background(), "k", "?", "")
	if !r.Granted {
		t.Fatalf("expected grant with nil auditor, got %+v", r)
	}
}

// --- Tool registration tests ---

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestRegisterRequestTool(t *testing.T) {
	reg := newMockRegistry()
	b, v, _ := newTestBroker(t, BrokerRules{AutoApprovedKeys: []string{"github_token"}})
	_ = v.Store(context.Background(), Credential{Key: "github_token", Value: []byte("ghp_xyz")})

	RegisterRequestTool(reg, b, func(_ context.Context) string { return "test-agent" })
	tool := reg.exec["vault.request"]
	if tool == nil {
		t.Fatal("vault.request not registered")
	}

	out, err := tool(context.Background(), `{"key":"github_token","purpose":"create PR"}`)
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var resp brokerToolResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output not valid JSON: %v\noutput: %s", err, out)
	}
	if !resp.Granted {
		t.Errorf("expected granted=true, got %+v", resp)
	}
	if resp.Value != "ghp_xyz" {
		t.Errorf("Value = %q, want ghp_xyz", resp.Value)
	}
}

func TestRegisteredTool_DenialOmitsValue(t *testing.T) {
	reg := newMockRegistry()
	b, _, _ := newTestBroker(t, BrokerRules{}) // nothing approved
	RegisterRequestTool(reg, b, nil)

	out, err := reg.exec["vault.request"](context.Background(), `{"key":"x","purpose":"y"}`)
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var resp brokerToolResponse
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Granted {
		t.Error("expected granted=false")
	}
	if resp.Value != "" {
		t.Errorf("Value must be empty on denial, got %q", resp.Value)
	}
}

func TestRegisteredTool_MissingPurpose(t *testing.T) {
	reg := newMockRegistry()
	b, _, _ := newTestBroker(t, BrokerRules{})
	RegisterRequestTool(reg, b, nil)

	_, err := reg.exec["vault.request"](context.Background(), `{"key":"x"}`)
	if err == nil {
		t.Error("expected error when purpose is missing")
	}
}

func TestRegisteredTool_MissingKey(t *testing.T) {
	reg := newMockRegistry()
	b, _, _ := newTestBroker(t, BrokerRules{})
	RegisterRequestTool(reg, b, nil)

	_, err := reg.exec["vault.request"](context.Background(), `{"purpose":"y"}`)
	if err == nil {
		t.Error("expected error when key is missing")
	}
}

func TestRegisteredTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	b, _, _ := newTestBroker(t, BrokerRules{})
	RegisterRequestTool(reg, b, nil)

	_, err := reg.exec["vault.request"](context.Background(), `not json`)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
