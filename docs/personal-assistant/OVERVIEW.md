# Personal Assistant

AI Butler includes data tools that manage your everyday life -- tasks, expenses, contacts, health, habits, and journal -- all stored locally in SQLite.

## Quick Example

```
You:    Add "buy groceries" to my tasks
Butler: Task added (id: 1)

You:    I spent $12 on lunch
Butler: Expense logged: 12.00 USD in food (id: 1)

You:    Log my weight: 75kg
Butler: Health metric logged: weight = 75 kg (id: 1)
```

## Features

| Feature | Tools | Doc |
|---------|-------|-----|
| Tasks & Lists | `task.add`, `task.list`, `task.complete` | [TASKS-AND-LISTS.md](TASKS-AND-LISTS.md) |
| Reminders | `reminder.set`, `reminder.list`, `reminder.cancel` | [REMINDERS.md](REMINDERS.md) |
| Contacts | `contact.add`, `contact.search` | [CONTACTS.md](CONTACTS.md) |
| Expenses & Budgets | `expense.log`, `expense.summary` | [EXPENSES-BUDGETS.md](EXPENSES-BUDGETS.md) |
| Health & Wellness | `health.log` | [HEALTH-WELLNESS.md](HEALTH-WELLNESS.md) |
| Habits & Streaks | `habit.create`, `habit.log`, `habit.streak` | [HABITS-STREAKS.md](HABITS-STREAKS.md) |
| Journal | `journal.write` | [JOURNAL.md](JOURNAL.md) |

## Privacy Model

- **Local storage only.** All data lives in a single SQLite database on your device.
- **Encrypted at rest.** The database uses Adiantum VFS encryption (XChaCha12 + AES) when a passphrase is set.
- **Health data gets double encryption.** The `user_health.value` column is a BLOB encrypted at the application level via `HealthEncryptor`, on top of the database-level encryption.
- **No cloud sync.** Nothing leaves your machine unless you explicitly configure a channel to relay it.

## Capability Gates

Each tool requires a specific capability grant. An agent without `data.tasks.write` cannot call `task.add`. Capabilities are scoped per-session and audited.

| Capability | Grants access to |
|------------|------------------|
| `data.tasks.read` | `task.list` |
| `data.tasks.write` | `task.add`, `task.complete` |
| `data.finance.read` | `expense.summary` |
| `data.finance.write` | `expense.log` |
| `data.contacts.read` | `contact.search` |
| `data.contacts.write` | `contact.add` |
| `data.health.write` | `health.log` |
| `data.journal.write` | `journal.write` |
| *(none required)* | `cost.status` |
