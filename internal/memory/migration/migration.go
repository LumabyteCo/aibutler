package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/bank"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
)

// MaxImportSize is the maximum size for imported files (100 MB).
const MaxImportSize = 100 * 1024 * 1024

// LimitedReader returns a reader that limits reads to MaxImportSize.
func LimitedReader(r io.Reader) io.Reader {
	return io.LimitReader(r, MaxImportSize)
}

// Importer converts a specific format into captured thoughts.
type Importer interface {
	Name() string
	Parse(ctx context.Context, r io.Reader, save SaveFunc) error
}

// SaveFunc is the callback importers use to persist each thought.
type SaveFunc func(ctx context.Context, content, source string, tags []string) error

// ImportOpts controls import behavior.
type ImportOpts struct {
	Filename string
	Tags     []string
	DryRun   bool
	// Dedup skips thoughts whose (source, content) already exists, making
	// re-importing the same export idempotent and de-duplicating repeats within
	// a single import.
	Dedup bool
}

// ImportResult summarizes an import run.
type ImportResult struct {
	ThoughtsImported  int
	EntitiesExtracted int
	Relationships     int
	Skipped           int
	Errors            []string
}

// Orchestrator manages the import pipeline.
type Orchestrator struct {
	db       *sql.DB
	memStore *memory.Store
	entity   *entity.Store
}

// NewOrchestrator creates an import orchestrator.
func NewOrchestrator(db *sql.DB, mem *memory.Store, ent *entity.Store) *Orchestrator {
	return &Orchestrator{db: db, memStore: mem, entity: ent}
}

// Run orchestrates the full import pipeline.
func (o *Orchestrator) Run(ctx context.Context, imp Importer, r io.Reader, opts ImportOpts) (*ImportResult, error) {
	result := &ImportResult{}
	filename := opts.Filename
	if filename == "" {
		filename = "stdin"
	}

	// Record import start.
	importID, err := o.recordStart(ctx, imp.Name(), filename)
	if err != nil {
		return nil, fmt.Errorf("migration: record start: %w", err)
	}

	// For idempotent re-import, load the keys of already-stored thoughts up front
	// and skip anything we have seen (in the DB or earlier in this same import).
	var seen map[string]struct{}
	if opts.Dedup {
		seen, err = o.loadThoughtKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("migration: load existing thoughts: %w", err)
		}
	}

	saver := func(ctx context.Context, content, source string, tags []string) error {
		// Check context cancellation between items.
		if err := ctx.Err(); err != nil {
			return err
		}

		if seen != nil {
			key := thoughtKey(source, content)
			if _, dup := seen[key]; dup {
				result.Skipped++
				return nil
			}
			seen[key] = struct{}{}
		}

		if opts.DryRun {
			result.ThoughtsImported++
			return nil
		}

		allTags := append([]string{}, opts.Tags...)
		allTags = append(allTags, tags...)

		_, err := o.memStore.SaveThought(ctx, content, source, "", allTags)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("save thought: %s", err))
			return nil // continue importing
		}
		result.ThoughtsImported++

		// Extract entities and link co-occurring ones into the knowledge graph.
		extracted := entity.Extract(content)
		entCount, edgeCount, entErrs := o.entity.SaveExtracted(ctx, extracted, "")
		result.EntitiesExtracted += entCount
		result.Relationships += edgeCount
		result.Errors = append(result.Errors, entErrs...)

		return nil
	}

	parseErr := imp.Parse(ctx, r, saver)
	status := "completed"
	errMsg := ""
	if parseErr != nil {
		status = "failed"
		errMsg = parseErr.Error()
	}

	o.recordComplete(ctx, importID, result, status, errMsg)
	return result, parseErr
}

func (o *Orchestrator) recordStart(ctx context.Context, source, filename string) (int64, error) {
	result, err := o.db.ExecContext(ctx,
		`INSERT INTO memory_imports (source, filename, status, started_at) VALUES (?, ?, 'running', ?)`,
		source, filename, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (o *Orchestrator) recordComplete(ctx context.Context, id int64, r *ImportResult, status, errMsg string) {
	_, err := o.db.ExecContext(ctx,
		`UPDATE memory_imports SET status = ?, thoughts_imported = ?, entities_extracted = ?,
		 error_message = ?, completed_at = ? WHERE id = ?`,
		status, r.ThoughtsImported, r.EntitiesExtracted,
		nullString(errMsg), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		log.Printf("migration: recordComplete: %v", err)
	}
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// thoughtKey is the dedup key for a thought: a hash of its normalized source and
// content. It applies SaveThought's empty-source -> "user" normalization so keys
// are stable between the saver and previously stored rows across re-imports.
func thoughtKey(source, content string) string {
	if source == "" {
		source = "user"
	}
	h := sha256.Sum256([]byte(source + "\x00" + content))
	return string(h[:])
}

// loadThoughtKeys returns the dedup keys of all stored thoughts, for idempotent
// re-import. Keys are hashes, so memory is bounded regardless of content size.
func (o *Orchestrator) loadThoughtKeys(ctx context.Context) (map[string]struct{}, error) {
	// Dedup within the importing bank only: an identical scrap of text in a
	// worker bank must not suppress importing the user's real thought.
	rows, err := o.db.QueryContext(ctx,
		`SELECT content, COALESCE(source, '') FROM captured_thoughts WHERE bank = ?`,
		bank.FromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var content, source string
		if err := rows.Scan(&content, &source); err != nil {
			return nil, err
		}
		seen[thoughtKey(source, content)] = struct{}{}
	}
	return seen, rows.Err()
}
