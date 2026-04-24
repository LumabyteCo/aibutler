-- Migration 006: Living Memory Phase 2
-- Adds entity extraction, knowledge graph, session transcripts, and FTS5 full-text search.

-- Entities extracted from conversations (people, projects, decisions, action items).
CREATE TABLE IF NOT EXISTS entities (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    type            TEXT NOT NULL,          -- 'person', 'project', 'decision', 'action_item', 'insight'
    name            TEXT NOT NULL,          -- canonical name
    attributes      TEXT,                   -- JSON: extra info (email, role, status, etc.)
    source_session  TEXT,                   -- session where first discovered
    first_seen      TEXT NOT NULL,          -- ISO 8601
    last_seen       TEXT NOT NULL,          -- ISO 8601
    mention_count   INTEGER DEFAULT 1       -- times mentioned across sessions
);

CREATE INDEX IF NOT EXISTS idx_entities_type_name ON entities(type, name);
CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
CREATE INDEX IF NOT EXISTS idx_entities_mention_count ON entities(mention_count DESC);

-- Relationships between entities (e.g., "Sarah works_on Project X").
CREATE TABLE IF NOT EXISTS entity_relationships (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    from_entity_id  INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    to_entity_id    INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    relationship    TEXT NOT NULL,          -- 'works_on', 'knows', 'decided', 'assigned_to', etc.
    confidence      REAL DEFAULT 1.0,       -- 0.0 to 1.0
    source_session  TEXT,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_entity_rel_from ON entity_relationships(from_entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_rel_to ON entity_relationships(to_entity_id);

-- Session transcripts for cross-session recall.
CREATE TABLE IF NOT EXISTS session_transcripts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      TEXT NOT NULL,
    role            TEXT NOT NULL,          -- 'user', 'assistant', 'tool'
    content         TEXT NOT NULL,
    turn_number     INTEGER NOT NULL,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_transcripts_session ON session_transcripts(session_id);

-- FTS5 full-text search on captured thoughts.
CREATE VIRTUAL TABLE IF NOT EXISTS captured_thoughts_fts USING fts5(
    content,
    content=captured_thoughts,
    content_rowid=id
);

-- FTS5 full-text search on session transcripts.
CREATE VIRTUAL TABLE IF NOT EXISTS session_transcripts_fts USING fts5(
    content,
    content=session_transcripts,
    content_rowid=id
);

-- Triggers to keep FTS5 indexes in sync with source tables.

-- Triggers for captured_thoughts ↔ captured_thoughts_fts sync.
CREATE TRIGGER IF NOT EXISTS captured_thoughts_fts_insert AFTER INSERT ON captured_thoughts BEGIN
    INSERT INTO captured_thoughts_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS captured_thoughts_fts_delete AFTER DELETE ON captured_thoughts BEGIN
    INSERT INTO captured_thoughts_fts(captured_thoughts_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;

CREATE TRIGGER IF NOT EXISTS captured_thoughts_fts_update AFTER UPDATE ON captured_thoughts BEGIN
    INSERT INTO captured_thoughts_fts(captured_thoughts_fts, rowid, content) VALUES ('delete', old.id, old.content);
    INSERT INTO captured_thoughts_fts(rowid, content) VALUES (new.id, new.content);
END;

-- Triggers for session_transcripts ↔ session_transcripts_fts sync.
CREATE TRIGGER IF NOT EXISTS session_transcripts_fts_insert AFTER INSERT ON session_transcripts BEGIN
    INSERT INTO session_transcripts_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS session_transcripts_fts_delete AFTER DELETE ON session_transcripts BEGIN
    INSERT INTO session_transcripts_fts(session_transcripts_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;

CREATE TRIGGER IF NOT EXISTS session_transcripts_fts_update AFTER UPDATE ON session_transcripts BEGIN
    INSERT INTO session_transcripts_fts(session_transcripts_fts, rowid, content) VALUES ('delete', old.id, old.content);
    INSERT INTO session_transcripts_fts(rowid, content) VALUES (new.id, new.content);
END;

-- Vector embeddings for semantic search (pure Go distance functions registered at connection init).
CREATE TABLE IF NOT EXISTS memory_vectors (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type TEXT NOT NULL,          -- 'thought', 'transcript', 'entity'
    source_id   INTEGER NOT NULL,       -- references row in source table
    embedding   BLOB NOT NULL,          -- float32 array as little-endian BLOB
    model       TEXT NOT NULL,          -- embedding model name (e.g., 'text-embedding-3-small')
    dimension   INTEGER NOT NULL,       -- vector dimension (e.g., 384, 1536)
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(source_type, source_id)      -- one embedding per source item
);

CREATE INDEX IF NOT EXISTS idx_vectors_source ON memory_vectors(source_type, source_id);
