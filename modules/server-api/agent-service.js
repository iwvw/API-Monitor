/**
 * Agent 服务 - 基于 Socket.IO 的实时连接管理器
 */

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const EventEmitter = require('events');
const { Server: SocketIOServer } = require('socket.io');
const { serverStorage } = require('./storage');
const {
  Events,
  TaskTypes,
  validateHostState,
  stateToFrontendFormat,
  normalizeNetworkMetrics,
  resolveCpuTemperature,
  sanitizeHostInfo,
} = require('./protocol');
const { ServerMetricsHistory, ServerMonitorConfig } = require('./models');
const { lookupCountryByIp } = require('./geoip-service');
const userSettings = require('../../src/services/userSettings');
const packageInfo = require('../../package.json');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('AgentService');

function normalizeOriginUrl(value) {
  const raw = String(value || '').trim().replace(/\/+$/, '');
  if (!raw) return '';

  try {
    const url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `https://${raw}`);
    if (url.protocol === 'http:' && url.port === '443') {
      url.protocol = 'https:';
      url.port = '';
    }
    return url.origin;
  } catch (error) {
    return raw;
  }
}

class AgentService extends EventEmitter {
  constructor() {
    super();
    // 调试模式 (环境变量 DEBUG=agent 开启)
    this.debug = process.env.DEBUG?.includes('agent');

    // 全局统一 Agent 密钥
    this.globalAgentKey = null;

    // Socket.IO 服务端实例
    this.io = null;

    // 连接池: serverId -> socket
    this.connections = new Map();

    // 主机信息缓存: serverId -> HostInfo
    this.hostInfoCache = new Map();

    // 实时状态缓存: serverId -> { state, timestamp }
    this.stateCache = new Map();

    // 心跳超时定时器: serverId -> timerId
    this.heartbeatTimers = new Map();

    // 心跳超时时间 (毫秒) - 增加到 30 秒以适应采集延迟
    this.heartbeatTimeout = 30000;

    // 兼容旧版 HTTP 推送的缓存 (过渡期使用)
    this.legacyMetrics = new Map();
    this.legacyStatus = new Map();

    // 统一任务注册表: taskId -> taskRecord
    this.taskRegistry = new Map();
    // 等待中的任务 Promise 解析器: taskId -> { resolve, reject }
    this.taskResolvers = new Map();
    // 任务进度轮询器: taskId -> intervalId
    this.taskPollers = new Map();
    // 任务清理策略
    this.taskRetentionMs = 30 * 60 * 1000; // 30 分钟
    this.taskCleanupTimer = setInterval(() => this.cleanupTaskRegistry(), 60 * 1000);
    if (typeof this.taskCleanupTimer.unref === 'function') {
      this.taskCleanupTimer.unref();
    }

    // 初始化加载或生成全局密钥
    this.loadOrGenerateGlobalKey();

    // 记录启动时间，用于抑制启动期间的通知风暴 (60秒静默期)
    this.startupTime = Date.now();
    
    // 资源告警活跃状态缓存 (serverId -> Set of active resource alerts)
    this.activeResourceAlerts = new Map();

    // 高精度滚动指标内存缓存 (serverId -> Array of high precision records)
    this.highPrecisionCache = new Map();

    // Agent 采样时间轴缓存：用 Agent 帧序号/采样时间消除 Socket 到达抖动
    this.metricTimeline = new Map();
  }

  /**
   * 调试日志 (仅在 DEBUG=agent 时输出)
   */
  log(message) {
    if (this.debug) {
      logger.debug(message);
    }
  }

  /**
   * 加载或生成全局 Agent 密钥
   */
  loadOrGenerateGlobalKey() {
    try {
      const { SystemConfig } = require('../../src/db/models');
      const savedKey = SystemConfig.getConfigValue('agent_global_key');

      if (savedKey) {
        this.globalAgentKey = savedKey;
        this.log('已加载全局 Agent 密钥 (来自数据库)');
      } else {
        // 回退逻辑：尝试从旧的文件系统加载
        const oldKeyPath = path.join(__dirname, '../../data/agent-key.txt');
        if (fs.existsSync(oldKeyPath)) {
          this.globalAgentKey = fs.readFileSync(oldKeyPath, 'utf8').trim();
          SystemConfig.setConfig('agent_global_key', this.globalAgentKey, 'Global Agent Authentication Key (Migrated)');
          this.log('已从旧文件迁移 Agent 密钥到数据库');

          // 标记文件可删除（或直接在这里删除，但为了安全建议由用户或清理脚本处理）
          try { fs.renameSync(oldKeyPath, oldKeyPath + '.bak'); } catch (e) { }
        } else {
          this.globalAgentKey = crypto.randomBytes(16).toString('hex');
          SystemConfig.setConfig('agent_global_key', this.globalAgentKey, 'Global Agent Authentication Key');
          this.log('已生成新的全局 Agent 密钥并保存至数据库');
        }
      }
    } catch (error) {
      logger.error(`Key management failed: ${error.message}`);
      this.globalAgentKey = crypto.randomBytes(16).toString('hex');
    }
  }

  /**
   * 获取全局 Agent 密钥
   */
  getAgentKey(serverId) {
    return this.globalAgentKey;
  }

  /**
   * 重新生成全局密钥
   */
  regenerateGlobalKey() {
    this.globalAgentKey = crypto.randomBytes(16).toString('hex');
    try {
      const { SystemConfig } = require('../../src/db/models');
      SystemConfig.setConfig('agent_global_key', this.globalAgentKey);
    } catch (e) {
      console.error('[AgentService] 保存密钥失败:', e.message);
    }
    return this.globalAgentKey;
  }

  /**
   * 验证 Agent 请求 (兼容性方法)
   */
  verifyAgent(serverId, providedKey) {
    return providedKey === this.globalAgentKey;
  }

  /**
   * 获取当前连接的 Agent 数量
   */
  getConnectionCount() {
    return this.connections.size;
  }

  /**
   * 检查 Agent 是否在线
   */
  isAgentOnline(serverId) {
    return this.connections.has(serverId);
  }

  /**
   * 获取指定主机的连接状态 (供 MonitorService 调用)
   */
  getStatus(serverId) {
    const isOnline = this.isAgentOnline(serverId);
    const legacy = this.legacyStatus.get(serverId);

    return {
      connected: isOnline,
      lastSeen: legacy ? legacy.lastSeen : (isOnline ? Date.now() : 0),
      version: legacy ? legacy.version : (isOnline ? 'socket.io' : null)
    };
  }

  /**
   * 获取全部主机当前连接状态快照，供前端连接/重连后对齐在线状态。
   */
  getServerStatusSnapshot() {
    return this.buildServerStatusSnapshot(serverStorage.getAll());
  }

  /**
   * 根据主机列表生成当前连接状态快照。
   */
  buildServerStatusSnapshot(servers = []) {
    const now = Date.now();

    return servers.map(server => {
      const socket = this.connections.get(server.id);
      const legacy = this.legacyStatus.get(server.id);
      const online = !!socket;

      return {
        serverId: server.id,
        status: online ? 'online' : (server.status || 'offline'),
        agent_online: online,
        connectedAt: socket?._connectedAt || 0,
        lastSeen: legacy?.lastSeen || 0,
        timestamp: now,
      };
    });
  }

  /**
   * 获取指定主机的最新指标 (供 MonitorService 调用)
   */
  getMetrics(serverId) {
    // 优先从 stateCache 获取
    const cached = this.stateCache.get(serverId);
    if (cached) {
      const hostInfo = this.hostInfoCache.get(serverId) || {};
      // 转换格式
      return stateToFrontendFormat(cached.state, hostInfo);
    }

    // 回退到 legacyMetrics
    return this.legacyMetrics.get(serverId) || null;
  }

  /**
   * 追加高频实时指标至内存滚动缓存中，限制大小以保证性能
   */
  appendHighPrecisionMetric(serverId, frontendMetrics, timestamp) {
    if (!this.highPrecisionCache.has(serverId)) {
      this.highPrecisionCache.set(serverId, []);
    }
    const cache = this.highPrecisionCache.get(serverId);

    // 防止因网络突发或重连导致瞬间存入多条时间相近的记录，确保滚动数据的时间步长均匀
    const lastRecord = cache[cache.length - 1];
    if (lastRecord) {
      const lastTimestamp = new Date(lastRecord.recorded_at).getTime();
      if (timestamp - lastTimestamp < 500) {
        return;
      }
    }

    // 解析内存数值 (格式: "123/456MB")
    let memUsed = 0;
    let memTotal = 0;
    if (frontendMetrics.mem && typeof frontendMetrics.mem === 'string') {
      const parts = frontendMetrics.mem.replace('MB', '').split('/');
      memUsed = parseInt(parts[0]) || 0;
      memTotal = parseInt(parts[1]) || 0;
    }

    // 解析 GPU 显存
    let gpuMemUsed = 0;
    if (frontendMetrics.gpu_mem_used !== undefined) {
      gpuMemUsed = frontendMetrics.gpu_mem_used;
    } else if (frontendMetrics.gpu_mem && typeof frontendMetrics.gpu_mem === 'string' && frontendMetrics.gpu_mem.includes('/')) {
      const parts = frontendMetrics.gpu_mem.replace('MB', '').split('/');
      gpuMemUsed = parseInt(parts[0]) || 0;
    }

    // 解析 GPU 使用率
    let gpuUsageNum = null;
    if (frontendMetrics.gpu_usage !== undefined && frontendMetrics.gpu_usage !== null) {
      gpuUsageNum = parseFloat(frontendMetrics.gpu_usage) || 0;
    }

    // 解析网速
    const parseSpeedToBytes = (speedStr) => {
      if (!speedStr || typeof speedStr !== 'string') return 0;
      const match = speedStr.trim().match(/^([0-9.]+)\s*([A-Za-z/]+)$/);
      if (!match) return 0;
      const value = parseFloat(match[1]);
      const unit = match[2].toLowerCase();
      if (unit.startsWith('g')) return value * 1024 * 1024 * 1024;
      if (unit.startsWith('m')) return value * 1024 * 1024;
      if (unit.startsWith('k')) return value * 1024;
      return value;
    };

    const record = {
      server_id: serverId,
      cpu_usage: parseFloat(frontendMetrics.cpu_usage) || 0,
      cpu_load: frontendMetrics.load || '',
      cpu_cores: frontendMetrics.physical_cores || frontendMetrics.cores || 1,
      cpu_threads: frontendMetrics.logical_cores || frontendMetrics.cores || 1,
      cpu_temp: resolveCpuTemperature(frontendMetrics),
      cpu_power: parseFloat(frontendMetrics.cpu_power) || parseFloat(frontendMetrics.cpu_power_w) || 0,
      mem_used: memUsed,
      mem_total: memTotal,
      mem_usage: frontendMetrics.mem_percent || 0,
      disk_used: frontendMetrics.disk_used || '',
      disk_total: frontendMetrics.disk_total || '',
      disk_usage: frontendMetrics.disk_percent || 0,
      docker_installed: frontendMetrics.docker?.installed ? 1 : 0,
      docker_running: frontendMetrics.docker?.running || 0,
      docker_stopped: frontendMetrics.docker?.stopped || 0,
      gpu_usage: gpuUsageNum,
      gpu_mem_percent: frontendMetrics.gpu_mem_percent || 0,
      gpu_mem_used: gpuMemUsed,
      gpu_mem_total: frontendMetrics.gpu_mem_total || 0,
      gpu_power: parseFloat(frontendMetrics.gpu_power) || 0,
      gpu_temp: parseFloat(frontendMetrics.gpu_temp) || 0,
      platform: frontendMetrics.platform || '',
      net_rx: parseSpeedToBytes(frontendMetrics.network?.rx_speed),
      net_tx: parseSpeedToBytes(frontendMetrics.network?.tx_speed),
      recorded_at: new Date(timestamp).toISOString(),
    };

    cache.push(record);

    // 最大保留 360 点 (按照 1.5 秒一上报，约 9 分钟)
    if (cache.length > 360) {
      cache.shift();
    }
  }

