package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanManifest_NoAuthor(t *testing.T) {
	s := New()
	m := &Manifest{Name: "test-plugin", Version: "1.0.0"}
	findings := s.ScanManifest(m)

	found := false
	for _, f := range findings {
		if f.Code == "MANIFEST_NO_AUTHOR" && f.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Error("expected MANIFEST_NO_AUTHOR warning")
	}
}

func TestScanManifest_ExcessiveCaps(t *testing.T) {
	s := New()
	caps := make([]string, 15)
	for i := range caps {
		caps[i] = "cap." + string(rune('a'+i))
	}
	m := &Manifest{Name: "test", Version: "1.0.0", Author: "tester", Capabilities: caps}
	findings := s.ScanManifest(m)

	found := false
	for _, f := range findings {
		if f.Code == "MANIFEST_EXCESSIVE_CAPS" {
			found = true
		}
	}
	if !found {
		t.Error("expected MANIFEST_EXCESSIVE_CAPS warning")
	}
}

func TestScanManifest_DangerousCap(t *testing.T) {
	s := New()
	m := &Manifest{
		Name:         "evil-plugin",
		Version:      "1.0.0",
		Author:       "tester",
		Capabilities: []string{"tool.call", "shell.exec"},
	}
	findings := s.ScanManifest(m)

	found := false
	for _, f := range findings {
		if f.Code == "MANIFEST_DANGEROUS_CAP" && f.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Error("expected MANIFEST_DANGEROUS_CAP critical finding")
	}
}

func TestScanManifest_SuspiciousName(t *testing.T) {
	s := New()
	m := &Manifest{Name: "../etc/passwd", Version: "1.0.0", Author: "tester"}
	findings := s.ScanManifest(m)

	found := false
	for _, f := range findings {
		if f.Code == "MANIFEST_SUSPICIOUS_NAME" {
			found = true
		}
	}
	if !found {
		t.Error("expected MANIFEST_SUSPICIOUS_NAME warning")
	}
}

func TestScanWASMFile_Missing(t *testing.T) {
	s := New()
	findings, err := s.ScanWASMFile("/nonexistent/path/plugin.wasm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, f := range findings {
		if f.Code == "WASM_MISSING" && f.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Error("expected WASM_MISSING critical finding")
	}
}

func TestScanWASMFile_Exists(t *testing.T) {
	s := New()
	// Create a small temp file.
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wasm")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	findings, err := s.ScanWASMFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Code == "WASM_MISSING" {
			t.Error("should not report WASM_MISSING for existing file")
		}
	}
	// Small file should have no WASM_TOO_LARGE.
	for _, f := range findings {
		if f.Code == "WASM_TOO_LARGE" {
			t.Error("should not report WASM_TOO_LARGE for small file")
		}
	}
}

func TestScanManifest_NoVersion(t *testing.T) {
	s := New()
	m := &Manifest{Name: "test-plugin", Author: "tester"}
	findings := s.ScanManifest(m)

	found := false
	for _, f := range findings {
		if f.Code == "MANIFEST_NO_VERSION" {
			found = true
		}
	}
	if !found {
		t.Error("expected MANIFEST_NO_VERSION warning")
	}
}
