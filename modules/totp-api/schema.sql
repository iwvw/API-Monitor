-- TOTP 验证器模块数据库表结构
-- 用于存储 2FA 账号和分组信息

-- 分组表
CREATE TABLE IF NOT EXISTS totp_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,              -- 分组名称
    icon TEXT,                       -- 图标
    color TEXT,                      -- 主题色
    sort_order INTEGER DEFAULT 0,    -- 排序权重
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- TOTP/HOTP 账号表
CREATE TABLE IF NOT EXISTS totp_accounts (
    id TEXT PRIMARY KEY,
    otp_type TEXT DEFAULT 'totp',    -- OTP类型: totp / hotp
    issuer TEXT NOT NULL,            -- 发行商名称 (如 GitHub, Microsoft)
    account TEXT NOT NULL,           -- 账户名 (如邮箱)
    secret TEXT NOT NULL,            -- Base32 编码的密钥
    algorithm TEXT DEFAULT 'SHA1',   -- 哈希算法: SHA1/SHA256/SHA512
    digits INTEGER DEFAULT 6,        -- 验证码位数: 6 或 8
    period INTEGER DEFAULT 30,       -- TOTP 刷新周期(秒)
    counter INTEGER DEFAULT 0,       -- HOTP 计数器
    group_id TEXT,                   -- 所属分组
    icon TEXT,                       -- 图标: URL / Font Awesome 类名 / 品牌标识
    color TEXT,                      -- 品牌主题色 (如 #4285f4)
    sort_order INTEGER DEFAULT 0,    -- 排序权重
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (group_id) REFERENCES totp_groups(id) ON DELETE SET NULL
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_totp_sort ON totp_accounts(sort_order, created_at);
CREATE INDEX IF NOT EXISTS idx_totp_group ON totp_accounts(group_id);
CREATE INDEX IF NOT EXISTS idx_totp_groups_sort ON totp_groups(sort_order);
