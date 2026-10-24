CREATE TABLE IF NOT EXISTS notifications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    channel      TEXT NOT NULL,
    account_id   TEXT NOT NULL DEFAULT '',
    text         TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    delivered_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_notifications_pending
    ON notifications (channel, delivered_at, id);
