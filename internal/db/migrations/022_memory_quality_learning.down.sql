-- Revert migration 022: memory quality, measured learning, checkpoints.

DROP INDEX IF EXISTS idx_checkpoints_created;
DROP INDEX IF EXISTS idx_checkpoints_run;
DROP TABLE IF EXISTS checkpoints;

DROP INDEX IF EXISTS idx_agent_changes_kind;
DROP TABLE IF EXISTS agent_changes;

DROP INDEX IF EXISTS idx_eval_results_run;
DROP TABLE IF EXISTS eval_results;
DROP TABLE IF EXISTS eval_runs;

DROP INDEX IF EXISTS idx_proposals_status;
DROP TABLE IF EXISTS agent_proposals;

DROP INDEX IF EXISTS idx_conflicts_reviewed;
DROP TABLE IF EXISTS memory_conflicts;

DROP INDEX IF EXISTS idx_facts_status;
DROP INDEX IF EXISTS idx_facts_fact_key;
DROP INDEX IF EXISTS idx_facts_bank;
DROP INDEX IF EXISTS idx_thoughts_bank;
DROP INDEX IF EXISTS idx_entities_bank;
DROP INDEX IF EXISTS idx_transcripts_bank;
DROP INDEX IF EXISTS idx_vectors_bank;

-- Added columns (key_facts.*, bank columns, schedules.capabilities)
-- remain but are unused after downgrade, consistent with prior
-- migrations (see 013): SQLite column drops require table rebuilds that
-- would risk data on tables with FTS triggers, so we leave them.
-- Consequence (also matches 007/010/013): this down is one-way — re-running
-- the 022 up-file afterwards fails on "duplicate column name" unless the
-- added columns are dropped manually first.
