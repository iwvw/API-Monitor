-- DeepSeek 模块数据库表

CREATE TABLE IF NOT EXISTS ds_accounts (
    id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    email TEXT DEFAULT '',
    mobile TEXT DEFAULT '',
    password TEXT DEFAULT '',
    token TEXT DEFAULT '',
    enable INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ds_settings (
    key TEXT PRIMARY KEY,
    value TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ds_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id TEXT,
    model TEXT,
    is_balanced INTEGER DEFAULT 0,
    path TEXT,
    method TEXT DEFAULT 'POST',
    status_code INTEGER,
    duration_ms INTEGER,
    first_token_time_ms INTEGER,
    client_ip TEXT,
    user_agent TEXT,
    detail TEXT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ds_model_redirects (
    source_model TEXT PRIMARY KEY,
    target_model TEXT NOT NULL
);

-- 会话缓存表 (用于连续对话)
CREATE TABLE IF NOT EXISTS ds_session_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content_key TEXT NOT NULL,
    session_id TEXT NOT NULL,
    parent_id INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 默认设置
INSERT OR IGNORE INTO ds_settings (key, value) VALUES ('API_KEY', '123456');
INSERT OR IGNORE INTO ds_settings (key, value) VALUES ('DEFAULT_TEMPERATURE', '1');
INSERT OR IGNORE INTO ds_settings (key, value) VALUES ('DEFAULT_MAX_TOKENS', '8192');
INSERT OR IGNORE INTO ds_settings (key, value) VALUES ('SYSTEM_INSTRUCTION', '');
