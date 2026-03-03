-- ==================== Uptime 监控模块 ====================

-- 监控项表
CREATE TABLE IF NOT EXISTS uptime_monitors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'http',
    url TEXT,
    hostname TEXT,
    port INTEGER,
    interval INTEGER DEFAULT 60,
    timeout INTEGER DEFAULT 30,
    confirm_count INTEGER DEFAULT 3,
    active INTEGER DEFAULT 1,
    method TEXT DEFAULT 'GET',
    headers TEXT,
    body TEXT,
    ignore_tls INTEGER DEFAULT 0,
    accepted_status_codes TEXT,
    expiry_notification INTEGER DEFAULT 7,
    notification_channels TEXT DEFAULT '[]',
    tags TEXT DEFAULT '[]',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 心跳记录表
CREATE TABLE IF NOT EXISTS uptime_heartbeats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id INTEGER NOT NULL,
    status INTEGER NOT NULL,
    ping INTEGER DEFAULT 0,
    msg TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

-- 事件（Incident）表
CREATE TABLE IF NOT EXISTS uptime_incidents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id INTEGER NOT NULL,
    started_at DATETIME NOT NULL,
    resolved_at DATETIME,
    duration_ms INTEGER,
    cause TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_heartbeats_monitor_time ON uptime_heartbeats(monitor_id, created_at);
CREATE INDEX IF NOT EXISTS idx_heartbeats_created ON uptime_heartbeats(created_at);
CREATE INDEX IF NOT EXISTS idx_incidents_monitor ON uptime_incidents(monitor_id, started_at);
