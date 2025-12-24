/**
 * 主机管理模块数据模型
 */

const dbService = require('../../src/db/database');
const { v4: uuidv4 } = require('uuid');
const { encrypt, decrypt } = require('../../src/utils/encryption');

// 获取数据库实例
const getDb = () => dbService.getDatabase();

/**
 * ServerAccount 模型 - 主机账号管理
 */
class ServerAccount {
    /**
     * 获取所有主机账号
     * @returns {Array} 主机账号列表
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
     * 根据 ID 获取主机账号
     * @param {string} id - 主机 ID
     * @returns {Object|null} 主机账号对象
     */
    static getById(id) {
        const stmt = getDb().prepare('SELECT * FROM server_accounts WHERE id = ?');
        const account = stmt.get(id);

        if (!account) return null;

        return this.decryptSensitiveData(account);
    }

    /**
     * 获取在线主机数量
     * @returns {number} 在线主机数
     */
    static getOnlineCount() {
        const stmt = getDb().prepare("SELECT COUNT(*) as count FROM server_accounts WHERE status = 'online'");
        return stmt.get().count;
    }

    /**
     * 获取离线主机数量
     * @returns {number} 离线主机数
     */
    static getOfflineCount() {
        const stmt = getDb().prepare("SELECT COUNT(*) as count FROM server_accounts WHERE status != 'online'");
        return stmt.get().count;
    }

    /**
     * 创建主机账号
     * @param {Object} data - 主机账号数据
     * @returns {Object} 创建的主机账号
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
     * 更新主机账号
     * @param {string} id - 主机 ID
     * @param {Object} data - 更新的数据
     * @returns {Object|null} 更新后的主机账号
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
                monitor_mode = ?,
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
            data.monitor_mode !== undefined ? data.monitor_mode : (existing.monitor_mode || 'agent'),
            now,
            id
        );

        return this.getById(id);
    }

    /**
 * 更新主机状态
 * @param {string} id - 主机 ID
 * @param {Object} statusData - 状态数据
 * @returns {boolean} 是否更新成功
 */
    static updateStatus(id, statusData) {
        const now = new Date().toISOString();

        // 如果有 cached_info，一并更新
        if (statusData.cached_info) {
            const stmt = getDb().prepare(`
            UPDATE server_accounts
            SET status = ?,
                last_check_time = ?,
                last_check_status = ?,
                response_time = ?,
                cached_info = ?,
                updated_at = ?
            WHERE id = ?
        `);

            const result = stmt.run(
                statusData.status,
                statusData.last_check_time || now,
                statusData.last_check_status,
                statusData.response_time || null,
                JSON.stringify(statusData.cached_info),
                now,
                id
            );

            return result.changes > 0;
        }

        // 无 cached_info 时保持原逻辑
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
     * 删除主机账号
     * @param {string} id - 主机 ID
     * @returns {boolean} 是否删除成功
     */
    static delete(id) {
        const stmt = getDb().prepare('DELETE FROM server_accounts WHERE id = ?');
        const result = stmt.run(id);
        return result.changes > 0;
    }

    /**
     * 批量删除主机账号
     * @param {Array<string>} ids - 主机 ID 数组
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

        const tryDecrypt = (val) => {
            if (!val) return val;
            // 简单检查是否符合 iv:authTag:data 格式
            if (typeof val === 'string' && val.split(':').length === 3) {
                try {
                    return decrypt(val);
                } catch (e) {
                    // console.warn('[SSH Models] 解密失败，可能已是明文或损坏');
                    return val;
                }
            }
            return val;
        };

        try {
            decrypted.password = tryDecrypt(account.password);
            decrypted.private_key = tryDecrypt(account.private_key);
            decrypted.passphrase = tryDecrypt(account.passphrase);

            if (account.tags) {
                decrypted.tags = JSON.parse(account.tags);
            }
            if (account.cached_info) {
                decrypted.cached_info = JSON.parse(account.cached_info);
            }
        } catch (error) {
            console.error('解析主机账号附加数据失败:', error);
        }

        return decrypted;
    }

    /**
     * 获取在线主机数量
     * @returns {number} 在线主机数量
     */
    static getOnlineCount() {
        const stmt = getDb().prepare('SELECT COUNT(*) as count FROM server_accounts WHERE status = ?');
        const result = stmt.get('online');
        return result.count;
    }

    /**
     * 获取离线主机数量
     * @returns {number} 离线主机数量
     */
    static getOfflineCount() {
        const stmt = getDb().prepare('SELECT COUNT(*) as count FROM server_accounts WHERE status = ?');
        const result = stmt.get('offline');
        return result.count;
    }
}

/**
 * ServerMonitorLog 模型 - 主机监控日志
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
     * 获取主机的监控日志
     * @param {string} serverId - 主机 ID
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
 * ServerMonitorConfig 模型 - 主机监控配置
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
                metrics_collect_interval = ?,
                metrics_retention_days = ?,
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
            data.metrics_collect_interval !== undefined ? data.metrics_collect_interval : 300,
            data.metrics_retention_days !== undefined ? data.metrics_retention_days : 30,
            now
        );

        return this.get();
    }
}

/**
 * ServerCredential 模型 - 主机凭据管理
 */
class ServerCredential {
    static getAll() {
        const stmt = getDb().prepare('SELECT * FROM server_credentials ORDER BY is_default DESC, created_at DESC');
        const credentials = stmt.all();
        return credentials.map(c => ({
            ...c,
            password: c.password ? decrypt(c.password) : null,
            is_default: Boolean(c.is_default)
        }));
    }

