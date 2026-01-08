/**
 * 通知系统数据模型
 */

const BaseModel = require('../../src/db/models/BaseModel');
const crypto = require('crypto');

/**
 * 生成唯一ID
 */
function generateId() {
    return `notif_${Date.now()}_${crypto.randomBytes(8).toString('hex')}`;
}

/**
 * 通知渠道模型
 */
class NotificationChannelModel extends BaseModel {
    constructor() {
        super('notification_channels');
    }

    /**
     * 创建渠道
     */
    createChannel(channelData) {
        const data = {
            id: channelData.id || generateId(),
            name: channelData.name,
            type: channelData.type,
            enabled: channelData.enabled !== undefined ? channelData.enabled : 1,
            config: channelData.config,
        };
        this.insert(data);
        return data;
    }

    /**
     * 获取启用的渠道
     */
    getEnabledChannels() {
        return this.findWhere({ enabled: 1 });
    }

    /**
     * 根据类型获取渠道
     */
    getChannelsByType(type) {
        return this.findWhere({ type, enabled: 1 });
    }

    /**
     * 更新渠道
     */
    updateChannel(id, channelData) {
        const data = {};

        if (channelData.name !== undefined) data.name = channelData.name;
        if (channelData.enabled !== undefined) data.enabled = channelData.enabled;
        if (channelData.config !== undefined) data.config = channelData.config;

        return this.update(id, data);
    }
}

/**
 * 告警规则模型
 */
class AlertRuleModel extends BaseModel {
    constructor() {
        super('alert_rules');
    }

    /**
     * 创建规则
     */
    createRule(ruleData) {
        const data = {
            id: ruleData.id || generateId(),
            name: ruleData.name,
            source_module: ruleData.source_module,
            event_type: ruleData.event_type,
            severity: ruleData.severity || 'warning',
            enabled: ruleData.enabled !== undefined ? ruleData.enabled : 1,
            channels: JSON.stringify(ruleData.channels || []),
            conditions: JSON.stringify(ruleData.conditions || {}),
            suppression: JSON.stringify(ruleData.suppression || {}),
            time_window: JSON.stringify(ruleData.time_window || { enabled: false }),
            description: ruleData.description || '',
        };
        this.insert(data);
        return data;
    }

    /**
     * 根据来源模块获取启用的规则
     */
    getEnabledRulesBySource(sourceModule) {
        const rules = this.findWhere({ source_module: sourceModule, enabled: 1 });
        return rules.map(rule => this.parseRuleFields(rule));
    }

