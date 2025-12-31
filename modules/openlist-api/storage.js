const db = require('../../src/db/database');
const { v4: uuidv4 } = require('uuid');
const fs = require('fs');
const path = require('path');

class OpenListStorage {
  constructor() {
    this.initDatabase();
  }

  initDatabase() {
    try {
      const schemaPath = path.join(__dirname, 'schema.sql');
      if (fs.existsSync(schemaPath)) {
        const schema = fs.readFileSync(schemaPath, 'utf8');
        db.getDatabase().exec(schema);
      }
    } catch (error) {
      console.error('Failed to initialize OpenList database:', error);
    }
  }

  getAllAccounts() {
    return db
      .getDatabase()
      .prepare('SELECT * FROM openlist_accounts ORDER BY created_at DESC')
      .all();
  }

  getAccountById(id) {
    return db.getDatabase().prepare('SELECT * FROM openlist_accounts WHERE id = ?').get(id);
  }

  addAccount(account) {
    const id = uuidv4();
    const stmt = db.getDatabase().prepare(`
            INSERT INTO openlist_accounts (id, name, api_url, api_token)
            VALUES (?, ?, ?, ?)
        `);
    stmt.run(id, account.name, account.api_url, account.api_token);
    return id;
  }

  updateAccount(id, account) {
    const stmt = db.getDatabase().prepare(`
            UPDATE openlist_accounts 
            SET name = ?, api_url = ?, api_token = ?, updated_at = CURRENT_TIMESTAMP
            WHERE id = ?
        `);
    return stmt.run(account.name, account.api_url, account.api_token, id);
  }

  updateStatus(id, status, version) {
    const stmt = db.getDatabase().prepare(`
            UPDATE openlist_accounts 
            SET status = ?, version = ?, updated_at = CURRENT_TIMESTAMP
            WHERE id = ?
        `);
    return stmt.run(status, version, id);
  }

  deleteAccount(id) {
    return db.getDatabase().prepare('DELETE FROM openlist_accounts WHERE id = ?').run(id);
  }

  // --- Settings ---
  getSetting(key) {
    const row = db
      .getDatabase()
      .prepare('SELECT value FROM openlist_settings WHERE key = ?')
      .get(key);
    return row ? row.value : null;
  }

  setSetting(key, value) {
    const stmt = db.getDatabase().prepare(`
            INSERT INTO openlist_settings (key, value) VALUES (?, ?)
            ON CONFLICT(key) DO UPDATE SET value = excluded.value
        `);
    return stmt.run(key, value);
  }
}

module.exports = new OpenListStorage();
