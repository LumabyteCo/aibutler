package audit_test

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/audit"
)

func TestRedactAPIKeys(t *testing.T) {
	tests := []struct {
		input string
		check string // Should NOT appear in output
	}{
		{"api_key=sk-proj1234567890abcdefghij", "sk-proj1234567890abcdefghij"},
		{"Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz", "sk-abcdefghijklmnopqrstuvwxyz"},
		{"token: ghp_abcdefghijklmnopqrstuvwxyz123456", "ghp_abcdefghijklmnopqrstuvwxyz123456"},
		{"AKIA1234567890ABCDEF secret", "AKIA1234567890ABCDEF"},
		{"xoxb-12345-67890-abcdefg", "xoxb-12345-67890-abcdefg"},
	}

	for _, tc := range tests {
		result := audit.Redact(tc.input)
		if strings.Contains(result, tc.check) {
			t.Errorf("Redact(%q) still contains %q: got %q", tc.input, tc.check, result)
		}
		if !strings.Contains(result, "[REDACTED]") {
			t.Errorf("Redact(%q) missing [REDACTED]: got %q", tc.input, result)
		}
	}
}

func TestRedactPreservesNonSensitive(t *testing.T) {
	input := "Hello, the weather is nice today."
	result := audit.Redact(input)
	if result != input {
		t.Errorf("Redact modified non-sensitive text: %q -> %q", input, result)
	}
}

func TestContainsSensitive(t *testing.T) {
	if !audit.ContainsSensitive("my api_key=sk-abcdefghijklmnopqrstuvwx") {
		t.Error("expected sensitive detection")
	}
	if audit.ContainsSensitive("Hello, the weather is nice today.") {
		t.Error("false positive on non-sensitive text")
	}
}
