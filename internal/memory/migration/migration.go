package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
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
}

// ImportResult summarizes an import run.
type ImportResult struct {
	ThoughtsImported  int
	EntitiesExtracted int
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

	saver := func(ctx context.Context, content, source string, tags []string) error {
		// Check context cancellation between items.
		if err := ctx.Err(); err != nil {
			return err
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

		// Extract entities — accumulate errors rather than swallowing.
		extracted := entity.Extract(content)
		count, entityErrs := o.saveEntities(ctx, extracted)
		result.EntitiesExtracted += count
		result.Errors = append(result.Errors, entityErrs...)

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

func (o *Orchestrator) saveEntities(ctx context.Context, extracted entity.Extracted) (int, []string) {
	count := 0
	var errs []string
	save := func(typ entity.Type, name string) {
		if _, err := o.entity.SaveOrUpdate(ctx, typ, name, "", nil); err != nil {
			errs = append(errs, fmt.Sprintf("entity %s %q: %s", typ, name, err))
		} else {
			count++
		}
	}
	for _, name := range extracted.People {
		save(entity.TypePerson, name)
	}
	for _, name := range extracted.Projects {
		save(entity.TypeProject, name)
	}
	for _, desc := range extracted.Decisions {
		save(entity.TypeDecision, desc)
	}
	for _, item := range extracted.ActionItems {
		save(entity.TypeActionItem, item)
	}
	for _, insight := range extracted.Insights {
		save(entity.TypeInsight, insight)
	}
	return count, errs
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
