// TilesBoard —— 卡片式图块看板（可嵌入正式仪表盘或独立 demo 页）。
// 消费 src/js/pages/tiles/ 组件库：TileGrid 拖拽/档位缩放/响应式列数，TileFrame/TileChart/StatValue 卡片体系。
// 布局按移动端/桌面端分桶保存到后端用户设置（data.db，云端）；顶栏提供时间范围/增删指标/刷新/重置。
import React, { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Button, DropdownMenu, Switch } from '@cloudflare/kumo';
import { Select } from '@cloudflare/kumo/components/select';
import { SquaresFour } from '@phosphor-icons/react';
import { TimeRangePicker } from '../../components/ui/TimeRangePicker.jsx';
import { TileGrid, TileFrame } from './index.js';
import useStore from '../../store.js';
import { HeaderToolsContext } from '../../modules/headerToolsContext.js';
import {
  CACHE_TTL,
  FETCH_TIMEOUT_MS,
  HOST_POLL_MS,
  MAX_COLS,
  MIN_COLS,
  RANGE_STORAGE_KEY,
  TILE_DEFS,
  TILE_DEFS_BY_ID,
  cacheGet,
  cacheSet,
  loadRangeFromStorage,
  packDefaultLayout,
  tileDensity,
  widthTier,
} from './constants.js';
import { fetchWithTimeout, normalizeServerStatus, parseCfTime, toArray } from './utils.js';
import ApiTrendMultiTile from './ApiTrendMultiTile.jsx';
import ApiTokensTile from './ApiTokensTile.jsx';
import OpenaiRequestsTile from './OpenaiRequestsTile.jsx';
import OpenaiLatencyTile from './OpenaiLatencyTile.jsx';
import OpenaiErrorsTile from './OpenaiErrorsTile.jsx';
import HostCpuTile from './HostCpuTile.jsx';
import UptimeTile from './UptimeTile.jsx';
import CfZoneTile from './CfZoneTile.jsx';
import ServerStatusTile from './ServerStatusTile.jsx';
import PaasTile from './PaasTile.jsx';
import SchedulerTile from './SchedulerTile.jsx';
import ModuleToolsTile from './ModuleToolsTile.jsx';
import StatusPagesTile from './StatusPagesTile.jsx';

