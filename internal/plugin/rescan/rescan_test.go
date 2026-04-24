package rescan

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/plugin/scanner"
)

// stubLister returns a fixed set of manifests.
type stubLister struct {
	manifests []scanner.Manifest
}

func (s *stubLister) ListManifests(_ context.Context) ([]scanner.Manifest, error) {
	return s.manifests, nil
}

func TestStartStop(t *testing.T) {
	s := scanner.New()
	lister := &stubLister{
		manifests: []scanner.Manifest{
			{Name: "test-plugin", Version: "1.0", Author: "tester", Capabilities: []string{"data.read"}},
		},
	}

	r := New(s, lister, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for at least one scan cycle.
	time.Sleep(200 * time.Millisecond)

	results := r.LastScanResult()
	if len(results) == 0 {
		t.Fatal("expected scan results after cycle")
	}

	if results[0].PluginName != "test-plugin" {
		t.Fatalf("expected plugin name 'test-plugin', got %q", results[0].PluginName)
	}

	r.Stop()

	// Should be safe to call Stop again.
	r.Stop()
}

func TestSignatureUpdate(t *testing.T) {
	s := scanner.New()
	lister := &stubLister{
		manifests: []scanner.Manifest{
			{Name: "malware-plugin", Version: "1.0", Author: "bad", Capabilities: []string{"credential.read", "network.send"}},
		},
	}

	r := New(s, lister, 100*time.Millisecond)
	r.UpdateSignatures([]string{"credential", "network"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	results := r.LastScanResult()
	if len(results) == 0 {
		t.Fatal("expected scan results")
	}

	// Should have signature match findings.
	foundSigMatch := false
	for _, res := range results {
		for _, f := range res.Findings {
			if f.Code == "SIGNATURE_MATCH" || f.Code == "SIGNATURE_MATCH_NAME" {
				foundSigMatch = true
				break
			}
		}
	}

	if !foundSigMatch {
		t.Fatal("expected at least one signature match finding")
	}

	r.Stop()
}
