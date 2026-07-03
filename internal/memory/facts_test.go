package memory_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/testutil"
)

// --- extraction keys ---

func TestExtractSingleValuedFactsCarryKeys(t *testing.T) {
	cases := []struct {
		text    string
		wantKey string
	}{
		{"My name is Ahmed.", "user.name"},
		{"I live in Cairo.", "user.location"},
		{"I work at LumaByte.", "user.employer"},
		{"I'm 35 years old", "user.age"},
		{"My favorite editor is Vim.", "user.favorite.editor"},
		{"My favorite coffee shop is Beanhouse.", "user.favorite.coffee_shop"},
	}
	for _, c := range cases {
		facts := memory.ExtractKeyFacts(c.text)
		if len(facts) == 0 {
			t.Errorf("%q: no facts extracted", c.text)
			continue
		}
		if facts[0].Key != c.wantKey {
			t.Errorf("%q: key = %q, want %q", c.text, facts[0].Key, c.wantKey)
		}
	}
}

func TestExtractMultiValuedFactsHaveNoKey(t *testing.T) {
	for _, text := range []string{
		"I like tea.",
		"I prefer tabs over spaces.",
		"I've decided to ship on Friday.",
		"I'm working on the garden shed.",
	} {
		facts := memory.ExtractKeyFacts(text)
		if len(facts) == 0 {
			t.Errorf("%q: no facts extracted", text)
			continue
		}
		if facts[0].Key != "" {
			t.Errorf("%q: key = %q, want empty (multi-valued)", text, facts[0].Key)
		}
	}
}

// --- conflict handling ---

func TestSaveFactSupersedesOnKeyConflict(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	oldID, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	})
	if err != nil {
		t.Fatalf("save old: %v", err)
	}
	newID, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Alexandria", Category: "identity", FactKey: "user.location",
	})
	if err != nil {
		t.Fatalf("save new: %v", err)
	}
	if newID == oldID {
		t.Fatalf("expected a new row, got the same id %d", oldID)
	}

	// Exactly one active holder of the key; the old fact is superseded, not gone.
	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 1 || facts[0].ID != newID {
		t.Fatalf("expected only the new fact active, got %+v", facts)
	}

	// The contradiction is on the ledger as a clear-case auto-supersede.
	conflicts, err := store.GetConflicts(ctx, false, 10)
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	c := conflicts[0]
	if c.OldFactID != oldID || c.NewFactID != newID || c.Resolution != memory.ResolutionAutoSupersede {
		t.Errorf("conflict = %+v, want old=%d new=%d auto_supersede", c, oldID, newID)
	}
}

func TestSaveFactFlagsLowConfidenceReplacementForReview(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User works at LumaByte", Category: "identity",
		FactKey: "user.employer", Confidence: 0.95,
	}); err != nil {
		t.Fatalf("save old: %v", err)
	}
	// Markedly less confident replacement (e.g. inferred from tool output):
	// still wins (newest statement), but flagged for the user to review.
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User works at Initech", Category: "identity",
		FactKey: "user.employer", Confidence: memory.ConfidenceToolOutput,
	}); err != nil {
		t.Fatalf("save new: %v", err)
	}

	conflicts, err := store.GetConflicts(ctx, true, 10)
	if err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].Resolution != memory.ResolutionNeedsReview {
		t.Fatalf("expected one needs_review conflict, got %+v", conflicts)
	}
}

func TestSaveFactMultiValuedNeverConflicts(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	for _, fact := range []string{"User likes tea", "User likes coffee", "User likes hiking"} {
		if _, err := store.SaveFact(ctx, memory.FactInput{Fact: fact, Category: "preference"}); err != nil {
			t.Fatalf("save %q: %v", fact, err)
		}
	}
	facts, err := store.GetKeyFacts(ctx, "preference", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 3 {
		t.Errorf("expected 3 coexisting preferences, got %d", len(facts))
	}
	conflicts, _ := store.GetConflicts(ctx, false, 10)
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts for multi-valued facts, got %d", len(conflicts))
	}
}

