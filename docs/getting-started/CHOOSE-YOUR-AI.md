# Choose Your AI

AI Butler supports multiple model providers. Pick one and configure it.

## Options at a glance

| Provider | API key needed | Cost | Privacy |
|----------|---------------|------|---------|
| **Claude** (default) | Yes | Pay per token | Cloud |
| **OpenAI GPT** | Yes | Pay per token | Cloud |
| **Ollama** | No | Free | Local, fully private |

## Claude (default)

Works out of the box. Set your API key:

```bash
./aibutler auth list   # Check stored credentials
```

Config -- no changes needed, this is the default:

```yaml
settings:
  model: claude-sonnet-4-6

configurations:
  models:
    primary: claude-sonnet-4-6
```

## OpenAI GPT

```yaml
settings:
  model: gpt-4o

configurations:
  models:
    primary: gpt-4o
```

## Ollama (local, free)

Install Ollama first ([ollama.com](https://ollama.com)), then pull a model:

```bash
ollama pull llama3
```

Configure AI Butler to use it:

```yaml
settings:
  model: ollama/llama3

configurations:
  models:
    primary: ollama/llama3
```

Local models have zero cost -- the cost tracker will report $0.

## Model defaults

These live under `options.models` in your config:

| Option | Default |
|--------|---------|
| `max_tokens` | `8192` |
| `temperature` | `0.7` |
| `request_timeout` | `120s` |
| `retry_count` | `3` |

Override them if needed:

```yaml
options:
  models:
    max_tokens: 4096
    temperature: 0.5
```

## Fallback model

Set a fallback for when the primary model is unavailable:

```yaml
configurations:
  models:
    primary: claude-sonnet-4-6
    fallback: gpt-4o
```

## Cost tracking

Cloud models cost money. AI Butler tracks spending automatically:

```bash
./aibutler cost status      # Current month spending
./aibutler cost history     # Past spending
./aibutler cost breakdown   # Per-model breakdown
./aibutler cost strategy    # Show cost strategy
./aibutler cost budget      # Show budget info
```

Budget settings:

```yaml
settings:
  cost:
    strategy: balanced      # frugal | balanced | quality
    monthly_budget: 10.00   # Alerts at 50%, 75%, 90%, 100%
```

## Next steps

- [Quick Start](QUICK-START.md) -- get running in 2 minutes
- [Setup](SETUP.md) -- configure language, timezone, and persona
