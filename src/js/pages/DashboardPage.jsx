import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
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
import { AppCard, ChartCard, PageStack } from '../components/ui/AppPrimitives.jsx';
import {
  Cpu,
  Server,
  Cloud,
  Globe,
  Activity,
  RefreshCw,
  ArrowRight,
  Box,
  Send,
  Shield,
  FolderOpen
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
  servers: { total: 0, online: 0, offline: 0, error: 0, items: [] },
  paas: {
    koyeb: { total: 0, running: 0 },
    fly: { total: 0, running: 0 },
  },
  dns: { zones: 0 },
  uptime: { total: 0, up: 0, down: 0 },
  filebox: { total: 0 },
  totp: { total: 0 },
  apiStats: {
    total: { audit: 0, ops: 0, all: 0 },
    trend: [],
  },
};

const DASHBOARD_CACHE_TTL_MS = 30_000;
const DASHBOARD_FETCH_TIMEOUT_MS = 6_000;
const HOST_METRICS_POLL_MS = 2_000;
const HOST_METRICS_FETCH_TIMEOUT_MS = 4_000;
const DASHBOARD_SERVER_STATUS_LIMIT = 8;

let dashboardStatsCache = null;
let dashboardStatsFetchPromise = null;
let dashboardHostMetricsCache = null;

const isAbortError = (error) => error?.name === 'AbortError';

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

function getInitialDashboardStats() {
  return dashboardStatsCache?.stats || DEFAULT_DASHBOARD_STATS;
}

function parseTrendTimestamp(point) {
  const timestamp = Number(point?.timestamp);
  if (Number.isFinite(timestamp)) return timestamp;

  if (point?.bucket) {
    const bucket = String(point.bucket);
    const parsed = Date.parse(
      /^\d{4}-\d{2}-\d{2}$/.test(bucket) ? `${bucket}T00:00:00Z` : `${bucket.replace(' ', 'T')}Z`
    );
    if (Number.isFinite(parsed)) return parsed;
  }

  return null;
}

function formatDashboardTime(timestamp) {
  const date = new Date(timestamp);
  return `${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
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

function normalizeServerStatus(status) {
  if (status === 'online') return 'online';
  if (status === 'error') return 'error';
  return 'offline';
}

function getServerStatusMeta(status) {
  const normalizedStatus = normalizeServerStatus(status);
  if (normalizedStatus === 'online') {
    return {
      label: '在线',
      className: 'border-kumo-success bg-kumo-success',
    };
  }

  if (normalizedStatus === 'error') {
    return {
      label: '异常',
      className: 'border-kumo-warning bg-kumo-warning',
    };
  }

  return {
    label: '离线',
    className: 'border-kumo-danger bg-kumo-danger/75',
  };
}

function normalizeDashboardServer(server, index) {
  const status = normalizeServerStatus(server?.status);

  return {
    id: server?.id || server?.host || server?.name || `server-${index}`,
    name: server?.name || server?.serverName || server?.host || `主机 ${index + 1}`,
    status,
  };
}

function ServerStatusCapsules({ servers = [], total = 0, online = 0, error = 0 }) {
  if (!total) {
    return null;
  }

  const capsuleServers = servers.length > 0
    ? servers
    : Array.from({ length: total }, (_, index) => ({
      id: `server-status-${index}`,
      name: `主机 ${index + 1}`,
      status: index < online ? 'online' : index < online + error ? 'error' : 'offline',
    }));
  const visibleServers = capsuleServers.slice(0, DASHBOARD_SERVER_STATUS_LIMIT);
  const hiddenCount = Math.max(0, total - visibleServers.length);

  return (
    <div
      className="flex min-h-2.5 max-w-[104px] flex-wrap items-center justify-end gap-1.5"
      aria-label="主机在线状态"
    >
      {visibleServers.map((server, index) => {
        const statusMeta = getServerStatusMeta(server.status);
        const label = `${server.name}: ${statusMeta.label}`;

        return (
          <span
            key={server.id || `${server.name}-${index}`}
            className={`h-2 w-2 rounded-full border shadow-none ${statusMeta.className}`}
            title={label}
            aria-label={label}
          />
        );
      })}
      {hiddenCount > 0 && (
        <Badge variant="secondary" className="h-3.5 rounded-full px-1 text-[9px] leading-none">
          +{hiddenCount}
        </Badge>
      )}
    </div>
  );
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

function DashboardOverviewCard({
  icon: Icon,
  iconClassName,
  badge,
  badgeClassName,
  label,
  value,
  unit,
  detail,
  detailClassName = '',
  statusVisual,
  onClick,
}) {
  return (
    <AppCard
      onClick={onClick}
      padding="none"
      interactive
      className="group grid min-h-[108px] cursor-pointer grid-rows-[auto_1fr_auto] gap-2 overflow-hidden p-3 sm:min-h-[112px] sm:p-3.5"
    >
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <div className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${iconClassName}`}>
            <Icon className="h-3.5 w-3.5" />
          </div>
          <span className="min-w-0 truncate text-[11px] font-semibold text-kumo-subtle">
            {label}
          </span>
        </div>
        <span className={`app-status-pill shrink-0 whitespace-nowrap ${badgeClassName}`}>
          {badge}
        </span>
      </div>

      <div className="flex min-w-0 items-end justify-between gap-3">
        <span className="flex min-w-0 items-baseline gap-1 truncate text-[1.35rem] font-bold leading-none text-kumo-strong tabular-nums sm:text-[1.45rem]">
          {value}
          <span className="truncate text-[11px] font-normal leading-none text-kumo-subtle">
            {unit}
          </span>
        </span>
        {statusVisual && (
          <div className="shrink-0 pb-0.5">
            {statusVisual}
          </div>
        )}
      </div>

      <div className="flex min-h-6 items-center justify-between gap-2 border-t border-kumo-line pt-2 text-[11px] leading-tight text-kumo-subtle transition-colors group-hover:text-kumo-strong">
        <span className={`min-w-0 truncate ${detailClassName}`}>
          {detail}
        </span>
        <ArrowRight className="h-3 w-3 shrink-0" />
      </div>
    </AppCard>
  );
}