  /**
   * 从内存中获取高精度滚动指标历史 (返回降序，新纪录在最前，以与 SQLite 保持一致)
   */
  getHighPrecisionHistory(serverId, limit = 300) {
    const cache = this.highPrecisionCache.get(serverId) || [];
    const reversed = [...cache].reverse();
    return reversed.slice(0, limit);
  }

  resolveMetricTimestamp(serverId, state = {}, receivedAt = Date.now()) {
    const rawInterval = Number(state.sample_interval_ms || state.interval_ms);
    const sampleInterval = Number.isFinite(rawInterval)
      ? Math.min(60000, Math.max(500, rawInterval))
      : 1500;
    const agentTimestamp = Number(state.timestamp_ms || state.timestamp || 0);
    const sequence = Number(state.sequence);
    const previous = this.metricTimeline.get(serverId);
    let timestamp = receivedAt;

    if (previous) {
      let delta = NaN;
      const hasAgentTimestamp = Number.isFinite(agentTimestamp) && agentTimestamp > previous.agentTimestamp;
      const hasSequence = Number.isFinite(sequence) && sequence > previous.sequence;

      if (hasAgentTimestamp) {
        delta = agentTimestamp - previous.agentTimestamp;
      } else if (hasSequence) {
        delta = (sequence - previous.sequence) * sampleInterval;
      }

      if (Number.isFinite(delta) && delta > 0 && delta < 120000) {
        const sequenceDelta = hasSequence ? sequence - previous.sequence : 1;
        const expectedDelta = Math.max(1, sequenceDelta) * sampleInterval;
        const snapTolerance = Math.max(650, sampleInterval * 0.45);
        const normalizedDelta = Math.abs(delta - expectedDelta) <= snapTolerance
          ? expectedDelta
          : delta;

        timestamp = previous.timestamp + normalizedDelta;

        // 如果 Agent 重启、系统时间跳变或长时间断线，重新锚定到服务端收到时间。
        if (Math.abs(timestamp - receivedAt) > Math.max(15000, sampleInterval * 10)) {
          timestamp = receivedAt;
        }
      }
    }

    this.metricTimeline.set(serverId, {
      timestamp,
      receivedAt,
      agentTimestamp: Number.isFinite(agentTimestamp) ? agentTimestamp : 0,
      sequence: Number.isFinite(sequence) ? sequence : 0,
      sampleInterval,
    });

    return timestamp;
  }

  /**
   * 向 Agent 发送升级任务
   */
  sendUpgradeTask(serverId) {
    if (!this.isAgentOnline(serverId)) return false;

    return this.sendTask(serverId, {
      type: TaskTypes.UPGRADE || 5, // 确保 TaskTypes.UPGRADE 存在，否则使用魔数 5
      data: '', // 升级任务不需要额外数据，Agent 会自动构造 URL
      timeout: 300, // 5分钟超时
    });
  }

  /**
   * 获取 Agent 连接详细信息 (用于精确判定上线时间)
   */
  getAgentConnectionInfo(serverId) {
    const socket = this.connections.get(serverId);
    if (!socket) return null;

    // 尝试获取版本号
    const hostInfo = this.hostInfoCache.get(serverId);
    const version = hostInfo ? hostInfo.agent_version : null;

    return {
      serverId,
      connectedAt: socket._connectedAt || 0,
      version,
      socketId: socket.id,
    };
  }

  // ==================== Socket.IO 服务 ====================

  /**
   * 初始化 Socket.IO 服务
   * @param {Object} httpServer - HTTP 服务器实例
   */
  initSocketIO(httpServer) {
    this.io = new SocketIOServer(httpServer, {
      cors: {
        origin: '*',
        methods: ['GET', 'POST'],
      },
      pingTimeout: 30000,
      pingInterval: 10000,
    });

    // Agent 命名空间 - 处理 Agent 连接
    const agentNamespace = this.io.of('/agent');
    agentNamespace.on('connection', socket => this.handleAgentConnection(socket));

    // Metrics 命名空间 - 处理前端订阅
    const metricsNamespace = this.io.of('/metrics');
    metricsNamespace.on('connection', socket => this.handleFrontendConnection(socket));

    // 启动历史指标自动采集定时器
    this.startHistoryCollector();

    this.log('Socket.IO 已初始化 (命名空间: /agent, /metrics)');
  }

  /**
   * 启动历史指标自动采集
   */
  startHistoryCollector() {
    // 如果已存在定时器，先清除
    if (this.historyCollectorTimer) {
      clearInterval(this.historyCollectorTimer);
    }

    // 获取采集间隔 (优先从配置读取，默认 300 秒)
    const config = ServerMonitorConfig.get();
    const intervalSec = config?.metrics_collect_interval || 60;
    const intervalMs = intervalSec * 1000;

    this.log(`历史指标自动采集已启动 (间隔: ${intervalSec}秒)`);

    // 立即执行一次采集
    this.collectHistoryMetrics();

    // 设置定时采集
    this.historyCollectorTimer = setInterval(() => {
      this.collectHistoryMetrics();
    }, intervalMs);
  }

  /**
   * 采集当前所有在线主机的指标并存入历史记录
   * 增加数据新鲜度检查和去重逻辑，避免保存陈旧或重复的数据
   */
  collectHistoryMetrics() {
    try {
      let collected = 0;
      let skippedStale = 0;
      let skippedDuplicate = 0;
      const servers = serverStorage.getAll();
      const now = Date.now();

      // 获取采集间隔用于判断数据新鲜度 (默认 60 秒)
      const config = ServerMonitorConfig.get();
      const intervalMs = (config?.metrics_collect_interval || 60) * 1000;
      // 数据超过 2 倍采集间隔认为陈旧
      const staleThreshold = intervalMs * 2;

      for (const server of servers) {
        const cached = this.stateCache.get(server.id);
        if (!cached) continue;

        // 检查数据新鲜度
        const dataAge = now - (cached.receivedAt || cached.timestamp);
        if (dataAge > staleThreshold) {
          skippedStale++;
          if (this.debug) {
            this.log(`跳过陈旧数据: ${server.id} (${Math.round(dataAge / 1000)}秒前)`);
          }
          continue;
        }

        const hostInfo = this.hostInfoCache.get(server.id) || {};
        const state = cached.state;

        // 使用协议转换函数获取前端格式指标
        const frontendMetrics = stateToFrontendFormat(state, hostInfo);

        // 生成数据指纹用于去重 (使用关键指标)
        const dataFingerprint = `${server.id}:${frontendMetrics.cpu_usage}:${frontendMetrics.cpu_temp}:${frontendMetrics.cpu_power}:${frontendMetrics.mem_percent}:${frontendMetrics.gpu_usage}:${frontendMetrics.gpu_mem_used}:${frontendMetrics.gpu_mem_percent}:${frontendMetrics.gpu_power}:${frontendMetrics.gpu_temp}:${frontendMetrics.load}`;

        // 初始化去重缓存
        if (!this.lastHistoryFingerprints) {
          this.lastHistoryFingerprints = new Map();
        }

        // 检查是否与上次保存的数据完全相同
        if (this.lastHistoryFingerprints.get(server.id) === dataFingerprint) {
          skippedDuplicate++;
          if (this.debug) {
            this.log(`跳过重复数据: ${server.id}`);
          }
          continue;
        }

        // 更新指纹缓存
        this.lastHistoryFingerprints.set(server.id, dataFingerprint);

        // 解析内存数值 (格式: "123/456MB")
        let memUsed = 0;
        let memTotal = 0;
        if (frontendMetrics.mem && typeof frontendMetrics.mem === 'string') {
          const parts = frontendMetrics.mem.replace('MB', '').split('/');
          memUsed = parseInt(parts[0]) || 0;
          memTotal = parseInt(parts[1]) || 0;
        }

        const parseSpeedToBytes = (speedStr) => {
          if (!speedStr || typeof speedStr !== 'string') return 0;
          const match = speedStr.trim().match(/^([0-9.]+)\s*([A-Za-z/]+)$/);
          if (!match) return 0;
          const value = parseFloat(match[1]);
          const unit = match[2].toLowerCase();
          if (unit.startsWith('g')) return value * 1024 * 1024 * 1024;
          if (unit.startsWith('m')) return value * 1024 * 1024;
          if (unit.startsWith('k')) return value * 1024;
          return value;
        };

        ServerMetricsHistory.create({
          server_id: server.id,
          cpu_usage: parseFloat(frontendMetrics.cpu_usage) || 0,
          cpu_load: frontendMetrics.load || '',
          cpu_cores: frontendMetrics.physical_cores || frontendMetrics.cores || 1,
          cpu_threads: frontendMetrics.logical_cores || frontendMetrics.cores || 1,
          cpu_temp: resolveCpuTemperature(frontendMetrics),
          cpu_power: parseFloat(frontendMetrics.cpu_power) || parseFloat(frontendMetrics.cpu_power_w) || 0,
          mem_used: memUsed,
          mem_total: memTotal,
          mem_usage: frontendMetrics.mem_percent || 0,
          disk_used: frontendMetrics.disk_used || '',
          disk_total: frontendMetrics.disk_total || '',
          disk_usage: frontendMetrics.disk_percent || 0,
          docker_installed: frontendMetrics.docker?.installed ? 1 : 0,
          docker_running: frontendMetrics.docker?.running || 0,
          docker_stopped: frontendMetrics.docker?.stopped || 0,
          gpu_usage: parseFloat(frontendMetrics.gpu_usage) || 0,
          gpu_mem_used: frontendMetrics.gpu_mem_used || 0,
          gpu_mem_total: hostInfo.gpu_mem_total || 0,
          gpu_power: parseFloat(frontendMetrics.gpu_power) || 0,
          gpu_temp: parseFloat(frontendMetrics.gpu_temp) || 0,
          platform: frontendMetrics.platform || '',
          net_rx: parseSpeedToBytes(frontendMetrics.network?.rx_speed),
          net_tx: parseSpeedToBytes(frontendMetrics.network?.tx_speed),
        });

        try {
          serverStorage.updateStatus(server.id, {
            status: 'online',
            last_check_status: 'success',
            cached_info: frontendMetrics,
          });
        } catch (e) {
          // Ignore
        }

        collected++;
      }

      // 输出统计信息
      if (collected > 0 || skippedStale > 0 || skippedDuplicate > 0) {
        const stats = [];
        if (collected > 0) stats.push(`采集 ${collected} 台`);
        if (skippedStale > 0) stats.push(`跳过陈旧 ${skippedStale} 台`);
        if (skippedDuplicate > 0) stats.push(`跳过重复 ${skippedDuplicate} 台`);

        if (this.debug || skippedStale > 0 || skippedDuplicate > 0) {
          this.log(`历史指标采集: ${stats.join(', ')}`);
        }
      }
    } catch (error) {
      console.error('[AgentService] 历史指标采集失败:', error.message);
    }
  }

