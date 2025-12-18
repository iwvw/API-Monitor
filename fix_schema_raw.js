const Database = require('better-sqlite3');
const path = require('path');
const fs = require('fs');

const dbPath = path.join(__dirname, 'data/api-monitor.db');

console.log('Database path:', dbPath);

if (!fs.existsSync(dbPath)) {
    console.error('Database file not found!');
    process.exit(1);
}

const db = new Database(dbPath);

console.log('Checking user_settings table...');

try {
    const tableInfo = db.pragma('table_info(user_settings)');
    const hasColumn = tableInfo.some(col => col.name === 'zeabur_refresh_interval');

    if (!hasColumn) {
        console.log('Adding column zeabur_refresh_interval...');
        db.prepare('ALTER TABLE user_settings ADD COLUMN zeabur_refresh_interval INTEGER DEFAULT 30000').run();
        console.log('Column added successfully.');
    } else {
        console.log('Column already exists.');
    }
} catch (error) {
    console.error('Error:', error.message);
} finally {
    db.close();
}
