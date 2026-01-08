-- 主机管理模块数据库表结构

-- 13. 主机账号表
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
    status TEXT DEFAULT 'unknown' CHECK(status IN ('online', 'offline', 'unknown')), -- 主机状态
    monitor_mode TEXT DEFAULT 'agent' CHECK(monitor_mode IN ('agent')), -- 监控模式：纯 Agent
    last_check_time DATETIME, -- 最后探测时间
    last_check_status TEXT, -- 最后探测状态
    response_time INTEGER, -- 响应时间（毫秒）
    cached_info TEXT, -- JSON 格式缓存的详细信息（CPU/内存/磁盘）
    tags TEXT, -- JSON 格式存储标签数组
    description TEXT, -- 主机描述
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 14. 主机监控日志表
CREATE TABLE IF NOT EXISTS server_monitor_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('success', 'failed')), -- 探测状态
    response_time INTEGER, -- 响应时间（毫秒）
    error_message TEXT, -- 错误信息
    checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
);

-- 15. 主机监控配置表
CREATE TABLE IF NOT EXISTS server_monitor_config (
    id INTEGER PRIMARY KEY CHECK (id = 1), -- 单例模式，只允许一条记录
    probe_interval INTEGER DEFAULT 60, -- 探测间隔（秒）
    probe_timeout INTEGER DEFAULT 10, -- 探测超时（秒）
    log_retention_days INTEGER DEFAULT 7, -- 日志保留天数
    max_connections INTEGER DEFAULT 10, -- 最大连接数
    session_timeout INTEGER DEFAULT 1800, -- 会话超时（秒，默认30分钟）
    auto_start INTEGER DEFAULT 1, -- 是否自动启动监控
    metrics_collect_interval INTEGER DEFAULT 300, -- 历史指标采集间隔（秒，默认5分钟）
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 16. 主机凭据表
CREATE TABLE IF NOT EXISTS server_credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL, -- 凭据名称，如 "默认Root", "Web服务器通用"
    username TEXT NOT NULL,
    password TEXT, -- 加密存储的密码
    is_default INTEGER DEFAULT 0, -- 是否为默认凭据（0=否，1=是）
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_server_accounts_status ON server_accounts(status);
CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_server ON server_monitor_logs(server_id, checked_at);
CREATE INDEX IF NOT EXISTS idx_server_monitor_logs_status ON server_monitor_logs(status, checked_at);

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

-- 17. 主机代码片段表
CREATE TABLE IF NOT EXISTS server_snippets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL, -- 片段标题
    content TEXT NOT NULL, -- 指令内容
    category TEXT DEFAULT 'common', -- 分类
    description TEXT, -- 描述
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 18. 实时指标历史表 (定期采集快照)
CREATE TABLE IF NOT EXISTS server_metrics_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id TEXT NOT NULL,                     -- 主机 ID
    cpu_usage REAL,                              -- CPU 使用率 (%)
    cpu_load TEXT,                               -- 负载均值 (1/5/15)
    cpu_cores INTEGER,                           -- CPU 核心数
    mem_used INTEGER,                            -- 已用内存 (MB)
    mem_total INTEGER,                           -- 总内存 (MB)
    mem_usage REAL,                              -- 内存使用率 (%)
    disk_used TEXT,                              -- 磁盘已用
    disk_total TEXT,                             -- 磁盘总量
    disk_usage REAL,                             -- 磁盘使用率 (%)
    docker_installed INTEGER DEFAULT 0,          -- Docker 是否安装
    docker_running INTEGER DEFAULT 0,            -- Docker 运行容器数
    docker_stopped INTEGER DEFAULT 0,            -- Docker 停止容器数
    gpu_usage REAL DEFAULT 0,                    -- GPU 使用率 (%)
    gpu_mem_used INTEGER DEFAULT 0,              -- GPU 显存已用 (MB)
    gpu_mem_total INTEGER DEFAULT 0,             -- GPU 显存总量 (MB)
    gpu_power REAL DEFAULT 0,                    -- GPU 功耗 (W)
    platform TEXT,                               -- 平台信息
    recorded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE
);

-- 索引优化历史查询
CREATE INDEX IF NOT EXISTS idx_metrics_history_server_time ON server_metrics_history(server_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_history_time ON server_metrics_history(recorded_at DESC);

-- 迁移：monitor_mode 已简化为纯 Agent 模式，移除了 SSH 和双模式选项
-- 旧数据库可能仍包含 'ssh' 或 'both' 值，应用层会统一按 Agent 模式处理
-- ALTER TABLE server_accounts ADD COLUMN monitor_mode TEXT DEFAULT 'agent';

-- 迁移：添加 GPU 监控字段 (v2.0.2)
-- ALTER TABLE server_metrics_history ADD COLUMN gpu_usage REAL DEFAULT 0;
-- ALTER TABLE server_metrics_history ADD COLUMN gpu_mem_used INTEGER DEFAULT 0;
-- ALTER TABLE server_metrics_history ADD COLUMN gpu_mem_total INTEGER DEFAULT 0;
-- ALTER TABLE server_metrics_history ADD COLUMN gpu_power REAL DEFAULT 0;
-- ALTER TABLE server_metrics_history ADD COLUMN platform TEXT;
