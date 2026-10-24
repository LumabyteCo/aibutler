# Agent Modes

Modes control how agents select and use tools, and (since v0.2) whether
the mission engine runtime is active.

## Quick Look

```bash
# Check current mode
aibutler mode
# Current agent mode: auto
#   (resolves to multi at runtime: parallel tool execution)

# Switch to single explicitly
aibutler mode single
# Agent mode switched to: single

# Switch to multi (parallel tool execution; same as auto's runtime behavior)
aibutler mode multi
# Agent mode switched to: multi
#   (parallel tool execution — same as auto's runtime behavior)

# Switch to mission mode (runtime starts on next `aibutler run`)
aibutler mode mission
# Agent mode switched to: mission
#   Mission runtime will start on next `aibutler run`.
#   Create missions via the mission.create tool; the runtime drives them.
```

Switches are written to `config.yaml` immediately, so the next
`aibutler mode` (or new process) sees the new value.

## Available Modes

### `auto` (default)

Resolves to `multi` at runtime: the agent executes independent tool
calls in parallel and has delegation enabled. The recommended setting —
the resolver picks up smarter behavior as future modes mature.

### `multi`

Parallel tool execution with delegation enabled. This is the behavior
`auto` already resolves to; select it explicitly if you want the mode
pinned rather than resolved.

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

### `swarm`

Multi execution plus the `agent.swarm` orchestration tool. The mode itself
resolves to `multi`; the swarm tool is registered independently, so this
selection is about intent and tool visibility rather than a second runtime.
Restart `aibutler run` after switching.

### `custom`

Routes each turn to a user-defined role from
`configurations.agents.custom_roles` (or roles stored in the database).
With no roles configured it behaves like `multi`. The role router is built
at startup, so **restart `aibutler run`** after switching.

| Mode      | Status     | Description                                |
|-----------|------------|--------------------------------------------|
| `auto`    | available  | Resolves to `multi` at runtime             |
| `single`  | available  | One agent, sequential tool execution       |
| `multi`   | available  | Parallel tool execution, delegation enabled |
| `swarm`   | available  | Multi execution + `agent.swarm` tool       |
| `custom`  | available  | Role routing from configured custom roles  |
| `mission` | available  | Mission engine runtime (supervisor + worker pairs) |

## Setting via Config

In `~/.aibutler/config.yaml`:

```yaml
settings:
  agent_mode: "auto"    # auto | single | multi | swarm | custom | mission
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
