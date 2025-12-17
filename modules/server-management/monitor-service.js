/**
 * 服务器监控服务
 * 定时探测服务器状态
 */

const cron = require('node-cron');
const { ServerAccount, ServerMonitorLog, ServerMonitorConfig } = require('../../src/db/models');
const sshService = require('./ssh-service');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('ServerMonitor');

class MonitorService {
    constructor() {
        this.task = null;
        this.isRunning = false;
    }

    /**
     * 启动监控服务
     */
    start() {
        if (this.isRunning) {
            logger.warn('监控服务已在运行');
            return;
        }

        const config = ServerMonitorConfig.get();
        if (!config || !config.auto_start) {
            logger.info('监控服务未启用（auto_start = 0）');
            return;
        }

        const interval = config.probe_interval || 60;

        // 创建定时任务（每 N 秒执行一次）
        this.task = cron.schedule(`*/${interval} * * * * *`, async () => {
            await this.probeAllServers();
        });

        this.isRunning = true;
        logger.success(`监控服务已启动，探测间隔: ${interval} 秒`);

        // 立即执行一次探测
        this.probeAllServers();
    }

    /**
     * 停止监控服务
     */
    stop() {
        if (this.task) {
            this.task.stop();
            this.task = null;
        }

        this.isRunning = false;
        logger.info('监控服务已停止');
    }

    /**
     * 重启监控服务
     */
    restart() {
        this.stop();
        this.start();
    }

    /**
     * 探测所有服务器
     */
    async probeAllServers() {
        try {
            const servers = ServerAccount.getAll();

            if (servers.length === 0) {
                return;
            }

            logger.info(`开始探测 ${servers.length} 台服务器`);

            // 并发探测所有服务器
            const results = await Promise.allSettled(
                servers.map(server => this.probeServer(server))
            );

            // 统计结果
            const successCount = results.filter(r => r.status === 'fulfilled' && r.value.success).length;
            const failedCount = results.length - successCount;

            logger.info(`探测完成: 成功 ${successCount}, 失败 ${failedCount}`);

            // 清理过期日志
            this.cleanupOldLogs();
        } catch (error) {
            logger.error('探测服务器失败', error.message);
        }
    }

    /**
     * 探测单个服务器
     * @param {Object} server - 服务器配置
     * @returns {Promise<Object>} 探测结果
     */
    async probeServer(server) {
        const startTime = Date.now();

        try {
            // 测试 SSH 连接
            const result = await sshService.testConnection(server);

            const responseTime = Date.now() - startTime;

            // 更新服务器状态
            ServerAccount.updateStatus(server.id, {
                status: result.success ? 'online' : 'offline',
                last_check_time: new Date().toISOString(),
                last_check_status: result.success ? 'success' : 'failed',
                response_time: responseTime
            });

            // 记录监控日志
            ServerMonitorLog.create({
                server_id: server.id,
                status: result.success ? 'success' : 'failed',
                response_time: responseTime,
                error_message: result.error || null
            });

            return {
                success: result.success,
                serverId: server.id,
                serverName: server.name,
                responseTime
            };
        } catch (error) {
            const responseTime = Date.now() - startTime;

            // 更新服务器状态为离线
            ServerAccount.updateStatus(server.id, {
                status: 'offline',
                last_check_time: new Date().toISOString(),
                last_check_status: 'failed',
                response_time: responseTime
            });

            // 记录监控日志
            ServerMonitorLog.create({
                server_id: server.id,
                status: 'failed',
                response_time: responseTime,
                error_message: error.message
            });

            return {
                success: false,
                serverId: server.id,
                serverName: server.name,
                error: error.message,
                responseTime
            };
        }
    }

    /**
     * 手动触发探测所有服务器
     * @returns {Promise<Object>} 探测结果
     */
    async manualProbeAll() {
        logger.info('手动触发探测所有服务器');

        const servers = ServerAccount.getAll();

        if (servers.length === 0) {
            return {
                success: true,
                message: '没有服务器需要探测',
                results: []
            };
        }

        const results = await Promise.allSettled(
            servers.map(server => this.probeServer(server))
        );

        const successCount = results.filter(r => r.status === 'fulfilled' && r.value.success).length;
        const failedCount = results.length - successCount;

        return {
            success: true,
            message: `探测完成: 成功 ${successCount}, 失败 ${failedCount}`,
            total: servers.length,
            successCount,
            failedCount,
            results: results.map(r => r.status === 'fulfilled' ? r.value : { success: false, error: r.reason.message })
        };
    }

    /**
     * 清理过期日志
     */
    cleanupOldLogs() {
        try {
            const config = ServerMonitorConfig.get();
            const retentionDays = config?.log_retention_days || 7;

            const deletedCount = ServerMonitorLog.deleteOldLogs(retentionDays);

            if (deletedCount > 0) {
                logger.info(`清理过期日志: ${deletedCount} 条`);
            }
        } catch (error) {
            logger.error('清理过期日志失败', error.message);
        }
    }

    /**
     * 获取监控服务状态
     * @returns {Object} 监控服务状态
     */
    getStatus() {
        const config = ServerMonitorConfig.get();
        const servers = ServerAccount.getAll();

        return {
            isRunning: this.isRunning,
            config: {
                probe_interval: config?.probe_interval || 60,
                probe_timeout: config?.probe_timeout || 10,
                log_retention_days: config?.log_retention_days || 7,
                auto_start: config?.auto_start || 0
            },
            servers: {
                total: servers.length,
                online: ServerAccount.getOnlineCount(),
                offline: ServerAccount.getOfflineCount()
            }
        };
    }
}

// 导出单例
module.exports = new MonitorService();
