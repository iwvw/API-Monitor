-- API Monitor 数据库表结构设计
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

-- 5. Cloudflare 账号表
CREATE TABLE IF NOT EXISTS cf_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    api_token TEXT NOT NULL,
    email TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used DATETIME,
    is_active INTEGER DEFAULT 1
);

-- 6. Cloudflare DNS 模板表
CREATE TABLE IF NOT EXISTS cf_dns_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    records TEXT NOT NULL, -- JSON 格式存储 DNS 记录数组
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 7. Cloudflare 域名表
CREATE TABLE IF NOT EXISTS cf_zones (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES cf_accounts(id) ON DELETE CASCADE
);

-- 8. Cloudflare DNS 记录表
CREATE TABLE IF NOT EXISTS cf_dns_records (
    id TEXT PRIMARY KEY,
    zone_id TEXT NOT NULL,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    content TEXT NOT NULL,
    ttl INTEGER DEFAULT 1,
    proxied INTEGER DEFAULT 0,
    priority INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (zone_id) REFERENCES cf_zones(id) ON DELETE CASCADE
);

-- 9. OpenAI API 端点表
CREATE TABLE IF NOT EXISTS openai_endpoints (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    status TEXT DEFAULT 'unknown',
    models TEXT, -- JSON 格式存储模型数组
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used DATETIME,
    last_checked DATETIME
);

-- 10. OpenAI 健康检查历史表
CREATE TABLE IF NOT EXISTS openai_health_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id TEXT NOT NULL,
    status TEXT NOT NULL,
    response_time INTEGER,
    error_message TEXT,
    checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (endpoint_id) REFERENCES openai_endpoints(id) ON DELETE CASCADE
);

-- 11. 用户设置表
CREATE TABLE IF NOT EXISTS user_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- 单例模式，只允许一条记录
    custom_css TEXT,
    module_visibility TEXT, -- JSON 格式
    module_order TEXT, -- JSON 格式
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 12. 操作日志表（新增，用于审计）
CREATE TABLE IF NOT EXISTS operation_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_type TEXT NOT NULL, -- 'create', 'update', 'delete'
    table_name TEXT NOT NULL,
    record_id TEXT,
    details TEXT, -- JSON 格式存储详细信息
    ip_address TEXT,
    user_agent TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 13. 服务器账号表
CREATE TABLE IF NOT EXISTS server_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER DEFAULT 22,
    username TEXT NOT NULL,
    auth_type TEXT NOT NULL CHECK(auth_type IN ('password', 'key')), -- 认证方式：密码或密钥
    password TEXT, -- 加密存储的密码
    private_key TEXT, -- 加密存储的私钥
    passphrase TEXT, -- 加密存储的私钥密码
    status TEXT DEFAULT 'unknown' CHECK(status IN ('online', 'offline', 'unknown')), -- 服务器状态
    last_check_time DATETIME, -- 最后探测时间
    last_check_status TEXT, -- 最后探测状态
    response_time INTEGER, -- 响应时间（毫秒）
    tags TEXT, -- JSON 格式存储标签数组
    description TEXT, -- 服务器描述
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 14. 服务器监控日志表
CREATE TABLE IF NOT EXISTS server_monitor_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('success', 'failed')), -- 探测状态
    response_time INTEGER, -- 响应时间（毫秒）
    error_message TEXT, -- 错误信息
    checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
);

-- 15. 服务器监控配置表
CREATE TABLE IF NOT EXISTS server_monitor_config (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- 单例模式，只允许一条记录
    probe_interval INTEGER DEFAULT 60, -- 探测间隔（秒）
    probe_timeout INTEGER DEFAULT 10, -- 探测超时（秒）
    log_retention_days INTEGER DEFAULT 7, -- 日志保留天数
    max_connections INTEGER DEFAULT 10, -- 最大连接数
    session_timeout INTEGER DEFAULT 1800, -- 会话超时（秒，默认30分钟）
    auto_start INTEGER DEFAULT 1, -- 是否自动启动监控
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引以提升查询性能
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(is_active, expires_at);
CREATE INDEX IF NOT EXISTS idx_zeabur_accounts_status ON zeabur_accounts(status);
CREATE INDEX IF NOT EXISTS idx_zeabur_projects_account ON zeabur_projects(account_id);
CREATE INDEX IF NOT EXISTS idx_cf_zones_account ON cf_zones(account_id);
CREATE INDEX IF NOT EXISTS idx_cf_dns_records_zone ON cf_dns_records(zone_id);
CREATE INDEX IF NOT EXISTS idx_openai_endpoints_status ON openai_endpoints(status);
CREATE INDEX IF NOT EXISTS idx_openai_health_endpoint ON openai_health_history(endpoint_id, checked_at);
CREATE INDEX IF NOT EXISTS idx_operation_logs_table ON operation_logs(table_name, created_at);
CREATE INDEX IF NOT EXISTS idx_server_accounts_status ON server_accounts(status);
CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_server ON server_monitor_logs(server_id, checked_at);
CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_status ON server_monitor_logs(status, checked_at);

-- 插入默认用户设置
INSERT OR IGNORE INTO user_settings (id, custom_css, module_visibility, module_order)
VALUES (
    1,
    '',
    '{"zeabur":true,"dns":true,"openai":true,"server":true}',
    '["zeabur","dns","openai","server"]'
);

-- 插入默认服务器监控配置
INSERT OR IGNORE INTO server_monitor_config (
    id,
    probe_interval,
    probe_timeout,
    log_retention_days,
    max_connections,
    session_timeout,
    auto_start
) VALUES (
    1,
    60,
    10,
    7,
    10,
    1800,
    1
);
