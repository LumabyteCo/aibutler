# Journal

Write free-form journal entries with optional mood tracking. Entries are timestamped and typed.

## Quick Example

```
You:    Journal: Had a great day at the park with the kids. Mood: happy
Butler: Journal entry saved (id: 1)

You:    Note to self: check the furnace filter this weekend
Butler: Journal entry saved (id: 2)

You:    Feeling a bit stressed today. Work was rough.
Butler: Journal entry saved (id: 3) -- mood: stressed
```

## Tools

### `journal.write` -- Write a journal entry

| Parameter | Type | Required | Default |
|-----------|------|----------|---------|
| `content` | string | yes | -- |
| `mood` | string | no | -- |
| `type` | string | no | `"journal"` |

The `type` field lets you categorize entries: `"journal"`, `"note"`, `"gratitude"`, `"reflection"`, or any custom value.

Capability: `data.journal.write`

## Planned Tools

- `journal.read` -- Read past entries by date range or search terms (capability: `data.journal.read`)
- `journal.mood_trend` -- Show mood patterns over time

## Table: `user_journal`

```sql
id          INTEGER PRIMARY KEY
type        TEXT NOT NULL DEFAULT 'journal'
content     TEXT NOT NULL
mood        TEXT                   -- e.g., "happy", "stressed", "calm"
date        TEXT NOT NULL DEFAULT (date('now'))
created_at  TEXT NOT NULL DEFAULT (datetime('now'))
```

## Mood Values

There is no fixed mood list -- use whatever feels natural. Common values:

`happy`, `calm`, `grateful`, `excited`, `neutral`, `tired`, `anxious`, `stressed`, `sad`, `frustrated`

## Privacy

All journal entries are stored locally in SQLite. Encrypted at rest when a database passphrase is configured. No cloud sync. Journal content is among the most private data -- `data.journal.read` is flagged as high-sensitivity in the capability system.
