-- Schema version 5: Learned Instructions

CREATE TABLE IF NOT EXISTS learned_instructions (
    id          INTEGER PRIMARY KEY,
    content     TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT 'rule',
    priority    INTEGER NOT NULL DEFAULT 50,
    scope       TEXT NOT NULL DEFAULT 'global',
    scope_value TEXT,
    active      INTEGER NOT NULL DEFAULT 1,
    source      TEXT NOT NULL DEFAULT 'explicit',
    source_text TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT,
    UNIQUE(content, scope, scope_value)
);

CREATE INDEX IF NOT EXISTS idx_instructions_active ON learned_instructions(active, scope);
CREATE INDEX IF NOT EXISTS idx_instructions_priority ON learned_instructions(priority DESC);
CREATE INDEX IF NOT EXISTS idx_instructions_category ON learned_instructions(category);
