package registry_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/protocol/a2a/registry"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestQuarantineAfterConsecutiveFailures(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	// Register an agent.
	if err := reg.Register(ctx, "flaky-agent", "http://flaky:8080", []string{"nlp"}, ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Mark 4 failures — should not quarantine yet.
	for i := 0; i < 4; i++ {
		if err := reg.MarkFailure(ctx, "flaky-agent"); err != nil {
			t.Fatalf("mark failure %d: %v", i+1, err)
		}
	}

	// Should still be discoverable (4 consecutive failures < 5).
	records, err := reg.Discover(ctx, "nlp")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	// Note: with 4 failures and 0 successes, the ratio filter may exclude it.
	// But quarantine should not be set yet.
	all, _ := reg.DiscoverAll(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 record in DiscoverAll, got %d", len(all))
	}
	if all[0].Quarantined {
		t.Error("should not be quarantined after 4 failures")
	}
	_ = records

	// 5th failure should trigger quarantine.
	if err := reg.MarkFailure(ctx, "flaky-agent"); err != nil {
		t.Fatalf("mark failure 5: %v", err)
	}

	// Should now be quarantined and excluded from Discover.
	records, err = reg.Discover(ctx, "nlp")
	if err != nil {
		t.Fatalf("discover after quarantine: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("quarantined agent should not appear in Discover, got %d", len(records))
	}

	// DiscoverAll should still show it.
	all, err = reg.DiscoverAll(ctx)
	if err != nil {
		t.Fatalf("discover all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 in DiscoverAll, got %d", len(all))
	}
	if !all[0].Quarantined {
		t.Error("expected quarantined=true")
	}
	if all[0].ConsecutiveFailures < 5 {
		t.Errorf("expected >= 5 consecutive failures, got %d", all[0].ConsecutiveFailures)
	}
}

func TestHeartbeatUnquarantines(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	if err := reg.Register(ctx, "recovering-agent", "http://rec:8080", []string{"search"}, ""); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Quarantine manually.
	if err := reg.Quarantine(ctx, "recovering-agent"); err != nil {
		t.Fatalf("quarantine: %v", err)
	}

	// Verify quarantined.
	records, _ := reg.Discover(ctx, "search")
	if len(records) != 0 {
		t.Error("quarantined agent should not appear in Discover")
	}

	// Heartbeat should unquarantine.
	if err := reg.Heartbeat(ctx, "recovering-agent"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	records, _ = reg.Discover(ctx, "search")
	if len(records) != 1 {
		t.Errorf("after heartbeat, expected 1 record, got %d", len(records))
	}
}
