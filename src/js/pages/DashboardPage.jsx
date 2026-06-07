import React, { useState, useEffect, useMemo } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { ChartPalette, Meter, TimeseriesChart } from '@cloudflare/kumo';
import * as echarts from 'echarts/core';
import { BarChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import useStore from '../store.js';
import {
  Cpu,
  Server,
  Terminal,
  Cloud,
  Globe,
  Activity,
  History,
  RefreshCw,
  ArrowRight,
  Box,
  Send,
  Shield,
  FolderOpen,
  TrendingUp
} from '../components/Icons.jsx';

const DEFAULT_DASHBOARD_STATS = {
  host: {
    hostname: '',
    platformLabel: '',
    uptime: 0,
    cpu: { usage: 0, cores: 0, loadAverage: [] },
    memory: { total: 0, used: 0, usage: 0 },
    disk: { root: '', total: 0, used: 0, usage: 0 },
  },
  servers: { total: 0, online: 0, offline: 0, error: 0 },
  geminiCli: { total_calls: 0, success_calls: 0, daily_trend: [] },
  paas: {
    koyeb: { total: 0, running: 0 },
    fly: { total: 0, running: 0 },
  },
  dns: { zones: 0 },
  uptime: { total: 0, up: 0, down: 0 },
  filebox: { total: 0 },
  totp: { total: 0 },
};

const DASHBOARD_CACHE_TTL_MS = 30_000;
const DASHBOARD_FETCH_TIMEOUT_MS = 6_000;
const DASHBOARD_API_STATS_CACHE_KEY = 'dashboard_api_stats_cache_v1';
const DASHBOARD_API_STATS_CACHE_TTL_MS = 10 * 60_000;
const DASHBOARD_API_STATS_CACHE_MAX_AGE_MS = 24 * 60 * 60_000;
const HOST_METRICS_POLL_MS = 2_000;
const HOST_METRICS_FETCH_TIMEOUT_MS = 4_000;

let dashboardStatsCache = null;
let dashboardStatsFetchPromise = null;
let dashboardApiStatsFetchPromise = null;
let dashboardHostMetricsCache = null;

echarts.use([
  BarChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

function ChartBoundaryBox({ className = '', children }) {
  const [boundary, setBoundary] = useState(null);
  return (
    <div ref={setBoundary} className={className}>
      {typeof children === 'function' ? children(boundary) : children}
    </div>
  );
}

function useMediaQuery(query) {
  const getMatches = () => (
    typeof window !== 'undefined' && typeof window.matchMedia === 'function'
      ? window.matchMedia(query).matches
      : false
  );
  const [matches, setMatches] = useState(getMatches);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined;
    const mediaQuery = window.matchMedia(query);
    const handleChange = () => setMatches(mediaQuery.matches);
    handleChange();
    mediaQuery.addEventListener('change', handleChange);
    return () => mediaQuery.removeEventListener('change', handleChange);
  }, [query]);

  return matches;
}

function normalizeApiStats(detail = {}) {
  return {
    total_calls: Number(detail.total_calls) || 0,
    success_calls: Number(detail.success_calls) || 0,
    daily_trend: Array.isArray(detail.daily_trend) ? detail.daily_trend : [],
  };
}

function readDashboardApiStatsCache() {
  if (typeof window === 'undefined') return null;

  try {
    const raw = window.localStorage.getItem(DASHBOARD_API_STATS_CACHE_KEY);
    if (!raw) return null;

    const parsed = JSON.parse(raw);
    const updatedAt = Number(parsed?.updatedAt) || 0;
    if (!updatedAt || Date.now() - updatedAt > DASHBOARD_API_STATS_CACHE_MAX_AGE_MS) {
      window.localStorage.removeItem(DASHBOARD_API_STATS_CACHE_KEY);
      return null;
    }

    return {
      stats: normalizeApiStats(parsed?.stats),
      updatedAt,
    };
  } catch (e) {
    console.error('[Dashboard] API trend cache read failed:', e);
    return null;
  }
}

function writeDashboardApiStatsCache(stats) {
  if (typeof window === 'undefined') return;

  try {
    window.localStorage.setItem(
      DASHBOARD_API_STATS_CACHE_KEY,
      JSON.stringify({
        stats: normalizeApiStats(stats),
        updatedAt: Date.now(),
      })
    );
  } catch (e) {
    console.error('[Dashboard] API trend cache write failed:', e);
  }
}

function getInitialDashboardStats() {
  const baseStats = dashboardStatsCache?.stats || DEFAULT_DASHBOARD_STATS;
  const cachedApiStats = readDashboardApiStatsCache();
  if (!cachedApiStats) return baseStats;

  return {
    ...baseStats,
    geminiCli: cachedApiStats.stats,
  };
}

function parseTrendTimestamp(point) {
  const timestamp = Number(point?.timestamp);
  if (Number.isFinite(timestamp)) return timestamp;

  if (point?.bucket) {
    const parsed = Date.parse(`${String(point.bucket).replace(' ', 'T')}Z`);
    if (Number.isFinite(parsed)) return parsed;
  }

  return null;
}

function formatDashboardTime(timestamp) {
  const date = new Date(timestamp);
  return `${String(date.getHours()).padStart(2, '0')}:00`;
}

function clampPercent(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  return Math.max(0, Math.min(100, number));
}

function formatPercent(value) {
  return `${Math.round(clampPercent(value))}%`;
}

function formatBytes(bytes) {
  const size = Number(bytes) || 0;
  if (size >= 1024 ** 4) return `${(size / 1024 ** 4).toFixed(1)} TB`;
  if (size >= 1024 ** 3) return `${(size / 1024 ** 3).toFixed(1)} GB`;
  if (size >= 1024 ** 2) return `${(size / 1024 ** 2).toFixed(1)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${Math.round(size)} B`;
}

function formatDuration(seconds) {
  const total = Number(seconds) || 0;
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);

  if (days > 0) return `${days}天 ${hours}小时`;
  if (hours > 0) return `${hours}小时 ${minutes}分钟`;
  return `${minutes}分钟`;
}

