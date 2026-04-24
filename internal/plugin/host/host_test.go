package host_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/host"
)

// --- Mock implementations ---

type mockToolCaller struct {
	result string
	err    error
	called string
}

func (m *mockToolCaller) CallTool(ctx context.Context, name, input string) (string, error) {
	m.called = name
	return m.result, m.err
}

type mockVault struct {
	data map[string][]byte
}

func (m *mockVault) Get(ctx context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

type mockConfig struct {
	data map[string]string
}

func (m *mockConfig) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

type mockLogger struct {
	entries []string
}

func (m *mockLogger) Log(pluginName, level, message string) {
	m.entries = append(m.entries, pluginName+":"+level+":"+message)
}

type mockAuditor struct {
	entries []auditEntry
}

type auditEntry struct {
	pluginID   int64
	action     string
	capability string
	status     string
}

func (m *mockAuditor) WriteAudit(ctx context.Context, pluginID int64, action, capability, status string) error {
	m.entries = append(m.entries, auditEntry{pluginID, action, capability, status})
	return nil
}

func baseDeps() *host.Deps {
	return &host.Deps{
		Caps:       []string{"tool.call"},
		PluginID:   1,
		PluginName: "test-plugin",
	}
}

// --- ToolCall tests ---

func TestToolCallSuccess(t *testing.T) {
	tc := &mockToolCaller{result: "ok"}
	deps := baseDeps()
	deps.ToolCaller = tc

	input, _ := json.Marshal(host.ToolCallRequest{Tool: "web.search", Args: `{"q":"test"}`})
	out, err := host.ExecuteToolCall(context.Background(), deps, input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var resp host.ToolCallResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Result != "ok" {
		t.Errorf("result = %q, want ok", resp.Result)
	}
	if tc.called != "web.search" {
		t.Errorf("called = %q, want web.search", tc.called)
	}
}

func TestToolCallDeniedWithoutCapability(t *testing.T) {
	deps := &host.Deps{
		Caps:       []string{}, // no tool.call cap
		PluginID:   1,
		PluginName: "test",
		Auditor:    &mockAuditor{},
	}

	input, _ := json.Marshal(host.ToolCallRequest{Tool: "shell.exec", Args: `{}`})
	out, _ := host.ExecuteToolCall(context.Background(), deps, input)

	var resp host.ToolCallResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error for denied capability")
	}
}

func TestToolCallAuditSuccess(t *testing.T) {
	auditor := &mockAuditor{}
	deps := baseDeps()
	deps.ToolCaller = &mockToolCaller{result: "r"}
	deps.Auditor = auditor

	input, _ := json.Marshal(host.ToolCallRequest{Tool: "test", Args: `{}`})
	_, _ = host.ExecuteToolCall(context.Background(), deps, input)

	if len(auditor.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(auditor.entries))
	}
	if auditor.entries[0].status != "success" {
		t.Errorf("audit status = %q, want success", auditor.entries[0].status)
	}
}

func TestToolCallAuditDenied(t *testing.T) {
	auditor := &mockAuditor{}
	deps := &host.Deps{
		Caps:    []string{}, // denied
		Auditor: auditor,
	}

	input, _ := json.Marshal(host.ToolCallRequest{Tool: "test", Args: `{}`})
	_, _ = host.ExecuteToolCall(context.Background(), deps, input)

	if len(auditor.entries) != 1 || auditor.entries[0].status != "denied" {
		t.Error("expected denied audit entry")
	}
}

// --- Log tests ---

func TestLogWritesToLogger(t *testing.T) {
	logger := &mockLogger{}
	deps := baseDeps()
	deps.Logger = logger

	input, _ := json.Marshal(host.LogRequest{Level: "info", Message: "hello"})
	_, _ = host.ExecuteLog(context.Background(), deps, input)

	if len(logger.entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(logger.entries))
	}
	if logger.entries[0] != "test-plugin:info:hello" {
		t.Errorf("log entry = %q", logger.entries[0])
	}
}

func TestLogWorksWithoutCapability(t *testing.T) {
	logger := &mockLogger{}
	deps := &host.Deps{
		Caps:       []string{}, // no caps needed for logging
		Logger:     logger,
		PluginName: "p",
	}

	input, _ := json.Marshal(host.LogRequest{Level: "warn", Message: "test"})
	out, _ := host.ExecuteLog(context.Background(), deps, input)

	if len(logger.entries) != 1 {
		t.Error("log should work without capabilities")
	}
	// Response should be ok.
	if string(out) != `{"ok":true}` {
		t.Errorf("response = %s", out)
	}
}

// --- ConfigGet tests ---

func TestConfigGetReturnsValue(t *testing.T) {
	deps := baseDeps()
	deps.Config = &mockConfig{data: map[string]string{"base_url": "https://example.com"}}

	input, _ := json.Marshal(host.ConfigGetRequest{Key: "base_url"})
	out, _ := host.ExecuteConfigGet(context.Background(), deps, input)

	var resp host.ConfigGetResponse
	_ = json.Unmarshal(out, &resp)
	if !resp.Found {
		t.Error("expected found=true")
	}
	if resp.Value != "https://example.com" {
		t.Errorf("value = %q", resp.Value)
	}
}

