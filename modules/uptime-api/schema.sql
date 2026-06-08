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
    keyword TEXT,
    dns_resolve_type TEXT DEFAULT 'A',
    dns_resolve_server TEXT,
    retry_interval INTEGER DEFAULT 30,
    resend_interval INTEGER DEFAULT 0,
    up_confirm_count INTEGER,
    down_confirm_count INTEGER,
    config_json TEXT,
    auth_json_encrypted TEXT,
    push_token TEXT UNIQUE,
    push_grace_seconds INTEGER DEFAULT 120,
    last_checked_at DATETIME,
    next_check_at DATETIME,
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
    state TEXT,
    ping INTEGER DEFAULT 0,
    duration_ms INTEGER,
    status_code INTEGER,
    error_code TEXT,
    details_json TEXT,
    maintenance INTEGER DEFAULT 0,
    probe_id TEXT,
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
    status TEXT DEFAULT 'open',
    severity TEXT,
    acknowledged_at DATETIME,
    acknowledged_by TEXT,
    maintenance_id INTEGER,
    resolved_reason TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS uptime_monitor_states (
    monitor_id INTEGER PRIMARY KEY,
    state TEXT DEFAULT 'up',
    fail_count INTEGER DEFAULT 0,
    recover_count INTEGER DEFAULT 0,
    active_incident_id INTEGER,
    last_transition_at DATETIME,
    last_error TEXT,
    last_ping INTEGER DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS uptime_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    order_index INTEGER DEFAULT 0,
    color TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS uptime_daily_stats (
    monitor_id INTEGER NOT NULL,
    date TEXT NOT NULL,
    uptime REAL,
    avg_latency REAL,
    p95_latency REAL,
    p99_latency REAL,
    up_count INTEGER DEFAULT 0,
    down_count INTEGER DEFAULT 0,
    maintenance_count INTEGER DEFAULT 0,
    incident_duration_ms INTEGER DEFAULT 0,
    PRIMARY KEY (monitor_id, date)
);

CREATE TABLE IF NOT EXISTS uptime_status_pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT UNIQUE NOT NULL,
    domain TEXT,
    title TEXT NOT NULL,
    description TEXT,
    theme TEXT DEFAULT 'auto',
    public INTEGER DEFAULT 1,
    cache_seconds INTEGER DEFAULT 300,
    config_json TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS uptime_status_page_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status_page_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    order_index INTEGER DEFAULT 0,
    FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS uptime_status_page_monitors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status_page_id INTEGER NOT NULL,
    group_id INTEGER,
    monitor_id INTEGER NOT NULL,
    order_index INTEGER DEFAULT 0,
    display_name TEXT,
    FOREIGN KEY (status_page_id) REFERENCES uptime_status_pages(id) ON DELETE CASCADE,
    FOREIGN KEY (monitor_id) REFERENCES uptime_monitors(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS uptime_maintenance_windows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    description TEXT,
    strategy TEXT DEFAULT 'manual',
    timezone TEXT DEFAULT 'UTC',
    start_at DATETIME,
    end_at DATETIME,
    cron TEXT,
    recurrence_json TEXT,
    active INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS uptime_maintenance_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    maintenance_id INTEGER NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT,
    FOREIGN KEY (maintenance_id) REFERENCES uptime_maintenance_windows(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_heartbeats_monitor_time ON uptime_heartbeats(monitor_id, created_at);
CREATE INDEX IF NOT EXISTS idx_heartbeats_created ON uptime_heartbeats(created_at);
CREATE INDEX IF NOT EXISTS idx_incidents_monitor ON uptime_incidents(monitor_id, started_at);
CREATE INDEX IF NOT EXISTS idx_uptime_states_state ON uptime_monitor_states(state, updated_at);
CREATE INDEX IF NOT EXISTS idx_uptime_status_pages_slug ON uptime_status_pages(slug, public);
CREATE INDEX IF NOT EXISTS idx_uptime_maintenance_active ON uptime_maintenance_windows(active, start_at, end_at);