  /**
   * 处理 Agent 连接
   * @param {Object} socket - Socket.IO 连接
   */
  handleAgentConnection(socket) {
    let serverId = null;
    let authenticated = false;

    this.log(`Agent 连接中: ${socket.id}`);

    const authTimeout = setTimeout(() => {
      if (!authenticated) {
        const msg = `Agent 认证超时: ${socket.id}`;
        console.warn(`[AgentService] ${msg}`);
        this.log(msg);
        socket.emit(Events.DASHBOARD_AUTH_FAIL, { reason: 'Authentication timeout' });
        socket.disconnect();
      }
    }, 10000);

    // 1. 处理认证请求
    socket.on(Events.AGENT_CONNECT, data => {
      clearTimeout(authTimeout);

      // 验证密钥
      if (!data || data.key !== this.globalAgentKey) {
        console.warn('[AgentService] Agent 认证失败: 无效密钥');
        socket.emit(Events.DASHBOARD_AUTH_FAIL, { reason: 'Invalid key' });
        socket.disconnect();
        return;
      }

      // 解析 server_id 和 hostname
      const requestedId = data.server_id;
      const hostname = data.hostname;

      if (!requestedId && !hostname) {
        socket.emit(Events.DASHBOARD_AUTH_FAIL, { reason: 'Missing server_id or hostname' });
        socket.disconnect();
        return;
      }

      // 智能匹配主机 ID
      serverId = this.resolveServerId(requestedId, hostname);

      if (!serverId) {
        console.warn(
          `[AgentService] Agent 认证失败: 无法匹配主机 (id=${requestedId}, hostname=${hostname})`
        );
        socket.emit(Events.DASHBOARD_AUTH_FAIL, {
          reason: 'Server not found in dashboard. Please add the host first.',
          requested_id: requestedId,
          hostname: hostname,
        });
        socket.disconnect();
        return;
      }

      // 检查是否有旧连接，静默断开它 (不触发离线状态)
      const oldSocket = this.connections.get(serverId);
      let isReconnect = false;
      if (oldSocket) {
        if (oldSocket.id !== socket.id) {
          this.log(`替换旧连接: ${serverId}`);
          oldSocket._isReplaced = true; // 标记为被替换，避免触发离线状态
          oldSocket.disconnect();
          isReconnect = true;
        } else {
          // 同一个 socket 重复认证，忽略日志
          return;
        }
      }

      // 注册新连接
      authenticated = true;
      socket._connectedAt = Date.now();
      this.connections.set(serverId, socket);
      this.startHeartbeat(serverId);

      // 更新数据库状态
      this.updateServerStatus(serverId, 'online');

      // 发送认证成功 (包含解析后的实际 serverId)
      socket.emit(Events.DASHBOARD_AUTH_OK, {
        server_time: Date.now(),
        heartbeat_interval: this.heartbeatTimeout / 2,
        resolved_id: serverId, // 告知 Agent 实际使用的 ID
      });

      // 触发上线通知
      this.triggerOnlineAlert(serverId);

      // 广播上线状态给前端
      this.broadcastServerStatus(serverId, 'online');

      // 仅在非重连时打印上线日志
      if (!isReconnect) {
        const msg = `Agent 上线: ${serverId}`;
        console.log(`[AgentService] ${msg}`);
        this.log(msg);
      } else {
        this.log(`Agent 重连: ${serverId}`);
      }

      // 自动请求主机信息 (延迟 2 秒，确保 Agent 已准备好)
      setTimeout(() => {
        if (this.connections.has(serverId)) {
          this.requestHostInfo(serverId);
          this.log(`已自动请求主机信息: ${serverId}`);
        }
      }, 2000);
    });

    // 2. 接收主机硬件信息
    socket.on(Events.AGENT_HOST_INFO, hostInfo => {
      if (!authenticated) return;

      const safeHostInfo = sanitizeHostInfo(hostInfo);

      this.hostInfoCache.set(serverId, {
        ...safeHostInfo,
        received_at: Date.now(),
      });

      try {
        serverStorage.updateStatus(serverId, {
          status: 'online',
          last_check_status: 'success',
          cached_info: safeHostInfo,
        });

        // 自动查询 IP 归属地
        const server = serverStorage.getById(serverId);
        if (server && (!server.country || server.country === 'auto') && safeHostInfo.ip) {
          this.lookupCountryByIP(serverId, safeHostInfo.ip);
        }
      } catch (err) {
        // Ignore
      }

      this.log(
        `收到主机信息: ${serverId} (${hostInfo.platform} ${hostInfo.platform_version}), Cores: ${hostInfo.cores}, GPU: ${JSON.stringify(hostInfo.gpu)}, GPU Mem Total: ${hostInfo.gpu_mem_total}`
      );
    });

    // 3. 接收实时状态
    socket.on(Events.AGENT_STATE, state => {
      if (!authenticated) {
        console.warn('[AgentService] 收到未认证 Agent 的状态数据，忽略');
        return;
      }

      // 验证数据
      if (!validateHostState(state)) {
        console.warn(
          `[AgentService] 无效状态数据: ${serverId}`,
          JSON.stringify(state).substring(0, 200)
        );
        return;
      }

      // 存储状态
      const receivedAt = Date.now();
      const timestamp = this.resolveMetricTimestamp(serverId, state, receivedAt);
      this.stateCache.set(serverId, {
        state,
        timestamp,
        receivedAt,
      });

      // 重置心跳 (高频操作，不打印日志)
      this.resetHeartbeat(serverId);

      // 转换为前端格式并广播
      const hostInfo = this.hostInfoCache.get(serverId) || {};

      // 如果 hostInfo 缺少关键静态信息（如核心数或 GPU 型号），主动请求 Agent 重新上报
      if (!hostInfo.cores && !hostInfo._requestedAt) {
        // 标记已请求，避免重复请求
        this.hostInfoCache.set(serverId, { ...hostInfo, _requestedAt: Date.now() });
        this.requestHostInfo(serverId);
        this.log(`主机信息缺失，已请求 Agent 重新上报: ${serverId}`);
      }

      const frontendData = stateToFrontendFormat(state, hostInfo);

      // 追加到内存中的高精监控数据滚动缓存
      this.appendHighPrecisionMetric(serverId, frontendData, timestamp);

      this.broadcastMetrics(serverId, frontendData, timestamp);

      // 同时更新兼容缓存
      this.legacyMetrics.set(serverId, frontendData);
      this.legacyStatus.set(serverId, {
        lastSeen: receivedAt,
        connected: true,
        version: hostInfo.agent_version || 'socket.io',
      });
    });

    // 4. 接收任务结果
    socket.on(Events.AGENT_TASK_RESULT, result => {
      if (!authenticated) return;
      this.log(`任务结果: ${serverId} -> ${result.id} (${result.successful ? '成功' : '失败'})`);
      this.finishTaskRecord(result.id, {
        id: result.id,
        type: result.type,
        successful: !!result.successful,
        data: result.data,
        delay: result.delay,
      });
    });

    // 5. 接收长任务进度（Agent 端主动推送，轮询仅作为兜底）
    socket.on(Events.AGENT_TASK_PROGRESS, progress => {
      if (!authenticated) return;
      const taskId = progress?.task_id || progress?.taskId || progress?.id;
      if (!taskId) return;
      this.updateTaskProgress(taskId, progress);
    });

    // 6. 接收 PTY 输出数据流
    socket.on(Events.AGENT_PTY_DATA, data => {
      if (!authenticated) return;
      // 通过内部 EventEmitter 分发数据，供 SSHService 等订阅
      this.emit(`pty:${data.id}`, data.data);

      // 同时也可以通过 socket.io 广播给感兴趣的前端（如果有直接订阅的话）
      if (this.io) {
        this.io.emit(`pty:${data.id}`, data.data);
      }
    });

    // 5. 断开连接
    socket.on('disconnect', reason => {
      if (serverId) {
        const msg = `Agent 离线: ${serverId} (${reason})`;
        this.log(msg);
        // 如果是被新连接替换，不更新离线状态
        if (socket._isReplaced) {
          this.log(`旧连接已被替换: ${serverId}`);
          return;
        }
        if (socket._offlineHandled) {
          this.log(`离线状态已处理: ${serverId}`);
          return;
        }

        console.log(`[AgentService] ${msg}`);
        this.connections.delete(serverId);
        this.stopHeartbeat(serverId);
        this.markServerOffline(serverId);
        this.failActiveTasksByServer(serverId, `Agent 已离线: ${reason}`);
        this.updateServerStatus(serverId, 'offline');
        this.broadcastServerStatus(serverId, 'offline');
        this.triggerOfflineAlert(serverId); // Ensure offline alert is triggered
      }
    });

    // 错误处理
    socket.on('error', err => {
      console.error(`[AgentService] Socket 错误 (${serverId || socket.id}):`, err.message);
    });
  }

