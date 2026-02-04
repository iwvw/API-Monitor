-- AI Draw 模块数据表
-- 用于存储绘图项目、Provider 配置和聊天历史

-- ==================== Provider 配置表 ====================
-- 支持两种来源：internal (复用 ai-chat-api) 和 external (独立配置)
CREATE TABLE IF NOT EXISTS ai_draw_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_type TEXT NOT NULL DEFAULT 'external' CHECK(source_type IN ('internal', 'external')),
    -- 外部来源配置
    base_url TEXT,
    api_key TEXT,
    default_model TEXT,
    -- 内部来源配置 (引用 ai-chat-api 的 provider)
    internal_provider_id TEXT,
    -- 通用配置
    enabled INTEGER DEFAULT 1,
    is_default INTEGER DEFAULT 0,
    sort_order INTEGER DEFAULT 0,
    created_at TEXT DEFAULT (datetime('now', 'localtime')),
    updated_at TEXT DEFAULT (datetime('now', 'localtime'))
);

-- ==================== 项目表 ====================
CREATE TABLE IF NOT EXISTS ai_draw_projects (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT 'Untitled',
    engine_type TEXT NOT NULL CHECK(engine_type IN ('mermaid', 'excalidraw', 'drawio')) DEFAULT 'drawio',
    content TEXT,           -- 图表内容 (JSON/XML/DSL)
    thumbnail TEXT,         -- Base64 缩略图
    provider_id TEXT,       -- 关联的 Provider
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (provider_id) REFERENCES ai_draw_providers(id) ON DELETE SET NULL
);

-- ==================== 聊天历史表 ====================
CREATE TABLE IF NOT EXISTS ai_draw_chat_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
    content TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES ai_draw_projects(id) ON DELETE CASCADE
);

-- ==================== 索引优化 ====================
CREATE INDEX IF NOT EXISTS idx_ai_draw_providers_default ON ai_draw_providers(is_default DESC, sort_order);
CREATE INDEX IF NOT EXISTS idx_ai_draw_projects_updated ON ai_draw_projects(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_draw_chat_project ON ai_draw_chat_history(project_id);
