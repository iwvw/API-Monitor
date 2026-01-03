/**
 * 监控指标模块
 * 负责实时指标流、轮询、历史记录、图表渲染等
 */

import { io } from 'socket.io-client';
import Chart from 'chart.js/auto';

/**
 * 监控指标方法集合
 */
export const metricsMethods = {
  // ==================== 日志与轮询 ====================

  async loadMonitorLogs(page) {
    if (typeof page === 'number') {
      this.logPage = page;
    }

    this.monitorLogsLoading = true;

    try {
      const params = new URLSearchParams({
        page: this.logPage,
        pageSize: this.logPageSize,
      });

      if (this.logFilter.serverId) {
        params.append('serverId', this.logFilter.serverId);
      }
      if (this.logFilter.status) {
        params.append('status', this.logFilter.status);
      }

      const response = await fetch(`/api/server/monitor/logs?${params}`, {
        headers: this.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.success) {
        this.monitorLogs = data.data;
      } else {
        this.showGlobalToast('加载日志失败: ' + data.error, 'error');
      }
    } catch (error) {
      console.error('加载监控日志失败:', error);
      this.showGlobalToast('加载监控日志失败', 'error');
    } finally {
      this.monitorLogsLoading = false;
    }
  },

  startServerPolling() {
    // 关键决策：若有 WebSocket 实时流，则无需发起任何 HTTP 主动探测
    if (this.metricsWsConnected) {
      if (this.serverPollingTimer) {
        console.warn('🛡️ 实时流已接管，正在休眠后台轮询任务');
        this.stopServerPolling();
      }
      return;
    }

    // 确保只有一个轮询定时器在运行
    if (this.serverPollingTimer) return;

    const interval = Math.max(30000, (this.monitorConfig.interval || 60) * 1000);
    console.log(`📡 实时流不可用，启动后台降级轮询 (${interval / 1000}s)`);

    // 重置倒计时
    this.serverRefreshCountdown = Math.floor(interval / 1000);
    this.serverRefreshProgress = 100;

    // 启动倒计时定时器 (仅在可见时运行)
    this.serverCountdownInterval = setInterval(() => {
      if (document.visibilityState !== 'visible') return;

      if (this.serverRefreshCountdown > 0) {
        this.serverRefreshCountdown--;
        this.serverRefreshProgress = (this.serverRefreshCountdown / (interval / 1000)) * 100;
      }
    }, 1000);

    // 启动主轮询定时器
    this.serverPollingTimer = setInterval(() => {
      // 只要可见且已认证就探测，不再局限于 server 标签页
      if (document.visibilityState === 'visible' && this.isAuthenticated) {
        this.probeAllServers();
        // 重置倒计时
        this.serverRefreshCountdown = Math.floor(interval / 1000);
        this.serverRefreshProgress = 100;
      }
    }, interval);
  },

  stopServerPolling() {
    if (this.serverPollingTimer) {
      clearInterval(this.serverPollingTimer);
      this.serverPollingTimer = null;
    }
    if (this.serverCountdownInterval) {
      clearInterval(this.serverCountdownInterval);
      this.serverCountdownInterval = null;
    }
  },

  // ==================== Socket.IO 实时流 ====================

  /**
   * 加载 Socket.IO 客户端 (已从本地 npm 模块导入)
   */
  async loadSocketIO() {
    // Socket.IO 已通过 import 从本地 node_modules 加载
    // 将其暴露到 window 以兼容旧的连接逻辑
    if (!window.io) {
      window.io = io;
    }
    console.log('[Metrics] ✅ Socket.IO 客户端已从本地模块加载');
    return true;
  },

  async connectMetricsStream() {
    if (!this.isAuthenticated) {
      console.warn('⚠️ 尝试连接实时流失败: 用户未登录');
      return;
    }

    if (this.metricsWsConnected || this.metricsWsConnecting) {
      console.warn('ℹ️ 实时指标流已在连接中或已连接');
      return;
    }

    this.metricsWsConnecting = true;

    // 动态加载 Socket.IO 客户端
    const loaded = await this.loadSocketIO();
    if (!loaded) {
      console.warn('[Metrics] Socket.IO 加载失败，降级到 HTTP 轮询');
      this.metricsWsConnecting = false;
      this.startServerPolling();
      return;
    }

    console.log('🚀 正在连接 Socket.IO 实时流...');

    try {
      // 连接到 /metrics 命名空间
      const socket = window.io('/metrics', {
        reconnection: true,
        reconnectionDelay: 1000,
        reconnectionDelayMax: 5000,
        reconnectionAttempts: Infinity,
        transports: ['websocket', 'polling'],
      });

      socket.on('connect', () => {
        this.metricsWsConnected = true;
        this.metricsWsConnecting = false;
        console.log('✅ Socket.IO 实时流已连接');

        // 停止 HTTP 轮询
        this.stopServerPolling();
      });

      // 单个主机指标更新
      socket.on('metrics:update', data => {
        if (data && data.serverId && data.metrics) {
          this.handleSingleMetricUpdate(data);
        }
      });

      // 批量指标更新 (初始连接时)
      socket.on('metrics:batch', dataArray => {
        if (Array.isArray(dataArray)) {
          dataArray.forEach(data => this.handleSingleMetricUpdate(data));
        }
      });

      // 主机状态变更
      socket.on('server:status', data => {
        if (data && data.serverId) {
          this.updateServerStatus(data.serverId, data.status);
        }
      });

      socket.on('disconnect', reason => {
        this.metricsWsConnected = false;
        this.metricsWsConnecting = false;
        console.warn('❌ Socket.IO 连接断开:', reason);

        // 如果不是主动断开，启动轮询作为降级
        if (reason === 'io server disconnect' || reason === 'transport close') {
          console.log('[Metrics] 启动 HTTP 轮询作为降级...');
          this.startServerPolling();
        }
      });

      socket.on('connect_error', err => {
        console.error('[Metrics] Socket.IO 连接错误:', err.message);
        this.metricsWsConnecting = false;
      });

      this.metricsSocket = socket;
    } catch (err) {
      console.error('[Metrics] Socket.IO 初始化失败:', err);
      this.metricsWsConnecting = false;
      this.startServerPolling();
    }
  },

  /**
   * 处理单个主机的指标更新 (Socket.IO 事件格式)
   * 优化: 使用增量更新避免不必要的 Vue 响应式触发
   */
  handleSingleMetricUpdate(data) {
    if (!data || !data.serverId || !data.metrics) return;

    const server = this.serverList.find(s => s.id === data.serverId);
    if (!server) return;

    try {
      const metrics = data.metrics;

      // 确保 info 对象存在，但不替换整个对象
      if (!server.info) {
        server.info = {
          cpu: {},
          memory: {},
          disk: [],
          network: {},
          docker: {},
        };
      }
      const info = server.info;

      // 增量更新 CPU (仅在值变化时更新)
      const newCpuLoad = metrics.load || '-';
      const newCpuUsage = metrics.cpu_usage || '0%';
      // 静态参数保持：只有当新核心数是有效的正整数（>=1）时才更新，避免被异常值覆盖
      const newCpuCores = parseInt(metrics.cores);
      const validNewCores = !isNaN(newCpuCores) && newCpuCores >= 1;

      if (!info.cpu) info.cpu = {};
      if (info.cpu.Load !== newCpuLoad) info.cpu.Load = newCpuLoad;
      if (info.cpu.Usage !== newCpuUsage) info.cpu.Usage = newCpuUsage;
      // 核心数：仅在有效值时更新，且优先保留较大的历史值（防止单次采样异常）
      if (validNewCores) {
        const existingCores = parseInt(info.cpu.Cores) || 0;
        // 如果新值 >= 现有值，或现有值无效，则更新
        if (newCpuCores >= existingCores || existingCores <= 0) {
          info.cpu.Cores = newCpuCores;
        }
      } else if (!info.cpu.Cores) {
        info.cpu.Cores = '-';
      }

      // 增量更新内存
      if (metrics.mem_usage || metrics.mem) {
        const memStr = metrics.mem_usage || metrics.mem || '';
        const memMatch = memStr.match(/(\d+)\/(\d+)MB/);
        if (memMatch) {
          const used = parseInt(memMatch[1]);
          const total = parseInt(memMatch[2]);
          const usagePercent = Math.round((used / total) * 100) + '%';
          const usedStr = used + ' MB';
          const totalStr = total + ' MB';

          if (!info.memory) info.memory = {};
          if (info.memory.Used !== usedStr) info.memory.Used = usedStr;
          if (info.memory.Total !== totalStr) info.memory.Total = totalStr;
          if (info.memory.Usage !== usagePercent) info.memory.Usage = usagePercent;
        }
      }

      // 增量更新磁盘
      if (metrics.disk_usage || metrics.disk) {
        const diskStr = metrics.disk_usage || metrics.disk || '';
        // 匹配格式: "473.78 GB/1.49 TB (31%)"
        const diskMatch = diskStr.match(/(.+?)\/(.+?)\s*\((\d+%?)\)/);
        if (diskMatch) {
          if (!Array.isArray(info.disk)) info.disk = [{}];
          if (!info.disk[0]) info.disk[0] = {};

          if (info.disk[0].device !== '/') info.disk[0].device = '/';
          if (info.disk[0].used !== diskMatch[1].trim()) info.disk[0].used = diskMatch[1].trim();
          if (info.disk[0].total !== diskMatch[2].trim()) info.disk[0].total = diskMatch[2].trim();
          if (info.disk[0].usage !== diskMatch[3]) info.disk[0].usage = diskMatch[3];
        }
      }

      // 增量更新 Docker
      if (metrics.docker) {
        if (!info.docker) info.docker = {};

        const installed = !!metrics.docker.installed;
        const running = metrics.docker.running || 0;
        const stopped = metrics.docker.stopped || 0;

        if (info.docker.installed !== installed) info.docker.installed = installed;
        if (info.docker.runningCount !== running) info.docker.runningCount = running;
        if (info.docker.stoppedCount !== stopped) info.docker.stoppedCount = stopped;

        // 比较容器列表：数量变化或任一容器状态变化时更新
        const newContainers = Array.isArray(metrics.docker.containers)
          ? metrics.docker.containers
          : [];
        const currentContainers = info.docker.containers || [];

        // 检测是否需要更新：数量不同 或 任一容器状态不同
        let shouldUpdate = newContainers.length !== currentContainers.length;
        if (!shouldUpdate && newContainers.length > 0) {
          // 数量相同时，比较每个容器的状态
          for (let i = 0; i < newContainers.length; i++) {
            const newC = newContainers[i];
            const oldC = currentContainers.find(c => c.id === newC.id);
            if (!oldC || oldC.status !== newC.status) {
              shouldUpdate = true;
              break;
            }
          }
        }

        if (shouldUpdate) {
          info.docker.containers = newContainers;
        }
      }

      // 增量更新网络
      if (metrics.network) {
        if (!info.network) info.network = {};
        Object.keys(metrics.network).forEach(key => {
          if (info.network[key] !== metrics.network[key]) {
            info.network[key] = metrics.network[key];
          }
        });
      }

      // 增量更新 GPU 信息（分离静态参数和动态指标）
      if (!info.gpu || typeof info.gpu === 'number') info.gpu = {};

      // GPU 型号（静态参数）：仅在有效非空值时更新，保持历史有效值
      if (metrics.gpu_model && metrics.gpu_model.trim() !== '') {
        if (info.gpu.Model !== metrics.gpu_model) info.gpu.Model = metrics.gpu_model;
      }

      // GPU 动态指标：始终更新（如果有新值的话）
      if (metrics.gpu_usage !== undefined) {
        if (info.gpu.Usage !== metrics.gpu_usage) info.gpu.Usage = metrics.gpu_usage;
      }
      if (metrics.gpu_mem !== undefined && metrics.gpu_mem !== '0 B/1 B') {
        if (info.gpu.Memory !== metrics.gpu_mem) info.gpu.Memory = metrics.gpu_mem;
      }
      if (metrics.gpu_power !== undefined) {
        if (info.gpu.Power !== metrics.gpu_power) info.gpu.Power = metrics.gpu_power;
      }
      if (metrics.gpu_mem_percent !== undefined) {
        if (info.gpu.Percent !== metrics.gpu_mem_percent)
          info.gpu.Percent = metrics.gpu_mem_percent;
      }
      if (metrics.platform && info.platform !== metrics.platform) {
        info.platform = metrics.platform;
        // 计算并缓存简化的平台名称
        info.platformShort = this.formatPlatformShort(metrics.platform, metrics.platformVersion);
      }
      if (metrics.platformVersion && info.platformVersion !== metrics.platformVersion) {
        info.platformVersion = metrics.platformVersion;
        // 平台版本变化时也更新简化名称
        if (info.platform) {
          info.platformShort = this.formatPlatformShort(info.platform, metrics.platformVersion);
        }
      }

      // 增量更新 Uptime
      if (metrics.uptime) {
        if (info.uptime !== metrics.uptime) info.uptime = metrics.uptime;
      }

      // 更新时间戳 (节流: 只有当旧时间戳不存在时才更新，避免频繁触发 Vue 重渲染)
      if (!info.lastUpdate) {
        info.lastUpdate = new Date(data.timestamp || Date.now()).toLocaleTimeString();
      }

      // 仅在状态变化时更新
      if (server.status !== 'online') server.status = 'online';
      if (server.error !== null) server.error = null;
    } catch (err) {
      console.warn('[Metrics] 数据转换失败:', err, data);
    }
  },

  /**
   * 格式化平台名称为简短版本
   */
  formatPlatformShort(platform, version) {
    if (!platform) return '';
    const p = platform.toLowerCase();

    let ver = '';
    if (version) {
      const verMatch = version.match(/(\d+)/);
      if (verMatch) ver = verMatch[1];
    }

    if (p.includes('windows')) {
      if (version) {
        if (version.includes('26') || version.includes('22') || version.includes('21'))
          return 'Win11';
        if (version.includes('19') || version.includes('18')) return 'Win10';
      }
      if (p.includes('11')) return 'Win11';
      if (p.includes('10')) return 'Win10';
      if (p.includes('server')) return 'WinSrv';
      return 'Windows';
    }

    if (p.includes('debian')) return 'Debian' + ver;
    if (p.includes('ubuntu')) return 'Ubuntu' + ver;
    if (p.includes('centos')) return 'CentOS' + ver;
    if (p.includes('fedora')) return 'Fedora' + ver;
    if (p.includes('redhat') || p.includes('rhel')) return 'RHEL' + ver;
    if (p.includes('rocky')) return 'Rocky' + ver;
    if (p.includes('alma')) return 'Alma' + ver;
    if (p.includes('arch')) return 'Arch';
    if (p.includes('alpine')) return 'Alpine' + ver;
    if (p.includes('darwin') || p.includes('macos')) return 'macOS' + ver;
    if (p.includes('freebsd')) return 'FreeBSD' + ver;
    if (p.includes('linux')) return 'Linux';

    return platform.substring(0, 10);
  },

  /**
   * 更新主机状态
   */
  updateServerStatus(serverId, status) {
    const server = this.serverList.find(s => s.id === serverId);
    if (server) {
      server.status = status;
      if (status === 'offline') {
        server.error = 'Agent 离线';
      }
    }
  },

  closeMetricsStream() {
    if (this.metricsSocket) {
      this.metricsSocket.disconnect();
      this.metricsSocket = null;
    }
    // 兼容旧的 WebSocket
    if (this.metricsWs) {
      this.metricsWs.close();
      this.metricsWs = null;
    }
    this.metricsWsConnected = false;
  },

  handleMetricsUpdate(data) {
    if (!data || !Array.isArray(data)) return;

    // 智能更新 serverList 中的数据
    data.forEach(item => {
      if (!item || !item.serverId || !item.metrics) return;

      const server = this.serverList.find(s => s.id === item.serverId);
      if (!server) return;

      // 1. 准备/初始化结构
      // 如果 server.info 不存在，先创建一个完整的基础镜像，避免多次触发 Fragment 更新
      const isNewInfo = !server.info;
      const info = server.info
        ? { ...server.info }
        : {
          cpu: { Load: '-', Usage: '0%', Cores: '-' },
          memory: { Used: '-', Total: '-', Usage: '0%' },
          disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
          network: {
            connections: 0,
            rx_speed: '0 B/s',
            tx_speed: '0 B/s',
            rx_total: '-',
            tx_total: '-',
          },
          system: {},
          docker: { installed: false, containers: [] },
        };

      try {
        // 2. 更新 CPU 数据
        info.cpu = {
          Load: item.metrics.load || '-',
          Usage: item.metrics.cpu_usage || '0%',
          Cores: item.metrics.cores || '-',
        };

        // 3. 更新内存数据 (逻辑增强：解析 "123/1024MB")
        if (item.metrics.mem_usage && typeof item.metrics.mem_usage === 'string') {
          const memMatch = item.metrics.mem_usage.match(/(\d+)\/(\d+)MB/);
          if (memMatch) {
            const used = parseInt(memMatch[1]);
            const total = parseInt(memMatch[2]);
            info.memory = {
              Used: used + ' MB',
              Total: total + ' MB',
              Usage: Math.round((used / total) * 100) + '%',
            };
          }
        }

        // 4. 更新磁盘数据 (逻辑增强：解析 "10G/50G (20%)")
        if (item.metrics.disk_usage && typeof item.metrics.disk_usage === 'string') {
          const diskMatch = item.metrics.disk_usage.match(/([^\/]+)\/([^\s]+)\s\(([\d%.]+)\)/);
          if (diskMatch) {
            // 确保 info.disk 是数组类型（可能从后端传来的是字符串）
            if (!Array.isArray(info.disk)) {
              info.disk = [{}];
            }
            info.disk[0] = {
              device: '/',
              used: diskMatch[1],
              total: diskMatch[2],
              usage: diskMatch[3],
            };
          }
        }

        // 5. 更新 Docker 概要信息 (确保 containers 数组始终存在)
        if (item.metrics.docker) {
          info.docker = {
            ...(info.docker || {}),
            installed: !!item.metrics.docker.installed,
            runningCount: item.metrics.docker.running || 0,
            stoppedCount: item.metrics.docker.stopped || 0,
            containers: Array.isArray(item.metrics.docker.containers)
              ? item.metrics.docker.containers
              : info.docker?.containers || [],
          };
        }
        // 兜底：确保 docker.containers 始终是数组
        if (!info.docker) {
          info.docker = { installed: false, containers: [] };
        } else if (!Array.isArray(info.docker.containers)) {
          info.docker.containers = [];
        }

        // 6. 更新网络信息
        if (item.metrics.network) {
          info.network = {
            ...(info.network || {}),
            ...item.metrics.network,
          };
        }

        // 7. 更新 GPU 和平台信息
        info.gpu = item.metrics.gpu;
        info.platform = item.metrics.platform;
        info.platformVersion = item.metrics.platformVersion;
        info.uptime = item.metrics.uptime;

        // 赋值回响应式对象
        // 如果是新对象，直接赋值；如果是旧对象，赋值新引用以触发更干净的 Patch
        server.info = info;
        server.status = 'online';
        server.error = null;
      } catch (err) {
        console.warn('[Metrics] 数据转换失败:', err, item);
      }
    });
  },

  // ==================== 主动探测 ====================

  async probeAllServers() {
    this.probeStatus = 'loading';
    try {
      const response = await fetch('/api/server/check-all', { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        this.probeStatus = 'success';
        await this.loadServerList();
      } else {
        this.probeStatus = 'error';
      }
    } catch (error) {
      console.error('探测主机失败:', error);
      this.probeStatus = 'error';
    }
    setTimeout(() => {
      this.probeStatus = '';
    }, 3000);
  },

  // ==================== 历史指标 ====================

  async loadMetricsHistory(page = null) {
    if (page !== null) {
      this.metricsHistoryPagination.page = page;
    }

    this.metricsHistoryLoading = true;

    try {
      // 计算时间范围 (使用 UTC 时间)
      let startTime = null;
      const now = Date.now();

      switch (this.metricsHistoryTimeRange) {
        case '1h':
          startTime = new Date(now - 60 * 60 * 1000).toISOString();
          break;
        case '6h':
          startTime = new Date(now - 6 * 60 * 60 * 1000).toISOString();
          break;
        case '24h':
          startTime = new Date(now - 24 * 60 * 60 * 1000).toISOString();
          break;
        case '7d':
          startTime = new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString();
          break;
        case 'all':
        default:
          startTime = null;
      }

      console.log('[History] 查询时间范围:', this.metricsHistoryTimeRange, '起始时间:', startTime);

      // 性能优化：限制单次加载数量，避免数据量过大导致页面卡顿
      const params = new URLSearchParams({
        page: 1,
        pageSize: 500, // 限制加载数量，配合前端降采样确保图表流畅
      });

      if (this.metricsHistoryFilter.serverId) {
        params.append('serverId', this.metricsHistoryFilter.serverId);
      }

      if (startTime) {
        params.append('startTime', startTime);
      }

      const response = await fetch(`/api/server/metrics/history?${params}`, {
        headers: this.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.success) {
        this.metricsHistoryList = data.data;
        this.metricsHistoryTotal = data.pagination.total;
        this.metricsHistoryPagination = {
          page: data.pagination.page,
          pageSize: data.pagination.pageSize,
          totalPages: data.pagination.totalPages,
        };
      } else {
        this.showGlobalToast('加载历史记录失败: ' + data.error, 'error');
      }

      // 同时加载采集器状态
      this.loadCollectorStatus();

      // 渲染图表
      this.$nextTick(() => {
        this.renderMetricsCharts();
      });
    } catch (error) {
      console.error('加载历史指标失败:', error);
      this.showGlobalToast('加载历史指标失败', 'error');
    } finally {
      this.metricsHistoryLoading = false;
    }
  },

  setMetricsTimeRange(range) {
    this.metricsHistoryTimeRange = range;
    this.loadMetricsHistory(1);

    // 如果主机列表有展开的卡片，同步刷新它们的图表
    if (this.expandedServers && this.expandedServers.length > 0) {
      this.expandedServers.forEach(serverId => {
        const server = this.serverList.find(s => s.id === serverId);
        // 延迟刷新，确保 DOM 已处于稳定状态
        setTimeout(() => this.loadCardMetrics(server || serverId), 300);
      });
    }
  },

  async triggerMetricsCollect() {
    try {
      const response = await fetch('/api/server/metrics/collect', { method: 'POST' });
      const data = await response.json();

      if (data.success) {
        this.showGlobalToast('已触发历史指标采集', 'success');
        setTimeout(() => this.loadMetricsHistory(), 1000);
      } else {
        this.showGlobalToast('触发采集失败: ' + data.error, 'error');
      }
    } catch (error) {
      console.error('触发采集失败:', error);
      this.showGlobalToast('触发采集失败', 'error');
    }
  },

  async clearMetricsHistory() {
    const confirmMsg = this.metricsHistoryFilter.serverId
      ? '确定要清空该主机的历史指标记录吗？'
      : '确定要清空所有主机的历史指标记录吗？此操作不可撤销！';

    if (!confirm(confirmMsg)) return;

    try {
      const params = new URLSearchParams();
      if (this.metricsHistoryFilter.serverId) {
        params.append('serverId', this.metricsHistoryFilter.serverId);
      }

      const response = await fetch(`/api/server/metrics/history/clear?${params}`, {
        method: 'DELETE',
        headers: this.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.success) {
        this.showGlobalToast(data.message, 'success');
        this.metricsHistoryList = [];
        this.metricsHistoryTotal = 0;
        this.loadMetricsHistory(1);
      } else {
        this.showGlobalToast('清空失败: ' + data.error, 'error');
      }
    } catch (error) {
      console.error('清空历史指标失败:', error);
      this.showGlobalToast('清空历史指标失败', 'error');
    }
  },

  // ==================== 图表渲染 ====================

  /**
   * 加载 Chart.js (已从本地 npm 模块导入)
   */
  async loadChartJsFallback() {
    // Chart.js 已通过 import 从本地 node_modules 加载
    // 将其暴露到 window 以兼容旧的图表渲染逻辑
    if (!window.Chart) {
      window.Chart = Chart;
    }
    console.log('[Charts] ✅ Chart.js 已从本地模块加载');
    return true;
  },

  async renderMetricsCharts(retryCount = 0) {
    // CDN 模式下 Chart.js 可能还未加载，使用回退机制动态加载
    if (!window.Chart) {
      if (retryCount < 2) {
        console.log(`[Charts] Chart.js 未就绪，${(retryCount + 1) * 300}ms 后重试...`);
        setTimeout(() => this.renderMetricsCharts(retryCount + 1), 300);
        return;
      }

      // 重试用尽，启动多源回退加载
      console.log('[Charts] 正在启动 CDN 多源回退加载...');
      const loaded = await this.loadChartJsFallback();
      if (!loaded) {
        console.warn('[Charts] Chart.js 加载失败，跳过图表渲染');
        return;
      }
    }

    if (!this.groupedMetricsHistory) return;

    Object.entries(this.groupedMetricsHistory).forEach(([serverId, records]) => {
      // 渲染历史页面的大图表
      this.renderSingleChart(serverId, records, `metrics-chart-${serverId}`);
      // 卡片正面图表
      this.renderSingleChart(serverId, records, `metrics-chart-card-${serverId}`);
      // 卡片背面 GPU 图表 (仅当已翻转或即将渲染时)
      this.renderGpuChart(serverId, records, `gpu-chart-${serverId}`);
    });
  },

  /**
   * 渲染单个指标图表
   * @param {string} serverId 主机 ID
   * @param {Array} records 历史记录数据
   * @param {string} canvasId Canvas 元素 ID
   */
  async renderSingleChart(serverId, records, canvasId, retryCount = 0) {
    // 确保 Chart.js 已加载，否则触发回退加载
    if (!window.Chart) {
      const loaded = await this.loadChartJsFallback();
      if (!loaded) return;
    }
    if (!records || records.length === 0) return;

    const canvas = document.getElementById(canvasId);
    if (!canvas) {
      // Canvas 不存在，可能动画还没完成，稍后重试
      if (retryCount < 3) {
        setTimeout(() => this.renderSingleChart(serverId, records, canvasId, retryCount + 1), 200);
      }
      return;
    }

    // 检查 canvas 尺寸是否为 0（可能在展开动画中）
    const rect = canvas.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) {
      if (retryCount < 5) {
        // 尺寸为 0，稍后重试
        setTimeout(() => this.renderSingleChart(serverId, records, canvasId, retryCount + 1), 200);
        return;
      }
      // 重试次数耗尽但仍然没有尺寸，可能是隐藏的标签页，跳过渲染
      console.warn(
        `[Charts] Canvas ${canvasId} has zero size after ${retryCount} retries, skipping render`
      );
      return;
    }

    // 由于记录通常是记录时间倒序排列的，绘图前先克隆并正序排列
    let sortedRecords = [...records].sort(
      (a, b) => new Date(a.recorded_at) - new Date(b.recorded_at)
    );

    // 性能优化：数据点过多时进行降采样 (最多保留 50 个点)
    const MAX_POINTS = 50;
    if (sortedRecords.length > MAX_POINTS) {
      const step = Math.ceil(sortedRecords.length / MAX_POINTS);
      sortedRecords = sortedRecords.filter((_, index) => index % step === 0);
    }

    // 准备数据
    const labels = sortedRecords.map(r => {
      const d = new Date(r.recorded_at);
      return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0');
    });
    const cpuData = sortedRecords.map(r => r.cpu_usage || 0);
    const memData = sortedRecords.map(r => r.mem_usage || 0);
    const gpuData = sortedRecords.map(r => r.gpu_usage || 0);

    // 检查是否包含有效的 GPU 数据
    const hasGpuData = sortedRecords.some(r => r.gpu_usage !== null && r.gpu_usage !== undefined);

    // 如果图表已存在，则尝试增量更新数据
    const existingChart = Chart.getChart(canvas);
    if (existingChart) {
      existingChart.data.labels = labels;
      existingChart.data.datasets[0].data = cpuData;
      existingChart.data.datasets[1].data = memData;
      if (hasGpuData && existingChart.data.datasets[2]) {
        existingChart.data.datasets[2].data = gpuData;
      } else if (hasGpuData && !existingChart.data.datasets[2]) {
        // 如果之前没 GPU 现在有了，则还是需要重新创建或者 push 进去
        existingChart.destroy();
      } else {
        // 正常更新
        existingChart.update('none'); // 使用 'none' 模式禁用更新动画，防止抖动
        return;
      }
    }

    const datasets = [
      {
        label: 'CPU (%)',
        data: cpuData,
        borderColor: '#10b981',
        backgroundColor: 'transparent',
        borderWidth: 2,
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 4,
        spanGaps: true,
      },
      {
        label: '内存 (%)',
        data: memData,
        borderColor: '#3b82f6',
        backgroundColor: 'transparent',
        borderWidth: 2,
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 4,
        spanGaps: true,
      },
    ];

    if (hasGpuData) {
      datasets.push({
        label: 'GPU (%)',
        data: gpuData,
        borderColor: '#76b900',
        backgroundColor: 'transparent',
        borderWidth: 2,
        fill: false,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 4,
        spanGaps: true,
      });
    }

    // 创建新图表
    this.$nextTick(() => {
      // 销毁旧图表实例（如果存在）
      const existingChart = Chart.getChart(canvas);
      if (existingChart) {
        existingChart.destroy();
      }

      new Chart(canvas, {
        type: 'line',
        data: {
          labels: labels,
          datasets: datasets,
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: { duration: 0 }, // 禁用初始化动画，防止翻转时抽搐
          plugins: {
            legend: { display: false },
            tooltip: {
              mode: 'index',
              intersect: false,
              padding: 10,
              backgroundColor: 'rgba(13, 17, 23, 0.9)',
              titleColor: '#8b949e',
              bodyColor: '#e6edf3',
              borderColor: 'rgba(255, 255, 255, 0.1)',
              borderWidth: 1,
            },
          },
          scales: {
            x: {
              display: true,
              grid: {
                display: true,
                color: 'rgba(255, 255, 255, 0.06)',
                drawBorder: false,
              },
              ticks: {
                maxRotation: 0,
                autoSkip: true,
                maxTicksLimit: 6,
                font: { size: 10 },
                color: '#6e7681',
              },
            },
            y: {
              display: true,
              min: 0,
              max: 100,
              grid: {
                display: true,
                color: 'rgba(255, 255, 255, 0.06)',
                drawBorder: false,
              },
              ticks: {
                font: { size: 10 },
                color: '#6e7681',
                stepSize: 25,
              },
            },
          },
          interaction: {
            mode: 'nearest',
            axis: 'x',
            intersect: false,
          },
        },
      });
    });
  },

  /**
   * 为特定主机加载指标历史数据（用于卡片展示）
   */
  async loadCardMetrics(serverOrId) {
    if (!serverOrId) return [];

    // 兼容处理：支持传入主机对象或主机 ID
    let server = typeof serverOrId === 'object' ? serverOrId : null;
    const serverId = typeof serverOrId === 'string' ? serverOrId : server ? server.id : null;

    // 如果只传了 ID，尝试在 serverList 中找到对象，以便能缓存数据
    if (!server && serverId && this.serverList) {
      server = this.serverList.find(s => s.id === serverId);
    }

    if (!serverId) return [];

    try {
      // 计算时间范围
      let startTime = null;
      const now = Date.now();

      switch (this.metricsHistoryTimeRange) {
        case '1h':
          startTime = new Date(now - 60 * 60 * 1000).toISOString();
          break;
        case '6h':
          startTime = new Date(now - 6 * 60 * 60 * 1000).toISOString();
          break;
        case '24h':
          startTime = new Date(now - 24 * 60 * 60 * 1000).toISOString();
          break;
        case '7d':
          startTime = new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString();
          break;
        case 'all':
        default:
          startTime = null;
      }

      const params = new URLSearchParams({
        serverId: serverId,
        page: 1,
        pageSize: 300,
      });

      if (startTime) {
        params.append('startTime', startTime);
      }

      const response = await fetch(`/api/server/metrics/history?${params}`, {
        headers: this.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.success && data.data) {
        const records = data.data;

        // 缓存数据到主机对象中
        if (server) {
          server.metricsCache = records;
        }

        // 更新正面图表
        this.$nextTick(() => {
          this.renderSingleChart(serverId, records, `metrics-chart-card-${serverId}`);
        });

        return records;
      }
      return [];
    } catch (error) {
      console.error('加载卡片指标失败:', error);
      return [];
    }
  },

  // ==================== 采集器管理 ====================

  async loadCollectorStatus() {
    try {
      const response = await fetch('/api/server/metrics/collector/status', {
        headers: this.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.success) {
        this.metricsCollectorStatus = data.data;
        if (data.data.interval) {
          this.metricsCollectInterval = Math.floor(data.data.interval / 60000);
        }
      }
    } catch (error) {
      console.error('加载采集器状态失败:', error);
    }
  },

  getCpuClass(usage) {
    if (!usage && usage !== 0) return '';
    const val = parseFloat(usage);
    if (val >= 90) return 'critical';
    if (val >= 70) return 'warning';
    return 'normal';
  },

  toggleMetricsServerExpand(serverId) {
    const index = this.expandedMetricsServers.indexOf(serverId);
    if (index === -1) {
      this.expandedMetricsServers.push(serverId);
    } else {
      this.expandedMetricsServers.splice(index, 1);
    }
  },

  async updateMetricsCollectInterval() {
    try {
      const intervalMs = this.metricsCollectInterval * 60 * 1000;
      const response = await fetch('/api/server/metrics/collector/interval', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ interval: intervalMs }),
      });
      const data = await response.json();

      if (data.success) {
        this.showGlobalToast(`采集间隔已更新为 ${this.metricsCollectInterval} 分钟`, 'success');
        this.loadCollectorStatus();
      } else {
        this.showGlobalToast('更新失败: ' + data.error, 'error');
      }
    } catch (error) {
      console.error('更新采集间隔失败:', error);
      this.showGlobalToast('更新采集间隔失败', 'error');
    }
  },

  /**
   * 加载监控配置
   */
  async loadMonitorConfig() {
    try {
      const response = await fetch('/api/server/monitor/config', {
        headers: this.getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        this.monitorConfig = data.data;
        // 同步更新显示用的采集间隔
        if (data.data.metrics_collect_interval) {
          this.metricsCollectInterval = Math.floor(data.data.metrics_collect_interval / 60);
        }
        // 加载采集器运行状态
        this.loadCollectorStatus();
      }
    } catch (error) {
      console.error('加载监控配置失败:', error);
    }
  },

  /**
   * 更新监控全局配置
   */
  async updateMonitorConfig() {
    try {
      const response = await fetch('/api/server/monitor/config', {
        method: 'PUT',
        headers: {
          ...this.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(this.monitorConfig),
      });
      const data = await response.json();
      if (data.success) {
        this.showGlobalToast('配置已更新', 'success');
        this.loadCollectorStatus();
        // 重新加载配置以确保同步
        this.loadMonitorConfig();
      }
    } catch (error) {
      this.showGlobalToast('配置更新失败', 'error');
      console.error('更新配置失败:', error);
    }
  },

  /**
   * 渲染 GPU 趋势图
   * @param {string} serverId 主机 ID
   * @param {Array} records 历史指标
   * @param {string} canvasId 画布 ID
   */
  async renderGpuChart(serverId, records, canvasId) {
    if (!window.Chart || !records || records.length === 0) return;
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;

    // 正序排列
    let sortedRecords = [...records].sort(
      (a, b) => new Date(a.recorded_at) - new Date(b.recorded_at)
    );

    // 降采样
    const MAX_POINTS = 50;
    if (sortedRecords.length > MAX_POINTS) {
      const step = Math.ceil(sortedRecords.length / MAX_POINTS);
      sortedRecords = sortedRecords.filter((_, index) => index % step === 0);
    }

    const labels = sortedRecords.map(r => {
      const d = new Date(r.recorded_at);
      return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0');
    });

    // 映射数据 (处理单位: gpu_mem_used 现在在数据库也是 Byte)
    const gpuUsageData = sortedRecords.map(r => r.gpu_usage || 0);
    const gpuMemData = sortedRecords.map(r => {
      if (!r.gpu_mem_total) return 0;
      return Math.min(100, (r.gpu_mem_used / r.gpu_mem_total) * 100);
    });
    const gpuPowerData = sortedRecords.map(r => r.gpu_power || 0);

    const existingChart = Chart.getChart(canvas);
    if (existingChart) {
      existingChart.data.labels = labels;
      existingChart.data.datasets[0].data = gpuUsageData;
      existingChart.data.datasets[1].data = gpuMemData;
      existingChart.data.datasets[2].data = gpuPowerData;
      existingChart.update('none'); // 静默更新，不触发重排
      return;
    }

    new Chart(canvas, {
      type: 'line',
      data: {
        labels,
        datasets: [
          {
            label: 'GPU (%)',
            data: gpuUsageData,
            borderColor: '#76b900',
            backgroundColor: 'rgba(118, 185, 0, 0.1)',
            fill: true,
            tension: 0.3,
            pointRadius: 0,
            borderWidth: 2,
            yAxisID: 'y',
          },
          {
            label: 'VRAM (%)',
            data: gpuMemData,
            borderColor: '#8bc34a',
            borderDash: [3, 3],
            fill: false,
            tension: 0.3,
            pointRadius: 0,
            borderWidth: 1.5,
            yAxisID: 'y',
          },
          {
            label: 'Power (W)',
            data: gpuPowerData,
            borderColor: '#ff9800',
            fill: false,
            tension: 0.3,
            pointRadius: 0,
            borderWidth: 1.2,
            yAxisID: 'y1',
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            display: true,
            position: 'top',
            labels: { boxWidth: 8, padding: 8, font: { size: 9 }, color: '#888' },
          },
          tooltip: {
            mode: 'index',
            intersect: false,
            backgroundColor: 'rgba(13, 17, 23, 0.9)',
            callbacks: {
              label: ctx => {
                const val = ctx.parsed.y.toFixed(1);
                const label = ctx.dataset.label;
                if (label.includes('%')) return `${label}: ${val}%`;
                return `${label}: ${val}W`;
              },
            },
          },
        },
        scales: {
          x: {
            display: true,
            grid: { display: false },
            ticks: { font: { size: 9 }, color: '#666', maxTicksLimit: 6 },
          },
          y: {
            type: 'linear',
            display: true,
            position: 'left',
            min: 0,
            max: 100,
            grid: { color: 'rgba(255, 255, 255, 0.05)' },
            ticks: { font: { size: 9 }, color: '#666', callback: v => v + '%' },
          },
          y1: {
            type: 'linear',
            display: true,
            position: 'right',
            min: 0,
            grid: { drawOnChartArea: false },
            ticks: { font: { size: 9 }, color: '#ff9800', callback: v => v + 'W' },
          },
        },
      },
    });
  },
};
