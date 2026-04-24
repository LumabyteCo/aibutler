# Quick Start

**End result:** AI Butler running locally, accepting messages via WebChat at `http://localhost:3377`.

## Prerequisites

- Go 1.22+

## Install

```bash
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
CGO_ENABLED=0 go build -o aibutler .
```

## Verify

```bash
./aibutler version
# aibutler v0.1.0
```

## Setup

```bash
./aibutler setup
# Creates ~/.aibutler/config.yaml
# Asks which AI provider (Claude, GPT, Ollama)
# Stores your API key in the encrypted vault
```

## Start

```bash
./aibutler start
# AI Butler v0.1.0 starting...
# Scheduler started.
#
# WebChat: http://localhost:3377
#
# Ready. Press Ctrl+C to stop.
```

Open the WebChat URL in your browser and send a message.

## What just happened

First run creates `~/.aibutler/` with:

| File | Purpose |
|------|---------|
| `config.yaml` | Your configuration |
| `aibutler.db` | SQLite database |
| `vault/` | Encrypted credential storage |

## Useful commands

```bash
./aibutler setup            # Show current settings
./aibutler config show      # Display full configuration
./aibutler cost status      # Check spending
./aibutler help             # List all commands
```

## Next steps

- [Setup](SETUP.md) -- configure language, model, and persona
- [Choose Your AI](CHOOSE-YOUR-AI.md) -- pick a model provider (Claude, OpenAI, Ollama)
