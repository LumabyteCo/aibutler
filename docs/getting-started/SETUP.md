# Setup

## First run

The first time you run `./aibutler`, it creates `~/.aibutler/` with a default `config.yaml`.

Check current settings:

```bash
./aibutler setup
# === AI Butler Setup ===
#
# Current configuration:
#   Persona:          Butler
#   Language:         en
#   Timezone:         UTC
#   Model:            claude-sonnet-4-6
#   Agent Mode:       auto
#   Active Channels:  terminal
#   Cost Strategy:    balanced
#   Monthly Budget:   $10.00
#
# Config file: /home/you/.aibutler/config.yaml
```

## Config file location

Default: `~/.aibutler/config.yaml`

Override with the `AIBUTLER_CONFIG` environment variable:

```bash
AIBUTLER_CONFIG=/path/to/config.yaml ./aibutler
```

## What to change first

Edit `~/.aibutler/config.yaml`:

```yaml
settings:
  language: en              # Language code
  timezone: UTC             # IANA timezone (e.g. America/New_York)
  model: claude-sonnet-4-6  # Primary AI model
  persona_name: Butler      # How the AI refers to itself
  agent_mode: auto          # auto | single
  cost:
    strategy: balanced      # frugal | balanced | quality
    monthly_budget: 10.00   # USD spending cap
```

Restart AI Butler after editing.

## The Three Enriches

Configuration is split into three layers:

| Layer | Who | What |
|-------|-----|------|
| **Settings** | Everyone | Language, model, persona, cost budget |
| **Configurations** | Power users | Model wiring, channel config, security, MCP servers |
| **Options** | Developers | Token limits, timeouts, tuning knobs |

Most users only need `settings`. See `./aibutler config show` for the full picture.

## Defaults reference

| Setting | Default |
|---------|---------|
| `language` | `en` |
| `timezone` | `UTC` |
| `model` | `claude-sonnet-4-6` |
| `persona_name` | `Butler` |
| `agent_mode` | `auto` |
| `cost.strategy` | `balanced` |
| `cost.monthly_budget` | `10.00` |
| `notifications` | `true` |
| `morning_briefing` | `8:00 AM` |
| `active_channels` | `[terminal]` |

## Next steps

- [Choose Your AI](CHOOSE-YOUR-AI.md) -- pick Claude, OpenAI, or Ollama
