package memory_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
)

// TestSaveKeyFactDedupesWhenStoredHasPunctuation covers the case the old
// LOWER(TRIM(fact)) comparison silently missed: the FIRST stored fact carries
// trailing punctuation / doubled whitespace, so its LOWER(TRIM) form did not
// equal a later variant's canonical form and a duplicate slipped in. The
// existing TestSaveKeyFactDedupes stores the clean variant first, which masked
// the bug. Here we store the punctuated form first.
func TestSaveKeyFactDedupesWhenStoredHasPunctuation(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	id1, err := store.SaveKeyFact(ctx, "User  prefers  dark mode.", "preference", "s1")
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}
	// A clean canonical variant must dedup to the same row.
	id2, err := store.SaveKeyFact(ctx, "user prefers dark mode", "preference", "s2")
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected dedup to the same id, got %d then %d", id1, id2)
	}
	facts, err := store.GetKeyFacts(ctx, "preference", 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("expected 1 fact row after dedup, got %d: %v", len(facts), facts)
	}
}

// TestExtractRepeatedSameRule verifies a single rule matching several times in
// one text yields a fact per match — the FindAllStringSubmatch fix. The old
// FindStringSubmatch returned only the first ("tea"), dropping "coffee".
func TestExtractRepeatedSameRule(t *testing.T) {
	facts := memory.ExtractKeyFacts("I like tea. I like coffee.")
	got := make(map[string]bool)
	for _, f := range facts {
		got[f.Fact] = true
	}
	if !got["User likes tea"] || !got["User likes coffee"] {
		t.Errorf("expected both 'User likes tea' and 'User likes coffee', got %v", facts)
	}
}
