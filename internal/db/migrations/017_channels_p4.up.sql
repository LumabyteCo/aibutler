-- Phase 4, Layer 3: Channels — Teams, Google Chat, Webhook & Nostr

CREATE TABLE IF NOT EXISTS teams_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    direction TEXT NOT NULL CHECK(direction IN ('inbound','outbound')),
    conversation_id TEXT NOT NULL,
    from_user TEXT,
    body TEXT NOT NULL,
    message_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_teams_messages_conv ON teams_messages(conversation_id);

CREATE TABLE IF NOT EXISTS webhook_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    direction TEXT NOT NULL CHECK(direction IN ('inbound','outbound')),
    channel_id TEXT,
    sender TEXT,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_webhook_messages_channel ON webhook_messages(channel_id);