    /**
     * 根据来源和事件类型获取规则
     */
    getRulesBySourceAndEvent(sourceModule, eventType) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE source_module = ? AND event_type = ? AND enabled = 1
        `);
        const rules = stmt.all(sourceModule, eventType);
        return rules.map(rule => this.parseRuleFields(rule));
    }

    /**
     * 解析规则字段
     */
    parseRuleFields(rule) {
        return {
            ...rule,
            channels: JSON.parse(rule.channels || '[]'),
            conditions: JSON.parse(rule.conditions || '{}'),
            suppression: JSON.parse(rule.suppression || '{}'),
            time_window: JSON.parse(rule.time_window || '{"enabled":false}'),
        };
    }

    /**
     * 更新规则
     */
    updateRule(id, ruleData) {
        const data = {};

        if (ruleData.name !== undefined) data.name = ruleData.name;
        if (ruleData.severity !== undefined) data.severity = ruleData.severity;
        if (ruleData.enabled !== undefined) data.enabled = ruleData.enabled;
        if (ruleData.channels !== undefined) data.channels = JSON.stringify(ruleData.channels);
        if (ruleData.conditions !== undefined) data.conditions = JSON.stringify(ruleData.conditions);
        if (ruleData.suppression !== undefined) data.suppression = JSON.stringify(ruleData.suppression);
        if (ruleData.time_window !== undefined) data.time_window = JSON.stringify(ruleData.time_window);
        if (ruleData.description !== undefined) data.description = ruleData.description;

        return this.update(id, data);
    }
}

/**
 * 通知历史模型
 */
class NotificationHistoryModel extends BaseModel {
    constructor() {
        super('notification_history');
    }

    /**
     * 创建历史记录
     */
    createLog(logData) {
        const data = {
            rule_id: logData.rule_id,
            channel_id: logData.channel_id,
            status: logData.status || 'pending',
            title: logData.title,
            message: logData.message,
            data: JSON.stringify(logData.data || {}),
            error_message: logData.error_message || null,
            sent_at: logData.sent_at || null,
            retry_count: logData.retry_count || 0,
        };
        const result = this.insert(data);
        return { ...data, id: result.lastInsertRowid };
    }

    /**
     * 获取最近的历史记录
     */
    getRecentHistory(limit = 100) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            ORDER BY created_at DESC
            LIMIT ?
        `);
        return stmt.all(limit);
    }

    /**
     * 获取失败待重试的记录
     */
    getFailedLogs(maxRetry = 3) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE status IN ('failed', 'retrying')
            AND (retry_count IS NULL OR retry_count < ?)
            ORDER BY created_at ASC
        `);
        return stmt.all(maxRetry);
    }

    /**
     * 根据状态获取历史记录
     */
    getHistoryByStatus(status, limit = 100) {
        const db = this.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE status = ?
            ORDER BY created_at DESC
            LIMIT ?
        `);
        return stmt.all(status, limit);
    }

    /**
     * 更新历史记录状态
     */
    updateStatus(id, status, sentAt = null, errorMessage = null) {
        const data = {
            status,
            sent_at: sentAt,
            error_message: errorMessage,
        };

        if (status === 'retrying') {
            const log = this.findById(id);
            data.retry_count = (log.retry_count || 0) + 1;
        }

        return this.update(id, data);
    }

    /**
     * 清空旧历史记录
     */
    cleanOldHistory(retentionDays = 30) {
        const db = this.getDb();
        const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE created_at < datetime('now', '-' || ? || ' days')
        `);
        return stmt.run(retentionDays);
    }
}

/**
 * 告警状态追踪模型
 */
class AlertStateTrackingModel extends BaseModel {
    constructor() {
        super('alert_state_tracking');
    }

    /**
     * 更新或插入状态
     */
    upsertState(ruleId, fingerprint, updates = {}) {
        const db = this.getDb();

        // 查找现有记录
        const existing = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE rule_id = ? AND fingerprint = ?
        `).get(ruleId, fingerprint);

        if (existing) {
            // 更新现有记录
            const data = {
                last_triggered_at: Date.now(),
                consecutive_failures: (existing.consecutive_failures || 0) + 1,
                ...updates,
            };
            this.update(existing.id, data);
            return { ...existing, ...data };
        } else {
            // 插入新记录
            const data = {
                rule_id: ruleId,
                fingerprint: fingerprint,
                last_triggered_at: Date.now(),
                consecutive_failures: 1,
                ...updates,
            };
            const result = this.insert(data);
            return { ...data, id: result.lastInsertRowid };
        }
    }

    /**
     * 重置状态（恢复时调用）
     */
    resetState(ruleId, fingerprint) {
        const db = this.getDb();
        const existing = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE rule_id = ? AND fingerprint = ?
        `).get(ruleId, fingerprint);

        if (existing) {
            this.update(existing.id, {
                consecutive_failures: 0,
                last_notified_at: null,
            });
            return existing;
        }
        return null;
    }

    /**
     * 更新最后通知时间
     */
    updateLastNotified(ruleId, fingerprint) {
        const db = this.getDb();
        const existing = db.prepare(`
            SELECT * FROM ${this.tableName}
            WHERE rule_id = ? AND fingerprint = ?
        `).get(ruleId, fingerprint);

        if (existing) {
            this.update(existing.id, {
                last_notified_at: Date.now(),
            });
            return existing;
        }
        return null;
    }

    /**
     * 获取状态
     */
    getState(ruleId, fingerprint) {
        return this.findWhere({ rule_id: ruleId, fingerprint })[0] || null;
    }

    /**
     * 清理旧状态记录
     */
    cleanOldStates(beforeTimestamp) {
        const db = this.getDb();
        const stmt = db.prepare(`
            DELETE FROM ${this.tableName}
            WHERE last_triggered_at < ?
        `);
        return stmt.run(beforeTimestamp);
    }
}

/**
 * 全局配置模型
 */
class NotificationGlobalConfigModel extends BaseModel {
    constructor() {
        super('notification_global_config');
    }

    /**
     * 获取配置（单例）
     */
    getConfig() {
        const config = this.findById(1);
        if (!config) {
            // 返回默认配置
            return {
                id: 1,
                max_retry_times: 3,
                retry_interval_seconds: 60,
                history_retention_days: 30,
                enable_batch: 1,
                batch_interval_seconds: 30,
                default_channels: '[]',
            };
        }
        return config;
    }

    /**
     * 更新配置
     */
    updateConfig(configData) {
        const data = {};

        if (configData.max_retry_times !== undefined) data.max_retry_times = configData.max_retry_times;
        if (configData.retry_interval_seconds !== undefined) data.retry_interval_seconds = configData.retry_interval_seconds;
        if (configData.history_retention_days !== undefined) data.history_retention_days = configData.history_retention_days;
        if (configData.enable_batch !== undefined) data.enable_batch = configData.enable_batch;
        if (configData.batch_interval_seconds !== undefined) data.batch_interval_seconds = configData.batch_interval_seconds;
        if (configData.default_channels !== undefined) data.default_channels = JSON.stringify(configData.default_channels);

        return this.update(1, data);
    }

    /**
     * 获取默认配置
     */
    getDefaultConfig() {
        const config = this.getConfig();
        return {
            max_retry_times: config.max_retry_times || 3,
            retry_interval_seconds: config.retry_interval_seconds || 60,
            history_retention_days: config.history_retention_days || 30,
            enable_batch: config.enable_batch === 1,
            batch_interval_seconds: config.batch_interval_seconds || 30,
            default_channels: JSON.parse(config.default_channels || '[]'),
        };
    }
}

// 导出单例实例
module.exports = {
    NotificationChannel: new NotificationChannelModel(),
    AlertRule: new AlertRuleModel(),
    NotificationHistory: new NotificationHistoryModel(),
    AlertStateTracking: new AlertStateTrackingModel(),
    NotificationGlobalConfig: new NotificationGlobalConfigModel(),
    generateId,
};
