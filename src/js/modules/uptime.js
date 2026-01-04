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
  },
};

/**
 * Uptime 方法对象
 */
export const uptimeMethods = {
  // ==================== 数据加载 ====================

  /**
   * 加载监测列表
   */
  async loadUptimeMonitors() {
    this.uptimeLoading = true;
    try {
      // TODO: 替换为真实 API
      // const res = await fetch('/api/uptime/monitors');
      // this.uptimeMonitors = await res.json();

      // Mock 数据用于前端开发
      await this.loadMockUptimeData();
      this.calculateUptimeStats();
    } catch (error) {
      console.error('加载监测列表失败:', error);
      this.showToast('加载监测列表失败', 'error');
    } finally {
      this.uptimeLoading = false;
    }
  },

  /**
   * 加载 Mock 数据 (开发阶段)
   */
  async loadMockUptimeData() {
    // Mock monitors
    this.uptimeMonitors = [
      {
        id: 1,
        name: '官网首页',
        type: 'http',
        url: 'https://example.com',
        interval: 60,
        timeout: 30,
        retries: 0,
        active: true,
        method: 'GET',
        accepted_status_codes: '200-299',
      },
      {
        id: 2,
        name: 'API 接口',
        type: 'http',
        url: 'https://api.example.com/health',
        interval: 30,
        timeout: 10,
        retries: 1,
        active: true,
        method: 'GET',
        accepted_status_codes: '200-299',
      },
      {
        id: 3,
        name: '数据库服务器',
        type: 'tcp',
        hostname: 'db.example.com',
        port: 5432,
        interval: 60,
        timeout: 10,
        retries: 0,
        active: true,
      },
      {
        id: 4,
        name: 'DNS 解析',
        type: 'dns',
        hostname: 'example.com',
        interval: 300,
        timeout: 10,
        retries: 0,
        active: true,
        dns_resolve_type: 'A',
      },
      {
        id: 5,
        name: '备用服务器',
        type: 'ping',
        hostname: '8.8.8.8',
        interval: 60,
        timeout: 10,
        retries: 0,
        active: false, // 已暂停
      },
    ];

    // 为每个 monitor 生成 mock 心跳数据
    const now = Date.now();
    this.uptimeMonitors.forEach((monitor) => {
      const beats = [];
      for (let i = 0; i < 60; i++) {
        const time = new Date(now - i * monitor.interval * 1000);
        // 随机生成状态和响应时间
        const rand = Math.random();
        let status = 'up';
        let ping = Math.floor(Math.random() * 150) + 20;

        if (!monitor.active) {
          status = 'empty';
          ping = null;
        } else if (rand < 0.02) {
          // 2% 几率 down
          status = 'down';
          ping = null;
        } else if (rand < 0.05) {
          // 3% 几率 pending
          status = 'pending';
          ping = Math.floor(Math.random() * 500) + 200;
        }

        beats.push({
          id: i,
          status,
          time: time.toISOString(),
          ping,
          msg: status === 'down' ? 'Connection timeout' : null,
        });
      }
      this.uptimeHeartbeats[monitor.id] = beats;
    });

    // Mock 最近事件
    this.uptimeRecentEvents = [
      {
        id: 1,
        monitorId: 2,
        monitorName: 'API 接口',
        status: 0,
        msg: 'Connection refused',
        time: new Date(now - 3600000).toISOString(),
      },
      {
        id: 2,
        monitorId: 2,
        monitorName: 'API 接口',
        status: 1,
        msg: '恢复正常',
        time: new Date(now - 3500000).toISOString(),
      },
    ];
  },

  /**
   * 加载指定 monitor 的心跳数据
   */
  async loadHeartbeats(monitorId, limit = 50) {
    try {
      // TODO: 替换为真实 API
      // const res = await fetch(`/api/uptime/monitors/${monitorId}/heartbeats?limit=${limit}`);
      // this.uptimeHeartbeats[monitorId] = await res.json();
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

    if (
      (this.uptimeForm.type === 'http' || this.uptimeForm.type === 'keyword') &&
      !this.uptimeForm.url
    ) {
      this.showToast('请输入 URL', 'warning');
      return;
    }

    if (
      (this.uptimeForm.type === 'tcp' ||
        this.uptimeForm.type === 'ping' ||
        this.uptimeForm.type === 'dns') &&
      !this.uptimeForm.hostname
    ) {
      this.showToast('请输入主机名', 'warning');
      return;
    }

    this.uptimeSaving = true;
    try {
      // TODO: 替换为真实 API
      // const method = this.uptimeForm.id ? 'PUT' : 'POST';
      // const url = this.uptimeForm.id
      //   ? `/api/uptime/monitors/${this.uptimeForm.id}`
      //   : '/api/uptime/monitors';
      // const res = await fetch(url, {
      //   method,
      //   headers: { 'Content-Type': 'application/json' },
      //   body: JSON.stringify(this.uptimeForm),
      // });

      // Mock: 添加到本地列表
      if (this.uptimeForm.id) {
        // 编辑
        const idx = this.uptimeMonitors.findIndex((m) => m.id === this.uptimeForm.id);
        if (idx !== -1) {
          this.uptimeMonitors[idx] = { ...this.uptimeForm };
        }
        this.showToast('监测已更新', 'success');
      } else {
        // 新增
        const newMonitor = {
          ...this.uptimeForm,
          id: Date.now(),
        };
        this.uptimeMonitors.push(newMonitor);
        this.uptimeHeartbeats[newMonitor.id] = [];
        this.showToast('监测已创建', 'success');
      }

      this.resetUptimeForm();
      this.uptimeCurrentTab = 'list';
      this.calculateUptimeStats();
    } catch (error) {
      console.error('保存监测失败:', error);
      this.showToast('保存监测失败', 'error');
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
      // TODO: 替换为真实 API
      // await fetch(`/api/uptime/monitors/${id}`, { method: 'DELETE' });

      // Mock
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
      // TODO: 替换为真实 API
      // const action = monitor.active ? 'pause' : 'resume';
      // await fetch(`/api/uptime/monitors/${monitor.id}/${action}`, { method: 'POST' });

      // Mock
      monitor.active = !monitor.active;
      this.calculateUptimeStats();
      this.showToast(monitor.active ? '监测已恢复' : '监测已暂停', 'success');
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
    };
  },

  /**
   * 编辑监测
   */
  editUptimeMonitor(monitor) {
    this.uptimeForm = { ...monitor };
    this.uptimeCurrentTab = 'add';
  },

  /**
   * 选择监测 (展开详情)
   */
  selectUptimeMonitor(monitor) {
    if (this.selectedUptimeMonitor?.id === monitor.id) {
      this.selectedUptimeMonitor = null;
    } else {
      this.selectedUptimeMonitor = monitor;
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
      if (i < beats.length) {
        result.unshift(beats[i]); // 倒序插入，使最新在右边
      } else {
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
      const oldest = beats[beats.length - 1];
      if (!oldest?.time) return '--';
      const minutes = Math.floor((Date.now() - new Date(oldest.time).getTime()) / 60000);
      if (minutes < 60) return `${minutes}m`;
      if (minutes < 1440) return `${Math.floor(minutes / 60)}h`;
      return `${Math.floor(minutes / 1440)}d`;
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
          (m.hostname && m.hostname.toLowerCase().includes(search))
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
    dotEl.className = `dot ${beat.status}`;
    statusEl.textContent =
      beat.status === 'up' ? '正常' : beat.status === 'down' ? '故障' : '等待中';
    pingEl.textContent = beat.ping ? `响应时间: ${beat.ping}ms` : beat.msg || '';

    // 定位
    const rect = event.target.getBoundingClientRect();
    tooltip.style.left = `${rect.left + rect.width / 2}px`;
    tooltip.style.top = `${rect.bottom + 10}px`;
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
  filteredUptimeMonitors() {
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
          (m.hostname && m.hostname.toLowerCase().includes(search))
      );
    }

    return result;
  },
};
