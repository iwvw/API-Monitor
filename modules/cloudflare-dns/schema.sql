-- Cloudflare DNS 模块数据库表结构

-- 5. Cloudflare 账号表
CREATE TABLE IF NOT EXISTS cf_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    api_token TEXT NOT NULL,
    email TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used DATETIME,
    is_active INTEGER DEFAULT 1
);

-- 6. Cloudflare DNS 模板表
CREATE TABLE IF NOT EXISTS cf_dns_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    records TEXT NOT NULL, -- JSON 格式存储 DNS 记录数组
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 7. Cloudflare 域名表
CREATE TABLE IF NOT EXISTS cf_zones (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES cf_accounts(id) ON DELETE CASCADE
);

-- 8. Cloudflare DNS 记录表
CREATE TABLE IF NOT EXISTS cf_dns_records (
    id TEXT PRIMARY KEY,
    zone_id TEXT NOT NULL,
    type TEXT NOT NULL,
    name TEXT NOT NULL,
    content TEXT NOT NULL,
    ttl INTEGER DEFAULT 1,
    proxied INTEGER DEFAULT 0,
    priority INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (zone_id) REFERENCES cf_zones(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_cf_zones_account ON cf_zones(account_id);
CREATE INDEX IF NOT EXISTS idx_cf_dns_records_zone ON cf_dns_records(zone_id);
