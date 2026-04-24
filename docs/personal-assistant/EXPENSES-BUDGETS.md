# Expenses & Budgets

Track spending by category and get summaries over time. Budget alerts use the `user_budgets` table.

## Quick Example

```
You:    I spent $45 on dinner
Butler: Expense logged (id: 1): 45.00 USD in food

You:    Also $30 on Uber
Butler: Expense logged (id: 2): 30.00 USD in transport

You:    How much did I spend this month?
Butler: food:      $45.00 (1 transaction)
        transport: $30.00 (1 transaction)

You:    What about March?
Butler: [summary for 2026-03]
```

## Tools

### `expense.log` -- Record an expense

| Parameter | Type | Required | Default |
|-----------|------|----------|---------|
| `amount` | number | yes | -- |
| `category` | string | yes | -- |
| `description` | string | no | -- |
| `currency` | string | no | `"USD"` |

Capability: `data.finance.write`

### `expense.summary` -- Spending breakdown by category

| Parameter | Type | Required | Notes |
|-----------|------|----------|-------|
| `period` | string | no | Month as `YYYY-MM`, or omit for all time |

Returns category, total, currency, and transaction count grouped by category. Capability: `data.finance.read`

## Tables

### `user_expenses`

```sql
id          INTEGER PRIMARY KEY
amount      REAL NOT NULL
currency    TEXT NOT NULL DEFAULT 'USD'
category    TEXT NOT NULL
description TEXT
date        TEXT NOT NULL DEFAULT (date('now'))
created_at  TEXT NOT NULL DEFAULT (datetime('now'))
```

Indexed on `date` and `category`.

### `user_budgets`

```sql
id          INTEGER PRIMARY KEY
category    TEXT NOT NULL
amount      REAL NOT NULL
period      TEXT NOT NULL DEFAULT 'monthly'
created_at  TEXT NOT NULL DEFAULT (datetime('now'))
UNIQUE(category, period)
```

Budget tools are planned. The table is ready for per-category monthly/weekly limits.

## Privacy

All expenses are stored locally in SQLite. Encrypted at rest when a database passphrase is configured. No cloud sync. Financial data never leaves your device.