  /**
   * 根据 IP 获取国家代码并存入数据库
   */
  async lookupCountryByIP(serverId, ip) {
    if (!ip) return;
    try {
      const countryCode = lookupCountryByIp(ip);
      if (!countryCode) return;

      this.log(`IP 地理位置查询成功: ${ip} -> ${countryCode}`);

      // 更新数据库中自动检测解析出来的国家代码
      const server = serverStorage.getById(serverId);
      if (server && (!server.country || server.country === 'auto')) {
        serverStorage.update(serverId, { resolved_country: countryCode });
        // 广播最新的已解析国家代码给前端，使其立即刷新渲染
        this.broadcastServerStatus(serverId, 'online', { resolved_country: countryCode });
      }
    } catch (err) {
      console.error(`[AgentService] 查询 IP 地理位置失败 (${ip}):`, err.message);
    }
  }

  /**
   * 处理前端连接
   * @param {Object} socket - Socket.IO 连接
   */
  handleFrontendConnection(socket) {
    // 自动加入广播房间
    socket.join('metrics_room');
    this.log(`前端连接: ${socket.id}`);

    // 发送当前所有在线主机的最新指标
    const initialData = [];
    const now = Date.now();
    for (const [serverId, cached] of this.stateCache.entries()) {
      if (!this.isOnline(serverId)) continue;

      const dataAge = now - (cached.receivedAt || cached.timestamp || now);
      if (dataAge > this.heartbeatTimeout * 2) continue;

      const hostInfo = this.hostInfoCache.get(serverId) || {};
      initialData.push({
        serverId,
        metrics: stateToFrontendFormat(cached.state, hostInfo),
        timestamp: cached.timestamp,
        receivedAt: cached.receivedAt || cached.timestamp,
      });
    }

    if (initialData.length > 0) {
      socket.emit(Events.METRICS_BATCH, initialData);
    }

    // 发送完整状态快照，包含离线主机，确保前端重连后不会保留陈旧在线状态。
    socket.emit(Events.SERVER_LIST, this.getServerStatusSnapshot());

    socket.on('disconnect', () => {
      this.log(`前端断开: ${socket.id}`);
    });
  }

  // ==================== 心跳管理 ====================

  /**
   * 启动心跳超时检测
   */
  startHeartbeat(serverId) {
    this.stopHeartbeat(serverId);
    this.heartbeatTimers.set(
      serverId,
      setTimeout(() => {
        console.warn(`[AgentService] 心跳超时: ${serverId}`);
        const socket = this.connections.get(serverId);
        if (socket) {
          socket._offlineHandled = true;
          socket.disconnect();
        }
        this.handleAgentTimeout(serverId);
      }, this.heartbeatTimeout)
    );
  }

  /**
   * 重置心跳计时器
   */
  resetHeartbeat(serverId) {
    this.startHeartbeat(serverId);
  }

  /**
   * 停止心跳检测
   */
  stopHeartbeat(serverId) {
    const timer = this.heartbeatTimers.get(serverId);
    if (timer) {
      clearTimeout(timer);
      this.heartbeatTimers.delete(serverId);
    }
  }

  /**
   * 清理离线主机的实时指标缓存，避免旧数据在前端重连后再次把主机标为在线。
   */
  markServerOffline(serverId) {
    const cached = this.stateCache.get(serverId);
    const legacy = this.legacyStatus.get(serverId) || {};
    const lastSeen = legacy.lastSeen || cached?.receivedAt || cached?.timestamp || Date.now();

    this.stateCache.delete(serverId);
    this.legacyMetrics.delete(serverId);
    this.legacyStatus.set(serverId, {
      ...legacy,
      connected: false,
      lastSeen,
    });
  }

  /**
   * 处理 Agent 超时
   */
  handleAgentTimeout(serverId) {
    this.connections.delete(serverId);
    this.markServerOffline(serverId);
    this.failActiveTasksByServer(serverId, 'Agent 心跳超时');
    this.updateServerStatus(serverId, 'offline');
    this.broadcastServerStatus(serverId, 'offline');

    // 触发离线告警
    this.triggerOfflineAlert(serverId);
  }

  /**
   * 触发主机离线告警
   */
  triggerOfflineAlert(serverId) {
    try {
      const server = serverStorage.getById(serverId);
      if (!server) return;

      const notificationService = require('../notification-api/service');
      const hostInfo = this.hostInfoCache.get(serverId);

      notificationService.trigger('server', 'offline', {
        serverId: serverId,
        serverName: server.name,
        host: server.host,
        lastSeen: hostInfo?.received_at || Date.now(),
        hostname: hostInfo?.hostname
      });

      logger.warn(`[主机告警] ${server.name} (${server.host}) 离线`);
    } catch (error) {
      logger.error(`触发离线告警失败: ${error.message}`);
    }
  }

  /**
   * 触发主机上线通知
   */
  triggerOnlineAlert(serverId) {
    try {
      const server = serverStorage.getById(serverId);
      if (!server) return;

      const notificationService = require('../notification-api/service');
      const hostInfo = this.hostInfoCache.get(serverId);

      // 检查启动静默期 (防止重启后通知风暴)
      if (Date.now() - this.startupTime < 60000) {
        this.log(`[主机通知] 静默期内跳过上线通知: ${server.name}`);
        return;
      }

      notificationService.trigger('server', 'online', {
        serverId: serverId,
        serverName: server.name,
        host: server.host,
        hostname: hostInfo?.hostname
      });

      logger.info(`[主机通知] ${server.name} (${server.host}) 已上线`);
    } catch (error) {
      logger.error(`触发上线通知失败: ${error.message}`);
    }
  }

  // ==================== 广播方法 ====================

  /**
   * 广播单个主机的指标更新
   */
  broadcastMetrics(serverId, metrics, timestamp = Date.now()) {
    if (!this.io) return;

    this.io.of('/metrics').to('metrics_room').emit(Events.METRICS_UPDATE, {
      serverId,
      metrics,
      timestamp,
    });
  }

  /**
   * 广播主机状态变更
   */
  broadcastServerStatus(serverId, status, additionalData = {}) {
    if (!this.io) return;

    this.io.of('/metrics').to('metrics_room').emit(Events.SERVER_STATUS, {
      serverId,
      status,
      timestamp: Date.now(),
      ...additionalData
    });
  }

  // ==================== 主机匹配 ====================

  /**
   * 智能解析 Agent 提供的标识符，匹配到数据库中的主机 ID
   * 匹配优先级: 精确 ID -> 名称匹配 -> 主机地址匹配
   * @param {string} requestedId - Agent 请求的 ID
   * @param {string} hostname - Agent 的 hostname
   * @returns {string|null} 匹配到的主机 ID，未匹配返回 null
   */
  resolveServerId(requestedId, hostname) {
    try {
      const servers = serverStorage.getAll();

      // 1. 精确 ID 匹配
      if (requestedId) {
        const exactMatch = servers.find(s => s.id === requestedId);
        if (exactMatch) {
          return exactMatch.id;
        }
      }

      // 2. 按名称匹配 (requestedId 或 hostname 与主机名称匹配)
      const nameToMatch = requestedId || hostname;
      if (nameToMatch) {
        // 精确名称匹配
        const nameMatch = servers.find(
          s => s.name === nameToMatch || s.name?.toLowerCase() === nameToMatch.toLowerCase()
        );
        if (nameMatch) {
          this.log(`按名称匹配: ${nameToMatch} -> ${nameMatch.id}`);
          return nameMatch.id;
        }
      }

      // 3. 按主机地址匹配 (hostname 与 host 字段匹配)
      if (hostname) {
        const hostMatch = servers.find(
          s => s.host === hostname || s.host?.toLowerCase() === hostname.toLowerCase()
        );
        if (hostMatch) {
          this.log(`按 host 匹配: ${hostname} -> ${hostMatch.id}`);
          return hostMatch.id;
        }
      }

      // 4. 部分名称匹配 (模糊匹配)
      if (nameToMatch) {
        const partialMatch = servers.find(
          s =>
            s.name?.toLowerCase().includes(nameToMatch.toLowerCase()) ||
            nameToMatch.toLowerCase().includes(s.name?.toLowerCase())
        );
        if (partialMatch) {
          this.log(`模糊名称匹配: ${nameToMatch} -> ${partialMatch.id}`);
          return partialMatch.id;
        }
      }

      return null;
    } catch (error) {
      console.error('[AgentService] 主机匹配失败:', error.message);
      return null;
    }
  }

  // ==================== 任务下发 ====================

  isTaskFinalState(state) {
    return ['success', 'failed', 'timeout', 'cancelled'].includes(state);
  }

  snapshotTask(task) {
    if (!task) return null;
    return {
      taskId: task.id,
      id: task.id,
      serverId: task.serverId,
      domain: task.domain,
      action: task.action,
      type: task.type,
      state: task.state,
      progress: task.progress,
      step: task.step,
      message: task.message,
      detail: task.detail,
      result: task.result,
      error: task.error,
      timeoutMs: task.timeoutMs,
      createdAt: task.createdAt,
      updatedAt: task.updatedAt,
      startedAt: task.startedAt,
      finishedAt: task.finishedAt,
    };
  }

