const dbService = require('../../src/db/database');
const { FlyAccount } = require('./models');
const { v4: uuidv4 } = require('uuid');

const storage = {
  // 获取所有账号
  async getAccounts() {
    const db = dbService.getDatabase();
    const stmt = db.prepare('SELECT * FROM fly_accounts ORDER BY created_at DESC');
    return stmt.all().map(row => new FlyAccount(row));
  },

  // 添加账号
  async addAccount(accountData) {
    const db = dbService.getDatabase();
    const id = uuidv4();
    const now = Date.now();
    const stmt = db.prepare(`
      INSERT INTO fly_accounts (id, name, api_token, email, organization_id, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?)
    `);

    stmt.run(
      id,
      accountData.name,
      accountData.api_token,
      accountData.email || null,
      accountData.organization_id || null,
      now,
      now
    );

    return this.getAccount(id);
  },

  // 获取单个账号
  async getAccount(id) {
    const db = dbService.getDatabase();
    const stmt = db.prepare('SELECT * FROM fly_accounts WHERE id = ?');
    const row = stmt.get(id);
    return row ? new FlyAccount(row) : null;
  },

  // 更新账号
  async updateAccount(id, data) {
    const db = dbService.getDatabase();
    const updates = [];
    const values = [];

    if (data.name) {
      updates.push('name = ?');
      values.push(data.name);
    }
    if (data.api_token) {
      updates.push('api_token = ?');
      values.push(data.api_token);
    }
    if (data.email !== undefined) {
      updates.push('email = ?');
      values.push(data.email);
    }
    if (data.organization_id !== undefined) {
      updates.push('organization_id = ?');
      values.push(data.organization_id);
    }

    updates.push('updated_at = ?');
    values.push(Date.now());
    values.push(id);

    const stmt = db.prepare(`UPDATE fly_accounts SET ${updates.join(', ')} WHERE id = ?`);
    stmt.run(...values);

    return this.getAccount(id);
  },

  // 删除账号
  async deleteAccount(id) {
    const db = dbService.getDatabase();
    const stmt = db.prepare('DELETE FROM fly_accounts WHERE id = ?');
    stmt.run(id);
    return true;
  },
};

module.exports = storage;
