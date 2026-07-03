-- Migration 022: Memory quality, measured learning, and checkpoint schema.
--
-- Data layer for four upcoming capabilities (code arrives in follow-up
-- commits; this migration is deliberately front-loaded so the schema
-- moves once):
--
--   1. Fact quality — per-fact provenance, confidence, importance, and
--      explicit supersede/conflict handling. Facts become discrete,
--      correctable rows instead of append-only text: when a new fact
--      contradicts an old one on the same single-valued attribute, the
--      old row is marked superseded (never deleted) and the conflict is
--      recorded for review. Provenance pointers make true deletion
--      possible: erasing a source row can cascade to everything derived
--      from it.
--   2. Bounded core memory — importance/access columns feed a scored
--      always-in-context working set selected at prompt-composition
--      time; scoring inputs live on the row so selection is one indexed
--      query with no retrieval round-trip.
--   3. Measured self-improvement — eval_runs/eval_results store
--      benchmark outcomes for agent trajectories; agent_changes is a
--      human-reviewable ledger of every change the system makes to its
--      own skills or heuristics, each entry tied to the eval run that
--      justified it. agent_proposals holds anything that needs explicit
--      human approval before taking effect.
--   4. Safe rollback — checkpoints stores pre-images of files before
--      agent-driven mutations so any change can be inspected and undone.
--
-- Also: per-schedule capability profiles (schedules.capabilities) so
-- background jobs run with only the permissions they need, and a bank
-- column on memory tables so agent profiles get isolated memory scopes
-- by default.

-- ---------------------------------------------------------------------
-- 1. key_facts: provenance, confidence, importance, lifecycle status.
-- ---------------------------------------------------------------------
-- fact_key: canonical subject.attribute slug (e.g. 'user.location') for
-- facts that describe a single-valued attribute; NULL for multi-valued
-- categories (preferences, projects) which never auto-conflict.
ALTER TABLE key_facts ADD COLUMN fact_key      TEXT;
-- importance: 1-10 salience used by core-memory selection (explicit
-- user statements default higher in code; 5 is the neutral prior).
ALTER TABLE key_facts ADD COLUMN importance    INTEGER NOT NULL DEFAULT 5;
-- confidence: 0.0-1.0 prior by source class, boosted on re-assertion,
-- set to 1.0 on explicit user confirmation.
ALTER TABLE key_facts ADD COLUMN confidence    REAL NOT NULL DEFAULT 0.7;
-- Provenance pointer to the originating row ('thought'/'transcript' +
-- id). Enables cascade deletion and per-fact source display.
ALTER TABLE key_facts ADD COLUMN source_type   TEXT;
ALTER TABLE key_facts ADD COLUMN source_id     INTEGER;
-- Access tracking for frequency-based promotion into core memory.
ALTER TABLE key_facts ADD COLUMN last_accessed TEXT;
ALTER TABLE key_facts ADD COLUMN access_count  INTEGER NOT NULL DEFAULT 0;
-- Lifecycle: 'active' | 'superseded' | 'retracted'. Superseded and
-- retracted rows are excluded from selection but retained for history.
ALTER TABLE key_facts ADD COLUMN status        TEXT NOT NULL DEFAULT 'active';
ALTER TABLE key_facts ADD COLUMN superseded_by INTEGER REFERENCES key_facts(id) ON DELETE SET NULL;
-- pinned: user-pinned facts always win core-memory selection.
ALTER TABLE key_facts ADD COLUMN pinned        INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_facts_status   ON key_facts(status);
CREATE INDEX IF NOT EXISTS idx_facts_fact_key ON key_facts(fact_key);

-- ---------------------------------------------------------------------
-- 2. Memory banks: per-profile isolation scope on memory tables.
-- ---------------------------------------------------------------------
-- Every read/write is scoped to a bank; 'main' preserves existing
-- behavior. Cross-bank access requires an explicit capability grant.
ALTER TABLE key_facts           ADD COLUMN bank TEXT NOT NULL DEFAULT 'main';
ALTER TABLE captured_thoughts   ADD COLUMN bank TEXT NOT NULL DEFAULT 'main';
ALTER TABLE entities            ADD COLUMN bank TEXT NOT NULL DEFAULT 'main';
ALTER TABLE session_transcripts ADD COLUMN bank TEXT NOT NULL DEFAULT 'main';
ALTER TABLE memory_vectors      ADD COLUMN bank TEXT NOT NULL DEFAULT 'main';

CREATE INDEX IF NOT EXISTS idx_facts_bank       ON key_facts(bank);
CREATE INDEX IF NOT EXISTS idx_thoughts_bank    ON captured_thoughts(bank);
CREATE INDEX IF NOT EXISTS idx_entities_bank    ON entities(bank);
CREATE INDEX IF NOT EXISTS idx_transcripts_bank ON session_transcripts(bank);
CREATE INDEX IF NOT EXISTS idx_vectors_bank     ON memory_vectors(bank);

