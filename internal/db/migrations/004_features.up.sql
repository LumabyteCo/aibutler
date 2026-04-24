-- Schema version 4: Layer 5 Features

-- Scheduled agents
CREATE TABLE IF NOT EXISTS schedules (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    cron_expr   TEXT NOT NULL,
    task        TEXT NOT NULL,
    channel     TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    skills      TEXT,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS schedule_runs (
    id           INTEGER PRIMARY KEY,
    schedule_id  TEXT NOT NULL,
    status       TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    completed_at TEXT,
    agent_id     TEXT,
    error        TEXT,
    FOREIGN KEY (schedule_id) REFERENCES schedules(id),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
CREATE INDEX IF NOT EXISTS idx_schedule_runs_schedule ON schedule_runs(schedule_id, started_at);

-- IoT devices
CREATE TABLE IF NOT EXISTS iot_devices (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    tier        INTEGER NOT NULL,
    device_type TEXT NOT NULL,
    adapter     TEXT NOT NULL,
    config      TEXT,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Finance watchlist
CREATE TABLE IF NOT EXISTS finance_watchlist (
    id          INTEGER PRIMARY KEY,
    symbol      TEXT NOT NULL,
    name        TEXT,
    type        TEXT NOT NULL DEFAULT 'stock',
    alert_above REAL,
    alert_below REAL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(symbol, type)
);
