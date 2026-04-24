-- 008_plugin_system.up.sql
-- Layer 3: Plugin Infrastructure — plugin registry, KV storage, audit log.

CREATE TABLE IF NOT EXISTS plugins (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL UNIQUE,
    version         TEXT    NOT NULL DEFAULT '0.0.0',
    manifest_hash   TEXT    NOT NULL,
    wasm_hash       TEXT    NOT NULL,
    status          TEXT    NOT NULL DEFAULT 'disabled',
    capabilities    TEXT    NOT NULL DEFAULT '[]',
    settings        TEXT    NOT NULL DEFAULT '{}',
    configurations  TEXT    NOT NULL DEFAULT '{}',
    installed_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_kv (
    plugin_id   INTEGER NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    key         TEXT    NOT NULL,
    value       BLOB,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (plugin_id, key)
);

CREATE TABLE IF NOT EXISTS plugin_audit (
    id              INTEGER PRIMARY KEY,
    plugin_id       INTEGER NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
    action          TEXT    NOT NULL,
    capability_used TEXT,
    input_summary   TEXT,
    status          TEXT    NOT NULL DEFAULT 'success',
    error_message   TEXT,
    timestamp       TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_plugin_audit_plugin ON plugin_audit(plugin_id);
CREATE INDEX IF NOT EXISTS idx_plugin_audit_ts     ON plugin_audit(timestamp);
