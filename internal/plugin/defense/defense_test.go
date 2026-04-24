package defense_test

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/defense"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

func TestValidateSandboxPassesToolOnly(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "safe",
		Version:      "1.0",
		Capabilities: []string{"tool.call", "kv.read"},
	}
	if err := defense.ValidateSandbox(m); err != nil {
		t.Errorf("expected pass, got: %v", err)
	}
}

func TestValidateSandboxRejectsFilesystem(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "bad",
		Version:      "1.0",
		Capabilities: []string{"fs.read", "tool.call"},
	}
	if err := defense.ValidateSandbox(m); err == nil {
		t.Error("expected error for filesystem access")
	}
}

func TestValidateSandboxRejectsRawNetwork(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "bad",
		Version:      "1.0",
		Capabilities: []string{"network.raw"},
	}
	if err := defense.ValidateSandbox(m); err == nil {
		t.Error("expected error for raw network access")
	}
}

func TestAuditCredentialPlusToolCall(t *testing.T) {
	result := defense.AuditCapabilities([]string{"credential.read:key1", "tool.call"})
	if result.Passed {
		t.Error("expected Passed=false for credential+tool combo")
	}
	if len(result.Critical) == 0 {
		t.Error("expected critical warning for credential+tool combo")
	}
}

func TestAuditCredentialPlusNetwork(t *testing.T) {
	result := defense.AuditCapabilities([]string{"credential.read:api_key", "http.get"})
	if result.Passed {
		t.Error("expected Passed=false for credential+http combo")
	}
	if len(result.Critical) == 0 {
		t.Error("expected critical for credential+http")
	}
}

func TestAuditCleanSet(t *testing.T) {
	result := defense.AuditCapabilities([]string{"tool.call", "kv.read", "log"})
	if !result.Passed {
		t.Error("expected Passed=true for clean capability set")
	}
	if len(result.Critical) != 0 {
		t.Errorf("expected no critical, got %v", result.Critical)
	}
}

func TestAuditKVWritePlusToolCall(t *testing.T) {
	result := defense.AuditCapabilities([]string{"kv.write", "tool.call"})
	// This should produce a warning, not a critical.
	if len(result.Warnings) == 0 {
		t.Error("expected warning for kv.write+tool.call")
	}
	// Warnings don't fail the audit.
	if !result.Passed {
		t.Error("kv.write+tool.call should not fail audit (warning only)")
	}
}

func TestAuditCriticalFailsAudit(t *testing.T) {
	result := defense.AuditCapabilities([]string{"credential.read:x", "tool.call"})
	if result.Passed {
		t.Error("critical combo should set Passed=false")
	}
	if len(result.Combos) == 0 {
		t.Error("expected at least one combo reported")
	}
	if result.Combos[0].Level != "critical" {
		t.Errorf("combo level = %q, want critical", result.Combos[0].Level)
	}
}

func TestAuditRejectsWildcardCapability(t *testing.T) {
	result := defense.AuditCapabilities([]string{"credential.read:*"})
	if result.Passed {
		t.Error("expected Passed=false for wildcard capability")
	}
	if len(result.Critical) == 0 {
		t.Error("expected critical for wildcard capability")
	}
}

func TestAuditRejectsStarOnly(t *testing.T) {
	result := defense.AuditCapabilities([]string{"*"})
	if result.Passed {
		t.Error("expected Passed=false for bare wildcard")
	}
}

func TestAuditCredentialPlusKVWrite(t *testing.T) {
	result := defense.AuditCapabilities([]string{"credential.read:api_key", "kv.write"})
	if result.Passed {
		t.Error("expected Passed=false for credential.read+kv.write")
	}
	if len(result.Critical) == 0 {
		t.Error("expected critical for credential exfiltration via KV store")
	}
}

func TestValidateSandboxRejectsSocket(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "socket-plugin",
		Version:      "1.0",
		Capabilities: []string{"socket"},
	}
	if err := defense.ValidateSandbox(m); err == nil {
		t.Error("expected error for socket capability")
	}
}
