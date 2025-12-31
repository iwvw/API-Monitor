/**
 * Metrics WebSocket 服务
 * 向前端实时推送主机指标数据
 */

const { WebSocketServer } = require('ws');
const { createLogger } = require('../utils/logger');

const logger = createLogger('Metrics');

let wss = null;
let broadcastInterval = null;

/**
 * 初始化 Metrics WebSocket 服务
 * @param {http.Server} server - HTTP 服务器实例
 * @returns {WebSocketServer} WebSocket 服务器实例
 */
function init(server) {
  wss = new WebSocketServer({
    noServer: true,
    perMessageDeflate: false,
  });

  wss.on('connection', handleConnection);

  // 启动定时广播
  startBroadcast();

  logger.success('Metrics WebSocket 服务已初始化');
  return wss;
}

/**
 * 处理 WebSocket 连接
 */
function handleConnection(ws, request) {
  logger.info('新的 Metrics 订阅者连接');

  ws.isAlive = true;

  ws.on('pong', () => {
    ws.isAlive = true;
  });

  ws.on('close', () => {
    logger.info('Metrics 订阅者断开');
  });

  ws.on('error', error => {
    logger.error('Metrics WebSocket 错误:', error.message);
  });

  // 立即发送一次当前数据
  sendMetricsUpdate(ws);
}

/**
 * 启动定时广播
 */
function startBroadcast() {
  // 每 5 秒广播一次指标更新
  broadcastInterval = setInterval(() => {
    if (wss && wss.clients.size > 0) {
      broadcastMetrics();
    }
  }, 5000);

  // 心跳检测
  setInterval(() => {
    if (wss) {
      wss.clients.forEach(ws => {
        if (ws.isAlive === false) {
          return ws.terminate();
        }
        ws.isAlive = false;
        ws.ping();
      });
    }
  }, 30000);
}

/**
 * 广播指标到所有客户端
 */
function broadcastMetrics() {
  const metricsData = collectMetrics();

  const message = JSON.stringify({
    type: 'metrics_update',
    data: metricsData,
    timestamp: Date.now(),
  });

  wss.clients.forEach(client => {
    if (client.readyState === 1) {
      // WebSocket.OPEN
      try {
        client.send(message);
      } catch (e) {
        // 忽略发送错误
      }
    }
  });
}

/**
 * 发送指标更新到单个客户端
 */
function sendMetricsUpdate(ws) {
  if (ws.readyState !== 1) return;

  const metricsData = collectMetrics();

  try {
    ws.send(
      JSON.stringify({
        type: 'metrics_update',
        data: metricsData,
        timestamp: Date.now(),
      })
    );
  } catch (e) {
    // 忽略发送错误
  }
}

/**
 * 收集所有主机的指标数据
 */
function collectMetrics() {
  try {
    const agentService = require('../../modules/server-management/agent-service');
    const { serverStorage } = require('../../modules/server-management/storage');

    const servers = serverStorage.getAll();
    const metricsData = [];

    servers.forEach(server => {
      const metrics = agentService.getMetrics(server.id);
      if (metrics) {
        // agent-service 存储的是扁平结构，直接读取
        metricsData.push({
          serverId: server.id,
          serverName: server.name,
          metrics: {
            cpu_usage: metrics.cpu_usage || '0%',
            load: metrics.load || '0 0 0',
            cores: metrics.cores || '-',
            mem_usage: metrics.mem_usage || metrics.mem || '0/0MB',
            disk_usage: metrics.disk_usage || metrics.disk || '-/- (0%)',
            network: metrics.network || {
              connections: 0,
              rx_speed: '0 B/s',
              tx_speed: '0 B/s',
            },
            docker: {
              installed: metrics.docker?.installed || false,
              running: metrics.docker?.running || 0,
              stopped: metrics.docker?.stopped || 0,
              containers: metrics.docker?.containers || [],
            },
          },
          timestamp: metrics.timestamp || Date.now(),
        });
      }
    });

    return metricsData;
  } catch (error) {
    logger.error('收集指标失败:', error.message);
    return [];
  }
}

/**
 * 格式化磁盘使用率
 */
function formatDiskUsage(disk) {
  if (!disk || !Array.isArray(disk) || disk.length === 0) {
    return '-/- (0%)';
  }
  const root = disk[0];
  return `${root.used || '-'}/${root.total || '-'} (${root.usage || '0%'})`;
}

/**
 * 获取客户端数量
 */
function getClientCount() {
  return wss ? wss.clients.size : 0;
}

module.exports = {
  init,
  wss: () => wss,
  getClientCount,
  broadcastMetrics,
};