func TestSaveFactReassertionBoostsConfidence(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id1, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity",
		FactKey: "user.location", Confidence: 0.75,
	})
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}
	id2, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo.", Category: "identity",
		FactKey: "user.location", Confidence: 0.75,
	})
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("re-assertion should dedup to the same row, got %d then %d", id1, id2)
	}
	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].Confidence <= 0.75 {
		t.Errorf("confidence = %v, want boosted above 0.75", facts[0].Confidence)
	}
	// No conflict recorded — same value is agreement, not contradiction.
	conflicts, _ := store.GetConflicts(ctx, false, 10)
	if len(conflicts) != 0 {
		t.Errorf("re-assertion must not create conflicts, got %d", len(conflicts))
	}
}

func TestReassertingSupersededValueCreatesFreshActiveFact(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	// Cairo → Alexandria → back to Cairo. The final state must be an active
	// "Cairo" fact (a fresh row), with the Alexandria fact superseded.
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Alexandria", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	finalID, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	})
	if err != nil {
		t.Fatal(err)
	}

	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ID != finalID || !strings.Contains(facts[0].Fact, "Cairo") {
		t.Fatalf("expected single active Cairo fact (id %d), got %+v", finalID, facts)
	}
	conflicts, _ := store.GetConflicts(ctx, false, 10)
	if len(conflicts) != 2 {
		t.Errorf("expected 2 recorded transitions, got %d", len(conflicts))
	}
}

// --- correction & deletion ---

func TestCorrectFactSupersedesWithDefinitiveConfidence(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	oldID, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User's name is Ahmad", Category: "identity", FactKey: "user.name",
	})
	if err != nil {
		t.Fatal(err)
	}
	newID, err := store.CorrectFact(ctx, oldID, "User's name is Ahmed")
	if err != nil {
		t.Fatalf("correct: %v", err)
	}

	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ID != newID {
		t.Fatalf("expected only the correction active, got %+v", facts)
	}
	if facts[0].Confidence != 1.0 {
		t.Errorf("corrected fact confidence = %v, want 1.0", facts[0].Confidence)
	}
	if facts[0].FactKey != "user.name" {
		t.Errorf("correction must inherit the fact key, got %q", facts[0].FactKey)
	}
	if facts[0].Importance < 7 {
		t.Errorf("corrected fact importance = %d, want >= 7", facts[0].Importance)
	}

	conflicts, _ := store.GetConflicts(ctx, false, 10)
	if len(conflicts) != 1 || conflicts[0].Resolution != memory.ResolutionUserCorrected || !conflicts[0].Reviewed {
		t.Fatalf("expected one pre-reviewed user_corrected conflict, got %+v", conflicts)
	}
}

