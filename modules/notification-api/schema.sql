-- ==================== 通知系统模块 ====================

-- 19. 通知渠道配置表
CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('email', 'telegram')),
    enabled INTEGER DEFAULT 1,
    config TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 20. 告警规则表
CREATE TABLE IF NOT EXISTS alert_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_module TEXT NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT DEFAULT 'warning' CHECK(severity IN ('critical', 'warning', 'info')),
    enabled INTEGER DEFAULT 1,
    channels TEXT NOT NULL,
    conditions TEXT,
    suppression TEXT,
    time_window TEXT,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 21. 通知历史表
CREATE TABLE IF NOT EXISTS notification_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'sent', 'failed', 'retrying')),
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    data TEXT,
    error_message TEXT,
    sent_at DATETIME,
    retry_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (rule_id) REFERENCES alert_rules(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES notification_channels(id) ON DELETE CASCADE
);

-- 22. 告警状态追踪表
CREATE TABLE IF NOT EXISTS alert_state_tracking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    last_triggered_at DATETIME NOT NULL,
    consecutive_failures INTEGER DEFAULT 1,
    last_notified_at DATETIME,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(rule_id, fingerprint),
    FOREIGN KEY (rule_id) REFERENCES alert_rules(id) ON DELETE CASCADE
);

-- 23. 全局通知配置表
CREATE TABLE IF NOT EXISTS notification_global_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    max_retry_times INTEGER DEFAULT 3,
    retry_interval_seconds INTEGER DEFAULT 60,
    history_retention_days INTEGER DEFAULT 30,
    enable_batch INTEGER DEFAULT 1,
    batch_interval_seconds INTEGER DEFAULT 30,
    default_channels TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 索引优化
CREATE INDEX IF NOT EXISTS idx_notification_channels_type ON notification_channels(type, enabled);
CREATE INDEX IF NOT EXISTS idx_alert_rules_source ON alert_rules(source_module, enabled);
CREATE INDEX IF NOT EXISTS idx_notification_history_rule ON notification_history(rule_id, created_at);
CREATE INDEX IF NOT EXISTS idx_notification_history_status ON notification_history(status, created_at);
CREATE INDEX IF NOT EXISTS idx_alert_state_tracking_rule ON alert_state_tracking(rule_id, fingerprint);
CREATE INDEX IF NOT EXISTS idx_alert_state_tracking_triggered ON alert_state_tracking(last_triggered_at);

-- 插入默认全局配置
INSERT OR IGNORE INTO notification_global_config (
    id, max_retry_times, retry_interval_seconds,
    history_retention_days, enable_batch, batch_interval_seconds
) VALUES (
    1, 3, 60, 30, 1, 30
);
