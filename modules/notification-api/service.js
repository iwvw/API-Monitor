/**
 * 通知服务核心引擎
 */

const EventEmitter = require('events');
const { createLogger } = require('../../src/utils/logger');
const { encrypt, decrypt } = require('../../src/utils/encryption');
const storage = require('./storage');

const emailChannel = require('./channels/email');
const telegramChannel = require('./channels/telegram');

const logger = createLogger('NotificationService');

class NotificationService extends EventEmitter {
    constructor() {
        super();
        this.initialized = false;
        this.queue = [];
        this.processing = false;
        this.retryTimer = null;
    }

    /**
     * 初始化服务
     */
    init(server) {
        if (this.initialized) {
            logger.warn('通知服务已经初始化');
            return;
        }

        logger.info('正在初始化通知服务...');

        // 加载所有启用的渠道
        this.loadChannels();

        // 启动队列处理器
        this.startQueueProcessor();

        // 启动失败重试处理器
        this.startRetryProcessor();

        // 启动定时清理任务
        this.startCleanupTasks();

        this.initialized = true;
        logger.info('✅ 通知服务已初始化');
    }

    /**
     * 触发告警 (供其他模块调用)
     * @param {string} sourceModule - 来源模块 (uptime/server/zeabur/openai)
     * @param {string} eventType - 事件类型 (down/up/offline/cpu_high/balance_low)
     * @param {object} data - 事件数据
     */
    async trigger(sourceModule, eventType, data) {
        try {
            logger.debug(`触发告警: ${sourceModule}/${eventType}`);

            // 自动处理恢复：如果是恢复事件，重置对应的故障状态追踪
            // 这样下次故障时 repeat_count 可以重新计数
            if (eventType === 'up' || eventType === 'online') {
                const oppositeType = eventType === 'up' ? 'down' : 'offline';
                const downRules = storage.rule.getBySourceAndEvent(sourceModule, oppositeType);
                if (downRules.length > 0) {
                    logger.debug(`检测到恢复事件,正在重置 ${downRules.length} 条故障规则的状态记录`);
                    for (const rule of downRules) {
                        const fingerprint = this.generateFingerprint(rule, data);
                        storage.stateTracking.reset(rule.id, fingerprint);
                    }
                }
            }

            // 查找匹配当前事件的规则
            const rules = storage.rule.getBySourceAndEvent(sourceModule, eventType);

            if (rules.length === 0) {
                logger.debug(`无匹配规则: ${sourceModule}/${eventType}`);
                return;
            }

            logger.info(`找到 ${rules.length} 条匹配规则`);

            // 对每条规则执行策略引擎
            for (const rule of rules) {
                await this.processRule(rule, data);
            }
        } catch (error) {
            logger.error(`触发告警失败: ${error.message}`);
        }
    }

    /**
     * 处理单条规则
     */
    async processRule(rule, eventData) {
        const { suppression, time_window, channels: channelIds } = rule;

        // 生成指纹 (唯一标识同一问题)
        const fingerprint = this.generateFingerprint(rule, eventData);

        // 1. 检查时间窗口
        if (time_window.enabled && !this.checkTimeWindow(time_window)) {
            logger.debug(`不在时间窗口内,跳过: ${rule.name}`);
            return;
        }

        // 2. 更新状态追踪
        const state = storage.stateTracking.upsert(rule.id, fingerprint, {
            metadata: JSON.stringify(eventData),
        });

        // 3. 检查重复抑制
        const repeatCount = suppression.repeat_count || 1;
        if (state.consecutive_failures < repeatCount) {
            logger.debug(`未达到重复阈值 (${state.consecutive_failures}/${repeatCount}): ${rule.name}`);
            return;
        }

        // 4. 检查静默期
        if (state.last_notified_at) {
            const silenceMs = (suppression.silence_minutes || 0) * 60 * 1000;
            if (Date.now() - state.last_notified_at < silenceMs) {
                logger.debug(`在静默期内,跳过: ${rule.name}`);
                return;
            }
        }

        // 5. 发送通知
        for (const channelId of channelIds) {
            const channel = storage.channel.getById(channelId);
            if (!channel || !channel.enabled) {
                logger.warn(`渠道不存在或已禁用: ${channelId}`);
                continue;
            }

            const notification = {
                rule_id: rule.id,
                channel_id: channelId,
                title: this.formatTitle(rule, eventData),
                message: this.formatMessage(rule, eventData),
                data: eventData,
            };

            this.enqueue(notification);
        }

        // 6. 更新最后通知时间
        storage.stateTracking.updateLastNotified(rule.id, fingerprint);
    }

