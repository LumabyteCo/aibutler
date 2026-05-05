# Agent Modes

Modes control how agents select and use tools, and (since v0.2) whether
the mission engine runtime is active.

## Quick Look

```bash
# Check current mode
aibutler mode
# Current agent mode: auto

# Switch to single explicitly
aibutler mode single
# Agent mode switched to: single

# Switch to mission mode (runtime starts on next `aibutler run`)
aibutler mode mission
# Agent mode switched to: mission
#   Mission runtime will start on next `aibutler run`.
#   Create missions via the mission.create tool; the runtime drives them.
```

## Available Modes

### `auto` (default)

Resolves to `single` in the current release. The recommended setting —
it automatically picks up smarter behaviour as future modes mature.

### `single`

One agent, one tool at a time, no delegation. The agent calls the model,
executes any tool calls sequentially, and repeats until done. This is
the model behind a normal interactive turn.

### `mission`

Activates the mission engine runtime alongside the normal agent loop.
The runtime polls the mission store for `planned` missions every 2s
and spawns a supervisor + worker pair for each, up to a concurrent-
mission cap. Workers run each step through the configured model adapter
with the full tool surface available; supervisors persist progress,
record an audit trail, and surface mission state to the dashboard.

See [MISSIONS.md](MISSIONS.md) for the full mission lifecycle, bus
protocol, and tool reference.

## Planned Modes (Not Yet Available)

Requesting these modes downgrades to `single`:

```bash
aibutler mode multi
# Mode "multi" is not available in v0.1, using single mode.
```

| Mode      | Status     | Description                                |
|-----------|------------|--------------------------------------------|
| `auto`    | available  | Resolves to `single` now                   |
| `single`  | available  | One agent, sequential tool execution       |
| `mission` | available  | Mission engine runtime (supervisor + worker pairs) |
| `multi`   | planned    | Multiple sub-agents in parallel            |
| `swarm`   | planned    | Dynamic agent swarm                        |
| `custom`  | planned    | User-defined orchestration                 |

## Setting via Config

In `~/.aibutler/config.yaml`:

```yaml
settings:
  agent_mode: "auto"    # auto | single | mission
```

Invalid modes are rejected during config validation:

```
config: invalid agent_mode "turbo" (must be auto, single, multi, custom, swarm, or mission)
```

## How Mode Affects Tools

The effective mode is passed to `ToolExecutor.AvailableTools()`, which
filters the tool set based on what the mode allows. In `single` mode,
delegation tools (spawn sub-agent, etc.) are excluded.

`mission` mode doesn't filter tools — the worker's task executor uses
the full tool surface granted to its capability set. The mission
isolation comes from the worker being a fresh agent per step rather
than from tool filtering.
