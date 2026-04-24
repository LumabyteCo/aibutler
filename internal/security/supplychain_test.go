package security_test

import (
	"os"
	"strings"
	"testing"
)

// TestSecurityWorkflowExists verifies that the expected CI coverage is present
// across both GitHub Actions workflows:
//
//   - .github/workflows/security.yml owns govulncheck + dependency-review
//     (runs on every push + PR + weekly schedule).
//   - .github/workflows/ci.yml owns go vet + race-detector tests (runs on
//     every push + PR).
//
// The split reflects a deliberate separation of concerns: security.yml is
// security-scoped (CVE scanning, dependency review), ci.yml is correctness-
// scoped (lint, build, test, integration). Previously both workflows duplicated
// go vet and -race; the duplication was removed when the workflows were
// cleaned up for the v0.1 release.
func TestSecurityWorkflowExists(t *testing.T) {
	security, err := os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatalf("security workflow not found: %v", err)
	}
	ci, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("ci workflow not found: %v", err)
	}

	securityContent := string(security)
	ciContent := string(ci)

	// Checks that must be present in security.yml specifically.
	securityChecks := []struct {
		name    string
		pattern string
	}{
		{"govulncheck step", "govulncheck"},
		{"dependency review", "dependency-review"},
	}
	for _, check := range securityChecks {
		if !strings.Contains(securityContent, check.pattern) {
			t.Errorf("security.yml missing %s (pattern: %q)", check.name, check.pattern)
		}
	}

	// Checks that must be present in ci.yml (where lint + test live).
	ciChecks := []struct {
		name    string
		pattern string
	}{
		{"go vet", "go vet"},
		{"race detector", "-race"},
	}
	for _, check := range ciChecks {
		if !strings.Contains(ciContent, check.pattern) {
			t.Errorf("ci.yml missing %s (pattern: %q)", check.name, check.pattern)
		}
	}
}
