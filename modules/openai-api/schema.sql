-- OpenAI API 模块数据库表结构

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

-- 索引
CREATE INDEX IF NOT EXISTS idx_openai_endpoints_status ON openai_endpoints(status);
CREATE INDEX IF NOT EXISTS idx_openai_health_endpoint ON openai_health_history(endpoint_id, checked_at);
