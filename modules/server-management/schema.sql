-- 服务器管理模块数据库表结构
-- 使用 SQLite 数据库

-- 1. 服务器账号表
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

-- 2. 服务器监控日志表
CREATE TABLE IF NOT EXISTS server_monitor_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('success', 'failed')), -- 探测状态
    response_time INTEGER, -- 响应时间（毫秒）
    error_message TEXT, -- 错误信息
    checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
);

-- 3. 服务器监控配置表
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
CREATE INDEX IF NOT EXISTS idx_server_accounts_status ON server_accounts(status);
CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_server ON server_monitor_logs(server_id, checked_at);
CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_status ON server_monitor_logs(status, checked_at);

-- 插入默认监控配置
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