func TestForgetThoughtCascades(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ctx := context.Background()

	thoughtID, err := store.SaveThought(ctx, "My name is Ahmed. I live in Cairo.", "terminal", "s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Facts derived from the thought, with provenance pointing at it.
	for _, e := range memory.ExtractKeyFacts("My name is Ahmed. I live in Cairo.") {
		if _, err := store.SaveFact(ctx, memory.FactInput{
			Fact: e.Fact, Category: e.Category, FactKey: e.Key,
			SourceType: "thought", SourceID: thoughtID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A fake embedding for the thought.
	if _, err := db.Conn().ExecContext(ctx,
		`INSERT INTO memory_vectors (source_type, source_id, embedding, model, dimension)
		 VALUES ('thought', ?, X'00000000', 'test-model', 1)`, thoughtID); err != nil {
		t.Fatal(err)
	}

	res, err := store.ForgetThought(ctx, thoughtID)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if res.Facts != 2 || res.Vectors != 1 || res.Rows != 1 {
		t.Errorf("cascade = %+v, want 2 facts / 1 vector / 1 row", res)
	}

	// Everything derived is gone: facts, vectors, the row, and its FTS entry.
	var n int
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM key_facts WHERE source_type='thought' AND source_id=?`, thoughtID).Scan(&n); err != nil || n != 0 {
		t.Errorf("facts remaining = %d (err %v), want 0", n, err)
	}
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_vectors WHERE source_type='thought' AND source_id=?`, thoughtID).Scan(&n); err != nil || n != 0 {
		t.Errorf("vectors remaining = %d (err %v), want 0", n, err)
	}
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM captured_thoughts_fts WHERE captured_thoughts_fts MATCH 'Ahmed'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("FTS entries remaining = %d (err %v), want 0", n, err)
	}
}

func TestForgetFactCascadesConflictRows(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ctx := context.Background()

	oldID, _ := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	})
	newID, _ := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Alexandria", Category: "identity", FactKey: "user.location",
	})

	if err := store.ForgetFact(ctx, newID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	// The conflict row referencing the deleted fact cascades away; the old
	// fact row survives (still superseded — restoring is a separate decision).
	var n int
	if err := db.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_conflicts`).Scan(&n); err != nil || n != 0 {
		t.Errorf("conflicts remaining = %d (err %v), want 0", n, err)
	}
	if err := db.Conn().QueryRowContext(ctx, `SELECT COUNT(*) FROM key_facts WHERE id=?`, oldID).Scan(&n); err != nil || n != 1 {
		t.Errorf("old fact rows = %d (err %v), want 1", n, err)
	}
}

func TestForgetFactNotFound(t *testing.T) {
	store := newStore(t)
	if err := store.ForgetFact(context.Background(), 99999); err == nil {
		t.Fatal("expected error for missing fact")
	}
}

// --- pin / importance / access ---

func TestPinAndImportance(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, err := store.SaveFact(ctx, memory.FactInput{Fact: "User likes tea", Category: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PinFact(ctx, id, true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := store.SetFactImportance(ctx, id, 9); err != nil {
		t.Fatalf("importance: %v", err)
	}
	if err := store.SetFactImportance(ctx, id, 11); err == nil {
		t.Error("expected range error for importance 11")
	}
	if err := store.SetFactImportance(ctx, id, 0); err == nil {
		t.Error("expected range error for importance 0")
	}

	facts, err := store.GetKeyFacts(ctx, "preference", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || !facts[0].Pinned || facts[0].Importance != 9 {
		t.Errorf("got %+v, want pinned importance-9 fact", facts)
	}
}

func TestTouchFactAccess(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ctx := context.Background()

	id, err := store.SaveFact(ctx, memory.FactInput{Fact: "User likes tea", Category: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.TouchFactAccess(ctx, []int64{id, id}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	var count int
	var last string
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT access_count, COALESCE(last_accessed,'') FROM key_facts WHERE id=?`, id).Scan(&count, &last); err != nil {
		t.Fatal(err)
	}
	if count != 2 || last == "" {
		t.Errorf("access_count=%d last_accessed=%q, want 2 and non-empty", count, last)
	}
}

// --- retraction visibility ---

