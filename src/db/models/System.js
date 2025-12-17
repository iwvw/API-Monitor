const BaseModel = require('./BaseModel');

/**
 * 系统配置模型
 */
class SystemConfig extends BaseModel {
    constructor() {
        super('system_config');
    }

    /**
     * 设置配置项
     */
    setConfig(key, value, description = null) {
        const existing = this.getConfig(key);

        if (existing) {
            return this.updateWhere(
                { key },
                { value, description, updated_at: new Date().toISOString() }
            );
        } else {
            return this.insert({
                key,
                value,
                description,
                updated_at: new Date().toISOString()
            });
        }
    }

    /**
     * 获取配置项
     */
    getConfig(key) {
        return this.findOneWhere({ key });
    }

    /**
     * 获取配置值
     */
    getConfigValue(key, defaultValue = null) {
        const config = this.getConfig(key);
        return config ? config.value : defaultValue;
    }

    /**
     * 删除配置项
     */
    deleteConfig(key) {
        return this.deleteWhere({ key });
    }

    /**
     * 获取所有配置
     */
    getAllConfigs() {
        return this.findAll('key ASC');
    }

    /**
     * 批量设置配置
     */
    batchSetConfigs(configs) {
        const db = this.getDb();

        const transaction = db.transaction(() => {
            Object.entries(configs).forEach(([key, value]) => {
                this.setConfig(key, value);
            });
        });

        transaction();
    }
}

/**
 * 会话管理模型
 */
class Session extends BaseModel {
    constructor() {
        super('sessions');
    }

    /**
     * 创建会话
     */
    createSession(sessionData) {
        const expiresAt = new Date();
        expiresAt.setHours(expiresAt.getHours() + 24); // 24小时后过期

        const data = {
            session_id: sessionData.session_id || this.generateSessionId(),
            password: sessionData.password,
            created_at: new Date().toISOString(),
            last_accessed_at: new Date().toISOString(),
            expires_at: sessionData.expires_at || expiresAt.toISOString(),
            is_active: 1
        };

        this.insert(data);
        return data;
    }

    /**
     * 生成会话 ID
     */
    generateSessionId() {
        return `sess_${Date.now()}_${Math.random().toString(36).substr(2, 16)}`;
    }

    /**
     * 获取会话
     */
    getSession(sessionId) {
        return this.findOneWhere({ session_id: sessionId });
    }

    /**
     * 验证会话
     */
    validateSession(sessionId) {
        const session = this.getSession(sessionId);

        if (!session) {
            return { valid: false, reason: 'session_not_found' };
        }

        if (!session.is_active) {
            return { valid: false, reason: 'session_inactive' };
        }

        const now = new Date();
        const expiresAt = new Date(session.expires_at);

        if (now > expiresAt) {
            // 会话已过期，标记为不活跃
            this.updateWhere({ session_id: sessionId }, { is_active: 0 });
            return { valid: false, reason: 'session_expired' };
        }

        // 更新最后访问时间
        this.updateLastAccessed(sessionId);

        return { valid: true, session };
    }

    /**
     * 更新最后访问时间
     */
    updateLastAccessed(sessionId) {
        return this.updateWhere({ session_id: sessionId }, {
            last_accessed_at: new Date().toISOString()
        });
    }

    /**
     * 使会话失效
     */
    invalidateSession(sessionId) {
        return this.updateWhere({ session_id: sessionId }, { is_active: 0 });
    }

    /**
     * 清理过期会话
     */
    cleanExpiredSessions() {
        const db = this.getDb();
        const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE expires_at < datetime('now')
                OR (is_active = 0 AND last_accessed_at < datetime('now', '-7 days'))
        `);
        return stmt.run().changes;
    }

    /**
     * 获取活跃会话数
     */
    getActiveSessionCount() {
        return this.count({ is_active: 1 });
    }

    /**
     * 获取所有活跃会话
     */
    getActiveSessions() {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE is_active = 1 AND expires_at > datetime('now')
            ORDER BY last_accessed_at DESC
        `);
        return stmt.all();
    }
}

