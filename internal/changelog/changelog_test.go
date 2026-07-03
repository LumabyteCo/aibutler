package changelog_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/changelog"
	"github.com/LumabyteCo/aibutler/testutil"
)

type captureAuditor struct {
	entries []capability.AuditEntry
}

func (c *captureAuditor) LogAccess(_ context.Context, e capability.AuditEntry) error {
	c.entries = append(c.entries, e)
	return nil
}

func TestRecordAndListMirrorsAudit(t *testing.T) {
	db := testutil.TestDB(t)
	aud := &captureAuditor{}
	ledger := changelog.New(db.Conn(), aud)
	ctx := context.Background()

	id, err := ledger.Record(ctx, changelog.Entry{
		Kind:       changelog.KindSkillCreated,
		SubjectID:  "skill:weekly-report",
		AfterHash:  "abc123",
		ApprovedBy: "user",
	})
	if err != nil || id == 0 {
		t.Fatalf("record: id=%d err=%v", id, err)
	}

	entries, err := ledger.List(ctx, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("list: %d entries, err %v", len(entries), err)
	}
	e := entries[0]
	if e.Kind != changelog.KindSkillCreated || e.SubjectID != "skill:weekly-report" ||
		e.AfterHash != "abc123" || e.ApprovedBy != "user" || e.CreatedAt == "" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.EvalRunID != 0 {
		t.Errorf("eval_run_id should be 0 when unset, got %d", e.EvalRunID)
	}

	// Mirrored into the audit trail: one query surface for all self-changes.
	if len(aud.entries) != 1 || aud.entries[0].Action != changelog.KindSkillCreated ||
		aud.entries[0].Target != "skill:weekly-report" {
		t.Fatalf("audit mirror missing/wrong: %+v", aud.entries)
	}
}

func TestRecordValidation(t *testing.T) {
	db := testutil.TestDB(t)
	ledger := changelog.New(db.Conn(), nil)
	if _, err := ledger.Record(context.Background(), changelog.Entry{Kind: "", SubjectID: "x"}); err == nil {
		t.Fatal("expected error for missing kind")
	}
	if _, err := ledger.Record(context.Background(), changelog.Entry{Kind: "k", SubjectID: ""}); err == nil {
		t.Fatal("expected error for missing subject")
	}
}

func TestEvalRunLink(t *testing.T) {
	db := testutil.TestDB(t)
	ledger := changelog.New(db.Conn(), nil)
	ctx := context.Background()

	// A real eval run row to link against (FK).
	res, err := db.Conn().ExecContext(ctx,
		`INSERT INTO eval_runs (suite_hash, mode) VALUES ('h', 'unit')`)
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := res.LastInsertId()

	if _, err := ledger.Record(ctx, changelog.Entry{
		Kind: changelog.KindSkillPatched, SubjectID: "skill:x", EvalRunID: runID,
	}); err != nil {
		t.Fatalf("record with eval link: %v", err)
	}
	entries, _ := ledger.List(ctx, 1)
	if len(entries) != 1 || entries[0].EvalRunID != runID {
		t.Fatalf("eval link not preserved: %+v", entries)
	}
}
