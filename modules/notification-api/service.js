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

        // --- 增强功能状态 ---
        this.batchBuffer = new Map(); // channelId -> notification[]
        this.batchTimers = new Map();
        this.sentCountInLastHour = 0;
        this.lastResetTime = Date.now();
        this.circuitBroken = false;
        this.startupTime = Date.now(); // 记录启动时间，用于启动保护
        this.activeProcessing = new Set(); // Bug 修复：标记当前正在处理的记录ID，防止重复捞取
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

        // 加载未完成的通知 (重启恢复)
        this.loadIncompleteNotifications();

        this.initialized = true;
        logger.info('✅ 通知服务已初始化');
    }

    /**
     * 触发告警 (供其他模块调用)
     * @param {string} sourceModule - 来源模块 (uptime/server/zeabur/openai)
     * @param {string} eventType - 事件类型 (down/up/offline/cpu_high/balance_low)
     * @param {object} data - 事件数据
     *
     * 注意：uptime 模块已通过状态机保证 down/up 仅在状态变迁时触发，
     *       不再需要通知层做抖动检测。
     */
    async trigger(sourceModule, eventType, data) {
        try {
            logger.debug(`触发告警: ${sourceModule}/${eventType}`);

            // 恢复事件：重置对应故障的状态追踪
            if (eventType === 'up' || eventType === 'online') {
                const oppositeType = eventType === 'up' ? 'down' : 'offline';
                const downRules = storage.rule.getBySourceAndEvent(sourceModule, oppositeType);
                for (const rule of downRules) {
                    const fingerprint = this.generateFingerprint(rule, data);
                    storage.stateTracking.reset(rule.id, fingerprint);
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

        // 4.1 检查手动静默 (quiet_until)
        if (rule.quiet_until && new Date(rule.quiet_until) > new Date()) {
            logger.debug(`规则处于手动静默期,跳过: ${rule.name}`);
            return;
        }

        // 4.2 检查全局/特定维护计划
        const maintenance = this.checkMaintenance(rule, eventData);
        if (maintenance) {
            logger.debug(`匹配到维护计划 [${maintenance.reason}], 跳过: ${rule.name}`);
            return;
        }

        // 5. 抖动检测 (Anti-Flapping)
        const isFlapping = this.detectFlapping(state, rule.event_type);
        if (isFlapping) {
            // 如果正在抖动，仅发送特定频率或直接抑制
            logger.debug(`检测到监控项处于抖动状态, 抑制重复变动通知: ${rule.name}`);
            return;
        }

        // 6. 发送通知
        for (const channelId of channelIds) {
            const channel = storage.channel.getById(channelId);
            if (!channel || !channel.enabled) {
                logger.warn(`渠道不存在或已禁用: ${channelId}`);
                continue;
            }

            const ctx = { rule, eventData, severity: rule.severity, state };
            const notification = {
                rule_id: rule.id,
                channel_id: channelId,
                title: this.formatTitle(rule, eventData, ctx),
                message: this.formatMessage(rule, eventData, ctx),
                data: eventData,
                severity: rule.severity
            };

            this.enqueue(notification);
        }

        // 7. 更新最后通知时间
        storage.stateTracking.updateLastNotified(rule.id, fingerprint);
    }

    /**
     * 发送通知 (核心逻辑，支持备用渠道漂移)
     */
    async send(notification) {
        const { channel_id, title, message, rule_id } = notification;
        let success = await this.doSend(channel_id, title, message, notification);

        // 如果首选渠道失败且有备用渠道，尝试漂移
        if (!success && rule_id) {
            const rule = storage.rule.getById(rule_id);
            if (rule?.backup_channels?.length > 0) {
                logger.warn(`首选渠道发送失败，尝试通过 ${rule.backup_channels.length} 个备用渠道漂移...`);
                for (const backupId of rule.backup_channels) {
                    success = await this.doSend(backupId, `[漂移] ${title}`, message, notification);
                    if (success) {
                        logger.info(`备用渠道 ${backupId} 漂移成功`);
                        break;
                    }
                }
            }
        }

        // 更新历史记录
        if (success) {
            storage.history.updateStatus(notification.log_id, 'sent', new Date().toISOString());
        } else {
            storage.history.updateStatus(notification.log_id, 'failed', null, '所有尝试(含漂移)均失败');
        }

        return success;
    }

    /**
     * 实际执行发送
     */
    async doSend(channel_id, title, message, notification) {
        const channel = storage.channel.getById(channel_id);
        if (!channel) return false;

        try {
            const config = JSON.parse(decrypt(channel.config));
            const options = { notification }; // 传递上下文给渠道格式化

            let success = false;
            if (channel.type === 'email') {
                success = await emailChannel.send(config, title, message, options);
            } else if (channel.type === 'telegram') {
                success = await telegramChannel.send(config, title, message, options);
            }
            return success;
        } catch (error) {
            logger.error(`执行渠道 ${channel_id} 发送失败: ${error.message}`);
            return false;
        }
    }

    /**
     * 队列管理 (增加合并与熔断支持)
     */
    enqueue(notification) {
        // 如果熔断开启，丢弃非 Critical 告警
        if (this.circuitBroken) {
            const rule = storage.rule.getById(notification.rule_id);
            if (rule?.severity !== 'critical') {
                logger.warn(`[熔断控制] 已丢弃非紧急告警: ${notification.title}`);
                return;
            }
        }

        // 创建/获取历史记录
        if (!notification.log_id) {
            const log = storage.history.create(notification);
            notification.log_id = log.id;
        }

        const config = storage.globalConfig.getDefault();
        const startupGracePeriod = 60000; // 启动 60 秒内为保护期
        const isStartup = Date.now() - this.startupTime < startupGracePeriod;

        // 检查是否需要合并 (Batching)
        // 启动保护期内强制开启聚合，防止重启轰炸
        if ((config.enable_batch || isStartup) && !notification.is_retry) {
            let interval = config.batch_interval_seconds || 30;
            if (isStartup && interval < 30) interval = 30; // 启动时至少等待 30 秒以收集首轮扫描的所有故障
            this.addToBatch(notification, interval);
        } else {
            this.queue.push(notification);
        }

        // 启动队列处理器
        if (!this.processing && this.queue.length > 0) {
            this.startQueueProcessor();
        }
    }

    /**
     * 加入合并缓冲区
     */
    addToBatch(notification, interval) {
        const channelId = notification.channel_id;
        if (!this.batchBuffer.has(channelId)) {
            this.batchBuffer.set(channelId, []);
        }

        this.batchBuffer.get(channelId).push(notification);

        // 如果没有定时器，启动一个
        if (!this.batchTimers.has(channelId)) {
            const timer = setTimeout(() => {
                this.flushBatch(channelId);
            }, interval * 1000);
            this.batchTimers.set(channelId, timer);
        }
    }

    /**
     * 刷新并发送合并通知
     */
    async flushBatch(channelId) {
        const notifications = this.batchBuffer.get(channelId) || [];
        this.batchBuffer.delete(channelId);
        this.batchTimers.delete(channelId);

        if (notifications.length === 0) return;

        if (notifications.length === 1) {
            this.queue.push(notifications[0]);
        } else {
            // 创建聚合通知
            const first = notifications[0];
            const batchNotification = {
                ...first,
                title: `📦 [聚合通知] 包含 ${notifications.length} 条告警`,
                message: notifications.map(n => `--- ${n.title} ---\n${n.message}`).join('\n\n'),
                is_batch: true
            };
            this.queue.push(batchNotification);
        }

        if (!this.processing) this.startQueueProcessor();
    }

    /**
     * 熔断检查
     */
    checkRateLimit() {
        const config = storage.globalConfig.getDefault();
        const limit = config.global_rate_limit_per_hour || 100;

        // 每小时重置计数器
        const now = Date.now();
        if (now - this.lastResetTime > 3600000) {
            this.sentCountInLastHour = 0;
            this.lastResetTime = now;
            if (this.circuitBroken) {
                this.circuitBroken = false;
                logger.info('熔断已自动解除，恢复正常发送');
            }
        }

        this.sentCountInLastHour++;

        if (this.sentCountInLastHour > limit) {
            if (!this.circuitBroken) {
                this.circuitBroken = true;
                logger.error(`[🚨 熔断控制] 已达到每小时发送上限 (${limit}), 进入保护模式！仅发送紧急告警。`);
            }
            return false;
        }
        return true;
    }

    /**
     * 检查维护状态
     */
    checkMaintenance(rule, eventData) {
        const activeSchedules = storage.maintenance.getActive();
        if (activeSchedules.length === 0) return null;

        return activeSchedules.find(s => {
            if (s.target_type === 'global') return true;
            if (s.target_type === 'monitor' && s.target_id == eventData.monitorId) return true;
            if (s.target_type === 'server' && s.target_id == eventData.serverId) return true;
            return false;
        });
    }

    /**
     * 启动队列处理器 (支持并发发送)
     */
    async startQueueProcessor() {
        if (this.processing) return;

        this.processing = true;

        try {
            const concurrency = 5; // 最大并发数
            const workers = [];

            // 启动多个 worker 并行处理队列
            for (let i = 0; i < concurrency; i++) {
                workers.push((async () => {
                    while (this.queue.length > 0) {
                        const notification = this.queue.shift();
                        if (!notification) continue;

                        try {
                            if (notification.log_id) {
                                this.activeProcessing.add(notification.log_id);
                            }
                            // Bug 6 修复：在发送前检查速率限制
                            this.checkRateLimit();
                            await this.send(notification);
                        } catch (error) {
                            logger.error(`异步发送通知异常: ${error.message}`);
                        } finally {
                            if (notification.log_id) {
                                this.activeProcessing.delete(notification.log_id);
                            }
                        }
                    }
                })());
            }

            // 等待所有 worker 完成
            await Promise.all(workers);
        } finally {
            this.processing = false;

            // 如果在 worker 完成期间又有新任务加入, 再次触发处理器
            if (this.queue.length > 0) {
                this.startQueueProcessor();
            }
        }
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
                    // 增加时效性检查：不重试 24 小时前的通知
                    const createdAt = new Date(log.created_at).getTime();
                    if (Date.now() - createdAt > 24 * 60 * 60 * 1000) {
                        storage.history.updateStatus(log.id, 'failed', null, '通知超过 24 小时，停止重试');
                        continue;
                    }

                    const retryCount = log.retry_count || 0;
                    if (retryCount >= maxRetry) {
                        logger.warn(`达到最大重试次数,放弃: ${log.title} (ID: ${log.id})`);
                        storage.history.updateStatus(log.id, 'failed', null, `达到最大重试次数 (${maxRetry})`);
                        continue;
                    }

                    // Bug 修复：检查是否已经在队列或者是由于上次还没改回状态且仍在被处理中
                    if (this.queue.some(n => n.log_id === log.id) || this.activeProcessing.has(log.id)) {
                        logger.debug(`通知 (ID: ${log.id}) 已经在处理队列或发送中，跳过本次重试调度`);
                        continue;
                    }

                    logger.info(`正在重试通知: ${log.title} (重试次数: ${retryCount + 1}/${maxRetry})`);

                    // 更新状态为重试中 (这会增加 retry_count)
                    storage.history.updateStatus(log.id, 'retrying');

                    // 重新加入队列
                    const notification = {
                        rule_id: log.rule_id,
                        channel_id: log.channel_id,
                        title: log.title,
                        message: log.message,
                        data: JSON.parse(log.data || '{}'),
                        log_id: log.id,
                        is_retry: true, // Bug 1 修复：标记为重试，防止走聚合分支导致死循环
                    };

                    this.enqueue(notification);
                }

                // 启动队列处理
                if (!this.processing && this.queue.length > 0) {
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
     * 加载未完成的通知 (用于系统启动时恢复)
     */
    async loadIncompleteNotifications() {
        try {
            // 获取待处理和重试中的记录
            const pending = storage.history.getByStatus('pending', 100);
            const retrying = storage.history.getByStatus('retrying', 100);

            const all = [...pending, ...retrying];

            if (all.length === 0) return;

            logger.info(`发现 ${all.length} 条未完成的通知, 正在重新加载...`);

            for (const log of all) {
                // 增加时效性检查：只加载 24 小时以内的未完成通知
                const createdAt = new Date(log.created_at).getTime();
                if (Date.now() - createdAt > 24 * 60 * 60 * 1000) {
                    storage.history.updateStatus(log.id, 'failed', null, '系统重启清理：忽略 24 小时前的陈旧通知');
                    continue;
                }

                // 检查是否已经在队列中 (防止重复)
                if (this.queue.some(n => n.log_id === log.id)) continue;

                // 补丁：更新历史遗留的图标 (如果是恢复类事件且包含旧图标)
                let title = log.title;
                if ((title.includes('恢复') || title.includes('online') || title.includes('up')) &&
                    (title.includes('ℹ️') || title.includes('⚠️'))) {
                    title = title.replace('ℹ️', '✅').replace('⚠️', '✅');
                }

                const notification = {
                    rule_id: log.rule_id,
                    channel_id: log.channel_id,
                    title: title,
                    message: log.message,
                    data: JSON.parse(log.data || '{}'),
                    log_id: log.id,
                    is_backlog: true // 标记为积压通知
                };

                // 使用 enqueue 进入逻辑流，这样可以触发启动期的聚合逻辑
                this.enqueue(notification);
            }
        } catch (error) {
            logger.error(`加载未完成通知失败: ${error.message}`);
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
    formatTitle(rule, eventData, ctx) {
        if (rule.title_template) {
            return this.renderTemplate(rule.title_template, eventData);
        }

        const severityIcon = {
            critical: '🚨',
            warning: '⚠️',
            info: '通知',
        };

        let icon = severityIcon[rule.severity] || '🔔';

        // 特殊处理：恢复类事件使用绿色对勾
        if (rule.event_type === 'up' || rule.event_type === 'online') {
            icon = '✅';
        }

        // 核心优化：直接在标题显示“主体 - 事件”
        const subject = eventData.monitorName || eventData.serverName || '';

        if (subject) {
            return `${icon} ${subject} - ${rule.name}`;
        }

        // 降级逻辑：如果没有具体主体，则显示 [严重程度] 规则名
        const severityText = {
            critical: '紧急',
            warning: '警告',
            info: '提示',
        };
        const text = severityText[rule.severity] || rule.severity.toUpperCase();
        return `${icon} [${text}] ${rule.name}`;
    }

    /**
     * 格式化消息
     */
    formatMessage(rule, eventData, ctx) {
        if (rule.message_template) {
            return this.renderTemplate(rule.message_template, eventData);
        }

        // 根据事件类型格式化消息
        const lines = [];

        // 添加基本信息
        if (eventData.monitorName) lines.push(`项目: ${eventData.monitorName}`);
        if (eventData.serverName) lines.push(`主机: ${eventData.serverName}`);
        if (eventData.error) lines.push(`错误: ${eventData.error}`);
        if (eventData.url || eventData.host) lines.push(`地址: ${eventData.url || eventData.host}`);
        if (eventData.hostname) lines.push(`标识: ${eventData.hostname}`);
        if (eventData.ping !== undefined) lines.push(`响应: ${eventData.ping}ms`);
        if (eventData.cpu_usage !== undefined) lines.push(`CPU: ${eventData.cpu_usage}%`);
        if (eventData.mem_percent !== undefined) lines.push(`内存: ${eventData.mem_percent}%`);
        if (eventData.mem_used !== undefined && eventData.mem_total !== undefined) {
            lines.push(`内存: ${eventData.mem_used}/${eventData.mem_total}MB`);
        }
        if (eventData.balance !== undefined) lines.push(`余额: $${eventData.balance}`);
        if (eventData.threshold !== undefined) lines.push(`阈值: ${eventData.threshold}`);
        if (eventData.lastSeen) {
            const lastSeenDate = new Date(eventData.lastSeen);
            lines.push(`最后活跃: ${lastSeenDate.toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })}`);
        }
        if (eventData.downDuration) lines.push(`故障持续: ${eventData.downDuration}`);

        // 如果没有任何特定信息,显示完整数据
        if (lines.length === 0) {
            return JSON.stringify(eventData, null, 2);
        }

        lines.push('');
        lines.push(`时间: ${new Date().toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })}`);

        return lines.join('\n');
    }

    /**
     * 模板渲染引擎
     */
    renderTemplate(template, data) {
        if (!template) return '';
        return template.replace(/\{\{(.*?)\}\}/g, (match, key) => {
            const val = data[key.trim()];
            return val !== undefined ? val : match;
        });
    }

    /**
     * 抖动检测 (Anti-Flapping)
     * 计算过去 10 次状态变化的频率
     */
    detectFlapping(stateRecord, currentEventType) {
        try {
            const history = JSON.parse(stateRecord.state_history || '[]');
            const eventVal = (currentEventType === 'up' || currentEventType === 'online') ? 1 : 0;
            const now = Date.now();

            // 1. 检查是否处于已锁定的抖动冷静期 (5分钟)
            if (stateRecord.is_flapping && stateRecord.updated_at) {
                const lastUpdate = new Date(stateRecord.updated_at).getTime();
                if (now - lastUpdate < 5 * 60 * 1000) {
                    return true;
                }
            }

            // 2. 记录历史
            history.push({ t: now, v: eventVal });
            if (history.length > 10) history.shift();

            // 计算跳变次数 (v 变化的次数)
            let flips = 0;
            for (let i = 1; i < history.length; i++) {
                if (history[i].v !== history[i - 1].v) flips++;
            }

            // 如果 10 次内有 4 次以上跳变，且时间间隔短（如 10 分钟内），判定为抖动
            const durationMin = (history[history.length - 1].t - history[0].t) / 60000;
            const isFlapping = flips >= 4 && durationMin < 10;

            // 更新到数据库
            storage.stateTracking.upsert(stateRecord.rule_id, stateRecord.fingerprint, {
                state_history: JSON.stringify(history),
                is_flapping: isFlapping ? 1 : 0
            });

            return isFlapping;
        } catch (e) {
            logger.error(`抖动检测异常: ${e.message}`);
            return false;
        }
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
