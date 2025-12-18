-- {{MODULE_NAME}} 模块 - 数据库表结构
--
-- 使用说明：
-- 1. 将 {{table_name}} 替换为实际表名（如 my_feature_items）
-- 2. 根据需要添加或删除字段
-- 3. 将此文件内容添加到 src/db/schema.sql 中

-- 主数据表
CREATE TABLE IF NOT EXISTS {{table_name}} (
    id TEXT PRIMARY KEY,                    -- 唯一标识符
    name TEXT NOT NULL,                     -- 名称
    description TEXT DEFAULT '',            -- 描述（可选）
    status TEXT DEFAULT 'active',           -- 状态：active, inactive, pending
    data TEXT DEFAULT '{}',                 -- JSON 格式的额外数据
    created_at TEXT NOT NULL,               -- 创建时间 (ISO 8601)
    updated_at TEXT NOT NULL                -- 更新时间 (ISO 8601)
);

-- 名称索引（便于搜索）
CREATE INDEX IF NOT EXISTS idx_{{table_name}}_name ON {{table_name}}(name);

-- 状态索引（便于筛选）
CREATE INDEX IF NOT EXISTS idx_{{table_name}}_status ON {{table_name}}(status);

-- 创建时间索引（便于排序）
CREATE INDEX IF NOT EXISTS idx_{{table_name}}_created ON {{table_name}}(created_at);


-- ==================== 可选：关联表示例 ====================

-- 如果需要一对多关系，可以创建子表
-- CREATE TABLE IF NOT EXISTS {{table_name}}_items (
--     id TEXT PRIMARY KEY,
--     parent_id TEXT NOT NULL,
--     name TEXT NOT NULL,
--     value TEXT,
--     created_at TEXT NOT NULL,
--     FOREIGN KEY (parent_id) REFERENCES {{table_name}}(id) ON DELETE CASCADE
-- );
-- 
-- CREATE INDEX IF NOT EXISTS idx_{{table_name}}_items_parent 
--     ON {{table_name}}_items(parent_id);


-- ==================== 可选：配置表示例 ====================

-- 如果需要存储模块配置
-- CREATE TABLE IF NOT EXISTS {{table_name}}_config (
--     key TEXT PRIMARY KEY,
--     value TEXT NOT NULL,
--     updated_at TEXT NOT NULL
-- );
