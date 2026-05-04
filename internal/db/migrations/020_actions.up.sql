-- Migration 020: Action recording.
--
-- Records fine-grained actions (each AppleScript / D-Bus / shell call) at
-- a level finer than the existing compliance_audit table — captures the
-- target, parsed payload summary, duration, status, result summary, and
-- error per action so reviewers can see exactly what was done, by whom,
-- in what order, and how long it took.
--
-- compliance_audit captures call-level audit (which tool / capability with
-- what input). actions captures effect-level detail derived from the call.

CREATE TABLE IF NOT EXISTS actions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    agent_id        TEXT,
    session_id      TEXT,
    action_type     TEXT NOT NULL,
    target          TEXT,
    payload_summary TEXT,
    payload_full    TEXT,
    duration_ms     INTEGER,
    status          TEXT NOT NULL,
    result_summary  TEXT,
    error           TEXT
);

CREATE INDEX IF NOT EXISTS idx_actions_time   ON actions(timestamp);
CREATE INDEX IF NOT EXISTS idx_actions_agent  ON actions(agent_id);
CREATE INDEX IF NOT EXISTS idx_actions_type   ON actions(action_type);
CREATE INDEX IF NOT EXISTS idx_actions_status ON actions(status);