/**
 * 用户设置模型
 */
class UserSettings extends BaseModel {
    constructor() {
        super('user_settings');
    }

    /**
     * 获取用户设置
     */
    getSettings() {
        let settings = this.findById(1);

        if (!settings) {
            // 如果不存在，创建默认设置
            settings = this.createDefaultSettings();
        }

        // 解析 JSON 字段
        if (settings.module_visibility) {
            settings.module_visibility = JSON.parse(settings.module_visibility);
        }
        if (settings.module_order) {
            settings.module_order = JSON.parse(settings.module_order);
        }

        return settings;
    }

    /**
     * 创建默认设置
     */
    createDefaultSettings() {
        const defaultSettings = {
            id: 1,
            custom_css: '',
            module_visibility: JSON.stringify({
                zeabur: true,
                dns: true,
                openai: true
            }),
            module_order: JSON.stringify(['zeabur', 'dns', 'openai']),
            updated_at: new Date().toISOString()
        };

        this.insert(defaultSettings);
        return defaultSettings;
    }

    /**
     * 更新用户设置
     */
    updateSettings(updates) {
        const data = { ...updates };

        // 处理 JSON 字段
        if (data.module_visibility && typeof data.module_visibility !== 'string') {
            data.module_visibility = JSON.stringify(data.module_visibility);
        }
        if (data.module_order && typeof data.module_order !== 'string') {
            data.module_order = JSON.stringify(data.module_order);
        }

        return this.update(1, data);
    }

    /**
     * 重置为默认设置
     */
    resetToDefault() {
        this.delete(1);
        return this.createDefaultSettings();
    }
}

/**
 * 操作日志模型
 */
class OperationLog extends BaseModel {
    constructor() {
        super('operation_logs');
    }

    /**
     * 记录操作
     */
    logOperation(logData) {
        const data = {
            operation_type: logData.operation_type,
            table_name: logData.table_name,
            record_id: logData.record_id || null,
            details: logData.details
                ? (typeof logData.details === 'string' ? logData.details : JSON.stringify(logData.details))
                : null,
            ip_address: logData.ip_address || null,
            user_agent: logData.user_agent || null,
            created_at: new Date().toISOString()
        };

        this.insert(data);
        return data;
    }

    /**
     * 获取操作日志
     */
    getLogs(tableName = null, limit = 100) {
        const db = this.getDb();

        if (tableName) {
            const stmt = db.prepare(`
                SELECT * FROM ${this.tableName}
                WHERE table_name = ?
                ORDER BY created_at DESC
                LIMIT ?
            `);
            return stmt.all(tableName, limit);
        }

        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            ORDER BY created_at DESC
            LIMIT ?
        `);
        return stmt.all(limit);
    }

    /**
     * 获取最近的操作
     */
    getRecentLogs(hours = 24, limit = 100) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE created_at >= datetime('now', '-${hours} hours')
            ORDER BY created_at DESC
            LIMIT ?
        `);
        return stmt.all(limit);
    }

    /**
     * 清理旧日志
     */
    cleanOldLogs(days = 30) {
        const db = this.getDb();
        const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE created_at < datetime('now', '-${days} days')
        `);
        return stmt.run().changes;
    }

    /**
     * 获取操作统计
     */
    getOperationStats(days = 7) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT
                operation_type,
                table_name,
                COUNT(*) as count
            FROM ${this.tableName}
            WHERE created_at >= datetime('now', '-${days} days')
            GROUP BY operation_type, table_name
            ORDER BY count DESC
        `);
        return stmt.all();
    }
}

module.exports = {
    SystemConfig: new SystemConfig(),
    Session: new Session(),
    UserSettings: new UserSettings(),
    OperationLog: new OperationLog()
};
