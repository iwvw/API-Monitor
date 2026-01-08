import { io } from 'socket.io-client';
import Chart from 'chart.js/auto';

/**
 * Uptime 监测模块
 * 负责监测目标管理、心跳数据处理、状态计算
 */

/**
 * Uptime 数据对象
 */
export const uptimeData = {
  // 监测列表
  uptimeMonitors: [],

  // 心跳数据 (按 monitor ID 索引)
  // { monitorId: [{ id, status, time, ping, msg }, ...] }
  uptimeHeartbeats: {},

  // 统计信息
  uptimeStats: {
    up: 0,
    down: 0,
    pending: 0,
    unknown: 0,
  },

  // 最近事件
  uptimeRecentEvents: [],

  // UI 状态
  uptimeCurrentTab: 'list', // 'list' | 'add' | 'stats'
  selectedUptimeMonitor: null,
  uptimeStatusFilter: null,
  uptimeSearchText: '',
  uptimeLoading: false,
  uptimeSaving: false,

  // Socket
  uptimeSocket: null,
  uptimeChartInstance: null,

  // 添加/编辑表单
  uptimeForm: {
    id: null,
    name: '',
    type: 'http',
    url: '',
    hostname: '',
    port: 443,
    method: 'GET',
    interval: 60,
    timeout: 30,
    retries: 0,
    active: true,
    accepted_status_codes: '200-299',
    keyword: '',
    dns_resolve_type: 'A',
    dns_resolve_server: '',
    headers: '',
    body: '',
    // New Fields
    ignoreTls: false,
    expiryNotification: 7,
    tags: [],
    tagsInput: '', // For UI input
    // 通知渠道配置
    notificationChannels: [], // 选中的通知渠道 ID
  },
};

/**
 * Uptime 方法对象
 */
