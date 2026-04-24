# Scheduled Agents

Run agents on a cron schedule. The scheduler checks for due tasks every tick and launches agents automatically.

## Quick Look

A schedule entry in the `schedules` SQLite table:

| Field       | Example                      |
|-------------|------------------------------|
| `id`        | `sched-morning-brief`        |
| `name`      | `Morning Briefing`           |
| `cron_expr` | `0 8 * * 1-5`               |
| `task`      | `Summarize my emails and calendar` |
| `channel`   | `telegram`                   |
| `enabled`   | `true`                       |

This runs at 8:00 AM Monday through Friday, sends results to Telegram.

## Cron Expressions

Standard 5-field format: `minute hour day-of-month month day-of-week`

```
# Every hour
0 * * * *

# Weekdays at 9am
0 9 * * 1-5

# Every 15 minutes
*/15 * * * *

# First of every month at midnight
0 0 1 * *
```

Supported shortcuts:

| Shortcut    | Equivalent      |
|-------------|-----------------|
| `@hourly`   | `0 * * * *`     |
| `@daily`    | `0 0 * * *`     |
| `@weekly`   | `0 0 * * 0`     |
| `@monthly`  | `0 0 1 * *`     |

Fields support `*`, ranges (`1-5`), lists (`1,3,5`), and steps (`*/15`).

## How the Scheduler Works

1. `Scheduler.Start()` begins a goroutine with a ticker
2. Every tick (default: 60s), `Tick()` runs
3. For each enabled schedule, it parses the cron expression
4. Compares `cron.Next(lastRunTime)` against now
5. If due, records a `Run` entry and launches the agent in a goroutine
6. On completion, updates the run record with status and agent ID

## Schedule Runs

Each execution is tracked in `schedule_runs`:

| Field          | Type     | Values                        |
|----------------|----------|-------------------------------|
| `status`       | string   | `running`, `completed`, `failed` |
| `started_at`   | time     | When the run began            |
| `completed_at` | time     | When the run finished (nullable) |
| `agent_id`     | string   | The agent that was spawned    |
| `error`        | string   | Error message if failed       |

## Configuration

```yaml
configurations:
  schedule:
    enabled: true           # Master switch (default: true)

options:
  schedule:
    tick_interval: 60s      # How often to check for due schedules
    max_concurrent: 3       # Max scheduled agents running at once
```

| Setting          | Default | Config key                         |
|------------------|---------|------------------------------------|
| Enabled          | `true`  | `configurations.schedule.enabled`  |
| Tick interval    | `60s`   | `options.schedule.tick_interval`   |
| Max concurrent   | `3`     | `options.schedule.max_concurrent`  |

## Store Operations

The `Store` provides CRUD for schedules:

| Method                | Description                          |
|-----------------------|--------------------------------------|
| `Create(sched)`      | Insert a new schedule                |
| `Get(id)`            | Retrieve by ID                       |
| `List()`             | All schedules, ordered by name       |
| `Delete(id)`         | Remove a schedule                    |
| `SetEnabled(id, bool)` | Enable or disable                 |
| `RecordRun(run)`     | Insert or update a run record        |
| `LastRun(scheduleID)` | Most recent run for a schedule      |