-- ---------------------------------------------------------------------
-- 3. Per-schedule capability profiles.
-- ---------------------------------------------------------------------
-- JSON array of capability resource strings a scheduled job runs with.
-- NULL preserves the legacy behavior (default capability set).
ALTER TABLE schedules ADD COLUMN capabilities TEXT;

-- ---------------------------------------------------------------------
-- 4. Conflict ledger for contradicting facts.
-- ---------------------------------------------------------------------
-- One row per detected contradiction. resolution:
--   'auto_supersede' — clear case, old fact marked superseded
--   'needs_review'   — ambiguous, surfaced for explicit user decision
CREATE TABLE IF NOT EXISTS memory_conflicts (
    id           INTEGER PRIMARY KEY,
    old_fact_id  INTEGER NOT NULL,
    new_fact_id  INTEGER NOT NULL,
    fact_key     TEXT,
    detected_at  TEXT NOT NULL DEFAULT (datetime('now')),
    resolution   TEXT NOT NULL DEFAULT 'auto_supersede',
    reviewed     INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (old_fact_id) REFERENCES key_facts(id) ON DELETE CASCADE,
    FOREIGN KEY (new_fact_id) REFERENCES key_facts(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_conflicts_reviewed ON memory_conflicts(reviewed);

-- ---------------------------------------------------------------------
-- 5. Proposals awaiting explicit human approval.
-- ---------------------------------------------------------------------
-- Shared surface for anything the system wants to change but must not
-- apply silently: synthesized skills, reflection recommendations,
-- conflict resolutions. kind: 'skill' | 'reflection' | 'conflict_review'.
-- payload is kind-specific JSON (e.g. staged skill path + content hash).
CREATE TABLE IF NOT EXISTS agent_proposals (
    id         INTEGER PRIMARY KEY,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    payload    TEXT,
    status     TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    decided_at TEXT,
    decided_by TEXT
);
CREATE INDEX IF NOT EXISTS idx_proposals_status ON agent_proposals(status, kind);

-- ---------------------------------------------------------------------
-- 6. Eval harness: benchmark runs and per-task results.
-- ---------------------------------------------------------------------
-- suite_hash pins the exact task-suite content a run was measured
-- against; results against different hashes are not comparable, which
-- also makes tampering with the suite visible in the record.
CREATE TABLE IF NOT EXISTS eval_runs (
    id           INTEGER PRIMARY KEY,
    suite_hash   TEXT NOT NULL,
    model        TEXT,
    mode         TEXT NOT NULL,
    started_at   TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT,
    tasks_total  INTEGER NOT NULL DEFAULT 0,
    tasks_passed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS eval_results (
    id             INTEGER PRIMARY KEY,
    run_id         INTEGER NOT NULL,
    task_id        TEXT NOT NULL,
    success        INTEGER NOT NULL,
    tool_calls     INTEGER NOT NULL DEFAULT 0,
    tool_errors    INTEGER NOT NULL DEFAULT 0,
    retries        INTEGER NOT NULL DEFAULT 0,
    tokens_in      INTEGER NOT NULL DEFAULT 0,
    tokens_out     INTEGER NOT NULL DEFAULT 0,
    cost_usd       REAL NOT NULL DEFAULT 0,
    wall_ms        INTEGER NOT NULL DEFAULT 0,
    failure_reason TEXT,
    FOREIGN KEY (run_id) REFERENCES eval_runs(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_eval_results_run ON eval_results(run_id);

-- ---------------------------------------------------------------------
-- 7. Ledger of changes the system makes to its own behavior.
-- ---------------------------------------------------------------------
-- kind: 'skill_created' | 'skill_patched' | 'skill_archived' |
--       'heuristic_changed' | 'core_memory_policy_changed'.
-- Every insert is mirrored to resource_access_log by the writer so the
-- existing audit trail remains the single query surface for "what did
-- the system do". eval_run_id links the measurement that justified the
-- change; approved_by records the human decision.
CREATE TABLE IF NOT EXISTS agent_changes (
    id          INTEGER PRIMARY KEY,
    kind        TEXT NOT NULL,
    subject_id  TEXT NOT NULL,
    before_hash TEXT,
    after_hash  TEXT,
    eval_run_id INTEGER,
    approved_by TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    -- SET NULL so pruning old eval runs never blocks on the ledger; the
    -- before/after hashes keep the entry meaningful without the run row.
    FOREIGN KEY (eval_run_id) REFERENCES eval_runs(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_changes_kind ON agent_changes(kind, created_at);

-- ---------------------------------------------------------------------
-- 8. Checkpoints: pre-images of files before agent-driven mutations.
-- ---------------------------------------------------------------------
-- absent=1 records "file did not exist before" so restore can delete.
-- pre_content is capped in code; oversized files spill to the data dir
-- and pre_content stays NULL with pre_hash still recorded.
CREATE TABLE IF NOT EXISTS checkpoints (
    id          INTEGER PRIMARY KEY,
    run_id      TEXT NOT NULL,
    tool        TEXT NOT NULL,
    path        TEXT NOT NULL,
    pre_hash    TEXT,
    pre_content BLOB,
    absent      INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_checkpoints_run     ON checkpoints(run_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_created ON checkpoints(created_at);
