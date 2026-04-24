-- Layer 1: Streaming, Compaction & Developer CLI

CREATE TABLE IF NOT EXISTS session_compactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    original_count INTEGER NOT NULL,
    compacted_count INTEGER NOT NULL,
    removed_count INTEGER NOT NULL,
    preserved_count INTEGER NOT NULL,
    tokens_before INTEGER NOT NULL,
    tokens_after INTEGER NOT NULL,
    tools_used TEXT NOT NULL DEFAULT '[]',
    key_files TEXT NOT NULL DEFAULT '[]',
    pending_work TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_session_compactions_session ON session_compactions(session_id);
