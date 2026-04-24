-- Channel message metadata (media tracking, cross-channel relay audit)
CREATE TABLE IF NOT EXISTS channel_messages (
    id           INTEGER PRIMARY KEY,
    session_id   TEXT NOT NULL,
    channel      TEXT NOT NULL,
    account_id   TEXT NOT NULL,
    direction    TEXT NOT NULL,
    message_type TEXT NOT NULL,
    mime_type    TEXT,
    file_size    INTEGER,
    metadata     TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_channel_msgs_session ON channel_messages(session_id);
CREATE INDEX IF NOT EXISTS idx_channel_msgs_channel ON channel_messages(channel, account_id);

-- MCP server connections (persistence across restarts)
CREATE TABLE IF NOT EXISTS mcp_servers (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    command     TEXT NOT NULL,
    args        TEXT,
    env         TEXT,
    transport   TEXT NOT NULL DEFAULT 'stdio',
    url         TEXT,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
