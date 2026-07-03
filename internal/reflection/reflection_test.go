package reflection_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/digest"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/reflection"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestMaintenanceReportsConflictsAndStaleness(t *testing.T) {
	db := testutil.TestDB(t)
	mem := memory.NewStore(db.Conn())
	digests := digest.NewGenerator(db.Conn(), mem, entity.NewStore(db.Conn()), graph.NewStore(db.Conn()))
	ctx := context.Background()

	// A flagged contradiction (low-confidence replacement) waits for review.
	if _, err := mem.SaveFact(ctx, memory.FactInput{
		Fact: "User works at LumaByte", Category: "identity",
		FactKey: "user.employer", Confidence: 0.95,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.SaveFact(ctx, memory.FactInput{
		Fact: "User works at Initech", Category: "identity",
		FactKey: "user.employer", Confidence: memory.ConfidenceToolOutput,
	}); err != nil {
		t.Fatal(err)
	}

	// A stale high-importance fact: important, never accessed, extracted long ago.
	old := time.Now().UTC().Add(-120 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := db.Conn().ExecContext(ctx,
		`INSERT INTO key_facts (fact, category, extracted_at, importance) VALUES ('User ships on Fridays', 'decision', ?, 9)`,
		old); err != nil {
		t.Fatal(err)
	}

	m := reflection.New(db.Conn(), mem, digests, nil)
	summary, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(summary, "1 review item") || !strings.Contains(summary, "1 stale flag") {
		t.Fatalf("summary = %q, want 1 review item and 1 stale flag", summary)
	}

	// The report persisted as a maintenance digest with the substance in it.
	saved, err := digests.List(ctx, reflection.MaintenanceDigest, 5)
	if err != nil || len(saved) != 1 {
		t.Fatalf("digests = %d (err %v), want 1", len(saved), err)
	}
	content := saved[0].Content
	if !strings.Contains(content, "Initech") || !strings.Contains(content, "Fridays") {
		t.Fatalf("report missing substance:\n%s", content)
	}
}

func TestMaintenanceQuietWhenHealthy(t *testing.T) {
	db := testutil.TestDB(t)
	mem := memory.NewStore(db.Conn())
	digests := digest.NewGenerator(db.Conn(), mem, entity.NewStore(db.Conn()), graph.NewStore(db.Conn()))
	ctx := context.Background()

	m := reflection.New(db.Conn(), mem, digests, nil)
	if _, err := m.Run(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := digests.List(ctx, reflection.MaintenanceDigest, 5)
	if len(saved) != 1 || !strings.Contains(saved[0].Content, "Nothing needs attention") {
		t.Fatalf("healthy pass should report nothing needing attention: %+v", saved)
	}
}

type fakeBackfill struct{ embedded, failed int }

func (f *fakeBackfill) Run(context.Context) (int, int, error) { return f.embedded, f.failed, nil }

func TestMaintenanceIncludesBackfill(t *testing.T) {
	db := testutil.TestDB(t)
	mem := memory.NewStore(db.Conn())
	digests := digest.NewGenerator(db.Conn(), mem, entity.NewStore(db.Conn()), graph.NewStore(db.Conn()))
	ctx := context.Background()

	m := reflection.New(db.Conn(), mem, digests, &fakeBackfill{embedded: 7, failed: 1})
	if _, err := m.Run(ctx); err != nil {
		t.Fatal(err)
	}
	saved, _ := digests.List(ctx, reflection.MaintenanceDigest, 5)
	if len(saved) != 1 || !strings.Contains(saved[0].Content, "7 item(s) embedded, 1 failed") {
		t.Fatalf("backfill result missing from report: %+v", saved)
	}
}
