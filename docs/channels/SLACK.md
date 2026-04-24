# Slack

Talk to AI Butler from your Slack workspace. Uses Socket Mode -- no public URL or webhook server needed.

## What You Get

- Message handling in channels and DMs
- Thread support (replies stay in-thread)
- Streaming responses via message updates
- Bot loop prevention (ignores its own messages)

## Setup (10 minutes)

### 1. Create a Slack App

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App**
2. Choose **From scratch**
3. Name it (e.g., "AI Butler") and select your workspace

### 2. Enable Socket Mode

1. In the app settings, go to **Socket Mode**
2. Toggle **Enable Socket Mode** on
3. Create an **App-Level Token** with the `connections:write` scope
4. Copy this token -- it's the Socket Mode token

### 3. Set Up Event Subscriptions

1. Go to **Event Subscriptions** and toggle on
2. Under **Subscribe to bot events**, add:
   - `message.channels` -- messages in public channels
   - `message.groups` -- messages in private channels
   - `message.im` -- direct messages

### 4. Set Bot Token Scopes

Go to **OAuth & Permissions** and add these **Bot Token Scopes**: `chat:write`, `channels:history`, `groups:history`, `im:history`.

### 5. Install to Workspace

1. Go to **Install App** and click **Install to Workspace**
2. Copy the **Bot User OAuth Token** (`xoxb-...`)

### 6. Store Tokens

```bash
aibutler vault set slack_bot_token "xoxb-..."
aibutler vault set slack_app_token "xapp-..."
```

### 7. Configure

In `~/.aibutler/config.yaml`:

```yaml
settings:
  active_channels:
    - slack

configurations:
  channels:
    slack:
      enabled: true
      typing_indicators: true
```

### 8. Start and Test

```bash
aibutler start
```

Invite the bot to a channel (`/invite @AI Butler`), then send a message. The bot should respond.

## How It Works

The adapter connects via **Socket Mode WebSocket** (no public endpoint). Incoming events are acknowledged with an `envelope_id` response. Only `message` events are processed; bot messages (with `bot_id`) are skipped to prevent loops. Responses stream via `chat.update`. Thread context is preserved using `thread_ts`.

**Note:** Slack has no public typing indicator API for bots. `SendTyping` is a no-op on this adapter.

## Troubleshooting

**Bot doesn't respond in a channel:**
- Make sure the bot is invited to the channel: `/invite @AI Butler`
- Verify both tokens are set: `aibutler vault get slack_bot_token`
- Check that the event subscriptions are enabled and the app is reinstalled after changes

**"no channel for account" error:**
- The bot received a reply before tracking the channel. This resolves on the next message from that user.

**Missing messages:**
- Confirm the correct `*:history` scopes are added for the channel type (public, private, DM).