    static getDefault() {
        const stmt = getDb().prepare('SELECT * FROM server_credentials WHERE is_default = 1 LIMIT 1');
        const credential = stmt.get();
        if (!credential) return null;
        return {
            ...credential,
            password: credential.password ? decrypt(credential.password) : null,
            is_default: true
        };
    }

    static create(data) {
        const { name, username, password } = data;
        const stmt = getDb().prepare(`
            INSERT INTO server_credentials (name, username, password)
            VALUES (?, ?, ?)
        `);
        const result = stmt.run(name, username, password ? encrypt(password) : null);
        return { id: result.lastInsertRowid, ...data };
    }

    static setDefault(id) {
        try {
            const db = getDb();
            // 先取消所有默认凭据
            db.prepare('UPDATE server_credentials SET is_default = 0').run();
            // 设置指定凭据为默认
            const result = db.prepare('UPDATE server_credentials SET is_default = 1 WHERE id = ?').run(id);

            console.log(`[ServerCredential] Set default credential ID=${id}, changes=${result.changes}`);

            return result.changes > 0;
        } catch (error) {
            console.error(`[ServerCredential] Failed to set default credential ID=${id}:`, error);
            throw error;
        }
    }

    static delete(id) {
        const stmt = getDb().prepare('DELETE FROM server_credentials WHERE id = ?');
        return stmt.run(id).changes > 0;
    }
}

/**
 * ServerSnippet 模型 - 代码片段管理
 */
class ServerSnippet {
    static getAll() {
        const stmt = getDb().prepare('SELECT * FROM server_snippets ORDER BY category, title ASC');
        return stmt.all();
    }

    static create(data) {
        const now = new Date().toISOString();
        const stmt = getDb().prepare(`
            INSERT INTO server_snippets (title, content, category, description, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?)
        `);
        const result = stmt.run(
            data.title,
            data.content,
            data.category || 'common',
            data.description || null,
            now,
            now
        );
        return { id: result.lastInsertRowid, ...data, created_at: now, updated_at: now };
    }

    static update(id, data) {
        const now = new Date().toISOString();
        const stmt = getDb().prepare(`
            UPDATE server_snippets
            SET title = ?, content = ?, category = ?, description = ?, updated_at = ?
            WHERE id = ?
        `);
        const result = stmt.run(
            data.title,
            data.content,
            data.category,
            data.description,
            now,
            id
        );
        return result.changes > 0;
    }

    static delete(id) {
        const stmt = getDb().prepare('DELETE FROM server_snippets WHERE id = ?');
        return stmt.run(id).changes > 0;
    }
}

/**
 * ServerMetricsHistory 模型 - 实时指标历史记录
 */
class ServerMetricsHistory {
    /**
     * 创建历史记录
     * @param {Object} data - 指标数据
     * @returns {Object} 创建的记录
     */
    static create(data) {
        const stmt = getDb().prepare(`
            INSERT INTO server_metrics_history (
                server_id, cpu_usage, cpu_load, cpu_cores,
                mem_used, mem_total, mem_usage,
                disk_used, disk_total, disk_usage,
                docker_installed, docker_running, docker_stopped,
                recorded_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `);

        const now = new Date().toISOString();
        const result = stmt.run(
            data.server_id,
            data.cpu_usage || 0,
            data.cpu_load || '',
            data.cpu_cores || 0,
            data.mem_used || 0,
            data.mem_total || 0,
            data.mem_usage || 0,
            data.disk_used || '',
            data.disk_total || '',
            data.disk_usage || 0,
            data.docker_installed ? 1 : 0,
            data.docker_running || 0,
            data.docker_stopped || 0,
            now
        );

        return { id: result.lastInsertRowid, ...data };
    }