  emitTaskUpdate(task) {
    const snapshot = this.snapshotTask(task);
    if (!snapshot) return;
    this.emit('task:update', snapshot);
  }

  createTaskRecord(serverId, task, options = {}) {
    const now = Date.now();
    const taskId = task.id || crypto.randomUUID();
    const timeoutMs = options.timeoutMs || 60000;

    const record = {
      id: taskId,
      serverId,
      domain: options.domain || 'system',
      action: options.action || '',
      type: task.type,
      state: 'running',
      progress: 0,
      step: 'queued',
      message: '任务已下发',
      detail: '',
      result: null,
      error: null,
      timeoutMs,
      createdAt: now,
      updatedAt: now,
      startedAt: now,
      finishedAt: null,
      _timeoutTimer: null,
    };

    this.taskRegistry.set(taskId, record);
    return record;
  }

  getTask(taskId) {
    return this.snapshotTask(this.taskRegistry.get(taskId));
  }

  getRecentTasks(serverId = '', limit = 100) {
    let tasks = Array.from(this.taskRegistry.values());
    if (serverId) {
      tasks = tasks.filter(item => item.serverId === serverId);
    }
    tasks.sort((a, b) => b.createdAt - a.createdAt);
    return tasks.slice(0, limit).map(item => this.snapshotTask(item));
  }

  cleanupTaskRegistry() {
    const now = Date.now();
    for (const [taskId, task] of this.taskRegistry.entries()) {
      if (!this.isTaskFinalState(task.state)) continue;
      const finishedAt = task.finishedAt || task.updatedAt || task.createdAt;
      if (now - finishedAt > this.taskRetentionMs) {
        this.stopTaskProgressPolling(taskId);
        if (task._timeoutTimer) {
          clearTimeout(task._timeoutTimer);
        }
        this.taskRegistry.delete(taskId);
        this.taskResolvers.delete(taskId);
      }
    }
  }

  updateTaskProgress(taskId, payload) {
    const task = this.taskRegistry.get(taskId);
    if (!task || this.isTaskFinalState(task.state)) return;

    let progressData = payload;
    if (typeof progressData === 'string') {
      try {
        progressData = JSON.parse(progressData);
      } catch (e) {
        return;
      }
    }
    if (!progressData || typeof progressData !== 'object') return;

    if (typeof progressData.percentage === 'number') {
      const bounded = Math.max(0, Math.min(100, Math.round(progressData.percentage)));
      task.progress = bounded;
    }
    if (typeof progressData.name === 'string' && progressData.name.trim()) {
      task.step = progressData.name.trim();
    }
    if (typeof progressData.message === 'string' && progressData.message.trim()) {
      task.message = progressData.message.trim();
    }
    if (typeof progressData.detail_msg === 'string') {
      task.detail = progressData.detail_msg;
    }
    if (progressData.is_done === true) {
      task.progress = 100;
    }

    task.updatedAt = Date.now();
    this.emitTaskUpdate(task);
  }

  stopTaskProgressPolling(taskId) {
    const timer = this.taskPollers.get(taskId);
    if (timer) {
      clearInterval(timer);
      this.taskPollers.delete(taskId);
    }
  }

  startTaskProgressPolling(taskId, serverId, intervalMs = 1500) {
    if (this.taskPollers.has(taskId)) return;

    const timer = setInterval(async () => {
      const task = this.taskRegistry.get(taskId);
      if (!task || this.isTaskFinalState(task.state)) {
        this.stopTaskProgressPolling(taskId);
        return;
      }
      if (!this.isOnline(serverId)) {
        return;
      }

      try {
        const result = await this._sendTaskAndWaitLegacy(
          serverId,
          {
            type: TaskTypes.DOCKER_TASK_PROGRESS,
            data: JSON.stringify({ task_id: taskId }),
            timeout: 10,
          },
          15000
        );

        if (result.successful && result.data) {
          this.updateTaskProgress(taskId, result.data);
        }
      } catch (error) {
        // 进度查询失败不直接中断主任务
      }
    }, Math.max(1000, intervalMs));

    if (typeof timer.unref === 'function') {
      timer.unref();
    }
    this.taskPollers.set(taskId, timer);
  }

  finishTaskRecord(taskId, result) {
    const task = this.taskRegistry.get(taskId);
    if (!task) return;

    if (task._timeoutTimer) {
      clearTimeout(task._timeoutTimer);
      task._timeoutTimer = null;
    }
    this.stopTaskProgressPolling(taskId);

    task.updatedAt = Date.now();
    task.finishedAt = task.updatedAt;

    if (result && result.successful) {
      task.state = 'success';
      task.progress = 100;
      task.message = '任务执行成功';
      task.result = result.data || '';
      task.error = null;
    } else {
      task.state = 'failed';
      task.message = '任务执行失败';
      task.error = result?.data || '未知错误';
      task.result = null;
    }

    this.emitTaskUpdate(task);

    const resolver = this.taskResolvers.get(taskId);
    if (resolver) {
      this.taskResolvers.delete(taskId);
      resolver.resolve(result);
    }
  }

  failActiveTasksByServer(serverId, reason = 'Agent 连接中断') {
    for (const [taskId, task] of this.taskRegistry.entries()) {
      if (task.serverId !== serverId || this.isTaskFinalState(task.state)) continue;

      if (task._timeoutTimer) {
        clearTimeout(task._timeoutTimer);
        task._timeoutTimer = null;
      }
      this.stopTaskProgressPolling(taskId);

      task.state = 'failed';
      task.error = reason;
      task.message = reason;
      task.updatedAt = Date.now();
      task.finishedAt = task.updatedAt;
      this.emitTaskUpdate(task);

      const resolver = this.taskResolvers.get(taskId);
      if (resolver) {
        this.taskResolvers.delete(taskId);
        resolver.reject(new Error(reason));
      }
    }
  }

  // 兼容内部短查询的旧实现（例如任务进度轮询）
  _sendTaskAndWaitLegacy(serverId, task, timeout = 60000) {
    return new Promise((resolve, reject) => {
      const taskId = task.id || crypto.randomUUID();
      const socket = this.connections.get(serverId);

      if (!socket) {
        return reject(new Error('主机不在线'));
      }

      const timer = setTimeout(() => {
        socket.off(Events.AGENT_TASK_RESULT, resultHandler);
        reject(new Error('任务执行超时'));
      }, timeout);

      const resultHandler = result => {
        if (result.id === taskId) {
          clearTimeout(timer);
          socket.off(Events.AGENT_TASK_RESULT, resultHandler);
          resolve(result);
        }
      };

      socket.on(Events.AGENT_TASK_RESULT, resultHandler);
      socket.emit(Events.DASHBOARD_TASK, {
        id: taskId,
        type: task.type,
        data: task.data,
        timeout: task.timeout || 0,
      });
    });
  }

  submitTask(serverId, task, options = {}) {
    const socket = this.connections.get(serverId);
    if (!socket) {
      throw new Error('主机不在线');
    }

    const timeoutMs = options.timeoutMs || 60000;
    const record = this.createTaskRecord(serverId, task, {
      timeoutMs,
      domain: options.domain,
      action: options.action,
    });

    record._timeoutTimer = setTimeout(() => {
      if (this.isTaskFinalState(record.state)) return;

      record.state = 'timeout';
      record.error = '任务执行超时';
      record.message = '任务执行超时';
      record.updatedAt = Date.now();
      record.finishedAt = record.updatedAt;
      this.stopTaskProgressPolling(record.id);
      this.emitTaskUpdate(record);

      const resolver = this.taskResolvers.get(record.id);
      if (resolver) {
        this.taskResolvers.delete(record.id);
        resolver.reject(new Error('任务执行超时'));
      }
    }, timeoutMs);

    if (typeof record._timeoutTimer.unref === 'function') {
      record._timeoutTimer.unref();
    }

    socket.emit(Events.DASHBOARD_TASK, {
      id: record.id,
      type: task.type,
      data: task.data,
      timeout: task.timeout || 0,
    });

    this.emitTaskUpdate(record);
    this.log(`任务已下发: ${serverId} -> ${task.type} (id: ${record.id})`);

    if (options.trackProgress) {
      this.startTaskProgressPolling(record.id, serverId, options.progressIntervalMs || 1500);
    }

    if (options.waitForResult === false) {
      return record.id;
    }

    return new Promise((resolve, reject) => {
      this.taskResolvers.set(record.id, { resolve, reject });
    });
  }

  /**
   * 向 Agent 下发任务
   * @param {string} serverId - 目标主机 ID
   * @param {Object} task - 任务对象
   * @returns {string|false} 任务 ID
   */
  sendTask(serverId, task) {
    // PTY 交互与非标准任务类型不进入任务注册表，避免高频输入污染任务中心
    if (task.type === TaskTypes.PTY_START || typeof task.type !== 'number') {
      const socket = this.connections.get(serverId);
      if (!socket) {
        return false;
      }
      socket.emit(Events.DASHBOARD_TASK, {
        id: task.id || crypto.randomUUID(),
        type: task.type,
        data: task.data,
        timeout: task.timeout || 0,
      });
      return task.id || true;
    }

    try {
      return this.submitTask(serverId, task, {
        waitForResult: false,
        timeoutMs: Math.max(30000, ((task.timeout || 60) + 5) * 1000),
      });
    } catch (error) {
      console.warn(`[AgentService] 无法下发任务: ${serverId} ${error.message}`);
      return false;
    }
  }

  /**
   * 请求 Agent 上报主机信息
   */
  requestHostInfo(serverId) {
    return this.sendTask(serverId, {
      type: TaskTypes.REPORT_HOST_INFO,
      data: '',
    });
  }

  /**
   * 检查主机是否在线
   * @param {string} serverId
   * @returns {boolean}
   */
  isOnline(serverId) {
    return this.connections.has(serverId);
  }

  /**
   * 获取主机硬件信息
   * @param {string} serverId
   * @returns {Object|null}
   */
  getHostInfo(serverId) {
    return this.hostInfoCache.get(serverId) || null;
  }

