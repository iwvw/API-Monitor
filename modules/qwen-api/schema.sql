-- e:\Code\api-monitor\modules\qwen-api\schema.sql
CREATE TABLE IF NOT EXISTS qwen_accounts (
    id TEXT PRIMARY KEY,
    name TEXT,
    email TEXT,
    mobile TEXT,
    password TEXT,
    token TEXT,
    refresh_token TEXT,
    uid TEXT,
    enable INTEGER DEFAULT 1,
    status TEXT DEFAULT 'unknown',
    last_use_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS qwen_settings (
    key TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE IF NOT EXISTS qwen_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id TEXT,
    account_id TEXT,
    model TEXT,
    prompt TEXT,
    response TEXT,
    messages TEXT,
    tokens INTEGER,
    status TEXT,
    error TEXT,
    duration INTEGER,
    first_token_time_ms INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS qwen_session_cache (
    content_key TEXT PRIMARY KEY,
    session_id TEXT,
    parent_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS qwen_model_redirects (
    source_model TEXT PRIMARY KEY,
    target_model TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
