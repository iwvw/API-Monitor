import React, { useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { ChartPalette, TimeseriesChart } from '@cloudflare/kumo';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { io } from 'socket.io-client';
import { AnimatedCollapse } from '../components/AnimatedCollapse.jsx';
import useStore from '../store.js';
import {
  Activity,
  AlertTriangle,
  ChevronDown,
  Check,
  Clock,
  Globe,
  RefreshCw,
  Shield,
} from '../components/Icons.jsx';

const STATE_META = {
  up: { label: '运行正常', tone: 'success' },
  down: { label: '服务故障', tone: 'danger' },
  pending_down: { label: '确认中', tone: 'warning' },
  pending_up: { label: '恢复确认', tone: 'warning' },
  maintenance: { label: '维护中', tone: 'warning' },
  paused: { label: '已暂停', tone: 'neutral' },
  unknown: { label: '等待数据', tone: 'neutral' },
};

echarts.use([
  LineChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

const toneClass = {
  success: 'border-kumo-success/30 bg-kumo-success/10 text-kumo-success',
  danger: 'border-kumo-danger/30 bg-kumo-danger/10 text-kumo-danger',
  warning: 'border-kumo-warning/30 bg-kumo-warning/10 text-kumo-warning',
  neutral: 'border-kumo-line bg-kumo-recessed text-kumo-subtle',
};

const statusPanelClass = {
  success: 'border-kumo-success/30 bg-kumo-success/10 text-kumo-success',
  danger: 'border-kumo-danger/30 bg-kumo-danger/10 text-kumo-danger',
  warning: 'border-kumo-warning/30 bg-kumo-warning/10 text-kumo-warning',
  neutral: 'border-kumo-line bg-kumo-base text-kumo-strong',
};

const normalizePublicPath = () => {
  const path = window.location.pathname.replace(/\/+$/, '');
  const match = path.match(/^\/(?:status|u)\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
};

const isLocalHost = (host) => /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/i.test(host || '');

const formatDateTime = (value) => {
  if (!value) return '尚未更新';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '尚未更新';
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
};

const formatUptimePercent = (value, fallback = '100.00') => {
  if (value === null || value === undefined || value === '') return fallback;
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return fallback;
  return numeric.toFixed(2);
};

const formatChartTime = (timestamp) => {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`;
};

const formatLatencyAxis = (value) => {
  const latency = Number(value) || 0;
  const abs = Math.abs(latency);
  if (abs >= 1000) return `${(latency / 1000).toFixed(abs >= 10000 ? 0 : 1)}s`;
  return `${Math.round(latency)}ms`;
};

const getStateMeta = (state) => STATE_META[state] || STATE_META.unknown;

const parseBeatTime = (value) => {
  if (!value) return null;
  if (typeof value === 'number') return Number.isFinite(value) ? value : null;
  const text = String(value).trim();
  if (!text) return null;
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/.test(String(value))
    ? `${text.replace(' ', 'T')}Z`
    : text;
  const timestamp = new Date(normalized).getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
};

const normalizeHeartbeat = (beat = {}) => {
  let status = beat.status;
  if (typeof status === 'number') status = status === 1 ? 'up' : 'down';
  const timestamp = parseBeatTime(beat.time || beat.created_at || beat.createdAt);
  return {
    ...beat,
    status,
    time: timestamp ? new Date(timestamp).toISOString() : beat.time,
    timestamp,
  };
};

const normalizeMonitorState = (value) => {
  const state = value?.state || value;
  return STATE_META[state] ? state : null;
};

const mergeHeartbeat = (heartbeats, beat) => {
  const normalized = normalizeHeartbeat(beat);
  const key = normalized.id || normalized.time || normalized.timestamp;
  const existing = Array.isArray(heartbeats) ? heartbeats.map(normalizeHeartbeat) : [];
  const filtered = key
    ? existing.filter((item) => (item.id || item.time || item.timestamp) !== key)
    : existing;
  return [normalized, ...filtered].slice(0, 60);
};

const heartbeatClass = (status) => {
  if (status === 'up') return 'bg-kumo-success';
  if (status === 'down') return 'bg-kumo-danger';
  if (status === 'pending' || status === 'pending_down' || status === 'pending_up') return 'bg-kumo-warning';
  return 'bg-kumo-line opacity-25';
};

const getHeartbeatRangeLabel = (heartbeats) => {
  if (!heartbeats.length) return '--';
  const oldest = heartbeats[Math.min(heartbeats.length, 60) - 1];
  if (!oldest?.timestamp) return '--';
  const seconds = Math.max(0, Math.floor((Date.now() - oldest.timestamp) / 1000));
  if (seconds < 60) return `${seconds}秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}分钟`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}小时`;
  return `${Math.floor(seconds / 86400)}天`;
};

const isHttpUrl = (value) => /^https?:\/\//i.test(String(value || ''));

function HeartbeatLatencyChart({ beats, isDarkMode }) {
  const chartData = useMemo(() => {
    const data = beats
      .slice(0, 60)
      .reverse()
      .map((beat) => [beat.timestamp ?? parseBeatTime(beat.time), Number(beat.ping) || 0])
      .filter(([timestamp]) => Number.isFinite(timestamp));
    return [{
      name: '响应时间',
      color: ChartPalette.semantic('Success', isDarkMode),
      data,
    }];
  }, [beats, isDarkMode]);

  if (chartData[0].data.length === 0) {
    return <div className="mt-2 text-xs text-kumo-subtle">暂无心跳记录</div>;
  }

  return (
    <div className="min-w-0">
      <div className="mb-1.5 flex items-center justify-between gap-3 text-xs">
        <span className="font-semibold text-kumo-strong">延迟趋势</span>
        <span className="font-mono text-kumo-subtle">最近 {chartData[0].data.length} 次</span>
      </div>
      <div className="min-w-0 overflow-hidden" style={{ height: 96 }}>
        <TimeseriesChart
          echarts={echarts}
          data={chartData}
          height={96}
          yAxisName="ms"
          isDarkMode={isDarkMode}
          xAxisTickCount={3}
          yAxisTickCount={3}
          yAxisTickFormat={formatLatencyAxis}
          tooltipValueFormat={(value) => `${Math.round(value)} ms`}
          xAxisTickFormat={formatChartTime}
          tooltipMode="single"
          gradient
          ariaDescription="公开状态页响应时间历史"
        />
      </div>
    </div>
  );
}

function PublicStatusPage({ domainOnly = false, onDomainNotFound }) {
  const slug = useMemo(() => normalizePublicPath(), []);
  const theme = useStore((state) => state.theme);
  const isDarkMode = theme === 'dark';
  const [page, setPage] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expandedMonitorId, setExpandedMonitorId] = useState(null);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const endpoint = slug && !domainOnly
        ? `/api/uptime/public/status-pages/${encodeURIComponent(slug)}`
        : `/api/uptime/public/status-page-by-domain?domain=${encodeURIComponent(window.location.host)}`;
      const response = await fetch(endpoint);
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        const error = new Error(result.error || '状态页不存在或未公开');
        error.status = response.status;
        throw error;
      }
      setPage(result.data || result);
    } catch (err) {
      if (!slug && domainOnly && err.status === 404 && onDomainNotFound) {
        onDomainNotFound();
        return;
      }
      setError(err.message || '状态页加载失败');
      setPage(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    if (!page?.title) return undefined;
    const previousTitle = document.title;
    document.title = page.title;
    return () => {
      document.title = previousTitle;
    };
  }, [page?.title]);

  useEffect(() => {
    if (!page) return undefined;
    const socket = io('/', { transports: ['polling'] });

    socket.on('uptime:heartbeat', ({ monitorId, beat }) => {
      if (!monitorId || !beat) return;
      setPage((prev) => {
        if (!prev || !Array.isArray(prev.monitors)) return prev;
        let changed = false;
        const monitors = prev.monitors.map((monitor) => {
          if (String(monitor.id) !== String(monitorId)) return monitor;
          changed = true;
          const normalizedBeat = normalizeHeartbeat(beat);
          const monitorState = beat.monitorState || {};
          const confirmedState = normalizeMonitorState(monitorState);
          return {
            ...monitor,
            state: confirmedState || monitor.state,
            lastError: monitorState.lastError ?? monitorState.last_error ?? monitor.lastError,
            lastPing: monitorState.lastPing ?? monitorState.last_ping ?? (normalizedBeat.status === 'up' ? normalizedBeat.ping : 0),
            updatedAt: monitorState.updatedAt || monitorState.updated_at || normalizedBeat.time || monitor.updatedAt,
            uptime24h: beat.uptime24h || monitor.uptime24h,
            uptime30d: beat.uptime30d || monitor.uptime30d,
            heartbeats: mergeHeartbeat(monitor.heartbeats, normalizedBeat),
          };
        });
        return changed ? { ...prev, monitors, updatedAt: new Date().toISOString() } : prev;
      });
    });

    return () => socket.disconnect();
  }, [page?.id]);

  const monitors = Array.isArray(page?.monitors) ? page.monitors : [];
  const downCount = monitors.filter((item) => getStateMeta(item.state).tone === 'danger').length;
  const warningCount = monitors.filter((item) => getStateMeta(item.state).tone === 'warning').length;
  const operationalCount = monitors.length - downCount - warningCount;
  const pageConfig = page?.config || {};
  const hideTargets = !!pageConfig.hideTargets;
  const linkMonitorNames = !!pageConfig.linkMonitorNames;
  const statusTone = monitors.length === 0 ? 'neutral' : downCount > 0 ? 'danger' : warningCount > 0 ? 'warning' : 'success';
  const statusText = monitors.length === 0
    ? '暂无公开监测项'
    : downCount > 0
    ? `${downCount} 项服务异常`
    : warningCount > 0
      ? `${warningCount} 项服务需要关注`
      : '全部服务运行正常';

  return (
    <div className="min-h-screen bg-kumo-canvas text-kumo-default">
      <main className="mx-auto flex min-h-screen w-full max-w-5xl flex-col px-4 py-6 sm:px-6 lg:px-8">
        <div className="mb-6 flex items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-kumo-line bg-kumo-base text-kumo-brand">
              <Activity className="h-4 w-4" />
            </div>
            <div className="min-w-0">
              <div className="truncate text-base font-bold text-kumo-strong">{page?.title || '服务状态'}</div>
            </div>
          </div>
          <Button size="sm" variant="secondary" onClick={load} loading={loading} icon={<RefreshCw className="h-3.5 w-3.5" />}>
            刷新
          </Button>
        </div>

        {loading && (
          <div className="flex flex-1 items-center justify-center rounded-lg border border-kumo-line bg-kumo-base p-10 text-sm text-kumo-subtle">
            正在加载状态页...
          </div>
        )}

        {!loading && error && (
          <div className="flex flex-1 flex-col items-center justify-center rounded-lg border border-kumo-line bg-kumo-base p-10 text-center">
            <AlertTriangle className="mb-3 h-9 w-9 text-kumo-warning" />
            <h1 className="text-lg font-bold text-kumo-strong">无法显示状态页</h1>
            <p className="mt-2 max-w-md text-sm leading-relaxed text-kumo-subtle">{error}</p>
            {!slug && isLocalHost(window.location.host) && (
              <p className="mt-2 text-xs text-kumo-subtle">本地根路径不会自动匹配自定义域名，请使用 /status/slug 或 /u/slug 访问。</p>
            )}
          </div>
        )}

        {!loading && page && (
          <div className="flex flex-col gap-4">
            <section className={`rounded-lg border px-4 py-3.5 ${statusPanelClass[statusTone]}`}>
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="flex items-center gap-2 text-base font-bold">
                    {statusTone === 'success' ? <Check className="h-4 w-4" /> : statusTone === 'neutral' ? <Activity className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
                    {statusText}
                  </div>
                  {page.description && (
                    <p className="mt-2 max-w-2xl text-sm leading-relaxed opacity-90">{page.description}</p>
                  )}
                </div>
                <div className="grid grid-cols-4 gap-2 text-center text-xs">
                  <div className="rounded-md border border-current/20 bg-kumo-base/50 px-2.5 py-1.5">
                    <div className="font-mono text-sm font-bold">{monitors.length}</div>
                    <div className="opacity-80">监测项</div>
                  </div>
                  <div className="rounded-md border border-current/20 bg-kumo-base/50 px-2.5 py-1.5">
                    <div className="font-mono text-sm font-bold">{operationalCount}</div>
                    <div className="opacity-80">正常</div>
                  </div>
                  <div className="rounded-md border border-current/20 bg-kumo-base/50 px-2.5 py-1.5">
                    <div className="font-mono text-sm font-bold">{downCount}</div>
                    <div className="opacity-80">故障</div>
                  </div>
                  <div className="rounded-md border border-current/20 bg-kumo-base/50 px-2.5 py-1.5">
                    <div className="font-mono text-sm font-bold">{warningCount}</div>
                    <div className="opacity-80">关注</div>
                  </div>
                </div>
              </div>
            </section>

            <section className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
                <h2 className="text-sm font-bold text-kumo-strong">服务状态</h2>
                <span className="text-xs text-kumo-subtle">24h 可用率</span>
              </div>
              {monitors.length === 0 ? (
                <div className="p-8 text-center text-sm text-kumo-subtle">这个状态页还没有绑定监测目标。</div>
              ) : (
                <div className="divide-y divide-kumo-line">
                  {monitors.map((monitor) => {
                    const meta = getStateMeta(monitor.state);
                    const isExpanded = expandedMonitorId === monitor.id;
                    const heartbeats = Array.isArray(monitor.heartbeats) ? monitor.heartbeats.map(normalizeHeartbeat) : [];
                    const detailedBeats = Array.from({ length: 60 }, (_, index) => heartbeats[59 - index] || { status: 'empty' });
                    const targetUrl = isHttpUrl(monitor.targetUrl) ? monitor.targetUrl : '';
                    return (
                      <div key={monitor.id}>
                        <div
                          role="button"
                          tabIndex={0}
                          className="grid w-full gap-3 px-4 py-3 text-left hover:bg-kumo-recessed/50 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
                          onClick={() => setExpandedMonitorId(isExpanded ? null : monitor.id)}
                          onKeyDown={(event) => {
                            if (event.key === 'Enter' || event.key === ' ') {
                              event.preventDefault();
                              setExpandedMonitorId(isExpanded ? null : monitor.id);
                            }
                          }}
                        >
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-center gap-2">
                              <Globe className="h-4 w-4 shrink-0 text-kumo-subtle" />
                              {linkMonitorNames && targetUrl ? (
                                <a
                                  href={targetUrl}
                                  target="_blank"
                                  rel="noreferrer"
                                  className="truncate text-sm font-semibold text-kumo-strong hover:text-kumo-brand hover:underline"
                                  onClick={(event) => event.stopPropagation()}
                                >
                                  {monitor.name}
                                </a>
                              ) : (
                                <div className="truncate text-sm font-semibold text-kumo-strong">{monitor.name}</div>
                              )}
                              <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-kumo-subtle transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
                            </div>
                            {!hideTargets && (
                              <div className="mt-1 truncate text-xs text-kumo-subtle">{monitor.target || monitor.type}</div>
                            )}
                          </div>
                          <div className="grid grid-cols-[5.25rem_5.5rem_5.5rem] items-center gap-2 sm:justify-end">
                            <span className={`inline-flex h-8 items-center justify-center rounded border px-2 text-xs font-semibold ${toneClass[meta.tone]}`}>
                              {meta.label}
                            </span>
                            <span className="inline-flex h-8 items-center justify-center gap-1 rounded border border-kumo-line bg-kumo-recessed px-2 font-mono text-xs text-kumo-subtle">
                              <Clock className="h-3 w-3" />
                              {monitor.lastPing ? `${monitor.lastPing}ms` : '--'}
                            </span>
                            <span className="inline-flex h-8 items-center justify-center rounded border border-kumo-line bg-kumo-recessed px-2 font-mono text-xs text-kumo-subtle">
                              {formatUptimePercent(monitor.uptime24h)}%
                            </span>
                          </div>
                        </div>
                        <AnimatedCollapse open={isExpanded}>
                          <div className="border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3">
                            <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_12rem]">
                              <div className="min-w-0">
                                <HeartbeatLatencyChart beats={heartbeats} isDarkMode={isDarkMode} />
                              </div>
                              <div className="grid grid-cols-2 gap-2 lg:grid-cols-1 lg:self-start">
                                <div className="rounded-md border border-kumo-line bg-kumo-base p-2">
                                  <div className="text-[10px] text-kumo-subtle">24小时可用率</div>
                                  <div className="mt-1 font-mono text-sm font-bold text-kumo-strong">{formatUptimePercent(monitor.uptime24h)}%</div>
                                </div>
                                <div className="rounded-md border border-kumo-line bg-kumo-base p-2">
                                  <div className="text-[10px] text-kumo-subtle">30天可用率</div>
                                  <div className="mt-1 font-mono text-sm font-bold text-kumo-strong">{formatUptimePercent(monitor.uptime30d, '--')}%</div>
                                </div>
                              </div>
                              <div className="min-w-0 lg:col-span-2">
                                <div className="mb-1.5 flex items-center justify-between gap-3 text-xs">
                                  <span className="font-semibold text-kumo-strong">最近心跳</span>
                                  <span className="font-mono text-kumo-subtle">{getHeartbeatRangeLabel(heartbeats)}前 - 现在</span>
                                </div>
                                <div className="flex h-4 w-full items-center gap-[3px]">
                                  {detailedBeats.map((beat, index) => (
                                    <div
                                      key={index}
                                      className={`h-3.5 min-w-[3px] flex-1 rounded-sm ${heartbeatClass(beat.status)}`}
                                      title={beat.time ? `${formatDateTime(beat.time)} ${beat.ping ? `${beat.ping}ms` : ''}` : ''}
                                    />
                                  ))}
                                </div>
                              </div>
                            </div>
                          </div>
                        </AnimatedCollapse>
                      </div>
                    );
                  })}
                </div>
              )}
            </section>

            <footer className="flex flex-col gap-2 py-4 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
              <span className="inline-flex items-center gap-1">
                <Shield className="h-3.5 w-3.5" />
                由 API Monitor 提供
              </span>
              <span>最后更新：{formatDateTime(page.updatedAt || page.createdAt)}</span>
            </footer>
          </div>
        )}
      </main>
    </div>
  );
}

export default PublicStatusPage;
