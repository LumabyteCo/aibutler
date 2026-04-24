-- Schema version 1: Phase 1 Foundation

-- Sessions (referenced by agents FK)
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    channel     TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    scope       TEXT NOT NULL DEFAULT 'default',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_channel ON sessions(channel, account_id);

-- Key Facts (rule-based extraction from conversations)
CREATE TABLE IF NOT EXISTS key_facts (
    id           INTEGER PRIMARY KEY,
    fact         TEXT NOT NULL,
    category     TEXT,
    source_session TEXT,
    extracted_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_facts_category ON key_facts(category);

-- Captured Thoughts (Living Memory Phase 1)
CREATE TABLE IF NOT EXISTS captured_thoughts (
    id          INTEGER PRIMARY KEY,
    content     TEXT NOT NULL,
    source      TEXT NOT NULL,
    session_id  TEXT,
    tags        TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_thoughts_created ON captured_thoughts(created_at);

-- User Tasks
CREATE TABLE IF NOT EXISTS user_tasks (
    id           INTEGER PRIMARY KEY,
    list_name    TEXT NOT NULL DEFAULT 'default',
    content      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    priority     INTEGER DEFAULT 0,
    due_at       TEXT,
    tags         TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT,
    UNIQUE(list_name, content, status)
);
CREATE INDEX IF NOT EXISTS idx_tasks_list ON user_tasks(list_name, status);
CREATE INDEX IF NOT EXISTS idx_tasks_due ON user_tasks(due_at) WHERE due_at IS NOT NULL;

-- User Reminders
CREATE TABLE IF NOT EXISTS user_reminders (
    id          INTEGER PRIMARY KEY,
    message     TEXT NOT NULL,
    remind_at   TEXT NOT NULL,
    recurrence  TEXT,
    channel     TEXT,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    fired_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_reminders_time ON user_reminders(remind_at) WHERE status = 'active';

-- User Contacts
CREATE TABLE IF NOT EXISTS user_contacts (
    id                INTEGER PRIMARY KEY,
    name              TEXT NOT NULL,
    phone             TEXT,
    email             TEXT,
    channel_ids       TEXT,
    preferred_channel TEXT,
    relationship      TEXT,
    notes             TEXT,
    birthday          TEXT,
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_contacts_name ON user_contacts(name);

-- User Expenses
CREATE TABLE IF NOT EXISTS user_expenses (
    id          INTEGER PRIMARY KEY,
    amount      REAL NOT NULL,
    currency    TEXT NOT NULL DEFAULT 'USD',
    category    TEXT NOT NULL,
    description TEXT,
    date        TEXT NOT NULL DEFAULT (date('now')),
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_expenses_date ON user_expenses(date);
CREATE INDEX IF NOT EXISTS idx_expenses_category ON user_expenses(category);

-- User Budgets
CREATE TABLE IF NOT EXISTS user_budgets (
    id          INTEGER PRIMARY KEY,
    category    TEXT NOT NULL,
    amount      REAL NOT NULL,
    period      TEXT NOT NULL DEFAULT 'monthly',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(category, period)
);

-- User Health (value is BLOB: double-encrypted at application level)
CREATE TABLE IF NOT EXISTS user_health (
    id          INTEGER PRIMARY KEY,
    metric      TEXT NOT NULL,
    value       BLOB NOT NULL,
    unit        TEXT,
    date        TEXT NOT NULL DEFAULT (date('now')),
    notes       TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_health_metric_date ON user_health(metric, date);

-- User Habits
CREATE TABLE IF NOT EXISTS user_habits (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    frequency   TEXT NOT NULL DEFAULT 'daily',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Habit Logs
CREATE TABLE IF NOT EXISTS user_habit_logs (
    id          INTEGER PRIMARY KEY,
    habit_id    INTEGER NOT NULL,
    date        TEXT NOT NULL DEFAULT (date('now')),
    notes       TEXT,
    FOREIGN KEY (habit_id) REFERENCES user_habits(id),
    UNIQUE(habit_id, date)
);

-- User Subscriptions
CREATE TABLE IF NOT EXISTS user_subscriptions (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    amount      REAL,
    currency    TEXT DEFAULT 'USD',
    frequency   TEXT NOT NULL,
    next_due    TEXT,
    category    TEXT,
    auto_renew  INTEGER DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Recipes
CREATE TABLE IF NOT EXISTS user_recipes (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,
    ingredients  TEXT,
    instructions TEXT,
    servings     INTEGER,
    prep_time    INTEGER,
    tags         TEXT,
    source       TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Journal
CREATE TABLE IF NOT EXISTS user_journal (
    id          INTEGER PRIMARY KEY,
    type        TEXT NOT NULL DEFAULT 'journal',
    content     TEXT NOT NULL,
    mood        TEXT,
    date        TEXT NOT NULL DEFAULT (date('now')),
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Places
CREATE TABLE IF NOT EXISTS user_places (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    address     TEXT,
    lat         REAL,
    lon         REAL,
    category    TEXT,
    notes       TEXT,
    rating      INTEGER,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Media
CREATE TABLE IF NOT EXISTS user_media (
    id          INTEGER PRIMARY KEY,
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'want',
    rating      INTEGER,
    progress    TEXT,
    notes       TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Maintenance
CREATE TABLE IF NOT EXISTS user_maintenance (
    id          INTEGER PRIMARY KEY,
    entity      TEXT NOT NULL,
    action      TEXT NOT NULL,
    date        TEXT NOT NULL DEFAULT (date('now')),
    next_due    TEXT,
    cost        REAL,
    notes       TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User Meals
CREATE TABLE IF NOT EXISTS user_meals (
    id          INTEGER PRIMARY KEY,
    date        TEXT NOT NULL DEFAULT (date('now')),
    meal_type   TEXT NOT NULL,
    description TEXT NOT NULL,
    recipe_id   INTEGER,
    calories    INTEGER,
    notes       TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (recipe_id) REFERENCES user_recipes(id)
);

-- Task Execution Context (multi-step task state machine)
CREATE TABLE IF NOT EXISTS task_contexts (
    id          INTEGER PRIMARY KEY,
    session_id  TEXT NOT NULL,
    task_type   TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'gathering',
    context     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_task_ctx_session ON task_contexts(session_id, state);

-- Token Usage (cost tracking)
CREATE TABLE IF NOT EXISTS token_usage (
    id              INTEGER PRIMARY KEY,
    timestamp       TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    model           TEXT NOT NULL,
    provider        TEXT NOT NULL,
    input_tokens    INTEGER NOT NULL,
    output_tokens   INTEGER NOT NULL,
    cached_tokens   INTEGER DEFAULT 0,
    cost_usd        REAL NOT NULL,
    skills_loaded   TEXT,
    tier2_tokens    INTEGER DEFAULT 0
);

-- Agents (agent execution persistence)
CREATE TABLE IF NOT EXISTS agents (
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
CREATE INDEX IF NOT EXISTS idx_agents_session ON agents(session_id);
CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_id);
CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
CREATE INDEX IF NOT EXISTS idx_agents_type_state ON agents(type, state);

-- Resource Access Log (audit trail)
CREATE TABLE IF NOT EXISTS resource_access_log (
    id              INTEGER PRIMARY KEY,
    timestamp       TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    agent_type      TEXT NOT NULL,
    session_id      TEXT,
    schedule_name   TEXT,
    resource_type   TEXT NOT NULL,
    service         TEXT NOT NULL,
    action          TEXT NOT NULL,
    target          TEXT,
    capability_used TEXT NOT NULL,
    credential_key  TEXT,
    status          TEXT NOT NULL,
    error           TEXT,
    tokens_consumed INTEGER DEFAULT 0,
    cost_usd        REAL DEFAULT 0.0,
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
CREATE INDEX IF NOT EXISTS idx_resource_access_agent ON resource_access_log(agent_id);
CREATE INDEX IF NOT EXISTS idx_resource_access_service ON resource_access_log(service);
CREATE INDEX IF NOT EXISTS idx_resource_access_time ON resource_access_log(timestamp);

-- Schema Migrations tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now')),
    direction  TEXT NOT NULL DEFAULT 'up'
);