func TestConfigGetUnknownKey(t *testing.T) {
	deps := baseDeps()
	deps.Config = &mockConfig{data: map[string]string{}}

	input, _ := json.Marshal(host.ConfigGetRequest{Key: "nonexistent"})
	out, _ := host.ExecuteConfigGet(context.Background(), deps, input)

	var resp host.ConfigGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Found {
		t.Error("expected found=false for unknown key")
	}
}

// --- CredentialGet tests ---

func TestCredentialGetWithMatchingCap(t *testing.T) {
	deps := &host.Deps{
		Caps:       []string{"credential.read:openai_api_key"},
		Vault:      &mockVault{data: map[string][]byte{"openai_api_key": []byte("sk-test")}},
		PluginID:   1,
		PluginName: "test",
		Auditor:    &mockAuditor{},
	}

	input, _ := json.Marshal(host.CredentialGetRequest{Key: "openai_api_key"})
	out, _ := host.ExecuteCredentialGet(context.Background(), deps, input)

	var resp host.CredentialGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Value != "sk-test" {
		t.Errorf("value = %q, want sk-test", resp.Value)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error = %q", resp.Error)
	}
}

func TestCredentialGetDeniedWithoutCap(t *testing.T) {
	auditor := &mockAuditor{}
	deps := &host.Deps{
		Caps:    []string{"tool.call"}, // no credential.read
		Auditor: auditor,
	}

	input, _ := json.Marshal(host.CredentialGetRequest{Key: "secret"})
	out, _ := host.ExecuteCredentialGet(context.Background(), deps, input)

	var resp host.CredentialGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error for denied credential access")
	}
}

func TestCredentialGetDeniedForNonMatchingKey(t *testing.T) {
	deps := &host.Deps{
		Caps:    []string{"credential.read:openai_api_key"}, // specific key only
		Auditor: &mockAuditor{},
	}

	input, _ := json.Marshal(host.CredentialGetRequest{Key: "github_token"})
	out, _ := host.ExecuteCredentialGet(context.Background(), deps, input)

	var resp host.CredentialGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error for non-matching key scope")
	}
}

func TestCredentialGetAuditsAccess(t *testing.T) {
	auditor := &mockAuditor{}
	deps := &host.Deps{
		Caps:    []string{"credential.read:key1"},
		Vault:   &mockVault{data: map[string][]byte{"key1": []byte("v")}},
		Auditor: auditor,
	}

	input, _ := json.Marshal(host.CredentialGetRequest{Key: "key1"})
	_, _ = host.ExecuteCredentialGet(context.Background(), deps, input)

	if len(auditor.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(auditor.entries))
	}
	if auditor.entries[0].action != "credential_get" {
		t.Errorf("action = %q", auditor.entries[0].action)
	}
	if auditor.entries[0].status != "success" {
		t.Errorf("status = %q", auditor.entries[0].status)
	}
}

func TestToolCallToolError(t *testing.T) {
	deps := baseDeps()
	deps.ToolCaller = &mockToolCaller{err: errors.New("tool failed")}

	input, _ := json.Marshal(host.ToolCallRequest{Tool: "bad.tool", Args: `{}`})
	out, _ := host.ExecuteToolCall(context.Background(), deps, input)

	var resp host.ToolCallResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error from tool failure")
	}
}

func TestCredentialGetRejectsEmptyKey(t *testing.T) {
	deps := &host.Deps{
		Caps:    []string{"credential.read"},
		Auditor: &mockAuditor{},
	}

	input, _ := json.Marshal(host.CredentialGetRequest{Key: ""})
	out, _ := host.ExecuteCredentialGet(context.Background(), deps, input)

	var resp host.CredentialGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error for empty credential key")
	}
}

func TestToolCallNoToolCaller(t *testing.T) {
	deps := baseDeps()
	// ToolCaller is nil

	input, _ := json.Marshal(host.ToolCallRequest{Tool: "test", Args: `{}`})
	out, _ := host.ExecuteToolCall(context.Background(), deps, input)

	var resp host.ToolCallResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error when ToolCaller is nil")
	}
}

func TestConfigGetNilConfig(t *testing.T) {
	deps := baseDeps()
	// Config is nil

	input, _ := json.Marshal(host.ConfigGetRequest{Key: "anything"})
	out, _ := host.ExecuteConfigGet(context.Background(), deps, input)

	var resp host.ConfigGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Found {
		t.Error("expected found=false when Config is nil")
	}
}

func TestLogNilLogger(t *testing.T) {
	deps := baseDeps()
	// Logger is nil — should not panic

	input, _ := json.Marshal(host.LogRequest{Level: "info", Message: "test"})
	out, err := host.ExecuteLog(context.Background(), deps, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("response = %s", out)
	}
}

func TestCredentialGetNoVault(t *testing.T) {
	deps := &host.Deps{
		Caps:    []string{"credential.read:key1"},
		Auditor: &mockAuditor{},
		// Vault is nil
	}

	input, _ := json.Marshal(host.CredentialGetRequest{Key: "key1"})
	out, _ := host.ExecuteCredentialGet(context.Background(), deps, input)

	var resp host.CredentialGetResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == "" {
		t.Error("expected error when Vault is nil")
	}
}