    /**
     * 发送通知 (核心逻辑)
     */
    async send(notification) {
        const { channel_id, title, message } = notification;
        const channel = storage.channel.getById(channel_id);

        if (!channel) {
            logger.error(`渠道不存在: ${channel_id}`);
            return false;
        }

        try {
            // 解密配置
            const config = JSON.parse(decrypt(channel.config));

            let success = false;

            if (channel.type === 'email') {
                success = await emailChannel.send(config, title, message);
            } else if (channel.type === 'telegram') {
                success = await telegramChannel.send(config, title, message);
            } else {
                logger.error(`未知渠道类型: ${channel.type}`);
                return false;
            }

            // 更新历史记录
            if (success) {
                storage.history.updateStatus(
                    notification.log_id,
                    'sent',
                    new Date().toISOString()
                );
                logger.info(`通知发送成功: ${title}`);
            } else {
                storage.history.updateStatus(
                    notification.log_id,
                    'failed',
                    null,
                    '发送失败'
                );
            }

            return success;
        } catch (error) {
            logger.error(`发送通知失败: ${error.message}`);

            // 更新历史记录为失败
            storage.history.updateStatus(
                notification.log_id,
                'failed',
                null,
                error.message
            );

            return false;
        }
    }

    /**
     * 队列管理
     */
    enqueue(notification) {
        // 创建历史记录
        const log = storage.history.create(notification);
        notification.log_id = log.id;

        // 加入队列
        this.queue.push(notification);

        logger.debug(`通知已加入队列: ${notification.title} (队列长度: ${this.queue.length})`);

        // 确保队列处理器运行
        if (!this.processing) {
            this.startQueueProcessor();
        }
    }

    /**
     * 启动队列处理器
     */
    async startQueueProcessor() {
        if (this.processing) return;

        this.processing = true;

        while (this.queue.length > 0) {
            const notification = this.queue.shift();
            await this.send(notification);
        }

        this.processing = false;
    }

    /**
     * 启动失败重试处理器
     */
    startRetryProcessor() {
        const config = storage.globalConfig.getDefault();
        const intervalMs = (config.retry_interval_seconds || 60) * 1000;

        this.retryTimer = setInterval(async () => {
            try {
                const maxRetry = config.max_retry_times || 3;
                const failedLogs = storage.history.getFailed(maxRetry);

                if (failedLogs.length === 0) return;

                logger.info(`发现 ${failedLogs.length} 条失败记录,准备重试`);

                for (const log of failedLogs) {
                    const retryCount = log.retry_count || 0;
                    if (retryCount >= maxRetry) {
                        logger.warn(`达到最大重试次数,放弃: ${log.title}`);
                        continue;
                    }

                    // 重新加入队列
                    const notification = {
                        rule_id: log.rule_id,
                        channel_id: log.channel_id,
                        title: log.title,
                        message: log.message,
                        data: JSON.parse(log.data || '{}'),
                        log_id: log.id,
                    };

                    this.enqueue(notification);
                }

                // 启动队列处理
                if (!this.processing) {
                    this.startQueueProcessor();
                }
            } catch (error) {
                logger.error(`重试处理器错误: ${error.message}`);
            }
        }, intervalMs);

        logger.info(`失败重试处理器已启动 (间隔: ${intervalMs}ms)`);
    }

    /**
     * 启动定时清理任务
     */
    startCleanupTasks() {
        // 每天凌晨 3 点清理旧记录
        const schedule = () => {
            const now = new Date();
            const tomorrow = new Date(now);
            tomorrow.setDate(tomorrow.getDate() + 1);
            tomorrow.setHours(3, 0, 0, 0);

            const delay = tomorrow - now;

            setTimeout(() => {
                this.cleanup();
                // 递归调用,安排下一次清理
                schedule();
            }, delay);

            logger.info(`下次清理时间: ${tomorrow.toLocaleString('zh-CN')}`);
        };

        schedule();
    }

