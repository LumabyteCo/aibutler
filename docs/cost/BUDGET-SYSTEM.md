# Budget System

AI Butler enforces a monthly spending budget with configurable alerts at multiple thresholds.

## Quick Start

```bash
# See current budget and usage
aibutler cost status

# Set a $20/month budget
aibutler cost budget 20
# Monthly budget set to: $20.00

# Check just the budget
aibutler cost budget
# Monthly budget: $20.00
```

## Defaults

| Setting            | Default   | Config key                          |
|--------------------|-----------|-------------------------------------|
| Monthly budget     | `$10.00`  | `settings.cost.monthly_budget`      |
| Alert thresholds   | 50, 75, 90, 100% | `configurations.cost.alerts` |
| Alert channel      | `same`    | `configurations.cost.alert_channel` |
| On budget reached  | `warn`    | `configurations.cost.on_budget_reached` |

## How Alerts Work

When `CheckBudget()` runs, it calculates `(spent / budget) * 100` and finds the highest threshold crossed:

| Threshold | Action  | Message                                          |
|-----------|---------|--------------------------------------------------|
| 50%       | `info`  | "You've used $5.00 of your $10.00 budget (50%)." |
| 75%       | `warn`  | "...Consider switching to frugal mode."           |
| 90%       | `warn`  | "...Almost at budget limit."                      |
| 100%      | `warn` or `pause` | "...Budget reached."                   |

The alert is shown inline in `aibutler cost status`:

```
=== Cost Status ===
  This Month:    $7.5200
  Budget:        $10.00
  Remaining:     $2.4800
  Strategy:      balanced
  Alert:         warn (75% used)
```

## What Happens at 100%

Controlled by `configurations.cost.on_budget_reached`:

- **`warn`** (default) -- shows a warning, keeps running
- **`pause`** -- returns a `pause` action, signaling the system to stop new agent runs

## Per-Agent Budget Cap

Individual agents also support a `BudgetCap` field (USD). If an agent's cumulative `costUSD` reaches its cap, the agent transitions to `cancelled` with error `"budget_exceeded"`. Set `BudgetCap: 0` (the default) for no per-agent limit.

## Config File

```yaml
settings:
  cost:
    strategy: "balanced"
    monthly_budget: 10.00

configurations:
  cost:
    alerts: [50, 75, 90, 100]
    alert_channel: "same"
    on_budget_reached: "warn"    # warn | pause
```

## CLI Reference

| Command                     | What it does                     |
|-----------------------------|----------------------------------|
| `aibutler cost budget`      | Show current monthly budget      |
| `aibutler cost budget 25`   | Set budget to $25.00             |
| `aibutler cost status`      | Full status with alert display   |
