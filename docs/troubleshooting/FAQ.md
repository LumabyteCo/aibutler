# Frequently Asked Questions

## What AI models does AI Butler support?

AI Butler supports any model via the `ModelAdapter` interface. Configure your provider in `config.yaml`:

```yaml
settings:
  model: "claude-sonnet-4-6"    # Primary model
configurations:
  models:
    primary: "claude-sonnet-4-6"
    fallback: ""                 # Optional fallback model
```

Planned providers include Claude (Anthropic), OpenAI, and Ollama (local models).

## Is it free?

The software is free and open source. AI model costs depend on your provider:
- **Cloud models** (Claude, OpenAI): charged per token by the provider.
- **Local models** (Ollama): $0 after hardware cost.

AI Butler includes built-in cost tracking: `aibutler cost status`.

## Where is my data stored?

Everything lives in `~/.aibutler/`:

```
~/.aibutler/
  config.yaml    # Configuration
  aibutler.db    # SQLite database (WAL mode, optional Adiantum encryption)
  vault/         # Encrypted credentials (keychain, age-encrypted files, or env vars)
```

## Can I use it offline?

Yes, with a local model provider like Ollama. No internet connection is needed for the core runtime. Cloud AI providers obviously require network access.

## What channels work in v0.1?

- **WebChat** -- built-in, runs on `localhost:3377`
- **Telegram** -- bot via Bot API
- **Slack** -- app via Slack API
- **Discord** -- bot via Discord API

WhatsApp is planned for a future release.

## How do I add API keys?

Use the vault, not plain text in config files:

```bash
aibutler auth list     # See what's stored
aibutler auth status   # Check credential health
```

The vault automatically selects a backend: OS keychain (preferred), age-encrypted files, or environment variables (CI/containers).

## What is the Three Enriches?

AI Butler's configuration has three layers:

| Layer | Who | What |
|-------|-----|------|
| **Settings** | Everyone | Language, timezone, model, active channels, cost strategy |
| **Configurations** | Power users | Model wiring, channel configs, agents, security, MCP servers |
| **Options** | Developers | Token limits, timeouts, cache TTLs, tuning knobs |

Settings override Configurations where they overlap (e.g., `settings.model` overrides `configurations.models.primary`).

## How do agents work?

Agents are autonomous task executors with a 6-state lifecycle:

```
spawned -> running -> waiting -> running -> completed
                  \-> failed
                  \-> cancelled
```

States: `spawned`, `running`, `waiting`, `completed`, `failed`, `cancelled`.

Key limits:
- Max concurrent agents: 5 (configurable)
- Max nesting depth: 3 (3 hops = 4 agents max)
- Background timeout: 4 hours
- Max tool calls per run: 50

## Is my data sent anywhere?

Only to the AI provider you configure (e.g., Anthropic API, OpenAI API, or your local Ollama instance). AI Butler does not phone home, collect telemetry, or send data to any other service.

## How do I back up my data?

```bash
aibutler backup now          # Create a backup
aibutler backup list         # List existing backups
aibutler backup verify       # Verify backup integrity
aibutler backup export       # Export for migration
aibutler backup import       # Import from export
```

Backups use the SQLite Online Backup API for consistent snapshots. The database file and vault are stored in `~/.aibutler/`.

## How do I check database health?

```bash
aibutler integrity           # Runs PRAGMA integrity_check
```

This verifies the SQLite database is not corrupt.
