package audit_test

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/audit"
)

// TestRedactPINs guards the B10 security fix: PIN values must be removed
// from text that re-enters model context, while everything around the PIN
// stays intact.
func TestRedactPINs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"PIN colon", "Unlock the door. Safety PIN: 2468", "Unlock the door. Safety PIN: [REDACTED]"},
		{"PIN space", "pin 2468", "pin [REDACTED]"},
		{"PIN equals", `{"pin":"2468"}`, `{"pin":"[REDACTED]"}`},
		{"PIN is phrasing", "My PIN is 2468, thanks", "My PIN is [REDACTED], thanks"},
		{"plain pin key", `"pin": 99112233`, `"pin": [REDACTED]`},
		{"safety pin variant", "safety pin: 0000", "safety pin: [REDACTED]"},
		{"case insensitive", "PIN: 13579", "PIN: [REDACTED]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := audit.RedactPINs(tc.in)
			if got != tc.want {
				t.Errorf("RedactPINs(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "2468") || strings.Contains(got, "13579") {
				t.Errorf("PIN value leaked: %q", got)
			}
		})
	}
}

// TestRedactPINsKeepsNonPINNumbers ensures ordinary numbers are untouched —
// over-redaction would break tool inputs like temperatures or IDs.
func TestRedactPINsKeepsNonPINNumbers(t *testing.T) {
	cases := []string{
		"set temperature to 21.5 degrees",
		"temperature is 5 to 32",
		"room 12 is available",      // short numbers not preceded by pin
		"call me at 5551234567",     // not PIN-labeled
		"2026-10-05 the 5th of October",
	}
	for _, in := range cases {
		if got := audit.RedactPINs(in); got != in {
			t.Errorf("RedactPINs(%q) = %q — must not touch non-PIN numbers", in, got)
		}
	}
}

// TestRedactCatchesPINs guards the log-side pattern: the general Redact
// (used by the audit-log writer) also strips PIN values.
func TestRedactCatchesPINs(t *testing.T) {
	logged := audit.Redact("iot.safety.control invoked with pin: 2468 confirmed=true")
	if strings.Contains(logged, "2468") {
		t.Errorf("Redact leaked PIN: %q", logged)
	}
	if !strings.Contains(logged, "[REDACTED]") {
		t.Errorf("Redact should replace PIN with [REDACTED], got: %q", logged)
	}
}