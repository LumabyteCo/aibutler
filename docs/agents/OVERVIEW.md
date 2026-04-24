# Agents

An agent is a single execution instance that receives a task, calls an LLM in a loop, and uses tools until it produces a final answer or hits a limit.

## Quick Look

```bash
# List active agents
aibutler agent list

# Output:
# ID                                   TYPE         STATE      TASK                           MODEL                CREATED
# --                                   ----         -----      ----                           -----                -------
# a1b2c3d4-...                         primary      running    Summarize today's emails       default              2025-05-01T10:00:00Z

# Get details for a specific agent
aibutler agent status a1b2c3d4-...

# Output:
# === Agent Status ===
#   ID:           a1b2c3d4-...
#   Type:         primary
#   State:        completed
#   Task:         Summarize today's emails
#   Model:        default
#   Tokens:       4200
#   Cost:         $0.0420
#   Tool Calls:   3
#   Duration:     12400ms
#   Created:      2025-05-01T10:00:00Z

# View recent agent history (last 25)
aibutler agent history
```

## Agent Types

| Type        | Value        | Description                    |
|-------------|--------------|--------------------------------|
| Primary     | `primary`    | Direct user-initiated agent    |
| Scheduled   | `scheduled`  | Triggered by cron schedule     |

Future releases will add `subagent` and `background` types.

## Lifecycle States

Every agent follows this 6-state lifecycle:

```
SPAWNED --> RUNNING --> WAITING --> RUNNING --> ... --> COMPLETED
                |          |                            |
                +----------+----> FAILED                |
                |          |                            |
                +----------+----> CANCELLED             |
```

| State       | Meaning                                    | Terminal? |
|-------------|--------------------------------------------|-----------|
| `spawned`   | Created, not yet started                   | No        |
| `running`   | Calling the model or processing response   | No        |
| `waiting`   | Executing tool calls                       | No        |
| `completed` | Finished successfully                      | Yes       |
| `failed`    | Hit an error (model error, etc.)           | Yes       |
| `cancelled` | Timed out or exceeded budget                | Yes       |

## The Agent Loop

1. Agent starts in `spawned`, transitions to `running`
2. Sends messages to the model via `ModelAdapter.Complete()`
3. If model returns no tool calls -- final answer, transition to `completed`
4. If model returns tool calls -- transition to `waiting`, execute each tool
5. Append tool results to messages, transition back to `running`
6. Repeat from step 2

## Limits

| Setting           | Default    | Config key                         |
|-------------------|------------|------------------------------------|
| Max tool calls    | `50`       | `options.agents.max_tool_calls`    |
| Timeout           | `5m`       | `options.agents.subagent_timeout`  |
| Background timeout| `4h`       | `options.agents.background_timeout`|
| Max concurrent    | `5`        | `configurations.agents.max_concurrent` |
| Max nesting depth | `3`        | `configurations.agents.max_depth`  |
| Per-agent budget  | `0` (none) | `Config.BudgetCap` (per agent)     |

When a limit is hit:
- **Max tool calls** -- completes with output `"complexity limit reached"`
- **Timeout** -- cancels with error `"timeout"`
- **Budget cap** -- cancels with error `"budget_exceeded"`

## CLI Commands

| Command                         | What it does                         |
|---------------------------------|--------------------------------------|
| `aibutler agent list`           | Active agents (spawned/running/waiting), last 50 |
| `aibutler agent status <id>`    | Full details for one agent           |
| `aibutler agent history`        | Last 25 agents, all states           |
