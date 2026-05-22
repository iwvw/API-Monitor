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
   * 启动监控服务
   */
  start() {
    if (this.isRunning) return;

    const config = ServerMonitorConfig.get();
    const intervalSec = config?.probe_interval || 60;
    const intervalMs = intervalSec * 1000;

    logger.info(`监控服务已启动 (探测间隔: ${intervalSec}秒)`);
    this.isRunning = true;

    // 立即执行一次探测
    this.probeAllServers();

    // 定时执行探测
    this.timer = setInterval(() => {
      this.probeAllServers();
    }, intervalMs);
  }

  /**
   * 停止监控服务
   */
  stop() {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
    this.isRunning = false;
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
    // 纯 Agent 心跳探测模式 (彻底废除 TCP ping 后台探测)
    const agentService = require('./agent-service');
    let agentStatus = { connected: false };
    let agentMetrics = null;

    try {
      agentStatus = agentService.getStatus(server.id);
      agentMetrics = agentService.getMetrics(server.id);
    } catch (e) {
      // Fallback if agent service not ready or method missing
    }

    if (agentStatus.connected) {
      // Agent 在线
      const metrics = agentMetrics ? {
        ...agentMetrics,
        cached_at: new Date().toISOString(),
      } : null;

      if (metrics) {
        // 更新内存缓存
        this.metricsCache.set(server.id, metrics);
      }

      if (!silent) {
        ServerAccount.updateStatus(server.id, {
          status: 'online',
          last_check_time: new Date().toISOString(),
          last_check_status: 'success',
          response_time: 0,
        });

        try {
          agentService.broadcastServerStatus(server.id, 'online', {
            responseTime: 0,
            lastCheckStatus: 'success'
          });
        } catch (e) {
          // ignore
        }

        ServerMonitorLog.create({
          server_id: server.id,
          status: 'success',
          response_time: 0,
        });
      }

      return { success: true, serverId: server.id, responseTime: 0 };

    } else {
      // Agent 离线
      if (!silent) {
        ServerAccount.updateStatus(server.id, {
          status: 'offline',
          last_check_time: new Date().toISOString(),
          last_check_status: 'failed',
          response_time: null,
        });

        try {
          agentService.broadcastServerStatus(server.id, 'offline', {
            responseTime: null,
            lastCheckStatus: 'failed',
            error: 'Agent 离线'
          });
        } catch (e) {
          // ignore
        }

        ServerMonitorLog.create({
          server_id: server.id,
          status: 'failed',
          response_time: null,
          error_message: 'Agent 未连接',
        });
      }

      return {
        success: false,
        serverId: server.id,
        error: 'Agent 未连接',
        responseTime: null,
      };
    }
  }

  /**
   * TCP Ping - 测量 TCP 端口连接延迟
   * @param {string} host - 主机地址
   * @param {number} port - 端口号
   * @param {number} timeout - 超时时间(ms)
   * @param {boolean} checkSSH - 是否验证 SSH 握手标识
   * @returns {Promise<number>} 延迟时间(ms)
   */
  tcpPing(host, port, timeout = 5000, checkSSH = false) {
    return new Promise((resolve, reject) => {
      const net = require('net');
      const { performance } = require('perf_hooks');
      const startTime = performance.now();

      const socket = new net.Socket();
      let hasResolved = false;

      socket.setNoDelay(true);
      socket.setTimeout(timeout);

      socket.on('connect', () => {
        if (!checkSSH) {
          const latency = Math.round(performance.now() - startTime);
          socket.destroy();
          hasResolved = true;
          resolve(latency);
        }
      });

      if (checkSSH) {
        socket.on('data', (chunk) => {
          const str = chunk.toString('utf8');
          if (str.startsWith('SSH-')) {
            const latency = Math.round(performance.now() - startTime);
            socket.destroy();
            if (!hasResolved) {
              hasResolved = true;
              resolve(latency);
            }
          } else {
            socket.destroy();
            if (!hasResolved) {
              hasResolved = true;
              reject(new Error('Invalid SSH banner response'));
            }
          }
        });
      }

      socket.on('timeout', () => {
        socket.destroy();
        if (!hasResolved) {
          hasResolved = true;
          reject(new Error(checkSSH ? 'SSH handshake timeout' : 'TCP ping timeout'));
        }
      });

      socket.on('error', err => {
        socket.destroy();
        if (!hasResolved) {
          hasResolved = true;
          reject(err);
        }
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

  getStatus() {
    const config = ServerMonitorConfig.get();
    const servers = ServerAccount.getAll();
    const agentService = require('./agent-service');

    // 获取 Agent 连接数
    const onlineAgents = agentService.getConnectionCount ? agentService.getConnectionCount() : 0;

    return {
      isRunning: this.isRunning,
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
