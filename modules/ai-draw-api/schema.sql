-- AI Draw 模块数据表
-- 用于存储绘图项目和协作会话

-- 项目表
CREATE TABLE IF NOT EXISTS ai_draw_projects (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT 'Untitled',
    engine_type TEXT NOT NULL CHECK(engine_type IN ('mermaid', 'excalidraw', 'drawio')) DEFAULT 'drawio',
    content TEXT,           -- 图表内容 (JSON/XML/DSL)
    thumbnail TEXT,         -- Base64 缩略图
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 项目聊天历史表
CREATE TABLE IF NOT EXISTS ai_draw_chat_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
    content TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES ai_draw_projects(id) ON DELETE CASCADE
);

-- 索引优化
CREATE INDEX IF NOT EXISTS idx_ai_draw_projects_updated ON ai_draw_projects(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_draw_chat_project ON ai_draw_chat_history(project_id);
