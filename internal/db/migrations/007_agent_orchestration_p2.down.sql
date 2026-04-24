-- 007_agent_orchestration_p2.down.sql
-- Reverses Layer 2: Agent Orchestration P2.

DROP TABLE IF EXISTS custom_agent_roles;
DROP TABLE IF EXISTS background_agents;
DROP TABLE IF EXISTS agent_delegations;

-- SQLite does not support DROP COLUMN before 3.35.0, so we recreate
-- the agents table without the Layer 2 columns.
CREATE TABLE agents_backup AS SELECT
    id, parent_id, session_id, type, state, task, capabilities, model,
    skills_loaded, created_at, updated_at, completed_at, result_summary,
    error, tokens_input, tokens_output, tokens_cached, cost_usd,
    tool_calls, max_tool_calls, timeout_ms, budget_cap_usd
FROM agents;

DROP TABLE agents;

CREATE TABLE agents (
    id              TEXT PRIMARY KEY,
    parent_id       TEXT,
    session_id      TEXT NOT NULL,
    type            TEXT NOT NULL,
    state           TEXT NOT NULL,
    task            TEXT NOT NULL,
    capabilities    TEXT NOT NULL,
    model           TEXT NOT NULL,
    skills_loaded   TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    completed_at    TEXT,
    result_summary  TEXT,
    error           TEXT,
    tokens_input    INTEGER DEFAULT 0,
    tokens_output   INTEGER DEFAULT 0,
    tokens_cached   INTEGER DEFAULT 0,
    cost_usd        REAL DEFAULT 0.0,
    tool_calls      INTEGER DEFAULT 0,
    max_tool_calls  INTEGER DEFAULT 50,
    timeout_ms      INTEGER DEFAULT 300000,
    budget_cap_usd  REAL,
    FOREIGN KEY (parent_id) REFERENCES agents(id),
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

INSERT INTO agents SELECT * FROM agents_backup;
DROP TABLE agents_backup;

CREATE INDEX IF NOT EXISTS idx_agents_session ON agents(session_id);
CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_id);
CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
CREATE INDEX IF NOT EXISTS idx_agents_type_state ON agents(type, state);
