package scanner

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is the narrow interface used by scanner to avoid import cycles.
type Manifest struct {
	Name         string
	Version      string
	Author       string
	Capabilities []string
	WASMPath     string
}

// Finding is a single scan result.
type Finding struct {
	Severity string // "critical", "warning", "info"
	Code     string // e.g., "MANIFEST_EXCESSIVE_CAPS", "MANIFEST_NO_AUTHOR"
	Message  string
}

// Scanner performs static analysis of WASM plugin manifests.
type Scanner struct {
	maxCapabilities int
	bannedCaps      []string
}

// New creates a Scanner with default settings.
func New() *Scanner {
	return &Scanner{
		maxCapabilities: 10,
		bannedCaps:      []string{"credential.write", "shell.exec", "file.write.all"},
	}
}

// ScanManifest checks a manifest for suspicious patterns.
func (s *Scanner) ScanManifest(m *Manifest) []Finding {
	var findings []Finding

	if m.Author == "" {
		findings = append(findings, Finding{
			Severity: "warning",
			Code:     "MANIFEST_NO_AUTHOR",
			Message:  "manifest has no author field",
		})
	}

	if m.Version == "" {
		findings = append(findings, Finding{
			Severity: "warning",
			Code:     "MANIFEST_NO_VERSION",
			Message:  "manifest has no version field",
		})
	}

	if len(m.Capabilities) > s.maxCapabilities {
		findings = append(findings, Finding{
			Severity: "warning",
			Code:     "MANIFEST_EXCESSIVE_CAPS",
			Message:  fmt.Sprintf("manifest requests %d capabilities (max recommended: %d)", len(m.Capabilities), s.maxCapabilities),
		})
	}

	for _, cap := range m.Capabilities {
		lower := strings.ToLower(cap)
		for _, banned := range s.bannedCaps {
			if lower == banned {
				findings = append(findings, Finding{
					Severity: "critical",
					Code:     "MANIFEST_DANGEROUS_CAP",
					Message:  fmt.Sprintf("manifest requests dangerous capability %q", cap),
				})
			}
		}
	}

	if strings.Contains(m.Name, "..") || strings.Contains(m.Name, "/") {
		findings = append(findings, Finding{
			Severity: "warning",
			Code:     "MANIFEST_SUSPICIOUS_NAME",
			Message:  fmt.Sprintf("plugin name %q contains suspicious characters", m.Name),
		})
	}

	return findings
}

// ScanWASMFile checks a WASM file for size and existence.
func (s *Scanner) ScanWASMFile(path string) ([]Finding, error) {
	var findings []Finding

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			findings = append(findings, Finding{
				Severity: "critical",
				Code:     "WASM_MISSING",
				Message:  fmt.Sprintf("WASM file not found: %s", filepath.Base(path)),
			})
			return findings, nil
		}
		return nil, fmt.Errorf("scanner: stat %s: %w", filepath.Base(path), err)
	}

	const maxSize = 50 * 1024 * 1024 // 50MB
	if info.Size() > maxSize {
		findings = append(findings, Finding{
			Severity: "warning",
			Code:     "WASM_TOO_LARGE",
			Message:  fmt.Sprintf("WASM file is %d bytes (max recommended: %d)", info.Size(), maxSize),
		})
	}

	return findings, nil
}

// HashFile returns the SHA-256 hex digest of a file.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("scanner: read %s: %w", filepath.Base(path), err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}
