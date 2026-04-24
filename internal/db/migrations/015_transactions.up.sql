-- Layer 5: Transactional Actions & AI Services

CREATE TABLE IF NOT EXISTS transactions (
    id TEXT PRIMARY KEY,
    service TEXT NOT NULL,
    category TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'prepare',
    details TEXT NOT NULL DEFAULT '{}',
    amount REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    confirmed_at DATETIME,
    executed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(status);

CREATE TABLE IF NOT EXISTS transaction_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id TEXT NOT NULL,
    action TEXT NOT NULL,
    details TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_transaction_audit_tx ON transaction_audit(transaction_id);
