-- 模板模块数据库表结构
-- 将 {{module_name}} 替换为你的模块名称 (小写，下划线分隔)

-- 1. 账号/配置表
CREATE TABLE IF NOT EXISTS {{module_name}}_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    config JSON,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_synced_at DATETIME
);

-- 2. 数据明细表 (可选)
CREATE TABLE IF NOT EXISTS {{module_name}}_items (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT,
    status TEXT,
    data JSON,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES {{module_name}}_accounts(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_{{module_name}}_accounts_status ON {{module_name}}_accounts(status);
CREATE INDEX IF NOT EXISTS idx_{{module_name}}_items_account ON {{module_name}}_items(account_id);
