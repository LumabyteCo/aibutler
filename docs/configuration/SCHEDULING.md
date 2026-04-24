# Scheduling

## Enable/Disable

```yaml
configurations:
  schedule:
    enabled: true     # default: true
```

## Cron Expression Format

Standard 5-field cron syntax:

```
┌───────── minute (0-59)
│ ┌─────── hour (0-23)
│ │ ┌───── day of month (1-31)
│ │ │ ┌─── month (1-12)
│ │ │ │ ┌─ day of week (0-6, 0=Sunday)
│ │ │ │ │
* * * * *
```

### Supported Syntax

| Syntax | Example | Meaning |
|--------|---------|---------|
| `*` | `* * * * *` | Every value in range |
| Single value | `30 * * * *` | At minute 30 |
| Range | `0-15 * * * *` | Minutes 0 through 15 |
| List | `0,15,30,45 * * * *` | At minutes 0, 15, 30, 45 |
| Step | `*/5 * * * *` | Every 5 minutes |
| Range + step | `0-30/10 * * * *` | At minutes 0, 10, 20, 30 |

### Special Expressions

| Expression | Equivalent | Meaning |
|------------|-----------|---------|
| `@hourly` | `0 * * * *` | Top of every hour |
| `@daily` | `0 0 * * *` | Midnight |
| `@weekly` | `0 0 * * 0` | Midnight on Sunday |
| `@monthly` | `0 0 1 * *` | Midnight on the 1st |

## Tuning Options

| Field | Default | Description |
|-------|---------|-------------|
| `options.schedule.tick_interval` | `60s` | How often the scheduler checks for due tasks |
| `options.schedule.max_concurrent` | `3` | Max scheduled tasks running at once |

```yaml
options:
  schedule:
    tick_interval: 60s
    max_concurrent: 3
```

## Examples

**Morning briefing at 8 AM on weekdays:**
```
0 8 * * 1-5
```

**Every 6 hours:**
```
0 */6 * * *
```

**First day of every month at noon:**
```
0 12 1 * *
```

**Every 15 minutes during business hours:**
```
*/15 9-17 * * 1-5
```

## How It Works

1. Scheduler starts on boot if `configurations.schedule.enabled` is `true`
2. Every `tick_interval` (default 60s), it checks the schedule store for due tasks
3. Due tasks are dispatched as agent runs, up to `max_concurrent` at a time
4. The cron `Next()` function calculates the next fire time after each execution
