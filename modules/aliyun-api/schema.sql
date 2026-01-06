/**
 * Aliyun 数据库 Schema
 * 
 * 包含：
 * 1. aliyun_accounts - 存储 AK/SK (简单加密存储或明文，视项目安全策略)
 */

-- 阿里云账号表
CREATE TABLE IF NOT EXISTS aliyun_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    access_key_secret TEXT NOT NULL,
    region_id TEXT DEFAULT 'cn-hangzhou', -- 默认区域
    description TEXT,
    is_default INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 阿里云 DNS 域名列表缓存 (简单缓存，主要靠实时查询)
CREATE TABLE IF NOT EXISTS aliyun_domains (
    instance_id TEXT PRIMARY KEY, -- 域名实例ID
    domain_name TEXT NOT NULL, -- 域名
    account_id INTEGER NOT NULL,
    version_name TEXT, -- 版本名称 (免费版/个人版等)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES aliyun_accounts(id) ON DELETE CASCADE
);
