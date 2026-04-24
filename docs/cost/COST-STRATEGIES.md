# Cost Strategies

Three strategies control how aggressively AI Butler spends tokens. The default is `balanced`.

## At a Glance

```bash
# Check current strategy
aibutler cost strategy
# Current strategy: balanced

# Switch to frugal
aibutler cost strategy frugal
# Cost strategy set to: frugal
```

## The Three Strategies

### `frugal` -- Minimize spend

- Sliding window: **30 messages** (smallest context)
- Best for: routine tasks, quick lookups, budget-conscious usage
- Trade-off: model sees less conversation history

### `balanced` -- Default

- Sliding window: **100 messages**
- Best for: everyday use, good quality without overspending
- This is the default when you first install AI Butler

### `quality` -- Maximum capability

- Sliding window: **200 messages** (largest context)
- Best for: complex multi-step tasks, long research sessions
- Trade-off: higher token cost per call

## How Strategies Affect Context

The strategy directly controls `SlidingWindowSize()` -- how many messages are kept in the conversation window sent to the model:

| Strategy   | Window Size | Relative Cost |
|------------|-------------|---------------|
| `frugal`   | 30          | Lowest        |
| `balanced` | 100         | Medium        |
| `quality`  | 200         | Highest       |

Fewer messages in the window means fewer input tokens per call, which means lower cost.

## Setting via Config

In `~/.aibutler/config.yaml`:

```yaml
settings:
  cost:
    strategy: "balanced"    # frugal | balanced | quality
    monthly_budget: 10.00   # USD
```

## Setting via CLI

```bash
# Valid values: frugal, balanced, quality
aibutler cost strategy balanced

# Invalid values are rejected:
aibutler cost strategy turbo
# Error: invalid strategy: turbo (valid: frugal, balanced, quality)
```

## When to Switch

- Running low on budget this month? `aibutler cost strategy frugal`
- Need deep analysis on something important? `aibutler cost strategy quality`
- Back to normal? `aibutler cost strategy balanced`
