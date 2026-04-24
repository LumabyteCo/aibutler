# Health & Wellness

Log health metrics -- weight, blood pressure, sleep, exercise, or any custom metric. Health data gets an extra layer of encryption beyond the database-level Adiantum VFS.

## Quick Example

```
You:    Log my weight: 75kg
Butler: Health metric logged: weight = 75 kg (id: 1)

You:    Blood pressure 120/80 today
Butler: Health metric logged: blood_pressure = 120/80 mmHg (id: 2)

You:    I slept 7 hours last night
Butler: Health metric logged: sleep = 7 hours (id: 3)
```

## Tools

### `health.log` -- Log a health metric

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `metric` | string | yes | e.g., `"weight"`, `"blood_pressure"`, `"sleep"`, `"exercise"` |
| `value` | string | yes | The measurement value |
| `unit` | string | no | e.g., `"kg"`, `"mmHg"`, `"hours"` |
| `notes` | string | no | Additional context |

Any metric name is accepted -- there is no fixed list. Common ones: `weight`, `blood_pressure`, `sleep`, `exercise`, `heart_rate`, `steps`, `water_intake`.

Capability: `data.health.write`

## Planned Tools

- `health.summary` -- Trend data for a metric over a date range
- `health.medication_reminder` -- Built on the reminder system

## Table: `user_health`

```sql
id          INTEGER PRIMARY KEY
metric      TEXT NOT NULL
value       BLOB NOT NULL          -- double-encrypted at application level
unit        TEXT
date        TEXT NOT NULL DEFAULT (date('now'))
notes       TEXT
created_at  TEXT NOT NULL DEFAULT (datetime('now'))
```

Indexed on `(metric, date)` for efficient time-range queries.

## Double Encryption

Health data is the most sensitive category. It has two encryption layers:

1. **Database level:** Adiantum VFS encrypts the entire SQLite file (XChaCha12 + AES).
2. **Application level:** `HealthEncryptor` encrypts the `value` column using a separate 32-byte key with AES-GCM before it reaches the database.

Even if the database passphrase is compromised, health values remain encrypted.

## Privacy

All health data is stored locally in SQLite with double encryption. No cloud sync. Health data never leaves your device.
