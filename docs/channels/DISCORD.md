# Discord

Talk to AI Butler from a Discord server. Uses the Gateway API over WebSocket -- no HTTP webhook server needed.

## What You Get

- Message handling in server channels and DMs
- Streaming responses via message editing
- Typing indicator ("Bot is typing...")
- Bot loop prevention (ignores bot-authored messages)

## Setup (10 minutes)

### 1. Create a Discord Application

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications)
2. Click **New Application**, give it a name (e.g., "AI Butler")

### 2. Create a Bot

1. Go to the **Bot** section in your application
2. Click **Add Bot**
3. Copy the **Token** -- this is your bot token

### 3. Enable Gateway Intents

In the **Bot** section, under **Privileged Gateway Intents**, enable **Message Content Intent**. This is required -- without it, `msg.Content` will be empty.

The adapter automatically requests Guild Messages and DM Messages intents at connect time.

### 4. Invite the Bot to Your Server

1. Go to **OAuth2 > URL Generator**
2. Select scopes: `bot`
3. Select bot permissions: `Send Messages`, `Read Message History`
4. Copy the generated URL and open it in your browser
5. Select your server and authorize

### 5. Store the Token

```bash
aibutler vault set discord_token "MTIzNDU2..."
```

### 6. Configure

In `~/.aibutler/config.yaml`:

```yaml
settings:
  active_channels:
    - discord

configurations:
  channels:
    discord:
      enabled: true
      typing_indicators: true
```

### 7. Start and Test

```bash
aibutler start
```

Go to a channel where the bot has access and send a message. The bot should respond.

## How It Works

The adapter connects to **Discord Gateway v10** over WebSocket. On connect, it receives a Hello event (heartbeat interval), sends Identify with the bot token and intents, then enters the read loop. Incoming `MESSAGE_CREATE` events are normalized into envelopes. Bot messages (`author.bot = true`) are skipped. Responses stream via message editing. Typing indicators use Discord's trigger typing endpoint.

## Troubleshooting

**Bot shows online but doesn't respond:**
- Make sure **Message Content Intent** is enabled in the Developer Portal (Bot > Privileged Gateway Intents). Without it, `msg.Content` will be empty.
- Verify the token: `aibutler vault get discord_token`
- Check that `discord` is in `settings.active_channels`

**"expected hello (op 10)" error on start:**
- The Gateway connection received an unexpected first frame. Check your network or try again -- Discord may be rate-limiting.

**Bot responds in some channels but not others:**
- The bot needs permission to read and send in each channel. Check the channel's permission overrides in Discord server settings.

**"ws connect" error:**
- Could not reach `gateway.discord.gg`. Check firewall rules or proxy settings.
