package registry_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/protocol/a2a/registry"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestRegister(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	err := reg.Register(ctx, "agent-1", "http://agent1:8080", []string{"nlp", "search"}, "http://agent1:8080/health")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	records, err := reg.DiscoverAll(ctx)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "agent-1" {
		t.Errorf("expected name agent-1, got %s", records[0].Name)
	}
	if records[0].URL != "http://agent1:8080" {
		t.Errorf("expected URL http://agent1:8080, got %s", records[0].URL)
	}
}

func TestRegisterUpsert(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	err := reg.Register(ctx, "agent-1", "http://agent1:8080", []string{"nlp"}, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Register again with a different URL (upsert).
	err = reg.Register(ctx, "agent-1", "http://agent1-new:9090", []string{"nlp", "vision"}, "")
	if err != nil {
		t.Fatalf("Register upsert: %v", err)
	}

	records, err := reg.DiscoverAll(ctx)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after upsert, got %d", len(records))
	}
	if records[0].URL != "http://agent1-new:9090" {
		t.Errorf("expected updated URL, got %s", records[0].URL)
	}
}

func TestDiscover(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "nlp-agent", "http://nlp:8080", []string{"nlp", "search"}, "")
	_ = reg.Register(ctx, "vision-agent", "http://vision:8080", []string{"vision"}, "")

	records, err := reg.Discover(ctx, "nlp")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 nlp agent, got %d", len(records))
	}
	if records[0].Name != "nlp-agent" {
		t.Errorf("expected nlp-agent, got %s", records[0].Name)
	}
}

func TestDiscoverNoMatch(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "nlp-agent", "http://nlp:8080", []string{"nlp"}, "")

	records, err := reg.Discover(ctx, "vision")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for vision, got %d", len(records))
	}
}

func TestDiscoverAll(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "agent-a", "http://a:8080", []string{"a"}, "")
	_ = reg.Register(ctx, "agent-b", "http://b:8080", []string{"b"}, "")
	_ = reg.Register(ctx, "agent-c", "http://c:8080", []string{"c"}, "")

	records, err := reg.DiscoverAll(ctx)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
}

func TestHeartbeat(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "agent-1", "http://agent1:8080", []string{"nlp"}, "")

	err := reg.Heartbeat(ctx, "agent-1")
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	records, _ := reg.DiscoverAll(ctx)
	if len(records) == 0 {
		t.Fatal("expected record after heartbeat")
	}
	if records[0].LastSeen == "" {
		t.Error("expected last_seen to be set")
	}
}

func TestMarkSuccess(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "agent-1", "http://agent1:8080", []string{"nlp"}, "")

	err := reg.MarkSuccess(ctx, "agent-1")
	if err != nil {
		t.Fatalf("MarkSuccess: %v", err)
	}

	records, _ := reg.DiscoverAll(ctx)
	if records[0].SuccessCount != 1 {
		t.Errorf("expected success_count=1, got %d", records[0].SuccessCount)
	}
}

func TestMarkFailure(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "agent-1", "http://agent1:8080", []string{"nlp"}, "")

	err := reg.MarkFailure(ctx, "agent-1")
	if err != nil {
		t.Fatalf("MarkFailure: %v", err)
	}

	records, _ := reg.DiscoverAll(ctx)
	if records[0].FailureCount != 1 {
		t.Errorf("expected failure_count=1, got %d", records[0].FailureCount)
	}
}

func TestDeregister(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	_ = reg.Register(ctx, "agent-1", "http://agent1:8080", []string{"nlp"}, "")

	err := reg.Deregister(ctx, "agent-1")
	if err != nil {
		t.Fatalf("Deregister: %v", err)
	}

	records, _ := reg.DiscoverAll(ctx)
	if len(records) != 0 {
		t.Errorf("expected 0 records after deregister, got %d", len(records))
	}
}

func TestDiscoverHealthFilter(t *testing.T) {
	db := testutil.TestDB(t)
	reg := registry.New(db.Conn())
	ctx := context.Background()

	// Register agent with bad reputation (many failures, low success rate < 80%).
	_ = reg.Register(ctx, "bad-agent", "http://bad:8080", []string{"nlp"}, "")

	// Manually set bad reputation: 1 success, 9 failures (10% success rate).
	conn := db.Conn()
	_, _ = conn.ExecContext(ctx,
		`UPDATE agent_registry SET success_count = 1, failure_count = 9 WHERE name = 'bad-agent'`)

	records, err := reg.Discover(ctx, "nlp")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// Bad agent should be filtered out (10% < 80% with 10 total calls).
	for _, r := range records {
		if r.Name == "bad-agent" {
			t.Errorf("bad-agent should have been filtered by health check (10%% success rate)")
		}
	}
}
