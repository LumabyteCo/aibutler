# Authentication & Credential Vault

AI Butler stores API keys and tokens in an encrypted vault. Three backends are available, selected automatically based on your environment.

## Quick Look

```bash
# Check vault health
aibutler auth status
# Vault status: healthy

# List stored credentials
aibutler auth list
# Stored credentials:
#   - anthropic
#   - openai
#   - telegram
#   - slack

# Revoke a credential
aibutler auth revoke telegram
# Revoked credential: telegram
```

## Vault Backends

The vault auto-selects a backend in this priority order:

| Priority | Backend           | When used                                    |
|----------|-------------------|----------------------------------------------|
| 1        | Environment vars  | `ForceEnv: true` (CI, containers)            |
| 2        | Encrypted file    | `ForceFile: true` or passphrase provided     |
| 3        | System keychain   | macOS Keychain / Linux secret service / Windows Credential Manager |
| 4        | Environment vars  | Fallback if keychain unavailable and no passphrase |

Config for the file backend:

| Field        | Description                              |
|--------------|------------------------------------------|
| `VaultDir`   | Path to `~/.aibutler/vault/`             |
| `Passphrase` | Encryption passphrase for age file vault |

## 18 Registered Services

The service registry maps domains to credential keys and auth patterns:

| Service          | Auth Type      | Credential Key    | Domain(s)                   |
|------------------|----------------|-------------------|-----------------------------|
| `openai`         | `api_key`      | `openai`          | api.openai.com              |
| `anthropic`      | `api_key`      | `anthropic`       | api.anthropic.com           |
| `github`         | `oauth2`       | `github`          | api.github.com              |
| `gmail`          | `oauth2`       | `gmail`           | gmail.googleapis.com        |
| `google_calendar`| `oauth2`       | `google_calendar` | www.googleapis.com          |
| `telegram`       | `bot_token`    | `telegram`        | api.telegram.org            |
| `slack`          | `bot_token`    | `slack`           | slack.com, api.slack.com    |
| `discord`        | `bot_token`    | `discord`         | discord.com, api.discord.com|
| `openweathermap` | `api_key`      | `openweathermap`  | api.openweathermap.org      |
| `google_maps`    | `api_key`      | `google_maps`     | maps.googleapis.com         |
| `newsapi`        | `api_key`      | `newsapi`         | newsapi.org                 |
| `alpha_vantage`  | `api_key`      | `alpha_vantage`   | www.alphavantage.co         |
| `stability_ai`   | `api_key`      | `stability_ai`    | api.stability.ai            |
| `elevenlabs`     | `api_key`      | `elevenlabs`      | api.elevenlabs.io           |
| `deepgram`       | `api_key`      | `deepgram`        | api.deepgram.com            |
| `tavily`         | `api_key`      | `tavily`          | api.tavily.com              |
| `perplexity`     | `api_key`      | `perplexity`      | api.perplexity.ai           |
| `deepl`          | `api_key`      | `deepl`           | api-free.deepl.com, api.deepl.com |

## Vault Interface

```go
type Vault interface {
    Store(ctx, cred)     error          // Add or update a credential
    Get(ctx, key)        (Credential, error)  // Retrieve by key
    Delete(ctx, key)     error          // Remove a credential
    List(ctx)            ([]string, error)    // All stored keys
    Has(ctx, key)        (bool, error)  // Check existence
    HealthCheck(ctx)     error          // Verify vault is operational
}
```

## CLI Commands

| Command                        | What it does                     |
|--------------------------------|----------------------------------|
| `aibutler auth list`           | Show all stored credential keys  |
| `aibutler auth status`         | Vault health check               |
| `aibutler auth revoke <svc>`   | Delete a credential by key       |
