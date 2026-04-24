package rescan

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/plugin/scanner"
)

// PluginLister is a narrow interface for listing installed plugins.
type PluginLister interface {
	ListManifests(ctx context.Context) ([]scanner.Manifest, error)
}

// ScanResult holds the result of scanning a single plugin.
type ScanResult struct {
	PluginName string            `json:"plugin_name"`
	Findings   []scanner.Finding `json:"findings"`
	ScannedAt  time.Time         `json:"scanned_at"`
}

// Rescanner performs periodic security rescans of installed plugins.
type Rescanner struct {
	scanner    *scanner.Scanner
	registry   PluginLister
	interval   time.Duration
	signatures []string // threat signature patterns

	mu          sync.RWMutex
	lastResults []ScanResult
	stopCh      chan struct{}
	running     bool
}

// New creates a Rescanner.
func New(s *scanner.Scanner, registry PluginLister, interval time.Duration) *Rescanner {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	return &Rescanner{
		scanner:  s,
		registry: registry,
		interval: interval,
	}
}

// Start begins the periodic rescan background goroutine.
func (r *Rescanner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil
	}
	r.stopCh = make(chan struct{})
	r.running = true

	go r.loop(ctx)
	return nil
}

// Stop halts the background rescan loop.
func (r *Rescanner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}
	close(r.stopCh)
	r.running = false
}

// UpdateSignatures replaces the threat signature patterns used during rescans.
func (r *Rescanner) UpdateSignatures(sigs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signatures = make([]string, len(sigs))
	copy(r.signatures, sigs)
}

// LastScanResult returns the results from the most recent scan cycle.
func (r *Rescanner) LastScanResult() []ScanResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]ScanResult, len(r.lastResults))
	copy(results, r.lastResults)
	return results
}

func (r *Rescanner) loop(ctx context.Context) {
	// Run an initial scan immediately.
	r.runScan(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.runScan(ctx)
		}
	}
}

func (r *Rescanner) runScan(ctx context.Context) {
	if r.registry == nil {
		return
	}

	manifests, err := r.registry.ListManifests(ctx)
	if err != nil {
		log.Printf("rescan: list manifests: %v", err)
		return
	}

	r.mu.RLock()
	sigs := make([]string, len(r.signatures))
	copy(sigs, r.signatures)
	r.mu.RUnlock()

	var results []ScanResult
	now := time.Now()

	for i := range manifests {
		m := &manifests[i]
		findings := r.scanner.ScanManifest(m)

		// Check against threat signatures.
		for _, sig := range sigs {
			sigLower := strings.ToLower(sig)
			for _, cap := range m.Capabilities {
				if strings.Contains(strings.ToLower(cap), sigLower) {
					findings = append(findings, scanner.Finding{
						Severity: "critical",
						Code:     "SIGNATURE_MATCH",
						Message:  "capability matches threat signature: " + sig,
					})
				}
			}
			if strings.Contains(strings.ToLower(m.Name), sigLower) {
				findings = append(findings, scanner.Finding{
					Severity: "warning",
					Code:     "SIGNATURE_MATCH_NAME",
					Message:  "plugin name matches threat signature: " + sig,
				})
			}
		}

		results = append(results, ScanResult{
			PluginName: m.Name,
			Findings:   findings,
			ScannedAt:  now,
		})
	}

	r.mu.Lock()
	r.lastResults = results
	r.mu.Unlock()

	if len(results) > 0 {
		totalFindings := 0
		for _, res := range results {
			totalFindings += len(res.Findings)
		}
		if totalFindings > 0 {
			log.Printf("rescan: scanned %d plugins, %d findings", len(results), totalFindings)
		}
	}
}
