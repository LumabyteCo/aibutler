-- Migration 009: Protocol & Memory Exposure (Layer 4)
-- Tables for memory import tracking, memory digests, and A2A delegation logging.

-- Memory import tracking
CREATE TABLE IF NOT EXISTS memory_imports (
    id INTEGER PRIMARY KEY,
    source TEXT NOT NULL,
    filename TEXT NOT NULL,
    thoughts_imported INTEGER NOT NULL DEFAULT 0,
    entities_extracted INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);

-- Memory digests (aggregated insights)
CREATE TABLE IF NOT EXISTS memory_digests (
    id INTEGER PRIMARY KEY,
    digest_type TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    period_start TEXT,
    period_end TEXT,
    source_thought_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_memory_digests_type ON memory_digests(digest_type);

-- A2A delegation log
CREATE TABLE IF NOT EXISTS a2a_delegations (
    id INTEGER PRIMARY KEY,
    direction TEXT NOT NULL,
    peer_agent TEXT NOT NULL,
    task_summary TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    result_summary TEXT,
    token_auth_hash TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_a2a_delegations_peer ON a2a_delegations(peer_agent);
