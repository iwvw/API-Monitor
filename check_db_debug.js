const dbService = require('./src/db/database');
const db = dbService.getDatabase();
try {
    const tables = db.prepare("SELECT name FROM sqlite_master WHERE type='table'").all();
    console.log('Tables:', JSON.stringify(tables.map(t => t.name)));
    const accounts = db.prepare("SELECT * FROM ds_accounts").all();
    console.log('DeepSeek Accounts Count:', accounts.length);
} catch (e) {
    console.error('Error:', e.message);
}
process.exit(0);