function DashboardPage({ onNavigate } = {}) {
  const { setMainActiveTab, theme } = useStore();
  const isDarkMode = theme === 'dark';
  const isCompactViewport = useMediaQuery('(max-width: 640px)');
  const apiChartHeight = isCompactViewport ? 126 : 170;
  const navigateToModule = useCallback((module) => {
    if (typeof onNavigate === 'function') {
      onNavigate(module);
      return;
    }

    setMainActiveTab(module);
  }, [onNavigate, setMainActiveTab]);

  const [stats, setStats] = useState(getInitialDashboardStats);

  const [loading, setLoading] = useState(false);

  // 串联并行请求所有模块数据
  const fetchDashboardStats = async (showLoading = true, { force = false } = {}) => {
    const cached = dashboardStatsCache;
    const cacheFresh = cached && Date.now() - cached.updatedAt < DASHBOARD_CACHE_TTL_MS;

    if (!force && cacheFresh) {
      setStats(cached.stats);
      return cached.stats;
    }

    if (!force && dashboardStatsFetchPromise) {
      if (showLoading && !cached) setLoading(true);
      const snapshot = await dashboardStatsFetchPromise;
      setStats(snapshot.stats);
      setLoading(false);
      return snapshot.stats;
    }

    if (cached && !force) {
      setStats(cached.stats);
    }

    if (showLoading) setLoading(true);

    const updateSegment = (key, value) => {
      setStats((prev) => {
        const next = { ...prev, [key]: value };
        if (dashboardStatsCache) {
          dashboardStatsCache.stats = next;
        }
        return next;
      });
    };

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
          if (!isAbortError(e)) {
            console.error('[Dashboard] Koyeb fetch failed:', e);
          }
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
          if (!isAbortError(e)) {
            console.error('[Dashboard] Fly.io fetch failed:', e);
          }
          return previousStats.paas.fly;
        }
      };

      const [koyeb, fly] = await Promise.all([fetchKoyeb(), fetchFly()]);
      const val = { koyeb, fly };
      updateSegment('paas', val);
      return val;
    };

    // 4. 获取 DNS 区域
    const fetchDns = async () => {
      try {
        const data = await fetchJson('/api/cloudflare/zones');
        if (data.success && Array.isArray(data.data)) {
          const val = { zones: data.data.length };
          updateSegment('dns', val);
          return val;
        }
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] DNS fetch failed:', e);
        }
      }
      updateSegment('dns', previousStats.dns);
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

        const val = { total: monitors.length, up, down };
        updateSegment('uptime', val);
        return val;
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] Uptime fetch failed:', e);
        }
      }
      updateSegment('uptime', previousStats.uptime);
      return previousStats.uptime;
    };

    // 6. 获取文件柜文件数量
    const fetchFilebox = async () => {
      try {
        const data = await fetchJson('/api/filebox/history');
        if (data.success && Array.isArray(data.data)) {
          const val = { total: data.data.length };
          updateSegment('filebox', val);
          return val;
        }
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] Filebox fetch failed:', e);
        }
      }
      updateSegment('filebox', previousStats.filebox);
      return previousStats.filebox;
    };

    // 7. 获取 TOTP 数量
    const fetchTotp = async () => {
      try {
        const data = await fetchJson('/api/totp/accounts');
        if (data.success && Array.isArray(data.data)) {
          const val = { total: data.data.length };
          updateSegment('totp', val);
          return val;
        }
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] TOTP fetch failed:', e);
        }
      }
      updateSegment('totp', previousStats.totp);
      return previousStats.totp;
    };

    const fetchApiStats = async () => {
      try {
        const data = await fetchJson('/api/system/api-stats');
        if (data.success && data.data) {
          const val = data.data;
          updateSegment('apiStats', val);
          return val;
        }
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] API stats fetch failed:', e);
        }
      }
      const fallback = previousStats.apiStats || DEFAULT_DASHBOARD_STATS.apiStats;
      updateSegment('apiStats', fallback);
      return fallback;
    };

    const fetchServers = async () => {
      try {
        const data = await fetchJson('/api/server/accounts');
        if (data.success && Array.isArray(data.data)) {
          const items = data.data.map(normalizeDashboardServer);
          const val = {
            total: items.length,
            online: items.filter((s) => s.status === 'online').length,
            offline: items.filter((s) => s.status === 'offline').length,
            error: items.filter((s) => s.status === 'error').length,
            items,
          };
          updateSegment('servers', val);
          return val;
        }
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] Servers fetch failed:', e);
        }
      }
      updateSegment('servers', previousStats.servers);
      return previousStats.servers;
    };

    const request = Promise.allSettled([
      fetchServers(),
      fetchPaaS(),
      fetchDns(),
      fetchUptime(),
      fetchFilebox(),
      fetchTotp(),
      fetchApiStats(),
    ]).then((results) => {
      const updatedStats = {
        host: dashboardHostMetricsCache || previousStats.host,
        servers: results[0].status === 'fulfilled' ? results[0].value : previousStats.servers,
        paas: results[1].status === 'fulfilled' ? results[1].value : previousStats.paas,
        dns: results[2].status === 'fulfilled' ? results[2].value : previousStats.dns,
        uptime: results[3].status === 'fulfilled' ? results[3].value : previousStats.uptime,
        filebox: results[4].status === 'fulfilled' ? results[4].value : previousStats.filebox,
        totp: results[5].status === 'fulfilled' ? results[5].value : previousStats.totp,
        apiStats: results[6].status === 'fulfilled' ? results[6].value : previousStats.apiStats || DEFAULT_DASHBOARD_STATS.apiStats,
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


  const apiTrend = stats.apiStats?.trend || [];
  const apiTrendTotal = stats.apiStats?.total?.all || 0;
  const apiTrendAudit = stats.apiStats?.total?.audit || 0;
  const apiTrendOps = stats.apiStats?.total?.ops || 0;
  const hasApiTrendCalls = apiTrend.length >= 2 && apiTrendTotal > 0;
  
  const apiStatsDetailText = apiTrendTotal > 0
    ? `审计型 ${apiTrendAudit}次 (${Math.round((apiTrendAudit / apiTrendTotal) * 100)}%) / 操作型 ${apiTrendOps}次`
    : '暂无系统 API 调用记录';
  const apiTrendStatusText = apiTrendTotal > 0
    ? `最近 7 天系统共处理了 ${apiTrendTotal} 次有效 API 请求`
    : '最近 7 天暂无系统 API 调用记录';

  const apiTrendChartData = useMemo(() => [
    {
      name: '审计 API (Audit)',
      color: ChartPalette.semantic('Info', isDarkMode),
      data: apiTrend
        .map((point) => [parseTrendTimestamp(point), Number(point.audit) || 0])
        .filter(([timestamp]) => Number.isFinite(timestamp)),
    },
    {
      name: '操作 API (Ops)',
      color: ChartPalette.semantic('Purple', isDarkMode),
      data: apiTrend
        .map((point) => [parseTrendTimestamp(point), Number(point.ops) || 0])
        .filter(([timestamp]) => Number.isFinite(timestamp)),
    },
  ], [apiTrend, isDarkMode]);

  const hostCpuUsage = clampPercent(stats.host?.cpu?.usage);
  const hostMemoryUsage = clampPercent(stats.host?.memory?.usage);
  const hostDiskUsage = clampPercent(stats.host?.disk?.usage);
  const hostLoad = stats.host?.cpu?.loadAverage?.[0];
  const hostHealthTone = Math.max(hostCpuUsage, hostMemoryUsage, hostDiskUsage) >= 90
    ? 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20'
    : 'text-kumo-success bg-kumo-success/10 border-kumo-success/20';
  const serverBadgeClassName = stats.servers.total === 0
    ? 'text-kumo-subtle bg-kumo-recessed border-kumo-line'
    : stats.servers.online === stats.servers.total
      ? 'text-kumo-success bg-kumo-success/10 border-kumo-success/20'
      : stats.servers.online === 0
        ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
        : 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20';
  const serverDetailText = stats.servers.total === 0
    ? '暂无主机实例'
    : stats.servers.online === stats.servers.total
      ? '所有主机运行正常'
      : stats.servers.online === 0
        ? '全部主机发生故障'
        : `${stats.servers.offline} 台离线`;
  const serverDetailClassName = stats.servers.total > 0 && stats.servers.online < stats.servers.total
    ? (stats.servers.online === 0 ? 'text-kumo-danger font-semibold' : 'text-kumo-warning font-semibold')
    : '';
  const serverStatusItems = Array.isArray(stats.servers.items) ? stats.servers.items : [];
  const uptimeBadgeClassName = stats.uptime.down > 0
    ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
    : 'text-kumo-success bg-kumo-success/10 border-kumo-success/20';
  const uptimeDetailText = stats.uptime.down > 0 ? `${stats.uptime.down} 个监测发生故障` : '服务状态健康';
  const uptimeDetailClassName = stats.uptime.down > 0 ? 'text-kumo-danger font-semibold' : '';

  return (
    <PageStack className="gap-3 sm:gap-4">
      
      {/* ==================== Header ==================== */}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-base font-bold text-kumo-strong sm:text-xl">系统控制台</h1>
          <p className="mt-0.5 truncate text-[11px] text-kumo-subtle sm:text-xs">系统运行概览与状态指标</p>
        </div>

        <div className="flex shrink-0 items-center">
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
      <div className="grid w-full grid-cols-1 gap-2.5 min-[520px]:grid-cols-2 md:grid-cols-3 2xl:grid-cols-5">
        
        <DashboardOverviewCard
          onClick={() => navigateToModule('server')}
          icon={Server}
          iconClassName="bg-kumo-info-tint text-kumo-info"
          badge={`${stats.servers.online}/${stats.servers.total} 在线`}
          badgeClassName={serverBadgeClassName}
          label="主机管理"
          value={stats.servers.total}
          unit="台主机"
          detail={serverDetailText}
          detailClassName={serverDetailClassName}
          statusVisual={(
            <ServerStatusCapsules
              servers={serverStatusItems}
              total={stats.servers.total}
              online={stats.servers.online}
              error={stats.servers.error}
            />
          )}
        />



        <DashboardOverviewCard
          onClick={() => navigateToModule('paas')}
          icon={Cloud}
          iconClassName="bg-kumo-badge-purple/10 text-kumo-badge-purple"
          badge={`${stats.paas.koyeb.running + stats.paas.fly.running} 运行`}
          badgeClassName="text-kumo-badge-purple bg-kumo-badge-purple/10 border-kumo-badge-purple/20"
          label="云应用实例"
          value={stats.paas.koyeb.total + stats.paas.fly.total}
          unit="个应用"
          detail="应用实例状态正常"
        />

        <DashboardOverviewCard
          onClick={() => navigateToModule('dns')}
          icon={Globe}
          iconClassName="bg-kumo-badge-orange/10 text-kumo-badge-orange"
          badge="Cloudflare"
          badgeClassName="text-kumo-subtle bg-kumo-recessed border-kumo-line"
          label="域名解析"
          value={stats.dns.zones}
          unit="个区域"
          detail="域名配置正常"
        />

        <DashboardOverviewCard
          onClick={() => navigateToModule('uptime')}
          icon={Activity}
          iconClassName="bg-kumo-success/10 text-kumo-success"
          badge={`${stats.uptime.up}/${stats.uptime.total} 在线`}
          badgeClassName={uptimeBadgeClassName}
          label="服务监控"
          value={stats.uptime.total}
          unit="个监测"
          detail={uptimeDetailText}
          detailClassName={uptimeDetailClassName}
        />

        <DashboardOverviewCard
          onClick={() => navigateToModule('settings')}
          icon={Activity}
          iconClassName="bg-kumo-brand/10 text-kumo-brand"
          badge="系统 API"
          badgeClassName="text-kumo-subtle bg-kumo-recessed border-kumo-line"
          label="系统 API 调用"
          value={apiTrendTotal}
          unit="次"
          detail={apiStatsDetailText}
        />

      </div>

      {/* ==================== Detail Column Split ==================== */}
      <div className="grid grid-cols-1 items-stretch gap-3 sm:gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(360px,1fr)]">
        
        {/* Left Column: API Trend Graph + Host Performance */}
        <div className="grid min-w-0 gap-3 sm:gap-4">
          <ChartCard className="flex min-h-0 flex-col sm:min-h-[260px] sm:p-5">
            {(tooltipBoundary) => (
              <>
                <div className="flex items-center justify-between border-b border-kumo-line pb-2 sm:pb-3">
                  <h3 className="text-xs font-semibold text-kumo-strong flex items-center gap-1.5 select-none sm:text-sm sm:gap-2">
                    <Activity className="h-3.5 w-3.5 text-kumo-brand sm:h-4 sm:w-4" />
                    系统 API 调用趋势
                  </h3>
                  <span className="text-[10px] text-kumo-subtle app-subcard bg-kumo-recessed px-2 py-0.5 rounded font-medium">
                    最近 7 天
                  </span>
                </div>

                <div className="min-w-0 pt-2 sm:pt-4">
                  {loading || hasApiTrendCalls ? (
                    <div className="min-w-0 overflow-hidden" style={{ height: apiChartHeight }}>
                      <TimeseriesChart
                        echarts={echarts}
                        isDarkMode={isDarkMode}
                        type="line"
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
                        ariaDescription="最近 7 天系统 API 调用量"
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
          </ChartCard>

          <AppCard padding="sm" className="sm:p-5">
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

            <div className="mt-3 grid gap-3 sm:mt-4 md:grid-cols-3 md:[&>*+*]:border-l md:[&>*+*]:border-kumo-line md:[&>*+*]:pl-4 md:[&>*:not(:last-child)]:pr-4">
              <MiniMeter label="CPU" value={hostCpuUsage} detail={`${formatPercent(hostCpuUsage)} / ${stats.host?.cpu?.cores || 0}C`} tone="success" />
              <MiniMeter label="内存" value={hostMemoryUsage} detail={`${formatPercent(hostMemoryUsage)} / ${formatBytes(stats.host?.memory?.total)}`} tone="info" />
              <MiniMeter label="磁盘" value={hostDiskUsage} detail={`${formatPercent(hostDiskUsage)} / ${formatBytes(stats.host?.disk?.total)}`} tone="brand" />
            </div>

            <div className="mt-3 grid gap-x-6 gap-y-2 border-t border-kumo-line pt-2 text-[11px] text-kumo-subtle sm:mt-4 sm:grid-cols-3 sm:pt-3 sm:text-xs">
              <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2">
                <span>运行时间</span>
                <span className="truncate text-right font-semibold text-kumo-strong">{formatDuration(stats.host?.uptime)}</span>
              </div>
              <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2">
                <span>负载</span>
                <span className="truncate text-right font-mono font-semibold text-kumo-strong">{Number.isFinite(hostLoad) ? hostLoad.toFixed(2) : '-'}</span>
              </div>
              <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2">
                <span>磁盘卷</span>
                <span className="truncate text-right font-semibold text-kumo-strong">{stats.host?.disk?.root || '-'}</span>
              </div>
            </div>
          </AppCard>
        </div>

        {/* Right Column: Services & Tools List */}
        <AppCard padding="sm" className="flex min-h-0 flex-col justify-between sm:min-h-[340px] sm:p-5">
          <div className="border-b border-kumo-line pb-2 sm:pb-3.5">
            <h3 className="text-xs font-semibold text-kumo-strong flex items-center gap-1.5 select-none sm:text-sm sm:gap-2">
              <Box className="h-3.5 w-3.5 text-kumo-brand sm:h-4 sm:w-4" />
              服务 & 工具
            </h3>
          </div>

          <div className="flex-1 space-y-2.5 py-2.5 sm:py-3.5">
            {/* Koyeb */}
            <div
              onClick={() => navigateToModule('paas')}
              className="group flex min-h-14 cursor-pointer items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/45 px-3 py-2.5 transition-colors hover:border-kumo-brand/60 hover:bg-kumo-base"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md bg-kumo-badge-purple/10 text-sm text-kumo-badge-purple">
                  <Box className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">Koyeb</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">边缘计算应用服务</p>
                </div>
              </div>
              <div className="flex h-7 min-w-10 items-center justify-center gap-1.5 rounded-md border border-kumo-line bg-kumo-base px-2 text-xs font-semibold text-kumo-strong tabular-nums">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.paas.koyeb.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.koyeb.running}
              </div>
            </div>

            {/* Fly.io */}
            <div
              onClick={() => navigateToModule('paas')}
              className="group flex min-h-14 cursor-pointer items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/45 px-3 py-2.5 transition-colors hover:border-kumo-brand/60 hover:bg-kumo-base"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md bg-kumo-brand/10 text-sm text-kumo-brand">
                  <Send className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">Fly.io</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">全球微型虚拟机</p>
                </div>
              </div>
              <div className="flex h-7 min-w-10 items-center justify-center gap-1.5 rounded-md border border-kumo-line bg-kumo-base px-2 text-xs font-semibold text-kumo-strong tabular-nums">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.paas.fly.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.fly.running}
              </div>
            </div>

            {/* 2FA */}
            <div
              onClick={() => navigateToModule('totp')}
              className="group flex min-h-14 cursor-pointer items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/45 px-3 py-2.5 transition-colors hover:border-kumo-brand/60 hover:bg-kumo-base"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md bg-kumo-success/10 text-sm text-kumo-success">
                  <Shield className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">2FA 安全令牌</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">OTP 动态验证码账号</p>
                </div>
              </div>
              <div className="flex h-7 min-w-10 items-center justify-center gap-1.5 rounded-md border border-kumo-line bg-kumo-base px-2 text-xs font-semibold text-kumo-strong tabular-nums">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.totp.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.totp.total}
              </div>
            </div>

            {/* FileBox */}
            <div
              onClick={() => navigateToModule('filebox')}
              className="group flex min-h-14 cursor-pointer items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/45 px-3 py-2.5 transition-colors hover:border-kumo-brand/60 hover:bg-kumo-base"
            >
              <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md bg-kumo-info-tint text-sm text-kumo-info">
                  <FolderOpen className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-kumo-brand transition-colors">文件分享柜</h4>
                  <p className="mt-0.5 truncate text-[10px] text-kumo-subtle">文件与片段分享柜</p>
                </div>
              </div>
              <div className="flex h-7 min-w-10 items-center justify-center gap-1.5 rounded-md border border-kumo-line bg-kumo-base px-2 text-xs font-semibold text-kumo-strong tabular-nums">
                <span className={`w-1.5 h-1.5 rounded-full ${stats.filebox.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.filebox.total}
              </div>
            </div>
          </div>

          <div className="text-[10px] text-kumo-subtle border-t border-kumo-line pt-2 select-none text-center sm:pt-3">
            点击以上卡片可直接跳转相应模块管理。
          </div>
        </AppCard>

      </div>

    </PageStack>
  );
}

export default DashboardPage;