  /**
   * 发送任务并等待结果
   * @param {string} serverId
   * @param {Object} task
   * @param {number} timeout - 超时时间 (毫秒)
   * @returns {Promise<Object>}
   */
  sendTaskAndWait(serverId, task, timeout = 60000) {
    return this.submitTask(serverId, task, {
      waitForResult: true,
      timeoutMs: timeout,
    });
  }

  sendInternalTaskAndWait(serverId, task, timeout = 60000) {
    return this._sendTaskAndWaitLegacy(serverId, task, timeout);
  }

  /**
   * 在远程主机执行命令
   * @param {string} serverId - 主机 ID
   * @param {string} command - 要执行的命令
   * @param {number} timeout - 超时时间 (秒)，默认 60
   * @returns {Promise<{success: boolean, output: string}>}
   */
  async executeCommand(serverId, command, timeout = 60) {
    if (!this.isOnline(serverId)) {
      throw new Error('主机不在线');
    }

    if (!command || typeof command !== 'string') {
      throw new Error('命令不能为空');
    }

    const result = await this.sendTaskAndWait(
      serverId,
      {
        type: TaskTypes.COMMAND,
        data: command,
        timeout: timeout,
      },
      (timeout + 5) * 1000 // 给一点额外时间等待 Agent 响应
    );

    return {
      success: result.successful,
      output: result.data || '',
      delay: result.delay || 0,
    };
  }


  /**
   * 获取所有在线 Agent 列表
   */
  getOnlineAgents() {
    return Array.from(this.connections.keys());
  }

  /**
   * 获取连接统计
   */
  getConnectionStats() {
    return {
      online: this.connections.size,
      cached: this.stateCache.size,
      frontendClients: this.io?.of('/metrics').sockets.size || 0,
    };
  }

  // ==================== 数据库状态同步 ====================

  /**
   * 更新数据库中的主机状态
   */
  updateServerStatus(serverId, status) {
    try {
      serverStorage.updateStatus(serverId, {
        status: status,
        last_check_time: new Date().toISOString(),
        last_check_status: status === 'online' ? 'success' : 'offline',
      });
    } catch (e) {
      // 主机可能不存在于数据库
    }
  }

  // ==================== 兼容旧 HTTP 推送 (过渡期) ====================

  /**
   * 处理 HTTP POST 推送的指标数据 (兼容旧 Agent)
   * @deprecated 将在未来版本移除
   */
  processMetrics(serverId, metrics) {
    const timestamp = Date.now();

    // 解析 CPU
    const cpuUsage = parseFloat(metrics.cpu) || 0;

    // 解析内存
    let memUsed = 0,
      memTotal = 0;
    if (metrics.mem) {
      const memMatch = metrics.mem.match(/(\d+)\/(\d+)/);
      if (memMatch) {
        memUsed = parseInt(memMatch[1]);
        memTotal = parseInt(memMatch[2]);
      }
    }

    // 解析磁盘
    let diskUsed = '',
      diskTotal = '',
      diskUsage = '';
    if (metrics.disk) {
      const diskMatch = metrics.disk.match(/([^/]+)\/([^\s]+)\s*\(?([.\d]+%?)?\)?/);
      if (diskMatch) {
        diskUsed = diskMatch[1];
        diskTotal = diskMatch[2];
        diskUsage = diskMatch[3] || '';
      }
    }

    const processedMetrics = {
      timestamp,
      cpu: cpuUsage,
      cpu_usage: `${cpuUsage}%`,
      mem: `${memUsed}/${memTotal}MB`,
      mem_usage: `${memUsed}/${memTotal}MB`,
      disk: metrics.disk,
      disk_used: diskUsed,
      disk_total: diskTotal,
      disk_usage: metrics.disk,
      load: metrics.load || '0 0 0',
      cores: parseInt(metrics.cores) || 1,
      physical_cores: parseInt(metrics.physical_cores) || parseInt(metrics.cores) || 1,
      logical_cores: parseInt(metrics.logical_cores) || parseInt(metrics.cores) || 1,
      cpu_temp: resolveCpuTemperature(metrics),
      cpu_power: parseFloat(metrics.cpu_power) || parseFloat(metrics.cpu_power_w) || 0,
      ip: metrics.ip || '',
      network: normalizeNetworkMetrics({
        rx_speed: metrics.rx_speed || '0 B/s',
        tx_speed: metrics.tx_speed || '0 B/s',
        rx_total: metrics.rx_total || '0 B',
        tx_total: metrics.tx_total || '0 B',
        connections: parseInt(metrics.connections) || 0,
      }),
      docker: {
        installed: metrics.docker_installed === true || metrics.docker_installed === 'true',
        running: parseInt(metrics.docker_running) || 0,
        stopped: parseInt(metrics.docker_stopped) || 0,
        containers: Array.isArray(metrics.containers) ? metrics.containers : [],
      },
    };

    // 存储到兼容缓存
    this.legacyMetrics.set(serverId, processedMetrics);
    this.legacyStatus.set(serverId, {
      lastSeen: timestamp,
      connected: true,
      version: metrics.agent_version || 'http-legacy',
    });

    // 检查资源告警
    this.checkResourceAlerts(serverId, processedMetrics);

    // 广播给前端
    this.broadcastMetrics(serverId, processedMetrics);

    console.log(`[AgentService] HTTP 推送: ${serverId} -> CPU: ${processedMetrics.cpu_usage}`);

    return processedMetrics;
  }

  /**
   * 检查资源告警
   */
  checkResourceAlerts(serverId, metrics) {
    try {
      const server = serverStorage.getById(serverId);
      if (!server) return;

      const notificationService = require('../notification-api/service');

      if (!this.activeResourceAlerts.has(serverId)) {
        this.activeResourceAlerts.set(serverId, new Set());
      }
      const activeAlerts = this.activeResourceAlerts.get(serverId);

      // 1. CPU 告警阈值 (80%)
      const cpuThreshold = 80;
      const isCpuHigh = metrics.cpu > cpuThreshold;
      if (isCpuHigh) {
        if (!activeAlerts.has('cpu')) {
          activeAlerts.add('cpu');
          notificationService.trigger('server', 'cpu_high', {
            serverId: serverId,
            serverName: server.name,
            host: server.host,
            cpu_usage: metrics.cpu,
            threshold: cpuThreshold
          });
          logger.warn(`[资源告警] ${server.name} CPU 使用率: ${metrics.cpu}%`);
        }
      } else {
        if (activeAlerts.has('cpu')) {
          activeAlerts.delete('cpu');
          notificationService.trigger('server', 'cpu_normal', {
            serverId: serverId,
            serverName: server.name,
            host: server.host,
            cpu_usage: metrics.cpu,
            threshold: cpuThreshold
          });
          logger.info(`[资源恢复] ${server.name} CPU 使用率恢复正常: ${metrics.cpu}%`);
        }
      }

      // 2. 内存告警阈值 (85%)
      if (metrics.mem) {
        const memMatch = metrics.mem.match(/(\d+)\/(\d+)/);
        if (memMatch) {
          const memUsed = parseInt(memMatch[1]);
          const memTotal = parseInt(memMatch[2]);
          const memPercent = (memUsed / memTotal) * 100;
          const memThreshold = 85;
          const isMemHigh = memPercent > memThreshold;

          if (isMemHigh) {
            if (!activeAlerts.has('memory')) {
              activeAlerts.add('memory');
              notificationService.trigger('server', 'memory_high', {
                serverId: serverId,
                serverName: server.name,
                host: server.host,
                mem_percent: memPercent.toFixed(2),
                mem_used: memUsed,
                mem_total: memTotal,
                threshold: memThreshold
              });
              logger.warn(`[资源告警] ${server.name} 内存使用率: ${memPercent.toFixed(2)}%`);
            }
          } else {
            if (activeAlerts.has('memory')) {
              activeAlerts.delete('memory');
              notificationService.trigger('server', 'memory_normal', {
                serverId: serverId,
                serverName: server.name,
                host: server.host,
                mem_percent: memPercent.toFixed(2),
                mem_used: memUsed,
                mem_total: memTotal,
                threshold: memThreshold
              });
              logger.info(`[资源恢复] ${server.name} 内存使用率恢复正常: ${memPercent.toFixed(2)}%`);
            }
          }
        }
      }

      // 3. 磁盘告警阈值 (90%)
      if (metrics.disk) {
        const diskMatch = metrics.disk.match(/([.\d]+)%/);
        if (diskMatch) {
          const diskPercent = parseFloat(diskMatch[1]);
          const diskThreshold = 90;
          const isDiskHigh = diskPercent > diskThreshold;

          if (isDiskHigh) {
            if (!activeAlerts.has('disk')) {
              activeAlerts.add('disk');
              notificationService.trigger('server', 'disk_high', {
                serverId: serverId,
                serverName: server.name,
                host: server.host,
                disk_usage: metrics.disk,
                disk_percent: diskPercent,
                threshold: diskThreshold
              });
              logger.warn(`[资源告警] ${server.name} 磁盘使用率: ${diskPercent}%`);
            }
          } else {
            if (activeAlerts.has('disk')) {
              activeAlerts.delete('disk');
              notificationService.trigger('server', 'disk_normal', {
                serverId: serverId,
                serverName: server.name,
                host: server.host,
                disk_usage: metrics.disk,
                disk_percent: diskPercent,
                threshold: diskThreshold
              });
              logger.info(`[资源恢复] ${server.name} 磁盘使用率恢复正常: ${diskPercent}%`);
            }
          }
        }
      }
    } catch (error) {
      logger.error(`检查资源告警失败: ${error.message}`);
    }
  }

  // ==================== 安装脚本生成 ====================

