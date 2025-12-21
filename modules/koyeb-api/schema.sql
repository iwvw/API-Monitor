-- Koyeb 账号表
CREATE TABLE IF NOT EXISTS koyeb_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    token TEXT NOT NULL,
    email TEXT DEFAULT '',
    balance REAL DEFAULT 0,
    status TEXT DEFAULT 'unknown',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_koyeb_accounts_name ON koyeb_accounts(name);
