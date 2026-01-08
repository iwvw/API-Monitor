/**
 * Tencent 数据库 Schema
 */

-- 腾讯云账号表
CREATE TABLE IF NOT EXISTS tencent_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    secret_id TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    region_id TEXT DEFAULT 'ap-guangzhou', -- 默认区域
    description TEXT,
    is_default INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 腾讯云 DNS 域名列表缓存
CREATE TABLE IF NOT EXISTS tencent_domains (
    domain_id TEXT PRIMARY KEY, -- 域名ID
    domain_name TEXT NOT NULL, -- 域名
    account_id INTEGER NOT NULL,
    status TEXT, -- 状态
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES tencent_accounts(id) ON DELETE CASCADE
);
