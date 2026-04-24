-- Layer 8: Hardening, Polish + Swarm Safety

-- Add quarantine support to agent registry.
ALTER TABLE agent_registry ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_registry ADD COLUMN quarantined INTEGER NOT NULL DEFAULT 0;

-- Cron job execution tracking.
CREATE TABLE IF NOT EXISTS cron_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_name TEXT NOT NULL,
    scheduled_at DATETIME NOT NULL,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running','completed','failed','retrying')),
    attempt INTEGER NOT NULL DEFAULT 1,
    error_message TEXT,
    UNIQUE(job_name, scheduled_at)
);
CREATE INDEX IF NOT EXISTS idx_cron_executions_job ON cron_executions(job_name, scheduled_at);

-- Rate limit tracking (for persistent rate limits).
CREATE TABLE IF NOT EXISTS rate_limit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_rate_limit_key ON rate_limit_log(key, timestamp);
