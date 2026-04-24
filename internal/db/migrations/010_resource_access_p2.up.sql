-- Layer 5: Resource Access & Services + Swarm Foundation

-- OAuth token storage.
CREATE TABLE IF NOT EXISTS oauth_tokens (
    id              INTEGER PRIMARY KEY,
    provider        TEXT NOT NULL,
    account_id      TEXT NOT NULL DEFAULT 'default',
    access_token    TEXT NOT NULL,
    refresh_token   TEXT,
    token_type      TEXT NOT NULL DEFAULT 'Bearer',
    expires_at      TEXT,
    scopes          TEXT NOT NULL DEFAULT '[]',
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_tokens_provider_account ON oauth_tokens(provider, account_id);

-- A2A v2: extend a2a_delegations.
ALTER TABLE a2a_delegations ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'completed';
ALTER TABLE a2a_delegations ADD COLUMN trace_id TEXT;

-- Dynamic agent registry.
CREATE TABLE IF NOT EXISTS agent_registry (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    url             TEXT NOT NULL,
    capabilities    TEXT NOT NULL DEFAULT '[]',
    health_check_url TEXT,
    last_seen       TEXT,
    success_count   INTEGER NOT NULL DEFAULT 0,
    failure_count   INTEGER NOT NULL DEFAULT 0,
    registered_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_agent_registry_name ON agent_registry(name);

-- Swarm runs.
CREATE TABLE IF NOT EXISTS swarm_runs (
    id              INTEGER PRIMARY KEY,
    run_id          TEXT NOT NULL UNIQUE,
    goal            TEXT NOT NULL,
    plan_json       TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'running',
    total_cost_usd  REAL NOT NULL DEFAULT 0.0,
    trace_id        TEXT,
    started_at      TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_swarm_runs_run_id ON swarm_runs(run_id);

-- Shared swarm workspace.
CREATE TABLE IF NOT EXISTS swarm_workspaces (
    id          INTEGER PRIMARY KEY,
    run_id      TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    written_by  TEXT,
    written_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_swarm_workspaces_run_key ON swarm_workspaces(run_id, key);
CREATE INDEX IF NOT EXISTS idx_swarm_workspaces_run_id ON swarm_workspaces(run_id);

-- Distributed trace spans.
CREATE TABLE IF NOT EXISTS swarm_trace (
    id              INTEGER PRIMARY KEY,
    trace_id        TEXT NOT NULL,
    span_id         TEXT NOT NULL UNIQUE,
    parent_span_id  TEXT,
    agent_id        TEXT NOT NULL,
    peer_url        TEXT,
    task_summary    TEXT,
    status          TEXT NOT NULL DEFAULT 'running',
    cost_usd        REAL NOT NULL DEFAULT 0.0,
    started_at      TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_swarm_trace_trace_id ON swarm_trace(trace_id);