export default function TilesBoard() {
  const theme = useStore((s) => s.theme);
  const isDarkMode = theme === 'dark';
  const setAppProcessUptimeSeconds = useStore((s) => s.setAppProcessUptimeSeconds);
  const headerToolsEl = useContext(HeaderToolsContext); // 正式面板：控制栏 portal 到面包屑栏；demo 页无 Provider 则内联
  const [rangeDays, setRangeDays] = useState(() => loadRangeFromStorage()?.days ?? 14);
  const [rangeLabel, setRangeLabel] = useState(() => loadRangeFromStorage()?.label ?? '过去 14 天');
  const storedRange = loadRangeFromStorage();
  const [rangeMinutes, setRangeMinutes] = useState(() => {
    const m = storedRange?.minutes;
    return Number.isFinite(m) && m > 0 && m < 1440 ? m : null;
  });
  // 按列数分桶：2~8 列各一套独立布局。当前列数取 TileGrid 容器实际宽度（useContainerWidth 上报），
  // 侧栏 AI 面板让位后主内容变窄 → 列数随之变化，而不是按视口固定。
  const [cols, setCols] = useState(5);
  const [layouts, setLayouts] = useState(null); // { '2'..'8' }，null = 云端布局尚未加载
  const layout = layouts ? layouts[cols] : null;
  const setLayout = useCallback((updater) => {
    setLayouts((prev) => {
      const base = prev || {};
      const current = base[cols] ?? null;
      const next = typeof updater === 'function' ? updater(current) : updater;
      return { ...base, [cols]: next };
    });
  }, [cols]);
  const [cloudReady, setCloudReady] = useState(false);

  const [apiStats, setApiStats] = useState(null);
  const [apiStatsLoading, setApiStatsLoading] = useState(false);
  const [openaiData, setOpenaiData] = useState(null);
  const [openaiLoading, setOpenaiLoading] = useState(false);
  const [host, setHost] = useState(null);
  const [uptime, setUptime] = useState(null);
  const [dash, setDash] = useState(null);

  const [cfAccounts, setCfAccounts] = useState([]);
  const [cfZones, setCfZones] = useState([]);
  const [cfAccountId, setCfAccountId] = useState('');
  const [cfZoneId, setCfZoneId] = useState('all'); // 'all' = 全部 Zone 聚合
  const [cfRange, setCfRange] = useState(() => loadRangeFromStorage()?.cfRange ?? '24h');
  const [cfData, setCfData] = useState(null);
  const [cfLoading, setCfLoading] = useState(false);

  const loadApiStats = useCallback(async () => {
    const cacheKey = `apiStats:${rangeDays}${rangeMinutes ? `:${rangeMinutes}` : ''}`;
    const cached = cacheGet(cacheKey);
    if (cached) setApiStats(cached);
    if (!cached) setApiStatsLoading(true);
    try {
      const q = new URLSearchParams({ days: String(rangeDays) });
      if (rangeMinutes) q.set('minutes', String(rangeMinutes));
      const res = await fetchWithTimeout(`/api/system/api-stats?${q.toString()}`);
      const json = await res.json().catch(() => null);
      if (json?.success && json.data) {
        setApiStats(json.data);
        cacheSet(cacheKey, json.data, CACHE_TTL.apiStats);
      }
    } catch (err) {
      console.error('[TilesDemo] api-stats', err);
    } finally {
      setApiStatsLoading(false);
    }
  }, [rangeDays, rangeMinutes]);

  const loadOpenai = useCallback(async () => {
    const cacheKey = `openai:${rangeDays}${rangeMinutes ? `:${rangeMinutes}` : ''}`;
    const cached = cacheGet(cacheKey);
    if (cached) setOpenaiData(cached);
    if (!cached) setOpenaiLoading(true);
    try {
      const gran = rangeMinutes ? 'hour' : rangeDays <= 1 ? 'hour' : 'day';
      const q = new URLSearchParams({ days: String(Math.max(1, rangeDays)), granularity: gran });
      if (rangeMinutes) q.set('minutes', String(rangeMinutes));
      const res = await fetchWithTimeout(`/api/openai/analytics/charts?${q.toString()}`);
      const json = await res.json().catch(() => null);
      if (json && Array.isArray(json.daily)) {
        setOpenaiData(json);
        cacheSet(cacheKey, json, CACHE_TTL.openai);
      }
    } catch (err) {
      console.error('[TilesDemo] openai charts', err);
    } finally {
      setOpenaiLoading(false);
    }
  }, [rangeDays, rangeMinutes]);

  const loadUptime = useCallback(async () => {
    const cached = cacheGet('uptime');
    if (cached) setUptime(cached);
    try {
      const res = await fetchWithTimeout('/api/uptime/monitors');
      const json = await res.json().catch(() => null);
      const monitors = toArray(json);
      let up = 0;
      monitors.forEach((m) => {
        if (!m.active) return;
        if (m.lastHeartbeat) {
          const status = m.lastHeartbeat.status;
          if (status === 1 || status === 'up') up += 1;
        } else {
          up += 1;
        }
      });
      const uptimeData = { total: monitors.length, up, items: monitors };
      setUptime(uptimeData);
      cacheSet('uptime', uptimeData, CACHE_TTL.uptime);
    } catch (err) {
      console.error('[TilesDemo] uptime', err);
    }
  }, []);

  // 仪表盘信息汇总（服务器 / PaaS / DNS / 文件柜 / TOTP / 调度 / 状态页）。
  // 分快慢两批结算：快批（本地 SQLite/CF 列表）先行渲染，服务器/定时任务/状态页等卡不等慢批；
  // 慢批（Koyeb/Fly 直连外部，可达 8s+，超时 16s）独立补齐 PaaS 卡，失败/超时只影响单卡，不拖垮整个统计组。
  const DASH_FAST_SOURCES = [
    { key: 'servers', url: '/api/server/accounts' },
    { key: 'dns', url: '/api/cloudflare/zones' },
    { key: 'filebox', url: '/api/filebox/history' },
    { key: 'totp', url: '/api/totp/accounts' },
    { key: 'scheduler', url: '/api/scheduler/tasks' },
    { key: 'spU', url: '/api/uptime/status-pages' },
    { key: 'spS', url: '/api/server/status-pages' },
    { key: 'spG', url: '/api/github/public-pages' },
  ];
  const DASH_SLOW_SOURCES = [
    { key: 'koyeb', url: '/api/koyeb/data', timeout: 16000 },
    { key: 'fly', url: '/api/flyio/proxy/apps', timeout: 16000 },
  ];
  // 逐接口独立容错：单个接口超时/失败只取其空结果，不影响同批其他接口
  const fetchDashBatch = async (sources) => {
    const settled = await Promise.allSettled(
      sources.map((src) =>
        fetchWithTimeout(src.url, {}, src.timeout || FETCH_TIMEOUT_MS).then((r) => r.json().catch(() => ({}))),
      ),
    );
    const results = {};
    sources.forEach((src, i) => {
      results[src.key] = settled[i].status === 'fulfilled' && settled[i].value ? settled[i].value : {};
    });
    return results;
  };
  const buildPaasStats = (koyebJson, flyJson) => {
    const koyeb = { total: 0, running: 0, list: [] };
    (koyebJson?.accounts || []).forEach((acc) => {
      acc?.projects?.forEach((project) => {
        project?.services?.forEach((service) => {
          koyeb.total += 1;
          if (service?.status === 'HEALTHY' || service?.status === 'RUNNING') koyeb.running += 1;
          koyeb.list.push({
            name: service?.name || service?.id || '',
            status: service?.status === 'HEALTHY' || service?.status === 'RUNNING' ? 'running' : 'stopped',
          });
        });
      });
    });
    const fly = { total: 0, running: 0, list: [] };
    toArray(flyJson).forEach((acc) => {
      acc?.apps?.forEach((app) => {
        fly.total += 1;
        if (app?.status === 'deployed' || app?.status === 'running') fly.running += 1;
        fly.list.push({
          name: app?.name || app?.id || '',
          status: app?.status === 'deployed' || app?.status === 'running' ? 'running' : 'stopped',
        });
      });
    });
    return {
      koyeb,
      fly,
      list: [
        ...koyeb.list.map((s) => ({ ...s, kind: 'koyeb' })),
        ...fly.list.map((a) => ({ ...a, kind: 'fly' })),
      ],
    };
  };
  const loadDashboardStats = useCallback(async () => {
    const cached = cacheGet('dash');
    if (cached) setDash(cached);

    // 快批：本地/短路径接口先行结算，服务器、定时任务、状态页、模块入口等卡立即渲染；
    // 有缓存时 paas 沿用旧值避免 PaaS 卡闪骨架，慢批返回后再覆盖。
    let fastDash = null;
    try {
      const { servers: serversJson, dns: dnsJson, filebox: fileboxJson, totp: totpJson, scheduler: schedJson, spU: spUJson, spS: spSJson, spG: spGJson } = await fetchDashBatch(DASH_FAST_SOURCES);

      const serverItems = toArray(serversJson).map((s) => ({
        name: s?.name || s?.host || s?.id || '',
        host: s?.host || '',
        country: s?.country || s?.resolved_country || '',
        responseTime: s?.response_time ?? s?.responseTime ?? null,
        info: s?.info || null,
        status: normalizeServerStatus(s?.status),
      }));
      const servers = {
        total: serverItems.length,
        online: serverItems.filter((s) => s.status === 'online').length,
        error: serverItems.filter((s) => s.status === 'error').length,
        list: serverItems,
      };
      servers.offline = servers.total - servers.online - servers.error;

      const schedTasks = toArray(schedJson);
      const scheduler = {
        total: schedTasks.length,
        enabled: schedTasks.filter((t) => !!(t?.enabled || t?.isEnabled)).length,
        list: schedTasks.map((t) => ({
          name: t?.name || t?.title || t?.id || '',
          enabled: !!(t?.enabled || t?.isEnabled),
        })),
      };

      const statusPages = [
        ...toArray(spUJson).map((p) => ({ kind: 'uptime', id: p.id, slug: p.slug || '', name: p.name || p.title || '', url: p.url || '' })),
        ...toArray(spSJson).map((p) => ({ kind: 'server', id: p.id, slug: p.slug || '', name: p.name || p.title || '', url: p.url || '' })),
        ...toArray(spGJson).map((p) => ({ kind: 'github', id: p.id, slug: p.slug || '', name: p.name || p.title || '', url: p.url || '' })),
      ];

      fastDash = {
        servers,
        paas: cached?.paas ?? null, // 慢批返回前：有缓存沿用旧值，无缓存保持骨架
        dns: { zones: toArray(dnsJson).length },
        filebox: { total: toArray(fileboxJson).length },
        totp: { total: toArray(totpJson).length },
        scheduler,
        statusPages: { total: statusPages.length, list: statusPages },
      };
      setDash(fastDash);
    } catch (err) {
      console.error('[TilesDemo] dashboard stats (fast)', err);
    }

    // 慢批：Koyeb/Fly 直连外部，独立结算；失败置空结构，PaaS 卡从骨架转空态而非永久等待
    const slowResults = await fetchDashBatch(DASH_SLOW_SOURCES);
    const paas = buildPaasStats(slowResults.koyeb || {}, slowResults.fly || {});
    const fullDash = { ...(fastDash || {}), paas };
    setDash(fullDash);
    cacheSet('dash', fullDash, CACHE_TTL.dash);
  }, []);

  // 云端布局：读 /api/settings 的 tileLayout 字段（存于 data.db，跨设备）。
  // 兼容三种格式：旧数组（= 桌面 5 列）、旧断点桶（{ mobile, tablet, desktop, wide }）、新列数桶（{ '2'..'8' }）。
  useEffect(() => {
    let stopped = false;
    (async () => {
      try {
        const res = await fetchWithTimeout('/api/settings');
        const json = await res.json().catch(() => null);
        const saved = json?.data?.tileLayout ?? json?.tileLayout ?? null;
        const valid = (arr) => (Array.isArray(arr)
          ? arr.filter((it) => it && TILE_DEFS_BY_ID[it.i] && Number.isFinite(it.x) && Number.isFinite(it.w) && Number.isFinite(it.h))
          : null);
        let loaded = null;
        if (Array.isArray(saved)) {
          loaded = { 5: valid(saved) }; // 旧格式：整段视为 5 列桌面布局
        } else if (saved && typeof saved === 'object') {
          const isBreakpointFormat = 'mobile' in saved || 'desktop' in saved;
          if (isBreakpointFormat) {
            // 旧断点桶 → 映射到列数桶：mobile→2 / tablet→3 / desktop→5 / wide→8
            loaded = {
              2: valid(saved.mobile),
              3: valid(saved.tablet),
              5: valid(saved.desktop),
              8: valid(saved.wide),
            };
          } else {
            // 新列数桶：'2'..'8' 各一套
            loaded = {};
            for (let c = MIN_COLS; c <= MAX_COLS; c += 1) loaded[c] = valid(saved[String(c)]);
          }
        }
        if (!stopped && loaded) {
          setLayouts((prev) => {
            const merged = { ...(prev || {}) };
            let changed = false;
            Object.entries(loaded).forEach(([key, val]) => {
              if (val && val.length) {
                merged[key] = val;
                changed = true;
              }
            });
            return changed ? merged : prev;
          });
        }
      } catch (err) {
        console.error('[TilesDemo] load tile layout', err);
      } finally {
        if (!stopped) setCloudReady(true);
      }
    })();
    return () => {
      stopped = true;
    };
  }, []);

  // TileGrid 上报真实列数（重置/默认布局按此铺满）
  const [gridCols, setGridCols] = useState(0);

  // 无云端布局（或切换列数后为空）时按当前列数生成铺满的默认布局，各列数桶独立
  useEffect(() => {
    if (!cloudReady || gridCols === 0) return;
    setLayouts((prev) => {
      if (prev && prev[cols] != null) return prev;
      return { ...(prev || {}), [cols]: packDefaultLayout(cols) };
    });
  }, [cloudReady, gridCols, cols, setLayout]);

  // 主机指标：5s 轮询，累积 CPU 采样做实时 sparkline
  useEffect(() => {
    let stopped = false;
    let timer;
    const tick = async () => {
      try {
        const res = await fetchWithTimeout('/api/system/host-metrics');
        const json = await res.json().catch(() => null);
        if (!stopped && json?.success && json.data) {
          // 页脚「已运行」时长依赖此回写（旧 DashboardPage 由轮询填充 store）
          setAppProcessUptimeSeconds(json.data.process?.uptime);
          setHost((prev) => {
            const next = json.data;
            // 首次加载用后端 1 分钟滚动历史初始化；之后轮询追加当前值并保留最近 60s（5s × 12 点）
            const seedCpu = Array.isArray(next.history?.cpu) ? next.history.cpu : [];
            const seedMem = Array.isArray(next.history?.memory) ? next.history.memory : [];
            const seedDisk = Array.isArray(next.history?.disk) ? next.history.disk : [];
            const samples = [...(prev?.samples?.length ? prev.samples : seedCpu), next.cpu?.usage ?? 0].slice(-60);
            const memSamples = [...(prev?.memSamples?.length ? prev.memSamples : seedMem), next.memory?.usage ?? 0].slice(-60);
            const diskSamples = [...(prev?.diskSamples?.length ? prev.diskSamples : seedDisk), next.disk?.usage ?? 0].slice(-60);
            // GPU 利用率：字段形态可能是 gpu.usage 或 gpu[0].usage，缺省时保持上一轮序列
            const gpuUsage = next.gpu?.usage ?? next.gpu?.[0]?.usage;
            const gpuSamples = gpuUsage != null
              ? [...(prev?.gpuSamples || []), gpuUsage].slice(-60)
              : (prev?.gpuSamples || []);
            return { ...next, samples, memSamples, diskSamples, gpuSamples };
          });
        }
      } catch (err) {
        // 轮询失败静默，下一轮继续
      }
      if (!stopped) timer = window.setTimeout(tick, HOST_POLL_MS);
    };
    tick();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, []);

  useEffect(() => {
    loadApiStats();
    loadOpenai();
  }, [loadApiStats, loadOpenai]);

  useEffect(() => {
    loadUptime();
    loadDashboardStats();
  }, [loadUptime, loadDashboardStats]);

  // Cloudflare：账号 → zone → 分析（24h / 7d 窗口，实时代理 CF API）
  useEffect(() => {
    let stopped = false;
    (async () => {
      try {
        const cached = cacheGet('cfAccounts');
        if (cached) {
          if (!stopped) {
            setCfAccounts(cached);
            setCfAccountId(cached[0].id);
          }
          return;
        }
        const res = await fetchWithTimeout('/api/cloudflare/accounts');
        const json = await res.json().catch(() => ({}));
        const accounts = toArray(json);
        if (stopped || !accounts.length) return;
        cacheSet('cfAccounts', accounts, CACHE_TTL.cfAccounts);
        setCfAccounts(accounts);
        setCfAccountId(accounts[0].id);
      } catch (err) {
        console.error('[TilesDemo] cf accounts', err);
      }
    })();
    return () => {
      stopped = true;
    };
  }, []);

  useEffect(() => {
    if (!cfAccountId) return undefined;
    let stopped = false;
    (async () => {
      try {
        const cacheKey = `cfZones:${cfAccountId}`;
        const cached = cacheGet(cacheKey);
        if (cached) {
          if (!stopped) setCfZones(cached);
          return;
        }
        const res = await fetchWithTimeout(`/api/cloudflare/accounts/${encodeURIComponent(cfAccountId)}/zones`);
        const json = await res.json().catch(() => ({}));
        if (stopped) return;
        // 该接口响应为 { zones: [...] }（无 data 键），toArray 只认数组/{data}，需直接取 zones
        const zones = Array.isArray(json?.zones) ? json.zones : toArray(json);
        cacheSet(cacheKey, zones, CACHE_TTL.cfZones);
        setCfZones(zones);
        // 默认保持 'all'（全部 Zone 聚合），不自动选中第一个 zone
      } catch (err) {
        console.error('[TilesDemo] cf zones', err);
      }
    })();
    return () => {
      stopped = true;
    };
  }, [cfAccountId]);

  // Cloudflare 分析：cfZoneId === 'all' 时并发拉取全部 Zone 并按时间对齐聚合（请求量/缓存求和）
  useEffect(() => {
    if (!cfAccountId) return undefined;
    const zoneIds = cfZoneId === 'all' ? cfZones.map((z) => z.id) : [cfZoneId];
    if (!zoneIds.length) return undefined;
    let stopped = false;
    const cacheKey = `cfAnalytics:${cfAccountId}:${cfZoneId}:${cfRange}`;
    const cached = cacheGet(cacheKey);
    if (cached) setCfData(cached);
    if (!cached) setCfLoading(true);
    (async () => {
      try {
        const results = await Promise.all(
          zoneIds.map((id) =>
            fetchWithTimeout(
              `/api/cloudflare/accounts/${encodeURIComponent(cfAccountId)}/zones/${encodeURIComponent(id)}/analytics?timeRange=${cfRange}`,
            ).then((r) => r.json().catch(() => ({}))),
          ),
        );
        const byTs = new Map();
        let totalRequests = 0;
        let totalCached = 0;
        results.forEach((json) => {
          const an = json?.analytics || {};
          totalRequests += Number(an.requests) || 0;
          totalCached += Number(an.cachedRequests) || 0;
          (Array.isArray(an.timeseries) ? an.timeseries : []).forEach((p) => {
            const ts = parseCfTime(p);
            if (ts == null) return;
            let e = byTs.get(ts);
            if (!e) {
              e = { ts, requests: 0, cached: 0 };
              byTs.set(ts, e);
            }
            e.requests += Number(p.requests) || 0;
            e.cached += Number(p.cachedRequests) || 0;
          });
        });
        const timeseries = [...byTs.values()]
          .sort((a, b) => a.ts - b.ts)
          .map((e) => ({ datetime: new Date(e.ts).toISOString(), requests: e.requests, cachedRequests: e.cached }));
        if (!stopped) {
          const cfDataOut = {
            requests: totalRequests,
            cachedRequests: totalCached,
            cacheHitRate: totalRequests ? totalCached / totalRequests : 0,
            timeseries,
          };
          setCfData(cfDataOut);
          cacheSet(cacheKey, cfDataOut, CACHE_TTL.cfAnalytics);
        }
      } catch (err) {
        console.error('[TilesDemo] cf analytics', err);
      } finally {
        if (!stopped) setCfLoading(false);
      }
    })();
    return () => {
      stopped = true;
    };
  }, [cfAccountId, cfZoneId, cfRange, cfZones]);

  // 布局每次修改立即自动保存到云端（/api/settings PATCH，四断点对象 merge 进用户设置）；相同内容去重避免冗余写入
  const lastSavedLayoutRef = useRef(null);
  useEffect(() => {
    if (!cloudReady) return;
    const json = JSON.stringify(layouts);
    if (lastSavedLayoutRef.current === json) return;
    lastSavedLayoutRef.current = json;
    fetchWithTimeout('/api/settings', {
      method: 'PATCH',
      body: JSON.stringify({ tileLayout: layouts }),
    }).catch((err) => console.error('[TilesDemo] save tile layout', err));
  }, [layouts, cloudReady]);

  const visibleIds = useMemo(() => new Set((layout || []).map((it) => it.i)), [layout]);

  const toggleTile = useCallback((id) => {
    const def = TILE_DEFS_BY_ID[id];
    if (!def) return;
    setLayout((prev) => {
      const base = prev || [];
      if (base.some((it) => it.i === id)) return base.filter((it) => it.i !== id);
      const bottom = base.reduce((m, it) => Math.max(m, it.y + it.h), 0);
      return [...base, { i: id, x: 0, y: bottom, w: def.w, h: def.h, minW: def.minW ?? 1, minH: 1, maxW: 4, maxH: 4 }];
    });
  }, [setLayout]);

  const resetLayout = useCallback(() => {
    setLayout(packDefaultLayout(gridCols || 4));
  }, [gridCols, setLayout]);

  const renderTile = (id, item) => {
    const density = tileDensity(item.w, item.h);
    switch (id) {
      case 'apiTrend':
        return (
          <TileFrame title="API 调用趋势">
            <ApiTrendMultiTile data={apiStats} loading={apiStatsLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'apiTokens':
        return (
          <TileFrame title="API 令牌消耗">
            <ApiTokensTile data={apiStats} loading={apiStatsLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'openaiRequests':
        return (
          <TileFrame title="OpenAI 网关请求">
            <OpenaiRequestsTile data={openaiData} loading={openaiLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'openaiLatency':
        return (
          <TileFrame title="OpenAI 延迟">
            <OpenaiLatencyTile data={openaiData} loading={openaiLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'openaiErrors':
        return (
          <TileFrame title="OpenAI 错误数">
            <OpenaiErrorsTile data={openaiData} loading={openaiLoading} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'hostCpu':
        return (
          <TileFrame title="主机性能">
            <HostCpuTile data={host} isDarkMode={isDarkMode} density={density} w={item.w} />
          </TileFrame>
        );
      case 'uptime':
        return (
          <TileFrame title="监控可用率">
            <UptimeTile data={uptime} density={density} w={item.w} />
          </TileFrame>
        );
      case 'cfZone':
        return (
          <TileFrame
            title="Cloudflare Zone 请求"
            action={widthTier(item.w) === 'narrow' ? undefined : (
              <Select
                size="xs"
                aria-label="选择 Zone"
                value={cfZoneId}
                onValueChange={(v) => setCfZoneId(String(v ?? 'all'))}
                items={[
                  { value: 'all', label: '全部 Zone' },
                  ...cfZones.map((z) => ({ value: z.id, label: z.name || z.id })),
                ]}
                renderValue={(v) => (
                  <span className="min-w-0 truncate">
                    {v === 'all' ? '全部 Zone' : (cfZones.find((z) => z.id === v)?.name || String(v))}
                  </span>
                )}
                alignItemWithTrigger
                className="min-w-0 max-w-[12rem]"
              />
            )}
          >
            <CfZoneTile
              data={cfData}
              loading={cfLoading}
              isDarkMode={isDarkMode}
              empty={!cfAccounts.length}
              range={cfRange}
              density={density}
              w={item.w}
            />
          </TileFrame>
        );
      case 'servers':
        return (
          <TileFrame title="服务器状态">
            <ServerStatusTile servers={dash?.servers} density={density} w={item.w} />
          </TileFrame>
        );
      case 'paas':
        return (
          <TileFrame title="PaaS 实例">
            <PaasTile dash={dash} density={density} w={item.w} />
          </TileFrame>
        );
      case 'scheduler':
        return (
          <TileFrame title="定时任务">
            <SchedulerTile dash={dash} density={density} w={item.w} />
          </TileFrame>
        );
      case 'moduleTools':
        return (
          <TileFrame title="模块入口">
            <ModuleToolsTile dash={dash} uptime={uptime} density={density} w={item.w} />
          </TileFrame>
        );
      case 'statusPages':
        return (
          <TileFrame title="状态页">
            <StatusPagesTile dash={dash} density={density} w={item.w} />
          </TileFrame>
        );
      default:
        return null;
    }
  };

  // 控制工具栏：正式面板经 portal 渲染到面包屑栏（无间距），独立 demo 页内联在网格上方（mb-4）
  const toolbar = (
    <div className={`flex flex-wrap items-center justify-end gap-2 ${headerToolsEl ? '' : 'mb-4'}`}>
      <TimeRangePicker
        value={rangeLabel}
        onApply={(days, cfRange, label, minutes) => {
          const m = minutes && minutes > 0 && minutes < 1440 ? minutes : null;
          setRangeDays(days);
          setRangeMinutes(m);
          setCfRange(cfRange);
          setRangeLabel(label);
          try {
            localStorage.setItem(RANGE_STORAGE_KEY, JSON.stringify({ days, cfRange, label, minutes: m }));
          } catch {
            /* ignore */
          }
        }}
      />
      <DropdownMenu>
        <DropdownMenu.Trigger>
          <Button size="sm" icon={<SquaresFour className="h-4 w-4" />}>
            卡片管理
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          {TILE_DEFS.map((def) => (
            <div
              key={def.id}
              className="flex min-w-[10rem] items-center justify-between gap-3 px-2.5 py-1.5"
            >
              <span className="min-w-0 truncate text-xs text-kumo-default">{def.title}</span>
              <Switch size="sm" checked={visibleIds.has(def.id)} onCheckedChange={() => toggleTile(def.id)} />
            </div>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu>
    </div>
  );

  return (
    <div className="w-full">
      {headerToolsEl ? createPortal(toolbar, headerToolsEl) : toolbar}

      <TileGrid
        layout={layout}
        onLayoutChange={setLayout}
        onColsChange={(c) => {
          setGridCols(c);
          setCols(c); // 列数取 TileGrid 容器实际宽度（侧栏让位后变窄会相应变化），作为当前布局桶
        }}
        renderTile={(item) => renderTile(item.i, item)}
      />
    </div>
  );
}