    /**
     * 清理旧记录
     */
    cleanup() {
        try {
            const config = storage.globalConfig.getDefault();
            const retentionDays = config.history_retention_days || 30;

            const historyResult = storage.history.cleanOld(retentionDays);
            logger.info(`清理历史记录: ${historyResult.changes} 条`);

            // 清理 30 天前的状态记录
            const beforeTimestamp = Date.now() - (30 * 24 * 60 * 60 * 1000);
            const stateResult = storage.stateTracking.cleanOld(beforeTimestamp);
            logger.info(`清理状态记录: ${stateResult.changes} 条`);
        } catch (error) {
            logger.error(`清理任务失败: ${error.message}`);
        }
    }

    /**
     * 加载渠道
     */
    loadChannels() {
        const channels = storage.channel.getEnabled();
        logger.info(`已加载 ${channels.length} 个启用的通知渠道`);
    }

    /**
     * 生成指纹
     */
    generateFingerprint(rule, eventData) {
        // 根据规则和事件数据生成唯一指纹
        const keyParts = [
            rule.source_module,
            rule.event_type,
        ];

        // 添加特定资源的ID
        if (eventData.monitorId) keyParts.push(`monitor:${eventData.monitorId}`);
        else if (eventData.serverId) keyParts.push(`server:${eventData.serverId}`);
        else if (eventData.accountId) keyParts.push(`account:${eventData.accountId}`);
        else keyParts.push('global');

        return keyParts.join(':');
    }

    /**
     * 检查时间窗口
     */
    checkTimeWindow(timeWindow) {
        if (!timeWindow.enabled) return true;

        try {
            const now = new Date();
            const [startHour, startMin] = timeWindow.start.split(':').map(Number);
            const [endHour, endMin] = timeWindow.end.split(':').map(Number);

            const currentMinutes = now.getHours() * 60 + now.getMinutes();
            const startMinutes = startHour * 60 + startMin;
            const endMinutes = endHour * 60 + endMin;

            // 如果结束时间小于开始时间,表示跨天
            if (endMinutes < startMinutes) {
                return currentMinutes >= startMinutes || currentMinutes <= endMinutes;
            }

            return currentMinutes >= startMinutes && currentMinutes <= endMinutes;
        } catch (error) {
            logger.error(`检查时间窗口失败: ${error.message}`);
            return true; // 出错时默认发送
        }
    }

    /**
     * 格式化标题
     */
    formatTitle(rule, eventData) {
        const severityIcon = {
            critical: '🚨',
            warning: '⚠️',
            info: 'ℹ️',
        };

        const icon = severityIcon[rule.severity] || '🔔';
        return `${icon} [${rule.severity.toUpperCase()}] ${rule.name}`;
    }

    /**
     * 格式化消息
     */
    formatMessage(rule, eventData) {
        // 根据事件类型格式化消息
        const lines = [];

        // 添加基本信息
        if (eventData.monitorName) lines.push(`📊 监控项: ${eventData.monitorName}`);
        if (eventData.serverName) lines.push(`🖥️ 主机: ${eventData.serverName}`);
        if (eventData.accountName) lines.push(`💳 账户: ${eventData.accountName}`);

        lines.push(''); // 空行

        // 添加详细信息
        if (eventData.url) lines.push(`🔗 URL: ${eventData.url}`);
        if (eventData.host) lines.push(`🌐 主机: ${eventData.host}`);
        if (eventData.error) lines.push(`❌ 错误: ${eventData.error}`);
        if (eventData.ping !== undefined) lines.push(`⏱️ 响应时间: ${eventData.ping}ms`);
        if (eventData.cpu_usage !== undefined) lines.push(`📊 CPU 使用率: ${eventData.cpu_usage}%`);
        if (eventData.mem_percent !== undefined) lines.push(`💾 内存使用率: ${eventData.mem_percent}%`);
        if (eventData.balance !== undefined) lines.push(`💰 余额: $${eventData.balance}`);
        if (eventData.threshold !== undefined) lines.push(`🎯 阈值: ${eventData.threshold}`);

        // 如果没有特定信息,显示完整数据
        if (lines.length <= 1) {
            return JSON.stringify(eventData, null, 2);
        }

        lines.push('');
        lines.push(`时间: ${new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })}`);

        return lines.join('\n');
    }

    /**
     * 停止服务
     */
    stop() {
        if (this.retryTimer) {
            clearInterval(this.retryTimer);
            this.retryTimer = null;
        }

        emailChannel.close();
        logger.info('通知服务已停止');
    }
}

// 导出单例
module.exports = new NotificationService();
