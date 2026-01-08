/**
 * 主机监控服务
 * 定时探测主机状态
 */

const cron = require('node-cron');
const { ServerAccount, ServerMonitorLog, ServerMonitorConfig } = require('./models');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('Monitor');

class MonitorService {
  constructor() {
    this.task = null;
    this.isRunning = false;
    // 内存缓存：serverId -> metrics
    this.metricsCache = new Map();
  }

  /**
   * 获取内存中的实时指标 (前端极速访问入口)
   */
  getMetrics(serverId) {
    return this.metricsCache.get(serverId) || null;
  }

  /**
   * 启动监控服务 (已废弃自动拨测，仅保留占位符)
   */
  start() {
    logger.info('监控服务已启动');
  }

  /**
   * 停止监控服务
   */
  stop() {
    this.metricsCache.clear();
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
   * 探测所有主机
   */
  async probeAllServers() {
    try {
      const servers = ServerAccount.getAll();

      if (servers.length === 0) {
        return;
      }

      logger.info(`开始探测 ${servers.length} 台主机`);

      // 并发探测所有主机
      const results = await Promise.allSettled(servers.map(server => this.probeServer(server)));

      // 统计结果
      const successCount = results.filter(r => r.status === 'fulfilled' && r.value.success).length;
      const failedCount = results.length - successCount;

      logger.info(`探测完成: 成功 ${successCount}, 失败 ${failedCount}`);

      // 清理过期日志
      this.cleanupOldLogs();
    } catch (error) {
      logger.error('探测主机失败', error.message);
    }
  }

  /**
   * 探测单个主机
   * @param {Object} server - 主机配置
   * @param {boolean} silent - 是否静默探测 (不写入数据库和日志)
   * @returns {Promise<Object>} 探测结果
   */
  async probeServer(server, silent = false) {
    // 纯 Agent 模式，跳过 SSH 相关的后台探测
    const agentService = require('./agent-service');
    const agentStatus = agentService.getStatus(server.id);
    const agentMetrics = agentService.getMetrics(server.id);

    // 状态记录逻辑
    const oldStatus = server.status;

    try {
      // 1. 先用 TCP ping 测量网络延迟
      const responseTime = await this.tcpPing(server.host, server.port || 22);

      // 2. 检查 Agent 状态
      if (agentStatus.connected && agentMetrics) {
        const metrics = {
          ...agentMetrics,
          cached_at: new Date().toISOString(),
        };

        // 更新内存缓存
        this.metricsCache.set(server.id, metrics);

        if (!silent) {
          ServerAccount.updateStatus(server.id, {
            status: 'online',
            last_check_time: new Date().toISOString(),
            last_check_status: 'success',
            response_time: responseTime,
          });

          ServerMonitorLog.create({
            server_id: server.id,
            status: 'success',
            response_time: responseTime,
          });
        }

        return { success: true, serverId: server.id, responseTime };
      } else {
        // Agent 未连接，但 TCP 可达
        if (!silent) {
          ServerAccount.updateStatus(server.id, {
            status: 'pending',
            last_check_time: new Date().toISOString(),
            last_check_status: 'agent_offline',
            response_time: responseTime,
          });
        }
        return { success: false, serverId: server.id, error: 'Agent 未连接', responseTime };
      }
    } catch (error) {
      // TCP ping 失败
      if (!silent) {
        ServerAccount.updateStatus(server.id, {
          status: 'offline',
          last_check_time: new Date().toISOString(),
          last_check_status: 'failed',
          response_time: null,
        });

        ServerMonitorLog.create({
          server_id: server.id,
          status: 'failed',
          response_time: null,
          error_message: error.message,
        });
      }

      return {
        success: false,
        serverId: server.id,
        error: error.message,
        responseTime: null,
      };
    }
  }

  /**
   * TCP Ping - 测量 TCP 端口连接延迟
   * @param {string} host - 主机地址
   * @param {number} port - 端口号
   * @param {number} timeout - 超时时间(ms)
   * @returns {Promise<number>} 延迟时间(ms)
   */
  tcpPing(host, port, timeout = 5000) {
    return new Promise((resolve, reject) => {
      const net = require('net');
      const { performance } = require('perf_hooks');
      const startTime = performance.now();

      const socket = new net.Socket();

      socket.setNoDelay(true);
      socket.setTimeout(timeout);

      socket.on('connect', () => {
        const latency = Math.round(performance.now() - startTime);
        socket.destroy();
        resolve(latency);
      });

      socket.on('timeout', () => {
        socket.destroy();
        reject(new Error('TCP ping timeout'));
      });

      socket.on('error', err => {
        socket.destroy();
        reject(err);
      });

      socket.connect(port, host);
    });
  }

  /**
   * 手动触发探测所有主机
   * @param {boolean} silent - 是否静默探测
   * @returns {Promise<Object>} 探测结果
   */
  async manualProbeAll(silent = false) {
    if (!silent) logger.info('手动触发探测所有主机');

    const servers = ServerAccount.getAll();

    if (servers.length === 0) {
      return {
        success: true,
        message: '没有主机需要探测',
        results: [],
      };
    }

    const results = await Promise.allSettled(
      servers.map(server => this.probeServer(server, silent))
    );

    const successCount = results.filter(r => r.status === 'fulfilled' && r.value.success).length;
    const failedCount = results.length - successCount;

    return {
      success: true,
      message: `探测完成: 成功 ${successCount}, 失败 ${failedCount}`,
      total: servers.length,
      successCount,
      failedCount,
      results: results.map(r =>
        r.status === 'fulfilled' ? r.value : { success: false, error: r.reason.message }
      ),
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
    const agentService = require('./agent-service');

    // 获取 Agent 连接数
    const onlineAgents = agentService.getConnectionCount ? agentService.getConnectionCount() : 0;

    return {
      isRunning: onlineAgents > 0,
      interval: (config?.metrics_collect_interval || 300) * 1000, // 转换为毫秒 (历史指标采集间隔)
      cachedServers: this.metricsCache.size,
      activeStreams: onlineAgents,
      config: {
        probe_interval: config?.probe_interval || 60,
        probe_timeout: config?.probe_timeout || 10,
        log_retention_days: config?.log_retention_days || 7,
        auto_start: config?.auto_start || 0,
      },
      servers: {
        total: servers.length,
        online: ServerAccount.getOnlineCount(),
        offline: ServerAccount.getOfflineCount(),
      },
    };
  }
}

// 导出单例
module.exports = new MonitorService();