    /**
     * 批量创建历史记录 (用于多台主机同时采集)
     * @param {Array} records - 记录数组
     * @returns {number} 插入的记录数
     */
    static createMany(records) {
        if (!records || records.length === 0) return 0;

        const insert = getDb().prepare(`
            INSERT INTO server_metrics_history (
                server_id, cpu_usage, cpu_load, cpu_cores,
                mem_used, mem_total, mem_usage,
                disk_used, disk_total, disk_usage,
                docker_installed, docker_running, docker_stopped,
                recorded_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        `);

        const now = new Date().toISOString();

        const insertMany = getDb().transaction((rows) => {
            for (const data of rows) {
                insert.run(
                    data.server_id,
                    data.cpu_usage || 0,
                    data.cpu_load || '',
                    data.cpu_cores || 0,
                    data.mem_used || 0,
                    data.mem_total || 0,
                    data.mem_usage || 0,
                    data.disk_used || '',
                    data.disk_total || '',
                    data.disk_usage || 0,
                    data.docker_installed ? 1 : 0,
                    data.docker_running || 0,
                    data.docker_stopped || 0,
                    now
                );
            }
            return rows.length;
        });

        return insertMany(records);
    }

    /**
     * 获取指定时间范围的历史记录
     * @param {Object} options - 查询选项
     * @returns {Array} 历史记录列表
     */
    static getHistory(options = {}) {
        const {
            serverId = null,
            startTime = null,
            endTime = null,
            limit = 100,
            offset = 0
        } = options;

        let sql = 'SELECT h.*, a.name as server_name, a.host as server_host FROM server_metrics_history h LEFT JOIN server_accounts a ON h.server_id = a.id WHERE 1=1';
        const params = [];

        if (serverId) {
            sql += ' AND h.server_id = ?';
            params.push(serverId);
        }

        if (startTime) {
            sql += ' AND h.recorded_at >= ?';
            params.push(startTime);
        }

        if (endTime) {
            sql += ' AND h.recorded_at <= ?';
            params.push(endTime);
        }

        sql += ' ORDER BY h.recorded_at DESC LIMIT ? OFFSET ?';
        params.push(limit, offset);

        const stmt = getDb().prepare(sql);
        return stmt.all(...params);
    }

    /**
     * 获取记录总数
     * @param {Object} filters - 过滤条件
     * @returns {number} 记录总数
     */
    static getCount(filters = {}) {
        let sql = 'SELECT COUNT(*) as count FROM server_metrics_history WHERE 1=1';
        const params = [];

        if (filters.serverId) {
            sql += ' AND server_id = ?';
            params.push(filters.serverId);
        }

        if (filters.startTime) {
            sql += ' AND recorded_at >= ?';
            params.push(filters.startTime);
        }

        if (filters.endTime) {
            sql += ' AND recorded_at <= ?';
            params.push(filters.endTime);
        }

        const stmt = getDb().prepare(sql);
        const result = stmt.get(...params);
        return result.count;
    }

    /**
     * 删除过期记录
     * @param {number} days - 保留天数
     * @returns {number} 删除的记录数
     */
    static deleteOldRecords(days) {
        const cutoffDate = new Date();
        cutoffDate.setDate(cutoffDate.getDate() - days);

        const stmt = getDb().prepare('DELETE FROM server_metrics_history WHERE recorded_at < ?');
        const result = stmt.run(cutoffDate.toISOString());
        return result.changes;
    }

    /**
     * 获取指标统计数据 (用于图表展示)
     * @param {string} serverId - 主机 ID
     * @param {number} hours - 统计最近多少小时
     * @returns {Object} 统计数据
     */
    static getStats(serverId, hours = 24) {
        const cutoffDate = new Date();
        cutoffDate.setHours(cutoffDate.getHours() - hours);

        const stmt = getDb().prepare(`
            SELECT 
                AVG(cpu_usage) as avg_cpu,
                MAX(cpu_usage) as max_cpu,
                MIN(cpu_usage) as min_cpu,
                AVG(mem_usage) as avg_mem,
                MAX(mem_usage) as max_mem,
                MIN(mem_usage) as min_mem,
                AVG(disk_usage) as avg_disk,
                MAX(disk_usage) as max_disk,
                COUNT(*) as record_count
            FROM server_metrics_history 
            WHERE server_id = ? AND recorded_at >= ?
        `);

        return stmt.get(serverId, cutoffDate.toISOString());
    }

    /**
     * 清空记录
     * @param {string} serverId - 可选，指定主机 ID
     * @returns {number} 删除的记录数
     */
    static clear(serverId = null) {
        const db = getDb();
        if (serverId) {
            const stmt = db.prepare('DELETE FROM server_metrics_history WHERE server_id = ?');
            const result = stmt.run(serverId);
            return result.changes;
        } else {
            const stmt = db.prepare('DELETE FROM server_metrics_history');
            const result = stmt.run();
            // 重置自增 ID (可选，通常建议清空时重置)
            try {
                db.prepare("DELETE FROM sqlite_sequence WHERE name = 'server_metrics_history'").run();
            } catch (e) { }
            return result.changes;
        }
    }
}

module.exports = {
    ServerAccount,
    ServerMonitorLog,
    ServerMonitorConfig,
    ServerCredential,
    ServerSnippet,
    ServerMetricsHistory
};
