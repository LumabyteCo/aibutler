# API Keys

AI Butler uses a BYOK (Bring Your Own Keys) model. You provide the API keys; the vault stores them encrypted.

## Credential Types

The vault supports 10 credential types for different auth patterns:

| Type               | Value              | Used by                        |
|--------------------|--------------------|--------------------------------|
| API Key            | `api_key`          | OpenAI, Anthropic, weather, maps, etc. |
| OAuth2             | `oauth2`           | GitHub, Gmail, Google Calendar |
| Bot Token          | `bot_token`        | Telegram, Slack, Discord       |
| App Password       | `app_password`     | Legacy service auth            |
| Platform Token     | `platform_token`   | Platform-specific tokens       |
| Webhook Secret     | `webhook_secret`   | Webhook signature verification |
| Device Identity    | `device_identity`  | IoT device credentials         |
| Session Key        | `session_key`      | Session-based auth             |
| Health Key         | `health_key`       | Health endpoint auth           |
| IoT PIN            | `iot_pin`          | IoT device PINs                |

## Credential Structure

Each credential stored in the vault:

```go
Credential{
    Key:          "openai",                    // Service key
    Type:         CredAPIKey,                  // One of the 10 types above
    Value:        []byte("sk-..."),            // The secret material
    ExpiresAt:    nil,                         // nil = never expires
    RefreshToken: nil,                         // For OAuth2 only
    Metadata:     map[string]string{...},      // Scopes, token URL, etc.
}
```

## Which Services Need Keys

**LLM Providers** (at least one required):

| Service      | Key             | Header format                    |
|--------------|-----------------|----------------------------------|
| `anthropic`  | `anthropic`     | `x-api-key: {key}`              |
| `openai`     | `openai`        | `Authorization: Bearer {key}`   |

**Messaging Channels** (for the channels you use):

| Service      | Key             | Header format                    |
|--------------|-----------------|----------------------------------|
| `telegram`   | `telegram`      | Passed as URL path parameter     |
| `slack`      | `slack`         | `Authorization: Bearer {key}`   |
| `discord`    | `discord`       | `Authorization: Bot {key}`      |

**External APIs** (optional, based on features you use):

| Service          | Key               | Header format                       |
|------------------|-------------------|-------------------------------------|
| `openweathermap` | `openweathermap`  | Query parameter                     |
| `google_maps`    | `google_maps`     | Query parameter                     |
| `newsapi`        | `newsapi`         | Query parameter                     |
| `alpha_vantage`  | `alpha_vantage`   | Query parameter                     |
| `stability_ai`   | `stability_ai`    | `Authorization: Bearer {key}`       |
| `elevenlabs`     | `elevenlabs`      | `xi-api-key: {key}`                |
| `deepgram`       | `deepgram`        | `Authorization: Token {key}`        |
| `tavily`         | `tavily`          | Query parameter                     |
| `perplexity`     | `perplexity`      | `Authorization: Bearer {key}`       |
| `deepl`          | `deepl`           | `Authorization: DeepL-Auth-Key {key}` |

## Security

- **Encrypted at rest** -- file backend uses age encryption with a passphrase; keychain backend uses OS-level encryption
- **Never logged** -- `ZeroBytes()` overwrites secret material when no longer needed
- **Scoped access** -- the service registry maps domains to credential keys, so only the correct key is sent to each API
- **Expiry support** -- credentials can have an `ExpiresAt` timestamp; OAuth2 credentials support `RefreshToken` for automatic renewal

## Custom Services

Override or extend the registry with `~/.aibutler/vault/registry.yaml`:

```yaml
my_custom_api:
  domains: ["api.custom.example.com"]
  auth_type: "api_key"
  credential_key: "my_custom_api"
  header: "Authorization: Bearer {key}"
```

User-defined entries override built-in defaults for the same service name.
