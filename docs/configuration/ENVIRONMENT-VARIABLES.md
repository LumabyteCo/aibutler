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

## Systemd

The systemd unit file (`dist/systemd/aibutler.service`) sets:

```ini
Environment=AIBUTLER_HOME=/home/aibutler/.aibutler
```

Note: `AIBUTLER_HOME` is used by the systemd unit to set the working directory context.
The data directory itself is hardcoded as `~/.aibutler` (from `os.UserHomeDir()`).

## Summary

Only one env var directly affects runtime behavior: `AIBUTLER_CONFIG`.
All `AIBUTLER_*` vars are readable as credentials via the env vault fallback.
