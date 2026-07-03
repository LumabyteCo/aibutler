// Package reflection is the idle-time maintenance cycle: on a schedule, it
// reviews recent memory for contradictions and staleness, checks index
// health, optionally backfills missing embeddings, and writes a maintenance
// report the user can read.
//
// It proposes, it never silently applies: contradictions land in the
// existing review queue, staleness observations go into the report, and
// nothing that touches permissions or deletes data happens here. The cycle
// runs as deterministic code (a scheduler builtin) — no model call, no tool
// access, and it works entirely through the same stores the rest of the
// system uses.
package reflection

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/digest"
)

// Backfiller embeds memory items that are missing vectors. Optional — nil
// when no embedding provider is configured. Satisfied by an adapter over
// the memory package's batched backfiller.
type Backfiller interface {
	Run(ctx context.Context) (embedded int, failed int, err error)
}

// MaintenanceDigest is the digest type maintenance reports are saved under.
const MaintenanceDigest = digest.DigestType("maintenance")

// Maintenance is the reflection cycle.
type Maintenance struct {
	db       *sql.DB
	mem      *memory.Store
	digests  *digest.Generator
	backfill Backfiller
}

// New creates the maintenance cycle. digests and backfill may be nil.
func New(db *sql.DB, mem *memory.Store, digests *digest.Generator, backfill Backfiller) *Maintenance {
	return &Maintenance{db: db, mem: mem, digests: digests, backfill: backfill}
}

// Run executes one maintenance pass and returns a one-line summary. It is
// registered as the scheduler builtin "memory.maintenance".
func (m *Maintenance) Run(ctx context.Context) (string, error) {
	var sections []string

	// 1. Contradiction sweep: unresolved flagged replacements wait in the
	// review queue; the report surfaces the count so they don't rot there.
	const conflictWindow = 50
	pending, err := m.mem.GetConflicts(ctx, true, conflictWindow)
	if err != nil {
		return "", fmt.Errorf("reflection: conflicts: %w", err)
	}
	if len(pending) > 0 {
		var lines []string
		for _, c := range pending {
			lines = append(lines, fmt.Sprintf("- %q replaced %q (%s)", c.NewFact, c.OldFact, c.FactKey))
		}
		count := fmt.Sprintf("%d", len(pending))
		if len(pending) == conflictWindow {
			count += "+"
		}
		sections = append(sections, fmt.Sprintf("%s contradiction(s) awaiting your review:\n%s",
			count, strings.Join(lines, "\n")))
	}

	// 2. Staleness observations: high-importance facts that haven't been
	// confirmed or used in a long time may be outdated. Observation only —
	// nothing is demoted or deleted here.
	stale, err := m.staleImportantFacts(ctx, 90*24*time.Hour, 10)
	if err != nil {
		return "", fmt.Errorf("reflection: staleness: %w", err)
	}
	if len(stale) > 0 {
		sections = append(sections, fmt.Sprintf("%d important fact(s) unconfirmed for 90+ days — worth re-checking:\n- %s",
			len(stale), strings.Join(stale, "\n- ")))
	}

	// 3. Index health.
	stats := m.mem.IndexStats()
	if stats.Failed > 0 || stats.Dropped > 0 {
		sections = append(sections, fmt.Sprintf("Embedding indexer: %d queued, %d indexed, %d FAILED, %d DROPPED — a reindex may be needed (aibutler memory reindex).",
			stats.Queued, stats.Indexed, stats.Failed, stats.Dropped))
	}

	// 4. Optional embedding backfill for items that missed indexing.
	if m.backfill != nil {
		embedded, bfFailed, err := m.backfill.Run(ctx)
		if err != nil {
			sections = append(sections, fmt.Sprintf("Embedding backfill errored: %v", err))
		} else if embedded > 0 || bfFailed > 0 {
			sections = append(sections, fmt.Sprintf("Embedding backfill: %d item(s) embedded, %d failed.", embedded, bfFailed))
		}
	}

	report := "Nothing needs attention."
	if len(sections) > 0 {
		report = strings.Join(sections, "\n\n")
	}

	// Persist the report as a digest (CLI: aibutler memory digests, which
	// lists titles by type — pass the maintenance type to find these).
	if m.digests != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		if err := m.digests.Save(ctx, &digest.Digest{
			Type:      MaintenanceDigest,
			Title:     "Memory maintenance report",
			Content:   report,
			PeriodEnd: now,
		}); err != nil {
			log.Printf("reflection: WARNING — maintenance report not persisted: %v", err)
		}
	}

	summary := fmt.Sprintf("maintenance pass: %d review item(s), %d stale flag(s)", len(pending), len(stale))
	return summary, nil
}

// staleImportantFacts lists active facts with importance >= 8 whose last
// confirmation (extraction or access) is older than the window.
func (m *Maintenance) staleImportantFacts(ctx context.Context, window time.Duration, limit int) ([]string, error) {
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)
	rows, err := m.db.QueryContext(ctx,
		`SELECT fact FROM key_facts
		 WHERE status = 'active' AND importance >= 8
		   AND extracted_at < ? AND COALESCE(last_accessed, '') < ?
		 ORDER BY importance DESC, extracted_at ASC LIMIT ?`,
		cutoff, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
