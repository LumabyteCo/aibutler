package db_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/db"
)

func TestMigrateUpAndDown(t *testing.T) {
	database, err := db.Open(db.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	ctx := context.Background()

	// Start at version 0
	v, _ := database.SchemaVersion(ctx)
	if v != 0 {
		t.Fatalf("initial version = %d, want 0", v)
	}

	// Migrate up
	if err := database.ApplySchema(ctx); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 20 {
		t.Fatalf("after up version = %d, want 20", v)
	}

	// Verify tables exist
	var count int
	err = database.Conn().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	// Through migration 018 = ~62 tables + migration 019 adds webauthn_credentials, security_events = ~64 + migration 020 adds actions = ~65
	if count < 57 {
		t.Errorf("table count = %d, want >= 57", count)
	}

	// Migrate down (reverts migration 020 — Action recording)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 020: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 19 {
		t.Fatalf("after down 020 version = %d, want 19", v)
	}

	// Migrate down (reverts migration 019 — Advanced Security)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 019: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 18 {
		t.Fatalf("after down 019 version = %d, want 18", v)
	}

	// Migrate down (reverts migration 018 — Enterprise RBAC/SSO/Compliance)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 018: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 17 {
		t.Fatalf("after down 018 version = %d, want 17", v)
	}

	// Migrate down (reverts migration 017 — Channels P4)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 017: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 16 {
		t.Fatalf("after down 017 version = %d, want 16", v)
	}

	// Migrate down (reverts migration 016 — Response Cache)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 016: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 15 {
		t.Fatalf("after down 016 version = %d, want 15", v)
	}

	// Migrate down (reverts migration 015 — Transactions & AI Services)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 015: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 14 {
		t.Fatalf("after down 015 version = %d, want 14", v)
	}

	// Migrate down (reverts migration 014 — Streaming & Compaction)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 014: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 13 {
		t.Fatalf("after down 014 version = %d, want 13", v)
	}

	// Migrate down (reverts migration 013 — Reliability)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 013: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 12 {
		t.Fatalf("after down 013 version = %d, want 12", v)
	}

	// Migrate down (reverts migration 012 — Web Dashboard)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 012: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 11 {
		t.Fatalf("after down 012 version = %d, want 11", v)
	}

	// Migrate down (reverts migration 011 — Channels P2)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 011: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 10 {
		t.Fatalf("after down 011 version = %d, want 10", v)
	}

	// Migrate down (reverts migration 010 — Resource Access P2)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 010: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 9 {
		t.Fatalf("after down 010 version = %d, want 9", v)
	}

	// Migrate down (reverts migration 009 — Protocol & Memory Exposure)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 009: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 8 {
		t.Fatalf("after down 009 version = %d, want 8", v)
	}

	// Migrate down (reverts migration 008 — Plugin System)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 008: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 7 {
		t.Fatalf("after down 008 version = %d, want 7", v)
	}

	// Migrate down (reverts migration 007 — Agent Orchestration P2)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 007: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 6 {
		t.Fatalf("after down 007 version = %d, want 6", v)
	}

	// Migrate down (reverts migration 006 — Living Memory P2)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 006: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 5 {
		t.Fatalf("after down 006 version = %d, want 5", v)
	}

	// Migrate down (reverts migration 005 — learned_instructions)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 005: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 4 {
		t.Fatalf("after down 005 version = %d, want 4", v)
	}

	// Migrate down (reverts migration 004 — features tables)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 004: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 3 {
		t.Fatalf("after down 004 version = %d, want 3", v)
	}

	// Migrate down (reverts migration 003 — channel_messages + mcp_servers)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 003: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 2 {
		t.Fatalf("after down 003 version = %d, want 2", v)
	}

	// Migrate down (reverts migration 002 — messages table)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 002: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 1 {
		t.Fatalf("after down 002 version = %d, want 1", v)
	}

	// Verify messages table is gone but core tables remain.
	err = database.Conn().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count)
	if err != nil {
		t.Fatalf("count tables after down: %v", err)
	}
	// 22 core tables + schema_migrations = 23 (messages removed)
	if count != 23 {
		t.Errorf("table count after down = %d, want 23", count)
	}

	// Migrate down again (reverts migration 001 — all core tables)
	if err := database.MigrateDown(ctx); err != nil {
		t.Fatalf("migrate down 001: %v", err)
	}
	v, _ = database.SchemaVersion(ctx)
	if v != 0 {
		t.Fatalf("after down 001 version = %d, want 0", v)
	}

	// Verify all tables are gone.
	err = database.Conn().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count)
	if err != nil {
		t.Fatalf("count tables after full down: %v", err)
	}
	if count != 0 {
		t.Errorf("table count after full down = %d, want 0", count)
	}
}
