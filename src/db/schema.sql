-- API Monitor 核心数据库表结构 (Core)
-- 使用 SQLite 数据库

-- 1. 系统配置表
CREATE TABLE IF NOT EXISTS system_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    description TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 2. 会话管理表
CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    password TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    is_active INTEGER DEFAULT 1
);

-- 11. 用户设置表
CREATE TABLE IF NOT EXISTS user_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- 单例模式，只允许一条记录
    custom_css TEXT,
    module_visibility TEXT, -- JSON 格式
    module_order TEXT, -- JSON 格式
    channel_enabled TEXT, -- JSON 格式: 启用的渠道
    channel_model_prefix TEXT, -- JSON 格式: 渠道模型前缀
    load_balancing_strategy TEXT DEFAULT 'random', -- 负载均衡策略: random/round-robin
    server_ip_display_mode TEXT DEFAULT 'normal', -- 主机 IP 显示模式: normal/masked/hidden
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 12. 操作日志表（用于审计）
CREATE TABLE IF NOT EXISTS operation_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_type TEXT NOT NULL, -- 'create', 'update', 'delete'
    table_name TEXT NOT NULL,
    record_id TEXT,
    details TEXT, -- JSON 格式存储详细信息
    ip_address TEXT,
    user_agent TEXT,
    trace_id TEXT, -- 关联 Trace ID
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(is_active, expires_at);
CREATE INDEX IF NOT EXISTS idx_operation_logs_table ON operation_logs(table_name, created_at);

-- 插入默认用户设置
INSERT OR IGNORE INTO user_settings (id, custom_css, module_visibility, module_order)
VALUES (
    1,
    '',
    '{"openai":true,"antigravity":true,"gemini-cli":true,"dns":true,"server":true}',
    '["openai","antigravity","gemini-cli","dns","server"]'
);

-- 聊天会话表
CREATE TABLE IF NOT EXISTS chat_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL DEFAULT '新对话',
    model TEXT,
    endpoint_id TEXT,
    persona_id INTEGER,
    system_prompt TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 聊天消息表
CREATE TABLE IF NOT EXISTS chat_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    role TEXT NOT NULL, -- 'user', 'assistant', 'system'
    content TEXT NOT NULL,
    reasoning TEXT, -- Optional reasoning/thinking content
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_updated ON chat_sessions(updated_at DESC);

-- 人设表
CREATE TABLE IF NOT EXISTS chat_personas (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    icon TEXT DEFAULT 'fa-robot',
    is_default INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 默认人设数据
INSERT OR IGNORE INTO chat_personas (id, name, system_prompt, icon, is_default) VALUES 
    (1, '默认助手', '你是一个有用的 AI 助手。', 'fa-robot', 1);