func TestRetractedFactExcludedEverywhere(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id, err := store.SaveFact(ctx, memory.FactInput{Fact: "User likes tea", Category: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RetractFact(ctx, id); err != nil {
		t.Fatalf("retract: %v", err)
	}
	facts, err := store.GetKeyFacts(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Errorf("retracted fact still returned: %+v", facts)
	}
	// And dedup ignores it: saving the same text again creates a fresh active row.
	newID, err := store.SaveFact(ctx, memory.FactInput{Fact: "User likes tea", Category: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if newID == id {
		t.Error("dedup resurrected a retracted fact; expected a fresh row")
	}
}

// --- regression tests from adversarial review ---

// A correction issued against a stale row (the panel was open while the agent
// superseded the fact) must still leave exactly one active holder of the key.
func TestCorrectFactOnStaleSupersededRowKeepsInvariant(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	cairoID, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Alexandria", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	// User corrects the (now superseded) Cairo row from a stale view.
	gizaID, err := store.CorrectFact(ctx, cairoID, "User lives in Giza")
	if err != nil {
		t.Fatalf("correct on stale row: %v", err)
	}

	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ID != gizaID {
		t.Fatalf("one-active-per-key violated: %+v", facts)
	}
}

// A correction of a stale KEYLESS fact must fail loudly instead of quietly
// resurrecting a replaced value.
func TestCorrectFactOnStaleKeylessRowFails(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ctx := context.Background()

	id, err := store.SaveFact(ctx, memory.FactInput{Fact: "User likes tea", Category: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RetractFact(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CorrectFact(ctx, id, "User likes green tea"); err == nil {
		t.Fatal("expected error correcting a non-active keyless fact")
	}
}

// Concurrent identical saves must dedup to one row and record no
// self-supersede "conflicts" — the dedup scan runs inside the transaction.
func TestSaveFactConcurrentIdenticalSaves(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.SaveFact(ctx, memory.FactInput{
				Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 deduped fact, got %d", len(facts))
	}
	conflicts, _ := store.GetConflicts(ctx, false, 10)
	if len(conflicts) != 0 {
		t.Errorf("identical concurrent saves recorded %d spurious conflict(s): %+v", len(conflicts), conflicts)
	}
}

// A queued embedding job that lands after the source row was forgotten must
// not resurrect the embedding.
func TestUpsertAfterForgetDoesNotResurrectEmbedding(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	vstore := vector.NewStore(db.Conn())
	ctx := context.Background()

	id, err := store.SaveThought(ctx, "secret plans", "terminal", "s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ForgetThought(ctx, id); err != nil {
		t.Fatal(err)
	}
	// The late async job fires now.
	if err := vstore.Upsert(ctx, "thought", id, []float32{0.1, 0.2}, "test-model"); err != nil {
		t.Fatalf("late upsert should be a silent no-op, got: %v", err)
	}
	var n int
	if err := db.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_vectors WHERE source_type='thought' AND source_id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("forgotten thought's embedding was resurrected (%d rows)", n)
	}
}

// Restore rejects a flagged replacement: old value active again, new value
// retracted, conflict closed as user_restored.
func TestRestoreConflict(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	oldID, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User works at LumaByte", Category: "identity",
		FactKey: "user.employer", Confidence: 0.95,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User works at Initech", Category: "identity",
		FactKey: "user.employer", Confidence: memory.ConfidenceToolOutput,
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.GetConflicts(ctx, true, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected 1 pending conflict, got %d (err %v)", len(pending), err)
	}

	if err := store.RestoreConflict(ctx, pending[0].ID); err != nil {
		t.Fatalf("restore: %v", err)
	}

	facts, err := store.GetKeyFacts(ctx, "identity", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ID != oldID {
		t.Fatalf("expected old fact restored as sole active, got %+v", facts)
	}
	// Queue is empty; ledger closed as user_restored.
	pending, _ = store.GetConflicts(ctx, true, 10)
	if len(pending) != 0 {
		t.Errorf("review queue not emptied: %+v", pending)
	}
	all, _ := store.GetConflicts(ctx, false, 10)
	if len(all) != 1 || all[0].Resolution != memory.ResolutionUserRestored {
		t.Errorf("expected user_restored on the ledger, got %+v", all)
	}
}

// The review queue only carries needs_review rows — routine auto-supersede
// history must not crowd it.
func TestPendingConflictsExcludeAutoSupersede(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact: "User lives in Alexandria", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.GetConflicts(ctx, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("auto_supersede rows leaked into the review queue: %+v", pending)
	}
	all, _ := store.GetConflicts(ctx, false, 10)
	if len(all) != 1 {
		t.Errorf("ledger should still hold the auto_supersede record, got %d", len(all))
	}
}