  /**
   * 生成新版 Agent 安装脚本 (Go Agent) - 支持无缝升级
   */
  generateInstallScript(serverId, serverUrl) {
    serverUrl = normalizeOriginUrl(serverUrl);
    const agentKey = this.getAgentKey(serverId);
    const downloadVersion = encodeURIComponent(`${packageInfo.version || 'dev'}-${Date.now()}`);
    const $ = '$'; // 用于在模板字符串中输出 $

    // 读取用户设置的自定义下载地址
    let customDownloadUrl = '';
    try {
      const settings = userSettings.loadUserSettings();
      customDownloadUrl = settings.agentDownloadUrl || '';
    } catch (e) {
      console.warn('[AgentService] 读取用户设置失败:', e.message);
    }

    // 如果设置了自定义地址，使用它；否则使用主控端地址
    const binaryBaseUrl = customDownloadUrl
      ? customDownloadUrl.replace(/\/$/, '') // 移除末尾斜杠
      : `${serverUrl}/agent`;

    return `#!/bin/bash
# API Monitor Agent 自动安装/升级脚本 (Rust 版)
# 支持从旧版 Node.js Agent 无缝升级

# 颜色定义
RED='\\033[0;31m'
GREEN='\\033[0;32m'
YELLOW='\\033[1;33m'
CYAN='\\033[0;36m'
NC='\\033[0m'

# 配置信息
SERVER_URL="${serverUrl}"
SERVER_ID="${serverId}"
AGENT_KEY="${agentKey}"
INSTALL_DIR="/opt/api-monitor-agent"
SERVICE_NAME="api-monitor-agent"
BINARY_BASE_URL="${binaryBaseUrl}"

# 检测系统架构
ARCH=${$}(uname -m)
case ${$}ARCH in
    x86_64)
        BINARY_NAME="agent-linux-amd64"
        ;;
    aarch64|arm64)
        BINARY_NAME="agent-linux-arm64"
        ;;
    *)
        echo -e "${$}{RED}错误: 不支持的架构 ${$}ARCH${$}{NC}"
        exit 1
        ;;
esac
BINARY_URL="${$}{BINARY_BASE_URL}/${$}{BINARY_NAME}?v=${downloadVersion}"

# 1. 自动检测权限模式
if [ "${$}EUID" -eq 0 ]; then
    INSTALL_MODE="system"
    echo -e "${$}{CYAN}>>> API Monitor Agent 系统级安装 (root)${$}{NC}"
else
    INSTALL_MODE="user"
    INSTALL_DIR="${$}HOME/.local/share/api-monitor-agent"
    USER_CONFIG_DIR="${$}HOME/.config/api-monitor-agent"
    USER_SERVICE_DIR="${$}HOME/.config/systemd/user"
    mkdir -p "${$}USER_CONFIG_DIR" "${$}USER_SERVICE_DIR"
    echo -e "${$}{CYAN}>>> API Monitor Agent 用户级安装 (无 root)${$}{NC}"
    echo -e "${$}{YELLOW}    提示: 如需系统级安装，请使用 sudo 运行${$}{NC}"
fi

# 2. 检测是否为升级安装
UPGRADE_MODE=false
if [ -f "${$}INSTALL_DIR/agent" ]; then
    UPGRADE_MODE=true
    echo -e "${$}{CYAN}>>> 检测到已安装 Agent，将执行升级...${$}{NC}"
fi

# 3. 停止现有服务
if [ "${$}INSTALL_MODE" = "system" ]; then
    systemctl is-active --quiet ${$}SERVICE_NAME 2>/dev/null && {
        echo -e "${$}{YELLOW}⏹ 停止现有服务...${$}{NC}"
        systemctl stop ${$}SERVICE_NAME
    }
else
    systemctl --user is-active --quiet ${$}SERVICE_NAME 2>/dev/null && {
        echo -e "${$}{YELLOW}⏹ 停止现有服务...${$}{NC}"
        systemctl --user stop ${$}SERVICE_NAME
    }
fi

# 4. 清理旧版文件 (Node.js Agent 残留)
if [ "${$}UPGRADE_MODE" = true ]; then
    echo -e "${$}{YELLOW}🧹 清理旧版 Agent 文件...${$}{NC}"
    rm -f "${$}INSTALL_DIR/agent-bin" 2>/dev/null
    rm -rf "${$}INSTALL_DIR/node_modules" 2>/dev/null
    rm -f "${$}INSTALL_DIR/package.json" "${$}INSTALL_DIR/package-lock.json" 2>/dev/null
    rm -f "${$}INSTALL_DIR/index.js" "${$}INSTALL_DIR/config.js" "${$}INSTALL_DIR/collector.js" 2>/dev/null
fi

# 5. 创建/进入目录
echo "📁 目录: ${$}INSTALL_DIR"
mkdir -p "${$}INSTALL_DIR"
cd "${$}INSTALL_DIR"

# 6. 下载新版二进制文件
echo -e "${$}{YELLOW}📥 下载 Agent 二进制文件 (${$}BINARY_NAME)...${$}{NC}"
curl -L -f -s "${$}BINARY_URL" -o agent.new
if [ ${$}? -ne 0 ]; then
    echo -e "${$}{RED}❌ 错误: 无法从 ${$}BINARY_URL 下载二进制文件。${$}{NC}"
    echo -e "${$}{YELLOW}请确保主控端已完成构建。${$}{NC}"
    if [ "${$}UPGRADE_MODE" = true ] && [ -f "${$}INSTALL_DIR/agent" ]; then
        echo -e "${$}{YELLOW}尝试恢复旧版服务...${$}{NC}"
        systemctl start ${$}SERVICE_NAME
    fi
    exit 1
fi

# 替换二进制文件 (原子操作)
mv agent.new agent
chmod +x agent

# 7. 生成/更新配置文件
echo -e "${$}{YELLOW}📝 生成配置文件...${$}{NC}"
# 始终更新配置文件以确保服务器地址正确（升级到新控制端时需要）
cat > config.json << 'CONFIGEOF'
{
    "serverUrl": "__SERVER_URL__",
    "serverId": "__SERVER_ID__",
    "agentKey": "__AGENT_KEY__",
    "reportInterval": 1500,
    "reconnectDelay": 4000
}
CONFIGEOF
sed -i "s|__SERVER_URL__|${$}SERVER_URL|g" config.json
sed -i "s|__SERVER_ID__|${$}SERVER_ID|g" config.json
sed -i "s|__AGENT_KEY__|${$}AGENT_KEY|g" config.json
echo -e "${$}{CYAN}   配置已更新: ${$}SERVER_URL${$}{NC}"

# 8. 检测 Systemd 可用性 & 配置服务
HAS_SYSTEMD=false
if command -v systemctl >/dev/null 2>&1 && systemctl --version >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    HAS_SYSTEMD=true
fi

if [ "${$}HAS_SYSTEMD" = true ]; then
    echo -e "${$}{YELLOW}⚙️ 配置 systemd 服务...${$}{NC}"
    if [ "${$}INSTALL_MODE" = "system" ]; then
        cat > /etc/systemd/system/${$}SERVICE_NAME.service << SERVICEEOF
[Unit]
Description=API Monitor Agent (Go)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${$}INSTALL_DIR
ExecStart=${$}INSTALL_DIR/agent
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
SERVICEEOF
        systemctl daemon-reload
        systemctl enable ${$}SERVICE_NAME
        systemctl restart ${$}SERVICE_NAME
    else
        cat > "${$}USER_SERVICE_DIR/${$}SERVICE_NAME.service" << SERVICEEOF
[Unit]
Description=API Monitor Agent (User Mode)
After=network-online.target

[Service]
Type=simple
WorkingDirectory=${$}INSTALL_DIR
ExecStart=${$}INSTALL_DIR/agent
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
SERVICEEOF
        # 尝试启用 lingering
        loginctl enable-linger ${$}USER 2>/dev/null || echo -e "${$}{YELLOW}⚠️ lingering 需管理员: loginctl enable-linger ${$}USER${$}{NC}"
        systemctl --user daemon-reload
        systemctl --user enable ${$}SERVICE_NAME
        systemctl --user restart ${$}SERVICE_NAME
    fi
else
    # 8b. 无 Systemd 环境 (如 Colab, Docker)
    echo -e "${$}{YELLOW}⚙️ 无 Systemd 环境，使用后台进程运行...${$}{NC}"
    # 尝试停止旧进程
    pkill -f "${$}INSTALL_DIR/agent" || true
    
    # 后台运行
    nohup "${$}INSTALL_DIR/agent" > "${$}INSTALL_DIR/agent.log" 2>&1 &
    
    # 保存 PID
    echo $! > "${$}INSTALL_DIR/agent.pid"
    echo -e "${$}{CYAN}   PID: $(cat "${$}INSTALL_DIR/agent.pid")${$}{NC}"
fi

# 9. 启动/状态检查
echo -e "${$}{YELLOW}🚀 正在启动...${$}{NC}"
sleep 1

IS_RUNNING=false

if [ "${$}HAS_SYSTEMD" = true ]; then
    if [ "${$}INSTALL_MODE" = "system" ]; then
        SERVICE_STATUS=${$}(systemctl is-active ${$}SERVICE_NAME 2>/dev/null)
    else
        SERVICE_STATUS=${$}(systemctl --user is-active ${$}SERVICE_NAME 2>/dev/null)
    fi
    if [ "${$}SERVICE_STATUS" = "active" ]; then
        IS_RUNNING=true
    fi
else
    # 检查进程是否存在
    if pgrep -f "${$}INSTALL_DIR/agent" > /dev/null; then
        IS_RUNNING=true
    fi
fi

if [ "${$}IS_RUNNING" = true ]; then
    echo -e "${$}{GREEN}================================================${$}{NC}"
    echo -e "${$}{GREEN}  ✅ API Monitor Agent 安装成功!${$}{NC}"
    echo -e "${$}{GREEN}  模式: ${$}INSTALL_MODE${$}{NC}"
    echo -e "${$}{GREEN}  架构: ${$}ARCH (${$}BINARY_NAME)${$}{NC}"
    
    if [ "${$}HAS_SYSTEMD" = true ]; then
        if [ "${$}INSTALL_MODE" = "system" ]; then
            echo -e "${$}{GREEN}  状态: systemctl status ${$}SERVICE_NAME${$}{NC}"
            echo -e "${$}{GREEN}  日志: journalctl -u ${$}SERVICE_NAME -f${$}{NC}"
        else
            echo -e "${$}{GREEN}  状态: systemctl --user status ${$}SERVICE_NAME${$}{NC}"
            echo -e "${$}{GREEN}  日志: journalctl --user -u ${$}SERVICE_NAME -f${$}{NC}"
        fi
    else
        echo -e "${$}{GREEN}  运行方式: 后台进程 (nohup)${$}{NC}"
        echo -e "${$}{GREEN}  日志文件: ${$}INSTALL_DIR/agent.log${$}{NC}"
        echo -e "${$}{GREEN}  停止命令: pkill -f ${$}INSTALL_DIR/agent${$}{NC}"
        echo -e "${$}{YELLOW}  ⚠️ 注意: 非 Systemd 环境重启后需重新运行${$}{NC}"
    fi
    echo -e "${$}{GREEN}================================================${$}{NC}"
else
    echo -e "${$}{RED}❌ 服务启动失败${$}{NC}"
    if [ "${$}HAS_SYSTEMD" = true ]; then
        if [ "${$}INSTALL_MODE" = "system" ]; then
            echo -e "${$}{RED}   journalctl -u ${$}SERVICE_NAME -n 20${$}{NC}"
        else
            echo -e "${$}{RED}   journalctl --user -u ${$}SERVICE_NAME -n 20${$}{NC}"
        fi
    else
        echo -e "${$}{RED}   请查看日志: cat ${$}INSTALL_DIR/agent.log${$}{NC}"
    fi
    exit 1
fi
`;
  }

