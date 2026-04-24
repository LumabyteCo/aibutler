DROP INDEX IF EXISTS idx_rate_limit_key;
DROP TABLE IF EXISTS rate_limit_log;
DROP INDEX IF EXISTS idx_cron_executions_job;
DROP TABLE IF EXISTS cron_executions;
-- SQLite does not support DROP COLUMN; the columns remain but are unused after downgrade.
