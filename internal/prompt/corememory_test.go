package prompt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/testutil"
)

func TestCoreFactScoreOrdering(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-120 * 24 * time.Hour).Format(time.RFC3339)
	fresh := now.Add(-1 * time.Hour).Format(time.RFC3339)

	// A pinned fact beats everything.
	pinned := coreFactScore(now, 1, 0, true, old, "")
	important := coreFactScore(now, 10, 50, false, fresh, fresh)
	if pinned <= important {
		t.Errorf("pinned=%v should beat max unpinned=%v", pinned, important)
	}

	// An important, frequently used old fact beats a fresh low-importance one:
	// the user's name from January outranks yesterday's throwaway remark.
	name := coreFactScore(now, 9, 40, false, old, now.Add(-24*time.Hour).Format(time.RFC3339))
	throwaway := coreFactScore(now, 3, 0, false, fresh, "")
	if name <= throwaway {
		t.Errorf("important+used old fact %v should beat fresh low-importance %v", name, throwaway)
	}

	// All else equal, fresher wins.
	freshMid := coreFactScore(now, 5, 0, false, fresh, "")
	oldMid := coreFactScore(now, 5, 0, false, old, "")
	if freshMid <= oldMid {
		t.Errorf("fresh %v should beat old %v at equal importance", freshMid, oldMid)
	}

	// last_accessed refreshes recency even when extraction is old.
	usedRecently := coreFactScore(now, 5, 5, false, old, fresh)
	if usedRecently <= oldMid {
		t.Errorf("recently used old fact %v should beat untouched old fact %v", usedRecently, oldMid)
	}
}

func TestLoadCoreFactsScoredSelection(t *testing.T) {
	db := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	c := NewComposer(cfg, nil, nil, db.Conn())
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour).Format(time.RFC3339)
	fresh := now.Format(time.RFC3339)

	seed := func(fact string, importance, access, pinned int, extractedAt string) {
		t.Helper()
		if _, err := db.Conn().ExecContext(ctx,
			`INSERT INTO key_facts (fact, category, extracted_at, importance, access_count, pinned)
			 VALUES (?, 'identity', ?, ?, ?, ?)`, fact, extractedAt, importance, access, pinned); err != nil {
			t.Fatal(err)
		}
	}
	seed("User's name is Ahmed", 9, 30, 0, old)      // important, much used, old
	seed("User mentioned the weather", 2, 0, 0, fresh) // fresh noise
	seed("User ships on Fridays", 5, 0, 1, old)        // pinned, old

	// Superseded facts never appear regardless of score.
	if _, err := db.Conn().ExecContext(ctx,
		`INSERT INTO key_facts (fact, category, extracted_at, importance, status)
		 VALUES ('User lives in Alexandria', 'identity', ?, 10, 'superseded')`, fresh); err != nil {
		t.Fatal(err)
	}

	facts := c.loadCoreFacts(ctx, 350)
	if len(facts) != 3 {
		t.Fatalf("expected 3 active facts, got %d: %v", len(facts), facts)
	}
	// Pinned first, then the important old fact, then the fresh noise.
	if !strings.Contains(facts[0], "Fridays") {
		t.Errorf("pinned fact should rank first, got %q", facts[0])
	}
	if !strings.Contains(facts[1], "Ahmed") {
		t.Errorf("important+used fact should rank second, got %q", facts[1])
	}
	for _, f := range facts {
		if strings.Contains(f, "Alexandria") {
			t.Errorf("superseded fact leaked into core memory: %q", f)
		}
	}
}

func TestLoadCoreFactsRespectsTokenBudget(t *testing.T) {
	db := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	c := NewComposer(cfg, nil, nil, db.Conn())
	ctx := context.Background()

	long := strings.Repeat("very important detail ", 8) // ~50 tokens each
	for i := 0; i < 20; i++ {
		if _, err := db.Conn().ExecContext(ctx,
			`INSERT INTO key_facts (fact, category, importance) VALUES (?, 'identity', 8)`,
			long+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}

	budget := 120
	facts := c.loadCoreFacts(ctx, budget)
	if len(facts) == 0 {
		t.Fatal("budget should admit at least one fact")
	}
	total := 0
	for _, f := range facts {
		total += estimateTokens(f) + 1
	}
	if total > budget {
		t.Errorf("selected %d facts totaling ~%d tokens, budget %d", len(facts), total, budget)
	}
	if len(facts) >= 20 {
		t.Errorf("budget failed to bound selection: %d facts admitted", len(facts))
	}
}

func TestComposeUsesScoredSelectionByDefaultAndLegacyOnRequest(t *testing.T) {
	db := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour).Format(time.RFC3339)

	// 12 fresh low-importance facts + 1 old pinned fact. Legacy top-10 recency
	// drops the pinned fact; scored selection must keep it.
	for i := 0; i < 12; i++ {
		if _, err := db.Conn().ExecContext(ctx,
			`INSERT INTO key_facts (fact, category, importance) VALUES (?, 'preference', 3)`,
			"User likes flavor number "+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Conn().ExecContext(ctx,
		`INSERT INTO key_facts (fact, category, extracted_at, importance, pinned)
		 VALUES ('User ships on Fridays', 'decision', ?, 8, 1)`, old); err != nil {
		t.Fatal(err)
	}

	cfg.Options.Prompts.CoreMemorySelection = "scored"
	c := NewComposer(cfg, nil, nil, db.Conn())
	tier1, err := c.buildTier1(ctx, "terminal", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tier1, "Fridays") {
		t.Error("scored selection dropped the pinned fact")
	}

	cfg.Options.Prompts.CoreMemorySelection = "recency"
	cLegacy := NewComposer(cfg, nil, nil, db.Conn())
	tier1Legacy, err := cLegacy.buildTier1(ctx, "terminal", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tier1Legacy, "Fridays") {
		t.Error("legacy recency mode unexpectedly included the old pinned fact — selection flag not honored")
	}
}

func TestWorkingStateInjection(t *testing.T) {
	db := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	c := NewComposer(cfg, nil, nil, db.Conn())
	ctx := context.Background()

	c.SetWorkingState(func(ctx context.Context, sessionID string) string {
		if sessionID == "s1" {
			return `active task "trip_planning" (awaiting_input)`
		}
		return ""
	})

	tier1, err := c.buildTier1(ctx, "terminal", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tier1, "Working state: active task") {
		t.Errorf("working state missing from Tier 1:\n%s", tier1)
	}

	other, err := c.buildTier1(ctx, "terminal", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(other, "Working state:") {
		t.Error("empty working state must not inject a header")
	}
}
