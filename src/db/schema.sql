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
    theme_mode TEXT DEFAULT 'auto',
    page_width_mode TEXT DEFAULT 'standard',
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

-- 13. 工具箱设置注册表
CREATE TABLE IF NOT EXISTS settings_registry (
    domain TEXT PRIMARY KEY,
    defaults_json TEXT,
    schema_json TEXT,
    mask_fields_json TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 14. 后台任务状态
CREATE TABLE IF NOT EXISTS toolbox_jobs (
    name TEXT PRIMARY KEY,
    interval_ms INTEGER,
    last_run_at DATETIME,
    next_run_at DATETIME,
    last_error TEXT,
    enabled INTEGER DEFAULT 1,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(is_active, expires_at);
CREATE INDEX IF NOT EXISTS idx_operation_logs_table ON operation_logs(table_name, created_at);
CREATE INDEX IF NOT EXISTS idx_toolbox_jobs_enabled ON toolbox_jobs(enabled, next_run_at);

-- 插入默认用户设置
INSERT OR IGNORE INTO user_settings (id, custom_css, module_visibility, module_order)
VALUES (
    1,
    '',
    '{"openai":true,"gemini-cli":true,"dns":true,"server":true}',
    '["openai","gemini-cli","dns","server"]'
);
