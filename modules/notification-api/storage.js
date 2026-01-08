/**
 * 通知系统数据访问层
 */

const {
    NotificationChannel,
    AlertRule,
    NotificationHistory,
    AlertStateTracking,
    NotificationGlobalConfig,
} = require('./models');

/**
 * 渠道存储操作
 */
const channelStorage = {
    /**
     * 获取所有渠道
     */
    getAll() {
        return NotificationChannel.findAll();
    },

    /**
     * 获取启用的渠道
     */
    getEnabled() {
        return NotificationChannel.getEnabledChannels();
    },

    /**
     * 根据ID获取渠道
     */
    getById(id) {
        return NotificationChannel.findById(id);
    },

    /**
     * 根据类型获取渠道
     */
    getByType(type) {
        return NotificationChannel.getChannelsByType(type);
    },

    /**
     * 创建渠道
     */
    create(channelData) {
        return NotificationChannel.createChannel(channelData);
    },

    /**
     * 更新渠道
     */
    update(id, channelData) {
        return NotificationChannel.updateChannel(id, channelData);
    },

    /**
     * 删除渠道
     */
    delete(id) {
        return NotificationChannel.delete(id);
    },

    /**
     * 启用/禁用渠道
     */
    setEnabled(id, enabled) {
        return NotificationChannel.update(id, { enabled: enabled ? 1 : 0 });
    },
};

/**
 * 规则存储操作
 */
const ruleStorage = {
    /**
     * 获取所有规则
     */
    getAll() {
        const rules = AlertRule.findAll();
        return rules.map(rule => AlertRule.parseRuleFields(rule));
    },

    /**
     * 根据来源模块获取启用的规则
     */
    getEnabledBySource(sourceModule) {
        return AlertRule.getEnabledRulesBySource(sourceModule);
    },

    /**
     * 根据来源和事件类型获取规则
     */
    getBySourceAndEvent(sourceModule, eventType) {
        return AlertRule.getRulesBySourceAndEvent(sourceModule, eventType);
    },

    /**
     * 根据ID获取规则
     */
    getById(id) {
        const rule = AlertRule.findById(id);
        return rule ? AlertRule.parseRuleFields(rule) : null;
    },

    /**
     * 创建规则
     */
    create(ruleData) {
        return AlertRule.createRule(ruleData);
    },

    /**
     * 更新规则
     */
    update(id, ruleData) {
        return AlertRule.updateRule(id, ruleData);
    },

    /**
     * 删除规则
     */
    delete(id) {
        return AlertRule.delete(id);
    },

    /**
     * 启用规则
     */
    enable(id) {
        return AlertRule.update(id, { enabled: 1 });
    },

    /**
     * 禁用规则
     */
    disable(id) {
        return AlertRule.update(id, { enabled: 0 });
    },
};

/**
 * 历史记录存储操作
 */
const historyStorage = {
    /**
     * 获取最近的历史记录
     */
    getRecent(limit = 100) {
        return NotificationHistory.getRecentHistory(limit);
    },

    /**
     * 根据状态获取历史记录
     */
    getByStatus(status, limit = 100) {
        return NotificationHistory.getHistoryByStatus(status, limit);
    },

    /**
     * 根据规则ID获取历史记录
     */
    getByRuleId(ruleId, limit = 50) {
        const db = NotificationHistory.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${NotificationHistory.tableName}
            WHERE rule_id = ?
            ORDER BY created_at DESC
            LIMIT ?
        `);
        return stmt.all(ruleId, limit);
    },

    /**
     * 根据渠道ID获取历史记录
     */
    getByChannelId(channelId, limit = 50) {
        const db = NotificationHistory.getDb();
        const stmt = db.prepare(`
            SELECT * FROM ${NotificationHistory.tableName}
            WHERE channel_id = ?
            ORDER BY created_at DESC
            LIMIT ?
        `);
        return stmt.all(channelId, limit);
    },

    /**
     * 创建历史记录
     */
    create(logData) {
        return NotificationHistory.createLog(logData);
    },

    /**
     * 更新状态
     */
    updateStatus(id, status, sentAt = null, errorMessage = null) {
        return NotificationHistory.updateStatus(id, status, sentAt, errorMessage);
    },

    /**
     * 获取失败待重试的记录
     */
    getFailed(maxRetry = 3) {
        return NotificationHistory.getFailedLogs(maxRetry);
    },

    /**
     * 清空历史记录
     */
    clear() {
        return NotificationHistory.truncate();
    },

    /**
     * 清理旧历史记录
     */
    cleanOld(retentionDays = 30) {
        return NotificationHistory.cleanOldHistory(retentionDays);
    },
};

/**
 * 状态追踪存储操作
 */
const stateTrackingStorage = {
    /**
     * 更新或插入状态
     */
    upsert(ruleId, fingerprint, updates = {}) {
        return AlertStateTracking.upsertState(ruleId, fingerprint, updates);
    },

    /**
     * 重置状态
     */
    reset(ruleId, fingerprint) {
        return AlertStateTracking.resetState(ruleId, fingerprint);
    },

    /**
     * 获取状态
     */
    get(ruleId, fingerprint) {
        return AlertStateTracking.getState(ruleId, fingerprint);
    },

    /**
     * 更新最后通知时间
     */
    updateLastNotified(ruleId, fingerprint) {
        return AlertStateTracking.updateLastNotified(ruleId, fingerprint);
    },

    /**
     * 清理旧状态记录
     */
    cleanOld(beforeTimestamp) {
        return AlertStateTracking.cleanOldStates(beforeTimestamp);
    },
};

/**
 * 全局配置存储操作
 */
const globalConfigStorage = {
    /**
     * 获取配置
     */
    get() {
        return NotificationGlobalConfig.getConfig();
    },

    /**
     * 获取默认配置
     */
    getDefault() {
        return NotificationGlobalConfig.getDefaultConfig();
    },

    /**
     * 更新配置
     */
    update(configData) {
        return NotificationGlobalConfig.updateConfig(configData);
    },
};

module.exports = {
    channel: channelStorage,
    rule: ruleStorage,
    history: historyStorage,
    stateTracking: stateTrackingStorage,
    globalConfig: globalConfigStorage,
};
