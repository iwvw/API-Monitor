CREATE TABLE IF NOT EXISTS cron_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    schedule TEXT NOT NULL, -- Cron expression
    command TEXT NOT NULL, -- Shell command or URL
    type TEXT DEFAULT 'shell', -- 'shell' or 'http'
    enabled INTEGER DEFAULT 1, -- 1: enabled, 0: disabled
    last_run INTEGER,
    next_run INTEGER,
    created_at INTEGER DEFAULT (strftime('%s', 'now'))
);

CREATE TABLE IF NOT EXISTS cron_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER,
    status TEXT, -- 'success', 'failed', 'running'
    output TEXT,
    start_time INTEGER,
    end_time INTEGER,
    duration INTEGER,
    FOREIGN KEY(task_id) REFERENCES cron_tasks(id) ON DELETE CASCADE
);
