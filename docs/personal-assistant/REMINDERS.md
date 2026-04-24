# Reminders

Set one-off or recurring reminders that fire at a specific time and deliver through your configured channel.

## Quick Example

```
You:    Remind me to call mom at 5pm
Butler: Reminder set for 17:00 today

You:    Remind me every Monday to submit my timesheet
Butler: Recurring reminder set (every Monday)

You:    Show my reminders
Butler: 1. [active] Call mom -- today 17:00
        2. [active] Submit timesheet -- every Monday

You:    Cancel the timesheet reminder
Butler: Reminder cancelled
```

## Tools (Planned)

> **Status:** The `user_reminders` table exists in the schema. Reminder tools (`reminder.set`, `reminder.list`, `reminder.cancel`) are planned for a future release and will integrate with the scheduling system (`internal/schedule`).

### `reminder.set` -- Set a reminder

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `message` | string | yes | What to remind about |
| `remind_at` | string | yes | ISO 8601 datetime |
| `recurrence` | string | no | Cron expression for recurring |
| `channel` | string | no | Delivery channel |

### `reminder.list` -- Show active reminders

Returns all reminders with `status = 'active'`, ordered by `remind_at`.

### `reminder.cancel` -- Cancel a reminder

| Parameter | Type | Required |
|-----------|------|----------|
| `id` | integer | yes |

Sets `status = 'cancelled'`.

## Table: `user_reminders`

```sql
id          INTEGER PRIMARY KEY
message     TEXT NOT NULL
remind_at   TEXT NOT NULL
recurrence  TEXT                -- cron expression for recurring reminders
channel     TEXT                -- delivery channel (telegram, webchat, etc.)
status      TEXT NOT NULL DEFAULT 'active'
created_at  TEXT NOT NULL DEFAULT (datetime('now'))
fired_at    TEXT                -- set when the reminder fires
```

Indexed on `remind_at` where `status = 'active'` for efficient polling.

## Privacy

All reminders are stored locally in SQLite. Encrypted at rest when a database passphrase is configured. No cloud sync.
