package migration_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/migration"
	"github.com/LumabyteCo/aibutler/testutil"
)

// fakeImporter yields a fixed list of thoughts, ignoring the reader.
type fakeImporter struct{ thoughts []string }

func (f *fakeImporter) Name() string { return "fake" }

func (f *fakeImporter) Parse(ctx context.Context, _ io.Reader, save migration.SaveFunc) error {
	for _, t := range f.thoughts {
		if err := save(ctx, t, "fake", nil); err != nil {
			return err
		}
	}
	return nil
}

func TestImportDedupIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := testutil.TestDB(t)
	conn := db.Conn()
	orch := migration.NewOrchestrator(conn, memory.NewStore(conn), entity.NewStore(conn))

	// "alpha" repeats within the import — the second occurrence is skipped.
	imp := &fakeImporter{thoughts: []string{"alpha", "beta", "alpha"}}

	r1, err := orch.Run(ctx, imp, strings.NewReader(""), migration.ImportOpts{Dedup: true})
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if r1.ThoughtsImported != 2 || r1.Skipped != 1 {
		t.Errorf("run 1: imported=%d skipped=%d, want 2/1", r1.ThoughtsImported, r1.Skipped)
	}

	// Re-importing the identical export stores nothing new.
	r2, err := orch.Run(ctx, imp, strings.NewReader(""), migration.ImportOpts{Dedup: true})
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if r2.ThoughtsImported != 0 || r2.Skipped != 3 {
		t.Errorf("run 2 (re-import): imported=%d skipped=%d, want 0/3", r2.ThoughtsImported, r2.Skipped)
	}

	mem := memory.NewStore(conn)
	if n, _ := mem.ThoughtCount(ctx); n != 2 {
		t.Errorf("thought count = %d, want 2 (no duplicates persisted)", n)
	}
}

func TestImportWithoutDedupKeepsDuplicates(t *testing.T) {
	ctx := context.Background()
	db := testutil.TestDB(t)
	conn := db.Conn()
	orch := migration.NewOrchestrator(conn, memory.NewStore(conn), entity.NewStore(conn))

	imp := &fakeImporter{thoughts: []string{"x", "x"}}
	r, err := orch.Run(ctx, imp, strings.NewReader(""), migration.ImportOpts{}) // Dedup off (default)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if r.ThoughtsImported != 2 || r.Skipped != 0 {
		t.Errorf("imported=%d skipped=%d, want 2/0 (dedup disabled)", r.ThoughtsImported, r.Skipped)
	}
}