export const uptimeMethods = {
  // ==================== 数据加载 ====================

  /**
   * 初始化模块
   */
  initUptimeModule() {
    if (this.uptimeSocket) return;
    this.connectUptimeSocket();
    this.loadUptimeMonitors();
  },

  connectUptimeSocket() {
    console.log('[Uptime] Connecting socket...');
    this.uptimeSocket = io('/', {
      transports: ['websocket', 'polling']
    });

    this.uptimeSocket.on('connect', () => {
      console.log('[Uptime] Socket connected');
    });

    this.uptimeSocket.on('uptime:heartbeat', (data) => {
      this.handleRealtimeHeartbeat(data);
    });
  },

  handleRealtimeHeartbeat({ monitorId, beat }) {
    if (!this.uptimeHeartbeats[monitorId]) {
      this.uptimeHeartbeats[monitorId] = [];
    }

    // 状态标准化
    // 后端使用 0 (Down) / 1 (Up), 前端使用 'down' / 'up' / 'pending'
    if (typeof beat.status === 'number') {
      beat.status = beat.status === 1 ? 'up' : 'down';
    }

    // 插入心跳
    this.uptimeHeartbeats[monitorId].unshift(beat);
    // 仅保留 50 条
    if (this.uptimeHeartbeats[monitorId].length > 50) {
      this.uptimeHeartbeats[monitorId].length = 50;
    }

    // 高效更新统计
    // 简单起见重新计算所有数据，对于 <100 个监控项来说足够快
    this.calculateUptimeStats();

    // 如果当前选中的是此监控项，则刷新图表
    if (this.selectedUptimeMonitor?.id === monitorId) {
      this.renderUptimeChart();
    }
  },

  /**
   * 加载监测列表
   */
  async loadUptimeMonitors() {
    this.uptimeLoading = true;
    try {
      const res = await fetch('/api/uptime/monitors');
      const data = await res.json();
      this.uptimeMonitors = data;

      // Initialize heartbeats container and load status
      this.uptimeMonitors.forEach(m => {
        if (!this.uptimeHeartbeats[m.id]) this.uptimeHeartbeats[m.id] = [];
        if (m.lastHeartbeat) {
          // Normalize status
          if (typeof m.lastHeartbeat.status === 'number') {
            m.lastHeartbeat.status = m.lastHeartbeat.status === 1 ? 'up' : 'down';
          }
          // Pre-fill latest status if provided by API list
          this.uptimeHeartbeats[m.id] = [m.lastHeartbeat];
        }
      });
      // 是否需要进一步获取历史数据？
      // 目前列表接口提供了最后一条数据。如果用户点击详情，我们再获取完整历史。
      // 但对于迷你图 (sparklines)，我们通常需要更多历史。
      // 优化：批量获取历史记录？还是让 Socket 填满它？
      // 简单起见，这里为所有项获取历史数据 (数量较少时可接受)
      this.uptimeMonitors.forEach(m => this.loadHeartbeats(m.id));

      this.calculateUptimeStats();
    } catch (error) {
      console.error('加载列表失败:', error);
      // Fallback or Toast?
      this.showToast('加载列表失败', 'error');
    } finally {
      this.uptimeLoading = false;
    }
  },

  /**
   * 加载指定 monitor 的心跳数据
   */
  async loadHeartbeats(monitorId, limit = 60) {
    try {
      const res = await fetch(`/api/uptime/monitors/${monitorId}/history`);
      const data = await res.json();

      // Normalize statuses
      const normalizedData = data.map(beat => {
        if (typeof beat.status === 'number') {
          return { ...beat, status: beat.status === 1 ? 'up' : 'down' };
        }
        return beat;
      });

      this.uptimeHeartbeats[monitorId] = normalizedData;
      this.calculateUptimeStats(); // Refine stats based on real history

      // Refresh chart if this monitor is selected
      if (this.selectedUptimeMonitor?.id === monitorId) {
        this.renderUptimeChart();
      }
    } catch (error) {
      console.error('加载心跳数据失败:', error);
    }
  },

  // ==================== CRUD 操作 ====================

  /**
   * 保存监测 (新增或编辑)
   */
  async saveUptimeMonitor() {
    if (!this.uptimeForm.name) {
      this.showToast('请输入监测名称', 'warning');
      return;
    }

    // 验证 ...
    if ((this.uptimeForm.type === 'http' || this.uptimeForm.type === 'keyword') && !this.uptimeForm.url) {
      this.showToast('请输入 URL', 'warning');
      return;
    }
    if ((this.uptimeForm.type === 'tcp' || this.uptimeForm.type === 'ping') && !this.uptimeForm.hostname) {
      this.showToast('请输入主机名', 'warning');
      return;
    }

    // Process Tags
    if (this.uptimeForm.tagsInput) {
      this.uptimeForm.tags = this.uptimeForm.tagsInput.split(/[,，]/).map(t => t.trim()).filter(Boolean);
    } else {
      this.uptimeForm.tags = [];
    }

    this.uptimeSaving = true;
    try {
      const method = this.uptimeForm.id ? 'PUT' : 'POST';
      const url = this.uptimeForm.id ? `/api/uptime/monitors/${this.uptimeForm.id}` : '/api/uptime/monitors';

      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.uptimeForm)
      });
      const data = await res.json();

      if (res.ok) {
        if (!this.uptimeForm.id) {
          // New
          this.uptimeMonitors.push(data);
          this.uptimeHeartbeats[data.id] = [];
          this.showToast('监测已创建', 'success');
        } else {
          // Updated
          const idx = this.uptimeMonitors.findIndex(m => m.id === data.id);
          if (idx !== -1) {
            this.uptimeMonitors[idx] = data;
          }
          this.showToast('监测已更新', 'success');
        }
        this.resetUptimeForm();
        this.uptimeCurrentTab = 'list';
        this.calculateUptimeStats();
      } else {
        throw new Error(data.error || 'Unknown error');
      }

    } catch (error) {
      console.error('保存失败:', error);
      this.showToast('保存失败: ' + error.message, 'error');
    } finally {
      this.uptimeSaving = false;
    }
  },

  /**
   * 删除监测
   */
  async deleteUptimeMonitor(id) {
    if (!confirm('确定要删除此监测吗？')) return;

    try {
      await fetch(`/api/uptime/monitors/${id}`, { method: 'DELETE' });

      this.uptimeMonitors = this.uptimeMonitors.filter((m) => m.id !== id);
      delete this.uptimeHeartbeats[id];

      if (this.selectedUptimeMonitor?.id === id) {
        this.selectedUptimeMonitor = null;
      }

      this.calculateUptimeStats();
      this.showToast('监测已删除', 'success');
    } catch (error) {
      console.error('删除监测失败:', error);
      this.showToast('删除监测失败', 'error');
    }
  },

  /**
   * 暂停/恢复监测
   */
  async toggleUptimeMonitor(monitor) {
    try {
      const res = await fetch(`/api/uptime/monitors/${monitor.id}/toggle`, { method: 'POST' });
      const data = await res.json();

      if (res.ok) {
        monitor.active = data.active;
        this.calculateUptimeStats();
        this.showToast(data.active ? '监测已恢复' : '监测已暂停', 'success');
      } else {
        this.showToast('操作失败', 'error');
      }
    } catch (error) {
      console.error('切换监测状态失败:', error);
      this.showToast('操作失败', 'error');
    }
  },


  // ==================== 表单操作 ====================

  /**
   * 初始化表单 (新增)
   */
  initUptimeForm() {
    this.resetUptimeForm();
  },

  /**
   * 重置表单
   */
  resetUptimeForm() {
    this.uptimeForm = {
      id: null,
      name: '',
      type: 'http',
      url: '',
      hostname: '',
      port: 443,
      method: 'GET',
      interval: 60,
      timeout: 30,
      retries: 0,
      active: true,
      accepted_status_codes: '200-299',
      keyword: '',
      dns_resolve_type: 'A',
      dns_resolve_server: '',
      headers: '',
      body: '',
      ignoreTls: false,
      expiryNotification: 7,
      tags: [],
      tagsInput: '',
      // 默认选中所有已启用的通知渠道
      notificationChannels: this.notificationChannels
        ? this.notificationChannels
          .filter(c => c.enabled === true || c.enabled === 1)
          .map(c => c.id)
        : [],
    };
  },

  /**
   * 编辑监测
   */
  editUptimeMonitor(monitor) {
    this.uptimeForm = {
      ...monitor,
      tagsInput: monitor.tags ? monitor.tags.join(', ') : '',
      // 加载已保存的通知渠道，如果没有则默认选中所有已启用渠道
      notificationChannels: monitor.notificationChannels ||
        (this.notificationChannels
          ? this.notificationChannels
            .filter(c => c.enabled === true || c.enabled === 1)
            .map(c => c.id)
          : []),
    };
    this.uptimeCurrentTab = 'add';
  },

  /**
   * 选择监测 (展开详情)
   */
  selectUptimeMonitor(monitor) {
    if (this.selectedUptimeMonitor?.id === monitor.id) {
      this.selectedUptimeMonitor = null;
      if (this.uptimeChartInstance) {
        this.uptimeChartInstance.destroy();
        this.uptimeChartInstance = null;
      }
    } else {
      this.selectedUptimeMonitor = monitor;
      this.$nextTick(() => {
        this.loadHeartbeats(monitor.id);
        this.renderUptimeChart();
      });
    }
  },

  // ==================== 数据计算 ====================

  /**
   * 计算统计信息
   */
  calculateUptimeStats() {
    this.uptimeStats = { up: 0, down: 0, pending: 0, unknown: 0 };

    this.uptimeMonitors.forEach((monitor) => {
      if (!monitor.active) {
        this.uptimeStats.unknown++;
        return;
      }

      const lastBeat = this.getLastHeartbeat(monitor.id);
      if (!lastBeat) {
        this.uptimeStats.unknown++;
      } else if (lastBeat.status === 'up') {
        this.uptimeStats.up++;
      } else if (lastBeat.status === 'down') {
        this.uptimeStats.down++;
      } else if (lastBeat.status === 'pending') {
        this.uptimeStats.pending++;
      } else {
        this.uptimeStats.unknown++;
      }
    });
  },

  /**
   * 获取最后一个心跳
   */
  getLastHeartbeat(monitorId) {
    const beats = this.uptimeHeartbeats[monitorId];
    if (!beats || beats.length === 0) return null;
    return beats[0]; // 假设按时间倒序排列
  },

  /**
   * 获取 HeartbeatBar 显示数据
   */
  getHeartbeatBars(monitorId, maxBars = 50) {
    const beats = this.uptimeHeartbeats[monitorId] || [];
    const result = [];

    // 从最新到最旧取指定数量
    for (let i = 0; i < maxBars; i++) {
      const beat = beats[i];
      if (beat) {
        result.unshift(beat); // 倒序插入，使最新在右边
      } else {
        // Fill empty if not enough history
        result.unshift({ status: 'empty', time: null, ping: null });
      }
    }

    return result;
  },

  /**
   * 计算可用率
   */
  calculateUptime(monitorId, days = 1) {
    const beats = this.uptimeHeartbeats[monitorId] || [];
    if (beats.length === 0) return '0.00';

    const now = Date.now();
    const cutoff = now - days * 24 * 60 * 60 * 1000;

    const relevantBeats = beats.filter(
      (b) => b.status !== 'empty' && new Date(b.time).getTime() >= cutoff
    );

    if (relevantBeats.length === 0) return '0.00';

    const upBeats = relevantBeats.filter((b) => b.status === 'up').length;
    return ((upBeats / relevantBeats.length) * 100).toFixed(2);
  },

  /**
   * 计算总体可用率
   */
  calculateOverallUptime(days = 1) {
    const activeMonitors = this.uptimeMonitors.filter((m) => m.active);
    if (activeMonitors.length === 0) return '0.00';

    let totalUp = 0;
    let totalBeats = 0;

    const now = Date.now();
    const cutoff = now - days * 24 * 60 * 60 * 1000;

    activeMonitors.forEach((monitor) => {
      const beats = this.uptimeHeartbeats[monitor.id] || [];
      const relevantBeats = beats.filter(
        (b) => b.status !== 'empty' && new Date(b.time).getTime() >= cutoff
      );
      totalBeats += relevantBeats.length;
      totalUp += relevantBeats.filter((b) => b.status === 'up').length;
    });

    if (totalBeats === 0) return '0.00';
    return ((totalUp / totalBeats) * 100).toFixed(2);
  },

  /**
   * 计算平均响应时间
   */
  calculateAvgPing(monitorId) {
    const beats = this.uptimeHeartbeats[monitorId] || [];
    const validBeats = beats.filter((b) => b.ping !== null && b.ping !== undefined);
    if (validBeats.length === 0) return '--';

    const sum = validBeats.reduce((acc, b) => acc + b.ping, 0);
    return Math.round(sum / validBeats.length);
  },

  // ==================== UI 辅助 ====================

  /**
   * 显示 Toast 提示 (Wrapper for global toast)
   */
  showToast(message, type = 'info') {
    if (this.showGlobalToast) {
      this.showGlobalToast(message, type);
    } else {
      console.warn('[Uptime] showGlobalToast not found, fallback to alert:', message);
      // Fallback
      alert(message);
    }
  },

  /**
   * 渲染详情图表
   */
  renderUptimeChart() {
    if (!this.selectedUptimeMonitor) return;
    const monitorId = this.selectedUptimeMonitor.id;
    const heartbeats = this.uptimeHeartbeats[monitorId] || [];

    // Cleanup
    if (this.uptimeChartInstance) {
      this.uptimeChartInstance.destroy();
      this.uptimeChartInstance = null;
    }

    const ctx = document.getElementById('uptimeDetailChart');
    if (!ctx) return;

    // 数据处理 (反转以按时间顺序显示)
    // 限制最近 60 个点以保证性能
    const reversed = [...heartbeats].slice(0, 60).reverse();
    const labels = reversed.map(b => {
      const d = new Date(b.time);
      return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}`;
    });
    const data = reversed.map(b => b.ping || 0);

    this.uptimeChartInstance = new Chart(ctx, {
      type: 'line',
      data: {
        labels: labels,
        datasets: [{
          label: '响应时间 (ms)',
          data: data,
          borderColor: '#10b981',
          backgroundColor: (context) => {
            const ctx = context.chart.ctx;
            const gradient = ctx.createLinearGradient(0, 0, 0, 200);
            gradient.addColorStop(0, 'rgba(16, 185, 129, 0.2)');
            gradient.addColorStop(1, 'rgba(16, 185, 129, 0)');
            return gradient;
          },
          borderWidth: 2,
          fill: true,
          tension: 0.4,
          pointRadius: 0,
          pointHoverRadius: 4
        }]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: {
          intersect: false,
          mode: 'index',
        },
        plugins: {
          legend: { display: false },
          tooltip: {
            mode: 'index',
            intersect: false,
            backgroundColor: 'rgba(0, 0, 0, 0.7)',
            titleColor: '#fff',
            bodyColor: '#fff',
            borderColor: 'rgba(255, 255, 255, 0.1)',
            borderWidth: 1
          }
        },
        scales: {
          x: {
            display: false,
            grid: { display: false }
          },
          y: {
            beginAtZero: true,
            grid: {
              color: 'rgba(156, 163, 175, 0.1)',
              drawBorder: false
            },
            ticks: {
              color: '#9ca3af',
              font: { size: 10 }
            }
          }
        }
      }
    });
  },

  /**
   * 获取监测状态
   */
  getUptimeStatus(monitor) {
    if (!monitor.active) return 'unknown';
    const lastBeat = this.getLastHeartbeat(monitor.id);
    return lastBeat?.status || 'unknown';
  },

  /**
   * 获取监测类型图标
   */
  getUptimeTypeIcon(type) {
    const icons = {
      http: 'fas fa-globe',
      keyword: 'fas fa-search',
      tcp: 'fas fa-ethernet',
      ping: 'fas fa-signal',
      dns: 'fas fa-server',
      push: 'fas fa-satellite-dish',
    };
    return icons[type] || 'fas fa-question';
  },

  /**
   * 获取监测显示 URL
   */
  getUptimeDisplayUrl(monitor) {
    if (monitor.type === 'http' || monitor.type === 'keyword') {
      return monitor.url;
    }
    if (monitor.type === 'tcp') {
      return `${monitor.hostname}:${monitor.port}`;
    }
    return monitor.hostname;
  },

  /**
   * 获取 ping 样式类
   */
  getPingClass(ping) {
    if (!ping) return 'error';
    if (ping < 100) return '';
    if (ping < 300) return 'slow';
    return 'error';
  },

  /**
   * 获取可用率样式类
   */
  getUptimeClass(uptime) {
    const value = parseFloat(uptime);
    if (value >= 99) return 'good';
    if (value >= 95) return 'warning';
    return 'bad';
  },

  /**
   * 获取心跳时间标签
   */
  getHeartbeatTimeLabel(monitorId, position) {
    const beats = this.uptimeHeartbeats[monitorId] || [];
    if (beats.length === 0) return '--';

    if (position === 'start') {
      // Find oldest relevant beat displayed in the bar (max 60)
      const count = beats.length > 60 ? 60 : beats.length;
      const oldestBeat = beats[count - 1]; // beats is sorted desc (0 is newest)

      if (!oldestBeat || !oldestBeat.time) return '--';

      // Calculate time diff from now (or from newest beat?)
      // Usually "10m ago" means from Now.
      const diffMs = Date.now() - new Date(oldestBeat.time).getTime();
      const seconds = Math.floor(diffMs / 1000);

      if (seconds < 60) return `${seconds}秒`;
      if (seconds < 3600) return `${Math.floor(seconds / 60)}分钟`;
      if (seconds < 86400) return `${Math.floor(seconds / 3600)}小时`;
      return `${Math.floor(seconds / 86400)}天`;
    }

    return '现在';
  },

  /**
   * 格式化时间为相对时间
   */
  formatTimeAgo(time) {
    if (!time) return '--';
    const seconds = Math.floor((Date.now() - new Date(time).getTime()) / 1000);

    if (seconds < 60) return '刚刚';
    if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
    return `${Math.floor(seconds / 86400)} 天前`;
  },

  /**
   * 格式化日期时间
   */
  formatDateTime(time) {
    if (!time) return '--';
    return new Date(time).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  },

  /**
   * 筛选后的监测列表 (作为方法而非 getter，供模板调用)
   */
  getFilteredUptimeMonitors() {
    let result = this.uptimeMonitors;

    // 状态筛选
    if (this.uptimeStatusFilter) {
      result = result.filter((m) => {
        const status = this.getUptimeStatus(m);
        return status === this.uptimeStatusFilter;
      });
    }

    // 搜索筛选
    if (this.uptimeSearchText) {
      const search = this.uptimeSearchText.toLowerCase();
      result = result.filter(
        (m) =>
          m.name.toLowerCase().includes(search) ||
          (m.url && m.url.toLowerCase().includes(search)) ||
          (m.hostname && m.hostname.toLowerCase().includes(search)) ||
          (m.tags && m.tags.some(t => t.toLowerCase().includes(search)))
      );
    }

    return result;
  },

  // ==================== Tooltip ====================

  /**
   * 显示 Tooltip
   */
  showUptimeTooltip(event, beat) {
    if (!beat || beat.status === 'empty') return;

    const tooltip = document.getElementById('uptime-tooltip');
    if (!tooltip) return;

    const timeEl = tooltip.querySelector('.uptime-tooltip-time');
    const dotEl = tooltip.querySelector('.uptime-tooltip-status .dot');
    const statusEl = tooltip.querySelector('.uptime-tooltip-status .status-text');
    const pingEl = tooltip.querySelector('.uptime-tooltip-ping');

    timeEl.textContent = this.formatDateTime(beat.time);

    // Reset classes
    dotEl.className = 'dot';
    dotEl.classList.add(beat.status);

    const pingText = beat.status === 'up' ? `响应时间: ${beat.ping}ms` : '响应时间: Timeout';
    const statusText = beat.status === 'up' ? '正常' :
      beat.status === 'down' ? '故障' :
        beat.status === 'pending' ? '检测中' : '未知';

    statusEl.textContent = statusText;
    pingEl.textContent = beat.status === 'up' ? pingText : beat.msg || '';

    // 定位
    const rect = event.target.getBoundingClientRect();
    // Center tooltip above the beat pill
    const left = rect.left + rect.width / 2;
    const top = rect.top - 10; // Above

    tooltip.style.left = `${left}px`;
    tooltip.style.top = `${top}px`;
    tooltip.style.transform = 'translate(-50%, -100%)'; // Move up
    tooltip.classList.add('visible');
  },

  /**
   * 隐藏 Tooltip
   */
  hideUptimeTooltip() {
    const tooltip = document.getElementById('uptime-tooltip');
    if (tooltip) {
      tooltip.classList.remove('visible');
    }
  },
};

/**
 * Uptime 计算属性
 */
export const uptimeComputed = {
  // Keeping this for compatibility if Vue needs it, 
  // but most logic is in getFilteredUptimeMonitors method now.
  filteredUptimeMonitors() {
    return uptimeMethods.getFilteredUptimeMonitors.call(this);
  },
};
