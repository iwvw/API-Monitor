-- OpenList 璐﹀彿瀛樺偍
CREATE TABLE IF NOT EXISTS openlist_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    api_url TEXT NOT NULL,
    api_token TEXT NOT NULL,
    status TEXT DEFAULT 'unknown',
    version TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- OpenList 瀛樺偍鎸傝浇蹇収锛堝彲閫夛紝鐢ㄤ簬缂撳瓨鐘舵�侊級
CREATE TABLE IF NOT EXISTS openlist_storage_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id TEXT NOT NULL,
    storage_name TEXT NOT NULL,
    driver TEXT,
    mount_path TEXT,
    status TEXT,
    total_size INTEGER,
    used_size INTEGER,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES openlist_accounts(id) ON DELETE CASCADE
);

-- OpenList 模块全局设置
CREATE TABLE IF NOT EXISTS openlist_settings (
    key TEXT PRIMARY KEY,
    value TEXT
);

