-- Rollback migration 021: drop mission tables and their indexes.
-- ON DELETE CASCADE is set on FKs so dropping the parent first cleans up children.
DROP INDEX IF EXISTS idx_mission_events_type;
DROP INDEX IF EXISTS idx_mission_events_time;
DROP INDEX IF EXISTS idx_mission_events_mission;
DROP TABLE IF EXISTS mission_events;

DROP INDEX IF EXISTS idx_mission_steps_state;
DROP INDEX IF EXISTS idx_mission_steps_mission;
DROP TABLE IF EXISTS mission_steps;

DROP INDEX IF EXISTS idx_missions_supervisor;
DROP INDEX IF EXISTS idx_missions_created;
DROP INDEX IF EXISTS idx_missions_state;
DROP TABLE IF EXISTS missions;
