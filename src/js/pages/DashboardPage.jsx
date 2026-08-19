import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Chart, ChartLegend, ChartPalette, Meter, Tabs } from '@cloudflare/kumo';
import * as echarts from 'echarts/core';
import { BarChart, LineChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { createSiteFontEcharts } from '../chartFont.js';
import useStore from '../store.js';
import { AppCard, ChartWarmupSkeleton, PageStack, SectionCard } from '../components/ui/AppPrimitives.jsx';
import { DASHBOARD_INVALIDATION_EVENT, readDashboardStatsInvalidatedAt } from '../modules/dashboardInvalidation.js';
import { parseDashboardTrendTimestamp } from '../modules/dashboardMetrics.js';
import { formatFileSize, formatTokens } from '../modules/utils.js';
import {
  Cpu,
  Server,
  Cloud,
  Globe,
  Activity,
  RefreshCw,
  ArrowRight,
  TrendingUp,
  Box,
  Shield,
  FolderOpen,
  PieChart,
  KoyebBrand,
  FlyIoBrand,
  Clock,
} from '../components/Icons.jsx';
import { PublicPageBrandIcon } from '../components/public/PublicPageIconPicker.jsx';

const DEFAULT_DASHBOARD_STATS = {
  host: {
    hostname: '',
    platformLabel: '',
    uptime: 0,
    cpu: { usage: 0, cores: 0, physicalCores: 0, logicalCores: 0, threads: 0, loadAverage: [] },
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
  scheduler: { total: 0, enabled: 0 },
  statusPages: [],
  apiStats: {
    total: { audit: 0, ops: 0, all: 0 },
    trend: [],
  },
};

const DASHBOARD_CACHE_TTL_MS = 30_000;
const DASHBOARD_FETCH_TIMEOUT_MS = 6_000;
const HOST_METRICS_POLL_MS = 2_000;
const HOST_METRICS_FETCH_TIMEOUT_MS = 4_000;
const DASHBOARD_SERVER_STATUS_LIMIT = 7;
const API_TREND_DEFAULT_DAYS = 14;
const API_TREND_NARROW_DAYS = 7;
const API_TREND_MAX_DAYS = 30;
const API_TREND_RANGE_TABS = [
  { value: '7', label: '7 天' },
  { value: '14', label: '14 天' },
  { value: '30', label: '30 天' },
];
const SERVICE_TOOL_ITEM_CLASS = 'group flex h-11 min-w-0 cursor-pointer items-center justify-between gap-2 rounded-md border border-kumo-line bg-kumo-recessed/45 px-2.5 py-1.5 transition-colors hover:border-brand/60 hover:bg-kumo-base cq-sm:h-12 cq-sm:px-3 cq-xl:h-auto cq-xl:min-h-12';
const SERVICE_TOOL_ICON_CLASS = 'flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md text-sm cq-sm:h-8 cq-sm:w-8';
const SERVICE_TOOL_BADGE_CLASS = 'flex h-6 min-w-9 shrink-0 items-center justify-center gap-1 rounded-md border border-kumo-line bg-kumo-base px-1.5 text-[10px] font-semibold text-kumo-strong tabular-nums cq-sm:min-w-10 cq-sm:text-[11px]';

let dashboardStatsCache = null;
let dashboardStatsFetchPromise = null;
let dashboardHostMetricsCache = null;

const isAbortError = (error) => error?.name === 'AbortError';

echarts.use([
  BarChart,
  LineChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

const siteFontEcharts = createSiteFontEcharts(echarts);

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
  const cachedSnapshot = dashboardStatsCache;
  const invalidatedAt = readDashboardStatsInvalidatedAt();
  if (!cachedSnapshot || cachedSnapshot.updatedAt < invalidatedAt) return DEFAULT_DASHBOARD_STATS;
  const cached = cachedSnapshot.stats;
  return {
    ...DEFAULT_DASHBOARD_STATS,
    ...cached,
  };
}

function formatKCount(count) {
  const number = Number(count) || 0;
  return `${(number / 1000).toFixed(1)}K`;
}

function formatCompactCount(count) {
  const number = Number(count) || 0;
  if (number < 1000) return Math.round(number).toString();
  return formatKCount(number);
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
  if (size >= 1024 ** 4) return `${(size / 1024 ** 4).toFixed(1)} T`;
  if (size >= 1024 ** 3) return `${(size / 1024 ** 3).toFixed(1)} G`;
  if (size >= 1024 ** 2) return `${(size / 1024 ** 2).toFixed(1)} M`;
  if (size >= 1024) return `${(size / 1024).toFixed(1)} K`;
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

function formatHostCpuDetail(cpu = {}) {
  const physicalCores = Number(cpu.physicalCores ?? cpu.physical_cores ?? cpu.cores) || 0;
  const logicalCores = Number(cpu.logicalCores ?? cpu.logical_cores ?? cpu.threads) || 0;
  return logicalCores && logicalCores !== physicalCores
    ? `${physicalCores || '-'}C · ${logicalCores}T`
    : `${physicalCores || logicalCores || 0}C`;
}

function normalizeServerStatus(status) {
  if (status === 'online') return 'online';
  if (status === 'error' || status === 'interrupted' || status === 'suspect') return 'error';
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
    return <div className="h-5 w-[128px]" aria-hidden="true" />;
  }

  const capsuleServers = servers.length > 0
    ? servers
    : Array.from({ length: total }, (_, index) => ({
      id: `server-status-${index}`,
      name: `主机 ${index + 1}`,
      status: index < online ? 'online' : index < online + error ? 'error' : 'offline',
  }));
  const visibleServers = capsuleServers.slice(0, DASHBOARD_SERVER_STATUS_LIMIT);
  const hiddenServers = capsuleServers.slice(DASHBOARD_SERVER_STATUS_LIMIT);
  const hiddenCount = Math.max(0, total - visibleServers.length);
  const hiddenTitle = hiddenServers
    .map((server) => {
      const statusMeta = getServerStatusMeta(server.status);
      return `${server.name}: ${statusMeta.label}`;
    })
    .join('\n');

  return (
    <div
      className="flex h-5 w-[128px] items-center justify-end gap-1.5"
      aria-label="主机在线状态"
    >
      <div className="flex min-w-0 flex-1 flex-nowrap items-center justify-end gap-1.5">
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
      </div>
      {hiddenCount > 0 && (
        <Badge
          variant="secondary"
          className="h-4 min-w-5 shrink-0 rounded-full px-1.5 text-[9px] font-bold leading-none tabular-nums"
          title={hiddenTitle}
          aria-label={`还有 ${hiddenCount} 台主机: ${hiddenTitle.replace(/\n/g, '，')}`}
        >
          +{hiddenCount}
        </Badge>
      )}
    </div>
  );
}

const getStatusPageUrl = (page) => {
  const domain = String(page?.domain || '').trim().replace(/^https?:\/\//i, '').replace(/\/+$/g, '');
  if (domain) return `https://${domain}`;
  const slug = encodeURIComponent(page?.slug || '');
  if (!slug) return '';
  if (page.kind === 'server') return `/servers/${slug}`;
  if (page.kind === 'github') return `/github/${slug}`;
  return `/status/${slug}`;
};

const STATUS_PAGE_SHORTCUT_ICON_CLASS = 'flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand';

function StatusPageShortcutCard({ page }) {
  const url = getStatusPageUrl(page);

  return (
    <AppCard
      padding="none"
      interactive
      onClick={() => url && window.open(url, '_blank', 'noopener,noreferrer')}
      className="group flex h-11 min-w-0 cursor-pointer items-center justify-between gap-2.5 px-3 cq-sm:h-12 cq-sm:px-3.5"
    >
      <span className="flex min-w-0 items-center gap-2.5">
        <span className={STATUS_PAGE_SHORTCUT_ICON_CLASS}>
          <PublicPageBrandIcon pageKind={page.kind} config={page.config} iconClassName="h-4 w-4" customIconClassName="h-4 w-4" />
        </span>
        <span className="min-w-0 truncate text-sm font-semibold text-kumo-strong group-hover:text-brand">
          {page.title || page.slug}
        </span>
      </span>
      <ArrowRight className="h-3.5 w-3.5 shrink-0 text-kumo-subtle transition-transform group-hover:translate-x-0.5 group-hover:text-brand" />
    </AppCard>
  );
}

function MiniMeter({ label, value, detail, tone = 'brand' }) {
  const indicatorClassName = {
    brand: 'bg-brand',
    info: 'bg-kumo-info',
    success: 'bg-kumo-success',
    warning: 'bg-kumo-warning',
  }[tone] || 'bg-brand';

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
      className="group grid min-h-[92px] cursor-pointer grid-rows-[auto_1fr_auto] gap-1.5 overflow-hidden p-2.5 cq-sm:min-h-[112px] cq-sm:p-3.5"
    >
      <div className="flex min-w-0 items-center justify-between gap-1.5">
        <div className="flex min-w-0 items-center gap-1.5 cq-sm:gap-2">
          <div className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md cq-sm:h-8 cq-sm:w-8 ${iconClassName}`}>
            <Icon className="h-3 w-3 cq-sm:h-3.5 cq-sm:w-3.5" />
          </div>
          <span className="min-w-0 truncate text-[10px] font-semibold text-kumo-subtle cq-sm:text-[11px]">
            {label}
          </span>
        </div>
        <Badge className={`shrink-0 whitespace-nowrap text-[9px] cq-sm:text-[11px] ${badgeClassName}`}>
          {badge}
        </Badge>
      </div>

      <div className="flex h-8 min-w-0 items-center justify-between gap-2 cq-sm:h-9">
        <span className="flex min-w-0 items-baseline gap-0.5 truncate text-[1.15rem] font-bold leading-none text-kumo-strong tabular-nums cq-sm:text-[1.45rem] cq-sm:gap-1">
          {value}
          <span className="truncate text-[10px] font-normal leading-none text-kumo-subtle cq-sm:text-[11px]">
            {unit}
          </span>
        </span>
        {statusVisual && (
          <div className="hidden h-5 shrink-0 items-center justify-end cq-sm:flex">
            {statusVisual}
          </div>
        )}
      </div>

      <div className="flex min-h-5 items-center justify-between gap-1.5 border-t border-kumo-line pt-1.5 text-[10px] leading-tight text-kumo-subtle transition-colors group-hover:text-kumo-strong cq-sm:min-h-6 cq-sm:pt-2 cq-sm:text-[11px]">
        <span className={`min-w-0 truncate ${detailClassName}`}>
          {detail}
        </span>
        <ArrowRight className="h-2.5 w-2.5 shrink-0 cq-sm:h-3 cq-sm:w-3" />
      </div>
    </AppCard>
  );
}

function DashboardPage({ onNavigate } = {}) {
  const { setMainActiveTab, setAppProcessUptimeSeconds, theme } = useStore();
  const isDarkMode = theme === 'dark';
  const isCompactViewport = useMediaQuery('(max-width: 640px)');
  const isNarrowApiTrend = useMediaQuery('(max-width: 1279px)');
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
  const fetchDashboardStats = useCallback(async (showLoading = true, { force = false } = {}) => {
    const cached = dashboardStatsCache;
    const invalidatedAt = readDashboardStatsInvalidatedAt();
    const cacheFresh = cached
      && cached.updatedAt >= invalidatedAt
      && Date.now() - cached.updatedAt < DASHBOARD_CACHE_TTL_MS;

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

    const headers = {
      'Content-Type': 'application/json',
    };
    const previousStats = dashboardStatsCache?.stats
      ? { ...DEFAULT_DASHBOARD_STATS, ...dashboardStatsCache.stats }
      : DEFAULT_DASHBOARD_STATS;

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

    // 8. 获取定时任务数量
    const fetchScheduler = async () => {
      try {
        const data = await fetchJson('/api/scheduler/tasks');
        if (data.success && Array.isArray(data.data)) {
          const val = {
            total: data.data.length,
            enabled: data.data.filter((t) => t.enabled === 1).length,
          };
          updateSegment('scheduler', val);
          return val;
        }
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] Scheduler fetch failed:', e);
        }
      }
      updateSegment('scheduler', previousStats.scheduler || { total: 0, enabled: 0 });
      return previousStats.scheduler || { total: 0, enabled: 0 };
    };

    // API 调用趋势卡片只等自身数据：loading 状态在 fetchApiStats 结束时清除，
    // 不被其余八路接口（cloudflare/uptime/server 等）的耗时拖住。
    const fetchApiStats = async () => {
      try {
        try {
          const data = await fetchJson(`/api/system/api-stats?days=${API_TREND_MAX_DAYS}`);
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
      } finally {
        setLoading(false);
      }
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

    const fetchStatusPages = async () => {
      const normalize = (item, kind) => ({ ...item, kind });
      const enabled = (item) => item?.public !== false && item?.config?.showOnDashboard === true;
      try {
        const [uptimeResult, serverResult, githubResult] = await Promise.allSettled([
          fetchJson('/api/uptime/status-pages'),
          fetchJson('/api/server/status-pages'),
          fetchJson('/api/github/public-pages'),
        ]);
        const uptimePages = uptimeResult.status === 'fulfilled' && Array.isArray(uptimeResult.value?.data)
          ? uptimeResult.value.data.filter(enabled).map((item) => normalize(item, 'uptime'))
          : [];
        const serverPages = serverResult.status === 'fulfilled' && Array.isArray(serverResult.value?.data)
          ? serverResult.value.data.filter(enabled).map((item) => normalize(item, 'server'))
          : [];
        const githubPages = githubResult.status === 'fulfilled' && Array.isArray(githubResult.value?.data)
          ? githubResult.value.data.filter(enabled).map((item) => normalize(item, 'github'))
          : [];
        const val = [...uptimePages, ...serverPages, ...githubPages];
        updateSegment('statusPages', val);
        return val;
      } catch (e) {
        if (!isAbortError(e)) {
          console.error('[Dashboard] Status pages fetch failed:', e);
        }
      }
      updateSegment('statusPages', previousStats.statusPages || []);
      return previousStats.statusPages || [];
    };

    const request = Promise.allSettled([
      fetchServers(),
      fetchPaaS(),
      fetchDns(),
      fetchUptime(),
      fetchFilebox(),
      fetchTotp(),
      fetchApiStats(),
      fetchStatusPages(),
      fetchScheduler(),
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
        statusPages: results[7].status === 'fulfilled' ? results[7].value : previousStats.statusPages || [],
        scheduler: results[8].status === 'fulfilled' ? results[8].value : previousStats.scheduler || { total: 0, enabled: 0 },
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
      // loading 由 fetchApiStats 的 finally 负责收尾（趋势卡只等自身数据）。
    }
  }, []);

  useEffect(() => {
    const cached = dashboardStatsCache;
    if (cached) {
      setStats(cached.stats);
    }

    const invalidatedAt = readDashboardStatsInvalidatedAt();
    const cacheStale = !cached
      || cached.updatedAt < invalidatedAt
      || Date.now() - cached.updatedAt > DASHBOARD_CACHE_TTL_MS;
    if (cacheStale) {
      fetchDashboardStats(!cached);
    }
  }, [fetchDashboardStats]);

  useEffect(() => {
    const handleInvalidate = () => {
      dashboardStatsCache = null;
      dashboardStatsFetchPromise = null;
      fetchDashboardStats(false, { force: true });
    };
    window.addEventListener(DASHBOARD_INVALIDATION_EVENT, handleInvalidate);
    return () => window.removeEventListener(DASHBOARD_INVALIDATION_EVENT, handleInvalidate);
  }, [fetchDashboardStats]);

  useEffect(() => {
    let stopped = false;
    let activeController = null;

    const fetchHostMetrics = async () => {
      if (activeController) return;

      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), HOST_METRICS_FETCH_TIMEOUT_MS);
      activeController = controller;

      try {
        const response = await fetch('/api/system/host-metrics', {
          headers: {
            'Content-Type': 'application/json',
          },
          signal: controller.signal,
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);

        const data = await response.json();
        if (!data.success || !data.data || stopped) return;

        dashboardHostMetricsCache = data.data;
        setAppProcessUptimeSeconds(data.data.process?.uptime);
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
    const interval = window.setInterval(() => {
      if (document.hidden) return;
      fetchHostMetrics();
    }, HOST_METRICS_POLL_MS);

    return () => {
      stopped = true;
      window.clearInterval(interval);
      activeController?.abort();
    };
  }, [setAppProcessUptimeSeconds]);


  // null = 跟随视口自适应（窄屏 7 天，否则 14 天）；数字 = 用户手动固定。
  const [apiTrendRange, setApiTrendRange] = useState(null);
  const apiTrendDays = apiTrendRange ?? (isNarrowApiTrend ? API_TREND_NARROW_DAYS : API_TREND_DEFAULT_DAYS);
  const apiTrendRaw = stats.apiStats?.trend || [];
  const apiTrend = useMemo(
    () => (apiTrendDays < apiTrendRaw.length ? apiTrendRaw.slice(-apiTrendDays) : apiTrendRaw),
    [apiTrendDays, apiTrendRaw],
  );
  const apiTrendAudit = apiTrend.reduce((sum, point) => sum + (Number(point.audit) || 0), 0);
  const apiTrendOps = apiTrend.reduce((sum, point) => sum + (Number(point.ops) || 0), 0);
  const apiTrendTotal = apiTrendAudit + apiTrendOps;

  // 与模型趋势一致：三种量纲（请求/词元/流量）归一为各自所选区间峰值 0-100% 共享一轴，
  // Legend 点击隔离某序列时改用该序列的原生单位与绝对量展示。
  const apiTrendSeries = useMemo(() => {
    const configs = [
      {
        key: 'requests',
        name: '请求次数',
        unit: '次',
        color: ChartPalette.categorical(0, isDarkMode),
        pick: (point) => (Number(point.audit) || 0) + (Number(point.ops) || 0),
        formatValue: (value) => formatCompactCount(value),
      },
      {
        key: 'tokens',
        name: '词元用量',
        unit: '词元',
        color: ChartPalette.categorical(1, isDarkMode),
        pick: (point) => Number(point.tokens) || 0,
        formatValue: formatTokens,
      },
      {
        key: 'traffic',
        name: '订阅流量',
        unit: '',
        color: ChartPalette.categorical(2, isDarkMode),
        pick: (point) => Number(point.traffic) || 0,
        formatValue: formatFileSize,
      },
    ];
    return configs.map((config) => {
      const data = apiTrend
        .map((point) => {
          const timestamp = parseDashboardTrendTimestamp(point);
          return Number.isFinite(timestamp) ? [timestamp, config.pick(point)] : null;
        })
        .filter(Boolean);
      const total = data.reduce((sum, [, value]) => sum + value, 0);
      const max = data.reduce((peak, [, value]) => Math.max(peak, value), 0);
      return { ...config, data, total, max };
    });
  }, [apiTrend, isDarkMode]);

  // null = 全部显示（相对归一化）；否则为被隔离序列的 key（原生单位）。
  const [apiIsolatedSeries, setApiIsolatedSeries] = useState(null);

  // 类别轴：与模型趋势一致，按日期对齐刻度，不用时间序列轴。
  const apiTrendLabels = useMemo(() => {
    const points = apiTrendSeries[0]?.data || [];
    return points.map(([timestamp]) => {
      const date = new Date(timestamp);
      return `${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
    });
  }, [apiTrendSeries]);

  const apiChartOptions = useMemo(() => {
    const isolated = apiIsolatedSeries
      ? apiTrendSeries.find((item) => item.key === apiIsolatedSeries)
      : null;
    const visibleSeries = isolated ? [isolated] : apiTrendSeries;
    const normalized = !isolated;

    const series = visibleSeries.map((item) => {
      const values = item.data.map(([, value]) => (
        normalized && item.max > 0 ? (value / item.max) * 100 : value
      ));
      return {
        name: item.name,
        type: 'line',
        data: values,
        smooth: true,
        showSymbol: true,
        symbolSize: 4,
        lineStyle: { width: 2, color: item.color },
        itemStyle: { color: item.color },
        emphasis: { focus: 'series' },
      };
    });

    const axisColor = ChartPalette.text('primary', isDarkMode);
    const tooltipFormat = (params) => {
      if (!Array.isArray(params)) return '';
      const lines = params.map((param) => {
        const match = visibleSeries.find((item) => item.name === param.seriesName);
        const value = Number(param.value) || 0;
        const text = !normalized && match
          ? (match.unit ? `${match.formatValue(value)} ${match.unit}` : match.formatValue(value))
          : `${value.toFixed(1)}%`;
        return `${param.marker}${param.seriesName}: ${text}`;
      });
      return lines.join('<br/>');
    };

    return {
      aria: {
        enabled: true,
        label: { description: `最近 ${apiTrendDays} 天系统 API 调用 / 词元 / 订阅流量趋势` },
      },
      backgroundColor: 'transparent',
      grid: { left: 8, right: 16, top: 28, bottom: 4, containLabel: true },
      tooltip: { trigger: 'axis', confine: true, dangerousHtmlFormatter: tooltipFormat },
      xAxis: {
        type: 'category',
        data: apiTrendLabels,
        boundaryGap: false,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        axisLabel: { show: false },
        splitLine: { show: true, lineStyle: { type: 'dashed', width: 1 } },
      },
      series,
    };
  }, [apiTrendSeries, apiTrendLabels, apiIsolatedSeries, isDarkMode, apiTrendDays]);

  const handleApiLegendClick = (key) => {
    setApiIsolatedSeries((prev) => (prev === key ? null : key));
  };

  const apiStatsDetailText = apiTrendTotal > 0
    ? `读取 ${formatKCount(apiTrendAudit)}次 / 变更 ${formatKCount(apiTrendOps)}次`
    : '暂无系统 API 调用记录';
  const hasApiTrendCalls = apiTrendSeries.some((series) => series.data.length >= 2 && series.total > 0);
  const apiTrendStatusText = apiTrendTotal > 0
    ? `最近 ${apiTrendDays} 天系统 API 调用 / 词元 / 订阅流量趋势`
    : `最近 ${apiTrendDays} 天暂无系统 API 调用记录`;

  const hostCpuUsage = clampPercent(stats.host?.cpu?.usage);
  const hostMemoryUsage = clampPercent(stats.host?.memory?.usage);
  const hostDiskUsage = clampPercent(stats.host?.disk?.usage);
  const hostLoad = stats.host?.cpu?.loadAverage?.[0];
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
        ? stats.servers.offline > 0 && stats.servers.error > 0
          ? `${stats.servers.offline} 台离线，${stats.servers.error} 台异常`
          : stats.servers.error > 0
            ? `${stats.servers.error} 台异常`
            : '全部主机发生故障'
        : stats.servers.error > 0
          ? `${stats.servers.error} 台异常`
          : `${stats.servers.offline} 台离线`;
  const serverDetailClassName = stats.servers.total > 0 && stats.servers.online < stats.servers.total
    ? (stats.servers.online === 0 ? 'text-kumo-danger font-semibold' : 'text-kumo-warning font-semibold')
    : '';
  const serverStatusItems = Array.isArray(stats.servers.items) ? stats.servers.items : [];
  const uptimeBadgeClassName = stats.uptime.down > 0
    ? 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20'
    : 'text-kumo-success bg-kumo-success/10 border-kumo-success/20';
  const uptimeDetailText = stats.uptime.down > 0 ? `${stats.uptime.down} 个监测故障` : '全部在线';
  const uptimeDetailClassName = stats.uptime.down > 0 ? 'text-kumo-danger font-semibold' : '';
  const dashboardStatusPages = Array.isArray(stats.statusPages) ? stats.statusPages : [];

  return (
    <PageStack className="gap-3 cq-sm:gap-4">
      {/* ==================== Stats Grid (5 Cards) ==================== */}
      <div className="grid w-full grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-xl:grid-cols-5">
        
        <DashboardOverviewCard
          onClick={() => navigateToModule('server')}
          icon={Server}
          iconClassName="bg-kumo-info-tint text-kumo-info"
          badge={`${stats.servers.online}/${stats.servers.total} 在线`}
          badgeClassName={serverBadgeClassName}
          label="主机"
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
          detail={`${stats.paas.koyeb.running + stats.paas.fly.running} 个运行中`}
        />

        <DashboardOverviewCard
          onClick={() => navigateToModule('dns')}
          icon={Globe}
          iconClassName="bg-kumo-badge-orange/10 text-kumo-badge-orange"
          badge="Cloudflare"
          badgeClassName="text-kumo-subtle bg-kumo-recessed border-kumo-line"
          label="域名解析"
          value={stats.dns.zones}
          unit="个域名"
          detail="Cloudflare 区域"
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
          icon={PieChart}
          iconClassName="bg-brand/10 text-brand"
          badge="系统 API"
          badgeClassName="text-kumo-subtle bg-kumo-recessed border-kumo-line"
          label="系统 API 调用"
          value={formatKCount(apiTrendTotal)}
          unit="次"
          detail={apiStatsDetailText}
        />

      </div>

      {/* ==================== Detail Column Split ==================== */}
      <div className="grid grid-cols-1 items-stretch gap-3 cq-sm:gap-4 cq-xl:grid-cols-[minmax(0,2fr)_minmax(360px,1fr)]">
        
        <SectionCard
          title="API 调用趋势"
          icon={<TrendingUp className="h-4 w-4 text-brand" />}
          action={(
            <Tabs
              variant="segmented"
              size="sm"
              tabs={API_TREND_RANGE_TABS}
              value={String(apiTrendDays)}
              onValueChange={(value) => setApiTrendRange(Number(value))}
            />
          )}
          bodyClassName="flex min-h-0 flex-1 flex-col p-2.5 cq-sm:p-5"
        >
          <div className="flex flex-nowrap gap-x-4 overflow-x-auto overscroll-x-contain px-1 pb-1 touch-pan-x scrollbar-thin">
            {apiTrendSeries.map((series) => (
              <ChartLegend.LargeItem
                key={series.key}
                name={series.name}
                color={series.color}
                value={series.formatValue(series.total)}
                unit={series.unit}
                inactive={apiIsolatedSeries !== null && apiIsolatedSeries !== series.key}
                onClick={() => handleApiLegendClick(series.key)}
                loading={loading}
              />
            ))}
          </div>
          <div className="min-w-0 overflow-hidden" style={{ height: apiChartHeight }}>
            {hasApiTrendCalls ? (
              <Chart
                echarts={siteFontEcharts}
                isDarkMode={isDarkMode}
                options={apiChartOptions}
                height={apiChartHeight}
                optionUpdateBehavior={{ notMerge: true }}
              />
            ) : loading ? (
              <ChartWarmupSkeleton height={apiChartHeight} bars={8} />
            ) : (
              <div className="flex h-full items-center justify-center text-center text-xs text-kumo-subtle">
                {apiTrendStatusText}
              </div>
            )}
          </div>
        </SectionCard>

        {/* Right Column: Services & Tools List */}
        <SectionCard
          title="模块入口"
          icon={<Box className="h-4 w-4 text-brand" />}
          className="order-2 h-full min-w-0 cq-xl:col-start-2 cq-xl:row-span-2 cq-xl:row-start-1"
          bodyClassName="flex min-h-0 flex-1 flex-col p-2.5 cq-sm:p-3"
        >
          <div className="grid flex-1 auto-rows-fr grid-cols-2 gap-2 cq-xl:grid-cols-1">
            {/* Koyeb */}
            <div
              onClick={() => navigateToModule('paas')}
              className={SERVICE_TOOL_ITEM_CLASS}
            >
              <div className="flex min-w-0 items-center gap-2.5 cq-sm:gap-3">
                <div className={`${SERVICE_TOOL_ICON_CLASS} bg-kumo-badge-purple/10 text-kumo-badge-purple`}>
                  <KoyebBrand className="h-3.5 w-3.5 cq-sm:h-4 cq-sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-brand transition-colors">Koyeb</h4>
                  <p className="mt-0.5 hidden truncate text-[10px] text-kumo-subtle cq-sm:block">边缘计算应用服务</p>
                </div>
              </div>
              <div className={SERVICE_TOOL_BADGE_CLASS}>
                <span className={`w-1 h-1 rounded-full cq-sm:w-1.5 cq-sm:h-1.5 ${stats.paas.koyeb.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.koyeb.running}
              </div>
            </div>

            {/* Fly.io */}
            <div
              onClick={() => navigateToModule('paas')}
              className={SERVICE_TOOL_ITEM_CLASS}
            >
              <div className="flex min-w-0 items-center gap-2.5 cq-sm:gap-3">
                <div className={`${SERVICE_TOOL_ICON_CLASS} bg-brand/10 text-brand`}>
                  <FlyIoBrand className="h-3.5 w-3.5 cq-sm:h-4 cq-sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-kumo-strong group-hover:text-brand transition-colors">Fly.io</h4>
                  <p className="mt-0.5 hidden truncate text-[10px] text-kumo-subtle cq-sm:block">全球微型虚拟机</p>
                </div>
              </div>
              <div className={SERVICE_TOOL_BADGE_CLASS}>
                <span className={`w-1 h-1 rounded-full cq-sm:w-1.5 cq-sm:h-1.5 ${stats.paas.fly.running > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.paas.fly.running}
              </div>
            </div>

            {/* Uptime */}
            <div
              onClick={() => navigateToModule('uptime')}
              className={SERVICE_TOOL_ITEM_CLASS}
            >
              <div className="flex min-w-0 items-center gap-2.5 cq-sm:gap-3">
                <div className={`${SERVICE_TOOL_ICON_CLASS} bg-kumo-success/10 text-kumo-success`}>
                  <Activity className="h-3.5 w-3.5 cq-sm:h-4 cq-sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-brand transition-colors">可用性监测</h4>
                  <p className="mt-0.5 hidden truncate text-[10px] text-kumo-subtle cq-sm:block">HTTP、TCP 与 Ping 监测</p>
                </div>
              </div>
              <div className={SERVICE_TOOL_BADGE_CLASS}>
                <span className={`w-1 h-1 rounded-full cq-sm:w-1.5 cq-sm:h-1.5 ${stats.uptime.total > 0 && stats.uptime.down === 0 ? 'bg-kumo-success' : stats.uptime.down > 0 ? 'bg-kumo-danger' : 'bg-kumo-fill'}`} />
                {stats.uptime.total}
              </div>
            </div>

            {/* Scheduler */}
            <div
              onClick={() => navigateToModule('scheduler')}
              className={SERVICE_TOOL_ITEM_CLASS}
            >
              <div className="flex min-w-0 items-center gap-2.5 cq-sm:gap-3">
                <div className={`${SERVICE_TOOL_ICON_CLASS} bg-kumo-warning/10 text-kumo-warning`}>
                  <Clock className="h-3.5 w-3.5 cq-sm:h-4 cq-sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-brand transition-colors">定时任务</h4>
                  <p className="mt-0.5 hidden truncate text-[10px] text-kumo-subtle cq-sm:block">任务、工作流与运行记录</p>
                </div>
              </div>
              <div className={SERVICE_TOOL_BADGE_CLASS}>
                <span className={`w-1 h-1 rounded-full cq-sm:w-1.5 cq-sm:h-1.5 ${stats.scheduler.enabled > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.scheduler.total}
              </div>
            </div>

            {/* 2FA */}
            <div
              onClick={() => navigateToModule('totp')}
              className={SERVICE_TOOL_ITEM_CLASS}
            >
              <div className="flex min-w-0 items-center gap-2.5 cq-sm:gap-3">
                <div className={`${SERVICE_TOOL_ICON_CLASS} bg-kumo-success/10 text-kumo-success`}>
                  <Shield className="h-3.5 w-3.5 cq-sm:h-4 cq-sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-brand transition-colors">双因子认证</h4>
                  <p className="mt-0.5 hidden truncate text-[10px] text-kumo-subtle cq-sm:block">OTP 动态验证码账号</p>
                </div>
              </div>
              <div className={SERVICE_TOOL_BADGE_CLASS}>
                <span className={`w-1 h-1 rounded-full cq-sm:w-1.5 cq-sm:h-1.5 ${stats.totp.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.totp.total}
              </div>
            </div>

            {/* FileBox */}
            <div
              onClick={() => navigateToModule('filebox')}
              className={SERVICE_TOOL_ITEM_CLASS}
            >
              <div className="flex min-w-0 items-center gap-2.5 cq-sm:gap-3">
                <div className={`${SERVICE_TOOL_ICON_CLASS} bg-kumo-info-tint text-kumo-info`}>
                  <FolderOpen className="h-3.5 w-3.5 cq-sm:h-4 cq-sm:w-4" />
                </div>
                <div className="min-w-0">
                  <h4 className="truncate text-xs font-bold text-kumo-strong group-hover:text-brand transition-colors">文件分享柜</h4>
                  <p className="mt-0.5 hidden truncate text-[10px] text-kumo-subtle cq-sm:block">文件与片段分享柜</p>
                </div>
              </div>
              <div className={SERVICE_TOOL_BADGE_CLASS}>
                <span className={`w-1 h-1 rounded-full cq-sm:w-1.5 cq-sm:h-1.5 ${stats.filebox.total > 0 ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
                {stats.filebox.total}
              </div>
            </div>
          </div>
        </SectionCard>

        {dashboardStatusPages.length > 0 && (
          <SectionCard
            title="状态页"
            icon={<Activity className="h-4 w-4 text-brand" />}
            className="order-3 min-w-0 cq-xl:col-span-2 cq-xl:row-start-3"
            bodyClassName="p-2.5 cq-sm:p-3"
          >
            <div className="grid w-full auto-rows-fr grid-cols-2 gap-2 cq-lg:grid-cols-3 cq-xl:grid-cols-5">
              {dashboardStatusPages.map((page) => (
                <StatusPageShortcutCard key={`${page.kind}-${page.id || page.slug}`} page={page} />
              ))}
            </div>
          </SectionCard>
        )}

        <SectionCard
          title={(
            <span className="flex min-w-0 items-center gap-3">
              <span className="shrink-0">宿主机性能</span>
              <span className="hidden min-w-0 flex-col gap-0.5 text-[10px] font-normal leading-tight text-kumo-subtle cq-sm:flex cq-sm:text-[11px]">
                <span className="truncate">主机: {stats.host?.hostname || '等待采样'}</span>
                <span className="truncate">系统: {stats.host?.platformLabel || '本机运行环境'}</span>
              </span>
            </span>
          )}
          icon={<Cpu className="h-4 w-4 text-kumo-success" />}
          className="order-4 min-w-0 cq-xl:col-span-1 cq-xl:row-start-2"
          bodyClassName="p-2.5 cq-sm:p-5"
          meta={(
            <span
              className="w-fit rounded border border-kumo-success/20 bg-kumo-success/10 px-2 py-0.5 text-[11px] font-semibold text-kumo-success"
              title="运行时间"
              aria-label={`运行时间 ${formatDuration(stats.host?.uptime)}`}
            >
              {formatDuration(stats.host?.uptime)}
            </span>
          )}
        >
          <div className="grid gap-2.5 cq-md:grid-cols-3 cq-md:gap-3 cq-md:[&>*+*]:border-l cq-md:[&>*+*]:border-kumo-line cq-md:[&>*+*]:pl-4 cq-md:[&>*:not(:last-child)]:pr-4">
            <MiniMeter label={`CPU (${Number.isFinite(hostLoad) ? hostLoad.toFixed(2) : '-'})`} value={hostCpuUsage} detail={formatHostCpuDetail(stats.host?.cpu)} tone="success" />
            <MiniMeter label="内存" value={hostMemoryUsage} detail={`${formatBytes(stats.host?.memory?.used)} / ${formatBytes(stats.host?.memory?.total)}`} tone="info" />
            <MiniMeter label={`磁盘 (${stats.host?.disk?.root || '-'})`} value={hostDiskUsage} detail={`${formatBytes(stats.host?.disk?.used)} / ${formatBytes(stats.host?.disk?.total)}`} tone="brand" />
          </div>
        </SectionCard>

      </div>

    </PageStack>
  );
}

export default DashboardPage;
