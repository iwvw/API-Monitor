-- Fly.io 账号表
CREATE TABLE IF NOT EXISTS fly_accounts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  api_token TEXT NOT NULL,
  email TEXT,
  organization_id TEXT, -- 默认组织ID
  created_at INTEGER,
  updated_at INTEGER
);
