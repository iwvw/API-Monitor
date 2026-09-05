// TilesDemoPage —— tiles 卡片看板 demo 页（/tiles-demo）。
// 新版卡片库视图复用 TilesBoard（与正式仪表盘同一组件）；页内提供旧版仪表盘样式对照。
import React, { useCallback, useEffect, useState } from 'react';
import { Badge, Tabs } from '@cloudflare/kumo';
import { Select } from '@cloudflare/kumo/components/select';
import { AppCard } from '../components/ui/AppPrimitives.jsx';
import {
  Server,
  Cloud,
  Globe,
  FolderOpen,
  Shield,
  Clock,
  Activity,
  Cpu,
  ArrowRight,
} from '../components/Icons.jsx';
import TilesBoard from '../components/tiles/TilesBoard.jsx';

const FETCH_TIMEOUT_MS = 8000;
const HOST_POLL_MS = 5000;
const RANGE_OPTIONS = [
  { value: '7', label: '7 天' },
  { value: '14', label: '14 天' },
  { value: '30', label: '30 天' },
];

function fetchWithTimeout(url, options = {}, timeoutMs = FETCH_TIMEOUT_MS) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);
  return fetch(url, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    signal: controller.signal,
  }).finally(() => window.clearTimeout(timer));
}

function toArray(value) {
  if (Array.isArray(value)) return value;
  if (value && Array.isArray(value.data)) return value.data;
  return [];
}

