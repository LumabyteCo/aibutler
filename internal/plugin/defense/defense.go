package defense

import (
	"fmt"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

// SuspiciousCombo is a pair of capabilities that together pose elevated risk.
type SuspiciousCombo struct {
	A      string
	B      string
	Reason string
	Level  string // "warning" or "critical"
}

// AuditResult contains the defense analysis of a manifest.
type AuditResult struct {
	Passed   bool
	Warnings []string
	Critical []string
	Combos   []SuspiciousCombo
}

// KnownCombos lists suspicious capability combinations.
var KnownCombos = []SuspiciousCombo{
	{A: "credential.read", B: "tool.call", Reason: "credential exfiltration via tool invocation", Level: "critical"},
	{A: "credential.read", B: "http", Reason: "credential exfiltration via network", Level: "critical"},
	{A: "credential.read", B: "web", Reason: "credential exfiltration via web access", Level: "critical"},
	{A: "kv.write", B: "tool.call", Reason: "data staging with tool exfiltration", Level: "warning"},
	{A: "credential.read", B: "kv.write", Reason: "credential exfiltration via persistent storage", Level: "critical"},
}

// ValidateSandbox checks L1 WASM sandbox constraints.
// Plugins cannot have filesystem or raw network access; all I/O goes through host functions.
func ValidateSandbox(m *manifest.Manifest) error {
	for _, cap := range m.Capabilities {
		lower := strings.ToLower(cap)
		if strings.HasPrefix(lower, "fs.") || lower == "filesystem" {
			return fmt.Errorf("defense: plugin %q requests filesystem access %q which is not allowed in WASM sandbox", m.Name, cap)
		}
		if lower == "network.raw" || lower == "socket" {
			return fmt.Errorf("defense: plugin %q requests raw network access %q which is not allowed in WASM sandbox", m.Name, cap)
		}
	}
	return nil
}

// AuditCapabilities performs L2 capability audit, checking for suspicious combinations.
func AuditCapabilities(caps []string) AuditResult {
	result := AuditResult{Passed: true}

	// Reject wildcard capabilities (defense-in-depth, complements manifest.Validate).
	for _, c := range caps {
		if strings.Contains(c, "*") {
			result.Critical = append(result.Critical, fmt.Sprintf("wildcard capability %q not allowed", c))
			result.Passed = false
		}
	}

	for _, combo := range KnownCombos {
		hasA := capMatchesAny(combo.A, caps)
		hasB := capMatchesAny(combo.B, caps)
		if hasA && hasB {
			result.Combos = append(result.Combos, combo)
			msg := fmt.Sprintf("%s + %s: %s", combo.A, combo.B, combo.Reason)
			switch combo.Level {
			case "critical":
				result.Critical = append(result.Critical, msg)
				result.Passed = false
			case "warning":
				result.Warnings = append(result.Warnings, msg)
			}
		}
	}

	return result
}

// capMatchesAny returns true if any capability in caps starts with or equals prefix.
func capMatchesAny(prefix string, caps []string) bool {
	for _, c := range caps {
		if c == prefix || strings.HasPrefix(c, prefix+":") || strings.HasPrefix(c, prefix+".") {
			return true
		}
	}
	return false
}
