# Channels

Channels are how you talk to AI Butler. Each channel is a messaging adapter that receives your messages, passes them to the agent, and sends back responses.

## Channels in v0.1

| Channel | Setup | Streaming | Voice | File Upload | Best For |
|---------|-------|-----------|-------|-------------|----------|
| **WebChat** | None (built-in) | WebSocket | -- | Yes (20 MB) | Quick start, local use |
| **Telegram** | BotFather token | Edit message | Yes (OGG via Whisper) | Yes | Mobile, voice messages |
| **Slack** | Slack App + Socket Mode | Message update | -- | -- | Teams, workspaces |
| **Discord** | Bot + Gateway intents | Message edit | -- | -- | Communities, servers |

## Enabling a Channel

Add the channel name to `settings.active_channels` and configure it under `configurations.channels`:

```yaml
settings:
  active_channels:
    - webchat
    - telegram

configurations:
  channels:
    telegram:
      enabled: true
      typing_indicators: true
      voice_response: auto    # text | voice | auto | both
```

## Per-Channel Config Fields

Every channel supports these fields under `configurations.channels.<name>`:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | false | Activate this channel |
| `typing_indicators` | bool | false | Show "typing..." while processing |
| `voice_response` | string | "text" | How to respond to voice: text, voice, auto, both |

## Setup Guides

- [WebChat](WEBCHAT.md) -- zero setup, works out of the box
- [Telegram](TELEGRAM.md) -- BotFather, 5 minutes
- [Slack](SLACK.md) -- Slack App + Socket Mode, 10 minutes
- [Discord](DISCORD.md) -- Developer Portal + Gateway, 10 minutes

## Architecture

All channels implement the same interface:

```
Channel.Start()      -- begin listening for messages
Channel.Stop()       -- graceful shutdown
Channel.Send()       -- send a response (or edit for streaming)
Channel.SendTyping() -- show typing indicator
```

Messages are normalized into an `Envelope` before reaching the agent. The agent never sees channel-specific details.
