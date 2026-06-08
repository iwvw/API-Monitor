-- Filebox metadata 持久化表

CREATE TABLE IF NOT EXISTS filebox_entries (
    code TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    content TEXT,
    original_name TEXT,
    filename TEXT NOT NULL,
    path TEXT,
    mimetype TEXT,
    size INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL,
    expiry INTEGER NOT NULL,
    burn_after_reading INTEGER DEFAULT 0,
    downloads INTEGER DEFAULT 0,
    max_downloads INTEGER DEFAULT 0,
    access_password_hash TEXT,
    metadata_json TEXT,
    deleted_at INTEGER
);

CREATE TABLE IF NOT EXISTS filebox_access_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL,
    action TEXT NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS filebox_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    max_file_size INTEGER NOT NULL DEFAULT 104857600,
    allowed_mime_types TEXT NOT NULL DEFAULT '[]',
    default_expiry_hours INTEGER NOT NULL DEFAULT 24,
    public_upload_enabled INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_filebox_entries_expiry ON filebox_entries(expiry);
CREATE INDEX IF NOT EXISTS idx_filebox_entries_created ON filebox_entries(created_at);
CREATE INDEX IF NOT EXISTS idx_filebox_access_code ON filebox_access_logs(code, created_at);
