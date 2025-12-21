/**
 * Koyeb 数据模型
 */

const dbService = require('../../src/db/database');

// 确保初始化数据库（会创建 Koyeb 相关表）
dbService.initialize();

const KoyebAccount = {
    /**
     * 获取所有账号
     */
    findAll() {
        const db = dbService.getDatabase();
        const stmt = db.prepare('SELECT * FROM koyeb_accounts ORDER BY created_at DESC');
        return stmt.all() || [];
    },

    /**
     * 根据 ID 获取账号
     */
    findById(id) {
        const db = dbService.getDatabase();
        const stmt = db.prepare('SELECT * FROM koyeb_accounts WHERE id = ?');
        return stmt.get(id);
    },

    /**
     * 根据名称获取账号
     */
    findByName(name) {
        const db = dbService.getDatabase();
        const stmt = db.prepare('SELECT * FROM koyeb_accounts WHERE name = ?');
        return stmt.get(name);
    },

    /**
     * 创建新账号
     */
    createAccount(data) {
        const db = dbService.getDatabase();
        const stmt = db.prepare(`
      INSERT INTO koyeb_accounts (name, token, email, balance, status, created_at)
      VALUES (?, ?, ?, ?, ?, datetime('now'))
    `);

        const result = stmt.run(
            data.name,
            data.token,
            data.email || '',
            data.balance || 0,
            data.status || 'unknown'
        );

        return {
            id: result.lastInsertRowid,
            ...data
        };
    },

    /**
     * 更新账号
     */
    updateAccount(id, data) {
        const db = dbService.getDatabase();
        const fields = [];
        const values = [];

        if (data.name !== undefined) {
            fields.push('name = ?');
            values.push(data.name);
        }
        if (data.token !== undefined) {
            fields.push('token = ?');
            values.push(data.token);
        }
        if (data.email !== undefined) {
            fields.push('email = ?');
            values.push(data.email);
        }
        if (data.balance !== undefined) {
            fields.push('balance = ?');
            values.push(data.balance);
        }
        if (data.status !== undefined) {
            fields.push('status = ?');
            values.push(data.status);
        }

        if (fields.length === 0) return null;

        values.push(id);
        const stmt = db.prepare(`UPDATE koyeb_accounts SET ${fields.join(', ')} WHERE id = ?`);
        stmt.run(...values);

        return this.findById(id);
    },

    /**
     * 删除账号
     */
    deleteAccount(id) {
        const db = dbService.getDatabase();
        const stmt = db.prepare('DELETE FROM koyeb_accounts WHERE id = ?');
        return stmt.run(id);
    },

    /**
     * 清空所有账号
     */
    truncate() {
        const db = dbService.getDatabase();
        const stmt = db.prepare('DELETE FROM koyeb_accounts');
        return stmt.run();
    }
};

module.exports = {
    KoyebAccount
};
