const dbService = require('./src/db/database');
dbService.initialize();
const db = dbService.getDatabase();
const info = db.prepare('PRAGMA table_info(gemini_cli_logs)').all();
console.log(JSON.stringify(info, null, 2));
