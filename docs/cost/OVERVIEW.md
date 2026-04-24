# Cost Tracking

AI Butler tracks every LLM call -- tokens in, tokens out, and USD cost -- in a SQLite `token_usage` table. You always know what you're spending.

## Quick Look

```bash
# See this month's spend, budget, and strategy
aibutler cost status

# Output:
# === Cost Status ===
#   This Month:    $1.2340
#   Budget:        $10.00
#   Remaining:     $8.7660
#   Strategy:      balanced
#   Alert:         warn (75% used)

# Break down costs by model
aibutler cost breakdown

# Output:
# MODEL                          CALLS      INPUT       OUTPUT       COST
# -----                          -----      -----       ------       ----
# claude-sonnet-4-6                 42       18400       12300   $0.9180
# haiku                              8        3200        1100   $0.3160
```

## How It Works

Every model call records a `UsageEntry`:

| Field          | Type     | Description                              |
|----------------|----------|------------------------------------------|
| `SessionID`    | string   | Which session made the call              |
| `Model`        | string   | Model name (e.g. `claude-sonnet-4-6`)    |
| `Provider`     | string   | Provider name                            |
| `InputTokens`  | int      | Tokens sent to the model                 |
| `OutputTokens` | int      | Tokens received from the model           |
| `CachedTokens` | int      | Tokens served from cache                 |
| `CostUSD`      | float64  | Calculated USD cost for this call        |
| `SkillsLoaded` | []string | Which skills were active                 |
| `Tier2Tokens`  | int      | Tokens from Tier 2 context               |

Costs are summed from the first of the current month (`YYYY-MM-01T00:00:00Z`).

## CLI Commands

| Command                          | What it does                          |
|----------------------------------|---------------------------------------|
| `aibutler cost status`           | Monthly spend, budget, remaining, strategy |
| `aibutler cost breakdown`        | Per-model token and cost breakdown    |
| `aibutler cost history`          | Current month total                   |
| `aibutler cost strategy`         | Show current strategy                 |
| `aibutler cost strategy <name>`  | Switch strategy (frugal/balanced/quality) |
| `aibutler cost budget`           | Show current monthly budget           |
| `aibutler cost budget <amount>`  | Set monthly budget in USD             |

## Related Docs

- [COST-STRATEGIES.md](COST-STRATEGIES.md) -- frugal, balanced, quality
- [BUDGET-SYSTEM.md](BUDGET-SYSTEM.md) -- alerts and budget enforcement