function MiniMeter({ label, value, detail, tone = 'brand' }) {
  const indicatorClassName = {
    brand: 'bg-kumo-brand',
    info: 'bg-kumo-info',
    success: 'bg-kumo-success',
    warning: 'bg-kumo-warning',
  }[tone] || 'bg-kumo-brand';

  return (
    <Meter
      label={label}
      value={clampPercent(value)}
      min={0}
      max={100}
      customValue={detail || formatPercent(value)}
      className="min-w-0 text-[10px]"
      trackClassName="!h-2 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed"
      indicatorClassName={`!h-full rounded-full ${indicatorClassName}`}
    />
  );
}

function DashboardPage() {
  const { setMainActiveTab, theme } = useStore();
  const isDarkMode = theme === 'dark';
  const isCompactViewport = useMediaQuery('(max-width: 640px)');
  const apiChartHeight = isCompactViewport ? 126 : 170;

  const [stats, setStats] = useState(getInitialDashboardStats);

  const [loading, setLoading] = useState(false);
  const [lastUpdate, setLastUpdate] = useState(() => dashboardStatsCache?.lastUpdate || '');

  // 串联并行请求所有模块数据
  const fetchDashboardStats = async (showLoading = true, { force = false } = {}) => {
    const cached = dashboardStatsCache;
    const cacheFresh = cached && Date.now() - cached.updatedAt < DASHBOARD_CACHE_TTL_MS;

    if (!force && cacheFresh) {
      setStats(cached.stats);
      setLastUpdate(cached.lastUpdate);
      return cached.stats;
    }

    if (!force && dashboardStatsFetchPromise) {
      if (showLoading && !cached) setLoading(true);
      const snapshot = await dashboardStatsFetchPromise;
      setStats(snapshot.stats);
      setLastUpdate(snapshot.lastUpdate);
      setLoading(false);
      return snapshot.stats;
    }

    if (cached && !force) {
      setStats(cached.stats);
      setLastUpdate(cached.lastUpdate);
    }

    if (showLoading) setLoading(true);

    const savedPassword = localStorage.getItem('admin_password') || '';
    const headers = {
      'Content-Type': 'application/json',
      'x-admin-password': savedPassword,
    };
    const previousStats = dashboardStatsCache?.stats || DEFAULT_DASHBOARD_STATS;

    const fetchJson = async (url) => {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), DASHBOARD_FETCH_TIMEOUT_MS);

      try {
        const response = await fetch(url, { headers, signal: controller.signal });
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        return response.json();
      } finally {
        window.clearTimeout(timeout);
      }
    };

    const mergeApiStatsIntoDashboard = (apiStats) => {
      setStats((currentStats) => {
        const nextStats = { ...currentStats, geminiCli: apiStats };

        if (dashboardStatsCache?.stats) {
          dashboardStatsCache = {
            ...dashboardStatsCache,
            stats: {
              ...dashboardStatsCache.stats,
              geminiCli: apiStats,
            },
          };
        }

        return nextStats;
      });
    };

    const requestApiStats = async () => {
      const data = await fetchJson('/api/gemini-cli/stats');
      const detail = data.data || data;
      const apiStats = normalizeApiStats(detail);
      writeDashboardApiStatsCache(apiStats);
      return apiStats;
    };

    const startApiStatsFetch = (forceRequest = false) => {
      if (!forceRequest && dashboardApiStatsFetchPromise) {
        return dashboardApiStatsFetchPromise;
      }

      const request = requestApiStats().finally(() => {
        if (dashboardApiStatsFetchPromise === request) {
          dashboardApiStatsFetchPromise = null;
        }
      });

      dashboardApiStatsFetchPromise = request;
      return request;
    };

    // 1. 获取主机监控
    const fetchServers = async () => {
      try {
        const data = await fetchJson('/api/server/accounts');
        if (data.success && Array.isArray(data.data)) {
          const list = data.data;
          return {
            total: list.length,
            online: list.filter((s) => s.status === 'online').length,
            offline: list.filter((s) => s.status === 'offline').length,
            error: list.filter((s) => s.status === 'error').length,
          };
        }
      } catch (e) {
        console.error('[Dashboard] Servers fetch failed:', e);
      }
      return previousStats.servers;
    };

    // 2. 获取 API 网关
    const fetchApiStats = async () => {
      const cachedApiStats = readDashboardApiStatsCache();
      const cacheFresh = cachedApiStats
        && Date.now() - cachedApiStats.updatedAt < DASHBOARD_API_STATS_CACHE_TTL_MS;

      if (!force && cachedApiStats) {
        if (!cacheFresh) {
          startApiStatsFetch()
            .then(mergeApiStatsIntoDashboard)
            .catch((e) => console.error('[Dashboard] API stats background refresh failed:', e));
        }

        return cachedApiStats.stats;
      }

      try {
        return await startApiStatsFetch(force);
      } catch (e) {
        console.error('[Dashboard] API stats fetch failed:', e);
      }
      return previousStats.geminiCli;
    };

    // 3. 获取 PaaS (Koyeb & Fly.io)
    const fetchPaaS = async () => {
      const fetchKoyeb = async () => {
        try {
          const data = await fetchJson('/api/koyeb/data');
          const koyeb = { total: 0, running: 0 };
          if (!data.success || !data.accounts) return koyeb;

          data.accounts.forEach((acc) => {
            acc.projects?.forEach((project) => {
              project.services?.forEach((service) => {
                koyeb.total++;
                if (service.status === 'HEALTHY' || service.status === 'RUNNING') {
                  koyeb.running++;
                }
              });
            });
          });

          return koyeb;
        } catch (e) {
          console.error('[Dashboard] Koyeb fetch failed:', e);
          return previousStats.paas.koyeb;
        }
      };

      const fetchFly = async () => {
        try {
          const data = await fetchJson('/api/flyio/proxy/apps');
          const fly = { total: 0, running: 0 };
          if (!data.success || !data.data) return fly;

          data.data.forEach((acc) => {
            acc.apps?.forEach((app) => {
              fly.total++;
              if (app.status === 'deployed' || app.status === 'running') {
                fly.running++;
              }
            });
          });

          return fly;
        } catch (e) {
          console.error('[Dashboard] Fly.io fetch failed:', e);
          return previousStats.paas.fly;
        }
      };

      const [koyeb, fly] = await Promise.all([fetchKoyeb(), fetchFly()]);

      return { koyeb, fly };
    };

    // 4. 获取 DNS 区域
    const fetchDns = async () => {
      try {
        const data = await fetchJson('/api/cloudflare/zones');
        if (data.success && Array.isArray(data.data)) {
          return { zones: data.data.length };
        }
      } catch (e) {
        console.error('[Dashboard] DNS fetch failed:', e);
      }
      return previousStats.dns;
    };

    // 5. 获取 Uptime monitors
    const fetchUptime = async () => {
      try {
        const data = await fetchJson('/api/uptime/monitors');
        const monitors = Array.isArray(data) ? data : data.data || [];
        let up = 0;
        let down = 0;

        monitors.forEach((m) => {
          if (m.active) {
            if (m.lastHeartbeat) {
              const status = m.lastHeartbeat.status;
              if (status === 1 || status === 'up') {
                up++;
              } else {
                down++;
              }
            } else {
              up++;
            }
          }
        });

        return { total: monitors.length, up, down };
      } catch (e) {
        console.error('[Dashboard] Uptime fetch failed:', e);
      }
      return previousStats.uptime;
    };

    // 6. 获取文件柜文件数量
    const fetchFilebox = async () => {
      try {
        const data = await fetchJson('/api/filebox/history');
        if (data.success && Array.isArray(data.data)) {
          return { total: data.data.length };
        }
      } catch (e) {
        console.error('[Dashboard] Filebox fetch failed:', e);
      }
      return previousStats.filebox;
    };

    // 7. 获取 TOTP 数量
    const fetchTotp = async () => {
      try {
        const data = await fetchJson('/api/totp/accounts');
        if (data.success && Array.isArray(data.data)) {
          return { total: data.data.length };
        }
      } catch (e) {
        console.error('[Dashboard] TOTP fetch failed:', e);
      }
      return previousStats.totp;
    };

    const request = Promise.allSettled([
      fetchServers(),
      fetchApiStats(),
      fetchPaaS(),
      fetchDns(),
      fetchUptime(),
      fetchFilebox(),
      fetchTotp(),
    ]).then((results) => {
      const latestApiStats = readDashboardApiStatsCache()?.stats;
      const updatedStats = {
        host: dashboardHostMetricsCache || previousStats.host,
        servers: results[0].status === 'fulfilled' ? results[0].value : previousStats.servers,
        geminiCli: latestApiStats || (results[1].status === 'fulfilled' ? results[1].value : previousStats.geminiCli),
        paas: results[2].status === 'fulfilled' ? results[2].value : previousStats.paas,
        dns: results[3].status === 'fulfilled' ? results[3].value : previousStats.dns,
        uptime: results[4].status === 'fulfilled' ? results[4].value : previousStats.uptime,
        filebox: results[5].status === 'fulfilled' ? results[5].value : previousStats.filebox,
        totp: results[6].status === 'fulfilled' ? results[6].value : previousStats.totp,
      };
      return {
        stats: updatedStats,
        lastUpdate: new Date().toLocaleTimeString(),
        updatedAt: Date.now(),
      };
    });

    dashboardStatsFetchPromise = request;

    try {
      const snapshot = await request;
      dashboardStatsCache = snapshot;
      setStats(snapshot.stats);
      setLastUpdate(snapshot.lastUpdate);
      return snapshot.stats;
    } finally {
      if (dashboardStatsFetchPromise === request) {
        dashboardStatsFetchPromise = null;
      }

      setLoading(false);
    }
  };

  useEffect(() => {
    const cached = dashboardStatsCache;
    if (cached) {
      setStats(cached.stats);
      setLastUpdate(cached.lastUpdate);
    }

    const cacheStale = !cached || Date.now() - cached.updatedAt > DASHBOARD_CACHE_TTL_MS;
    if (cacheStale) {
      fetchDashboardStats(!cached);
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    let activeController = null;

    const fetchHostMetrics = async () => {
      if (activeController) return;

      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), HOST_METRICS_FETCH_TIMEOUT_MS);
      activeController = controller;

      const savedPassword = localStorage.getItem('admin_password') || '';

      try {
        const response = await fetch('/api/system/host-metrics', {
          headers: {
            'Content-Type': 'application/json',
            'x-admin-password': savedPassword,
          },
          signal: controller.signal,
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);

        const data = await response.json();
        if (!data.success || !data.data || stopped) return;

        dashboardHostMetricsCache = data.data;
        setStats((currentStats) => {
          const nextStats = { ...currentStats, host: data.data };

          if (dashboardStatsCache?.stats) {
            dashboardStatsCache = {
              ...dashboardStatsCache,
              stats: {
                ...dashboardStatsCache.stats,
                host: data.data,
              },
            };
          }

          return nextStats;
        });
      } catch (e) {
        if (e.name !== 'AbortError') {
          console.error('[Dashboard] Host metrics realtime fetch failed:', e);
        }
      } finally {
        window.clearTimeout(timeout);
        if (activeController === controller) {
          activeController = null;
        }
      }
    };

    fetchHostMetrics();
    const interval = window.setInterval(fetchHostMetrics, HOST_METRICS_POLL_MS);

    return () => {
      stopped = true;
      window.clearInterval(interval);
      activeController?.abort();
    };
  }, []);

  const apiSuccessRate = () => {
    const { total_calls, success_calls } = stats.geminiCli;
    if (total_calls === 0) return '0%';
    return `${Math.round((success_calls / total_calls) * 1000) / 10}%`;
  };

  const apiTrend = stats.geminiCli.daily_trend || [];
  const apiTrendTotal = apiTrend.reduce((sum, item) => sum + (item.total || 0), 0);
  const apiTrendSuccess = apiTrend.reduce((sum, item) => sum + (item.success || 0), 0);
  const hasApiTrendCalls = apiTrend.length >= 2 && apiTrendTotal > 0;
  const apiTrendSuccessRate = apiTrendTotal > 0
    ? `${Math.round((apiTrendSuccess / apiTrendTotal) * 1000) / 10}%`
    : '0%';
  const apiTrendStatusText = apiTrendTotal > 0
    ? `最近 24 小时 ${apiTrendTotal} 次调用 / ${apiTrendSuccessRate} 成功率`
    : stats.geminiCli.total_calls > 0
      ? '最近 24 小时暂无调用'
      : '暂无 Gemini CLI API 调用记录';
  const apiTrendChartData = useMemo(() => [{
    name: '调用量',
    color: ChartPalette.semantic('Neutral', isDarkMode),
    data: apiTrend
      .map((point) => [parseTrendTimestamp(point), Number(point.total) || 0])
      .filter(([timestamp]) => Number.isFinite(timestamp)),
  }], [apiTrend, isDarkMode]);
  const hostCpuUsage = clampPercent(stats.host?.cpu?.usage);
  const hostMemoryUsage = clampPercent(stats.host?.memory?.usage);
  const hostDiskUsage = clampPercent(stats.host?.disk?.usage);
  const hostLoad = stats.host?.cpu?.loadAverage?.[0];
  const hostHealthTone = Math.max(hostCpuUsage, hostMemoryUsage, hostDiskUsage) >= 90
    ? 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20'
    : 'text-kumo-success bg-kumo-success/10 border-kumo-success/20';

  return (
    <div className="space-y-3 sm:space-y-6">
      
      {/* ==================== Header ==================== */}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-base font-bold text-kumo-strong sm:text-xl">系统控制台</h1>
          <p className="mt-0.5 truncate text-[11px] text-kumo-subtle sm:text-xs">系统运行概览与状态指标</p>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-1.5">
          {lastUpdate && (
            <div className="flex items-center gap-1.5 text-[10px] text-kumo-subtle select-none sm:text-[11px]">
              <History className="w-3.5 h-3.5" />
              <span>上次更新: {lastUpdate}</span>
            </div>
          )}
          
          <Button
            onClick={() => fetchDashboardStats(true, { force: true })}
            variant="secondary" size="sm"
            loading={loading}
          >
            {!loading && <RefreshCw className="w-3.5 h-3.5" />}
            <span className="hidden min-[360px]:inline">刷新数据</span>
          </Button>
        </div>
      </div>

      {/* ==================== Stats Grid (5 Cards) ==================== */}
      <div className="grid grid-cols-2 gap-2 sm:gap-4 md:grid-cols-2 lg:grid-cols-5">
        
        {/* Servers Card */}
        <div
          onClick={() => setMainActiveTab('server')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-3 sm:p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-2 sm:mb-4">
              <div className="h-7 w-7 rounded-md bg-kumo-info-tint text-kumo-info flex items-center justify-center sm:h-8 sm:w-8">
                <Server className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
              </div>
              <span className={`text-[11px] font-semibold px-2 py-0.5 rounded border ${
                stats.servers.total === 0
                  ? 'text-kumo-subtle bg-kumo-recessed border-kumo-line'
                  : stats.servers.online === stats.servers.total
                    ? 'text-kumo-success bg-kumo-success/10 border-kumo-success/20'
                    : stats.servers.online === 0
                      ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
                      : 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20'
              }`}>
                {stats.servers.online}/{stats.servers.total} 在线
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">主机管理</span>
              <span className="text-xl font-bold text-kumo-strong tabular-nums sm:text-2xl">
                {stats.servers.total} <span className="text-xs font-normal text-kumo-subtle">台主机</span>
              </span>
            </div>
          </div>
          <div className="mt-2 pt-2 sm:mt-4 sm:pt-3 border-t border-kumo-line flex items-center justify-between text-[11px] text-kumo-subtle group-hover:text-kumo-strong transition-colors sm:text-xs">
            <span className={stats.servers.total > 0 && stats.servers.online < stats.servers.total ? (stats.servers.online === 0 ? 'text-kumo-danger font-medium' : 'text-kumo-warning font-medium') : ''}>
              {stats.servers.total === 0 
                ? '暂无主机实例' 
                : stats.servers.online === stats.servers.total 
                  ? '所有主机运行正常' 
                  : stats.servers.online === 0
                    ? '全部主机发生故障'
                    : `${stats.servers.offline} 台离线`
              }
            </span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* API Gateway Card */}
        <div
          onClick={() => setMainActiveTab('gemini-cli')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-3 sm:p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-2 sm:mb-4">
              <div className="h-7 w-7 rounded-md bg-kumo-brand/10 text-kumo-brand flex items-center justify-center sm:h-8 sm:w-8">
                <Terminal className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
              </div>
              <span className="text-[11px] font-semibold text-kumo-subtle bg-kumo-recessed px-2 py-0.5 rounded border border-kumo-line">
                API 网关
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">调用次数</span>
              <span className="text-xl font-bold text-kumo-strong tabular-nums sm:text-2xl">
                {stats.geminiCli.total_calls} <span className="text-xs font-normal text-kumo-subtle">次</span>
              </span>
            </div>
          </div>
          <div className="mt-2 pt-2 sm:mt-4 sm:pt-3 border-t border-kumo-line flex items-center justify-between text-[11px] text-kumo-subtle group-hover:text-kumo-strong transition-colors sm:text-xs">
            <span>{apiSuccessRate()} 成功率</span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* PaaS Applications Card */}
        <div
          onClick={() => setMainActiveTab('paas')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-3 sm:p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-2 sm:mb-4">
              <div className="h-7 w-7 rounded-md bg-kumo-badge-purple/10 text-kumo-badge-purple flex items-center justify-center sm:h-8 sm:w-8">
                <Cloud className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
              </div>
              <span className="text-[11px] font-semibold text-kumo-badge-purple bg-kumo-badge-purple/10 px-2 py-0.5 rounded border border-kumo-badge-purple/20">
                {stats.paas.koyeb.running + stats.paas.fly.running} 运行
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">云应用实例</span>
              <span className="text-xl font-bold text-kumo-strong tabular-nums sm:text-2xl">
                {stats.paas.koyeb.total + stats.paas.fly.total} <span className="text-xs font-normal text-kumo-subtle">个应用</span>
              </span>
            </div>
          </div>
          <div className="mt-2 pt-2 sm:mt-4 sm:pt-3 border-t border-kumo-line flex items-center justify-between text-[11px] text-kumo-subtle group-hover:text-kumo-strong transition-colors sm:text-xs">
            <span>应用实例状态正常</span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* Cloudflare DNS Card */}
        <div
          onClick={() => setMainActiveTab('dns')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-3 sm:p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-2 sm:mb-4">
              <div className="h-7 w-7 rounded-md bg-kumo-badge-orange/10 text-kumo-badge-orange flex items-center justify-center sm:h-8 sm:w-8">
                <Globe className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
              </div>
              <span className="text-[11px] font-semibold text-kumo-subtle bg-kumo-recessed px-2 py-0.5 rounded border border-kumo-line">
                Cloudflare
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">域名解析</span>
              <span className="text-xl font-bold text-kumo-strong tabular-nums sm:text-2xl">
                {stats.dns.zones} <span className="text-xs font-normal text-kumo-subtle">个区域</span>
              </span>
            </div>
          </div>
          <div className="mt-2 pt-2 sm:mt-4 sm:pt-3 border-t border-kumo-line flex items-center justify-between text-[11px] text-kumo-subtle group-hover:text-kumo-strong transition-colors sm:text-xs">
            <span>域名配置正常</span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

        {/* Uptime Monitors Card */}
        <div
          onClick={() => setMainActiveTab('uptime')}
          className="bg-kumo-base border border-kumo-line hover:border-kumo-brand rounded-lg p-3 sm:p-5 cursor-pointer shadow-sm hover:shadow transition-all group flex flex-col justify-between"
        >
          <div>
            <div className="flex items-center justify-between mb-2 sm:mb-4">
              <div className="h-7 w-7 rounded-md bg-kumo-success/10 text-kumo-success flex items-center justify-center sm:h-8 sm:w-8">
                <Activity className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
              </div>
              <span className={`text-[11px] font-semibold px-2 py-0.5 rounded border ${
                stats.uptime.down > 0
                  ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
                  : 'text-kumo-success bg-kumo-success/10 border-kumo-success/20'
              }`}>
                {stats.uptime.up}/{stats.uptime.total} 在线
              </span>
            </div>
            <div className="space-y-1">
              <span className="text-xs text-kumo-subtle block">服务监控</span>
              <span className="text-xl font-bold text-kumo-strong tabular-nums sm:text-2xl">
                {stats.uptime.total} <span className="text-xs font-normal text-kumo-subtle">个监测</span>
              </span>
            </div>
          </div>
          <div className="mt-2 pt-2 sm:mt-4 sm:pt-3 border-t border-kumo-line flex items-center justify-between text-[11px] text-kumo-subtle group-hover:text-kumo-strong transition-colors sm:text-xs">
            <span className={stats.uptime.down > 0 ? 'text-kumo-danger font-medium' : ''}>
              {stats.uptime.down > 0 ? `${stats.uptime.down} 个监测发生故障` : '服务状态健康'}
            </span>
            <ArrowRight className="w-3 h-3" />
          </div>
        </div>

      </div>

      {/* ==================== Detail Column Split ==================== */}
      <div className="grid grid-cols-1 items-start gap-3 sm:gap-6 lg:grid-cols-3">
        
        {/* Left Column: API Trend Graph + Host Performance */}
        <div className="grid gap-3 sm:gap-4 lg:col-span-2">
          <ChartBoundaryBox className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-3 sm:p-5 flex min-h-0 flex-col overflow-hidden sm:min-h-[260px]">
            {(tooltipBoundary) => (
              <>
                <div className="flex items-center justify-between border-b border-kumo-line pb-2 sm:pb-3">
                  <h3 className="text-xs font-semibold text-kumo-strong flex items-center gap-1.5 select-none sm:text-sm sm:gap-2">
                    <TrendingUp className="h-3.5 w-3.5 text-kumo-brand sm:h-4 sm:w-4" />
                    API 调用趋势
                  </h3>
                  <span className="text-[10px] text-kumo-subtle bg-kumo-recessed border border-kumo-line px-2 py-0.5 rounded font-medium">
                    最近 24 小时
                  </span>
                </div>

                <div className="min-w-0 pt-2 sm:pt-4">
                  {loading || hasApiTrendCalls ? (
                    <div className="min-w-0 overflow-hidden" style={{ height: apiChartHeight }}>
                      <TimeseriesChart
                        echarts={echarts}
                        isDarkMode={isDarkMode}
                        type="bar"
                        data={apiTrendChartData}
                        height={apiChartHeight}
                        xAxisName="时间"
                        yAxisName="调用"
                        xAxisTickCount={3}
                        xAxisTickFormat={formatDashboardTime}
                        yAxisTickFormat={(value) => `${Math.round(value)}`}
                        tooltipValueFormat={(value) => `${Math.round(value)} 次`}
                        tooltipBoundary={tooltipBoundary ?? undefined}
                        tooltipFollowCursor="x"
                        loading={loading && !hasApiTrendCalls}
                        ariaDescription="最近 24 小时 Gemini CLI API 调用量"
                      />
                    </div>
                  ) : (
                    <div className="flex items-center justify-center text-center text-xs text-kumo-subtle" style={{ height: apiChartHeight }}>
                      {apiTrendStatusText}
                    </div>
                  )}
                </div>

                <div className="mt-2 flex items-center gap-2 border-t border-kumo-line pt-2 text-[10px] text-kumo-subtle select-none sm:mt-3 sm:pt-3 sm:text-[11px]">
                  <span className="w-1.5 h-1.5 rounded-full bg-kumo-brand flex-shrink-0" />
                  <span>{apiTrendStatusText}</span>
                </div>
              </>
            )}
          </ChartBoundaryBox>

          <div className="bg-kumo-base border border-kumo-line rounded-lg p-3 shadow-sm sm:p-5">
            <div className="flex flex-col gap-2 border-b border-kumo-line pb-2 sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:pb-3">
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-kumo-success/10 text-kumo-success sm:h-8 sm:w-8">
                  <Cpu className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h3 className="text-xs font-semibold text-kumo-strong sm:text-sm">宿主机性能</h3>
                  <p className="truncate text-[11px] text-kumo-subtle" title={stats.host?.hostname || '-'}>
                    {stats.host?.hostname || '等待采样'} · {stats.host?.platformLabel || '本机运行环境'}
                  </p>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-1.5 sm:gap-2">
                <span className="rounded border border-kumo-line bg-kumo-recessed px-2 py-0.5 text-[11px] font-semibold text-kumo-subtle">
                  2s 实时
                </span>
                <span className={`w-fit rounded border px-2 py-0.5 text-[11px] font-semibold ${hostHealthTone}`}>
                  CPU {formatPercent(hostCpuUsage)}
                </span>
              </div>
            </div>

            <div className="mt-3 grid gap-2 sm:mt-4 sm:gap-4 md:grid-cols-3">
              <MiniMeter label="CPU" value={hostCpuUsage} detail={`${formatPercent(hostCpuUsage)} / ${stats.host?.cpu?.cores || 0}C`} tone="success" />
              <MiniMeter label="内存" value={hostMemoryUsage} detail={`${formatPercent(hostMemoryUsage)} / ${formatBytes(stats.host?.memory?.total)}`} tone="info" />
              <MiniMeter label="磁盘" value={hostDiskUsage} detail={`${formatPercent(hostDiskUsage)} / ${formatBytes(stats.host?.disk?.total)}`} tone="brand" />
            </div>

            <div className="mt-3 grid gap-2 border-t border-kumo-line pt-2 text-[11px] text-kumo-subtle sm:mt-4 sm:grid-cols-3 sm:gap-3 sm:pt-3 sm:text-xs">
              <div className="flex items-center justify-between gap-3">
                <span>运行时间</span>
                <span className="truncate font-semibold text-kumo-strong">{formatDuration(stats.host?.uptime)}</span>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span>负载</span>
                <span className="font-mono font-semibold text-kumo-strong">{Number.isFinite(hostLoad) ? hostLoad.toFixed(2) : '-'}</span>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span>磁盘卷</span>
                <span className="truncate font-semibold text-kumo-strong">{stats.host?.disk?.root || '-'}</span>
              </div>
            </div>
          </div>
        </div>

        {/* Right Column: Services & Tools List */}
        <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-3 sm:p-6 flex flex-col justify-between min-h-0 sm:min-h-[340px]">
          <div className="border-b border-kumo-line pb-2 sm:pb-3.5">
            <h3 className="text-xs font-semibold text-kumo-strong flex items-center gap-1.5 select-none sm:text-sm sm:gap-2">
              <Box className="h-3.5 w-3.5 text-kumo-brand sm:h-4 sm:w-4" />
              服务 & 工具
            </h3>
          </div>

          <div className="flex-1 space-y-2 py-2 sm:space-y-3.5 sm:py-4">
            {/* Koyeb */}
            <div
              onClick={() => setMainActiveTab('paas')}
              className="flex items-center justify-between gap-2 p-2.5 sm:p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="h-7 w-7 rounded-md bg-kumo-badge-purple/10 text-kumo-badge-purple flex items-center justify-center text-sm flex-shrink-0 sm:h-8 sm:w-8">
                  <Box className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">Koyeb</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">边缘计算应用服务</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.paas.koyeb.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.koyeb.running}
              </div>
            </div>

            {/* Fly.io */}
            <div
              onClick={() => setMainActiveTab('paas')}
              className="flex items-center justify-between gap-2 p-2.5 sm:p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="h-7 w-7 rounded-md bg-kumo-brand/10 text-kumo-brand flex items-center justify-center text-sm flex-shrink-0 sm:h-8 sm:w-8">
                  <Send className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">Fly.io</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">全球微型虚拟机</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.paas.fly.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.fly.running}
              </div>
            </div>

            {/* 2FA */}
            <div
              onClick={() => setMainActiveTab('totp')}
              className="flex items-center justify-between gap-2 p-2.5 sm:p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="h-7 w-7 rounded-md bg-kumo-success/10 text-kumo-success flex items-center justify-center text-sm flex-shrink-0 sm:h-8 sm:w-8">
                  <Shield className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">2FA 安全令牌</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">OTP 动态验证码账号</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.totp.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.totp.total}
              </div>
            </div>

            {/* FileBox */}
            <div
              onClick={() => setMainActiveTab('filebox')}
              className="flex items-center justify-between gap-2 p-2.5 sm:p-3.5 bg-kumo-recessed border border-kumo-line hover:border-kumo-brand rounded-md cursor-pointer transition-all group"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="h-7 w-7 rounded-md bg-kumo-info-tint text-kumo-info flex items-center justify-center text-sm flex-shrink-0 sm:h-8 sm:w-8">
                  <FolderOpen className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">文件分享柜</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">文件与片段分享柜</p>
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs font-semibold text-kumo-strong tabular-nums bg-kumo-base border border-kumo-line px-2 py-0.5 rounded">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.filebox.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.filebox.total}
              </div>
            </div>
          </div>

          <div className="text-[10px] text-kumo-subtle border-t border-kumo-line pt-2 select-none text-center sm:pt-3">
            点击以上卡片可直接跳转相应模块管理。
          </div>
        </div>

      </div>

    </div>
  );
}

export default DashboardPage;
