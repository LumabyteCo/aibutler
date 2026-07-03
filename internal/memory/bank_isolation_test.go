package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/bank"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/testutil"
)

// A worker bank must not see the primary bank's memory through ANY read
// path, and its writes must not land in the primary bank.
func TestBankIsolationAcrossStores(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ents := entity.NewStore(db.Conn())
	ftsStore := fts.NewStore(db.Conn())

	main := context.Background() // unset ctx = default bank
	worker := bank.With(context.Background(), "swarm")

	// Primary-bank content.
	if _, err := store.SaveThought(main, "the launch password is stored in the vault", "user", "s1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveFact(main, memory.FactInput{
		Fact: "User lives in Cairo", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ents.SaveOrUpdate(main, entity.TypePerson, "Sarah", "s1", nil); err != nil {
		t.Fatal(err)
	}

	// Worker-bank content.
	if _, err := store.SaveThought(worker, "subtask scratch note about the launch", "agent", "sub1", nil); err != nil {
		t.Fatal(err)
	}

	// 1. Thought listing: each bank sees only its own.
	mainThoughts, err := store.GetThoughts(main, memory.ThoughtQuery{})
	if err != nil || len(mainThoughts) != 1 {
		t.Fatalf("main thoughts = %d (err %v), want 1", len(mainThoughts), err)
	}
	workerThoughts, err := store.GetThoughts(worker, memory.ThoughtQuery{})
	if err != nil || len(workerThoughts) != 1 || workerThoughts[0].Source != "agent" {
		t.Fatalf("worker thoughts = %+v (err %v), want only its own", workerThoughts, err)
	}

	// 2. Counts are per-bank.
	if n, _ := store.ThoughtCount(worker); n != 1 {
		t.Fatalf("worker thought count = %d, want 1", n)
	}

	// 3. Full-text search cannot cross banks — the worker searching for the
	// primary bank's content finds nothing.
	hits, err := ftsStore.SearchAll(worker, "password vault", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Source == "thought" && h.ID == mainThoughts[0].ID {
			t.Fatal("worker bank retrieved a primary-bank thought via full-text search")
		}
	}
	if hits, _ := ftsStore.SearchAll(main, "password", 10); len(hits) != 1 {
		t.Fatalf("primary bank should find its own thought, got %d hits", len(hits))
	}

	// 4. Facts are per-bank, and a worker's fact with the same key does NOT
	// supersede the primary bank's fact.
	if _, err := store.SaveFact(worker, memory.FactInput{
		Fact: "User lives in Alexandria", Category: "identity", FactKey: "user.location",
	}); err != nil {
		t.Fatal(err)
	}
	mainFacts, err := store.GetKeyFacts(main, "identity", 10)
	if err != nil || len(mainFacts) != 1 || mainFacts[0].Fact != "User lives in Cairo" {
		t.Fatalf("primary fact affected by worker write: %+v (err %v)", mainFacts, err)
	}
	if mainFacts[0].Status != memory.FactStatusActive {
		t.Fatal("primary fact was superseded by a worker-bank write")
	}
	workerFacts, _ := store.GetKeyFacts(worker, "identity", 10)
	if len(workerFacts) != 1 || workerFacts[0].Fact != "User lives in Alexandria" {
		t.Fatalf("worker facts wrong: %+v", workerFacts)
	}

	// 5. Entities are per-bank: the worker doesn't see Sarah, and saving the
	// same name creates a distinct row instead of bumping the primary's.
	if es, _ := ents.Search(worker, "Sarah", 10); len(es) != 0 {
		t.Fatalf("worker bank sees primary-bank entity: %+v", es)
	}
	if _, err := ents.SaveOrUpdate(worker, entity.TypePerson, "Sarah", "sub1", nil); err != nil {
		t.Fatal(err)
	}
	mainSarah, _ := ents.Search(main, "Sarah", 10)
	if len(mainSarah) != 1 || mainSarah[0].MentionCount != 1 {
		t.Fatalf("worker save mutated the primary bank's entity: %+v", mainSarah)
	}
}

// Existing rows (created before banks) belong to the default bank and stay
// fully visible on unscoped contexts — single-profile behavior is unchanged.
func TestDefaultBankBackCompat(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ctx := context.Background()

	if _, err := db.Conn().ExecContext(ctx,
		`INSERT INTO captured_thoughts (content, source) VALUES ('pre-banks row', 'user')`); err != nil {
		t.Fatal(err)
	}
	thoughts, err := store.GetThoughts(ctx, memory.ThoughtQuery{})
	if err != nil || len(thoughts) != 1 {
		t.Fatalf("default-bank row invisible on unscoped context: %d (err %v)", len(thoughts), err)
	}
	if bank.FromContext(ctx) != bank.Default {
		t.Fatal("unscoped context must resolve to the default bank")
	}
}

// The async embedding path stamps each vector with the SOURCE row's bank —
// captured at enqueue time — so semantic search cannot become a cross-bank
// channel even though the worker drains on a store-owned context. The same
// holds for the batched backfill.
func TestVectorBankStamping(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	vstore := vector.NewStore(db.Conn())
	worker := bank.With(context.Background(), "swarm")
	mainCtx := context.Background()

	// Async path: a worker-bank thought's embedding must land in the worker
	// bank. The indexer runs through the real queue + worker goroutine.
	store.SetIndexer(memory.NewVectorIndexer(vstore, func(_ context.Context, _ string) ([]float32, error) {
		return []float32{0.1, 0.2, 0.3}, nil
	}, "fake-embedder"))
	t.Cleanup(func() { store.Close() })

	id, err := store.SaveThought(worker, "worker secret plan", "agent", "sub1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the async job to land.
	deadline := time.Now().Add(5 * time.Second)
	var vBank string
	for time.Now().Before(deadline) {
		if err := db.Conn().QueryRowContext(mainCtx,
			`SELECT bank FROM memory_vectors WHERE source_type='thought' AND source_id=?`, id).Scan(&vBank); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if vBank != "swarm" {
		t.Fatalf("async-indexed vector bank = %q, want swarm", vBank)
	}

	// Main-bank KNN must not return the worker item.
	hits, err := vstore.Search(mainCtx, []float32{0.1, 0.2, 0.3}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.SourceType == "thought" && h.SourceID == id {
			t.Fatal("main-bank KNN returned a worker-bank embedding")
		}
	}
	// The worker's own KNN finds it.
	hits, err = vstore.Search(worker, []float32{0.1, 0.2, 0.3}, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("worker KNN hits = %d (err %v), want 1", len(hits), err)
	}
	// And hydration refuses cross-bank ids even if handed one.
	content, err := store.ResolveContent(mainCtx, "thought", []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatal("hydration leaked worker-bank content to the default bank")
	}
}

// Backfill stamps each vector with its source row's bank even though the
// maintenance context is unscoped.
func TestBackfillPreservesSourceBank(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	vstore := vector.NewStore(db.Conn())
	worker := bank.With(context.Background(), "swarm")
	mainCtx := context.Background()

	// One thought per bank, no indexer wired → no vectors yet.
	wid, err := store.SaveThought(worker, "worker note", "agent", "sub1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveThought(mainCtx, "user note", "user", "s1", nil); err != nil {
		t.Fatal(err)
	}

	bf := memory.NewBackfiller(db.Conn(), vstore, func(_ context.Context, texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{1, 0}
		}
		return out, nil
	}, "fake-embedder")
	res, err := bf.BackfillMissing(mainCtx) // unscoped maintenance context
	if err != nil || res.Embedded != 2 {
		t.Fatalf("backfill embedded = %d (err %v), want 2", res.Embedded, err)
	}

	var vBank string
	if err := db.Conn().QueryRowContext(mainCtx,
		`SELECT bank FROM memory_vectors WHERE source_type='thought' AND source_id=?`, wid).Scan(&vBank); err != nil {
		t.Fatal(err)
	}
	if vBank != "swarm" {
		t.Fatalf("backfilled vector bank = %q, want the source row's bank (swarm)", vBank)
	}
}

// Transcript FTS is bank-scoped too (the isolation test above covers thoughts).
func TestTranscriptFTSBankIsolation(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	ftsStore := fts.NewStore(db.Conn())
	worker := bank.With(context.Background(), "swarm")
	mainCtx := context.Background()

	if _, err := store.SaveTranscript(mainCtx, "s1", "user", "the quarterly numbers are confidential", 1); err != nil {
		t.Fatal(err)
	}
	hits, err := ftsStore.SearchTranscripts(worker, "confidential quarterly", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatal("worker bank retrieved a primary-bank transcript via FTS")
	}
	if hits, _ := ftsStore.SearchTranscripts(mainCtx, "confidential", 10); len(hits) != 1 {
		t.Fatalf("primary bank should find its own transcript, got %d", len(hits))
	}
}

// Id-addressed mutations refuse cross-bank targets: guessable integer ids
// must not be a side door around isolation.
func TestIdAddressedOpsRefuseCrossBank(t *testing.T) {
	db := testutil.TestDB(t)
	store := memory.NewStore(db.Conn())
	worker := bank.With(context.Background(), "swarm")
	mainCtx := context.Background()

	fid, err := store.SaveFact(mainCtx, memory.FactInput{Fact: "User lives in Cairo", Category: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := store.SaveThought(mainCtx, "primary-bank note", "user", "s1", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.ForgetFact(worker, fid); err == nil {
		t.Fatal("worker deleted a primary-bank fact by id")
	}
	if err := store.PinFact(worker, fid, true); err == nil {
		t.Fatal("worker pinned a primary-bank fact by id")
	}
	if _, err := store.CorrectFact(worker, fid, "poisoned"); err == nil {
		t.Fatal("worker corrected a primary-bank fact by id")
	}
	if _, err := store.ForgetThought(worker, tid); err == nil {
		t.Fatal("worker deleted a primary-bank thought by id")
	}
	// The rightful owner can still do all of it.
	if err := store.PinFact(mainCtx, fid, true); err != nil {
		t.Fatalf("owner pin failed: %v", err)
	}
	if _, err := store.ForgetThought(mainCtx, tid); err != nil {
		t.Fatalf("owner forget failed: %v", err)
	}
}
