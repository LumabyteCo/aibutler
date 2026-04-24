# Telegram

Talk to AI Butler from Telegram. Supports text, photos, voice messages, and file attachments.

## What You Get

- Text, image, voice, and file messages
- Voice transcription (OGG via Whisper STT)
- Streaming responses via message editing
- Typing indicator ("typing..." in chat)

## Setup (5 minutes)

### 1. Create a Bot

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot`
3. Choose a name (e.g., "My AI Butler")
4. Choose a username (must end in `bot`, e.g., `my_aibutler_bot`)
5. Copy the **HTTP API token** BotFather gives you

### 2. Store the Token

Add it to the AI Butler credential vault (not plain text config):

```bash
aibutler vault set telegram_token "123456:ABC-DEF..."
```

### 3. Configure

In `~/.aibutler/config.yaml`:

```yaml
settings:
  active_channels:
    - telegram

configurations:
  channels:
    telegram:
      enabled: true
      typing_indicators: true
      voice_response: auto
```

### 4. Start and Test

```bash
aibutler start
```

Open Telegram and send "hello" to your bot. You should get a response.

## How It Works

The adapter uses **long polling** (`getUpdates`) to receive messages -- no webhooks or public URL required. Responses stream back by sending a message and then editing it as tokens arrive (`editMessageText`). Typing indicators use Telegram's `sendChatAction` API.

### Supported Message Types

| Type | Envelope Type | Notes |
|------|---------------|-------|
| Text | `text` | Plain messages |
| Photo | `image` | Largest resolution selected, always JPEG |
| Voice | `voice` | OGG format, transcribed via Whisper |
| Document | `file` | Any file type |

## Troubleshooting

**Bot doesn't respond:**
- Verify the token: `aibutler vault get telegram_token`
- Make sure `telegram` is in `settings.active_channels`
- Check logs: `aibutler logs --channel telegram`

**"invalid chat ID" errors:**
- This means the adapter received a non-numeric chat ID. Usually a bug -- file an issue.

**Voice messages not transcribed:**
- Confirm `configurations.voice.stt_provider` is set to `whisper`
- Check that the Whisper API key is in the vault

**Bot responds but very slowly:**
- Long polling has a 30-second timeout per cycle. First response after idle may take a moment.
- Check `options.typing.interval_ms` (default 3000) for typing indicator frequency.
