-- 007_agent_orchestration_p2.up.sql
-- Layer 2: Agent Orchestration P2 — delegation tracking, background lifecycle,
-- custom roles, extended agent metadata.

-- Track parent→child delegation relationships.
CREATE TABLE IF NOT EXISTS agent_delegations (
    id            INTEGER PRIMARY KEY,
    parent_id     TEXT    NOT NULL,                      -- Parent agent ID
    child_id      TEXT    NOT NULL,                      -- Child agent ID
    delegation_type TEXT  NOT NULL DEFAULT 'delegate',   -- 'delegate' or 'spawn'
    status        TEXT    NOT NULL DEFAULT 'pending',    -- pending, running, completed, failed, cancelled
    max_cost      REAL,                                  -- Per-subagent budget cap (USD)
    task          TEXT,                                   -- Task description
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    completed_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_delegations_parent ON agent_delegations(parent_id);
CREATE INDEX IF NOT EXISTS idx_delegations_child  ON agent_delegations(child_id);
CREATE INDEX IF NOT EXISTS idx_delegations_status ON agent_delegations(status);

-- Track background agent lifecycle (spawned agents that outlive the parent turn).
CREATE TABLE IF NOT EXISTS background_agents (
    id            INTEGER PRIMARY KEY,
    agent_id      TEXT    NOT NULL UNIQUE,               -- References agents.id
    owner_session TEXT    NOT NULL,                      -- Session that spawned it
    task          TEXT,
    max_duration  INTEGER,                               -- Max seconds
    max_cost      REAL,                                  -- Budget cap (USD)
    status        TEXT    NOT NULL DEFAULT 'running',    -- running, completed, failed, cancelled, expired
    report_channel TEXT,                                 -- Channel to report results to
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    expires_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_bg_agents_session ON background_agents(owner_session);
CREATE INDEX IF NOT EXISTS idx_bg_agents_status  ON background_agents(status);

-- Custom agent roles (user-defined specialists).
CREATE TABLE IF NOT EXISTS custom_agent_roles (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE,
    description   TEXT,
    model_override TEXT,                                 -- Override primary model for this role
    tools         TEXT,                                  -- JSON array of allowed tool names
    system_prompt TEXT,                                  -- Additional system instructions for this role
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Extend agents table with Layer 2 columns (only columns NOT in 001_initial).
-- Existing columns from 001: cost_usd, tokens_input, tokens_output, error,
-- tool_calls, max_tool_calls, timeout_ms, budget_cap_usd.
ALTER TABLE agents ADD COLUMN autonomy_level TEXT DEFAULT 'l1';
ALTER TABLE agents ADD COLUMN mode           TEXT DEFAULT 'auto';
ALTER TABLE agents ADD COLUMN duration_ms    INTEGER DEFAULT 0;
