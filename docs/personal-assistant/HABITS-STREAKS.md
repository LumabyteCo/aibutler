# Habits & Streaks

Track daily or weekly habits and build streaks. The schema supports habit definitions and per-day log entries.

## Quick Example

```
You:    I want to start tracking meditation
Butler: Habit created: meditation (daily)

You:    I meditated today
Butler: Logged! meditation streak: 3 days

You:    What habits am I tracking?
Butler: 1. meditation -- daily, 3-day streak
        2. exercise   -- daily, 1-day streak
```

## Tools (Planned)

> **Status:** The `user_habits` and `user_habit_logs` tables exist in the schema. Habit tools (`habit.create`, `habit.log`, `habit.streak`, `habit.list`) are planned for a future release.

### `habit.create` -- Start tracking a habit

| Parameter | Type | Required | Default |
|-----------|------|----------|---------|
| `name` | string | yes | -- |
| `frequency` | string | no | `"daily"` |

### `habit.log` -- Record today's completion

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `name` | string | yes | Habit name |
| `notes` | string | no | Optional context |

Enforced uniqueness: one log per habit per day (`UNIQUE(habit_id, date)`).

### `habit.streak` -- Show current streak

Returns the consecutive-day count for a given habit.

### `habit.list` -- Show all tracked habits

Returns all habits with current streak and last logged date.

## Tables

### `user_habits`

```sql
id          INTEGER PRIMARY KEY
name        TEXT NOT NULL UNIQUE
frequency   TEXT NOT NULL DEFAULT 'daily'
created_at  TEXT NOT NULL DEFAULT (datetime('now'))
```

### `user_habit_logs`

```sql
id          INTEGER PRIMARY KEY
habit_id    INTEGER NOT NULL       -- FK to user_habits
date        TEXT NOT NULL DEFAULT (date('now'))
notes       TEXT
UNIQUE(habit_id, date)             -- one check-in per day
```

## Privacy

All habit data is stored locally in SQLite. Encrypted at rest when a database passphrase is configured. No cloud sync.
