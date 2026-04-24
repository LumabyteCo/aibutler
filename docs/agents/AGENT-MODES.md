# Agent Modes

Modes control how agents select and use tools. In v0.1, there are two modes -- both result in single-agent execution.

## Quick Look

```bash
# Check current mode
aibutler mode
# Current agent mode: auto
#   (behaves as single in v0.1)

# Switch to single explicitly
aibutler mode single
# Agent mode switched to: single
```

## v0.1 Modes

### `auto` (default)

Resolves to `single` in v0.1. This is the recommended setting -- it will automatically upgrade to smarter behavior as multi-agent delegation matures.

### `single`

One agent, one tool at a time, no delegation. The agent calls the model, executes any tool calls sequentially, and repeats until done.

## Planned Modes (Not Yet Available)

Requesting these modes in v0.1 gracefully downgrades to `single`:

```bash
aibutler mode multi
# Mode "multi" is not available in v0.1, using single mode.

aibutler mode swarm
# Mode "swarm" is not available in v0.1, using single mode.

aibutler mode custom
# Mode "custom" is not available in v0.1, using single mode.
```

| Mode     | Status          | Description                          |
|----------|-----------------|--------------------------------------|
| `auto`   | v0.1            | Resolves to `single` now             |
| `single` | v0.1            | One agent, sequential tool execution |
| `multi`  | planned         | Multiple sub-agents in parallel      |
| `swarm`  | planned         | Dynamic agent swarm                  |
| `custom` | planned         | User-defined orchestration           |

## Setting via Config

In `~/.aibutler/config.yaml`:

```yaml
settings:
  agent_mode: "auto"    # auto | single (v0.1)
```

Invalid modes are rejected during config validation:

```
config: invalid agent_mode "turbo" (must be auto or single)
```

## How Mode Affects Tools

The effective mode is passed to `ToolExecutor.AvailableTools()`, which filters the tool set based on what the mode allows. In `single` mode, delegation tools (spawn sub-agent, etc.) are excluded.
