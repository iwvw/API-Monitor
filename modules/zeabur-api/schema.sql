-- Zeabur 模块数据库表结构

-- 3. Zeabur 账号表
CREATE TABLE IF NOT EXISTS zeabur_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    email TEXT,
    username TEXT,
    balance REAL DEFAULT 0,
    cost REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_synced_at DATETIME
);

-- 4. Zeabur 项目表
CREATE TABLE IF NOT EXISTS zeabur_projects (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    name TEXT NOT NULL,
    region TEXT,
    status TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES zeabur_accounts(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_zeabur_accounts_status ON zeabur_accounts(status);
CREATE INDEX IF NOT EXISTS idx_zeabur_projects_account ON zeabur_projects(account_id);
