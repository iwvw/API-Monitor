-- Gemini CLI API 模块数据库表结构

-- Gemini CLI 账号表 (存储 OAuth 相关信息)
CREATE TABLE IF NOT EXISTS gemini_cli_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    client_id TEXT,
    client_secret TEXT,
    refresh_token TEXT,
    enable INTEGER DEFAULT 1,
    status TEXT DEFAULT 'unknown',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used DATETIME
);

-- Gemini CLI Token 缓存表
CREATE TABLE IF NOT EXISTS gemini_cli_tokens (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    access_token TEXT NOT NULL,
    expires_at INTEGER,
    project_id TEXT,
    email TEXT,
    enable INTEGER DEFAULT 1,
    FOREIGN KEY (account_id) REFERENCES gemini_cli_accounts(id) ON DELETE CASCADE
);

-- Gemini CLI 设置表
CREATE TABLE IF NOT EXISTS gemini_cli_settings (
    key TEXT PRIMARY KEY,
    value TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Gemini CLI 调用日志表
CREATE TABLE IF NOT EXISTS gemini_cli_logs (
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

-- 索引
CREATE INDEX IF NOT EXISTS idx_gemini_cli_tokens_account ON gemini_cli_tokens(account_id);
CREATE INDEX IF NOT EXISTS idx_gemini_cli_logs_account ON gemini_cli_logs(account_id);
