# Environment Variables

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `AIBUTLER_CONFIG` | `~/.aibutler/config.yaml` | Override config file path |

```bash
AIBUTLER_CONFIG=/etc/aibutler/config.yaml aibutler
```

## Credentials (CI / Containers)

The env vault reads any `AIBUTLER_` prefixed variable as a credential.
Key name is derived by stripping the prefix and lowercasing.

| Variable | Becomes Vault Key | Example |
|----------|-------------------|---------|
| `AIBUTLER_ANTHROPIC` | `anthropic` | API key for Claude |
| `AIBUTLER_OPENAI` | `openai` | API key for OpenAI |
| `AIBUTLER_TELEGRAM` | `telegram` | Telegram bot token |

```bash
export AIBUTLER_ANTHROPIC=sk-ant-...
export AIBUTLER_OPENAI=sk-...
aibutler
```

The env vault is for CI/container use only. In production, credentials are stored
encrypted in `~/.aibutler/vault/` using Adiantum (XChaCha12 + AES).

## Systemd / Docker

The systemd unit (`deploy/systemd/aibutler.service`) and the Docker image set:

```ini
Environment=AIBUTLER_DATA=/var/lib/aibutler
```

`AIBUTLER_DATA` overrides the data directory (default `~/.aibutler`). All
state — config (unless `AIBUTLER_CONFIG` points elsewhere), SQLite database,
vault, plugins — lives under this path.

## Summary

| Variable | Effect |
|---|---|
| `AIBUTLER_DATA` | Data directory (default `~/.aibutler`) — used by Docker, systemd, and Helm |
| `AIBUTLER_CONFIG` | Explicit config file path (default: `<data-dir>/config.yaml`) |
| `AIBUTLER_*` (others) | Readable as credentials via the env vault fallback (CI/containers only) |
