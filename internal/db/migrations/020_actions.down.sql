-- Rollback migration 020: drop the actions table and its indexes.
DROP INDEX IF EXISTS idx_actions_status;
DROP INDEX IF EXISTS idx_actions_type;
DROP INDEX IF EXISTS idx_actions_agent;
DROP INDEX IF EXISTS idx_actions_time;
DROP TABLE IF EXISTS actions;
