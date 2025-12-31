/**
 * TOTP/HOTP 账号和分组数据模型
 */

const BaseModel = require('../../src/db/models/BaseModel');

/**
 * TOTP/HOTP 账号模型
 */
class TotpAccount extends BaseModel {
  constructor() {
    super('totp_accounts');
  }

  /**
   * 创建账号
   */
  createAccount(data) {
    const account = {
      id: data.id || `totp_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      otp_type: data.otp_type || 'totp',
      issuer: data.issuer || '未知',
      account: data.account || '',
      secret: data.secret,
      algorithm: data.algorithm || 'SHA1',
      digits: data.digits || 6,
      period: data.period || 30,
      counter: data.counter || 0,
      group_id: data.group_id || null,
      icon: data.icon || null,
      color: data.color || null,
      sort_order: data.sort_order || 0,
      created_at: data.created_at || new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    this.insert(account);
    return account;
  }

  /**
   * 更新账号
   */
  updateAccount(id, updates) {
    const allowedFields = [
      'otp_type',
      'issuer',
      'account',
      'secret',
      'algorithm',
      'digits',
      'period',
      'counter',
      'group_id',
      'icon',
      'color',
      'sort_order',
    ];
    const data = { updated_at: new Date().toISOString() };

    Object.keys(updates).forEach(key => {
      if (allowedFields.includes(key)) {
        data[key] = updates[key];
      }
    });

    return this.update(id, data);
  }

  /**
   * 获取所有账号（按排序）
   */
  getAllSorted() {
    const db = require('../../src/db/database').getDatabase();
    const stmt = db.prepare(
      `SELECT * FROM ${this.tableName} ORDER BY sort_order ASC, created_at ASC`
    );
    return stmt.all();
  }

  /**
   * 按分组获取账号
   */
  getByGroup(groupId) {
    const db = require('../../src/db/database').getDatabase();
    const stmt = db.prepare(
      `SELECT * FROM ${this.tableName} WHERE group_id = ? ORDER BY sort_order ASC`
    );
    return stmt.all(groupId);
  }

  /**
   * 批量更新排序
   */
  updateOrder(orderedIds) {
    const db = require('../../src/db/database').getDatabase();
    const stmt = db.prepare(
      `UPDATE ${this.tableName} SET sort_order = ?, updated_at = ? WHERE id = ?`
    );
    const now = new Date().toISOString();

    const transaction = db.transaction(() => {
      orderedIds.forEach((id, index) => {
        stmt.run(index, now, id);
      });
    });

    transaction();
  }
}

/**
 * TOTP 分组模型
 */
class TotpGroup extends BaseModel {
  constructor() {
    super('totp_groups');
  }

  /**
   * 创建分组
   */
  createGroup(data) {
    const group = {
      id: data.id || `group_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      name: data.name,
      icon: data.icon || null,
      color: data.color || null,
      sort_order: data.sort_order || 0,
      created_at: new Date().toISOString(),
    };

    this.insert(group);
    return group;
  }

  /**
   * 更新分组
   */
  updateGroup(id, updates) {
    const allowedFields = ['name', 'icon', 'color', 'sort_order'];
    const data = {};

    Object.keys(updates).forEach(key => {
      if (allowedFields.includes(key)) {
        data[key] = updates[key];
      }
    });

    return this.update(id, data);
  }

  /**
   * 获取所有分组（按排序）
   */
  getAllSorted() {
    const db = require('../../src/db/database').getDatabase();
    const stmt = db.prepare(
      `SELECT * FROM ${this.tableName} ORDER BY sort_order ASC, created_at ASC`
    );
    return stmt.all();
  }
}

module.exports = {
  TotpAccount: new TotpAccount(),
  TotpGroup: new TotpGroup(),
};
