import React, { useState, useEffect, useRef, useMemo } from 'react';
import io from 'socket.io-client';
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
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { ChartPalette, ClipboardText, Loader, Tabs, TimeseriesChart, Toolbar } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { AnimatedCollapse, DeferredRender } from '../components/AnimatedCollapse.jsx';
import { AppCard, ChartCard, ChartWarmupSkeleton, DataTableFrame, EmptyState, ResponsiveSearchInput, SectionCard, StatusBadge, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import { PublicPageBrandIcon } from '../components/public/PublicPageIconPicker.jsx';
import useStore from '../store.js';
import {
  Activity,
  Plus,
  Trash,
  Play,
  Pause,
  Save,
  RotateCw,
  Search,
  Edit,
  Globe,
  Terminal,
  TrendingUp,
  Server,
  Shield,
  Bell,
  Info,
  Download,
  Upload,
  Copy,
  ExternalLink,
  X
} from '../components/Icons.jsx';

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

// ==================== 样式辅助 ====================
const formatUptimeChartTime = (timestamp) => {
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

const createEmptyStatusPageForm = () => ({
  id: null,
  title: '',
  slug: '',
  domain: '',
  description: '',
  public: true,
  hideTargets: false,
  linkMonitorNames: false,
  showOnDashboard: true,
  publicIconId: '',
  cacheSeconds: 300,
  monitorIds: [],
});

const normalizeStatusSlug = (value, fallback = 'status') => {
  const text = String(value || fallback).trim().toLowerCase();
  const slug = text.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return slug || fallback;
};

const normalizeStatusDomain = (value) => (
  String(value || '')
    .trim()
    .replace(/^https?:\/\//i, '')
    .split('/')[0]
    .replace(/\/+$/g, '')
    .toLowerCase()
);

const parseUptimeBeatTime = (value) => {
  if (!value) return null;
  if (typeof value === 'number') return Number.isFinite(value) ? value : null;
  const text = String(value).trim();
  if (!text) return null;
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/.test(text)
    ? `${text.replace(' ', 'T')}Z`
    : text;
  const timestamp = new Date(normalized).getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
};

const normalizeUptimeBeat = (beat = {}) => {
  const timestamp = parseUptimeBeatTime(beat.time || beat.created_at || beat.createdAt);
  let status = beat.status;
  if (typeof status === 'number') {
    status = status === 1 ? 'up' : 'down';
  }
  return {
    ...beat,
    status,
    time: timestamp ? new Date(timestamp).toISOString() : beat.time,
    timestamp,
  };
};

const getUptimeChartColor = (isDarkMode) => ChartPalette.semantic('Success', isDarkMode);

const getUptimeImportActionMeta = (action) => (
  action === 'update'
    ? { tone: 'warning', label: '更新' }
    : { tone: 'success', label: '创建' }
);

const buildUptimeImportSections = (preview) => {
  if (!preview) return [];

  const sections = [
    {
      key: 'monitors',
      title: '监测目标',
      description: '按名称、类型和地址匹配。',
      emptyLabel: '本次配置不包含监测目标。',
      items: (preview.monitors || []).map((item, index) => ({
        id: `monitor-${index}-${item.name || 'unnamed'}`,
        label: item.name || '未命名监测',
        detail: item.type ? `类型: ${String(item.type).toUpperCase()}` : '监测配置',
        action: item.action,
      })),
    },
    {
      key: 'statusPages',
      title: '状态页',
      description: '按 slug 匹配。',
      emptyLabel: '本次配置不包含状态页。',
      items: (preview.statusPages || []).map((item, index) => ({
        id: `status-page-${index}-${item.slug || item.title || 'untitled'}`,
        label: item.title || item.slug || '未命名状态页',
        detail: item.slug ? `Slug: ${item.slug}` : '状态页配置',
        action: item.action,
      })),
    },
    {
      key: 'maintenanceWindows',
      title: '维护窗口',
      description: '按标题匹配。',
      emptyLabel: '本次配置不包含维护窗口。',
      items: (preview.maintenanceWindows || []).map((item, index) => ({
        id: `maintenance-${index}-${item.title || 'untitled'}`,
        label: item.title || '未命名维护窗口',
        detail: '维护通知与时间窗口配置',
        action: item.action,
      })),
    },
  ];

  return sections.map((section) => {
    const creates = section.items.filter((item) => item.action !== 'update').length;
    const updates = section.items.length - creates;
    return {
      ...section,
      total: section.items.length,
      creates,
      updates,
    };
  });
};

// ==================== SSL Certificate Panel ====================
function SslCertificatePanel({ monitorId }) {
  const [sslData, setSslData] = useState(null);
  const [sslLoading, setSslLoading] = useState(false);
  const [sslExpanded, setSslExpanded] = useState(false);

  const loadSslInfo = async () => {
    if (sslData) { setSslExpanded(prev => !prev); return; }
    setSslLoading(true);
    setSslExpanded(true);
    try {
      const res = await fetch(`/api/uptime/monitors/${monitorId}/ssl`, {
        headers: { 'Content-Type': 'application/json' }
      });
      const data = await res.json();
      setSslData(data);
    } catch (e) {
      setSslData({ ssl: false, error: e.message });
    } finally {
      setSslLoading(false);
    }
  };

  const formatDate = (d) => d ? new Date(d).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '--';

  return (
    <div className="rounded-lg border border-kumo-line bg-kumo-base overflow-hidden">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={loadSslInfo}
        className="h-auto w-full justify-between rounded-none px-3 py-2 text-left"
      >
        <span className="text-[11px] font-bold text-kumo-strong uppercase tracking-wider flex items-center gap-1.5 select-none">
          <Shield className="w-3.5 h-3.5" />
          SSL 证书信息
        </span>
        <span className="text-[10px] text-kumo-subtle">
          {sslLoading ? '加载中...' : sslExpanded ? '收起' : '展开'}
        </span>
      </Button>
      <AnimatedCollapse open={sslExpanded}>
        <div className="px-3 pb-3 space-y-2">
          {sslData && !sslData.ssl && (
            <div className="text-[10px] text-kumo-subtle py-2">{sslData.reason || sslData.error || '无 SSL 证书'}</div>
          )}
          {sslData && sslData.ssl && (() => {
            const daysLeft = sslData.daysLeft;
            let shieldColor = 'text-kumo-success';
            let bgColor = 'bg-kumo-success/10';
            if (daysLeft <= 7) { shieldColor = 'text-kumo-danger'; bgColor = 'bg-kumo-danger/10'; }
            else if (daysLeft <= 30) { shieldColor = 'text-kumo-warning'; bgColor = 'bg-kumo-warning/10'; }
            return (
              <>
                {/* 证书状态概览 */}
                <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                  <div className={`rounded-md p-2 ${bgColor} flex flex-col`}>
                    <span className="text-[9px] text-kumo-subtle select-none">剩余天数</span>
                    <span className={`text-sm font-bold font-mono ${shieldColor} flex items-center gap-1`}>
                      <Shield className="w-3 h-3" />
                      {daysLeft} 天
                    </span>
                  </div>
                  <div className="rounded-md p-2 bg-kumo-recessed flex flex-col">
                    <span className="text-[9px] text-kumo-subtle select-none">主体 (Subject)</span>
                    <span className="text-[10px] font-semibold text-kumo-strong truncate" title={sslData.subject}>{sslData.subject}</span>
                  </div>
                  <div className="rounded-md p-2 bg-kumo-recessed flex flex-col">
                    <span className="text-[9px] text-kumo-subtle select-none">签发机构 (Issuer)</span>
                    <span className="text-[10px] font-semibold text-kumo-strong truncate" title={sslData.issuer}>{sslData.issuer}</span>
                  </div>
                  <div className="rounded-md p-2 bg-kumo-recessed flex flex-col">
                    <span className="text-[9px] text-kumo-subtle select-none">有效期</span>
                    <span className="text-[10px] font-mono text-kumo-strong">{formatDate(sslData.notBefore)} ~ {formatDate(sslData.notAfter)}</span>
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {/* DNS SANs */}
                  {sslData.dnsNames && sslData.dnsNames.length > 0 && (
                    <div className="rounded-md p-2 bg-kumo-recessed">
                      <span className="text-[9px] text-kumo-subtle select-none block mb-1">DNS 备用名称 (SANs)</span>
                      <div className="flex flex-wrap gap-1">
                        {sslData.dnsNames.map((name, i) => (
                          <span key={i} className="text-[9px] px-1.5 py-0.5 rounded bg-kumo-base text-kumo-strong font-mono border border-kumo-line">{name}</span>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* 证书链 */}
                  {sslData.chain && sslData.chain.length > 1 && (
                    <div className="rounded-md p-2 bg-kumo-recessed">
                      <span className="text-[9px] text-kumo-subtle select-none block mb-1.5">证书链 ({sslData.chain.length} 级)</span>
                      <div className="space-y-1">
                        {sslData.chain.map((cert, i) => (
                          <div key={i} className="flex items-center gap-2 text-[9px] font-mono text-kumo-strong">
                            <span className="w-3.5 h-3.5 rounded-full bg-kumo-base border border-kumo-line flex items-center justify-center text-[7px] flex-shrink-0">{i + 1}</span>
                            <span className="truncate flex-1" title={cert.subject}>{cert.subject || '(unnamed)'}</span>
                            <span className="text-kumo-subtle flex-shrink-0">{cert.isCA ? 'CA' : 'Leaf'}</span>
                            <span className="text-kumo-subtle flex-shrink-0">{formatDate(cert.notAfter)}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              </>
            );
          })()}
        </div>
      </AnimatedCollapse>
    </div>
  );
}

// ==================== UptimeMonitorDetails 子组件 ====================
// 使用独立的子组件隔离 Kumo TimeseriesChart，在折叠/销毁时由组件自身清理 ECharts 实例
function UptimeMonitorDetails({
  monitor,
  heartbeats = [],
  loading = false,
  uptime24h,
  uptime30d,
  isDarkMode,
  onPauseResume,
  onEdit,
  onDelete,
  expanded = true,
}) {
  const { isArmed, confirmPress } = useConfirmPress();
  const chartData = useMemo(() => {
    return [{
      name: '响应时间',
      color: getUptimeChartColor(isDarkMode),
      data: [...heartbeats]
        .slice(0, 60)
        .reverse()
        .map((beat) => [beat.timestamp ?? parseUptimeBeatTime(beat.time), Number(beat.ping) || 0])
        .filter(([timestamp]) => Number.isFinite(timestamp)),
    }];
  }, [heartbeats, isDarkMode]);

  return (
    <div className="space-y-3 border-t border-kumo-interact/80 bg-kumo-recessed/40 p-3">
      {/* 头部操作栏 */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h5 className="flex items-center gap-1.5 text-xs font-semibold text-kumo-strong">
          <TrendingUp className="w-3.5 h-3.5" />
          监控详情
        </h5>
        <div className="flex items-center gap-1.5">
          <Button size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onPauseResume(monitor);
            }}
            icon={monitor.active ? <Pause className="w-3 h-3" /> : <Play className="w-3 h-3" />}
          >
            {monitor.active ? '暂停' : '启用'}
          </Button>
          <Button size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onEdit(monitor);
            }}
            icon={<Edit className="w-3 h-3" />}
          >
            编辑
          </Button>
          <Button size="sm"
            variant={isArmed(`monitor:${monitor.id}`) ? 'destructive' : 'secondary-destructive'}
            onClick={(e) => {
              e.stopPropagation();
              onDelete(monitor.id);
            }}
            icon={<Trash className="w-3 h-3" />}
          >
            删除
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,1fr)_15rem]">
        {/* 图表主栏 (Span 3) */}
        <ChartCard className="relative h-36 !border-kumo-interact/90 !bg-kumo-base">
          {(tooltipBoundary) => (
            <DeferredRender open={expanded} fallback={<ChartWarmupSkeleton height={120} />}>
              <TimeseriesChart
                echarts={echarts}
                data={chartData}
                height={120}
                yAxisName="ms"
                loading={loading}
                tooltipBoundary={tooltipBoundary ?? undefined}
                xAxisTickCount={3}
                yAxisTickCount={3}
                isDarkMode={isDarkMode}
                yAxisTickFormat={formatLatencyAxis}
                tooltipValueFormat={(value) => `${Math.round(value)} ms`}
                xAxisTickFormat={formatUptimeChartTime}
                tooltipMode="single"
                gradient
                ariaDescription="Uptime monitor response time history"
              />
            </DeferredRender>
          )}
        </ChartCard>

        {/* 右侧可用率统计指标 */}
        <div className="grid grid-cols-2 gap-2 lg:grid-cols-1">
          <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
            <span className="text-[10px] text-kumo-subtle select-none">24小时可用率</span>
            <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{uptime24h}%</span>
          </div>
          <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
            <span className="text-[10px] text-kumo-subtle select-none">30天可用率</span>
            <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{uptime30d}%</span>
          </div>
        </div>
      </div>

      {/* SSL 证书信息面板 */}
      {monitor.url && monitor.url.startsWith('https://') && (
        <SslCertificatePanel monitorId={monitor.id} />
      )}

    </div>
  );
}

// ==================== 主 UptimePage 组件 ====================
function UptimePage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const theme = useStore((state) => state.theme);
  const isDarkMode = theme === 'dark';
  const [uptimeCurrentTab, setUptimeCurrentTab] = useState('list'); // 'list' | 'add' | 'stats'
  const [uptimeMonitors, setUptimeMonitors] = useState([]);
  const [uptimeStatusPages, setUptimeStatusPages] = useState([]);
  const [uptimeMaintenanceWindows, setUptimeMaintenanceWindows] = useState([]);
  const [uptimeHeartbeats, setUptimeHeartbeats] = useState({});
  const [uptimeHeartbeatLoading, setUptimeHeartbeatLoading] = useState({});
  const [uptimeRateCache, setUptimeRateCache] = useState({});
  const [uptimeStats, setUptimeStats] = useState({ up: 0, down: 0, pending: 0, unknown: 0 });

  // UI 筛选与搜索
  const [uptimeStatusFilter, setUptimeStatusFilter] = useState(null); // null | 'up' | 'down' | 'pending'
  const [uptimeSearchText, setUptimeSearchText] = useState('');
  const [uptimeLoading, setUptimeLoading] = useState(false);
  const [uptimeSaving, setUptimeSaving] = useState(false);
  const [uptimeMetaLoading, setUptimeMetaLoading] = useState(false);
  const [uptimeImportPreview, setUptimeImportPreview] = useState(null);
  const [uptimeImportPayload, setUptimeImportPayload] = useState(null);
  const [selectedMonitorIds, setSelectedMonitorIds] = useState([]);
  const [monitorSelectionMode, setMonitorSelectionMode] = useState(false);
  const [expandedMonitorId, setExpandedMonitorId] = useState(null);
  const [statusPageForm, setStatusPageForm] = useState(() => createEmptyStatusPageForm());

  // 通知渠道配置
  const [notificationChannels, setNotificationChannels] = useState([]);

  const uptimeImportSections = useMemo(
    () => buildUptimeImportSections(uptimeImportPreview),
    [uptimeImportPreview]
  );

  const uptimeImportSummary = useMemo(() => {
    const totals = uptimeImportSections.reduce((acc, section) => ({
      total: acc.total + section.total,
      creates: acc.creates + section.creates,
      updates: acc.updates + section.updates,
    }), { total: 0, creates: 0, updates: 0 });

    return {
      ...totals,
      nonEmptySections: uptimeImportSections.filter((section) => section.total > 0),
    };
  }, [uptimeImportSections]);

  // 表单状态
  const [uptimeForm, setUptimeForm] = useState({
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
    jsonQueryPath: '',
    jsonQueryOperator: 'equals',
    jsonExpectedValue: '',
    dns_resolve_type: 'A',
    dns_resolve_server: '',
    pushToken: '',
    pushGraceSeconds: 120,
    headers: '',
    body: '',
    ignoreTls: false,
    expiryNotification: 7,
    tagsInput: '',
    notificationChannels: []
  });

  const socketRef = useRef(null);
  const uptimeImportInputRef = useRef(null);

  // 获取请求 Header
  const getAuthHeaders = () => {
    return {
      'Content-Type': 'application/json',
    };
  };

  // ==================== 1. 数据载入 ====================
  const loadUptimeMonitors = async () => {
    setUptimeLoading(true);
    setSelectedMonitorIds([]);
    try {
      const headers = getAuthHeaders();
      const [monitorsRes, channelsRes] = await Promise.all([
        fetch('/api/uptime/monitors', { headers }),
        fetch('/api/notification/channels', { headers })
      ]);

      const monitorsData = await monitorsRes.json();
      const channelsData = await channelsRes.json();

      if (Array.isArray(monitorsData)) {
        setUptimeMonitors(monitorsData);
        // 初始化心跳容器并缓存末次状态
        const initialBeats = {};
        monitorsData.forEach(m => {
          if (m.lastHeartbeat) {
            initialBeats[m.id] = [normalizeUptimeBeat(m.lastHeartbeat)];
          } else {
            initialBeats[m.id] = [];
          }
        });
        setUptimeHeartbeats(initialBeats);

        // 延时加载具体历史记录与可用率
        monitorsData.forEach(m => {
          loadHeartbeats(m.id);
          loadUptimeRates(m.id);
        });
      }

      if (channelsData.success && Array.isArray(channelsData.data)) {
        setNotificationChannels(channelsData.data);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入 Uptime 监测数据失败');
    } finally {
      setUptimeLoading(false);
    }
  };

  const loadHeartbeats = async (monitorId) => {
    setUptimeHeartbeatLoading(prev => ({ ...prev, [monitorId]: true }));
    try {
      const res = await fetch(`/api/uptime/monitors/${monitorId}/history`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (Array.isArray(data)) {
        const normalized = data.map(normalizeUptimeBeat);
        setUptimeHeartbeats(prev => ({ ...prev, [monitorId]: normalized }));
      }
    } catch (e) {
      console.error(`加载心跳历史失败 (${monitorId}):`, e);
    } finally {
      setUptimeHeartbeatLoading(prev => ({ ...prev, [monitorId]: false }));
    }
  };

  const loadUptimeRates = async (monitorId) => {
    try {
      const headers = getAuthHeaders();
      const [res1, res30] = await Promise.all([
        fetch(`/api/uptime/monitors/${monitorId}/uptime?days=1`, { headers }),
        fetch(`/api/uptime/monitors/${monitorId}/uptime?days=30`, { headers }),
      ]);
      const d1 = await res1.json();
      const d30 = await res30.json();
      setUptimeRateCache(prev => ({
        ...prev,
        [monitorId]: { 1: d1.uptime || '100.000', 30: d30.uptime || '100.000' }
      }));
    } catch (e) {
      // 静默失败
    }
  };

  const loadUptimeStatusPages = async () => {
    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/status-pages', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && Array.isArray(data.data)) {
        setUptimeStatusPages(data.data);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入状态页失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const loadUptimeMaintenance = async () => {
    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/maintenance', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && Array.isArray(data.data)) {
        setUptimeMaintenanceWindows(data.data);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入维护窗口失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const createDefaultStatusPage = async () => {
    if (uptimeMonitors.length === 0) {
      toast.warning('请先创建监测目标');
      return;
    }
    try {
      const res = await fetch('/api/uptime/status-pages', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          title: 'Main Status',
          slug: 'main-status',
          monitorIds: uptimeMonitors.map(m => m.id),
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '创建失败');
      toast.success('状态页已创建');
      await loadUptimeStatusPages();
    } catch (e) {
      toast.error(e.message || '创建状态页失败');
    }
  };

  const getStatusPageBaseOrigin = () => {
    const configured = String(useStore.getState().publicApiUrl || '').trim().replace(/\/+$/g, '');
    return configured || window.location.origin;
  };

  const getStatusPagePublicUrl = (pageOrForm, mode = 'status') => {
    const slug = normalizeStatusSlug(pageOrForm?.slug || pageOrForm?.title || 'status');
    return `${getStatusPageBaseOrigin()}/${mode}/${encodeURIComponent(slug)}`;
  };

  const getStatusPageDomainUrl = (pageOrForm) => {
    const domain = normalizeStatusDomain(pageOrForm?.domain);
    return domain ? `https://${domain}` : '';
  };

  const copyStatusUrl = async (value, label = '公开地址') => {
    if (!value) {
      toast.warning('没有可复制的地址');
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`${label}已复制`);
    } catch (error) {
      toast.error('复制失败');
    }
  };

  const resetStatusPageForm = () => {
    setStatusPageForm(createEmptyStatusPageForm());
  };

  const editStatusPage = (page) => {
    const config = page.config || {};
    setStatusPageForm({
      id: page.id,
      title: page.title || '',
      slug: page.slug || '',
      domain: page.domain || '',
      description: page.description || '',
      public: page.public !== false,
      hideTargets: !!config.hideTargets,
      linkMonitorNames: !!config.linkMonitorNames,
      showOnDashboard: !!config.showOnDashboard,
      publicIconId: String(config.publicIconId || '').trim(),
      cacheSeconds: page.cacheSeconds || 300,
      monitorIds: Array.isArray(page.monitorIds) ? page.monitorIds : [],
    });
  };

  const saveStatusPage = async () => {
    const title = statusPageForm.title.trim();
    const slug = normalizeStatusSlug(statusPageForm.slug || title);
    if (!title) {
      toast.warning('请填写状态页名称');
      return;
    }
    if (statusPageForm.monitorIds.length === 0) {
      toast.warning('请至少绑定一个监测目标');
      return;
    }
    setUptimeMetaLoading(true);
    try {
      const payload = {
        title,
        slug,
        domain: normalizeStatusDomain(statusPageForm.domain),
        description: statusPageForm.description.trim(),
        public: !!statusPageForm.public,
        cacheSeconds: Math.max(30, Number(statusPageForm.cacheSeconds) || 300),
        config: {
          hideTargets: !!statusPageForm.hideTargets,
          linkMonitorNames: !!statusPageForm.linkMonitorNames,
          showOnDashboard: !!statusPageForm.showOnDashboard,
          ...(statusPageForm.publicIconId ? { publicIconId: statusPageForm.publicIconId } : {}),
        },
        monitorIds: statusPageForm.monitorIds,
      };
      const isEdit = !!statusPageForm.id;
      const response = await fetch(isEdit ? `/api/uptime/status-pages/${statusPageForm.id}` : '/api/uptime/status-pages', {
        method: isEdit ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) {
        throw new Error(result.error || '保存状态页失败');
      }
      toast.success(isEdit ? '状态页已更新' : '状态页已创建');
      resetStatusPageForm();
      await loadUptimeStatusPages();
    } catch (error) {
      toast.error(error.message || '保存状态页失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const deleteStatusPage = async (page) => {
    if (!confirmPress(`status-page:${page.id}`, `删除状态页「${page.title || page.slug}」`)) return;
    setUptimeMetaLoading(true);
    try {
      const response = await fetch(`/api/uptime/status-pages/${page.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        throw new Error(result.error || '删除状态页失败');
      }
      toast.success('状态页已删除');
      if (statusPageForm.id === page.id) resetStatusPageForm();
      await loadUptimeStatusPages();
    } catch (error) {
      toast.error(error.message || '删除状态页失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const toggleStatusPageMonitor = (monitorId, checked) => {
    setStatusPageForm(prev => {
      const ids = new Set(prev.monitorIds);
      if (checked) ids.add(monitorId);
      else ids.delete(monitorId);
      return { ...prev, monitorIds: Array.from(ids) };
    });
  };

  const createQuickMaintenance = async () => {
    if (selectedMonitorIds.length === 0) {
      toast.warning('请先在仪表盘选择监测目标');
      return;
    }
    try {
      const res = await fetch('/api/uptime/maintenance', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          title: '快速维护窗口',
          description: '由 Uptime 工作台创建',
          strategy: 'manual',
          startAt: new Date().toISOString(),
          endAt: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
          targets: selectedMonitorIds,
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '创建失败');
      toast.success('维护窗口已创建');
      await loadUptimeMaintenance();
    } catch (e) {
      toast.error(e.message || '创建维护窗口失败');
    }
  };

  const exportUptimeConfig = async () => {
    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/export', { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '导出失败');

      const blob = new Blob([JSON.stringify(data.data, null, 2)], { type: 'application/json' });
      const blobUrl = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = blobUrl;
      link.download = `api-monitor-uptime-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(blobUrl);
      toast.success('Uptime 配置已导出');
    } catch (e) {
      console.error(e);
      toast.error(e.message || '导出 Uptime 配置失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const previewUptimeImportFile = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;

    setUptimeMetaLoading(true);
    setUptimeImportPreview(null);
    setUptimeImportPayload(null);
    try {
      const text = await file.text();
      const parsed = JSON.parse(text);
      const payload = parsed?.data?.type === 'api-monitor-uptime-export' ? parsed.data : parsed;
      const res = await fetch('/api/uptime/import/preview', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ data: payload }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '导入预览失败');
      setUptimeImportPayload(payload);
      setUptimeImportPreview(data.data);
      toast.success('导入预览已生成');
    } catch (e) {
      console.error(e);
      toast.error(e.message || '导入预览失败');
    } finally {
      setUptimeMetaLoading(false);
      if (event.target) event.target.value = '';
    }
  };

  const commitUptimeImport = async () => {
    if (!uptimeImportPayload) {
      toast.warning('请先选择导入文件并完成预览');
      return;
    }
    if (!(await dialog.confirm('确定要导入 Uptime 配置吗？同名监测、状态页和维护窗口将被更新。'))) {
      return;
    }

    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/import', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ data: uptimeImportPayload }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '导入失败');
      toast.success(`导入完成：监测 ${data.data?.monitorsChanged || 0}，状态页 ${data.data?.pagesChanged || 0}，维护窗口 ${data.data?.maintenanceChanged || 0}`);
      setUptimeImportPreview(null);
      setUptimeImportPayload(null);
      await Promise.all([loadUptimeMonitors(), loadUptimeStatusPages(), loadUptimeMaintenance()]);
    } catch (e) {
      console.error(e);
      toast.error(e.message || '导入失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  // ==================== 2. Socket 实时更新 ====================
  useEffect(() => {
    loadUptimeMonitors();

    // 建立 Socket 推送连接
    const socket = io('/', {
      transports: ['polling']
    });

    socket.on('connect', () => {
      console.log('✅ Uptime Socket Connected');
    });

    socket.on('uptime:heartbeat', ({ monitorId, beat }) => {
      const normalizedBeat = normalizeUptimeBeat(beat);

      setUptimeHeartbeats(prev => {
        const list = prev[monitorId] ? [...prev[monitorId]] : [];
        list.unshift(normalizedBeat);
        if (list.length > 60) {
          list.length = 60;
        }
        return { ...prev, [monitorId]: list };
      });
    });

    socketRef.current = socket;

    return () => {
      if (socketRef.current) {
        socketRef.current.disconnect();
      }
    };
  }, []);

  // ==================== 3. 统计状态运算 ====================
  useEffect(() => {
    const stats = { up: 0, down: 0, pending: 0, unknown: 0 };
    uptimeMonitors.forEach(m => {
      if (!m.active) {
        stats.unknown++;
        return;
      }
      const beats = uptimeHeartbeats[m.id] || [];
      const lastBeat = beats[0];

      if (!lastBeat) {
        stats.unknown++;
      } else if (lastBeat.status === 'up') {
        stats.up++;
      } else if (lastBeat.status === 'down') {
        stats.down++;
      } else if (lastBeat.status === 'pending') {
        stats.pending++;
      } else {
        stats.unknown++;
      }
    });
    setUptimeStats(stats);
  }, [uptimeMonitors, uptimeHeartbeats]);

  // ==================== 4. 筛选与数据处理 ====================
  const filteredMonitors = useMemo(() => {
    let result = [...uptimeMonitors];

    // 按可用状态过滤
    if (uptimeStatusFilter) {
      result = result.filter(m => {
        if (!m.active) return uptimeStatusFilter === 'pending'; // 暂停算等待/未知
        const beats = uptimeHeartbeats[m.id] || [];
        const lastBeat = beats[0];
        return lastBeat?.status === uptimeStatusFilter;
      });
    }

    // 按搜索关键字过滤
    if (uptimeSearchText.trim()) {
      const q = uptimeSearchText.toLowerCase();
      result = result.filter(
        m =>
          m.name.toLowerCase().includes(q) ||
          (m.url && m.url.toLowerCase().includes(q)) ||
          (m.hostname && m.hostname.toLowerCase().includes(q)) ||
          (m.tags && m.tags.some(t => t.toLowerCase().includes(q)))
      );
    }

    return result;
  }, [uptimeMonitors, uptimeStatusFilter, uptimeSearchText, uptimeHeartbeats]);

  // 获取可用率辅助函数
  const getUptimeRate = (monitorId, days = 1) => {
    const cache = uptimeRateCache[monitorId];
    if (cache && cache[days]) return cache[days];
    return '100.000';
  };

  const getUptimeRateClass = (rateStr) => {
    const rate = parseFloat(rateStr);
    if (rate >= 99) return 'text-kumo-success';
    if (rate >= 95) return 'text-kumo-warning';
    return 'text-kumo-danger';
  };

  const formatUptimeRateCompact = (rateStr) => {
    const rate = Number(rateStr);
    return Number.isFinite(rate) ? String(Math.round(rate)) : '--';
  };

  // 格式化连接地址
  const getDisplayUrl = (monitor) => {
    if (monitor.type === 'http' || monitor.type === 'keyword' || monitor.type === 'json') {
      return monitor.url;
    }
    if (monitor.type === 'tcp') {
      return `${monitor.hostname}:${monitor.port}`;
    }
    if (monitor.type === 'push') {
      return monitor.pushToken ? `/api/uptime/push/${monitor.pushToken}` : 'Push token 待生成';
    }
    return monitor.hostname;
  };

  const getUptimeTypeIcon = (type) => {
    switch (type) {
      case 'http':
      case 'keyword':
      case 'json':
        return <Globe className="w-3.5 h-3.5" />;
      case 'tcp':
        return <Terminal className="w-3.5 h-3.5" />;
      default:
        return <Activity className="w-3.5 h-3.5" />;
    }
  };

  const formatDateTime = (timeStr) => {
    if (!timeStr) return '--';
    return new Date(timeStr).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });
  };

  // ==================== 5. 单项控制与 CRUD ====================
  const handleToggleActive = async (monitor) => {
    try {
      const res = await fetch(`/api/uptime/monitors/${monitor.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (res.ok) {
        setUptimeMonitors(prev =>
          prev.map(m => (m.id === monitor.id ? { ...m, active: data.active } : m))
        );
        toast.success(data.active ? '监测目标已恢复' : '监测目标已暂停');
      } else {
        toast.error('切换状态失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('操作异常');
    }
  };

  const handleDeleteMonitor = async (id) => {
    const monitor = uptimeMonitors.find(item => item.id === id);
    if (!confirmPress(`monitor:${id}`, `删除监测目标「${monitor?.name || '#' + id}」`)) return;
    try {
      const res = await fetch(`/api/uptime/monitors/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      if (res.ok) {
        setUptimeMonitors(prev => prev.filter(m => m.id !== id));
        setSelectedMonitorIds(prev => prev.filter(x => x !== id));
        if (expandedMonitorId === id) setExpandedMonitorId(null);
        toast.success('监测目标已删除');
      } else {
        toast.error('删除目标失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('删除目标失败');
    }
  };

  // ==================== 6. 批量操作 ====================
  const isAllSelected = useMemo(() => {
    if (filteredMonitors.length === 0) return false;
    return filteredMonitors.every(m => selectedMonitorIds.includes(m.id));
  }, [filteredMonitors, selectedMonitorIds]);
  const showMonitorSelectionControls = monitorSelectionMode || selectedMonitorIds.length > 0;

  const handleToggleSelectAll = () => {
    if (isAllSelected) {
      const filteredIds = filteredMonitors.map(m => m.id);
      setSelectedMonitorIds(prev => prev.filter(id => !filteredIds.includes(id)));
    } else {
      const newIds = [...selectedMonitorIds];
      filteredMonitors.forEach(m => {
        if (!newIds.includes(m.id)) {
          newIds.push(m.id);
        }
      });
      setSelectedMonitorIds(newIds);
    }
  };

  const handleBatchDelete = async () => {
    if (selectedMonitorIds.length === 0) return;
    if (!confirmPress('batch-delete-monitors', `批量删除选中的 ${selectedMonitorIds.length} 个监测目标`)) return;

    try {
      const res = await fetch('/api/uptime/monitors/batch-delete', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ ids: selectedMonitorIds })
      });
      const data = await res.json();
      if (res.ok) {
        setUptimeMonitors(prev => prev.filter(m => !selectedMonitorIds.includes(m.id)));
        setSelectedMonitorIds([]);
        setExpandedMonitorId(null);
        toast.success(`成功删除 ${data.count || selectedMonitorIds.length} 个监测目标`);
      } else {
        toast.error(data.error || '批量删除失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('批量删除失败');
    }
  };

  // ==================== 7. 表单操作 ====================
  const handleOpenAdd = () => {
    const defaultChannels = notificationChannels
      .filter(c => c.enabled === true || c.enabled === 1)
      .map(c => c.id);

    setUptimeForm({
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
      jsonQueryPath: '',
      jsonQueryOperator: 'equals',
      jsonExpectedValue: '',
      dns_resolve_type: 'A',
      dns_resolve_server: '',
      pushToken: '',
      pushGraceSeconds: 120,
      headers: '',
      body: '',
      ignoreTls: false,
      expiryNotification: 7,
      tagsInput: '',
      notificationChannels: defaultChannels
    });
    setUptimeCurrentTab('add');
  };

  const handleOpenEdit = (monitor) => {
    setUptimeForm({
      id: monitor.id,
      name: monitor.name || '',
      type: monitor.type || 'http',
      url: monitor.url || '',
      hostname: monitor.hostname || '',
      port: monitor.port || 443,
      method: monitor.method || 'GET',
      interval: monitor.interval || 60,
      timeout: monitor.timeout || 30,
      retries: monitor.retries || 0,
      active: !!monitor.active,
      accepted_status_codes: monitor.accepted_status_codes || '200-299',
      keyword: monitor.keyword || '',
      jsonQueryPath: monitor.jsonQueryPath || monitor.config?.jsonQueryPath || '',
      jsonQueryOperator: monitor.jsonQueryOperator || monitor.config?.jsonQueryOperator || 'equals',
      jsonExpectedValue: monitor.jsonExpectedValue || monitor.config?.jsonExpectedValue || '',
      dns_resolve_type: monitor.dns_resolve_type || 'A',
      dns_resolve_server: monitor.dns_resolve_server || '',
      pushToken: monitor.pushToken || '',
      pushGraceSeconds: monitor.pushGraceSeconds || monitor.config?.graceSeconds || 120,
      headers: monitor.headers || '',
      body: monitor.body || '',
      ignoreTls: !!monitor.ignoreTls,
      expiryNotification: monitor.expiryNotification || 7,
      tagsInput: Array.isArray(monitor.tags) ? monitor.tags.join(',') : '',
      notificationChannels: monitor.notificationChannels || []
    });
    setUptimeCurrentTab('add');
  };

  const handleSaveMonitor = async () => {
    if (!uptimeForm.name.trim()) {
      toast.warning('请输入显示名称');
      return;
    }
    if (['http', 'keyword', 'json'].includes(uptimeForm.type) && !uptimeForm.url.trim()) {
      toast.warning('请输入 URL');
      return;
    }
    if (['tcp', 'ping', 'dns'].includes(uptimeForm.type) && !uptimeForm.hostname.trim()) {
      toast.warning('请输入 Hostname');
      return;
    }
    if (uptimeForm.type === 'json' && !uptimeForm.jsonQueryPath.trim()) {
      toast.warning('请输入 JSON 查询路径');
      return;
    }

    // 处理标签数组
    const tags = uptimeForm.tagsInput
      ? uptimeForm.tagsInput.split(/[,，]/).map(t => t.trim()).filter(Boolean)
      : [];

    setUptimeSaving(true);
    try {
      const isEdit = !!uptimeForm.id;
      const url = isEdit ? `/api/uptime/monitors/${uptimeForm.id}` : '/api/uptime/monitors';
      const method = isEdit ? 'PUT' : 'POST';

      const payload = {
        ...uptimeForm,
        tags,
        notificationChannels: uptimeForm.notificationChannels,
        config: {
          jsonQueryPath: uptimeForm.jsonQueryPath,
          jsonQueryOperator: uptimeForm.jsonQueryOperator,
          jsonExpectedValue: uptimeForm.jsonExpectedValue,
          graceSeconds: uptimeForm.pushGraceSeconds,
        },
      };

      const res = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(payload)
      });
      const result = await res.json();

      if (res.ok) {
        toast.success(isEdit ? '监测目标已更新' : '监测目标已创建');
        setUptimeCurrentTab('list');
        await loadUptimeMonitors();
      } else {
        toast.error(result.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('保存请求异常');
    } finally {
      setUptimeSaving(false);
    }
  };

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 sm:gap-4">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={uptimeCurrentTab}
          onValueChange={(value) => {
            if (value === 'add') {
              handleOpenAdd();
              return;
            }
            setUptimeCurrentTab(value);
            if (value === 'status-pages') loadUptimeStatusPages();
            if (value === 'maintenance') loadUptimeMaintenance();
          }}
          tabs={[
            { value: 'list', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />仪表盘</span> },
            { value: 'add', label: <span className="inline-flex items-center gap-1.5"><Plus className="w-3.5 h-3.5" />添加监测</span> },
            { value: 'status-pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-3.5 h-3.5" />状态页</span> },
            { value: 'maintenance', label: <span className="inline-flex items-center gap-1.5"><Shield className="w-3.5 h-3.5" />维护窗口</span> },
            { value: 'stats', label: <span className="inline-flex items-center gap-1.5"><Upload className="w-3.5 h-3.5" />配置迁移</span> },
          ]}
        />

        {uptimeCurrentTab === 'list' && (
          <div className="flex min-w-0 items-center gap-2">
            <ResponsiveSearchInput
              value={uptimeSearchText}
              onChange={(e) => setUptimeSearchText(e.target.value)}
              placeholder="搜索监测目标..."
              ariaLabel="搜索监测目标"
              className="md:w-56"
            />

            <TabBarOverflowActions
              items={[
                {
                  key: 'add-target',
                  label: '新建目标',
                  icon: <Plus className="w-4 h-4" />,
                  onClick: handleOpenAdd,
                  variant: 'primary',
                },
              ]}
            />
          </div>
        )}
      </div>

      {/* ==================== 1. 监测目标仪表盘 (Dashboard) ==================== */}
      {uptimeCurrentTab === 'list' && (
        <div className="space-y-4">
          {/* 可用状态概览胶囊栏 */}
          <div className="flex flex-wrap items-center gap-2 pb-2">
            <Button
              onClick={() => setUptimeStatusFilter(null)}
              variant={uptimeStatusFilter === null ? 'primary' : 'secondary'} size="sm"
            >
              全部 ({uptimeMonitors.length})
            </Button>
            <Button
              onClick={() => setUptimeStatusFilter('up')}
              variant="secondary" size="sm"
              className={uptimeStatusFilter === 'up' ? 'text-kumo-success ring-kumo-success/30' : ''}
            >
              正常 ({uptimeStats.up})
            </Button>
            <Button
              onClick={() => setUptimeStatusFilter('down')}
              variant="secondary" size="sm"
              className={uptimeStatusFilter === 'down' ? 'text-kumo-danger ring-kumo-danger/30' : ''}
            >
              故障 ({uptimeStats.down})
            </Button>
            <Button
              onClick={() => setUptimeStatusFilter('pending')}
              variant="secondary" size="sm"
              className={uptimeStatusFilter === 'pending' ? 'text-kumo-warning ring-kumo-warning/30' : ''}
            >
              等待 ({uptimeStats.pending})
            </Button>

            <Button
              onClick={() => {
                if (showMonitorSelectionControls) {
                  setSelectedMonitorIds([]);
                  setMonitorSelectionMode(false);
                } else {
                  setMonitorSelectionMode(true);
                }
              }}
              variant={showMonitorSelectionControls ? 'primary' : 'secondary'} size="sm"
            >
              {showMonitorSelectionControls ? '取消选择' : '选择'}
            </Button>

            <Button
              onClick={loadUptimeMonitors}
              loading={uptimeLoading}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新监测目标"
              className="ml-auto"
              title="刷新"
            >
              {!uptimeLoading && <RotateCw className="w-3.5 h-3.5" />}
            </Button>
          </div>

          {uptimeLoading && uptimeMonitors.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle">
              <Loader size={32} className="text-kumo-brand mb-4" />
              <span>载入监控目标中...</span>
            </div>
          ) : filteredMonitors.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <Activity className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">
                {uptimeSearchText ? '未找到匹配的监测目标' : '暂无监测目标，开始添加一个吧'}
              </div>
              {!uptimeSearchText && (
                <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAdd}>
                  添加第一个监测
                </Button>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              {/* 批量控制条 */}
              {showMonitorSelectionControls && (
                <AppCard padding="none" className="flex items-center justify-between bg-kumo-recessed/30 px-4 py-2.5">
                  <Checkbox
                    checked={isAllSelected}
                    onCheckedChange={handleToggleSelectAll}
                    label={`全选 (已选 ${selectedMonitorIds.length} 个)`}
                  />
                  {selectedMonitorIds.length > 0 && (
                    <Button
                      variant={isArmed('batch-delete-monitors') ? 'destructive' : 'secondary-destructive'} size="sm"
                      onClick={handleBatchDelete}
                      icon={<Trash className="w-3 h-3" />}
                    >
                      批量删除
                    </Button>
                  )}
                </AppCard>
              )}

              {/* 监测卡片列表 */}
              <div className="flex flex-col gap-3">
                {filteredMonitors.map((monitor) => {
                  const beats = uptimeHeartbeats[monitor.id] || [];
                  const lastBeat = beats[0];
                  const isExpanded = expandedMonitorId === monitor.id;

                  // 状态指示
                  let statusClass = 'border-kumo-interact/75';
                  let statusPillClass = 'bg-kumo-line/20 text-kumo-subtle';
                  let statusText = '暂停/未激活';

                  if (monitor.active) {
                    if (!lastBeat) {
                      statusClass = 'border-kumo-interact/75';
                      statusPillClass = 'bg-kumo-line/20 text-kumo-subtle';
                      statusText = '等待中';
                    } else if (lastBeat.status === 'up') {
                      statusClass = 'border-kumo-interact/75';
                      statusPillClass = 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20';
                      statusText = '正常';
                    } else if (lastBeat.status === 'down') {
                      statusClass = 'border-kumo-interact/75';
                      statusPillClass = 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20';
                      statusText = '故障';
                    } else if (lastBeat.status === 'pending') {
                      statusClass = 'border-kumo-interact/75';
                      statusPillClass = 'bg-kumo-warning/10 text-kumo-warning border border-kumo-warning/20';
                      statusText = '检测中';
                    }
                  }

                  // 30 个心跳迷你丸
                  const miniBeats = [];
                  for (let i = 0; i < 30; i++) {
                    const beat = beats[i];
                    if (beat) {
                      miniBeats.unshift(beat);
                    } else {
                      miniBeats.unshift({ status: 'empty' });
                    }
                  }

                  return (
                    <div
                      key={monitor.id}
                      className={`overflow-hidden rounded-lg border bg-kumo-base ${statusClass}`}
                    >
                      {/* 卡片头部行 */}
                      <div
                        onClick={() => setExpandedMonitorId(isExpanded ? null : monitor.id)}
                        className="flex flex-col md:flex-row items-start md:items-center justify-between p-2 gap-4 cursor-pointer transition-colors hover:bg-kumo-recessed/25"
                      >
                        {/* 左侧选择复选框 & 图标 & 核心信息 */}
                        <div className="flex items-center gap-3 min-w-0 flex-1">
                          {showMonitorSelectionControls && (
                            <Checkbox
                              checked={selectedMonitorIds.includes(monitor.id)}
                              onCheckedChange={(checked) => {
                                setSelectedMonitorIds(prev =>
                                  checked ? [...prev, monitor.id] : prev.filter(id => id !== monitor.id)
                                );
                              }}
                              onClick={(e) => e.stopPropagation()}
                              aria-label={`选择监测目标: ${monitor.name}`}
                            />
                          )}

                          {/* 类型图标 */}
                          <div className="w-8 h-8 rounded-lg border border-kumo-line/70 bg-kumo-recessed flex items-center justify-center text-kumo-strong flex-shrink-0">
                            {getUptimeTypeIcon(monitor.type)}
                          </div>

                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-xs font-bold text-kumo-strong truncate">
                                {monitor.name}
                              </span>
                              <span className={`text-[9px] px-1.5 py-0.5 rounded font-semibold ${statusPillClass}`}>
                                {statusText}
                              </span>
                              {/* 标签 */}
                              {monitor.tags && monitor.tags.map(t => (
                                <StatusBadge key={t} tone="neutral" className="text-[9px] font-medium">
                                  {t}
                                </StatusBadge>
                              ))}
                              {/* SSL 证书到期徽章 */}
                              {monitor.sslExpiry && (() => {
                                const daysLeft = Math.floor((new Date(monitor.sslExpiry) - Date.now()) / 86400000);
                                let sslColor = 'bg-kumo-success/10 text-kumo-success border-kumo-success/20';
                                if (daysLeft <= 7) sslColor = 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20';
                                else if (daysLeft <= 30) sslColor = 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20';
                                return (
                                  <span className={`text-[9px] px-1.5 py-0.5 rounded font-semibold border flex items-center gap-1 ${sslColor}`}>
                                    <Shield className="w-2.5 h-2.5" />
                                    SSL {daysLeft}天
                                  </span>
                                );
                              })()}
                            </div>
                            <div className="text-[10px] text-kumo-subtle truncate max-w-[320px] mt-1 select-all" onClick={(e) => e.stopPropagation()}>
                              <span className="select-none">频率[{monitor.interval}s]</span>
                              <span className="text-kumo-subtle/40 mx-1.5 select-none">•</span>
                              {getDisplayUrl(monitor)}
                            </div>
                          </div>
                        </div>

                        {/* 右侧数据 & Heartbeat 迷你丸列 */}
                        <div className="flex items-center gap-4 w-full md:w-auto justify-between md:justify-end flex-shrink-0">
                          {/* 实时响应时延 & 可用率 */}
                          <div className="flex items-center gap-3 text-right">
                            <div className="flex flex-col">
                              <span className="text-[9px] text-kumo-subtle select-none">时延</span>
                              <span className="text-xs font-bold tabular-nums text-kumo-strong">
                                {lastBeat && lastBeat.status === 'up' ? `${lastBeat.ping}ms` : '--'}
                              </span>
                            </div>
                            <div className="flex flex-col">
                              <span className="text-[9px] text-kumo-subtle select-none">可用率</span>
                              <span className={`text-xs font-bold tabular-nums ${getUptimeRateClass(getUptimeRate(monitor.id, 1))}`}>
                                {formatUptimeRateCompact(getUptimeRate(monitor.id, 1))}%
                              </span>
                            </div>
                          </div>

                          {/* 30 心跳丸小条 */}
                          <div className="grid h-4 shrink-0 grid-cols-[repeat(30,4px)] items-center gap-[4px] select-none">
                            {miniBeats.map((beat, idx) => {
                              let colorClass = 'bg-kumo-line opacity-20';
                              if (beat.status === 'up') colorClass = 'bg-kumo-success';
                              if (beat.status === 'down') colorClass = 'bg-kumo-danger';
                              if (beat.status === 'pending') colorClass = 'bg-kumo-warning';
                              return (
                                <div key={idx} className={`h-[14px] w-[4px] rounded-full ${colorClass}`} />
                              );
                            })}
                          </div>
                        </div>
                      </div>

                      {/* 卡片下半部详情抽屉 */}
                      <AnimatedCollapse open={isExpanded}>
                        <UptimeMonitorDetails
                          monitor={monitor}
                          heartbeats={beats}
                          loading={!!uptimeHeartbeatLoading[monitor.id]}
                          uptime24h={formatUptimeRateCompact(getUptimeRate(monitor.id, 1))}
                          uptime30d={formatUptimeRateCompact(getUptimeRate(monitor.id, 30))}
                          isDarkMode={isDarkMode}
                          onPauseResume={handleToggleActive}
                          onEdit={handleOpenEdit}
                          onDelete={handleDeleteMonitor}
                          expanded={isExpanded}
                        />
                      </AnimatedCollapse>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      )}

      {uptimeCurrentTab === 'status-pages' && (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(24rem,0.9fr)_minmax(0,1.1fr)]">
          <SectionCard
            title={statusPageForm.id ? '编辑状态页' : '新建状态页'}
            icon={<Globe className="h-4 w-4 text-kumo-brand" />}
            action={statusPageForm.id ? (
              <Button size="sm" variant="secondary" shape="square" icon={<X className="h-3.5 w-3.5" />} onClick={resetStatusPageForm} aria-label="取消编辑" />
            ) : null}
            bodyPadding="lg"
            bodyClassName="space-y-4"
          >

            <div className="grid gap-3 sm:grid-cols-2">
              <Input
                size="sm"
                label="名称"
                value={statusPageForm.title}
                onChange={(event) => setStatusPageForm(prev => ({
                  ...prev,
                  title: event.target.value,
                  slug: prev.slug || normalizeStatusSlug(event.target.value),
                }))}
                placeholder="DSUK Hub 状态"
              />
              <Input
                size="sm"
                label="Slug"
                value={statusPageForm.slug}
                onChange={(event) => setStatusPageForm(prev => ({ ...prev, slug: normalizeStatusSlug(event.target.value) }))}
                placeholder="demo"
              />
              <Input
                size="sm"
                label="自定义域名"
                value={statusPageForm.domain}
                onChange={(event) => setStatusPageForm(prev => ({ ...prev, domain: normalizeStatusDomain(event.target.value) }))}
                placeholder="status.example.com"
              />
              <Input
                size="sm"
                label="缓存秒数"
                type="number"
                min="30"
                value={statusPageForm.cacheSeconds}
                onChange={(event) => setStatusPageForm(prev => ({ ...prev, cacheSeconds: event.target.value }))}
              />
              <div className="sm:col-span-2">
                <Textarea
                  size="sm"
                  label="说明"
                  value={statusPageForm.description}
                  onChange={(event) => setStatusPageForm(prev => ({ ...prev, description: event.target.value }))}
                  placeholder="可选说明"
                  rows={3}
                />
              </div>
            </div>

            <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
              <div className="min-w-0">
                <div className="text-sm font-semibold text-kumo-strong">公开访问</div>
                <div className="mt-1 text-xs text-kumo-subtle">关闭后公开 API 和单页都会返回不可用。</div>
              </div>
              <Switch checked={!!statusPageForm.public} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, public: checked }))} />
            </div>

            <div className="grid gap-2 sm:grid-cols-2">
              <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
                <div className="min-w-0">
                  <div className="text-sm font-semibold text-kumo-strong">隐藏地址</div>
                  <div className="mt-1 text-xs text-kumo-subtle">公开页不直接显示监测目标 URL。</div>
                </div>
                <Switch checked={!!statusPageForm.hideTargets} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, hideTargets: checked }))} />
              </div>
              <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
                <div className="min-w-0">
                  <div className="text-sm font-semibold text-kumo-strong">名称跳转</div>
                  <div className="mt-1 text-xs text-kumo-subtle">点击服务名称打开对应网页。</div>
                </div>
                <Switch checked={!!statusPageForm.linkMonitorNames} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, linkMonitorNames: checked }))} />
              </div>
              <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3">
                <div className="min-w-0">
                  <div className="text-sm font-semibold text-kumo-strong">首页快捷卡片</div>
                  <div className="mt-1 text-xs text-kumo-subtle">在仪表盘显示跳转到此状态页的快捷入口。</div>
                </div>
                <Switch checked={!!statusPageForm.showOnDashboard} onCheckedChange={(checked) => setStatusPageForm(prev => ({ ...prev, showOnDashboard: checked }))} />
              </div>
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between gap-2">
                <div className="text-sm font-semibold text-kumo-strong">绑定监测目标</div>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setStatusPageForm(prev => ({ ...prev, monitorIds: uptimeMonitors.map(item => item.id) }))}
                  disabled={uptimeMonitors.length === 0}
                >
                  全选
                </Button>
              </div>
              <div className="max-h-64 overflow-y-auto rounded-lg border border-kumo-line bg-kumo-base p-2 scrollbar-thin">
                {uptimeMonitors.length === 0 ? (
                  <div className="p-4 text-center text-xs text-kumo-subtle">暂无监测目标</div>
                ) : (
                  <div className="grid gap-1.5">
                    {uptimeMonitors.map((monitor) => (
                      <label key={monitor.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed">
                        <Checkbox
                          checked={statusPageForm.monitorIds.includes(monitor.id)}
                          onCheckedChange={(checked) => toggleStatusPageMonitor(monitor.id, checked)}
                          aria-label={`绑定 ${monitor.name}`}
                        />
                        <span className="min-w-0 flex-1 truncate text-sm text-kumo-strong">{monitor.name}</span>
                        <span className="hidden max-w-[12rem] truncate font-mono text-[10px] text-kumo-subtle sm:block">{getDisplayUrl(monitor)}</span>
                      </label>
                    ))}
                  </div>
                )}
              </div>
            </div>

            <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
              <div className="font-semibold text-kumo-strong">预览地址</div>
              <div className="mt-2 space-y-1 font-mono">
                <div className="truncate">{getStatusPagePublicUrl(statusPageForm, 'status')}</div>
                <div className="truncate">{getStatusPagePublicUrl(statusPageForm, 'u')}</div>
                {getStatusPageDomainUrl(statusPageForm) && <div className="truncate">{getStatusPageDomainUrl(statusPageForm)}</div>}
              </div>
            </div>

            <div className="flex flex-wrap justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={resetStatusPageForm}>重置</Button>
              <Button size="sm" variant="primary" loading={uptimeMetaLoading} onClick={saveStatusPage} icon={<Save className="h-3.5 w-3.5" />}>
                {statusPageForm.id ? '保存状态页' : '创建状态页'}
              </Button>
            </div>
          </SectionCard>

          <SectionCard
            title="已发布状态页"
            icon={<Globe className="h-4 w-4 text-kumo-brand" />}
            className="self-start"
            actions={(
              <>
                <Button size="sm" variant="secondary" icon={<RotateCw className="h-3.5 w-3.5" />} onClick={loadUptimeStatusPages} disabled={uptimeMetaLoading}>刷新</Button>
                <Button size="sm" variant="secondary" icon={<Plus className="h-3.5 w-3.5" />} onClick={createDefaultStatusPage} disabled={uptimeMetaLoading}>默认页</Button>
              </>
            )}
            bodyPadding="lg"
            bodyClassName="space-y-4"
          >

            {uptimeMetaLoading && uptimeStatusPages.length === 0 ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, index) => <SkeletonLine key={index} className="h-16 w-full" />)}
              </div>
            ) : uptimeStatusPages.length === 0 ? (
              <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
                <Globe className="mb-3 h-8 w-8 opacity-40" />
                暂无状态页，创建一个公开单页后即可分享。
              </div>
            ) : (
              <div className="grid gap-3">
                {uptimeStatusPages.map((page) => {
                  const statusUrl = getStatusPagePublicUrl(page, 'status');
                  const compactUrl = getStatusPagePublicUrl(page, 'u');
                  const domainUrl = getStatusPageDomainUrl(page);
                  return (
                    <div key={page.id} className="rounded-lg border border-kumo-line bg-kumo-base p-3">
                      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-kumo-brand/10 text-kumo-brand">
                              <PublicPageBrandIcon pageKind="uptime" config={page.config} iconClassName="h-4 w-4" customIconClassName="h-4 w-4" />
                            </span>
                            <span className="truncate text-sm font-bold text-kumo-strong">{page.title || page.slug}</span>
                            <span className={`rounded px-2 py-0.5 text-[10px] font-semibold ${page.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>
                              {page.public ? '公开' : '私有'}
                            </span>
                            <span className="rounded bg-kumo-recessed px-2 py-0.5 font-mono text-[10px] text-kumo-subtle">{page.cacheSeconds || 300}s</span>
                          </div>
                          <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">{page.slug}</div>
                          {page.description && <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{page.description}</div>}
                        </div>
                        <div className="flex shrink-0 flex-wrap gap-2">
                          <Button size="sm" variant="secondary" shape="square" icon={<Edit className="h-3.5 w-3.5" />} onClick={() => editStatusPage(page)} aria-label="编辑状态页" />
                          <Button size="sm" variant="secondary" shape="square" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={() => window.open(statusUrl, '_blank', 'noopener,noreferrer')} aria-label="打开状态页" />
                          <Button size="sm" variant="secondary" shape="square" icon={<Copy className="h-3.5 w-3.5" />} onClick={() => copyStatusUrl(statusUrl)} aria-label="复制状态页地址" />
                          <Button size="sm" variant={isArmed(`status-page:${page.id}`) ? 'destructive' : 'secondary-destructive'} shape="square" icon={<Trash className="h-3.5 w-3.5" />} onClick={() => deleteStatusPage(page)} aria-label="删除状态页" />
                        </div>
                      </div>
                      <div className="mt-3 grid gap-2 text-xs">
                        <ClipboardText size="sm" text={statusUrl} className="min-w-0 w-full" tooltip={{ text: '复制状态页地址', copiedText: '地址已复制' }} />
                        <ClipboardText size="sm" text={compactUrl} className="min-w-0 w-full" tooltip={{ text: '复制 /u 地址', copiedText: '地址已复制' }} />
                        {domainUrl && (
                          <ClipboardText size="sm" text={domainUrl} className="min-w-0 w-full" tooltip={{ text: '复制自定义域名', copiedText: '地址已复制' }} />
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </SectionCard>
        </div>
      )}

      {uptimeCurrentTab === 'maintenance' && (
        <SectionCard
          title="维护窗口"
          description="维护期内抑制告警"
          icon={<Shield className="h-4 w-4 text-kumo-brand" />}
          actions={(
            <>
              <Button
                size="sm"
                variant="secondary"
                icon={<RotateCw className="w-3.5 h-3.5" />}
                onClick={loadUptimeMaintenance}
                disabled={uptimeMetaLoading}
              >
                刷新
              </Button>
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="w-3.5 h-3.5" />}
                onClick={createQuickMaintenance}
                disabled={uptimeMetaLoading}
              >
                创建 1 小时窗口
              </Button>
            </>
          )}
          bodyPadding="lg"
          bodyClassName="space-y-5"
        >
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-kumo-subtle">
            <span>已选 <span className="font-semibold text-kumo-strong">{selectedMonitorIds.length}</span> 个监测目标，可直接用于快速维护窗口。</span>
            <span>当前共 <span className="font-semibold text-kumo-strong">{uptimeMaintenanceWindows.length}</span> 个维护窗口。</span>
            <span>
              当前生效{' '}
              <span className={`font-semibold ${uptimeMaintenanceWindows.filter((item) => {
                const start = item.startAt ? new Date(item.startAt).getTime() : null;
                const end = item.endAt ? new Date(item.endAt).getTime() : null;
                const now = Date.now();
                return item.active && Number.isFinite(start) && Number.isFinite(end) && start <= now && end >= now;
              }).length > 0 ? 'text-kumo-warning' : 'text-kumo-strong'}`}>
                {uptimeMaintenanceWindows.filter((item) => {
                  const start = item.startAt ? new Date(item.startAt).getTime() : null;
                  const end = item.endAt ? new Date(item.endAt).getTime() : null;
                  const now = Date.now();
                  return item.active && Number.isFinite(start) && Number.isFinite(end) && start <= now && end >= now;
                }).length}
              </span>{' '}
              个。
            </span>
          </div>

          {uptimeMetaLoading ? (
            <div className="space-y-3">
              {Array.from({ length: 3 }).map((_, index) => <SkeletonLine key={index} className="h-14 w-full" />)}
            </div>
          ) : uptimeMaintenanceWindows.length === 0 ? (
            <EmptyState
              icon={Shield}
              title="暂无维护窗口"
              description="维护期内抑制告警"
              action={(
                <Button size="sm" variant="primary" icon={<Plus className="w-3.5 h-3.5" />} onClick={createQuickMaintenance}>
                  创建 1 小时窗口
                </Button>
              )}
            />
          ) : (
            <AppCard padding="none" className="overflow-hidden">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/20 px-4 py-3">
                <div>
                  <div className="text-sm font-semibold text-kumo-strong">维护窗口列表</div>
                  <div className="mt-1 text-xs text-kumo-subtle">查看维护计划。</div>
                </div>
                <div className="text-xs text-kumo-subtle">
                  共 <span className="font-semibold text-kumo-strong">{uptimeMaintenanceWindows.length}</span> 条记录
                </div>
              </div>

              <DataTableFrame variant="embedded">
                <Table layout="fixed">
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head className="w-52">标题</Table.Head>
                      <Table.Head className="w-24 text-center">状态</Table.Head>
                      <Table.Head>时间窗口</Table.Head>
                      <Table.Head className="w-28">策略</Table.Head>
                      <Table.Head className="w-24">时区</Table.Head>
                      <Table.Head className="w-36">更新时间</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {uptimeMaintenanceWindows.map((item) => {
                      const start = item.startAt ? new Date(item.startAt).getTime() : null;
                      const end = item.endAt ? new Date(item.endAt).getTime() : null;
                      const now = Date.now();
                      const isActiveNow = item.active && Number.isFinite(start) && Number.isFinite(end) && start <= now && end >= now;
                      const isUpcoming = item.active && Number.isFinite(start) && start > now;
                      return (
                        <Table.Row key={item.id}>
                          <Table.Cell className="font-semibold text-kumo-strong truncate">
                            {item.title}
                          </Table.Cell>
                          <Table.Cell className="text-center">
                            {isActiveNow ? (
                              <StatusBadge tone="warning">生效中</StatusBadge>
                            ) : isUpcoming ? (
                              <StatusBadge tone="info">待开始</StatusBadge>
                            ) : item.active ? (
                              <StatusBadge tone="success">启用</StatusBadge>
                            ) : (
                              <StatusBadge tone="neutral">停用</StatusBadge>
                            )}
                          </Table.Cell>
                          <Table.Cell className="font-mono text-xs text-kumo-subtle truncate">
                            {formatDateTime(item.startAt)} - {formatDateTime(item.endAt)}
                          </Table.Cell>
                          <Table.Cell className="text-xs">
                            {item.strategy || 'manual'}
                          </Table.Cell>
                          <Table.Cell className="text-xs">
                            {item.timezone || 'UTC'}
                          </Table.Cell>
                          <Table.Cell className="text-xs text-kumo-subtle">
                            {formatDateTime(item.updatedAt || item.createdAt)}
                          </Table.Cell>
                        </Table.Row>
                      );
                    })}
                  </Table.Body>
                </Table>
              </DataTableFrame>
            </AppCard>
          )}
        </SectionCard>
      )}

      {/* ==================== 2. 添加/修改监测目标 ==================== */}
      {uptimeCurrentTab === 'add' && (
        <SectionCard
          title={uptimeForm.id ? '编辑监测目标' : '新建监测目标'}
          icon={<Activity className="h-4 w-4 text-kumo-brand" />}
          bodyPadding="xl"
          bodyClassName="space-y-6"
        >

          <div className="grid grid-cols-1 md:grid-cols-12 gap-5">
            {/* 监控类型选择 (Full Width) */}
            <div className="md:col-span-12 space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">监测类型</label>
              <Tabs
                {...TOOL_TABS_PROPS}
                value={uptimeForm.type}
                onValueChange={(value) => setUptimeForm(prev => ({ ...prev, type: value }))}
                tabs={[
                  { value: 'http', label: 'HTTP(s)' },
                  { value: 'keyword', label: '网页关键词' },
                  { value: 'json', label: 'JSON 查询' },
                  { value: 'tcp', label: 'TCP 端口' },
                  { value: 'ping', label: 'ICMP Ping' },
                  { value: 'dns', label: 'DNS 解析' },
                  { value: 'push', label: 'Push' },
                ]}
              />
            </div>

            {/* 目标显示名称 */}
            <div className="md:col-span-4">
              <Input
                label="名称 *"
                type="text" size="sm"
                placeholder="如：生产数据库端口"
                value={uptimeForm.name}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* 地址输入域 */}
            {['http', 'keyword', 'json'].includes(uptimeForm.type) ? (
              <div className="md:col-span-8">
                <Input
                  label="请求 URL *"
                  type="text" size="sm"
                  placeholder="https://api.domain.com/v1/health"
                  value={uptimeForm.url}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, url: e.target.value }))}
                  className="w-full"
                />
              </div>
            ) : uptimeForm.type === 'push' ? (
              <div className="md:col-span-8">
                <AppCard padding="none" className="bg-kumo-recessed/40 p-3">
                  <div className="text-[10px] font-semibold text-kumo-subtle">Push URL</div>
                  <div className="mt-1 truncate font-mono text-xs text-kumo-strong">
                    {uptimeForm.pushToken ? `/api/uptime/push/${uptimeForm.pushToken}` : '保存后自动生成 token URL'}
                  </div>
                </AppCard>
              </div>
            ) : (
              <>
                <div className={uptimeForm.type === 'tcp' ? 'md:col-span-6' : 'md:col-span-8'}>
                  <Input
                    label="主机名 / IP *"
                    type="text" size="sm"
                    placeholder="如：192.168.1.100 或 db.server.internal"
                    value={uptimeForm.hostname}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, hostname: e.target.value }))}
                    className="w-full"
                  />
                </div>
                {uptimeForm.type === 'tcp' && (
                  <div className="md:col-span-2">
                    <Input
                      label="连接端口 *"
                      type="number" size="sm"
                      placeholder="3306"
                      value={uptimeForm.port}
                      onChange={(e) => setUptimeForm(prev => ({ ...prev, port: parseInt(e.target.value) || 0 }))}
                      className="w-full font-mono"
                    />
                  </div>
                )}
              </>
            )}

            {/* 监测频率与重试参数 */}
            <div className="md:col-span-6">
              <Input
                label="检测频率（秒）"
                type="number" size="sm"
                min="20"
                value={uptimeForm.interval}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, interval: parseInt(e.target.value) || 60 }))}
                className="w-full font-mono"
              />
            </div>
            <div className="md:col-span-6">
              <Input
                label="重试次数"
                type="number" size="sm"
                min="0"
                value={uptimeForm.retries}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, retries: parseInt(e.target.value) || 0 }))}
                className="w-full font-mono"
              />
            </div>

            {/* 高级设置小节 */}
            <div className="md:col-span-12 border-t border-kumo-line pt-4 mt-2">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5 select-none">
                <Shield className="w-3.5 h-3.5" />
                安全与高级设置
              </h4>
            </div>

            {/* 证书过期设置 */}
            {['http', 'json'].includes(uptimeForm.type) && (
              <div className="md:col-span-6">
                <Input
                label="SSL 到期提醒（天）"
                  type="number" size="sm"
                  placeholder="7"
                  value={uptimeForm.expiryNotification}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, expiryNotification: parseInt(e.target.value) || 7 }))}
                  className="w-full font-mono"
                />
              </div>
            )}

            {/* 忽略 TLS 选项 */}
            {['http', 'keyword', 'json'].includes(uptimeForm.type) && (
              <div className="md:col-span-6 flex items-end pb-2">
                <Checkbox
                  checked={uptimeForm.ignoreTls}
                  onCheckedChange={(checked) => setUptimeForm(prev => ({ ...prev, ignoreTls: checked }))}
                  label="忽略不可信 / 自签 TLS"
                />
              </div>
            )}

            {/* 网页关键字匹配 */}
            {uptimeForm.type === 'keyword' && (
              <div className="md:col-span-12">
                <Input
                  label="关键字匹配 *"
                  type="text" size="sm"
                  placeholder="如：success 或 正常"
                  value={uptimeForm.keyword}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, keyword: e.target.value }))}
                  className="w-full"
                />
              </div>
            )}

            {uptimeForm.type === 'json' && (
              <>
                <div className="md:col-span-4">
                  <Input
                    label="JSON 路径 *"
                    type="text" size="sm"
                    placeholder="如：$.data.status"
                    value={uptimeForm.jsonQueryPath}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonQueryPath: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
                <div className="md:col-span-4">
                  <Input
                    label="操作符"
                    type="text" size="sm"
                    placeholder="如：equals / regex"
                    value={uptimeForm.jsonQueryOperator}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonQueryOperator: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
                <div className="md:col-span-4">
                  <Input
                    label="期望值"
                    type="text" size="sm"
                    placeholder="如：ok"
                    value={uptimeForm.jsonExpectedValue}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonExpectedValue: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
              </>
            )}

            {uptimeForm.type === 'push' && (
              <div className="md:col-span-6">
                <Input
                  label="Push 宽限（秒）"
                  type="number" size="sm"
                  min="30"
                  value={uptimeForm.pushGraceSeconds}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, pushGraceSeconds: parseInt(e.target.value) || 120 }))}
                  className="w-full font-mono"
                />
              </div>
            )}

            {/* 告警通知渠道设置 */}
            <div className="md:col-span-12 border-t border-kumo-line pt-4 mt-2">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5 select-none">
                <Bell className="w-3.5 h-3.5" />
                故障通知分发渠道
              </h4>
            </div>

            <div className="md:col-span-12 space-y-2">
              <AppCard padding="none" className="flex flex-wrap gap-4 bg-kumo-recessed/50 p-3.5">
                {notificationChannels.filter(c => c.enabled).map((channel) => (
                  <Checkbox
                    key={channel.id}
                    checked={uptimeForm.notificationChannels.includes(channel.id)}
                    onCheckedChange={(checked) => {
                      const id = channel.id;
                      setUptimeForm(prev => ({
                        ...prev,
                        notificationChannels: checked
                          ? [...prev.notificationChannels, id]
                          : prev.notificationChannels.filter(x => x !== id)
                      }));
                    }}
                    label={`${channel.name} (${channel.type === 'email' ? '邮箱' : 'TG'})`}
                  />
                ))}

                {notificationChannels.filter(c => c.enabled).length === 0 && (
                  <div className="text-xs text-kumo-subtle flex items-center gap-1.5 select-none w-full">
                    <Info className="w-4 h-4 text-kumo-subtle/60" />
                    <span>暂无启用的告警通道。</span>
                  </div>
                )}
              </AppCard>
            </div>

            {/* 标签管理 */}
            <div className="md:col-span-12">
              <Input
                label="分组标签"
                type="text" size="sm"
                placeholder="prod, api, test（逗号或空格分隔）"
                value={uptimeForm.tagsInput}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, tagsInput: e.target.value }))}
                className="w-full"
              />
            </div>
          </div>

          {/* 表单按钮栏 */}
          <div className="flex justify-end gap-3 border-t border-kumo-line pt-4 select-none">
            <Button size="sm" onClick={() => setUptimeCurrentTab('list')}>取消</Button>
            <Button size="sm" variant="primary" onClick={handleSaveMonitor} loading={uptimeSaving} icon={<Save className="w-3.5 h-3.5" />}>
              保存目标
            </Button>
          </div>
        </SectionCard>
      )}

      {/* ==================== 3. 配置迁移 Tab ==================== */}
      {uptimeCurrentTab === 'stats' && (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
          <SectionCard
            title="配置导入预览"
            icon={<Upload className="h-4 w-4 text-kumo-brand" />}
            actions={(
              <>
                <Input
                  ref={uptimeImportInputRef}
                  type="file"
                  accept=".json,application/json"
                  aria-label="选择 Uptime 配置文件"
                  className="hidden"
                  onChange={previewUptimeImportFile}
                />
                <Toolbar size="sm" aria-label="导出导入 Uptime 配置" className="shrink-0">
                    <Toolbar.Button onClick={exportUptimeConfig} loading={uptimeMetaLoading} aria-label="导出当前配置" icon={<Upload className="h-3.5 w-3.5" />}>
                      <span className="hidden sm:inline">导出</span>
                    </Toolbar.Button>
                    <Toolbar.Button onClick={() => uptimeImportInputRef.current?.click()} loading={uptimeMetaLoading} aria-label="导入配置文件" icon={<Download className="h-3.5 w-3.5" />}>
                      <span className="hidden sm:inline">导入</span>
                    </Toolbar.Button>
                  </Toolbar>
              </>
            )}
            bodyPadding="lg"
            bodyClassName="space-y-4"
          >

            {!uptimeImportPreview ? (
              <EmptyState
                card={false}
                icon={Upload}
                title="尚未选择配置文件"
                description="导入 JSON 并预览"
                className="min-h-[20rem] py-16"
              />
            ) : (
              <div className="space-y-4">
                <AppCard padding="none" className="bg-kumo-recessed/40 px-4 py-3.5">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="text-sm font-semibold text-kumo-strong">
                        本次将同步 {uptimeImportSummary.total} 个配置对象
                      </div>
                      <div className="text-xs leading-relaxed text-kumo-subtle">
                        系统会按现有监测、状态页和维护窗口判断创建或更新，确认前可先检查影响范围。
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <StatusBadge tone="success">创建 {uptimeImportSummary.creates}</StatusBadge>
                      <StatusBadge tone="warning">更新 {uptimeImportSummary.updates}</StatusBadge>
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-xs">
                    {uptimeImportSections.map((section) => (
                      <div key={section.key} className="flex items-center gap-2 text-kumo-subtle">
                        <span className="font-semibold text-kumo-strong">{section.title}</span>
                        <span>{section.total} 项</span>
                        {section.updates > 0 && <span>更新 {section.updates}</span>}
                      </div>
                    ))}
                  </div>
                </AppCard>

                <div className="grid gap-3 xl:grid-cols-3">
                  {uptimeImportSections.map((section) => (
                    <AppCard key={section.key} padding="none" className="overflow-hidden bg-kumo-recessed/40">
                      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-kumo-line/70 bg-kumo-recessed/20 px-4 py-3">
                        <div className="min-w-0">
                          <div className="text-sm font-semibold text-kumo-strong">{section.title}</div>
                          <div className="mt-1 text-xs leading-relaxed text-kumo-subtle">{section.description}</div>
                        </div>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <StatusBadge tone="neutral">{section.total} 项</StatusBadge>
                          {section.creates > 0 && <StatusBadge tone="success">创建 {section.creates}</StatusBadge>}
                          {section.updates > 0 && <StatusBadge tone="warning">更新 {section.updates}</StatusBadge>}
                        </div>
                      </div>

                      {section.total === 0 ? (
                        <div className="px-4 py-6 text-xs text-kumo-subtle">{section.emptyLabel}</div>
                      ) : (
                        <div className="max-h-80 overflow-y-auto">
                          {section.items.map((item) => {
                            const actionMeta = getUptimeImportActionMeta(item.action);
                            return (
                              <div
                                key={item.id}
                                className="flex items-start justify-between gap-3 border-b border-kumo-line/60 px-4 py-3 last:border-b-0"
                              >
                                <div className="min-w-0">
                                  <div className="truncate text-sm font-semibold text-kumo-strong">{item.label}</div>
                                  <div className="mt-1 truncate text-xs text-kumo-subtle">{item.detail}</div>
                                </div>
                                <StatusBadge tone={actionMeta.tone}>{actionMeta.label}</StatusBadge>
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </AppCard>
                  ))}
                </div>
              </div>
            )}
          </SectionCard>

          <SectionCard
            title="导入执行"
            icon={<Download className="h-4 w-4 text-kumo-brand" />}
            className="self-start"
            bodyPadding="lg"
            bodyClassName="space-y-3.5"
          >
            <AppCard padding="none" className="bg-kumo-recessed/40 px-4 py-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-sm font-semibold text-kumo-strong">执行状态</div>
                {uptimeImportPreview ? (
                  <StatusBadge tone="info">预览就绪</StatusBadge>
                ) : (
                  <StatusBadge tone="neutral">等待文件</StatusBadge>
                )}
              </div>
              <div className="mt-2 text-xs leading-relaxed text-kumo-subtle">
                {uptimeImportPreview
                  ? `已预览 ${uptimeImportSummary.total} 个对象，确认后会按匹配规则创建或更新。`
                  : '先选择配置文件生成预览，再决定是否导入当前 Uptime。'}
              </div>
              {uptimeImportPreview && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  <StatusBadge tone="success">创建 {uptimeImportSummary.creates}</StatusBadge>
                  <StatusBadge tone="warning">更新 {uptimeImportSummary.updates}</StatusBadge>
                  <StatusBadge tone="neutral">{uptimeImportSummary.nonEmptySections.length} 类对象</StatusBadge>
                </div>
              )}
            </AppCard>

            <div className="space-y-2 rounded-lg border border-kumo-line/70 bg-kumo-recessed/20 px-4 py-3">
              <div className="text-xs font-semibold text-kumo-strong">匹配规则</div>
              <div className="space-y-1.5 text-xs leading-relaxed text-kumo-subtle">
                <div>监测目标会按名称、类型、地址等字段匹配已有对象。</div>
                <div>状态页按 `slug` 匹配，维护窗口按标题匹配；命中后会执行更新。</div>
              </div>
            </div>

            <div className="space-y-2">
              <Button
                size="sm"
                variant="primary"
                className="w-full"
                onClick={commitUptimeImport}
                disabled={!uptimeImportPreview}
                loading={uptimeMetaLoading}
              >
                确认导入配置
              </Button>
              {uptimeImportPreview && (
                <Button
                  size="sm"
                  variant="secondary"
                  className="w-full"
                  onClick={() => {
                    setUptimeImportPreview(null);
                    setUptimeImportPayload(null);
                  }}
                >
                  清除预览
                </Button>
              )}
            </div>
          </SectionCard>
        </div>
      )}
    </div>
  );
}

export default UptimePage;
