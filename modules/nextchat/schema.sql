-- NextChat 模块数据库 Schema
-- 会话管理和消息存储

-- 会话表
CREATE TABLE IF NOT EXISTS nextchat_sessions (
    id TEXT PRIMARY KEY,
    topic TEXT DEFAULT '新对话',
    model TEXT DEFAULT 'gemini-2.5-flash',
    system_prompt TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 消息表
CREATE TABLE IF NOT EXISTS nextchat_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant', 'system')),
    content TEXT NOT NULL,
    model TEXT,
    token_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES nextchat_sessions(id) ON DELETE CASCADE
);

-- 索引优化
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON nextchat_messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON nextchat_messages(created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON nextchat_sessions(updated_at);
