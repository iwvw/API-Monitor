/**
 * 服务器管理模块数据模型
 */

const dbService = require('../database');
const { v4: uuidv4 } = require('uuid');
const { encrypt, decrypt } = require('../../utils/encryption');

// 获取数据库实例
const getDb = () => dbService.getDatabase();

/**
 * ServerAccount 模型 - 服务器账号管理
 */
class ServerAccount {
    /**
     * 获取所有服务器账号
     * @returns {Array} 服务器账号列表
     */
    static getAll() {
        const stmt = getDb().prepare(`
            SELECT * FROM server_accounts
            ORDER BY created_at DESC
        `);
        const accounts = stmt.all();

        // 解密敏感信息
        return accounts.map(account => this.decryptSensitiveData(account));
    }

    /**
     * 根据 ID 获取服务器账号
     * @param {string} id - 服务器 ID
     * @returns {Object|null} 服务器账号对象
     */
    static getById(id) {
        const stmt = getDb().prepare('SELECT * FROM server_accounts WHERE id = ?');
        const account = stmt.get(id);

        if (!account) return null;

        return this.decryptSensitiveData(account);
    }

    /**
     * 创建服务器账号
     * @param {Object} data - 服务器账号数据
     * @returns {Object} 创建的服务器账号
     */
    static create(data) {
        const id = data.id || uuidv4();
        const now = new Date().toISOString();

        // 加密敏感信息
        const encryptedData = this.encryptSensitiveData(data);

        const stmt = getDb().prepare(`
            INSERT INTO server_accounts (
                id, name, host, port, username, auth_type,
                password, private_key, passphrase,
                status, tags, description, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `);

        stmt.run(
            id,
            data.name,
            data.host,
            data.port || 22,
            data.username,
            data.auth_type,
            encryptedData.password || null,
            encryptedData.private_key || null,
            encryptedData.passphrase || null,
            data.status || 'unknown',
            data.tags ? JSON.stringify(data.tags) : null,
            data.description || null,
            now,
            now
        );

        return this.getById(id);
    }

    /**
     * 更新服务器账号
     * @param {string} id - 服务器 ID
     * @param {Object} data - 更新的数据
     * @returns {Object|null} 更新后的服务器账号
     */
    static update(id, data) {
        // 首先从数据库获取原始记录(未解密)
        const stmt = getDb().prepare('SELECT * FROM server_accounts WHERE id = ?');
        const existingRaw = stmt.get(id);
        if (!existingRaw) return null;

        // 同时获取解密后的记录用于返回默认值
        const existing = this.decryptSensitiveData(existingRaw);
        const now = new Date().toISOString();

        // 加密敏感信息(如果提供了新值)
        const encryptedData = this.encryptSensitiveData(data);

        const updateStmt = getDb().prepare(`
            UPDATE server_accounts
            SET name = ?,
                host = ?,
                port = ?,
                username = ?,
                auth_type = ?,
                password = ?,
                private_key = ?,
                passphrase = ?,
                tags = ?,
                description = ?,
                updated_at = ?
            WHERE id = ?
        `);

        updateStmt.run(
            data.name !== undefined ? data.name : existing.name,
            data.host !== undefined ? data.host : existing.host,
            data.port !== undefined ? data.port : existing.port,
            data.username !== undefined ? data.username : existing.username,
            data.auth_type !== undefined ? data.auth_type : existing.auth_type,
            encryptedData.password !== undefined ? encryptedData.password : existingRaw.password,
            encryptedData.private_key !== undefined ? encryptedData.private_key : existingRaw.private_key,
            encryptedData.passphrase !== undefined ? encryptedData.passphrase : existingRaw.passphrase,
            data.tags !== undefined ? JSON.stringify(data.tags) : existingRaw.tags,
            data.description !== undefined ? data.description : existing.description,
            now,
            id
        );

        return this.getById(id);
    }

    /**
     * 更新服务器状态
     * @param {string} id - 服务器 ID
     * @param {Object} statusData - 状态数据
     * @returns {boolean} 是否更新成功
     */
    static updateStatus(id, statusData) {
        const now = new Date().toISOString();

        const stmt = getDb().prepare(`
            UPDATE server_accounts
            SET status = ?,
                last_check_time = ?,
                last_check_status = ?,
                response_time = ?,
                updated_at = ?
            WHERE id = ?
        `);

        const result = stmt.run(
            statusData.status,
            statusData.last_check_time || now,
            statusData.last_check_status,
            statusData.response_time || null,
            now,
            id
        );

        return result.changes > 0;
    }

    /**
     * 删除服务器账号
     * @param {string} id - 服务器 ID
     * @returns {boolean} 是否删除成功
     */
    static delete(id) {
        const stmt = getDb().prepare('DELETE FROM server_accounts WHERE id = ?');
        const result = stmt.run(id);
        return result.changes > 0;
    }

    /**
     * 批量删除服务器账号
     * @param {Array<string>} ids - 服务器 ID 数组
     * @returns {number} 删除的数量
     */
    static deleteMany(ids) {
        if (!ids || ids.length === 0) return 0;

        const placeholders = ids.map(() => '?').join(',');
        const stmt = getDb().prepare(`DELETE FROM server_accounts WHERE id IN (${placeholders})`);
        const result = stmt.run(...ids);
        return result.changes;
    }

    /**
     * 加密敏感数据
     * @param {Object} data - 原始数据
     * @returns {Object} 加密后的数据
     */
    static encryptSensitiveData(data) {
        const result = {};

        if (data.password) {
            result.password = encrypt(data.password);
        }
        if (data.private_key) {
            result.private_key = encrypt(data.private_key);
        }
        if (data.passphrase) {
            result.passphrase = encrypt(data.passphrase);
        }

        return result;
    }

