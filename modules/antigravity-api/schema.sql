-- Antigravity API 模块数据库表结构

-- Antigravity 账号表
CREATE TABLE IF NOT EXISTS antigravity_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    password TEXT,
    api_key TEXT,
    panel_user TEXT,
    panel_password TEXT,
    enable INTEGER DEFAULT 1,
    status TEXT DEFAULT 'unknown',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used DATETIME
);

-- Antigravity Token 表
CREATE TABLE IF NOT EXISTS antigravity_tokens (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_in INTEGER,
    timestamp INTEGER,
    project_id TEXT,
    email TEXT,
    user_id TEXT,
    user_email TEXT,
    enable INTEGER DEFAULT 1,
    FOREIGN KEY (account_id) REFERENCES antigravity_accounts(id) ON DELETE CASCADE
);

-- Antigravity 调用日志表
CREATE TABLE IF NOT EXISTS antigravity_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id TEXT,
    request_path TEXT,
    request_method TEXT,
    status_code INTEGER,
    duration_ms INTEGER,
    client_ip TEXT,
    user_agent TEXT,
    detail TEXT,  -- 存储完整的请求和响应快照 (JSON)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Antigravity 设置表
CREATE TABLE IF NOT EXISTS antigravity_settings (
    key TEXT PRIMARY KEY,
    value TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Antigravity 模型配置表
CREATE TABLE IF NOT EXISTS antigravity_model_config (
    model_id TEXT PRIMARY KEY,
    enabled INTEGER DEFAULT 1,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_antigravity_tokens_account ON antigravity_tokens(account_id);
CREATE INDEX IF NOT EXISTS idx_antigravity_logs_account ON antigravity_logs(account_id);
