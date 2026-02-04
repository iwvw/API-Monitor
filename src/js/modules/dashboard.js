/**
 * Dashboard Module - 系统状态概览
 * 优化版：支持缓存预加载、并行请求、后台静默刷新
 */
import { store } from '../store.js';

// 缓存 key
const CACHE_KEY = 'dashboard_stats_cache';
const CACHE_EXPIRY = 5 * 60 * 1000; // 5 分钟缓存有效期

/**
 * 从 localStorage 加载缓存
 */
function loadFromCache() {
  try {
    const cached = localStorage.getItem(CACHE_KEY);
    if (cached) {
      const { data, timestamp } = JSON.parse(cached);
      // 缓存有效期内直接使用
      if (Date.now() - timestamp < CACHE_EXPIRY) {
        return data;
      }
    }
  } catch (e) {
    console.warn('[Dashboard] Cache load failed:', e);
  }
  return null;
}

/**
 * 保存到 localStorage 缓存
 */
function saveToCache(data) {
  try {
    localStorage.setItem(
      CACHE_KEY,
      JSON.stringify({
        data,
        timestamp: Date.now(),
      })
    );
  } catch (e) {
    console.warn('[Dashboard] Cache save failed:', e);
  }
}

export const dashboardMethods = {
  /**
   * 初始化仪表盘数据
   * 优化：先从缓存加载实现瞬时显示，再后台刷新
   */
  async initDashboard() {
    console.log('[Dashboard] Initializing...');

    // 1. 优先从缓存加载（实现瞬时展示）
    const cached = loadFromCache();
    if (cached) {
      console.log('[Dashboard] Loaded from cache');
      Object.assign(store.dashboardStats, cached);
      store.dashboardLastUpdate = '缓存';

      // 后台静默刷新（不显示 loading 状态）
      this.refreshDashboardDataSilent();
    } else {
      // 无缓存时也直接渲染页面（使用默认初始值），后台异步加载数据
      // 不使用 await，让页面立即展示结构
      this.refreshDashboardData();
    }

    // 2. 音乐收藏异步加载（立即设置加载状态，避免空状态闪烁）
    if (this.musicAutoLoadFavorites) {
      // 如果没有当前歌曲且没有缓存，立即进入加载状态
      if (!store.musicCurrentSong) {
        const musicCache =
          localStorage.getItem('music_play_state') || localStorage.getItem('music_widget_cache');
        if (!musicCache) {
          store.musicWidgetLoading = true;
        }
      }
      // 立即执行，不延迟（延迟会导致先显示空状态）
      this.musicAutoLoadFavorites();
    }
  },

  /**
   * 刷新仪表盘所有数据（显示 loading 状态）
   */
  async refreshDashboardData() {
    if (store.dashboardLoading) return;
    store.dashboardLoading = true;

    try {
      await this._fetchAllData();
    } catch (error) {
      console.error('[Dashboard] Refresh error:', error);
    } finally {
      store.dashboardLoading = false;
      store.dashboardLastUpdate = new Date().toLocaleTimeString();
    }
  },

  /**
   * 静默刷新（不显示 loading 状态，用于后台更新）
   */
  async refreshDashboardDataSilent() {
    try {
      await this._fetchAllData();
      store.dashboardLastUpdate = new Date().toLocaleTimeString();
    } catch (error) {
      console.error('[Dashboard] Silent refresh error:', error);
    }
  },

  /**
   * 内部方法：并行获取所有数据
   */
  async _fetchAllData() {
    // 使用 Promise.allSettled 确保部分失败不影响整体
    // 所有请求完全并行，不串行等待
    await Promise.allSettled([
      this.fetchServerSummary(),
      this.fetchApiSummary(),
      this.fetchPaaSSummary(),
      this.fetchDnsSummary(),
      this.fetchUptimeSummary(),
      this.fetchFileBoxSummary ? this.fetchFileBoxSummary() : Promise.resolve(),
      this.loadTotpAccounts ? this.loadTotpAccounts() : Promise.resolve(),
    ]);

    // 保存到缓存
    saveToCache({
      servers: store.dashboardStats.servers,
      antigravity: store.dashboardStats.antigravity,
      geminiCli: store.dashboardStats.geminiCli,
      paas: store.dashboardStats.paas,
      dns: store.dashboardStats.dns,
      uptime: store.dashboardStats.uptime,
      filebox: store.dashboardStats.filebox,
    });
  },

  /**
   * 获取主机状态摘要
   */
  async fetchServerSummary() {
    try {
      const response = await fetch('/api/server/accounts', { headers: store.getAuthHeaders() });
      const data = await response.json();
      if (data.success) {
        const servers = data.data || [];
        store.dashboardStats.servers = {
          total: servers.length,
          online: servers.filter(s => s.status === 'online').length,
          offline: servers.filter(s => s.status === 'offline').length,
          error: servers.filter(s => s.status === 'error').length,
        };
      }
    } catch (e) {
      console.error('[Dashboard] Fetch server summary failed:', e);
    }
  },

  /**
   * 获取 API 网关摘要 (Antigravity & Gemini CLI)
   * 优化：两个请求并行执行
   */
  async fetchApiSummary() {
    const updateAntigravity = async () => {
      try {
        const res = await fetch('/api/antigravity/stats', { headers: store.getAuthHeaders() });
        if (res.ok) {
          const data = await res.json();
          store.dashboardStats.antigravity = data.data || data;
        }
      } catch (e) {
        console.error('[Dashboard] Antigravity stats failed:', e);
      }
    };

    const updateGemini = async () => {
      try {
        const res = await fetch('/api/gemini-cli/stats', { headers: store.getAuthHeaders() });
        if (res.ok) {
          const data = await res.json();
          store.dashboardStats.geminiCli = data.data || data;
        }
      } catch (e) {
        console.error('[Dashboard] Gemini stats failed:', e);
      }
    };

    // Fire both, don't wait for all to finish before updating individual stats
    // But await the group to know when API section is fully done (for loading state)
    await Promise.allSettled([updateAntigravity(), updateGemini()]);

    // 渲染图表
    this.renderApiCharts();
  },

  /**
   * 渲染 API 趋势图表
   */
  renderApiCharts() {
    // 确保 DOM 更新后执行
    setTimeout(() => {
      if (store.dashboardStats.antigravity.daily_trend) {
        this.drawTrendChart('agChart', store.dashboardStats.antigravity.daily_trend, '#f97316'); // Orange for AG
      }
      if (store.dashboardStats.geminiCli.daily_trend) {
        this.drawTrendChart('geminiChart', store.dashboardStats.geminiCli.daily_trend, '#3b82f6'); // Blue for Gemini
      }
    }, 100);
  },

  /**
   * 绘制 Canvas 趋势图 (Smooth Curve + Interaction)
   */
  /* Helper to create/get singleton tooltip */
  ensureTooltipElement() {
    let el = document.getElementById('dashboard-chart-tooltip');
    if (!el) {
      el = document.createElement('div');
      el.id = 'dashboard-chart-tooltip';
      Object.assign(el.style, {
        position: 'fixed',
        zIndex: '99999',
        pointerEvents: 'none',
        background: 'var(--card-bg)', // dynamic theme
        color: 'var(--text-primary)',
        border: '1px solid var(--border-color)',
        borderRadius: '6px',
        padding: '6px 10px',
        fontSize: '11px',
        fontWeight: '700',
        fontFamily: 'Inter, sans-serif',
        boxShadow: '0 4px 16px rgba(0,0,0,0.2)',
        display: 'none',
        whiteSpace: 'nowrap',
        transition: 'opacity 0.1s, transform 0.1s',
        backdropFilter: 'blur(8px)',
      });
      document.body.appendChild(el);
    }
    return el;
  },

  /**
   * 绘制 Canvas 趋势图 (Smooth Curve + Interaction)
   */
  drawTrendChart(refName, data, color) {
    const app = document.querySelector('#app')?.__vue_app__?._instance;

    let canvas = null;
    let container = null;
    if (refName === 'agChart') {
      const groups = document.querySelectorAll('.api-stat-group');
      if (groups.length >= 1) {
        canvas = groups[0].querySelector('canvas');
        container = groups[0].querySelector('.chart-container');
      }
    } else if (refName === 'geminiChart') {
      const groups = document.querySelectorAll('.api-stat-group');
      if (groups.length >= 2) {
        canvas = groups[1].querySelector('canvas');
        container = groups[1].querySelector('.chart-container');
      }
    }

    if (!canvas) return;

    // Check availability of data
    if (!data || data.length === 0) {
      const ctx = canvas.getContext('2d');
      const rect = canvas.getBoundingClientRect();
      canvas.width = rect.width * (window.devicePixelRatio || 1);
      canvas.height = rect.height * (window.devicePixelRatio || 1);
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.fillStyle = 'rgba(255, 255, 255, 0.1)';
      ctx.font = '10px sans-serif';
      ctx.fillText('No Data', 10, canvas.height / 2);
      return;
    }

    // Save state on canvas for interaction
    canvas.chartState = {
      data: data.map(d => d.total),
      labels: data.map(d => d.date), // Assuming data has date
      color: color,
      paddingX: 10,
      paddingTop: 8, // More space for tooltip
      paddingBottom: 5
    };

    // Attach event listeners once
    if (!canvas.hasInteractionListeners) {
      canvas.hasInteractionListeners = true;

      canvas.addEventListener('mousemove', (e) => {
        const rect = canvas.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const state = canvas.chartState;
        if (!state) return;

        const effectiveWidth = rect.width - (state.paddingX * 2);
        const stepX = effectiveWidth / (state.data.length - 1 || 1);

        // Find closest index
        let index = Math.round((x - state.paddingX) / stepX);
        if (index < 0) index = 0;
        if (index >= state.data.length) index = state.data.length - 1;

        this.renderChartFrame(canvas, index);

        // Update DOM Tooltip
        const tooltip = this.ensureTooltipElement();
        const drawingHeight = rect.height - state.paddingBottom - state.paddingTop;
        const maxVal = Math.max(...state.data, 10);

        // Calculate Logic Coordinates
        const logicX = state.paddingX + index * stepX;
        const logicY = state.paddingTop + drawingHeight - (state.data[index] / maxVal) * drawingHeight;

        // Screen Coordinates
        const screenX = rect.left + logicX;
        const screenY = rect.top + logicY;

        // Content
        tooltip.textContent = state.data[index];
        tooltip.style.display = 'block';
        tooltip.style.borderColor = state.color;

        // Position strategy: Default Top
        // translate(-50%, -100%) places it above the point, centered
        let top = screenY - 12;
        let transform = 'translate(-50%, -100%)';

        // Check top boundary (e.g. if close to window top)
        if (top < 50) { // arbitrary buffer
          // Flip to bottom
          top = screenY + 12;
          transform = 'translate(-50%, 0)';
        }

        tooltip.style.left = screenX + 'px';
        tooltip.style.top = top + 'px';
        tooltip.style.transform = transform;
      });

      canvas.addEventListener('mouseleave', () => {
        this.renderChartFrame(canvas, null);
        const tooltip = document.getElementById('dashboard-chart-tooltip');
        if (tooltip) tooltip.style.display = 'none';
      });
    }

    // Initial Render
    this.renderChartFrame(canvas, null);
  },

  /**
   * Internal render function
   */
  renderChartFrame(canvas, highlightIndex) {
    const ctx = canvas.getContext('2d');
    const dpr = window.devicePixelRatio || 1;

    const rect = canvas.getBoundingClientRect();
    const state = canvas.chartState;

    // Logic coords
    const width = rect.width;
    const height = rect.height;

    // Physical coords
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);

    ctx.clearRect(0, 0, width, height);

    const { data, color, paddingX, paddingTop, paddingBottom } = state;
    const maxVal = Math.max(...data, 10);
    const minVal = 0;
    const range = maxVal - minVal;

    const values = data;
    const drawingHeight = height - paddingBottom - paddingTop;
    const stepX = (width - paddingX * 2) / (values.length - 1 || 1);

    // Helper to get coords
    const getPoint = (i) => {
      const x = paddingX + i * stepX;
      const y = paddingTop + drawingHeight - (values[i] / maxVal) * drawingHeight;
      return { x, y };
    };

    // 1. Fill Gradient Area
    const gradient = ctx.createLinearGradient(0, 0, 0, height);
    gradient.addColorStop(0, color + '66'); // 40%
    gradient.addColorStop(1, color + '00'); // 0%
    ctx.fillStyle = gradient;

    ctx.beginPath();
    ctx.moveTo(paddingX, height);

    // Smooth Curve for Fill
    if (values.length > 1) {
      const first = getPoint(0);
      ctx.lineTo(first.x, first.y);

      for (let i = 0; i < values.length - 1; i++) {
        const p0 = getPoint(i > 0 ? i - 1 : i);
        const p1 = getPoint(i);
        const p2 = getPoint(i + 1);
        const p3 = getPoint(i + 2 < values.length ? i + 2 : i + 1);

        const cp1x = p1.x + (p2.x - p0.x) * 0.2; // Tension 0.2
        const cp1y = p1.y + (p2.y - p0.y) * 0.2;
        const cp2x = p2.x - (p3.x - p1.x) * 0.2;
        const cp2y = p2.y - (p3.y - p1.y) * 0.2;

        ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, p2.x, p2.y);
      }
    } else {
      const p = getPoint(0);
      ctx.lineTo(p.x, p.y);
    }

    ctx.lineTo(width - paddingX, height);
    ctx.closePath();
    ctx.fill();

    // 2. Stroke Line (Smooth)
    ctx.strokeStyle = color;
    ctx.lineWidth = 2;
    ctx.lineCap = 'round';
    ctx.lineJoin = 'round';
    ctx.beginPath();

    if (values.length > 1) {
      const first = getPoint(0);
      ctx.moveTo(first.x, first.y);

      for (let i = 0; i < values.length - 1; i++) {
        const p0 = getPoint(i > 0 ? i - 1 : i);
        const p1 = getPoint(i);
        const p2 = getPoint(i + 1);
        const p3 = getPoint(i + 2 < values.length ? i + 2 : i + 1);

        const cp1x = p1.x + (p2.x - p0.x) * 0.2;
        const cp1y = p1.y + (p2.y - p0.y) * 0.2;
        const cp2x = p2.x - (p3.x - p1.x) * 0.2;
        const cp2y = p2.y - (p3.y - p1.y) * 0.2;

        ctx.bezierCurveTo(cp1x, cp1y, cp2x, cp2y, p2.x, p2.y);
      }
    } else {
      const p = getPoint(0);
      ctx.moveTo(p.x, p.y);
      ctx.lineTo(p.x, p.y);
    }
    ctx.stroke();

    // 3. Highlight Interaction
    if (highlightIndex !== null && highlightIndex >= 0 && highlightIndex < values.length) {
      const p = getPoint(highlightIndex);

      // Vertical Line
      ctx.beginPath();
      ctx.moveTo(p.x, paddingTop);
      ctx.lineTo(p.x, height);
      // Use CSS variable if possible, else fallback
      // We can't easily get guidelines color here without re-querying style or passing it in.
      // Reuse logic from before or hardcode a safe consistent color
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.2)';
      ctx.lineWidth = 1;
      ctx.setLineDash([2, 2]);
      ctx.stroke();
      ctx.setLineDash([]); // Reset

      // Outer Glow
      ctx.beginPath();
      ctx.arc(p.x, p.y, 6, 0, Math.PI * 2);
      ctx.fillStyle = color + '40'; // Transparent
      ctx.fill();

      // Inner Dot
      ctx.beginPath();
      ctx.arc(p.x, p.y, 3, 0, Math.PI * 2);
      ctx.fillStyle = '#fff';
      ctx.fill();

      // NOTE: Tooltip drawing removed, handled by DOM element in mousemove

    } else {
      // Draw last point if not hovering
      const lastIdx = values.length - 1;
      const p = getPoint(lastIdx);
      ctx.beginPath();
      ctx.arc(p.x, p.y, 3, 0, Math.PI * 2);
      ctx.fillStyle = '#fff';
      ctx.fill();
    }
  },

  /**
   * 获取 PaaS 摘要 (Zeabur, Koyeb, Fly.io)
   * 优化：三个平台的请求完全并行
   */
  async fetchPaaSSummary() {
    const updateZeabur = async () => {
      try {
        const res = await fetch('/api/zeabur/projects', { headers: store.getAuthHeaders() });
        if (res.ok) {
          const data = await res.json();
          let appCount = 0;
          let runningCount = 0;
          if (Array.isArray(data)) {
            data.forEach(acc => {
              if (acc.projects) {
                acc.projects.forEach(p => {
                  if (p.services) {
                    appCount += p.services.length;
                    runningCount += p.services.filter(s => s.status === 'RUNNING').length;
                  }
                });
              }
            });
          }
          store.dashboardStats.paas.zeabur = { total: appCount, running: runningCount };
        }
      } catch (e) {
        console.error('[Dashboard] Zeabur stats failed:', e);
      }
    };

    const updateKoyeb = async () => {
      try {
        const res = await fetch('/api/koyeb/data', { headers: store.getAuthHeaders() });
        if (res.ok) {
          const data = await res.json();
          let appCount = 0;
          let runningCount = 0;
          if (data.success && data.accounts) {
            data.accounts.forEach(acc => {
              if (acc.projects) {
                acc.projects.forEach(p => {
                  if (p.services) {
                    p.services.forEach(s => {
                      appCount++;
                      if (s.status === 'HEALTHY' || s.status === 'RUNNING') {
                        runningCount++;
                      }
                    });
                  }
                });
              }
            });
          }
          store.dashboardStats.paas.koyeb = { total: appCount, running: runningCount };
        }
      } catch (e) {
        console.error('[Dashboard] Koyeb stats failed:', e);
      }
    };

    const updateFly = async () => {
      try {
        const res = await fetch('/api/flyio/proxy/apps', { headers: store.getAuthHeaders() });
        if (res.ok) {
          const data = await res.json();
          let appCount = 0;
          let runningCount = 0;
          if (data.success && data.data) {
            data.data.forEach(acc => {
              if (acc.apps) {
                acc.apps.forEach(app => {
                  appCount++;
                  if (app.status === 'deployed' || app.status === 'running') {
                    runningCount++;
                  }
                });
              }
            });
          }
          store.dashboardStats.paas.fly = { total: appCount, running: runningCount };
        }
      } catch (e) {
        console.error('[Dashboard] Fly.io stats failed:', e);
      }
    };

    // Parallel execution with independent cache/store updates
    await Promise.allSettled([updateZeabur(), updateKoyeb(), updateFly()]);
  },

  /**
   * 获取 DNS 摘要
   */
  async fetchDnsSummary() {
    try {
      const res = await fetch('/api/cloudflare/zones', { headers: store.getAuthHeaders() });
      if (res.ok) {
        const data = await res.json();
        if (data.success && Array.isArray(data.data)) {
          store.dashboardStats.dns.zones = data.data.length;
        } else if (typeof data.zones === 'number') {
          store.dashboardStats.dns.zones = data.zones;
        }
      }
    } catch (e) {
      console.error('[Dashboard] Fetch DNS summary failed:', e);
    }
  },

  /**
   * 获取 Uptime 摘要
   */
  async fetchUptimeSummary() {
    try {
      const res = await fetch('/api/uptime/monitors', { headers: store.getAuthHeaders() });
      if (res.ok) {
        const data = await res.json();
        const monitors = Array.isArray(data) ? data : (data.data || []);

        let up = 0;
        let down = 0;
        let paused = 0;

        // 遍历统计真实状态
        monitors.forEach(m => {
          if (!m.active) {
            paused++;
          } else {
            // 根据 lastHeartbeat 判断状态
            if (m.lastHeartbeat) {
              const status = m.lastHeartbeat.status;
              // 兼容数字状态 (1=up, 0=down) 和字符串状态 ('up', 'down')
              if (status === 1 || status === 'up') {
                up++;
              } else {
                down++;
              }
            } else {
              // 无心跳数据，暂时视为未知，计入 up 避免误报
              up++;
            }
          }
        });

        store.dashboardStats.uptime.total = monitors.length;
        store.dashboardStats.uptime.up = up;
        store.dashboardStats.uptime.down = down;
      }
    } catch (e) {
      console.error('[Dashboard] Fetch Uptime summary failed:', e);
    }
  },

  /**
   * 获取文件柜摘要
   */
  async fetchFileBoxSummary() {
    try {
      const res = await fetch('/api/filebox/history', { headers: store.getAuthHeaders() });
      if (res.ok) {
        const data = await res.json();
        if (data.success && Array.isArray(data.data)) {
          store.dashboardStats.filebox.total = data.data.length;
        }
      }
    } catch (e) {
      console.error('[Dashboard] Fetch FileBox summary failed:', e);
    }
  },
};

// 在 store 中初始化相关状态
Object.assign(store, {
  dashboardLoading: false,
  dashboardLastUpdate: '',
  dashboardStats: {
    servers: { total: 0, online: 0, offline: 0, error: 0 },
    antigravity: { total_calls: 0, success_calls: 0, fail_calls: 0 },
    geminiCli: { total_calls: 0, success_calls: 0, fail_calls: 0 },
    paas: {
      zeabur: { total: 0, running: 0 },
      koyeb: { total: 0, running: 0 },
      fly: { total: 0, running: 0 },
    },
    dns: { zones: 0 },
    uptime: { total: 0, up: 0, down: 0 },
    filebox: { total: 0 },
  },
});