    /**
     * 解密敏感数据
     * @param {Object} account - 数据库中的账号对象
     * @returns {Object} 解密后的账号对象
     */
    static decryptSensitiveData(account) {
        if (!account) return null;

        const decrypted = { ...account };

        try {
            if (account.password) {
                decrypted.password = decrypt(account.password);
            }
            if (account.private_key) {
                decrypted.private_key = decrypt(account.private_key);
            }
            if (account.passphrase) {
                decrypted.passphrase = decrypt(account.passphrase);
            }
            if (account.tags) {
                decrypted.tags = JSON.parse(account.tags);
            }
        } catch (error) {
            console.error('解密服务器账号数据失败:', error);
        }

        return decrypted;
    }

    /**
     * 获取在线服务器数量
     * @returns {number} 在线服务器数量
     */
    static getOnlineCount() {
        const stmt = getDb().prepare('SELECT COUNT(*) as count FROM server_accounts WHERE status = ?');
        const result = stmt.get('online');
        return result.count;
    }

    /**
     * 获取离线服务器数量
     * @returns {number} 离线服务器数量
     */
    static getOfflineCount() {
        const stmt = getDb().prepare('SELECT COUNT(*) as count FROM server_accounts WHERE status = ?');
        const result = stmt.get('offline');
        return result.count;
    }
}

/**
 * ServerMonitorLog 模型 - 服务器监控日志
 */
class ServerMonitorLog {
    /**
     * 创建监控日志
     * @param {Object} data - 日志数据
     * @returns {Object} 创建的日志
     */
    static create(data) {
        const now = new Date().toISOString();

        const stmt = getDb().prepare(`
            INSERT INTO server_monitor_logs (
                server_id, status, response_time, error_message, checked_at
            ) VALUES (?, ?, ?, ?, ?)
        `);

        const result = stmt.run(
            data.server_id,
            data.status,
            data.response_time || null,
            data.error_message || null,
            data.checked_at || now
        );

        return {
            id: result.lastInsertRowid,
            ...data,
            checked_at: data.checked_at || now
        };
    }

    /**
     * 获取服务器的监控日志
     * @param {string} serverId - 服务器 ID
     * @param {Object} options - 查询选项
     * @returns {Array} 监控日志列表
     */
    static getByServerId(serverId, options = {}) {
        const { limit = 100, offset = 0, status = null } = options;

        let sql = 'SELECT * FROM server_monitor_logs WHERE server_id = ?';
        const params = [serverId];

        if (status) {
            sql += ' AND status = ?';
            params.push(status);
        }

        sql += ' ORDER BY checked_at DESC LIMIT ? OFFSET ?';
        params.push(limit, offset);

        const stmt = getDb().prepare(sql);
        return stmt.all(...params);
    }

    /**
     * 获取所有监控日志
     * @param {Object} options - 查询选项
     * @returns {Array} 监控日志列表
     */
    static getAll(options = {}) {
        const { limit = 100, offset = 0, status = null, serverId = null } = options;

        let sql = 'SELECT * FROM server_monitor_logs WHERE 1=1';
        const params = [];

        if (serverId) {
            sql += ' AND server_id = ?';
            params.push(serverId);
        }

        if (status) {
            sql += ' AND status = ?';
            params.push(status);
        }

        sql += ' ORDER BY checked_at DESC LIMIT ? OFFSET ?';
        params.push(limit, offset);

        const stmt = getDb().prepare(sql);
        return stmt.all(...params);
    }

    /**
     * 删除过期日志
     * @param {number} days - 保留天数
     * @returns {number} 删除的日志数量
     */
    static deleteOldLogs(days) {
        const cutoffDate = new Date();
        cutoffDate.setDate(cutoffDate.getDate() - days);

        const stmt = getDb().prepare('DELETE FROM server_monitor_logs WHERE checked_at < ?');
        const result = stmt.run(cutoffDate.toISOString());
        return result.changes;
    }

    /**
     * 获取日志总数
     * @param {Object} filters - 过滤条件
     * @returns {number} 日志总数
     */
    static getCount(filters = {}) {
        let sql = 'SELECT COUNT(*) as count FROM server_monitor_logs WHERE 1=1';
        const params = [];

        if (filters.serverId) {
            sql += ' AND server_id = ?';
            params.push(filters.serverId);
        }

        if (filters.status) {
            sql += ' AND status = ?';
            params.push(filters.status);
        }

        const stmt = getDb().prepare(sql);
        const result = stmt.get(...params);
        return result.count;
    }
}

/**
 * ServerMonitorConfig 模型 - 服务器监控配置
 */
class ServerMonitorConfig {
    /**
     * 获取监控配置
     * @returns {Object} 监控配置
     */
    static get() {
        const stmt = getDb().prepare('SELECT * FROM server_monitor_config WHERE id = 1');
        return stmt.get();
    }

    /**
     * 更新监控配置
     * @param {Object} data - 配置数据
     * @returns {Object} 更新后的配置
     */
    static update(data) {
        const now = new Date().toISOString();

        const stmt = getDb().prepare(`
            UPDATE server_monitor_config
            SET probe_interval = ?,
                probe_timeout = ?,
                log_retention_days = ?,
                max_connections = ?,
                session_timeout = ?,
                auto_start = ?,
                updated_at = ?
            WHERE id = 1
        `);

        stmt.run(
            data.probe_interval,
            data.probe_timeout,
            data.log_retention_days,
            data.max_connections,
            data.session_timeout,
            data.auto_start !== undefined ? (data.auto_start ? 1 : 0) : 1,
            now
        );

        return this.get();
    }
}

module.exports = {
    ServerAccount,
    ServerMonitorLog,
    ServerMonitorConfig
};
