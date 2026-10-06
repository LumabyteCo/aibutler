package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/testutil"
)

// TestExtractMovedToLocation guards FINDING-01: "I moved to Tokyo"
// must extract with fact_key=user.location — the same key "I live in X"
// uses — so SaveFact sees the conflict and supersedes the old location
// instead of accumulating both cities.
func TestExtractMovedToLocation(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantHas  string
		wantKey  string
	}{
		{"moved to", "I moved to Tokyo, Japan", "Tokyo", "user.location"},
		{"moved with period-only", "I moved to Tokyo.", "Tokyo", "user.location"},
		{"now living in", "I'm now living in Tokyo", "Tokyo", "user.location"},
		{"now live in", "I now live in Tokyo", "Tokyo", "user.location"},
		{"my home city is", "My home city is Tokyo", "Tokyo", "user.location"},
		{"have moved", "I have moved to Paris", "Paris", "user.location"},
		{"third person moved", "Sam moved to Tokyo, Japan", "Tokyo", "user.location"},
		{"third person lives", "User lives in Berlin", "Berlin", "user.location"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results := memory.ExtractKeyFacts(tc.in)
			if len(results) == 0 {
				t.Fatalf("no extraction for %q", tc.in)
			}
			found := false
			for _, r := range results {
				if r.Key == tc.wantKey && strings.Contains(r.Fact, tc.wantHas) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected fact key %q containing %q for %q, got %v", tc.wantKey, tc.wantHas, tc.in, results)
			}
		})
	}
}

// TestSaveFactSupersedesOnLocationChange is the end-to-end contradiction
// test: Berlin first, then "moved to Tokyo" — the Berlin fact must be
// superseded and Tokyo must be the only ACTIVE location fact.
func TestSaveFactSupersedesOnLocationChange(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()
	store := memory.NewStore(conn)

	// 1. User states Berlin (the pattern the old extractor caught).
	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact:    "User lives in Berlin",
		Category: memory.CategoryIdentity,
		FactKey: "user.location",
		SourceType: "thought",
	}); err != nil {
		t.Fatalf("save Berlin: %v", err)
	}

	// Sanity: one active location fact.
	var activeBefore int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM key_facts WHERE fact_key='user.location' AND status='active'`).Scan(&activeBefore); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if activeBefore != 1 {
		t.Fatalf("active before = %d, want 1", activeBefore)
	}

	// 2. User announces the move — the NEW pattern must extract with the
	// same key so SaveFact supersedes Berlin.
	extracted := memory.ExtractKeyFacts("I moved to Tokyo")
	if len(extracted) == 0 {
		t.Fatal("moved-to pattern produced no fact")
	}
	var tokyoFact memory.ExtractionResult
	for _, r := range extracted {
		if r.Key == "user.location" {
			tokyoFact = r
			break
		}
	}
	if tokyoFact.Fact == "" {
		t.Fatalf("no user.location fact from 'I moved to Tokyo': %+v", extracted)
	}

	if _, err := store.SaveFact(ctx, memory.FactInput{
		Fact:    tokyoFact.Fact,
		Category: memory.CategoryIdentity,
		FactKey: tokyoFact.Key,
		SourceType: "thought",
		Confidence: memory.ConfidenceUserStated,
	}); err != nil {
		t.Fatalf("save Tokyo: %v", err)
	}

	// 3. Invariant: at most one ACTIVE user.location fact.
	var activeAfter int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM key_facts WHERE fact_key='user.location' AND status='active'`).Scan(&activeAfter); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if activeAfter != 1 {
		t.Errorf("active location facts = %d, want 1 — two active location facts means contradictions accumulate", activeAfter)
	}

	// 4. The surviving active fact is Tokyo; Berlin is superseded-but-kept.
	var activeFact string
	if err := conn.QueryRowContext(ctx,
		`SELECT fact FROM key_facts WHERE fact_key='user.location' AND status='active'`).Scan(&activeFact); err != nil {
		t.Fatalf("read active: %v", err)
	}
	if !strings.Contains(activeFact, "Tokyo") {
		t.Errorf("active location fact = %q, want Tokyo", activeFact)
	}

	// 5. The conflict was recorded.
	var conflicts int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_conflicts WHERE fact_key='user.location'`).Scan(&conflicts); err != nil {
		t.Fatalf("conflicts: %v", err)
	}
	if conflicts != 1 {
		t.Errorf("memory_conflicts rows = %d, want 1", conflicts)
	}
}