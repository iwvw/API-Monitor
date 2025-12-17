const Database = require('better-sqlite3');
const db = new Database('data/api-monitor.db');

const row = db.prepare('SELECT value FROM system_config WHERE key = ?').get('admin_password');
console.log('管理员密码:', row ? row.value : '未设置');

db.close();