  /**
   * 生成 Windows (PowerShell) 安装脚本 - 用户级部署，无需管理员权限
   * Agent 安装到 LOCALAPPDATA，使用 HKCU Run 注册表实现开机自启
   * 运行在用户会话中，100% 继承用户环境 (PATH, SSH, Git 等)
   */
  generateWinInstallScript(serverId, serverUrl) {
    serverUrl = normalizeOriginUrl(serverUrl);
    const agentKey = this.getAgentKey(serverId);
    const downloadVersion = encodeURIComponent(`${packageInfo.version || 'dev'}-${Date.now()}`);

    // 读取用户设置的自定义下载地址
    let customDownloadUrl = '';
    try {
      const settings = userSettings.loadUserSettings();
      customDownloadUrl = settings.agentDownloadUrl || '';
    } catch (e) {
      console.warn('[AgentService] 读取用户设置失败:', e.message);
    }

    // 如果设置了自定义地址，使用它；否则使用主控端地址
    const binaryBaseUrl = customDownloadUrl
      ? customDownloadUrl.replace(/\/$/, '')
      : `${serverUrl}/agent`;

    return `
# API Monitor Agent Windows 自动安装/升级脚本 (Rust 版)
# 用户级部署，无需管理员权限，登录后自动启动
$ErrorActionPreference = "Stop"

$SERVER_URL = "${serverUrl}"
$SERVER_ID = "${serverId}"
$AGENT_KEY = "${agentKey}"
$INSTALL_DIR = "$env:LOCALAPPDATA\\APIMonitorAgent"
$BINARY_URL = "${binaryBaseUrl}/agent-windows-amd64.exe?v=${downloadVersion}"

Write-Host ">>> API Monitor Agent 安装/升级脚本 (Rust 版)" -ForegroundColor Cyan
Write-Host "    用户级部署，登录后自动启动，完整继承用户环境" -ForegroundColor Gray

# 1. 检测是否为升级安装
$upgradeMode = $false
$agentExe = Join-Path $INSTALL_DIR "agent.exe"

if (Test-Path $agentExe) {
    $upgradeMode = $true
    Write-Host ">>> 检测到已安装 Agent，将执行升级..." -ForegroundColor Cyan
}

# 2. 停止现有 Agent 进程
Write-Host "⏹  停止现有 Agent..." -ForegroundColor Yellow
Get-Process -Name "agent" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

# 2.1 清理旧版 Windows 服务 (如果存在，从旧版升级)
$existingService = Get-Service -Name "APIMonitorAgent" -ErrorAction SilentlyContinue
if ($existingService) {
    Write-Host "🧹 清理旧版 Windows 服务..." -ForegroundColor Yellow
    Stop-Service -Name "APIMonitorAgent" -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 1
    sc.exe delete APIMonitorAgent 2>$null
    Start-Sleep -Seconds 1
    Remove-Item "$env:ProgramFiles\\APIMonitorAgent" -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host "   ✓ 旧版服务已清理" -ForegroundColor Green
}

# 2.2 清理旧版计划任务 (如果存在)
$oldTask = Get-ScheduledTask -TaskName "APIMonitorAgent" -ErrorAction SilentlyContinue
if ($oldTask) {
    Write-Host "🧹 清理旧版计划任务..." -ForegroundColor Yellow
    Stop-ScheduledTask -TaskName "APIMonitorAgent" -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName "APIMonitorAgent" -Confirm:$false -ErrorAction SilentlyContinue
}

# 3. 创建安装目录
if (-not (Test-Path $INSTALL_DIR)) {
    Write-Host "📁 创建目录: $INSTALL_DIR" -ForegroundColor Gray
    New-Item -ItemType Directory -Force -Path $INSTALL_DIR | Out-Null
}
Set-Location $INSTALL_DIR

# 4. 下载新版二进制文件
Write-Host "📥 下载 Agent 二进制文件..." -ForegroundColor Yellow
$tempExe = Join-Path $INSTALL_DIR "agent.exe.new"
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -Uri $BINARY_URL -OutFile $tempExe -UseBasicParsing
    
    if (Test-Path $agentExe) { Remove-Item $agentExe -Force }
    Rename-Item $tempExe "agent.exe"
    Write-Host "   ✓ 下载完成" -ForegroundColor Green
} catch {
    Write-Host "❌ 下载失败: $_" -ForegroundColor Red
    Write-Host "   尝试备用地址..." -ForegroundColor Yellow
    $BINARY_URL_ALT = "${binaryBaseUrl}/am-agent-win.exe?v=${downloadVersion}"
    try {
        Invoke-WebRequest -Uri $BINARY_URL_ALT -OutFile $tempExe -UseBasicParsing
        if (Test-Path $agentExe) { Remove-Item $agentExe -Force }
        Rename-Item $tempExe "agent.exe"
        Write-Host "   ✓ 使用备用地址下载完成" -ForegroundColor Green
    } catch {
        Write-Host "❌ 备用地址也下载失败: $_" -ForegroundColor Red
        if (Test-Path $tempExe) { Remove-Item $tempExe -Force }
        exit 1
    }
}

# 5. 生成/更新配置文件
$configPath = Join-Path $INSTALL_DIR "config.json"
Write-Host "📝 生成配置文件..." -ForegroundColor Yellow
$config = @{
    serverUrl = $SERVER_URL
    serverId = $SERVER_ID
    agentKey = $AGENT_KEY
    reportInterval = 1500
    reconnectDelay = 4000
} | ConvertTo-Json -Compress
[System.IO.File]::WriteAllText($configPath, $config)
Write-Host "   ✓ 配置已保存" -ForegroundColor Green

# 6. 注册用户级开机自启 (HKCU Run 注册表)
Write-Host "⚙️ 注册用户级开机自启..." -ForegroundColor Yellow
$installResult = & "$agentExe" install 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "❌ 注册自启失败: $installResult" -ForegroundColor Red
    exit 1
}
Write-Host "   ✓ 开机自启已注册" -ForegroundColor Green

# 7. 立即以后台模式启动
Write-Host "🚀 启动 Agent..." -ForegroundColor Yellow
Start-Process -FilePath "$agentExe" -WorkingDirectory $INSTALL_DIR -ArgumentList "-b" -WindowStyle Hidden
Start-Sleep -Seconds 2

# 8. 验证进程
$proc = Get-Process -Name "agent" -ErrorAction SilentlyContinue
if ($proc) {
    Write-Host ""
    Write-Host "================================================" -ForegroundColor Green
    if ($upgradeMode) {
        Write-Host "  ✅ API Monitor Agent 升级成功!" -ForegroundColor Green
    } else {
        Write-Host "  ✅ API Monitor Agent 安装成功!" -ForegroundColor Green
    }
    Write-Host "================================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  安装目录: $INSTALL_DIR" -ForegroundColor White
    Write-Host "  运行模式: 用户级后台 (登录自启)" -ForegroundColor White
    Write-Host "  自启方式: HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run" -ForegroundColor White
    Write-Host ""
    Write-Host "  管理命令:" -ForegroundColor Cyan
    Write-Host "    停止:   & '$agentExe' stop" -ForegroundColor Gray
    Write-Host "    卸载:   & '$agentExe' uninstall" -ForegroundColor Gray
    Write-Host "    启动:   Start-Process '$agentExe' -ArgumentList '-b' -WindowStyle Hidden" -ForegroundColor Gray
    Write-Host ""
} else {
    Write-Host "❌ Agent 启动失败" -ForegroundColor Red
    Write-Host "   请查看日志: $INSTALL_DIR\\agent.log" -ForegroundColor Yellow
    exit 1
}
        `.trim();
  }

  /**
   * 生成卸载脚本
   */
  generateUninstallScript() {
    return `#!/bin/bash
# API Monitor Agent 卸载脚本
# 自动检测权限并卸载对应模式的安装

SERVICE_NAME="api-monitor-agent"

if [ "\\$EUID" -eq 0 ]; then
    # 系统级卸载
    INSTALL_DIR="/opt/api-monitor-agent"
    echo "正在卸载 API Monitor Agent (系统级)..."
    systemctl stop \\$SERVICE_NAME 2>/dev/null || true
    systemctl disable \\$SERVICE_NAME 2>/dev/null || true
    rm -f /etc/systemd/system/\\$SERVICE_NAME.service
    systemctl daemon-reload
    rm -rf "\\$INSTALL_DIR"
else
    # 用户级卸载
    INSTALL_DIR="\\$HOME/.local/share/api-monitor-agent"
    CONFIG_DIR="\\$HOME/.config/api-monitor-agent"
    SERVICE_DIR="\\$HOME/.config/systemd/user"
    echo "正在卸载 API Monitor Agent (用户级)..."
    systemctl --user stop \\$SERVICE_NAME 2>/dev/null || true
    systemctl --user disable \\$SERVICE_NAME 2>/dev/null || true
    rm -f "\\$SERVICE_DIR/\\$SERVICE_NAME.service"
    systemctl --user daemon-reload
    rm -rf "\\$INSTALL_DIR"
    rm -rf "\\$CONFIG_DIR"
fi

echo "✅ 卸载完成"
`;
  }
}

module.exports = new AgentService();