function fmtCompact(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(2)}k`;
  return String(Math.round(n));
}

function normalizeServerStatus(status) {
  if (status === 'online') return 'online';
  if (status === 'error' || status === 'interrupted' || status === 'suspect') return 'error';
  return 'offline';
}

// —— 旧版样式参考视图（复刻仪表盘原 DashboardOverviewCard 卡片样式，与新版卡片库对照）——

function OldOverviewCard({ icon: Icon, iconClassName, label, value, unit, detail, badge }) {
  return (
    <AppCard
      padding="none"
      interactive
      className="group grid min-h-[92px] cursor-pointer grid-rows-[auto_1fr_auto] gap-1.5 overflow-hidden px-2.5 pb-1.5 pt-2.5 cq-sm:min-h-[112px] cq-sm:p-3.5"
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
        {badge ? (
          <Badge className="shrink-0 whitespace-nowrap text-[9px] cq-sm:text-[11px]">{badge}</Badge>
        ) : null}
      </div>
      <div className="flex h-8 min-w-0 items-center justify-between gap-2 cq-sm:h-9">
        <span className="flex min-w-0 items-baseline gap-0.5 truncate text-[1.15rem] font-semibold leading-none text-kumo-strong tabular-nums cq-sm:text-[1.45rem] cq-sm:gap-1">
          {value}
          {unit ? (
            <span className="truncate text-[10px] font-normal leading-none text-kumo-subtle cq-sm:text-[11px]">
              {unit}
            </span>
          ) : null}
        </span>
      </div>
      <div className="hidden min-h-5 items-center justify-between gap-1.5 border-t border-kumo-line pt-1.5 text-[10px] leading-tight text-kumo-subtle group-hover:text-kumo-strong cq-sm:flex cq-sm:min-h-6 cq-sm:pt-2 cq-sm:text-[11px]">
        <span className="min-w-0 truncate">{detail}</span>
        <ArrowRight className="h-2.5 w-2.5 shrink-0 cq-sm:h-3 cq-sm:w-3" />
      </div>
    </AppCard>
  );
}

// —— 页面 ——

export default function TilesDemoPage() {
  const [viewMode, setViewMode] = useState('new');
  const [rangeDays, setRangeDays] = useState(14);

  // 旧版对照视图数据（新版卡片库数据在 TilesBoard 内部自取）
  const [host, setHost] = useState(null);
  const [uptime, setUptime] = useState(null);
  const [dash, setDash] = useState(null);
  const [apiStats, setApiStats] = useState(null);

  const loadApiStats = useCallback(async () => {
    try {
      const res = await fetchWithTimeout(`/api/system/api-stats?days=${rangeDays}`);
      const json = await res.json().catch(() => null);
      if (json?.success && json.data) setApiStats(json.data);
    } catch (err) {
      console.error('[TilesDemo] api-stats', err);
    }
  }, [rangeDays]);

  const loadUptime = useCallback(async () => {
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
      setUptime({ total: monitors.length, up, items: monitors });
    } catch (err) {
      console.error('[TilesDemo] uptime', err);
    }
  }, []);

  // 仪表盘信息汇总（服务器 / PaaS / DNS / 文件柜 / TOTP / 调度 / 状态页），逐接口独立容错
  const DASH_SOURCES = [
    { key: 'servers', url: '/api/server/accounts' },
    { key: 'koyeb', url: '/api/koyeb/data', timeout: 16000 },
    { key: 'fly', url: '/api/flyio/proxy/apps', timeout: 16000 },
    { key: 'dns', url: '/api/cloudflare/zones' },
    { key: 'filebox', url: '/api/filebox/history' },
    { key: 'totp', url: '/api/totp/accounts' },
    { key: 'scheduler', url: '/api/scheduler/tasks' },
    { key: 'spU', url: '/api/uptime/status-pages' },
    { key: 'spS', url: '/api/server/status-pages' },
    { key: 'spG', url: '/api/github/public-pages' },
  ];
  const loadDashboardStats = useCallback(async () => {
    const settled = await Promise.allSettled(
      DASH_SOURCES.map((src) =>
        fetchWithTimeout(src.url, {}, src.timeout || FETCH_TIMEOUT_MS).then((r) => r.json().catch(() => ({}))),
      ),
    );
    const results = {};
    DASH_SOURCES.forEach((src, i) => {
      results[src.key] = settled[i].status === 'fulfilled' && settled[i].value ? settled[i].value : {};
    });
    const { servers: serversJson, koyeb: koyebJson, fly: flyJson, dns: dnsJson, filebox: fileboxJson, totp: totpJson, scheduler: schedJson, spU: spUJson, spS: spSJson, spG: spGJson } = results;

    try {
      const serverItems = toArray(serversJson).map((s) => ({
        name: s?.name || s?.host || s?.id || '',
        status: normalizeServerStatus(s?.status),
      }));
      const servers = {
        total: serverItems.length,
        online: serverItems.filter((s) => s.status === 'online').length,
        error: serverItems.filter((s) => s.status === 'error').length,
      };
      servers.offline = servers.total - servers.online - servers.error;

      const koyeb = { total: 0, running: 0 };
      (koyebJson?.accounts || []).forEach((acc) => {
        acc?.projects?.forEach((project) => {
          project?.services?.forEach((service) => {
            koyeb.total += 1;
            if (service?.status === 'HEALTHY' || service?.status === 'RUNNING') koyeb.running += 1;
          });
        });
      });
      const fly = { total: 0, running: 0 };
      toArray(flyJson).forEach((acc) => {
        acc?.apps?.forEach((app) => {
          fly.total += 1;
          if (app?.status === 'deployed' || app?.status === 'running') fly.running += 1;
        });
      });

      const schedTasks = toArray(schedJson);
      const scheduler = {
        total: schedTasks.length,
        enabled: schedTasks.filter((t) => t?.enabled === true || t?.isEnabled === true).length,
      };

      const statusPages = toArray(spUJson).length + toArray(spSJson).length + toArray(spGJson).length;

      setDash({
        servers,
        paas: { koyeb, fly },
        dns: { zones: toArray(dnsJson).length },
        filebox: { total: toArray(fileboxJson).length },
        totp: { total: toArray(totpJson).length },
        scheduler,
        statusPages: { total: statusPages },
      });
    } catch (err) {
      console.error('[TilesDemo] dashboard stats', err);
    }
  }, []);

  useEffect(() => {
    loadApiStats();
  }, [loadApiStats]);

  useEffect(() => {
    loadUptime();
    loadDashboardStats();
  }, [loadUptime, loadDashboardStats]);

  // 主机指标：5s 轮询（旧版对照视图的 CPU 卡）
  useEffect(() => {
    let stopped = false;
    let timer;
    const tick = async () => {
      try {
        const res = await fetchWithTimeout('/api/system/host-metrics');
        const json = await res.json().catch(() => null);
        if (!stopped && json?.success && json.data) setHost(json.data);
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

  // —— 旧版样式视图（仪表盘原卡片样式）——
  const renderOldView = () => {
    const cpuUsage = host?.cpu?.usage;
    const load1 = host?.cpu?.loadAverage?.[0];
    return (
      <div className="grid w-full grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-xl:grid-cols-5">
        <OldOverviewCard
          icon={Cpu}
          iconClassName="bg-brand/10 text-brand"
          label="主机 CPU"
          value={cpuUsage != null ? `${cpuUsage.toFixed(1)}` : '—'}
          unit="%"
          detail={host ? `${host.cpu?.cores ?? ''} 核 · 负载 ${load1 != null ? load1.toFixed(2) : '-'}` : '加载中…'}
        />
        <OldOverviewCard
          icon={Server}
          iconClassName="bg-kumo-success/10 text-kumo-success"
          label="服务器"
          value={dash ? `${dash.servers.online}/${dash.servers.total}` : '—'}
          unit="在线"
          badge={dash ? `${dash.servers.online} 在线` : undefined}
          detail={dash ? `异常 ${dash.servers.error} · 离线 ${dash.servers.offline}` : '加载中…'}
        />
        <OldOverviewCard
          icon={Cloud}
          iconClassName="bg-brand/10 text-brand"
          label="PaaS 实例"
          value={dash ? `${dash.paas.koyeb.running + dash.paas.fly.running}/${dash.paas.koyeb.total + dash.paas.fly.total}` : '—'}
          unit="运行中"
          detail={dash ? `Koyeb ${dash.paas.koyeb.running}/${dash.paas.koyeb.total} · Fly ${dash.paas.fly.running}/${dash.paas.fly.total}` : '加载中…'}
        />
        <OldOverviewCard
          icon={Globe}
          iconClassName="bg-kumo-info/10 text-kumo-info"
          label="DNS 区域"
          value={dash ? String(dash.dns.zones) : '—'}
          unit="个"
          detail={dash ? 'Cloudflare 托管域名' : '加载中…'}
        />
        <OldOverviewCard
          icon={FolderOpen}
          iconClassName="bg-kumo-warning/10 text-kumo-warning"
          label="文件柜"
          value={dash ? String(dash.filebox.total) : '—'}
          unit="个"
          detail={dash ? '已归档文件' : '加载中…'}
        />
        <OldOverviewCard
          icon={Shield}
          iconClassName="bg-kumo-success/10 text-kumo-success"
          label="TOTP"
          value={dash ? String(dash.totp.total) : '—'}
          unit="个"
          detail={dash ? '双因素凭据' : '加载中…'}
        />
        <OldOverviewCard
          icon={Clock}
          iconClassName="bg-kumo-info/10 text-kumo-info"
          label="定时任务"
          value={dash ? `${dash.scheduler.enabled}/${dash.scheduler.total}` : '—'}
          unit="启用"
          detail={dash ? '已启用的计划任务' : '加载中…'}
        />
        <OldOverviewCard
          icon={Activity}
          iconClassName="bg-brand/10 text-brand"
          label="状态页"
          value={dash ? String(dash.statusPages.total) : '—'}
          unit="个"
          detail={dash ? '公开状态页' : '加载中…'}
        />
        <OldOverviewCard
          icon={Activity}
          iconClassName="bg-kumo-success/10 text-kumo-success"
          label="可用率"
          value={uptime ? `${uptime.up}/${uptime.total}` : '—'}
          unit="在线"
          detail={uptime ? `${uptime.total - uptime.up} 台离线` : '加载中…'}
        />
        <OldOverviewCard
          icon={Activity}
          iconClassName="bg-brand/10 text-brand"
          label="API 请求"
          value={fmtCompact(apiStats?.total?.all ?? 0)}
          detail={`过去 ${rangeDays} 天请求总数`}
        />
      </div>
    );
  };

  return (
    <div className="min-h-dvh w-full bg-kumo-canvas">
      <div className="mx-auto w-full max-w-[1440px] px-4 pt-6 pb-16">
        <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="text-lg font-semibold text-kumo-default">Tiles 看板 Demo</h1>
            <p className="mt-1 text-xs text-kumo-subtle">
              卡片式组件库：拖拽 / 档位缩放 / 响应式列数；布局云端分桶保存；下方可切换新旧版样式对照。
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Tabs
              value={viewMode}
              onValueChange={(v) => setViewMode(String(v))}
              tabs={[
                { value: 'new', label: '新版卡片库' },
                { value: 'old', label: '旧版样式' },
              ]}
              size="sm"
              className="min-w-max shrink-0"
            />
            <label className="flex items-center gap-1.5 text-xs text-kumo-subtle">
              时间范围
              <Select
                size="sm"
                aria-label="时间范围"
                className="h-7 rounded-md border border-kumo-line bg-kumo-base px-1.5 text-xs text-kumo-default outline-none"
                value={String(rangeDays)}
                onValueChange={(v) => setRangeDays(Number(v))}
                items={RANGE_OPTIONS}
              />
            </label>
          </div>
        </div>

        {viewMode === 'old' ? renderOldView() : <TilesBoard />}
      </div>
    </div>
  );
}
