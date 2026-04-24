# Tasks & Lists

Manage to-do items organized into named lists, with priority levels and completion tracking.

## Quick Example

```
You:    Add "buy groceries" to my shopping list, high priority
Butler: Task added (id: 1)

You:    What's on my shopping list?
Butler: 1. [pending] buy groceries (priority: 1)

You:    Done with the groceries
Butler: Task completed
```

## Tools

### `task.add` -- Add a new task

| Parameter | Type | Required | Default |
|-----------|------|----------|---------|
| `content` | string | yes | -- |
| `list` | string | no | `"default"` |
| `priority` | integer | no | `0` |

Capability: `data.tasks.write`

### `task.list` -- List tasks with optional filters

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `list` | string | no | Filter by list name |
| `status` | string | no | `"pending"` or `"completed"` |

Returns up to 50 tasks, ordered by priority (desc) then creation time (desc). Capability: `data.tasks.read`

### `task.complete` -- Mark a task done

| Parameter | Type | Required |
|-----------|------|----------|
| `id` | integer | yes |

Sets `status = 'completed'` and records `completed_at` timestamp. Capability: `data.tasks.write`

## Table: `user_tasks`

```sql
id           INTEGER PRIMARY KEY
list_name    TEXT NOT NULL DEFAULT 'default'
content      TEXT NOT NULL
status       TEXT NOT NULL DEFAULT 'pending'
priority     INTEGER DEFAULT 0
due_at       TEXT
tags         TEXT
created_at   TEXT NOT NULL DEFAULT (datetime('now'))
completed_at TEXT
UNIQUE(list_name, content, status)
```

## Privacy

All tasks are stored locally in SQLite. Encrypted at rest when a database passphrase is configured. No cloud sync.
