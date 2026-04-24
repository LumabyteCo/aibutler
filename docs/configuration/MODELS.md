# Model Configuration

## Quick Setup

Set your model in `~/.aibutler/config.yaml`:

```yaml
settings:
  model: claude-sonnet-4-6
```

Store your API key:

```bash
# Key is stored encrypted in ~/.aibutler/vault/
aibutler auth list    # check stored credentials
```

## Config Fields

### settings.model

The simple way. Sets the primary model for all requests.

```yaml
settings:
  model: claude-sonnet-4-6    # default
```

`settings.model` overrides `configurations.models.primary` at load time.

### configurations.models

For power users who want a fallback chain.

```yaml
configurations:
  models:
    primary: claude-sonnet-4-6
    fallback: haiku
```

When `primary` fails (timeout, rate limit, error), the system retries with `fallback`.

### configurations.agents.default_subagent_model

Sub-agents spawned by the primary agent use this model.

```yaml
configurations:
  agents:
    default_subagent_model: haiku    # default
```

## Tuning Options

| Field | Default | Description |
|-------|---------|-------------|
| `options.models.max_tokens` | `8192` | Max tokens per response |
| `options.models.temperature` | `0.7` | Sampling temperature (0.0-1.0) |
| `options.models.request_timeout` | `120s` | Timeout per API request |
| `options.models.retry_count` | `3` | Retries on transient failure |

```yaml
options:
  models:
    max_tokens: 8192
    temperature: 0.7
    request_timeout: 120s
    retry_count: 3
```

## How Fallback Works

1. Request sent to `primary` model
2. If the request fails (timeout, HTTP error), retry up to `retry_count` times
3. If all retries fail and `fallback` is set, retry the request with `fallback`
4. If no fallback is configured, the error propagates

## Cost Strategy and Models

The `settings.cost.strategy` affects conversation window size, not model selection:

| Strategy | Sliding Window | Use Case |
|----------|---------------|----------|
| `frugal` | 30 messages | Minimize token usage |
| `balanced` | 100 messages | Default, good context retention |
| `quality` | 200 messages | Maximum context, higher cost |

```bash
aibutler cost strategy frugal     # switch strategy
aibutler cost status              # check spend
```

## Local Models (Ollama)

Use a local model for free, private inference. No API key required.

```yaml
settings:
  model: ollama/llama3

configurations:
  models:
    primary: ollama/llama3
    fallback: claude-sonnet-4-6   # cloud fallback
```

Trade-offs: slower inference, no tool calling on some models, limited context window.

## Provider Examples

**Claude (Anthropic):**
```yaml
settings:
  model: claude-sonnet-4-6
```

**OpenAI:**
```yaml
settings:
  model: gpt-4o
```

**Ollama (local):**
```yaml
settings:
  model: ollama/llama3
```
