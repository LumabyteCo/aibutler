-- Migration 021: Mission engine — foundation.
--
-- A mission is a long-running goal that one or more agents collaborate to
-- achieve. Where v0.1 has agents that handle one user turn at a time, the
-- mission engine introduces persistent, replannable work units with their
-- own state, budget, hierarchy of agents, and progress reporting.
--
-- This migration adds only the data-layer tables. Supervisor/worker
-- orchestration, bus refactor, and dashboard arrive in follow-up
-- migrations and code commits.

CREATE TABLE IF NOT EXISTS missions (
    id                    TEXT PRIMARY KEY,
    goal                  TEXT NOT NULL,
    state                 TEXT NOT NULL DEFAULT 'created',
    plan_json             TEXT,
    budget_usd            REAL NOT NULL DEFAULT 0,
    cost_so_far_usd       REAL NOT NULL DEFAULT 0,
    supervisor_agent_id   TEXT,
    created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at            DATETIME,
    completed_at          DATETIME
);

CREATE INDEX IF NOT EXISTS idx_missions_state      ON missions(state);
CREATE INDEX IF NOT EXISTS idx_missions_created    ON missions(created_at);
CREATE INDEX IF NOT EXISTS idx_missions_supervisor ON missions(supervisor_agent_id);

-- Plan steps for a mission. depends_on holds a JSON array of step IDs the
-- step waits on before becoming ready to dispatch.
CREATE TABLE IF NOT EXISTS mission_steps (
    id                  TEXT PRIMARY KEY,
    mission_id          TEXT NOT NULL,
    task                TEXT NOT NULL,
    depends_on_json     TEXT,
    assigned_worker_id  TEXT,
    state               TEXT NOT NULL DEFAULT 'created',
    output              TEXT,
    error               TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at          DATETIME,
    completed_at        DATETIME,
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mission_steps_mission ON mission_steps(mission_id);
CREATE INDEX IF NOT EXISTS idx_mission_steps_state   ON mission_steps(state);

-- Append-only event log for a mission. Used by the supervisor to track
-- worker progress and by reviewers to replay what happened.
CREATE TABLE IF NOT EXISTS mission_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    mission_id   TEXT NOT NULL,
    timestamp    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type   TEXT NOT NULL,
    payload_json TEXT,
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mission_events_mission ON mission_events(mission_id);
CREATE INDEX IF NOT EXISTS idx_mission_events_time    ON mission_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_mission_events_type    ON mission_events(event_type);
