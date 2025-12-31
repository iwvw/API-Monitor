const dbService = require('../database');

/**
 * 基础模型类，提供通用的 CRUD 操作
 */
class BaseModel {
  constructor(tableName) {
    this.tableName = tableName;
  }

  /**
   * 获取数据库实例
   */
  getDb() {
    return dbService.getDatabase();
  }

  /**
   * 检查表是否有指定列
   */
  hasColumn(columnName) {
    const db = this.getDb();
    const stmt = db.prepare(`PRAGMA table_info(${this.tableName})`);
    const columns = stmt.all();
    return columns.some(col => col.name === columnName);
  }

  /**
   * 查询所有记录
   */
  findAll(orderBy = 'created_at DESC') {
    const db = this.getDb();
    const stmt = db.prepare(`SELECT * FROM ${this.tableName} ORDER BY ${orderBy}`);
    return stmt.all();
  }

  /**
   * 根据 ID 查询单条记录
   */
  findById(id) {
    const db = this.getDb();
    const stmt = db.prepare(`SELECT * FROM ${this.tableName} WHERE id = ?`);
    return stmt.get(id);
  }

  /**
   * 根据条件查询
   * @param {Object} conditions - 查询条件对象
   */
  findWhere(conditions) {
    const db = this.getDb();
    const keys = Object.keys(conditions);
    const whereClause = keys.map(key => `${key} = ?`).join(' AND ');
    const values = keys.map(key => conditions[key]);

    const stmt = db.prepare(`SELECT * FROM ${this.tableName} WHERE ${whereClause}`);
    return stmt.all(...values);
  }

  /**
   * 根据条件查询单条记录
   */
  findOneWhere(conditions) {
    const db = this.getDb();
    const keys = Object.keys(conditions);
    const whereClause = keys.map(key => `${key} = ?`).join(' AND ');
    const values = keys.map(key => conditions[key]);

    const stmt = db.prepare(`SELECT * FROM ${this.tableName} WHERE ${whereClause}`);
    return stmt.get(...values);
  }

  /**
   * 插入记录
   * @param {Object} data - 要插入的数据
   */
  insert(data) {
    const db = this.getDb();
    const keys = Object.keys(data);
    const placeholders = keys.map(() => '?').join(', ');
    const values = keys.map(key => data[key]);

    const stmt = db.prepare(`
            INSERT INTO ${this.tableName} (${keys.join(', ')})
            VALUES (${placeholders})
        `);

    const result = stmt.run(...values);
    return result.changes > 0;
  }

  /**
   * 更新记录
   * @param {string} id - 记录 ID
   * @param {Object} data - 要更新的数据
   */
  update(id, data) {
    const db = this.getDb();
    const keys = Object.keys(data);
    const setClause = keys.map(key => `${key} = ?`).join(', ');
    const values = [...keys.map(key => data[key]), id];

    // 检查表是否有 updated_at 字段
    const hasUpdatedAt = this.hasColumn('updated_at');
    const updateTimestamp = hasUpdatedAt ? ', updated_at = CURRENT_TIMESTAMP' : '';

    const stmt = db.prepare(`
            UPDATE ${this.tableName}
            SET ${setClause}${updateTimestamp}
            WHERE id = ?
        `);

    const result = stmt.run(...values);
    return result.changes > 0;
  }

  /**
   * 根据条件更新
   */
  updateWhere(conditions, data) {
    const db = this.getDb();
    const dataKeys = Object.keys(data);
    const condKeys = Object.keys(conditions);

    const setClause = dataKeys.map(key => `${key} = ?`).join(', ');
    const whereClause = condKeys.map(key => `${key} = ?`).join(' AND ');

    const values = [...dataKeys.map(key => data[key]), ...condKeys.map(key => conditions[key])];

    // 检查表是否有 updated_at 字段
    const hasUpdatedAt = this.hasColumn('updated_at');
    const updateTimestamp = hasUpdatedAt ? ', updated_at = CURRENT_TIMESTAMP' : '';

    const stmt = db.prepare(`
            UPDATE ${this.tableName}
            SET ${setClause}${updateTimestamp}
            WHERE ${whereClause}
        `);

    const result = stmt.run(...values);
    return result.changes;
  }

  /**
   * 删除记录
   * @param {string} id - 记录 ID
   */
  delete(id) {
    const db = this.getDb();
    const stmt = db.prepare(`DELETE FROM ${this.tableName} WHERE id = ?`);
    const result = stmt.run(id);
    return result.changes > 0;
  }

  /**
   * 根据条件删除
   */
  deleteWhere(conditions) {
    const db = this.getDb();
    const keys = Object.keys(conditions);
    const whereClause = keys.map(key => `${key} = ?`).join(' AND ');
    const values = keys.map(key => conditions[key]);

    const stmt = db.prepare(`DELETE FROM ${this.tableName} WHERE ${whereClause}`);
    const result = stmt.run(...values);
    return result.changes;
  }

  /**
   * 统计记录数
   */
  count(conditions = null) {
    const db = this.getDb();

    if (!conditions) {
      const stmt = db.prepare(`SELECT COUNT(*) as count FROM ${this.tableName}`);
      return stmt.get().count;
    }

    const keys = Object.keys(conditions);
    const whereClause = keys.map(key => `${key} = ?`).join(' AND ');
    const values = keys.map(key => conditions[key]);

    const stmt = db.prepare(`SELECT COUNT(*) as count FROM ${this.tableName} WHERE ${whereClause}`);
    return stmt.get(...values).count;
  }

  /**
   * 检查记录是否存在
   */
  exists(id) {
    return this.count({ id }) > 0;
  }

  /**
   * 批量插入
   */
  batchInsert(dataArray) {
    if (!dataArray || dataArray.length === 0) return 0;

    const db = this.getDb();
    const keys = Object.keys(dataArray[0]);
    const placeholders = keys.map(() => '?').join(', ');

    const stmt = db.prepare(`
            INSERT INTO ${this.tableName} (${keys.join(', ')})
            VALUES (${placeholders})
        `);

    const transaction = db.transaction(items => {
      for (const item of items) {
        const values = keys.map(key => item[key]);
        stmt.run(...values);
      }
    });

    transaction(dataArray);
    return dataArray.length;
  }

  /**
   * 清空表
   */
  truncate() {
    const db = this.getDb();
    const stmt = db.prepare(`DELETE FROM ${this.tableName}`);
    return stmt.run();
  }
}

module.exports = BaseModel;
