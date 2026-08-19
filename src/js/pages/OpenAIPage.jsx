import React, { useState, useEffect, useRef, useMemo, useCallback, memo } from 'react';
import { ArrowDown, ArrowUp, CalendarDotsIcon } from '@phosphor-icons/react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button, RefreshButton } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Autocomplete } from '@cloudflare/kumo/components/autocomplete';
import {
  Chart,
  ChartLegend,
  ClipboardText,
  ChartPalette,
  Collapsible,
  Badge,
  DatePicker,
  Label,
  LayerCard,
  Loader,
  Pagination,
  Popover,
  Table,
  Tabs,
  Toolbar,
} from '@cloudflare/kumo';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { handleEditableRowDoubleClick } from '../modules/tableInteractions.js';
import { formatDateTime } from '../modules/utils.js';
import { renderMarkdown } from '../modules/markdown.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import useStore from '../store.js';
import * as echarts from 'echarts/core';
import { BarChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { createSiteFontEcharts } from '../chartFont.js';

echarts.use([
  BarChart,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
  AriaComponent,
  CanvasRenderer,
]);
const siteFontEcharts = createSiteFontEcharts(echarts);

// formatErrorResponseForDisplay 与 errorKindLabel 已迁至 ./openai/utils.js（报错展示工具）。
import {
  DEFAULT_MODEL_HEALTH_CONCURRENCY,
  DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
  countModelHealthResults,
  endpointModelIds,
  modelHealthKey,
  modelHealthTargets,
  normalizeModelHealthRecord,
  resolveModelHealthConcurrency,
} from '../modules/openaiModelHealth.js';
import {
  PageStack,
  AppCard,
  StatusBadge,
  EmptyState,
  stickyTabsBaseClass,
  TabBarOverflowActions,
  iconButtonIconClass,
  actionIconClass,
  cx,
} from '../components/ui/AppPrimitives.jsx';
import {
  Server,
  MessageSquare,
  Plus,
  Trash,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  RefreshCw,
  History,
  PieChart,
  Bot,
  Star,
  Pin,
  Activity,
  Send,
  Check,
  Paperclip,
  Brain,
  Settings as SettingsIcon,
  Copy,
  Cpu,
  AlertTriangle,
  Key,
  Reboot,
  Play,
  Clock,
  TrendingUp,
  LogList,
  Globe,
  Sliders,
  ChevronDown,
} from '../components/Icons.jsx';
import {
  ENDPOINT_PROTOCOL_OPTIONS,
  PROXY_PREVIEW_LIMIT,
  LOG_DETAIL_COLLAPSE_LIMIT,
  GATEWAY_EXPIRY_HOURS,
  GATEWAY_EXPIRY_MINUTES,
} from './openai/constants.js';
import {
  formatErrorResponseForDisplay,
  errorKindLabel,
  kumoHex,
  formatCompact,
  formatTokensM,
  createHealthCheckProgress,
  parseProxyEntry,
  activeModelIdsForEndpoint,
  resultTone,
  ttfbTone,
  statusCodeTone,
  maskIp,
  toLocalDateTimeValue,
  parseLocalDateTime,
  getAuthHeaders,
} from './openai/utils.js';
import { useAnalytics } from './openai/useAnalytics.js';
import { useGatewayKeys } from './openai/useGatewayKeys.js';

// 自绘 ECharts 柱状时间桶：每个桶(小时/天/周)一根柱，类目轴标签 = 桶名，避免时间轴重复标签。

function ProxyRuntimeMeta({ proxy, state }) {
  const hasExit = state && state.lastExitIP;
  const hasTTFB = state && Number(state.lastTTFB) > 0;
  if (!hasExit && !hasTTFB) return null;
  return (
    <div className="mt-0.5 flex items-center gap-2 font-mono text-[10px] leading-none text-kumo-subtle">
      {hasExit && (
        <span title={`出口 IP（经代理出网，探活记录）\n${proxy}`} className="truncate">
          <Globe className="mr-0.5 inline h-2.5 w-2.5" />{state.lastExitIP}
        </span>
      )}
      {hasTTFB && (
        <span title="最近一次请求的首字耗时">
          ~{(state.lastTTFB / 1000).toFixed(1)}s 首字
        </span>
      )}
    </div>
  );
}

// IpCell 展示脱敏 IP，点击弹出 Popover 显示完整 IP。
function IpCell({ value, viaProxy, placeholder, v6EdgeOnly }) {
  if (!value) return <>{placeholder || '—'}</>;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <span
            className={`cursor-pointer truncate ${viaProxy ? 'text-kumo-info' : ''}`}
          >
            {maskIp(value, v6EdgeOnly)}
          </span>
        }
      />
      <Popover.Content className="p-3 max-w-xs">
        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
          {viaProxy ? '出口 IP' : '客户端 IP'}
        </Popover.Title>
        <div className="mt-2">
          <code className="rounded bg-kumo-surface-2 px-2 py-1 text-xs font-mono text-kumo-strong select-all">
            {value}
          </code>
        </div>
      </Popover.Content>
    </Popover>
  );
}

// MultiSelectPopover 下拉多选：Popover + Checkbox 列表 + 搜索过滤，
// 用于 API 密钥编辑的「允许的模型 / 允许的端点」白名单选择。
function MultiSelectPopover({ triggerLabel, options, selected, onToggle, onClear, searchPlaceholder, emptyText }) {
  const [search, setSearch] = useState('');
  const keyword = search.trim().toLowerCase();
  const filtered = keyword
    ? options.filter(o => String(o.value).toLowerCase().includes(keyword) || String(o.label).toLowerCase().includes(keyword))
    : options;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <Button type="button" size="sm" variant="secondary" className="flex items-center gap-1.5">
            <Plus className="h-3 w-3" />
            {triggerLabel}
            {selected.length > 0 && <Badge variant="secondary">{selected.length}</Badge>}
          </Button>
        }
      />
      <Popover.Content side="bottom" align="start" className="w-72 p-2">
        <Input
          size="sm"
          type="text"
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder={searchPlaceholder}
          className="w-full"
          aria-label={searchPlaceholder}
        />
        <div className="mt-1.5 max-h-60 overflow-y-auto overscroll-contain scrollbar-thin">
          {filtered.length === 0 ? (
            <p className="px-2 py-4 text-center text-xs leading-normal text-kumo-subtle">{emptyText}</p>
          ) : (
            <div className="grid gap-0.5">
              {filtered.map(option => (
                <label
                  key={option.value}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed"
                >
                  <Checkbox
                    checked={selected.includes(option.value)}
                    onCheckedChange={checked => onToggle(option.value, !!checked)}
                    aria-label={`选择 ${option.label}`}
                  />
                  <span className="min-w-0 flex-1 truncate text-sm leading-normal text-kumo-strong">{option.label}</span>
                </label>
              ))}
            </div>
          )}
        </div>
        <div className="mt-1.5 flex items-center justify-between border-t border-kumo-line pt-1.5">
          <span className="text-xs leading-normal text-kumo-subtle">已选 {selected.length} 项</span>
          <Button
            type="button"
            size="xs"
            variant="ghost"
            disabled={selected.length === 0}
            onClick={onClear}
          >
            清空
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  );
}

// maskIp 压缩 IP 展示：去掉端口，仅保留首尾片段、中间用 *** 隐藏，用于日志表格
// 减少宽度占用。IPv4 保留前 2 段 + 后 1 段；IPv6 默认保留前 2 段 + 后 2 段，
// v6EdgeOnly 时仅保留首尾各 1 段、中间 ***::***。
// FailoverPathBadge 在端点列展示渠道迁移标记：当一次请求经历过多个端点尝试时，
// 端点名以橙色高亮，点击弹出完整迁移路径。
function FailoverPathBadge({ path, endpointName }) {
  let steps = [];
  if (path) {
    try {
      const parsed = JSON.parse(path);
      if (Array.isArray(parsed)) steps = parsed;
    } catch (e) {}
  }
  if (steps.length < 2) return <span className="truncate">{endpointName}</span>;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <span className="cursor-pointer truncate font-medium text-kumo-warning">
            {endpointName}
          </span>
        }
      />
      <Popover.Content className="p-3 max-w-xs">
        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
          渠道迁移路径
        </Popover.Title>
        <div className="mt-2 flex flex-col gap-1">
          {steps.map((s, i) => (
            <div key={`${s.endpoint}-${i}`} className="flex items-center gap-1.5 text-xs">
              <span className="font-mono truncate max-w-[140px]" title={s.endpoint}>
                {s.endpoint || 'unknown'}
              </span>
              <StatusBadge tone={statusCodeTone(s.status)}>{s.status || '-'}</StatusBadge>
              {i < steps.length - 1 && <span className="text-kumo-subtle">→</span>}
            </div>
          ))}
        </div>
      </Popover.Content>
    </Popover>
  );
}

// 端点多 key 检测结果 -> 状态徽标。check 为 null 表示该行尚未检测。
function keyCheckBadgeProps(check) {
  if (!check) return null;
  switch (check.status) {
    case 'checking':
      return { tone: 'info', label: '检测中' };
    case 'valid':
      return { tone: 'success', label: '有效' };
    case 'invalid':
      return { tone: 'danger', label: '失效' };
    case 'overdue':
      return { tone: 'warning', label: '欠费' };
    default:
      return { tone: 'neutral', label: '异常' };
  }
}

function KeyStatusBadge({ check }) {
  const props = keyCheckBadgeProps(check);
  if (!props) return <span className="w-9 shrink-0" />;
  return (
    <StatusBadge tone={props.tone} title={check?.message} className="shrink-0">
      {props.label}
    </StatusBadge>
  );
}

  // 时间序列（小时/天/周粒度）：为每根柱提供独立可对齐的类目轴。
// 后端每个桶返回 day(bucket label) + count/tokens/avgLatency/errors，仅用于柱状展示。
const TrendBarChart = memo(function TrendBarChart({
  labels,
  values,
  color,
  isDarkMode,
  loading = false,
  formatValue = value => (Number.isFinite(Number(value)) ? String(Number(value)) : String(value)),
  formatAxis = formatValue,
}) {
  const chartRef = useRef(null);
  const options = useMemo(() => {
    if (!labels || labels.length === 0) return null;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    return {
      grid: { left: 8, right: 12, top: 10, bottom: 0, containLabel: true },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        appendTo: 'body',
        backgroundColor: kumoHex('--color-kumo-base'),
        textStyle: { color: axisColor, fontSize: 11 },
        valueFormatter: formatValue,
      },
      xAxis: {
        type: 'category',
        data: labels,
        boundaryGap: false,
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
        axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: gridColor } },
        axisLabel: { color: axisColor, fontSize: 10, formatter: formatAxis },
      },
      series: [
        {
          type: 'bar',
          data: values,
          barMaxWidth: 26,
          itemStyle: { color, borderRadius: [2, 2, 0, 0] },
        },
      ],
    };
  }, [labels, values, color, isDarkMode, formatValue, formatAxis]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    if (loading) {
      chart.showLoading({
        text: '',
        color: kumoHex('--color-brand'),
        maskColor: 'rgba(0,0,0,0)',
      });
    } else {
      chart.hideLoading();
    }
  }, [loading]);

  const hasData = !!(labels && labels.length > 0);
  if (!hasData && !loading) return null;

  return <Chart ref={chartRef} echarts={siteFontEcharts} isDarkMode={isDarkMode} options={hasData ? options : {}} height={168} />;
});

// 全宽「模型 × 时间」折线趋势：类别轴（每桶唯一刻度），稀疏段断线成 Trend；
// 顶部图例按调用次数降序，颜色与折线同一份映射，点击隔离/恢复。
const ModelTrendChart = memo(function ModelTrendChart({ labels, series, isDarkMode, loading = false }) {
  const containerRef = useRef(null);
  const chartRef = useRef(null);
  const [hiddenSeries, setHiddenSeries] = useState({});

  // 排序必须完全确定：调用次数降序、相同次数按模型名升序，避免图例顺序
  // 随接口返回顺序（后端 map 迭代随机）漂移；颜色在排序后按固定位次分配，
  // 保证同一模型始终同色。
  const ordered = useMemo(() => {
    const withMeta = (series || []).map(item => ({
      model: item.model,
      total: (item.data || []).reduce((sum, value) => sum + (Number(value) || 0), 0),
      values: (item.data || []).map(value => Number(value) || 0),
    }));
    withMeta.sort((a, b) => b.total - a.total || (a.model < b.model ? -1 : a.model > b.model ? 1 : 0));
    return withMeta.map((item, index) => ({
      ...item,
      color: ChartPalette.categorical(index, isDarkMode),
    }));
  }, [series, isDarkMode]);

  const visibleSeries = useMemo(
    () => ordered.filter(item => !hiddenSeries[item.model]),
    [ordered, hiddenSeries]
  );

  // 图表实例只在挂载时初始化一次；labels/系列的更新全部走 setOption，
  // 否则 30 秒自动刷新带来的新数组引用会触发 dispose+重建，造成闪烁与交互状态丢失。
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const chart = siteFontEcharts.init(el);
    chartRef.current = chart;
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(el);
    return () => {
      observer.disconnect();
      chart.dispose();
      chartRef.current = null;
    };
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart || !labels || labels.length === 0) return;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    chart.setOption(
      {
        grid: { left: 8, right: 12, top: 8, bottom: 0, containLabel: true },
        tooltip: {
          trigger: 'axis',
          traceHigh: true,
          // 挂到 body：LayerCard 自带 overflow-hidden，悬浮框默认渲染在图表
          // 容器内会被卡片裁剪遮挡。
          appendTo: 'body',
          backgroundColor: kumoHex('--color-kumo-base'),
          textStyle: { color: axisColor, fontSize: 11 },
        },
        xAxis: {
          type: 'category',
          data: labels,
          boundaryGap: false,
          axisLine: { lineStyle: { color: gridColor } },
          axisTick: { show: false },
          axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
        },
        yAxis: {
          type: 'value',
          splitLine: { lineStyle: { color: gridColor } },
          axisLabel: {
            color: axisColor,
            fontSize: 10,
            formatter: value => formatCompact(value, 0),
          },
        },
        series: visibleSeries.map(item => ({
          type: 'line',
          name: item.model,
          data: item.values,
          smooth: true,
          connectNulls: false,
          showSymbol: true,
          symbolSize: 4,
          lineStyle: { width: 2, color: item.color },
          itemStyle: { color: item.color },
          areaStyle: { opacity: 0 },
        })),
      },
      // replaceMerge：隐藏/恢复系列时按 name 精确替换，避免默认 merge 模式下
      //「新 series 数组变短 → 按 index 合并」导致被隐藏的旧系列残留、颜色错位。
      { replaceMerge: ['series'] }
    );
  }, [labels, visibleSeries, isDarkMode]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    if (loading) {
      chart.showLoading({
        text: '',
        color: kumoHex('--color-brand'),
        maskColor: 'rgba(0,0,0,0)',
      });
    } else {
      chart.hideLoading();
    }
  }, [loading]);

  const handleClick = name => {
    setHiddenSeries(prev => {
      const isIsolated = ordered.every(
        item => (item.model === name ? !prev[item.model] : prev[item.model])
      );
      const next = {};
      for (const item of ordered) {
        next[item.model] = isIsolated ? false : item.model !== name;
      }
      return next;
    });
  };

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex flex-nowrap gap-x-4 overflow-x-auto overscroll-x-contain px-1 pb-1 touch-pan-x scrollbar-thin">
        {ordered.map(item => (
          <ChartLegend.LargeItem
            key={item.model}
            name={item.model}
            color={item.color}
            value={item.total.toLocaleString('en-US', { useGrouping: false })}
            unit="次"
            inactive={hiddenSeries[item.model] ?? false}
            onClick={() => handleClick(item.model)}
          />
        ))}
      </div>
<div ref={containerRef} className="h-[240px] w-full" />
    </div>
  );
});

function OpenAIPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const { theme } = useStore();
  const isDarkMode = theme === 'dark';
  const gatewayOrigin = useMemo(() => {
    if (typeof window === 'undefined') return 'http://localhost:3000';
    const url = new URL(window.location.origin);
    if (url.port === '5173' || url.port === '4173') url.port = '3000';
    return url.origin;
  }, []);

  // Tab State
  const [activeTab, setActiveTab] = useState('analytics'); // 'analytics' | 'endpoints' | 'keys' | 'logs'

  // Gateway Analytics（状态/拉取/SSE/日志清理由 useAnalytics 统一管理）
  const {
    analyticsDays, setAnalyticsDays,
    analyticsGranularity, setAnalyticsGranularity,
    analyticsSummary,
    tokenTrendMode, setTokenTrendMode,
    latencyTrendMode, setLatencyTrendMode,
    errorTrendMode, setErrorTrendMode,
    modelTrendMode, setModelTrendMode,
    tokenShareMode, setTokenShareMode,
    countShareMode, setCountShareMode,
    analyticsCharts,
    analyticsLogs,
    analyticsPage, setAnalyticsPage,
    analyticsPageSize, setAnalyticsPageSize,
    analyticsTotal,
    analyticsLoading,
    logStatusFilter, setLogStatusFilter,
    logModelFilter, setLogModelFilter,
    logEndpointFilter, setLogEndpointFilter,
    logDetail, setLogDetail,
    logDetailExpanded, setLogDetailExpanded,
    fetchAnalytics,
    clearGatewayLogs,
  } = useAnalytics(activeTab);

  // 网关 API 密钥（列表/表单/轮换/默认，由 useGatewayKeys 统一管理）
  const {
    gatewayKeys,
    gatewayKeysLoading,
    gatewayKeyToggleLoading,
    gatewayKeyDialogOpen, setGatewayKeyDialogOpen,
    editingGatewayKey,
    gatewayKeyForm, setGatewayKeyForm,
    gatewayKeyAdvancedOpen, setGatewayKeyAdvancedOpen,
    gatewayKeyFormError,
    gatewayKeySaving,
    newGatewayKey, setNewGatewayKey,
    loadGatewayKeys,
    defaultGatewayKey,
    openAddGatewayKeyModal,
    openEditGatewayKeyModal,
    applyGatewayKeyExpiryPreset,
    updateGatewayKeyExpiryDate,
    updateGatewayKeyExpiryTime,
    toggleGatewayKeyListItem,
    removeGatewayKeyListItem,
    saveGatewayKey,
    toggleGatewayKey,
    setDefaultGatewayKey,
    rotateGatewayKey,
    deleteGatewayKey,
  } = useGatewayKeys();

  // ==================== 1. Endpoints State ====================
  const [endpoints, setEndpoints] = useState([]);
  const [endpointsLoading, setEndpointsLoading] = useState(false);
  const [endpointsRefreshing, setEndpointsRefreshing] = useState(false);
  const [endpointToggleLoading, setEndpointToggleLoading] = useState({});
  const [selectedEndpointId, setSelectedEndpointId] = useState('');
  const [draggedEndpointId, setDraggedEndpointId] = useState(null);
  const [endpointReorderSaving, setEndpointReorderSaving] = useState(false);
  const [endpointFormOpen, setEndpointFormOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointForm, setEndpointForm] = useState({
    name: '',
    baseUrl: '',
    apiKey: '',
    apiKeys: [],
    notes: '',
    headers: [],
    proxyPool: [],
    autoSwitch: false,
    proxyEnabled: false,
    allowDirectFallback: true,
    protocol: 'auto',
  });
  const [endpointFormError, setEndpointFormError] = useState('');
  const [endpointSaving, setEndpointSaving] = useState(false);
  // 端点编辑弹窗中的多 key 状态：数组下标与 key 行对齐（0=主 key/K1，n=备用 key/K(n+1)）。
  const [endpointKeyChecks, setEndpointKeyChecks] = useState([]);
  const [endpointKeyChecking, setEndpointKeyChecking] = useState(false);

  // Batch adding endpoints
  // Load Endpoints
  const loadEndpoints = useCallback(
    async (silent = false) => {
      if (!silent) setEndpointsLoading(true);
      try {
        const response = await fetch('/api/openai/endpoints', {
          headers: getAuthHeaders(),
        });
        const data = await response.json();
        if (Array.isArray(data)) {
          setEndpoints(data.map(ep => ({ ...ep, showKey: false, refreshing: false })));
        }
      } catch (error) {
        console.error('Failed to load endpoints:', error);
        toast.error('加载端点失败');
      } finally {
        if (!silent) setEndpointsLoading(false);
      }
    },
    [getAuthHeaders]
  );

  useEffect(() => {
    localStorage.removeItem('openai_endpoints_cache');
    loadEndpoints();
  }, [loadEndpoints]);

  const endpointImportInputRef = useRef(null);
  const [endpointImporting, setEndpointImporting] = useState(false);
  const [importModeDialog, setImportModeDialog] = useState(null);
  const [endpointExporting, setEndpointExporting] = useState(false);

  const exportEndpoints = async () => {
    setEndpointExporting(true);
    try {
      const response = await fetch('/api/openai/export', { headers: getAuthHeaders() });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || payload.success !== true) throw new Error(payload.error || '导出端点失败');
      const list = Array.isArray(payload.endpoints) ? payload.endpoints : [];
      if (list.length === 0) { toast.warning('暂无端点可导出'); return; }
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `openai-endpoints-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
      toast.success(`已导出 ${list.length} 个端点（包含 API Key，请注意保管）`);
    } catch (error) {
      toast.error(error.message || '导出端点失败');
    } finally {
      setEndpointExporting(false);
    }
  };

  const importEndpointsFromFile = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setEndpointImporting(true);
    try {
      const text = await file.text();
      const data = JSON.parse(text);
      const list = Array.isArray(data) ? data : (data.endpoints || []);
      if (list.length === 0) throw new Error('文件中没有端点数据');
      // 弹出模式选择：覆盖导入（替换全部）或跳过已有（仅新增）
      setImportModeDialog({ list, count: list.length });
    } catch (error) {
      toast.error(error.message || '导入端点失败');
    } finally {
      setEndpointImporting(false);
    }
  };

  const runEndpointImport = async (list, overwrite) => {
    setImportModeDialog(null);
    setEndpointImporting(true);
    try {
      const response = await fetch('/api/openai/import', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpoints: list, overwrite }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || payload.success !== true) throw new Error(payload.error || '导入端点失败');
      await loadEndpoints(true);
      toast.success(overwrite
        ? `覆盖导入完成：${payload.imported ?? 0} 个端点已替换`
        : `导入完成：新增 ${payload.imported ?? 0} 个，跳过 ${payload.skipped ?? 0} 个`);
    } catch (error) {
      toast.error(error.message || '导入端点失败');
    } finally {
      setEndpointImporting(false);
    }
  };


  useEffect(() => {
    if (activeTab === 'keys') {
      loadGatewayKeys();
    }
  }, [activeTab, loadGatewayKeys]);

  const selectedEndpoint = useMemo(
    () => endpoints.find(endpoint => endpoint.id === selectedEndpointId) || endpoints[0] || null,
    [endpoints, selectedEndpointId]
  );

  // 实际启用（未被禁用）的模型总数，跨启用端点去重。
  const enabledModelCount = useMemo(() => {
    const ids = new Set();
    endpoints
      .filter(endpoint => endpoint.enabled)
      .forEach(endpoint => activeModelIdsForEndpoint(endpoint).forEach(id => ids.add(id)));
    return ids.size;
  }, [endpoints]);

  const trendSeries = useMemo(() => {
    const buckets = Array.isArray(analyticsCharts.daily) ? analyticsCharts.daily : [];
    const build = (color, pick, name) => {
      const labels = buckets.map(point => point.day || '');
      const values = buckets.map(point => Number(pick(point)) || 0);
      return { name, color, labels, values };
    };
    return {
      requests: {
        ...build(ChartPalette.categorical(0, isDarkMode), p => p.count, '请求数'),
        formatValue: value => formatCompact(value, 0),
        formatAxis: value => formatCompact(value, 0),
      },
      tokens: {
        ...build(ChartPalette.categorical(1, isDarkMode), p => p.tokens, '词元 (M)'),
        formatValue: formatTokensM,
        formatAxis: value => `${Math.round(Number(value) / 1e6)}M`,
      },
      tokensUncached: {
        ...build(
          ChartPalette.categorical(1, isDarkMode),
          p => Math.max(0, (Number(p.tokens) || 0) - (Number(p.cachedTokens) || 0)),
          '未缓存词元 (M)'
        ),
        formatValue: formatTokensM,
        formatAxis: value => `${Math.round(Number(value) / 1e6)}M`,
      },
      latency: {
        ...build(ChartPalette.categorical(2, isDarkMode), p => p.avgLatency, '平均延迟 (s)'),
        formatValue: value => `${(Number(value) / 1000).toFixed(2)} s`,
        formatAxis: value => `${(Number(value) / 1000).toFixed(0)}`,
      },
      latencyTtfb: {
        ...build(ChartPalette.categorical(2, isDarkMode), p => p.avgTtfbMs, '平均首字延迟 (s)'),
        formatValue: value => `${(Number(value) / 1000).toFixed(2)} s`,
        formatAxis: value => `${(Number(value) / 1000).toFixed(0)}`,
      },
      errorRate: {
        ...build(
          ChartPalette.categorical(3, isDarkMode),
          p => (Number(p.count) > 0 ? ((Number(p.errors) || 0) / Number(p.count)) * 100 : 0),
          '错误率 (%)'
        ),
        formatValue: value => `${Number(value).toFixed(2)}%`,
        formatAxis: value => `${Number(value).toFixed(0)}%`,
      },
      errorCount: {
        ...build(ChartPalette.categorical(3, isDarkMode), p => p.errors, '错误数'),
        formatValue: value => formatCompact(value, 0),
        formatAxis: value => formatCompact(value, 0),
      },
    };
  }, [analyticsCharts, isDarkMode]);

  // 全宽「模型调用趋势」数据：按后端 byModel / buckets 对齐。
  const byModelTrend = useMemo(() => {
    const daily = Array.isArray(analyticsCharts.daily) ? analyticsCharts.daily : [];
    const labels =
      Array.isArray(analyticsCharts.buckets) && analyticsCharts.buckets.length
        ? analyticsCharts.buckets
        : trendSeries.requests.labels;
    const tsValues = daily.map(point => (Number(point.tsSec) || 0) * 1000);
    const models = Array.isArray(analyticsCharts.byModel) ? analyticsCharts.byModel : [];
    const endpoints = Array.isArray(analyticsCharts.byEndpoint) ? analyticsCharts.byEndpoint : [];
    return { labels, tsValues, models, endpoints };
  }, [analyticsCharts, trendSeries]);


  useEffect(() => {
    if (activeTab === 'endpoints' || activeTab === 'keys') {
      loadGatewayKeys();
    }
  }, [activeTab, loadGatewayKeys]);

  useEffect(() => {
    if (endpoints.length === 0) {
      setSelectedEndpointId('');
      return;
    }
    if (!endpoints.some(endpoint => endpoint.id === selectedEndpointId)) {
      setSelectedEndpointId(endpoints[0].id);
    }
  }, [endpoints, selectedEndpointId]);

  // Endpoint Verification & Model Refresh
  const verifyEndpoint = async endpoint => {
    try {
      toast.info(`正在验证 ${endpoint.name || '端点'}...`, { isManual: true });
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.valid) {
        toast.success(`验证成功！找到 ${data.modelsCount || 0} 个模型`);
        await loadEndpoints(true);
      } else {
        toast.error('验证失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      toast.error('验证失败: ' + error.message);
    }
  };

  const refreshEndpointModels = async endpoint => {
    if (endpoint.refreshing) return;
    // Set local refreshing
    setEndpoints(prev => prev.map(e => (e.id === endpoint.id ? { ...e, refreshing: true } : e)));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.valid) {
        toast.success(`${endpoint.name || '端点'} 模型列表已更新`);
        await loadEndpoints(true);
      } else {
        toast.error('刷新失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      setEndpoints(prev => prev.map(e => (e.id === endpoint.id ? { ...e, refreshing: false } : e)));
    }
  };

  const refreshAllEndpoints = async () => {
    setEndpointsRefreshing(true);
    try {
      const response = await fetch('/api/openai/endpoints/refresh', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        const successCount = data.results?.filter(r => r.success).length || 0;
        toast.success(`刷新完成！已更新 ${successCount} 个启用端点`);
        await loadEndpoints(true);
      } else {
        toast.error('刷新失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      setEndpointsRefreshing(false);
    }
  };

  const toggleEndpointEnabled = async endpoint => {
    if (endpointToggleLoading[endpoint.id]) return;
    const updatedEnabled = !endpoint.enabled;
    setEndpointToggleLoading(prev => ({ ...prev, [endpoint.id]: true }));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: updatedEnabled }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '未知错误');

      const confirmedEnabled = Boolean(data.enabled);
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, enabled: confirmedEnabled } : e))
      );
      const endpointName = endpoint.name || '端点';
      toast.success(confirmedEnabled ? `${endpointName} 已启用` : `${endpointName} 已停用`);
      await loadAllModels(true);
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    } finally {
      setEndpointToggleLoading(prev => ({ ...prev, [endpoint.id]: false }));
    }
  };

  // 保存端点路由优先级/权重：PUT /api/openai/endpoints/:id/routing（照搬模型映射模式）。
  const saveEndpointRouting = async (endpointId, field, value) => {
    setRoutingEditKey(null);
    const payload = field === 'priority' ? { priority: value } : { weight: value };
    try {
      const res = await fetch(`/api/openai/endpoints/${endpointId}/routing`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.success) throw new Error(data.error || '保存失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpointId
            ? {
                ...e,
                priority: typeof data.priority === 'number' ? data.priority : e.priority,
                weight: typeof data.weight === 'number' ? data.weight : e.weight,
              }
            : e
        )
      );
    } catch (error) {
      toast.error('路由设置保存失败: ' + error.message);
    }
  };

  // 端点列表拖拽排序：本地先更新顺序，再持久化到后端；失败时回滚。
  const saveEndpointOrder = async nextEndpoints => {
    const orderedIds = nextEndpoints.map(ep => ep.id);
    setEndpointReorderSaving(true);
    try {
      const response = await fetch('/api/openai/endpoints/reorder', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpointIds: orderedIds }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '保存失败');
      toast.success('端点顺序已保存');
    } catch (error) {
      toast.error('排序保存失败: ' + error.message);
      await loadEndpoints(true);
    } finally {
      setEndpointReorderSaving(false);
    }
  };

  const handleEndpointDragStart = (item, event) => {
    // 如果拖拽起点是输入框，忽略拖拽（避免 priority/weight inline 编辑时误触）。
    if (event.target && event.target.tagName === 'INPUT') return;
    setDraggedEndpointId(String(item.id));
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', String(item.id));
  };

  const handleEndpointDragOver = event => {
    if (!draggedEndpointId) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  };

  const handleEndpointDrop = async (targetItem, event) => {
    event.preventDefault();
    const sourceId = draggedEndpointId || event.dataTransfer.getData('text/plain');
    setDraggedEndpointId(null);
    if (!sourceId || String(sourceId) === String(targetItem.id)) return;
    const fromIndex = endpoints.findIndex(ep => String(ep.id) === String(sourceId));
    const toIndex = endpoints.findIndex(ep => String(ep.id) === String(targetItem.id));
    if (fromIndex < 0 || toIndex < 0) return;
    const next = [...endpoints];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    setEndpoints(next);
    await saveEndpointOrder(next);
  };

  const handleEndpointDragEnd = () => {
    setDraggedEndpointId(null);
  };

  // 模型开关的进行中标记：ref 用于同步去重，state 用于驱动按钮禁用态渲染。
  const modelSwitchLoadingRef = useRef({});
  const [modelSwitchLoading, setModelSwitchLoading] = useState({});

  const toggleModelEnabled = async (endpoint, modelId, enabled, silent = false, skipReload = false) => {
    const key = `${endpoint.id}:${modelId}`;
    if (modelSwitchLoadingRef.current[key]) return;
    modelSwitchLoadingRef.current[key] = true;
    setModelSwitchLoading(prev => ({ ...prev, [key]: true }));
    const prevDisabled = Array.isArray(endpoint.disabledModels) ? endpoint.disabledModels : [];
    // 乐观更新：立即切换开关状态，无需等待后端往返。
    setEndpoints(prev =>
      prev.map(e =>
        e.id === endpoint.id
          ? {
              ...e,
              disabledModels: enabled
                ? (Array.isArray(e.disabledModels) ? e.disabledModels : []).filter(id => id !== modelId)
                : [
                    ...(Array.isArray(e.disabledModels) ? e.disabledModels : []).filter(
                      id => id !== modelId
                    ),
                    modelId,
                  ],
            }
          : e
      )
    );
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/models/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ model: modelId, enabled }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpoint.id
            ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
            : e
        )
      );
      if (!silent) toast.success(enabled ? `${modelId} 已启用` : `${modelId} 已停用`);
      if (!skipReload) await loadAllModels(true);
    } catch (error) {
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, disabledModels: prevDisabled } : e))
      );
      toast.error(`更新模型状态失败: ${error.message}`);
    } finally {
      modelSwitchLoadingRef.current[key] = false;
      setModelSwitchLoading(prev => ({ ...prev, [key]: false }));
    }
  };

  const modelEnabledForEndpoint = (endpoint, modelId) => {
    const disabled = Array.isArray(endpoint?.disabledModels) ? endpoint.disabledModels : [];
    return !disabled.includes(modelId);
  };

  const openAddEndpointModal = () => {
    setEditingEndpoint(null);
    setEndpointForm({
      name: '',
      baseUrl: '',
      apiKey: '',
      apiKeys: [],
      notes: '',
      headers: [],
      proxyPool: [],
      proxyBatches: [],
      autoSwitch: false,
      proxyEnabled: false,
      allowDirectFallback: true,
      protocol: 'auto',
    });
    setEndpointFormError('');
    setEndpointFormOpen(true);
    setEndpointKeyChecks([]);
  };

  const openEditEndpointModal = endpoint => {
    setEditingEndpoint(endpoint);
    setEndpointForm({
      name: endpoint.name || '',
      baseUrl: endpoint.baseUrl || '',
      apiKey: endpoint.apiKey || '',
      apiKeys: Array.isArray(endpoint.apiKeys) ? endpoint.apiKeys : [],
      notes: endpoint.notes || '',
      headers: Array.isArray(endpoint.headers) ? endpoint.headers : [],
      proxyPool: Array.isArray(endpoint.proxyPool) ? endpoint.proxyPool : [],
      proxyBatches: Array.isArray(endpoint.proxyBatches) ? endpoint.proxyBatches : [],
      autoSwitch: Boolean(endpoint.autoSwitch),
      proxyEnabled: Boolean(endpoint.proxyEnabled),
      allowDirectFallback: !endpoint.forceProxy,
      protocol: endpoint.protocol || 'auto',
    });
    setEndpointFormError('');
    setEndpointFormOpen(true);
    setEndpointKeyChecks([]);
    checkEndpointKeys(
      [endpoint.apiKey || '', ...(Array.isArray(endpoint.apiKeys) ? endpoint.apiKeys : [])],
      endpoint.id
    );
  };

  const updateEndpointProxy = (index, value) => {
    setEndpointForm(current => {
      const proxyPool = (current.proxyPool || []).map((proxy, i) => (i === index ? value : proxy));
      return { ...current, proxyPool };
    });
  };

  const addEndpointProxy = () => {
    setEndpointForm(current => ({
      ...current,
      proxyPool: [...(current.proxyPool || []), ''],
    }));
  };

  const removeEndpointProxy = index => {
    setEndpointForm(current => ({
      ...current,
      proxyPool: (current.proxyPool || []).filter((_, i) => i !== index),
    }));
  };

  const [proxyBatchOpen, setProxyBatchOpen] = useState(false);
  const [proxyBatchText, setProxyBatchText] = useState('');
  const [proxyImportLoading, setProxyImportLoading] = useState(false);
  const [subscriptionUrlOpen, setSubscriptionUrlOpen] = useState(false);
  const [subscriptionUrl, setSubscriptionUrl] = useState('');
  // editingProxyIndex 标记当前正在编辑完整 URL 的代理条目索引；-1 表示无。
  const [editingProxyIndex, setEditingProxyIndex] = useState(-1);
  // proxyManagerOpen 控制「出口代理池」独立管理弹窗。
  const [proxyManagerOpen, setProxyManagerOpen] = useState(false);

  // manualProxyEntries：池中不属于任何导入批次的代理（手动添加），
  // 携带真实池下标，供管理弹窗内编辑与删除。
  const manualProxyEntries = useMemo(() => {
    const batchUrls = new Set((endpointForm.proxyBatches || []).flatMap(batch => batch.proxies || []));
    return (endpointForm.proxyPool || [])
      .map((proxy, index) => ({ proxy, index }))
      .filter(({ proxy }) => !batchUrls.has(proxy));
  }, [endpointForm.proxyPool, endpointForm.proxyBatches]);

  const saveProxyBatch = () => {
    const lines = proxyBatchText
      .split(/\r?\n/)
      .map(line => line.trim())
      .filter(Boolean);
    if (lines.length === 0) {
      toast.warning('请粘贴至少一个代理地址');
      return;
    }
    const added = addProxyBatch(`批量添加 ${new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`, lines);
    if (added === 0) {
      toast.info('粘贴的代理已全部属于其他批次，无需重复添加', { isManual: true });
      return;
    }
    setProxyBatchText('');
    setProxyBatchOpen(false);
    toast.success(`已批量添加 ${added} 个代理`);
  };

  // addProxyBatch 把一批代理（文件/订阅/批量粘贴）登记为一个「批次」：
  //   a) 池中尚不存在 → 新增并入池；
  //   b) 已在池中但无任何批次归属（历史导入/手动加入的同 URL）→ 一并记入本批次；
  //   c) 已属于其他批次 → 跳过，避免两个批次拥有同一 URL 导致删除互相牵连。
  // 这样旧数据（批次功能上线前导入的池）重新导入同一来源即可获得批次管理能力。
  // 返回本次归入批次的条数（0 表示无新增）。
  const addProxyBatch = (batchName, urls) => {
    const list = (Array.isArray(urls) ? urls : []).filter(Boolean);
    if (list.length === 0) return 0;
    const pool = endpointForm.proxyPool || [];
    const poolSet = new Set(pool);
    const owned = new Set((endpointForm.proxyBatches || []).flatMap(batch => batch.proxies || []));
    const fresh = list.filter(proxy => !poolSet.has(proxy));
    const newlyOwned = list.filter(proxy => poolSet.has(proxy) && !owned.has(proxy));
    const batchProxies = [...fresh, ...newlyOwned];
    if (batchProxies.length === 0) return 0;
    const batchId = `pb_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
    setEndpointForm(current => ({
      ...current,
      proxyPool: [...pool, ...fresh],
      proxyBatches: [
        ...(current.proxyBatches || []),
        {
          id: batchId,
          name: batchName,
          createdAt: new Date().toISOString(),
          proxies: batchProxies,
        },
      ],
    }));
    return batchProxies.length;
  };

  // 文件导入：读取本地代理列表文件（.txt，每行一个代理），交给后端解析清洗后，
  // 以文件为单位建立一个「批次」追加到池，便于之后按文件批量删除/管理。
  const proxyFileInputRef = useRef(null);
  const importProxyFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async e => {
      const text = String(e.target?.result || '');
      if (!text.trim()) {
        toast.warning('文件内容为空');
        return;
      }
      const rawLineCount = text.split(/\r?\n/).map(l => l.trim()).filter(Boolean).length;
      if (proxyImportLoading) return;
      setProxyImportLoading(true);
      try {
        const response = await fetch('/api/openai/proxies/import-list', {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ text }),
        });
        const data = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
        const list = Array.isArray(data.proxies) ? data.proxies : [];
        if (list.length === 0) {
          toast.info('文件中没有找到可导入的代理（支持 http(s)://、socks5://、host:port）', { isManual: true });
          return;
        }
        const batchName = file.name || '代理列表';
        const added = addProxyBatch(batchName, list);
        if (added === 0) {
          toast.info(`文件中的 ${list.length} 个代理已全部属于其他批次，无需重复导入`, { isManual: true });
          return;
        }
        const skipped = rawLineCount - list.length;
        toast.success(
          `已导入批次「${batchName}」${added} 条${skipped > 0 ? `（跳过 ${skipped} 行无效/重复）` : ''}`,
        );
      } catch (err) {
        toast.error(err.message || '文件导入失败');
      } finally {
        setProxyImportLoading(false);
        if (proxyFileInputRef.current) proxyFileInputRef.current.value = '';
      }
    };
    reader.readAsText(file);
  };

  // 批次管理：展开预览 / 删除整批 / 移出单条。
  const [expandedBatchId, setExpandedBatchId] = useState(null);
  const [manualProxyExpanded, setManualProxyExpanded] = useState(false);
  // proxyRuntimeStates：代理池各出口的运行时禁用状态（冷却 / 429 冻结），
  // 用于在管理弹窗里把被禁用的代理标红。key 为代理 URL。
  const [proxyRuntimeStates, setProxyRuntimeStates] = useState({});
  useEffect(() => {
    if (!proxyManagerOpen || !editingEndpoint?.id) return;
    let cancelled = false;
    const load = async () => {
      try {
        const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
          headers: getAuthHeaders(),
        });
        const data = await response.json().catch(() => ({}));
        if (!response.ok || !Array.isArray(data.proxies)) return;
        if (!cancelled) {
          const map = {};
          data.proxies.forEach(item => {
            map[item.proxy] = item;
          });
          setProxyRuntimeStates(map);
        }
      } catch {
        // 状态加载失败不阻断弹窗使用。
      }
    };
    load();
    const timer = setInterval(load, 15000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [proxyManagerOpen, editingEndpoint?.id]);
  // disabledProxyUntil 返回某代理的禁用信息：{label, disableUntil} 或 null（可用）。
  const disabledProxyUntil = proxy => {
    const item = proxyRuntimeStates[proxy];
    if (!item) return null;
    if (item.rateLimitedUntil && new Date(item.rateLimitedUntil).getTime() > Date.now()) {
      return { label: `429 冻结至 ${formatDateTime(item.rateLimitedUntil)}`, until: item.rateLimitedUntil };
    }
    if (item.cooldownUntil && new Date(item.cooldownUntil).getTime() > Date.now()) {
      return { label: `连接失败冷却至 ${formatDateTime(item.cooldownUntil)}`, until: item.cooldownUntil };
    }
    if (item.sunkUntil && new Date(item.sunkUntil).getTime() > Date.now()) {
      return { label: `坏代理沉淀至 ${formatDateTime(item.sunkUntil)}`, until: item.sunkUntil };
    }
    return null;
  };
  // disabledProxyCount 返回当前被禁用（冷却/冻结/沉淀）的代理条数。
  const disabledProxyCount = useMemo(() => {
    if (!editingEndpoint?.id) return 0;
    return (endpointForm.proxyPool || []).filter(proxy => disabledProxyUntil(proxy)).length;
  }, [endpointForm.proxyPool, proxyRuntimeStates]);
  // unbanAllProxies 一键解封端点代理池全部出口：清除冷却/429 冻结/坏代理沉淀，
  // 解封后重新拉取运行时状态使 UI 同步。
  const [unbanningProxies, setUnbanningProxies] = useState(false);
  const unbanAllProxies = async () => {
    if (!editingEndpoint?.id || unbanningProxies) return;
    setUnbanningProxies(true);
    try {
      const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state/unban`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || `HTTP ${response.status}`);
      toast.success(data.cleared ? `已解封 ${data.cleared} 条代理` : '代理池无被禁用的出口');
      const stateRes = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
        headers: getAuthHeaders(),
      });
      const stateData = await stateRes.json().catch(() => ({}));
      if (stateRes.ok && Array.isArray(stateData.proxies)) {
        const map = {};
        stateData.proxies.forEach(item => {
          map[item.proxy] = item;
        });
        setProxyRuntimeStates(map);
      }
    } catch (error) {
      toast.error('解封失败: ' + error.message);
    } finally {
      setUnbanningProxies(false);
    }
  };
  // probeAllProxies 对端点代理池全部出口发起一次手动探活，完成后刷新运行时状态。
  const [probingProxies, setProbingProxies] = useState(false);
  const probeAllProxies = async () => {
    if (!editingEndpoint?.id || probingProxies) return;
    setProbingProxies(true);
    try {
      const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state/probe`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || `HTTP ${response.status}`);
      toast.success(data.probed ? `已探测 ${data.probed} 条代理，可达 ${data.reachable} 条` : '代理池为空');
      const stateRes = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
        headers: getAuthHeaders(),
      });
      const stateData = await stateRes.json().catch(() => ({}));
      if (stateRes.ok && Array.isArray(stateData.proxies)) {
        const map = {};
        stateData.proxies.forEach(item => {
          map[item.proxy] = item;
        });
        setProxyRuntimeStates(map);
      }
    } catch (error) {
      toast.error('批量测试失败: ' + error.message);
    } finally {
      setProbingProxies(false);
    }
  };
  const removeProxyBatch = batch => {
    if (!confirmPress(`proxy-batch:${batch.id}`, `移除文件批次「${batch.name}」及其全部 ${batch.proxies.length} 条代理？`)) return;
    const members = new Set(batch.proxies || []);
    setEndpointForm(current => ({
      ...current,
      proxyPool: (current.proxyPool || []).filter(proxy => !members.has(proxy)),
      proxyBatches: (current.proxyBatches || []).filter(item => item.id !== batch.id),
    }));
    toast.success(`已移除批次「${batch.name}」的 ${batch.proxies.length} 条代理`);
  };
  const removeProxyFromBatch = (batch, proxy) => {
    setEndpointForm(current => {
      const batches = (current.proxyBatches || [])
        .map(item =>
          item.id === batch.id
            ? { ...item, proxies: (item.proxies || []).filter(p => p !== proxy) }
            : item,
        )
        .filter(item => item.id !== batch.id || (item.proxies || []).length > 0);
      return {
        ...current,
        proxyPool: (current.proxyPool || []).filter(item => item !== proxy),
        proxyBatches: batches,
      };
    });
  };

  // resolveSubscriptionProxies 通过后端拉取并解析订阅链接中的 socks/http 节点。
  const resolveSubscriptionProxies = async () => {
    const url = subscriptionUrl.trim();
    if (!url) {
      toast.warning('请填写订阅链接');
      return;
    }
    if (proxyImportLoading) return;
    setProxyImportLoading(true);
    try {
      const response = await fetch('/api/openai/proxies/resolve-subscription', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
      const list = Array.isArray(data.proxies) ? data.proxies : [];
      if (list.length === 0) {
        toast.info(data.message || '订阅内容中没有找到 socks/http 节点', { isManual: true });
        return;
      }
      let batchName = url;
      try {
        batchName = `订阅 ${new URL(url).hostname}`;
      } catch {
        // URL 解析失败时退化为完整链接。
      }
      const added = addProxyBatch(batchName, list.map(item => item.proxy).filter(Boolean));
      if (added === 0) {
        toast.info('订阅链接中的代理已全部属于其他批次，无需重复导入', { isManual: true });
      } else {
        toast.success(`已从订阅链接导入 ${added} 个代理`);
      }
      setSubscriptionUrl('');
      setSubscriptionUrlOpen(false);
    } catch (error) {
      toast.error('解析订阅链接失败: ' + error.message);
    } finally {
      setProxyImportLoading(false);
    }
  };

  const updateEndpointHeader = (index, field, value) => {
    setEndpointForm(current => {
      const headers = (current.headers || []).map((header, i) =>
        i === index ? { ...header, [field]: value } : header
      );
      return { ...current, headers };
    });
  };

  const addEndpointHeader = () => {
    setEndpointForm(current => ({
      ...current,
      headers: [...(current.headers || []), { name: '', value: '' }],
    }));
  };

  const removeEndpointHeader = index => {
    setEndpointForm(current => ({
      ...current,
      headers: (current.headers || []).filter((_, i) => i !== index),
    }));
  };

  const saveEndpoint = async () => {
    if (!endpointForm.baseUrl || !endpointForm.apiKey) {
      setEndpointFormError('请填写 API 地址和 API Key');
      return;
    }
    setEndpointSaving(true);
    setEndpointFormError('');
    try {
      const url = editingEndpoint
        ? `/api/openai/endpoints/${editingEndpoint.id}`
        : '/api/openai/endpoints';
      const apiKeys = (endpointForm.apiKeys || []).map(k => (k || '').trim()).filter(Boolean);
      const response = await fetch(url, {
        method: editingEndpoint ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ ...endpointForm, apiKeys, forceProxy: !endpointForm.allowDirectFallback }),
      });
      const data = await response.json();
      if (response.ok && (data.success || data.endpoint || data.id)) {
        toast.success(editingEndpoint ? '端点已更新' : '端点已添加');
        setEndpointFormOpen(false);
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        setEndpointFormError(data.error || '保存失败');
      }
    } catch (error) {
      setEndpointFormError('保存失败: ' + error.message);
    } finally {
      setEndpointSaving(false);
    }
  };

  // 对端点多 key 逐个做有效性检测（调后端 GET /models 判定）。keysArray 与弹窗行对齐：
  // 第 0 项=主 key（K1），后续项=备用 key（K2...），空值行跳过但仍占据对应下标。
  const checkEndpointKeys = useCallback(async (keysArray, endpointId) => {
    const rows = keysArray.map(k => (k || '').trim());
    const entries = rows
      .map((key, rowIndex) => ({ rowIndex, key }))
      .filter(e => e.key !== '');
    if (!endpointId) {
      return;
    }
    if (entries.length === 0) {
      setEndpointKeyChecks(Array(rows.length).fill(null));
      return;
    }
    setEndpointKeyChecking(true);
    setEndpointKeyChecks(rows.map(() => ({ status: 'checking' })));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpointId}/key-check`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          keys: entries.map(e => e.key),
          timeout: 10000,
        }),
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || '检测失败');
      }
      const next = Array(rows.length).fill(null);
      (data.results || []).forEach((result, idx) => {
        const rowIndex = entries[idx]?.rowIndex;
        if (rowIndex != null) next[rowIndex] = result;
      });
      setEndpointKeyChecks(next);
    } catch (error) {
      setEndpointKeyChecks(rows.map(() => ({ status: 'error', message: error.message })));
      toast.error(`Key 检测失败：${error.message}`);
    } finally {
      setEndpointKeyChecking(false);
    }
  }, [getAuthHeaders]);

  const appendEndpointKey = () => {
    setEndpointForm(current => ({
      ...current,
      apiKeys: [...(current.apiKeys || []), ''],
    }));
    setEndpointKeyChecks(prev => [...prev, null]);
  };

  const removeEndpointKey = rowIndex => {
    setEndpointForm(current => {
      const keys = [current.apiKey || '', ...(current.apiKeys || [])];
      keys.splice(rowIndex, 1);
      return {
        ...current,
        apiKey: keys[0] || '',
        apiKeys: keys.slice(1),
      };
    });
    setEndpointKeyChecks(prev => {
      const next = [...prev];
      next.splice(rowIndex, 1);
      return next;
    });
  };

  const [pendingDeleteEndpointId, setPendingDeleteEndpointId] = useState(null);
  const DELETE_ENDPOINT_CONFIRM_MS = 3000;
  const deleteEndpointConfirmActive = id =>
    pendingDeleteEndpointId?.id === id && pendingDeleteEndpointId.expiresAt > Date.now();

  const deleteEndpoint = async endpoint => {
    if (!deleteEndpointConfirmActive(endpoint.id)) {
      setPendingDeleteEndpointId({ id: endpoint.id, expiresAt: Date.now() + DELETE_ENDPOINT_CONFIRM_MS });
      toast.info(`删除端点 ${endpoint.name || endpoint.baseUrl}？请再次点击确认`, { isManual: true });
      return;
    }
    setPendingDeleteEndpointId(null);
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success('端点已删除');
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        toast.error('删除失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  };

  // ==================== 2. Health Checking ====================
  const [openaiModelHealth, setOpenaiModelHealth] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_model_health_cache');
      return saved ? JSON.parse(saved) : {};
    } catch {
      return {};
    }
  });

  useEffect(() => {
    localStorage.setItem('openai_model_health_cache', JSON.stringify(openaiModelHealth));
  }, [openaiModelHealth]);

  const [modelHealthBatchLoading, setModelHealthBatchLoading] = useState(false);
  const [healthCheckProgress, setHealthCheckProgress] = useState(() => createHealthCheckProgress());
  const [healthCheckModal, setHealthCheckModal] = useState(false);
  const [healthCheckForm, setHealthCheckForm] = useState({
    timeout: DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
    concurrency: DEFAULT_MODEL_HEALTH_CONCURRENCY,
  });
  const modelHealthAbortControllersRef = useRef(new Map());
  // 批量检测进行中请求：切换端点时 abort，避免旧端点的检测状态带偏新端点。
  const batchHealthAbortRef = useRef(null);

  // 切换选中端点时立即终止该端点所有检测（单个 + 批量），并清理「检测中」状态。
  useEffect(() => {
    modelHealthAbortControllersRef.current.forEach(controller => controller.abort());
    modelHealthAbortControllersRef.current.clear();
    batchHealthAbortRef.current?.abort();
    batchHealthAbortRef.current = null;
    setModelHealthBatchLoading(false);
    setOpenaiModelHealth(prev => {
      const next = {};
      for (const [key, record] of Object.entries(prev)) {
        next[key] = record?.loading ? { ...record, loading: false } : record;
      }
      return next;
    });
  }, [selectedEndpointId]);

  const markModelsChecking = targets => {
    const checkedAt = Date.now();
    setOpenaiModelHealth(prev => {
      const next = { ...prev };
      targets.forEach(({ endpointId, modelId }) => {
        next[modelHealthKey(endpointId, modelId)] = {
          status: 'checking',
          loading: true,
          latency: null,
          checkedAt,
        };
      });
      return next;
    });
  };


  const applyEndpointHealthResults = (endpointId, modelIds, records, fallbackError) => {
    const recordsByModel = new Map(
      (Array.isArray(records) ? records : []).map(record => [
        String(record?.model || '').trim(),
        record,
      ])
    );
    const results = modelIds.map(modelId =>
      normalizeModelHealthRecord(recordsByModel.get(modelId), fallbackError)
    );

    setOpenaiModelHealth(prev => {
      const next = { ...prev };
      modelIds.forEach((modelId, index) => {
        next[modelHealthKey(endpointId, modelId)] = results[index];
      });
      return next;
    });

    return results;
  };

  const testModelHealth = async (model, targetEndpointId, silentToast = false) => {
    const modelId = String(model?.id || '').trim();
    if (!modelId || !targetEndpointId) return null;
    const healthKey = modelHealthKey(targetEndpointId, modelId);
    const activeController = modelHealthAbortControllersRef.current.get(healthKey);
    if (activeController) {
      activeController.abort();
      modelHealthAbortControllersRef.current.delete(healthKey);
      setOpenaiModelHealth(prev => ({
        ...prev,
        [healthKey]: {
          status: 'cancelled',
          loading: false,
          latency: null,
          checkedAt: Date.now(),
          error: '检测已停止',
        },
      }));
      if (!silentToast) toast.warning(`${modelId} 检测已停止`);
      return null;
    }

    const controller = new AbortController();
    modelHealthAbortControllersRef.current.set(healthKey, controller);

    markModelsChecking([{ endpointId: targetEndpointId, modelId }]);

    try {
      const response = await fetch(
        `/api/openai/endpoints/${encodeURIComponent(targetEndpointId)}/health-check`,
        {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          signal: controller.signal,
          body: JSON.stringify({
            model: modelId,
            timeout: Math.max(1, Number(healthCheckForm.timeout) || DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS) * 1000,
          }),
        }
      );
      const data = await response.json().catch(() => ({}));
      if (!response.ok) {
        throw new Error(data.error || `HTTP ${response.status}`);
      }

      const result = applyEndpointHealthResults(targetEndpointId, [modelId], [data])[0];
      if (!silentToast) {
        if (result.status === 'healthy') {
          toast.success(`${modelId} 可用，延迟 ${result.latency ?? '-'} ms`);
        } else if (result.status === 'degraded') {
          toast.warning(`${modelId} 响应较慢，延迟 ${result.latency ?? '-'} ms`);
        } else {
          toast.error(`${modelId} 检测失败: ${result.error || '未知错误'}`);
        }
      }
      return result;
    } catch (e) {
      if (controller.signal.aborted) return null;
      const result = applyEndpointHealthResults(targetEndpointId, [modelId], [], e.message)[0];
      if (!silentToast) toast.error(`${modelId} 检测失败: ${result.error || e.message}`);
      return result;
    } finally {
      if (modelHealthAbortControllersRef.current.get(healthKey) === controller) {
        modelHealthAbortControllersRef.current.delete(healthKey);
      }
    }
  };

  // 批量检测：前端并发逐模型发请求，每个完成立即回填状态（无需等全部完成）。
  const runBatchHealthCheckRequest = async (targets, fallbackMessage) => {
    if (!Array.isArray(targets) || targets.length === 0) return [];
    const concurrency = resolveModelHealthConcurrency(healthCheckForm.concurrency, targets.length);
    markModelsChecking(targets);
    const results = new Array(targets.length);
    let cursor = 0;
    const workers = Array.from({ length: concurrency }, async () => {
      while (true) {
        const index = cursor;
        cursor += 1;
        if (index >= targets.length) return;
        const target = targets[index];
        // silentToast=true：每个模型的 toast 由批量结果统一汇总，避免刷屏。
        const result = await testModelHealth(
          { id: target.modelId },
          target.endpointId,
          true
        );
        results[index] =
          result ||
          normalizeModelHealthRecord(
            { status: 'failed', error: '检测未返回结果', checkedAt: Date.now() },
            '检测未返回结果'
          );
      }
    });
    await Promise.all(workers);
    return results;
  };

  const startBatchHealthCheck = async () => {
    const endpointTargets = endpoints.filter(
      endpoint => endpoint.enabled && endpointModelIds(endpoint).length > 0
    );
    const allTargets = modelHealthTargets(endpointTargets);
    if (allTargets.length === 0) {
      toast.warning('没有找到任何启用的端点或模型');
      return;
    }

    setHealthCheckModal(false);
    setModelHealthBatchLoading(true);
    setHealthCheckProgress(createHealthCheckProgress(allTargets.length, true));
    const concurrency = resolveModelHealthConcurrency(
      healthCheckForm.concurrency,
      allTargets.length
    );
    toast.info(`正在按 ${concurrency} 并发批量检测 ${allTargets.length} 个模型...`, { isManual: true });

    try {
      const results = await runBatchHealthCheckRequest(allTargets, '批量检测失败');
      const counts = countModelHealthResults(results);
      setHealthCheckProgress({
        running: false,
        total: allTargets.length,
        completed: results.length,
        ...counts,
      });

      const message = `检测完成：可用 ${counts.healthy}，较慢 ${counts.degraded}，失败 ${counts.failed}`;
      if (counts.failed > 0) toast.warning(message);
      else toast.success(message);
    } catch {
      // 错误已在单模型检测内提示，此处仅终止流程。
    } finally {
      setModelHealthBatchLoading(false);
    }
  };

  const openHealthCheckForEndpoint = async endpointId => {
    const ep = endpoints.find(e => e.id === endpointId);
    const modelIds = endpointModelIds(ep);
    if (!ep || modelIds.length === 0) {
      toast.warning('该端点无可用模型');
      return;
    }

    setModelHealthBatchLoading(true);
    setHealthCheckProgress(createHealthCheckProgress(modelIds.length, true));
    const concurrency = resolveModelHealthConcurrency(healthCheckForm.concurrency, modelIds.length);
    toast.info(
      `正在按 ${concurrency} 并发批量检测 ${ep.name || '端点'} 的 ${modelIds.length} 个模型...`,
      { isManual: true }
    );

    try {
      const targets = modelIds.map(modelId => ({ endpointId, modelId }));
      const results = await runBatchHealthCheckRequest(targets, '端点检测失败');
      const counts = countModelHealthResults(results);
      setHealthCheckProgress({
        running: false,
        total: modelIds.length,
        completed: results.length,
        ...counts,
      });
      const message = `${ep.name || '端点'}：可用 ${counts.healthy}，较慢 ${counts.degraded}，失败 ${counts.failed}`;
      if (counts.failed > 0) toast.warning(message);
      else toast.success(message);
    } catch {
      // 错误已在单模型检测内提示，此处仅终止流程。
    } finally {
      setModelHealthBatchLoading(false);
    }
  };

  // ==================== 3. Models List ====================
  const [allModels, setAllModels] = useState([]);

  const loadAllModels = useCallback(
    async (silent = false) => {
      try {
        const response = await fetch('/api/openai/v1/models', {
          headers: getAuthHeaders(),
        });
        const data = await response.json();
        if (data && Array.isArray(data.data)) {
          const sorted = data.data.sort((a, b) => {
            if (a.owned_by !== b.owned_by) return a.owned_by.localeCompare(b.owned_by);
            return a.id.localeCompare(b.id);
          });
          setAllModels(sorted);
        }
      } catch (error) {
        console.error('Failed to load models list:', error);
      }
    },
    [getAuthHeaders]
  );

  useEffect(() => {
    loadAllModels(true);
  }, [loadAllModels]);

  const failedModelIdsForEndpoint = endpoint => {
    if (!endpoint) return [];
    return endpointModelIds(endpoint).filter(
      modelId => openaiModelHealth[modelHealthKey(endpoint.id, modelId)]?.status === 'error'
    );
  };

  const [modelBatchActionLoading, setModelBatchActionLoading] = useState(false);

  // 模型映射（对外名称）行内编辑状态。
  const [mappingEditKey, setMappingEditKey] = useState(null);
  const [mappingDraft, setMappingDraft] = useState('');

  // 端点路由优先级/权重行内编辑状态（照搬模型映射的编辑模式）。
  const [routingEditKey, setRoutingEditKey] = useState(null); // `${endpointId}:priority|weight`
  const [routingDraft, setRoutingDraft] = useState('');

  // 批量切换端点模型的启用状态（原子接口，避免并发逐个 toggle 丢失）。
  const batchToggleEndpointModels = async (endpoint, modelIds, enabled, successMessage) => {
    if (modelBatchActionLoading) return;
    const ids = Array.from(new Set((modelIds || []).filter(Boolean)));
    if (ids.length === 0) return;
    setModelBatchActionLoading(true);
    try {
      const response = await fetch(
        `/api/openai/endpoints/${endpoint.id}/models/toggle-batch`,
        {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ models: ids, enabled }),
        }
      );
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpoint.id
            ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
            : e
        )
      );
      await loadAllModels(true);
      toast.success(successMessage || `已${enabled ? '启用' : '关闭'} ${ids.length} 个模型`);
      return true;
    } catch (error) {
      toast.error(`批量更新失败: ${error.message}`);
      return false;
    } finally {
      setModelBatchActionLoading(false);
    }
  };

  // 关闭端点上所有「非有效」模型（未检测/检测失败/较慢之外的），仅停用不隐藏。
  // 检测为有效的模型（healthy/degraded）不在此列，由每行手动开关控制。
  const batchCloseNonHealthyModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const targets = endpointModelIds(endpoint).filter(modelId => {
      if (!modelEnabledForEndpoint(endpoint, modelId)) return false;
      const health = openaiModelHealth[modelHealthKey(endpoint.id, modelId)];
      return health?.status !== 'healthy' && health?.status !== 'degraded';
    });
    if (targets.length === 0) {
      toast.info('当前没有可批量关闭的模型（非有效模型均为空）', { isManual: true });
      return;
    }
    await batchToggleEndpointModels(endpoint, targets, false, `已关闭 ${targets.length} 个非有效模型`);
  };

  // 兼容旧调用：全局一键关闭失败的模型（保留，供顶栏使用）。
  const batchCloseFailedModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const failed = failedModelIdsForEndpoint(endpoint).filter(modelId =>
      modelEnabledForEndpoint(endpoint, modelId)
    );
    if (failed.length === 0) {
      toast.info('当前端点没有检测失败的模型', { isManual: true });
      return;
    }
    await batchToggleEndpointModels(endpoint, failed, false, `已关闭 ${failed.length} 个检测失败的模型`);
  };

  // 全局一键：跨全部启用端点，关闭所有检测失败的模型（仅停用，不隐藏）。
  const batchCloseAllFailedModels = async () => {
    if (modelBatchActionLoading) return;
    const byEndpoint = {};
    endpoints.forEach(endpoint => {
      if (!endpoint.enabled) return;
      failedModelIdsForEndpoint(endpoint).forEach(modelId => {
        if (modelEnabledForEndpoint(endpoint, modelId)) {
          byEndpoint[endpoint.id] = byEndpoint[endpoint.id] || { endpoint, models: [] };
          byEndpoint[endpoint.id].models.push(modelId);
        }
      });
    });
    const entries = Object.values(byEndpoint);
    const total = entries.reduce((sum, entry) => sum + entry.models.length, 0);
    if (total === 0) {
      toast.info('当前没有检测失败的模型', { isManual: true });
      return;
    }
    if (!(await dialog.confirm(`确认关闭全部 ${total} 个检测失败的模型吗？（仅停用对应模型）`))) {
      return;
    }
    setModelBatchActionLoading(true);
    try {
      for (const entry of entries) {
        const response = await fetch(
          `/api/openai/endpoints/${entry.endpoint.id}/models/toggle-batch`,
          {
            method: 'POST',
            headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
            body: JSON.stringify({ models: entry.models, enabled: false }),
          }
        );
        const data = await response.json().catch(() => ({}));
        if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
        setEndpoints(prev =>
          prev.map(e =>
            e.id === entry.endpoint.id
              ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
              : e
          )
        );
      }
      await loadAllModels(true);
      toast.success(`已关闭 ${total} 个检测失败的模型`);
    } catch (error) {
      toast.error(`批量更新失败: ${error.message}`);
    } finally {
      setModelBatchActionLoading(false);
    }
  };

  // 保存模型映射：PUT /api/openai/endpoints/:id/model-mappings。
  const saveEndpointMapping = async (endpoint, modelId, alias) => {
    setMappingEditKey(null);
    const clean = (alias || '').trim();
    try {
      const res = await fetch(`/api/openai/endpoints/${endpoint.id}/model-mappings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          mappings: { ...(endpoint.modelMappings || {}), [modelId]: clean },
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.success) throw new Error(data.error || '保存失败');
      toast.success(clean ? `已映射 ${modelId} → ${clean}` : `已清除 ${modelId} 的映射`);
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, modelMappings: data.modelMappings } : e))
      );
      await loadAllModels(true);
    } catch (error) {
      toast.error('保存映射失败: ' + error.message);
    }
  };

  // 批量启用被停用的模型（与「关闭检测失败的模型」拆分为两个明确动作）。
  const batchEnableDisabledModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const disabled = Array.isArray(endpoint.disabledModels) ? endpoint.disabledModels : [];
    if (disabled.length === 0) return;
    await batchToggleEndpointModels(endpoint, disabled, true, `已启用 ${disabled.length} 个被停用的模型`);
  };

  return (
    <PageStack viewport>
      {/* Tab Navigation */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            {
              value: 'analytics',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Activity className="w-3.5 h-3.5" />
                  数据看板
                </span>
              ),
            },
            {
              value: 'endpoints',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Server className="w-3.5 h-3.5" />
                  API 端点
                </span>
              ),
            },
            {
              value: 'keys',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Key className="w-3.5 h-3.5" />
                  API 密钥
                </span>
              ),
            },
            {
              value: 'logs',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <History className="w-3.5 h-3.5" />
                  网关日志
                </span>
              ),
            },
          ]}
        />
        <div className="flex min-w-0 items-center justify-end gap-2">
            {activeTab === 'analytics' && (
              <TabBarOverflowActions
                items={[
                  {
                    key: 'granularity',
                    type: 'select',
                    label: '时间粒度',
                    icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                    value: analyticsGranularity,
                    onValueChange: val => setAnalyticsGranularity(val || 'day'),
                    options: [
                      { value: 'hour', label: '按小时' },
                      { value: 'day', label: '按天' },
                      { value: 'week', label: '按周' },
                    ],
                    selectClassName: 'w-28',
                  },
                  {
                    key: 'range',
                    type: 'select',
                    label: '分析范围',
                    icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                    value: String(analyticsDays),
                    onValueChange: val => {
                      setAnalyticsDays(Number(val));
                      setAnalyticsPage(1);
                    },
                    options: [
                      { value: '1', label: '最近 24 小时' },
                      { value: '7', label: '最近 7 天' },
                      { value: '30', label: '最近 30 天' },
                    ],
                    selectClassName: 'w-36',
                  },
                  {
                    key: 'refresh',
                    label: '刷新',
                    icon: <RefreshCw className="w-3.5 h-3.5" />,
                    onClick: fetchAnalytics,
                    disabled: analyticsLoading,
                    loading: analyticsLoading,
                  },
                ]}
              />
            )}
            {activeTab === 'keys' && (
              <TabBarOverflowActions
                items={[
                  {
                    key: 'refresh',
                    label: '刷新',
                    icon: <RefreshCw className={iconButtonIconClass} />,
                    onClick: loadGatewayKeys,
                    disabled: gatewayKeysLoading,
                    loading: gatewayKeysLoading,
                  },
                  {
                    key: 'add',
                    label: '新建密钥',
                    icon: <Plus className={iconButtonIconClass} />,
                    onClick: openAddGatewayKeyModal,
                    variant: 'primary',
                  },
                ]}
              />
            )}
            {activeTab === 'logs' && (
              <div className="flex shrink-0 items-center gap-2">
                <TabBarOverflowActions
                  items={[
                    {
                      key: 'range',
                      type: 'select',
                      label: '时间范围',
                      icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                      value: String(analyticsDays),
                      onValueChange: val => {
                        setAnalyticsDays(Number(val));
                        setAnalyticsPage(1);
                      },
                      options: [
                        { value: '1', label: '最近 24 小时' },
                        { value: '7', label: '最近 7 天' },
                        { value: '30', label: '最近 30 天' },
                      ],
                      selectClassName: 'w-36',
                    },
                    {
                      key: 'clear',
                      label: '清除数据',
                      icon: <Trash className="h-3.5 w-3.5" />,
                      onClick: clearGatewayLogs,
                      danger: true,
                    },
                  ]}
                />
                <RefreshButton
                  size="sm"
                  variant="secondary"
                  loading={analyticsLoading}
                  disabled={analyticsLoading}
                  aria-label="刷新网关日志"
                  title="刷新网关日志"
                  onClick={() => fetchAnalytics()}
                />
              </div>
            )}
            {activeTab === 'endpoints' && (
              <div className="flex shrink-0 items-center gap-2">
                <Toolbar size="sm" aria-label="端点导入导出" className="shrink-0">
                  <Toolbar.Button
                    onClick={exportEndpoints}
                    disabled={endpoints.length === 0 || endpointExporting}
                    icon={<Upload className="h-3.5 w-3.5" />}
                  >
                    <span className="hidden cq-sm:inline">导出</span>
                  </Toolbar.Button>
                  <Toolbar.Button
                    onClick={() => endpointImportInputRef.current?.click()}
                    disabled={endpointImporting}
                    icon={<Download className="h-3.5 w-3.5" />}
                  >
                    <span className="hidden cq-sm:inline">导入</span>
                  </Toolbar.Button>
                </Toolbar>
                <TabBarOverflowActions
                  items={[
                    {
                      key: 'health',
                      label: '健康检测',
                      icon: <Activity className={iconButtonIconClass} />,
                      onClick: () => setHealthCheckModal(true),
                      disabled: modelHealthBatchLoading,
                      loading: modelHealthBatchLoading,
                    },
                    {
                      key: 'refresh',
                      label: '刷新列表',
                      icon: <RefreshCw className={iconButtonIconClass} />,
                      onClick: refreshAllEndpoints,
                      disabled: endpointsRefreshing,
                      loading: endpointsRefreshing,
                    },
                    {
                      key: 'add',
                      label: '新增端点',
                      icon: <Plus className={iconButtonIconClass} />,
                      onClick: openAddEndpointModal,
                      variant: 'primary',
                    },
                  ]}
                />
              </div>
            )}
          </div>
      </div>

      {/* ==================== 1. API 端点 Tab ==================== */}
      {activeTab === 'endpoints' && (
        <div className="flex min-h-0 flex-1 flex-col gap-2.5">
          <Input
            ref={endpointImportInputRef}
            type="file"
            accept="application/json,.json"
            aria-label="导入端点 JSON"
            className="hidden"
            onChange={importEndpointsFromFile}
          />
          <div className="flex flex-col gap-2 rounded-lg border border-kumo-line bg-kumo-base p-2 shadow-none cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
<div className="flex min-w-0 items-center gap-2">
              <ClipboardText
                size="sm"
                text={`${gatewayOrigin}/v1`}
                className="min-w-0 max-w-md flex-1 font-mono text-[0.9em]"
                tooltip={{ text: '复制 API Base URL', copiedText: '地址已复制', side: 'bottom' }}
                labels={{ copyAction: '复制 API Base URL' }}
              />
              {defaultGatewayKey?.apiKey ? (
                <ClipboardText
                  size="sm"
                  text={defaultGatewayKey.apiKey}
                  className="min-w-0 max-w-md flex-1 font-mono text-[0.9em]"
                  tooltip={{ text: '复制默认密钥', copiedText: '密钥已复制', side: 'bottom' }}
                  labels={{ copyAction: '复制默认密钥' }}
                />
              ) : (
                <span className="shrink-0 text-xs font-medium text-kumo-subtle">
                  未设置默认密钥
                </span>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-1.5">
              <StatusBadge tone="neutral">
                {endpoints.filter(endpoint => endpoint.enabled).length} 个启用端点
              </StatusBadge>
              <StatusBadge tone="brand">
                {enabledModelCount} 个启用模型
              </StatusBadge>
            </div>
          </div>
          {endpointsLoading ? (
            <div className="space-y-2.5">
              {[...Array(2)].map((_, i) => (
                <AppCard key={i} padding="md" className="space-y-2.5">
                  <div className="flex items-center gap-3">
                    <SkeletonLine className="w-10 h-10 rounded-lg" />
                    <div className="flex-1 space-y-1.5">
                      <SkeletonLine className="w-1/4 h-3.5" />
                      <SkeletonLine className="w-1/2 h-2.5" />
                    </div>
                  </div>
                </AppCard>
              ))}
            </div>
          ) : endpoints.length === 0 ? (
            <EmptyState
              icon={Bot}
              title="暂无 API 端点"
              description="新增 OpenAI 兼容端点"
            />
          ) : (
            (() => {
              const endpoint = selectedEndpoint;
              const validStatus = endpoint.status === 'valid';
              const invalidStatus = endpoint.status === 'invalid';
              const disabledModelCount = Array.isArray(endpoint.disabledModels)
                ? endpoint.disabledModels.length
                : 0;

              return (
                <div className="grid min-w-0 gap-3 cq-lg:grid-cols-[fit-content(28rem)_minmax(0,1fr)]">
                  <section className="flex min-w-0 flex-col gap-2 cq-lg:sticky cq-lg:top-[70px] cq-lg:self-start">
                    <div className="flex min-h-8 items-center justify-between gap-2 px-1">
                      <div className="flex items-center gap-2 text-xs text-kumo-subtle">
                        <Server className="h-3.5 w-3.5" />
                        <span className="font-medium text-kumo-strong">上游端点</span>
                      </div>
                      <span className="text-xs text-kumo-subtle">{endpoints.length} 个</span>
                    </div>
                    <LayerCard className="min-w-0 p-0 shadow-none">
                      <div className="overflow-x-auto overscroll-x-contain touch-pan-x scrollbar-thin">
                        <Table layout="fixed" className="w-full max-w-fit min-w-[420px] text-xs cq-lg:max-w-none">
                          <colgroup>
                            <col style={{ width: 176 }} />
                            <col style={{ width: 64 }} />
                            <col style={{ width: 64 }} />
                            <col style={{ width: 64 }} />
                            <col style={{ width: 64 }} />
                          </colgroup>
                          <Table.Header sticky variant="compact">
                            <Table.Row className="h-8">
                              <Table.Head className="!px-2.5 !py-1.5">端点</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">模型</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center" title="路由优先级（值越大越优先）">优先</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center" title="同优先级内的加权因子">权重</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {endpoints.map(item => (
                              <Table.Row
                                key={item.id}
                                variant={item.id === endpoint.id ? 'selected' : 'default'}
                                className={`h-11 cursor-pointer ${draggedEndpointId === String(item.id) ? 'opacity-40' : ''}`}
                                draggable={!endpointReorderSaving}
                                onDragStart={event => handleEndpointDragStart(item, event)}
                                onDragOver={handleEndpointDragOver}
                                onDrop={event => handleEndpointDrop(item, event)}
                                onDragEnd={handleEndpointDragEnd}
                                onClick={() => setSelectedEndpointId(item.id)}
                                onDoubleClick={() => openEditEndpointModal(item)}
                              >
                                <Table.Cell className="!px-2.5 !py-1.5">
                                  <div className="min-w-0">
                                    <div
                                      className="truncate font-semibold leading-5 text-kumo-strong"
                                      title={item.name}
                                    >
                                      {item.name || '未命名端点'}
                                    </div>
                                    <div
                                      className="truncate font-mono text-[10px] leading-4 text-kumo-subtle"
                                      title={item.baseUrl}
                                    >
                                      {item.baseUrl}
                                    </div>
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                                  {activeModelIdsForEndpoint(item).length}
                                </Table.Cell>
                                <Table.Cell className="!px-1.5 !py-1.5 text-center">
                                {routingEditKey === `${item.id}:priority` ? (
                                  <Input
                                    autoFocus
                                    size="sm"
                                    type="number"
                                    min={0}
                                    max={999}
                                    aria-label="路由优先级"
                                    value={routingDraft}
                                    onChange={event => setRoutingDraft(event.target.value)}
                                    onKeyDown={event => {
                                      event.stopPropagation();
                                      if (event.key === 'Enter') {
                                        saveEndpointRouting(item.id, 'priority', Number(routingDraft) || 0);
                                      } else if (event.key === 'Escape') {
                                        setRoutingEditKey(null);
                                      }
                                    }}
                                    onBlur={() => {
                                      if (routingEditKey === `${item.id}:priority`) {
                                        saveEndpointRouting(item.id, 'priority', Number(routingDraft) || 0);
                                      }
                                    }}
                                    className="h-6 w-12 text-center font-mono text-[11px]"
                                  />
                                ) : (
                                  <span
                                    className="block cursor-text font-mono text-[11px] text-kumo-strong"
                                    title="双击编辑路由优先级（值越大越优先）"
                                    onDoubleClick={event => {
                                      event.stopPropagation();
                                      setRoutingDraft(String(item.priority ?? 0));
                                      setRoutingEditKey(`${item.id}:priority`);
                                    }}
                                  >
                                    {item.priority ?? 0}
                                  </span>
                                )}
                              </Table.Cell>
                              <Table.Cell className="!px-1.5 !py-1.5 text-center">
                                {routingEditKey === `${item.id}:weight` ? (
                                  <Input
                                    autoFocus
                                    size="sm"
                                    type="number"
                                    min={1}
                                    max={9999}
                                    aria-label="路由权重"
                                    value={routingDraft}
                                    onChange={event => setRoutingDraft(event.target.value)}
                                    onKeyDown={event => {
                                      event.stopPropagation();
                                      if (event.key === 'Enter') {
                                        saveEndpointRouting(item.id, 'weight', Number(routingDraft) || 1);
                                      } else if (event.key === 'Escape') {
                                        setRoutingEditKey(null);
                                      }
                                    }}
                                    onBlur={() => {
                                      if (routingEditKey === `${item.id}:weight`) {
                                        saveEndpointRouting(item.id, 'weight', Number(routingDraft) || 1);
                                      }
                                    }}
                                    className="h-6 w-12 text-center font-mono text-[11px]"
                                  />
                                ) : (
                                  <span
                                    className="block cursor-text font-mono text-[11px] text-kumo-strong"
                                    title="双击编辑加权因子（值越大被选中概率越高）"
                                    onDoubleClick={event => {
                                      event.stopPropagation();
                                      setRoutingDraft(String(item.weight ?? 100));
                                      setRoutingEditKey(`${item.id}:weight`);
                                    }}
                                  >
                                    {item.weight ?? 100}
                                  </span>
                                )}
                              </Table.Cell>
                                <Table.Cell className="!px-2 !py-1.5 text-center">
                                  <div
                                    className="flex justify-center"
                                    onClick={event => event.stopPropagation()}
                                  >
                                    <Switch
                                      size="sm"
                                      aria-label={item.enabled ? '停用端点' : '启用端点'}
                                      checked={item.enabled}
                                      onCheckedChange={() => toggleEndpointEnabled(item)}
                                      disabled={!!endpointToggleLoading[item.id]}
                                    />
                                  </div>
                                </Table.Cell>
                              </Table.Row>
                            ))}
                          </Table.Body>
                        </Table>
                      </div>
                    </LayerCard>
                  </section>

                  <section className="flex min-h-0 min-w-0 flex-col gap-2">
                    <div className="flex min-h-8 flex-wrap items-center justify-between gap-2 px-1">
                      <div className="flex min-w-0 items-center gap-2 text-xs">
                        <span className="truncate font-medium text-kumo-strong">
                          {endpoint.name || '未命名端点'}
                        </span>
                        <Button
                          size="sm"
                          variant="secondary"
                          aria-label="复制端点地址"
                          title={endpoint.baseUrl}
                          onClick={() => {
                            navigator.clipboard
                              .writeText(endpoint.baseUrl)
                              .then(() => toast.success('端点地址已复制'))
                              .catch(() => toast.error('复制失败'));
                          }}
                        >
                          端点
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          aria-label="复制 API Key"
                          title="复制 API Key（默认首个）"
                          onClick={() => {
                            const keys = [endpoint.apiKey, ...(endpoint.apiKeys || [])].filter(Boolean);
                            navigator.clipboard
                              .writeText(keys[0] || '')
                              .then(() => toast.success(keys.length > 1 ? '已复制首个 API Key' : 'API Key 已复制'))
                              .catch(() => toast.error('复制失败'));
                          }}
                        >
                          密钥
                        </Button>
                      </div>
                      <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                        <StatusBadge
                          tone={validStatus ? 'success' : invalidStatus ? 'danger' : 'neutral'}
                        >
                          {validStatus ? '有效' : invalidStatus ? '无效' : '待检测'}
                        </StatusBadge>
                        {Array.isArray(endpoint.headers) && endpoint.headers.length > 0 && (
                          <StatusBadge
                            tone="info"
                            title={(endpoint.headers || [])
                              .map(h => `${h.name}: ${h.value}`)
                              .join('\n')}
                          >
                            {endpoint.headers.length} 请求头
                          </StatusBadge>
                        )}
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="模型健康检测"
                          onClick={() => openHealthCheckForEndpoint(endpoint.id)}
                          disabled={modelHealthBatchLoading}
                          title="模型健康检测"
                          icon={
                            modelHealthBatchLoading ? (
                              <Loader size="sm" />
                            ) : (
                              <Activity className={actionIconClass} />
                            )
                          }
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="刷新模型列表"
                          onClick={() => refreshEndpointModels(endpoint)}
                          loading={endpoint.refreshing}
                          title="刷新模型列表"
                          icon={<RefreshCw className={actionIconClass} />}
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="编辑端点"
                          onClick={() => openEditEndpointModal(endpoint)}
                          title="编辑端点"
                        >
                          <Edit className={actionIconClass} />
                        </Button>
                        <Button
                          shape="square"
                          size="sm"
                          variant={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? 'primary'
                              : 'secondary-destructive'
                          }
                          aria-label={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? `再次点击确认删除 ${endpoint.name || endpoint.baseUrl}`
                              : `删除 ${endpoint.name || endpoint.baseUrl}`
                          }
                          onClick={() => deleteEndpoint(endpoint)}
                          title={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? '再次点击确认删除'
                              : '删除端点'
                          }
                        >
                          <Trash className={actionIconClass} />
                        </Button>
                      </div>
                    </div>

                    <LayerCard className="min-w-0 p-0 shadow-none">
                      <div className="overflow-x-auto overscroll-x-contain touch-pan-x scrollbar-thin">
                        <Table layout="fixed" className="min-w-[820px] text-xs">
                          <colgroup>
                            <col style={{ width: 56 }} />
                            <col style={{ width: 260 }} />
                            <col style={{ width: 150 }} />
                            <col style={{ width: 92 }} />
                            <col style={{ width: 96 }} />
                            <col style={{ width: 150 }} />
                          </colgroup>
                          <Table.Header sticky variant="compact">
                            <Table.Row className="h-8">
                              <Table.Head className="!px-2 !py-1.5 text-center">
                                <div
                                  className="flex items-center justify-center"
                                  onClick={event => event.stopPropagation()}
                                  title={
                                    disabledModelCount > 0
                                      ? `启用 ${disabledModelCount} 个被停用的模型`
                                      : '关闭所有非有效模型（检测有效的保留）'
                                  }
                                >
                                  <Switch
                                    size="sm"
                                    aria-label={
                                      disabledModelCount > 0
                                        ? `启用 ${disabledModelCount} 个被停用的模型`
                                        : '关闭所有非有效模型（检测有效的保留）'
                                    }
                                    checked={disabledModelCount === 0}
                                    onCheckedChange={checked => {
                                      if (checked) {
                                        // 从关→开：启用全部被停用的模型
                                        batchEnableDisabledModels(endpoint);
                                      } else {
                                        // 从开→关：关闭所有非有效模型
                                        batchCloseNonHealthyModels(endpoint);
                                      }
                                    }}
                                    disabled={modelBatchActionLoading || modelHealthBatchLoading}
                                  />
                                </div>
                              </Table.Head>
                              <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">模型映射</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">健康</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">延迟</Table.Head>
                              <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {endpoint.models && endpoint.models.length > 0 ? (
                              endpoint.models.map(model => {
                                const modelId =
                                  typeof model === 'string'
                                    ? model.trim()
                                    : (model.id || '').trim();
                                const healthKey = modelHealthKey(endpoint.id, modelId);
                                const health = openaiModelHealth[healthKey];
                                const canStopHealthCheck =
                                  health?.loading &&
                                  modelHealthAbortControllersRef.current.has(healthKey);
                                const healthCheckAnimating = !!health?.loading;
                                const healthTone = health?.loading
                                  ? 'info'
                                  : health?.status === 'healthy'
                                    ? 'success'
                                    : health?.status === 'degraded'
                                      ? 'warning'
                                      : health?.status === 'error'
                                        ? 'danger'
                                        : 'neutral';
                                const healthLabel = health?.loading
                                  ? '检测中'
                                  : health?.status === 'healthy'
                                    ? '可用'
                                    : health?.status === 'degraded'
                                      ? '较慢'
                                      : health?.status === 'error'
                                        ? '失败'
                                        : health?.status === 'cancelled'
                                          ? '已停止'
                                          : '未检测';

                                return (
                                  <Table.Row key={`${endpoint.id}:${modelId}`} className="h-9">
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div
                                        className="flex justify-center"
                                        onClick={event => event.stopPropagation()}
                                      >
                                        <Switch
                                          size="sm"
                                          aria-label={modelEnabledForEndpoint(endpoint, modelId) ? `停用 ${modelId}` : `启用 ${modelId}`}
                                          checked={modelEnabledForEndpoint(endpoint, modelId)}
                                          onCheckedChange={enabled =>
                                            toggleModelEnabled(endpoint, modelId, enabled)
                                          }
                                          disabled={!!modelSwitchLoading[`${endpoint.id}:${modelId}`]}
                                        />
                                      </div>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2.5 !py-1.5">
                                      <span
                                        className="block truncate font-medium leading-5 text-kumo-strong"
                                        title={modelId}
                                      >
                                        {modelId}
                                      </span>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      {mappingEditKey === `${endpoint.id}:${modelId}` ? (
                                        <Input
                                          autoFocus
                                          size="sm"
                                          value={mappingDraft}
                                          aria-label="模型对外映射名称"
                                          onChange={event => setMappingDraft(event.target.value)}
                                          onKeyDown={event => {
                                            event.stopPropagation();
                                            if (event.key === 'Enter') {
                                              saveEndpointMapping(endpoint, modelId, mappingDraft);
                                            } else if (event.key === 'Escape') {
                                              setMappingEditKey(null);
                                            }
                                          }}
                                          onBlur={() => {
                                            if (mappingEditKey === `${endpoint.id}:${modelId}`) {
                                              saveEndpointMapping(endpoint, modelId, mappingDraft);
                                            }
                                          }}
                                          className="w-full font-mono text-[10px] text-center"
                                          placeholder="对外名称"
                                        />
                                      ) : (
                                        <span
                                          className="block cursor-text truncate font-mono text-[10px]"
                                          title="双击编辑对外映射名称"
                                          onDoubleClick={event => {
                                            event.stopPropagation();
                                            setMappingDraft(endpoint.modelMappings?.[modelId] || '');
                                            setMappingEditKey(`${endpoint.id}:${modelId}`);
                                          }}
                                        >
                                          {endpoint.modelMappings?.[modelId] ? (
                                            <span className="text-brand">
                                              {endpoint.modelMappings[modelId]}
                                            </span>
                                          ) : (
                                            <span className="text-kumo-subtle">双击设置</span>
                                          )}
                                        </span>
                                      )}
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <StatusBadge tone={healthTone}>
                                        {healthLabel}
                                      </StatusBadge>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                                      {health?.latency != null ? `${health.latency} ms` : '-'}
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div className="inline-flex gap-1">
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant={
                                            canStopHealthCheck
                                              ? 'secondary-destructive'
                                              : 'secondary'
                                          }
                                          aria-label={
                                            canStopHealthCheck
                                              ? `停止检测 ${modelId}`
                                              : `检测 ${modelId}`
                                          }
                                          onClick={() =>
                                            testModelHealth({ id: modelId }, endpoint.id)
                                          }
                                          disabled={modelHealthBatchLoading}
                                          title={
                                            health?.error ||
                                            (canStopHealthCheck
                                              ? '停止检测'
                                              : health?.loading
                                                ? '检测中'
                                                : '检测模型')
                                          }
                                          icon={
                                            healthCheckAnimating ? (
                                              <Loader size="sm" />
                                            ) : (
                                              <Activity className="h-3.5 w-3.5" />
                                            )
                                          }
                                        />
<Button
                                            shape="square"
                                            size="sm"
                                            variant="secondary"
                                            aria-label={`复制 ${modelId}`}
                                            onClick={() => {
                                              navigator.clipboard.writeText(modelId);
                                              toast.success('已复制模型名称');
                                            }}
                                            title="复制模型名称"
                                            icon={<Copy className="h-3.5 w-3.5" />}
                                          />
                                        </div>
                                      </Table.Cell>
                                    </Table.Row>
                                );
                              })
                            ) : (
                              <Table.Row>
                                <Table.Cell
                                  colSpan={6}
                                  className="py-10 text-center text-kumo-subtle"
                                >
                                  暂无模型数据，可刷新端点获取
                                </Table.Cell>
                              </Table.Row>
                            )}
                          </Table.Body>
                        </Table>
                      </div>
                    </LayerCard>
                  </section>
                </div>
              );
            })()
          )}
        </div>
      )}

      {/* ==================== 2. API 密钥 Tab ==================== */}
      {activeTab === 'keys' && (
        <div className="flex grow flex-col gap-3">
          <LayerCard className="w-full min-w-0 overflow-hidden p-0 shadow-none">
            <div className="min-w-0 overflow-x-auto scrollbar-thin">
              <Table layout="fixed" className="min-w-[1200px]">
                <colgroup>
                  <col style={{ width: 180 }} />
                  <col style={{ width: 240 }} />
                  <col style={{ width: 88 }} />
                  <col style={{ width: 150 }} />
                  <col style={{ width: 150 }} />
                  <col style={{ width: 100 }} />
                  <col style={{ width: 140 }} />
                  <col style={{ width: 152 }} />
                </colgroup>
                <Table.Header sticky variant="compact">
                  <Table.Row>
                    <Table.Head>名称</Table.Head>
                    <Table.Head className="text-center">密钥</Table.Head>
                    <Table.Head className="text-center">状态</Table.Head>
                    <Table.Head className="text-center">最近使用</Table.Head>
                    <Table.Head className="text-center">过期时间</Table.Head>
                    <Table.Head className="text-center">请求数</Table.Head>
                    <Table.Head className="text-center">Token 用量</Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {gatewayKeysLoading ? (
                    [...Array(3)].map((_, i) => (
                      <Table.Row key={i}>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-24" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-28" />
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <SkeletonLine className="mx-auto h-4 w-12" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-24" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-24" />
                        </Table.Cell>
                        <Table.Cell className="text-right">
                          <SkeletonLine className="ml-auto h-4 w-12" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="mx-auto h-4 w-24" />
                        </Table.Cell>
                      </Table.Row>
                    ))
                  ) : gatewayKeys.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">
                        暂无网关 API 密钥
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    gatewayKeys.map(key => (
                      <Table.Row
                        key={key.id}
                        className="hover:bg-kumo-recessed/5 cursor-pointer"
                        title="双击编辑密钥"
                        onDoubleClick={event =>
                          handleEditableRowDoubleClick(event, () => openEditGatewayKeyModal(key))
                        }
                      >
                        <Table.Cell
                          className="truncate font-semibold text-kumo-strong"
                          title={key.name}
                        >
                          {key.name || '未命名密钥'}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          {key.apiKey ? (
                            <ClipboardText
                              size="sm"
                              text={key.apiKey}
                              className="min-w-0 w-full font-mono text-[0.9em]"
                              tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制', side: 'bottom' }}
                              labels={{ copyAction: `复制 ${key.name} 的 API Key` }}
                            />
                          ) : (
                            <span className="text-sm text-kumo-subtle">轮换后可查看并复制</span>
                          )}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <StatusBadge tone={key.enabled ? 'success' : 'neutral'}>
                            {key.enabled ? '已启用' : '已停用'}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                          {key.lastUsed ? formatDateTime(key.lastUsed) : '从未使用'}
                        </Table.Cell>
                        <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                          {key.expiresAt ? formatDateTime(key.expiresAt) : '永不过期'}
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-[0.9em] text-kumo-strong">
                          {(key.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-[0.9em]">
                          {key.maxTokensQuota > 0 ? (
                            <span className="inline-flex items-center gap-1">
                              <span
                                className={
                                  (key.totalTokensUsed || 0) >= key.maxTokensQuota
                                    ? 'text-kumo-danger'
                                    : 'text-kumo-strong'
                                }
                              >
{(key.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                              </span>
                              <span className="text-kumo-subtle">
                                / {key.maxTokensQuota.toLocaleString('en-US', { useGrouping: false })}
                              </span>
                            </span>
                          ) : (
                            <span className="text-kumo-subtle">
                              {(key.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                            </span>
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex justify-center gap-1.5">
                            <Button
                              shape="square"
                              size="sm"
                              variant={key.enabled ? 'secondary-destructive' : 'primary'}
                              aria-label={key.enabled ? '停用密钥' : '启用密钥'}
                              onClick={() => toggleGatewayKey(key)}
                              title={key.enabled ? '停用密钥' : '启用密钥'}
                              loading={!!gatewayKeyToggleLoading[key.id]}
                              icon={<Reboot className="h-3.5 w-3.5" />}
                            />
                            <Button
                              shape="square"
                              size="sm"
                              variant={key.isDefault ? 'primary' : 'outline'}
                              aria-label={key.isDefault ? '默认密钥' : '设为默认密钥'}
                              onClick={() => setDefaultGatewayKey(key)}
                              disabled={key.isDefault}
                              className={key.isDefault ? undefined : 'text-kumo-subtle hover:text-brand'}
                              title={key.isDefault ? '当前为默认密钥' : '设为默认密钥'}
                            >
                              <Star className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant="outline"
                              aria-label="轮换密钥"
                              onClick={() => rotateGatewayKey(key)}
                              className="text-kumo-subtle hover:text-brand"
                              title="轮换密钥"
                            >
                              <RotateCw className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant="outline"
                              aria-label="编辑密钥"
                              onClick={() => openEditGatewayKeyModal(key)}
                              className="hover:text-brand text-kumo-subtle"
                              title="编辑密钥"
                            >
                              <Edit className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant={
                                isArmed(`gateway-key-${key.id}`)
                                  ? 'destructive'
                                  : 'secondary-destructive'
                              }
                              aria-label="删除密钥"
                              onClick={() => deleteGatewayKey(key)}
                              title={isArmed(`gateway-key-${key.id}`) ? '再次点击确认删除' : '删除密钥'}
                            >
                              <Trash className="w-3.5 h-3.5" />
                            </Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>
          </LayerCard>
        </div>
      )}

      {/* ==================== 3. 网关分析 Tab ==================== */}
      {activeTab === 'analytics' && (
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-sm:gap-3 cq-xl:grid-cols-6">
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">网关请求</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Activity className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                {analyticsLoading ? (
                  <SkeletonLine className="h-6 w-20" />
                ) : (
                  <div className="flex min-w-0 items-baseline gap-1">
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看成功/失败详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-lg font-semibold leading-none text-kumo-strong cq-sm:text-xl cq-xl:text-2xl">
                            {String(analyticsSummary.totalRequests)}
                          </span>
                        }
                      />
                      <Popover.Content className="w-56 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          网关请求详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">成功</span>
                            <span className="font-mono">
                              {String(Math.max(0, analyticsSummary.totalRequests - (analyticsSummary.errorCount || 0)))}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">失败</span>
                            <span className="font-mono">
                              {String(analyticsSummary.errorCount || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 border-t border-kumo-line text-kumo-strong">
                            <span className="text-kumo-subtle">错误率</span>
                            <span className="font-mono">
                              {((analyticsSummary.errorRate || 0) * 100).toFixed(2)}%
                            </span>
                          </div>
                        </div>
                      </Popover.Content>
                    </Popover>
                  </div>
                )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">最近 {analyticsDays} 天</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均端到端延迟</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-warning">
                    <Clock className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看首字/总耗时详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-lg font-semibold leading-none text-kumo-warning cq-sm:text-xl cq-xl:text-2xl">
                            {(analyticsSummary.avgLatency / 1000).toFixed(2)}
                          </span>
                        }
                      />
                      <Popover.Content className="w-64 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          延迟详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">平均首字延迟</span>
                            <span className="font-mono">
                              {analyticsSummary.avgTtfbMs > 0
                                ? `${(analyticsSummary.avgTtfbMs / 1000).toFixed(2)}s`
                                : '—'}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">平均端到端耗时</span>
                            <span className="font-mono">
                              {(analyticsSummary.avgLatency / 1000).toFixed(2)}s
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 border-t border-kumo-line text-kumo-strong">
                            <span className="text-kumo-subtle">首字后耗时（输出+传输）</span>
                            <span className="font-mono">
                              {analyticsSummary.avgTtfbMs > 0 && analyticsSummary.avgLatency > 0
                                ? `${(Math.max(0, analyticsSummary.avgLatency - analyticsSummary.avgTtfbMs) / 1000).toFixed(2)}s`
                                : '—'}
                            </span>
                          </div>
                        </div>
                      </Popover.Content>
                    </Popover>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">最近 {analyticsDays} 天</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">词元用量</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Brain className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                {analyticsLoading ? (
                  <SkeletonLine className="h-6 w-24" />
                ) : (
                  <div className="flex min-w-0 items-baseline gap-1">
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看输入/输出详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-lg font-semibold leading-none text-brand cq-sm:text-xl cq-xl:text-2xl">
                            {formatTokensM(analyticsSummary.totalTokens)}
                          </span>
                        }
                      />
                      <Popover.Content className="w-64 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          词元用量详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">输入（含缓存）</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalPromptTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">缓存命中</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalCachedTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">输出</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalCompletionTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 text-kumo-strong border-t border-kumo-line">
                            <span className="text-kumo-subtle">合计</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalTokens || 0)}
                            </span>
                          </div>
                        </div>
                      </Popover.Content>
                    </Popover>
                  </div>
                )}
                </div>
                <span
                  className="hidden truncate font-mono text-[11px] text-kumo-subtle cq-xl:block"
                  title="非缓存输入 = 输入（含缓存）− 缓存命中的词元"
                >
                  <ArrowDown
                    className="inline h-3 w-3 align-[-1px]"
                    aria-hidden="true"
                  />{' '}
                  {formatTokensM(Math.max(0, analyticsSummary.totalPromptTokens - analyticsSummary.totalCachedTokens))}（
                  {analyticsSummary.totalPromptTokens > 0
                    ? `${(
                        (Math.max(0, analyticsSummary.totalPromptTokens - analyticsSummary.totalCachedTokens) /
                          analyticsSummary.totalPromptTokens) *
                        100
                      ).toFixed(1)}%`
                    : '0.0%'}
                  ） · <ArrowUp className="inline h-3 w-3 align-[-1px]" aria-hidden="true" />{' '}
                  {formatTokensM(analyticsSummary.totalCompletionTokens || 0)}
                </span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均 TPM</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Cpu className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-lg font-semibold leading-none text-brand cq-sm:text-xl cq-xl:text-2xl">
                        {((analyticsSummary.totalTokens || 0) / Math.max(1, analyticsDays * 24 * 60)).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">/min</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">每分钟词元</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均 RPM</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <TrendingUp className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-lg font-semibold leading-none text-brand cq-sm:text-xl cq-xl:text-2xl">
                        {((analyticsSummary.totalRequests || 0) / Math.max(1, analyticsDays * 24 * 60)).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">/min</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">每分钟请求</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">上游错误率</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-danger">
                    <AlertTriangle className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看各渠道错误率"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-lg font-semibold leading-none text-kumo-danger cq-sm:text-xl cq-xl:text-2xl">
                            {(analyticsSummary.errorRate * 100).toFixed(1)}
                          </span>
                        }
                      />
                      <Popover.Content className="w-72 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          各渠道错误率
                        </Popover.Title>
                        {analyticsSummary.endpointErrorRates?.length ? (
                          <div className="mt-2 flex max-h-60 flex-col gap-1.5 overflow-y-auto pr-1 text-xs text-kumo-strong">
                            {analyticsSummary.endpointErrorRates.map((item) => (
                              <div
                                key={item.endpointId || item.endpointName}
                                className="flex items-center justify-between gap-3"
                              >
                                <span className="min-w-0 truncate text-kumo-subtle" title={item.endpointName}>
                                  {item.endpointName}
                                </span>
                                <span className="flex shrink-0 items-baseline gap-1.5 font-mono">
                                  <span className={item.errorRate > 0 ? 'text-kumo-danger' : 'text-kumo-strong'}>
                                    {((item.errorRate || 0) * 100).toFixed(1)}%
                                  </span>
                                  <span className="text-[10px] text-kumo-subtle">
                                    {item.errors}/{item.requests}
                                  </span>
                                </span>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div className="mt-2 text-xs text-kumo-subtle">暂无渠道数据</div>
                        )}
                      </Popover.Content>
                    </Popover>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">请求失败占比</span>
              </AppCard>
            </div>

            <div className="grid gap-3 cq-xl:grid-cols-2">
            {[
              {
                key: 'requests',
                icon: <Activity className="h-4 w-4 text-brand" />,
                title: '请求量趋势',
                series: trendSeries.requests,
              },
              {
                key: 'tokens',
                icon: <Brain className="h-4 w-4 text-brand" />,
                title: '词元趋势',
                series: trendSeries.tokens,
              },
              {
                key: 'latency',
                icon: <Clock className="h-4 w-4 text-kumo-warning" />,
                title: '平均延迟趋势',
                series: trendSeries.latency,
              },
              {
                key: 'errors',
                icon: <AlertTriangle className="h-4 w-4 text-kumo-danger" />,
                title: '错误率趋势',
                series: trendSeries.errorRate,
              },
            ].map(card => {
              const series =
                card.key === 'tokens'
                  ? tokenTrendMode === 'uncached'
                    ? trendSeries.tokensUncached
                    : trendSeries.tokens
                  : card.key === 'latency'
                    ? latencyTrendMode === 'ttfb'
                      ? trendSeries.latencyTtfb
                      : trendSeries.latency
                    : card.key === 'errors'
                      ? errorTrendMode === 'count'
                        ? trendSeries.errorCount
                        : trendSeries.errorRate
                      : card.series;
              const toggleTabs =
                card.key === 'tokens' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={tokenTrendMode}
                    onValueChange={setTokenTrendMode}
                    tabs={[
                      { value: 'all', label: '全部' },
                      { value: 'uncached', label: '未缓存' },
                    ]}
                  />
                ) : card.key === 'latency' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={latencyTrendMode}
                    onValueChange={setLatencyTrendMode}
                    tabs={[
                      { value: 'total', label: '总耗时' },
                      { value: 'ttfb', label: '首字' },
                    ]}
                  />
                ) : card.key === 'errors' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={errorTrendMode}
                    onValueChange={setErrorTrendMode}
                    tabs={[
                      { value: 'rate', label: '错误率' },
                      { value: 'count', label: '错误数' },
                    ]}
                  />
                ) : null;
              return (
              <LayerCard key={card.key} className="min-w-0 p-0">
                <LayerCard.Secondary>
                  {toggleTabs ? (
                    <div className="flex w-full items-center justify-between gap-2">
                      <span>{card.title}</span>
                      {toggleTabs}
                    </div>
                  ) : (
                    card.title
                  )}
                </LayerCard.Secondary>
                <LayerCard.Primary className="flex min-h-0 flex-col gap-2 !p-3">
                  <div className="min-h-0 w-full" style={{ height: 168 }}>
                    {series.labels.length === 0 && !analyticsLoading ? (
                      <div className="flex h-full items-center justify-center text-sm text-kumo-subtle">
                        暂无数据
                      </div>
                    ) : (
                      <TrendBarChart
                        labels={series.labels}
                        values={series.values}
                        color={series.color}
                        isDarkMode={isDarkMode}
                        loading={analyticsLoading}
                        formatValue={series.formatValue}
                        formatAxis={series.formatAxis}
                      />
                    )}
                  </div>
                </LayerCard.Primary>
              </LayerCard>
              );
            })}
          </div>

            <div className="grid">
            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary>
                <div className="flex w-full items-center justify-between gap-2">
                  <span>模型调用趋势</span>
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={modelTrendMode}
                    onValueChange={setModelTrendMode}
                    tabs={[
                      { value: 'model', label: '按模型' },
                      { value: 'endpoint', label: '按站点' },
                    ]}
                  />
                </div>
              </LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
              {(!Array.isArray(byModelTrend.labels) || byModelTrend.labels.length === 0) && !analyticsLoading ? (
                <div className="flex h-[240px] items-center justify-center text-sm text-kumo-subtle">
                  暂无数据
                </div>
              ) : (
                <ModelTrendChart
                  labels={byModelTrend.labels}
                  series={modelTrendMode === 'endpoint' ? byModelTrend.endpoints : byModelTrend.models}
                  isDarkMode={isDarkMode}
                  loading={analyticsLoading}
                />
              )}
              </LayerCard.Primary>
            </LayerCard>
          </div>

            <div className="grid gap-3 cq-xl:grid-cols-2">
            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary>
                <div className="flex w-full items-center justify-between gap-2">
                  <span>模型词元分布</span>
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={tokenShareMode}
                    onValueChange={setTokenShareMode}
                    tabs={[
                      { value: 'model', label: '按模型' },
                      { value: 'endpoint', label: '按站点' },
                    ]}
                  />
                </div>
              </LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
                <div className="min-h-0">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="w-full h-4" />
                    <SkeletonLine className="w-full h-4" />
                  </div>
                ) : (
                  (() => {
                    const shareData =
                      tokenShareMode === 'endpoint'
                        ? analyticsCharts.endpoints || []
                        : analyticsCharts.models || [];
                    if (shareData.length === 0) {
                      return <div className="py-16 text-center text-sm text-kumo-subtle">暂无数据</div>;
                    }
                    const totalTokens =
                      shareData.reduce(
                        (sum, model) => sum + (Number(model.tokens) || 0),
                        0
                      ) || 1;
                    const sorted = [...shareData]
                      .sort((a, b) => (Number(b.tokens) || 0) - (Number(a.tokens) || 0))
                      .slice(0, 20);
                    return (
                      <div className="flex flex-col gap-1.5">
                        {sorted.map((model, index) => {
                          const tokens = Number(model.tokens) || 0;
                          const percent = (tokens / totalTokens) * 100;
                          return (
                            <div
                              key={`${model.model}:${index}`}
                              className="flex items-center gap-2 text-xs"
                            >
                              <span
                                className="w-40 shrink-0 truncate font-medium text-kumo-strong"
                                title={model.model}
                              >
                                {model.model}
                              </span>
                              <div className="h-3 min-w-0 flex-1 overflow-hidden rounded-full bg-kumo-recessed">
                                <div
                                  className="h-full rounded-full"
                                  style={{
                                    width: `${Math.max(2, Math.min(100, percent))}%`,
                                    background: ChartPalette.categorical(index, isDarkMode),
                                  }}
                                />
                              </div>
                              <span className="w-14 shrink-0 text-right font-mono text-[11px] text-kumo-subtle">
                                {formatTokensM(tokens)}
                              </span>
                              <span className="w-11 shrink-0 text-right font-mono text-[10px] text-kumo-subtle">
                                {percent.toFixed(1)}%
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    );
                  })()
                )}
              </div>
              </LayerCard.Primary>
            </LayerCard>

            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary>
                <div className="flex w-full items-center justify-between gap-2">
                  <span>模型调用次数</span>
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={countShareMode}
                    onValueChange={setCountShareMode}
                    tabs={[
                      { value: 'model', label: '按模型' },
                      { value: 'endpoint', label: '按站点' },
                    ]}
                  />
                </div>
              </LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
                <div className="min-h-0">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="h-4 w-full" />
                    <SkeletonLine className="h-4 w-full" />
                  </div>
                ) : (
                  (() => {
                    const shareData =
                      countShareMode === 'endpoint'
                        ? analyticsCharts.endpoints || []
                        : analyticsCharts.models || [];
                    if (shareData.length === 0) {
                      return <div className="py-16 text-center text-sm text-kumo-subtle">暂无数据</div>;
                    }
                    const totalCount =
                      shareData.reduce(
                        (sum, model) => sum + (Number(model.count) || 0),
                        0
                      ) || 1;
                    const sorted = [...shareData]
                      .sort((a, b) => (Number(b.count) || 0) - (Number(a.count) || 0))
                      .slice(0, 20);
                    return (
                      <div className="flex flex-col gap-1.5">
                        {sorted.map((model, index) => {
                          const count = Number(model.count) || 0;
                          const percent = (count / totalCount) * 100;
                          return (
                            <div
                              key={`${model.model}:${index}`}
                              className="flex items-center gap-2 text-xs"
                            >
                              <span
                                className="w-40 shrink-0 truncate font-medium text-kumo-strong"
                                title={model.model}
                              >
                                {model.model}
                              </span>
                              <div className="h-3 min-w-0 flex-1 overflow-hidden rounded-full bg-kumo-recessed">
                                <div
                                  className="h-full rounded-full"
                                  style={{
                                    width: `${Math.max(2, Math.min(100, percent))}%`,
                                    background: ChartPalette.categorical(index, isDarkMode),
                                  }}
                                />
                              </div>
                              <span className="w-14 shrink-0 text-right font-mono text-[11px] text-kumo-subtle">
                                {formatCompact(count, 0)}
                              </span>
                              <span className="w-11 shrink-0 text-right font-mono text-[10px] text-kumo-subtle">
                                {percent.toFixed(1)}%
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    );
                  })()
                )}
              </div>
              </LayerCard.Primary>
            </LayerCard>
          </div>
        </div>
      )}

      {/* ==================== 4. 网关日志 Tab ==================== */}
      {activeTab === 'logs' && (
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          {/* 日志筛选区：状态 / 模型 / 端点，均即时生效 */}
          <div className="flex flex-wrap items-center gap-2">
            <Select
              size="sm"
              className="w-28"
              value={logStatusFilter || undefined}
              onValueChange={value => {
                setLogStatusFilter(value || '');
                setAnalyticsPage(1);
              }}
              placeholder="全部状态"
              aria-label="状态筛选"
            >
              <Select.Option value="">全部状态</Select.Option>
              <Select.Option value="success">成功</Select.Option>
              <Select.Option value="error">失败 (≥400)</Select.Option>
              <Select.Option value="429">限流 429</Select.Option>
              <Select.Option value="5xx">服务端 5xx</Select.Option>
            </Select>
            <Input
              size="sm"
              className="w-52"
              value={logModelFilter}
              aria-label="按模型筛选"
              onChange={e => {
                setLogModelFilter(e.target.value);
                setAnalyticsPage(1);
              }}
              placeholder="按模型筛选，如 deepseek-v4-flash"
              spellCheck={false}
            />
            <Input
              size="sm"
              className="w-52"
              value={logEndpointFilter}
              aria-label="按端点筛选"
              onChange={e => {
                setLogEndpointFilter(e.target.value);
                setAnalyticsPage(1);
              }}
              placeholder="按端点筛选（完整名称或 ID）"
              spellCheck={false}
            />
            {(logStatusFilter || logModelFilter || logEndpointFilter) && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setLogStatusFilter('');
                  setLogModelFilter('');
                  setLogEndpointFilter('');
                  setAnalyticsPage(1);
                }}
                icon={<X className="h-3.5 w-3.5" />}
              >
                清除筛选
              </Button>
            )}
          </div>
          {/* Logs table and pagination */}
          <LayerCard className="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden p-0 shadow-none">
            <div className="min-h-0 min-w-0 flex-1 overflow-auto scrollbar-thin">
              <Table layout="fixed" className="min-w-[1380px] [&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
<colgroup>
                  <col style={{ width: 140 }} />
                  <col style={{ width: 80 }} />
                  <col style={{ width: 104 }} />
                  <col style={{ width: 140 }} />
                  <col style={{ width: 100 }} />
                  <col style={{ width: 100 }} />
                  <col style={{ width: 64 }} />
                  <col style={{ width: 160 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 132 }} />
                </colgroup>
                <Table.Header sticky variant="compact">
                  <Table.Row>
                    <Table.Head className="text-center">时间</Table.Head>
                    <Table.Head className="text-center">路由</Table.Head>
                    <Table.Head className="text-center">端点</Table.Head>
                    <Table.Head className="text-center">模型</Table.Head>
                    <Table.Head className="text-center">出口 IP</Table.Head>
                    <Table.Head className="text-center">客户端 IP</Table.Head>
                    <Table.Head className="text-center">状态</Table.Head>
                    <Table.Head className="text-center">耗时/首字</Table.Head>
                    <Table.Head className="text-left">输入 / 输出</Table.Head>
                    <Table.Head className="text-left">缓存</Table.Head>
                    <Table.Head className="text-left">总消耗</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {analyticsLoading && analyticsLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={11} className="text-center py-8">
                        <Loader size={20} className="mx-auto text-kumo-subtle" />
                      </Table.Cell>
                    </Table.Row>
                  ) : analyticsLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={11} className="text-center py-8 text-kumo-subtle text-sm">
                        暂无网关日志记录
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    analyticsLogs.map(log => {
                      return (
                        <Table.Row key={log.id} className="text-sm">
                          <Table.Cell className="truncate text-center font-mono text-kumo-subtle">
                            {formatDateTime(log.timestamp)}
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-mono text-kumo-subtle"
                            title={log.route}
                          >
                            {!log.route ? '-' : log.route.replace(/^chat\./, '')}
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-semibold text-kumo-strong"
                            title={log.endpointName}
                          >
                            <span className="inline-flex items-center justify-center gap-1.5">
                              <FailoverPathBadge path={log.failoverPath} endpointName={log.endpointName} />
                              {typeof log.keyIndex === 'number' && log.keyIndex >= 0 && (
                                <StatusBadge tone="info" title={`使用的 API Key 序号（0=主 key）`}>
                                  K{log.keyIndex + 1}
                                </StatusBadge>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell
                            className={`truncate text-center font-mono font-medium ${log.statusCode >= 400 && log.errorResponse ? 'cursor-pointer text-kumo-danger' : 'text-kumo-strong'}`}
                            title={log.statusCode >= 400 && log.errorResponse ? '点击查看报错详情' : log.model}
                            onClick={log.statusCode >= 400 && log.errorResponse ? () => {
                              setLogDetailExpanded(false);
                              setLogDetail(log);
                            } : undefined}
                          >
                            {log.model}
                          </Table.Cell>
                          <Table.Cell
                            className="text-center font-mono text-kumo-subtle"
                            title={log.upstreamIp || '本机出口'}
                          >
                            <div
                              className="inline-flex items-center justify-center gap-1"
                              title="经代理池出口"
                            >
                              <IpCell value={log.upstreamIp} viaProxy={log.viaProxy} />
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-mono text-kumo-subtle"
                            title={log.clientIp || '无客户端 IP'}
                          >
                            <IpCell value={log.clientIp} v6EdgeOnly />
                          </Table.Cell>
                          <Table.Cell className="text-center">
                            <span className="inline-flex items-center gap-1">
                              <StatusBadge tone={statusCodeTone(log.statusCode)}>
                                {log.statusCode}
                              </StatusBadge>
                              {log.statusCode === 429 && (
                                <StatusBadge tone="warning" title="上游限流或网关无可用渠道">
                                  限
                                </StatusBadge>
                              )}
                              {log.statusCode === 503 && (
                                <StatusBadge tone="warning" title="网关无可用渠道">
                                  无
                                </StatusBadge>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell className="text-center">
                            <div
                              className="inline-flex items-center gap-1"
                              title={log.stream ? '流式响应' : '非流式响应'}
                            >
                              <StatusBadge tone={resultTone(log.statusCode, log.completionTokens, log.latencyMs)}>
                                {(log.latencyMs / 1000).toFixed(1)}s
                              </StatusBadge>
                              <StatusBadge tone={ttfbTone(log.ttfbMs)}>
                                {log.ttfbMs > 0 ? (log.ttfbMs / 1000).toFixed(1) + 's' : '—'}
                              </StatusBadge>
                              <StatusBadge
                                tone={log.stream ? 'info' : 'warning'}
                                className="!px-1.5 !text-[10px]"
                              >
                                {log.stream ? '流' : '非流'}
                              </StatusBadge>
                            </div>
                          </Table.Cell>
                          <Table.Cell className="text-left font-mono">
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right text-kumo-strong">
                                {log.promptTokens}
                              </span>
                              <span className="shrink-0 px-0.5 text-kumo-subtle">/</span>
                              <span className="text-left text-kumo-strong">
                                {log.completionTokens}
                              </span>
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="text-left font-mono"
                            title="缓存命中 词元（占比 = 缓存 / 输入）"
                          >
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right text-kumo-strong">
                                {log.cachedTokens}
                              </span>
                              <span className="shrink-0 px-0.5 text-kumo-subtle">（</span>
                              <span className="text-left text-kumo-strong">
                                {log.promptTokens > 0
                                  ? ((log.cachedTokens / log.promptTokens) * 100).toFixed(1)
                                  : '0.0'}
                                %
                              </span>
                              <span className="shrink-0 text-kumo-subtle">）</span>
                            </div>
                          </Table.Cell>
<Table.Cell
                            className="text-left font-mono"
                            title="总消耗（实际消耗 = 总消耗 − 缓存）"
                          >
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right font-semibold leading-none text-brand">
                                {log.totalTokens}
                              </span>
                              <span className="shrink-0 px-0.5 leading-none text-kumo-subtle">（</span>
                              <span className="text-left font-mono leading-none text-kumo-subtle">
                                {Math.max(0, log.totalTokens - log.cachedTokens)}
                              </span>
                              <span className="shrink-0 leading-none text-kumo-subtle">）</span>
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      );
                    })
                  )}
                </Table.Body>
              </Table>
            </div>

            {analyticsTotal > 0 && (
              <Pagination
                page={analyticsPage}
                setPage={setAnalyticsPage}
                perPage={analyticsPageSize}
                totalCount={analyticsTotal}
                labels={{
                  navigation: '网关日志分页',
                  firstPage: '第一页',
                  previousPage: '上一页',
                  nextPage: '下一页',
                  lastPage: '最后一页',
                  pageNumber: '页码',
                  pageSize: '每页数量',
                }}
                className="shrink-0 flex-wrap gap-x-3 gap-y-1 border-x-0 border-b-0 border-t border-kumo-line bg-kumo-base px-3 py-2 text-sm shadow-none [&_[data-slot=pagination-controls]]:ml-auto [&_[data-slot=pagination-info]]:min-w-0 max-sm:[&_[data-slot=pagination-info]]:hidden max-sm:[&_[data-slot=pagination-page-size]]:hidden max-sm:[&_[data-slot=pagination-separator]]:hidden max-sm:[&_[data-slot=pagination-controls]]:m-auto"
              >
                <Pagination.Info>
                  {({ pageShowingRange, totalCount }) => (
                    <span className="text-kumo-subtle">
                      显示 {pageShowingRange}，共 {totalCount} 条
                    </span>
                  )}
                </Pagination.Info>
                <Pagination.Separator />
                <Pagination.PageSize
                  value={analyticsPageSize}
                  onChange={size => {
                    setAnalyticsPageSize(size);
                    setAnalyticsPage(1);
                  }}
                  options={[10, 20, 50, 100]}
                  label="每页"
                />
                <Pagination.Controls />
              </Pagination>
            )}
          </LayerCard>
        </div>
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 0. 网关日志报错详情 Dialog（仅失败请求记录 errorKind/errorMessage/errorResponse） */}
      <Dialog.Root open={!!importModeDialog} onOpenChange={open => !open && setImportModeDialog(null)}>
        <Dialog className="!w-[min(30rem,calc(100vw-2rem))]">
          <div className="grid gap-1 px-6 pt-5 pb-4">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">导入端点</Dialog.Title>
            <Dialog.Description className="text-xs text-kumo-subtle">
              共 {importModeDialog?.count ?? 0} 个端点，请选择导入方式（文件包含完整配置：密钥、模型、映射、请求头、代理池与订阅、优先级权重）
            </Dialog.Description>
          </div>
          <div className="flex items-center justify-end gap-2 border-t border-kumo-line px-6 py-4">
            <Button size="sm" variant="secondary" onClick={() => setImportModeDialog(null)}>取消</Button>
            <Button size="sm" variant="secondary" onClick={() => importModeDialog && runEndpointImport(importModeDialog.list, false)}>
              跳过已有（仅新增）
            </Button>
            <Button size="sm" variant="primary" onClick={() => importModeDialog && runEndpointImport(importModeDialog.list, true)}>
              覆盖导入（替换全部）
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!logDetail} onOpenChange={open => !open && setLogDetail(null)}>        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(52rem,calc(100vw-2rem))] !max-w-[min(52rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 border-b border-kumo-line px-6 pt-5 pb-4">
            <div className="mb-1 flex items-center gap-2">
              <Dialog.Title className="text-sm font-semibold text-kumo-strong">
                报错详情
              </Dialog.Title>
              {logDetail?.errorKind && (
                <StatusBadge tone="danger" title={`错误环节：${logDetail.errorKind}`}>
                  {errorKindLabel(logDetail.errorKind)}
                </StatusBadge>
              )}
            </div>
            <Dialog.Description className="text-xs text-kumo-subtle">
              {logDetail && (
                <>
                  {formatDateTime(logDetail.timestamp)} · {logDetail.route || 'chat.completions'} ·{' '}
                  {logDetail.model || '—'} · 状态 {logDetail.statusCode}
                  {logDetail.endpointName ? ` · ${logDetail.endpointName}` : ''}
                </>
              )}
            </Dialog.Description>
            {logDetail?.errorMessage && (
              <div className="mt-2 rounded-md border border-kumo-danger/25 bg-kumo-danger/5 px-3 py-2 text-xs font-medium text-kumo-danger">
                {logDetail.errorMessage}
              </div>
            )}
          </div>
          <div className="min-h-0 flex-1 overflow-auto px-6 py-4 scrollbar-thin">
            {(() => {
              if (!logDetail?.errorResponse) {
                return (
                  <div className="text-xs text-kumo-subtle">
                    该请求无报错 JSON 记录（如流式响应未采集响应体，可查看调用日志行与 relay-errors 接口）。
                  </div>
                );
              }
              let parses = true;
              try {
                JSON.parse(logDetail.errorResponse);
              } catch {
                parses = false;
              }
              const truncated = logDetail.errorResponse.includes('...(truncated)');
              if (!parses || truncated) {
                return (
                  <div className="mb-3 rounded-md border border-kumo-warning/30 bg-kumo-warning/10 px-3 py-2 text-xs text-kumo-warning">
                    {truncated
                      ? '报错 JSON 超过记录上限（64KB）已截断，内容不完整'
                      : '该内容不是标准 JSON，以下为原始内容排版'}
                  </div>
                );
              }
              return null;
            })()}
            {logDetail?.errorResponse && (
              <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-kumo-strong">
                {(() => {
                  const text = formatErrorResponseForDisplay(logDetail.errorResponse);
                  if (logDetailExpanded || text.length <= LOG_DETAIL_COLLAPSE_LIMIT) return text;
                  return `${text.slice(0, LOG_DETAIL_COLLAPSE_LIMIT)}…\n\n（内容较长，仅显示前 ${LOG_DETAIL_COLLAPSE_LIMIT} 字符，点击下方「展开全部」查看完整报错 JSON）`;
                })()}
              </pre>
            )}
          </div>
          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line px-6 py-3">
            {logDetail?.errorResponse && logDetail.errorResponse.length > LOG_DETAIL_COLLAPSE_LIMIT && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setLogDetailExpanded(v => !v)}
                title={logDetailExpanded ? '折叠为预览内容' : '显示完整报错 JSON'}
              >
                {logDetailExpanded ? '收起' : '展开全部'}
              </Button>
            )}
            <Button
              size="sm"
              variant="secondary"
              disabled={!logDetail?.errorResponse}
              onClick={() => {
                navigator.clipboard
                  .writeText(String(logDetail?.errorResponse || ''))
                  .then(() => toast.success('报错 JSON 已复制'))
                  .catch(() => toast.error('复制失败'));
              }}
            >
              复制报错 JSON
            </Button>
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>关闭</Button>} />
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 1. Endpoint Add/Edit Dialog */}
      <Dialog.Root
        open={endpointFormOpen}
        onOpenChange={open => {
          setEndpointFormOpen(open);
          if (!open) setEndpointKeyChecks([]);
        }}
      >
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),42rem)] !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {editingEndpoint ? '编辑端点' : '添加 API 端点'}
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              配置 OpenAI 兼容的 API 端点以供中转或对话使用。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="grid grid-cols-2 gap-x-5 gap-y-4">
              {/* ====== 左列：基本信息 ====== */}
              <div className="space-y-4">
                <Input
                  size="sm"
                  label="名称"
                  type="text"
                  value={endpointForm.name}
                  onChange={e => setEndpointForm({ ...endpointForm, name: e.target.value })}
                  placeholder="如：DeepSeek 官方"
                  className="w-full text-kumo-strong text-sm font-sans"
                />

                <Input
                  size="sm"
                  label="Base URL"
                  type="text"
                  value={endpointForm.baseUrl}
                  onChange={e => setEndpointForm({ ...endpointForm, baseUrl: e.target.value })}
                  placeholder="https://api.openai.com/v1"
                  className="w-full text-kumo-strong text-[0.9em] font-mono"
                />

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label>API Key 列表</Label>
                    <Button
                      size="xs"
                      variant="secondary"
                      type="button"
                      disabled={endpointKeyChecking || !editingEndpoint}
                      onClick={() =>
                        checkEndpointKeys(
                          [endpointForm.apiKey, ...(endpointForm.apiKeys || [])],
                          editingEndpoint?.id
                        )
                      }
                    >
                      <RotateCw className={cx(endpointKeyChecking && 'animate-spin')} size={14} />
                      {endpointKeyChecking ? '检测中' : '重新检测'}
                    </Button>
                  </div>

                  <div className="space-y-1.5">
                    {[endpointForm.apiKey, ...(endpointForm.apiKeys || [])].map((key, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="grid grid-cols-[2rem_minmax(0,1fr)_auto_1.75rem] items-center gap-1.5"
                      >
                        <span className="text-center text-[11px] font-semibold text-kumo-subtle select-none">
                          K{rowIndex + 1}
                        </span>
                        <Input
                          size="sm"
                          type="text"
                          value={key}
                          aria-label={`API Key K${rowIndex + 1}`}
                          onChange={e => {
                            const value = e.target.value;
                            setEndpointForm(current => {
                              if (rowIndex === 0) {
                                return { ...current, apiKey: value };
                              }
                              return {
                                ...current,
                                apiKeys: (current.apiKeys || []).map((k, j) =>
                                  j === rowIndex - 1 ? value : k
                                ),
                              };
                            });
                            setEndpointKeyChecks(prev => {
                              const next = [...prev];
                              next[rowIndex] = null;
                              return next;
                            });
                          }}
                          placeholder="sk-xxxxxxxxxxxxxxxxxxxxxxxx"
                          autoComplete="off"
                          data-1p-ignore
                          data-lpignore="true"
                          data-bwignore="true"
                          data-form-type="other"
                          spellCheck={false}
                          className="w-full text-kumo-strong text-[0.9em] font-mono"
                        />
                        <KeyStatusBadge check={endpointKeyChecks?.[rowIndex]} />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary-destructive"
                          aria-label={`删除 Key K${rowIndex + 1}`}
                          onClick={() => removeEndpointKey(rowIndex)}
                          title="删除此 Key"
                          icon={<Trash className="h-3.5 w-3.5" />}
                        />
                      </div>
                    ))}

                    <Button
                      size="xs"
                      variant="outline"
                      onClick={appendEndpointKey}
                      icon={<Plus className="h-3.5 w-3.5" />}
                    >
                      添加 Key
                    </Button>
                  </div>
                </div>

                <Input
                  size="sm"
                  label="备注"
                  type="text"
                  value={endpointForm.notes}
                  onChange={e => setEndpointForm({ ...endpointForm, notes: e.target.value })}
                  placeholder="选填"
                  className="w-full text-kumo-strong text-sm font-sans"
                />
              </div>

              {/* ====== 右列：连接与代理 ====== */}
              <div className="space-y-4">
                <div className="space-y-1.5">
                  <Label>
                    自定义请求头
                    <span className="font-normal text-kumo-subtle">（可选）</span>
                  </Label>
                  <div className="space-y-2">
                    {(endpointForm.headers || []).map((header, index) => (
                      <div
                        key={index}
                        className="grid grid-cols-[minmax(0,1.2fr)_minmax(0,1.8fr)_2rem] items-center gap-2"
                      >
                        <Input
                          size="sm"
                          type="text"
                          value={header.name}
                          aria-label="Header 名称"
                          onChange={e => updateEndpointHeader(index, 'name', e.target.value)}
                          placeholder="Header 名称"
                          spellCheck={false}
                          autoComplete="off"
                          data-1p-ignore
                          className="w-full text-kumo-strong font-mono text-[0.85em]"
                        />
                        <Input
                          size="sm"
                          type="text"
                          value={header.value}
                          aria-label="Header 值"
                          onChange={e => updateEndpointHeader(index, 'value', e.target.value)}
                          placeholder="Header 值"
                          spellCheck={false}
                          autoComplete="off"
                          data-1p-ignore
                          className="w-full text-kumo-strong font-mono text-[0.85em]"
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary-destructive"
                          aria-label="删除请求头"
                          onClick={() => removeEndpointHeader(index)}
                          title="删除请求头"
                          icon={<Trash className="h-3.5 w-3.5" />}
                        />
                      </div>
                    ))}
                  </div>
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={addEndpointHeader}
                    icon={<Plus className="h-3.5 w-3.5" />}
                  >
                    添加请求头
                  </Button>
                </div>

                <Select
                  size="sm"
                  label="连接协议"
                  value={endpointForm.protocol || 'auto'}
                  onValueChange={value => setEndpointForm(current => ({ ...current, protocol: value }))}
                  items={ENDPOINT_PROTOCOL_OPTIONS}
                  className="w-full"
                />

                <div className="space-y-1.5">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <Label>
                      出口代理池
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => setProxyManagerOpen(true)}
                      icon={<Sliders className="h-3.5 w-3.5" />}
                    >
                      管理代理池（{endpointForm.proxyPool?.length || 0}）
                    </Button>
                  </div>
                  <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2">
                    {endpointForm.proxyPool?.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1.5">
                        {(endpointForm.proxyBatches || []).map(batch => (
                          <Badge
                            key={batch.id}
                            variant="outline"
                            className="w-fit gap-1 !text-[11px] font-medium"
                            title={`${batch.name}\n${batch.proxies?.length || 0} 条`}
                          >
                            <span className="max-w-40 truncate">{batch.name}</span>
                            <span className="shrink-0 font-mono text-[10px] text-kumo-subtle">
                              {batch.proxies?.length || 0} 条
                            </span>
                          </Badge>
                        ))}
                        {manualProxyEntries.length > 0 && (
                          <Badge
                            variant="outline"
                            className="w-fit gap-1 !text-[11px] font-medium"
                            title="手动添加/粘贴/订阅导入的代理"
                          >
                            <span>手动</span>
                            <span className="shrink-0 font-mono text-[10px] text-kumo-subtle">
                              {manualProxyEntries.length} 条
                            </span>
                          </Badge>
                        )}
                        <span className="text-[11px] text-kumo-subtle">
                          共 {endpointForm.proxyPool.length} 条
                        </span>
                      </div>
                    ) : (
                      <span className="text-xs text-kumo-subtle">
                        未配置代理，请求将直连上游。
                      </span>
                    )}
                  </div>
                </div>

                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="代理开关"
                    checked={!!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, proxyEnabled: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-strong">代理开关</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="限流自动切换代理"
                    checked={!!endpointForm.autoSwitch}
                    disabled={!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, autoSwitch: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-subtle">限流或连接失败自动切换代理</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="允许直连兜底"
                    checked={!!endpointForm.allowDirectFallback}
                    disabled={!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, allowDirectFallback: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-subtle">代理池异常时允许直连兜底</span>
                </div>
              </div>
            </div>

            {endpointFormError && (
                <p className="mt-4 text-sm text-kumo-danger font-semibold">{endpointFormError}</p>
              )}
            </div>

            <div className="flex shrink-0 flex-wrap items-center justify-end gap-3 border-t border-kumo-line bg-kumo-base px-6 py-4">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button size="sm" variant="primary" disabled={endpointSaving} onClick={saveEndpoint}>
                {endpointSaving ? '保存中...' : '保存端点'}
              </Button>
            </div>
        </Dialog>
      </Dialog.Root>

      {/* 1b. 出口代理池管理弹窗 */}
      <Dialog.Root open={proxyManagerOpen} onOpenChange={setProxyManagerOpen}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(38rem,calc(100vw-1rem))] !max-w-[min(38rem,calc(100vw-1rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 border-b border-kumo-line px-4 py-3 cq-sm:px-5 cq-sm:py-4">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">
              出口代理池（{endpointForm.proxyPool?.length || 0}）
            </Dialog.Title>
            <Dialog.Description className="mt-0.5 text-xs text-kumo-subtle">
              请求按池轮换出口 IP。适合 IP 敏感的源；留空则直连。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="xs"
                variant="outline"
                onClick={addEndpointProxy}
                icon={<Plus className="h-3.5 w-3.5" />}
              >
                添加代理
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={() => setProxyBatchOpen(current => !current)}
                icon={<LogList className="h-3.5 w-3.5" />}
              >
                批量添加
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={() => proxyFileInputRef.current?.click()}
                disabled={proxyImportLoading}
                icon={proxyImportLoading ? <Loader size="sm" /> : <ArrowDown className="h-3.5 w-3.5" />}
              >
                {proxyImportLoading ? '解析中...' : '导入文件'}
              </Button>
              <input
                ref={proxyFileInputRef}
                type="file"
                accept=".txt,.list,.conf,.csv,text/plain"
                className="hidden"
                onChange={e => importProxyFile(e.target.files?.[0])}
              />
              <Button
                size="xs"
                variant="outline"
                onClick={() => setSubscriptionUrlOpen(current => !current)}
                icon={<Globe className="h-3.5 w-3.5" />}
              >
                订阅链接导入
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={probeAllProxies}
                disabled={probingProxies}
                icon={probingProxies ? <Loader size="sm" /> : <Activity className="h-3.5 w-3.5" />}
                title="立即对全部出口做一次连通性探活并记录出口 IP"
              >
                {probingProxies ? '测试中...' : '批量测试'}
              </Button>
              {disabledProxyCount > 0 && (
                <Button
                  size="xs"
                  variant="secondary-destructive"
                  onClick={unbanAllProxies}
                  disabled={unbanningProxies}
                  icon={unbanningProxies ? <Loader size="sm" /> : <RefreshCw className="h-3.5 w-3.5" />}
                  title="清除全部冷却 / 429 冻结 / 坏代理沉淀，使被禁用的出口立即恢复可选"
                >
                  {unbanningProxies ? '解封中...' : `一键解封（${disabledProxyCount}）`}
                </Button>
              )}
            </div>

            {proxyBatchOpen && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                <Textarea
                  size="sm"
                  value={proxyBatchText}
                  onChange={e => setProxyBatchText(e.target.value)}
                  placeholder={'每行一个代理地址，支持 socks5://、http(s):// 或 host:port\n如：\nsocks5://user:pass@1.2.3.4:1080\nhttp://5.6.7.8:8080'}
                  spellCheck={false}
                  rows={5}
                  className="w-full font-mono text-[0.85em]"
                />
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setProxyBatchText('');
                      setProxyBatchOpen(false);
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    size="xs"
                    variant="primary"
                    onClick={saveProxyBatch}
                    icon={<Check className="h-3.5 w-3.5" />}
                  >
                    确定添加
                  </Button>
                </div>
              </div>
            )}

            {subscriptionUrlOpen && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                <Input
                  size="sm"
                  type="url"
                  value={subscriptionUrl}
                  aria-label="订阅 URL"
                  onChange={e => setSubscriptionUrl(e.target.value)}
                  placeholder="https://example.com/sub?token=xxx"
                  spellCheck={false}
                  autoComplete="off"
                  data-1p-ignore
                  className="w-full font-mono text-[0.85em]"
                />
                <p className="text-xs leading-snug text-kumo-subtle">
                  后端将拉取订阅并解析其中的 socks/http 节点，导入为出口代理。仅本机/服务器能访问的节点可用。
                </p>
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setSubscriptionUrl('');
                      setSubscriptionUrlOpen(false);
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    size="xs"
                    variant="primary"
                    onClick={resolveSubscriptionProxies}
                    disabled={proxyImportLoading}
                    icon={proxyImportLoading ? <Loader size="sm" /> : <Globe className="h-3.5 w-3.5" />}
                  >
                    {proxyImportLoading ? '解析中...' : '解析并导入'}
                  </Button>
                </div>
              </div>
            )}

            <div className="space-y-2">
              {(endpointForm.proxyBatches || []).length > 0 && (
                <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-2">
                  <div className="px-1 pt-0.5 text-xs font-semibold text-kumo-strong">
                    导入批次
                  </div>
                  {(endpointForm.proxyBatches || []).map(batch => {
                    const expanded = expandedBatchId === batch.id;
                    return (
                      <div key={batch.id} className="overflow-hidden rounded-md border border-kumo-line bg-kumo-base">
                        <div className="flex min-w-0 items-center gap-2 px-3 py-2">
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label={expanded ? '收起' : '展开'}
                            onClick={() => setExpandedBatchId(expanded ? null : batch.id)}
                            icon={<ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-xs font-semibold text-kumo-strong">{batch.name}</div>
                            <div className="truncate font-mono text-[11px] text-kumo-subtle">
                              {batch.proxies?.length || 0} 条 · {formatDateTime(batch.createdAt)}
                            </div>
                          </div>
                          <Button
                            size="sm"
                            shape="square"
                            variant="secondary-destructive"
                            aria-label={`移除批次 ${batch.name}`}
                            onClick={() => removeProxyBatch(batch)}
                            icon={<Trash className="h-3.5 w-3.5" />}
                          />
                        </div>
                        {expanded && (
                          <div className="space-y-1.5 border-t border-kumo-line p-2">
                            {(batch.proxies || []).slice(0, PROXY_PREVIEW_LIMIT).map(proxy => {
                            const disabled = disabledProxyUntil(proxy);
                            return (
                              <div key={proxy} className="flex min-w-0 items-center gap-2">
                                <span
                                  className={`min-w-0 flex-1 truncate rounded border px-2 py-1 font-mono text-[11px] ${
                                    disabled
                                      ? 'border-kumo-danger/50 bg-kumo-danger/10 text-kumo-danger'
                                      : 'border-kumo-line bg-kumo-recessed/25 text-kumo-subtle'
                                  }`}
                                  title={disabled ? `${proxy}\n${disabled.label}` : proxy}
                                >
                                  {proxy}
                                </span>
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary-destructive"
                                  aria-label="移出此条"
                                  onClick={() => removeProxyFromBatch(batch, proxy)}
                                  title="移出此条"
                                  icon={<X className="h-3 w-3" />}
                                />
                              </div>
                            );
                          })}
                            {(batch.proxies || []).length > PROXY_PREVIEW_LIMIT && (
                              <p className="px-1 text-[11px] text-kumo-subtle">
                                仅预览前 {PROXY_PREVIEW_LIMIT} 条，共 {batch.proxies.length} 条
                              </p>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            <div className="space-y-2">
              {manualProxyEntries.length > 0 && (
                <div className="flex items-center gap-2 px-1 pt-1">
                  <span className="text-xs font-semibold text-kumo-strong">
                    手动 / 未分组代理（{manualProxyEntries.length}）
                  </span>
                  <Button
                    shape="square"
                    size="xs"
                    variant="ghost"
                    aria-label={manualProxyExpanded ? '收起手动代理' : '展开手动代理'}
                    onClick={() => setManualProxyExpanded(current => !current)}
                    icon={<ChevronDown className={`h-3.5 w-3.5 transition-transform ${manualProxyExpanded ? 'rotate-180' : ''}`} />}
                  />
                </div>
              )}
              {manualProxyExpanded &&
                manualProxyEntries.slice(0, PROXY_PREVIEW_LIMIT).map(({ proxy, index }) => {
                const entry = parseProxyEntry(proxy);
                const editing = editingProxyIndex === index;
                const disabled = disabledProxyUntil(proxy);
                return (
                  <div key={`m-${index}`} className="flex min-w-0 items-center gap-2">
                    {editing ? (
                      <Input
                        size="sm"
                        type="text"
                        value={proxy}
                        aria-label="代理地址"
                        onChange={e => updateEndpointProxy(index, e.target.value)}
                        onBlur={() => setEditingProxyIndex(-1)}
                        onKeyDown={e => {
                          if (e.key === 'Enter' || e.key === 'Escape') setEditingProxyIndex(-1);
                        }}
                        spellCheck={false}
                        autoComplete="off"
                        data-1p-ignore
                        autoFocus
                        className="min-w-0 flex-1 font-mono text-[0.85em] text-kumo-strong"
                      />
                    ) : (
                      <div
                        className={`min-w-0 flex-1 cursor-pointer rounded-md border px-3 py-2 ${
                          disabled
                            ? 'border-kumo-danger/50 bg-kumo-danger/10'
                            : 'border-kumo-line bg-kumo-recessed/25'
                        }`}
                        onClick={() => setEditingProxyIndex(index)}
                        title={`${entry.full}${disabled ? `\n\n${disabled.label}` : ''}\n点击可编辑完整代理地址`}
                      >
                        {entry.label ? (
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-baseline gap-1.5">
                              <span className={`truncate text-sm font-semibold ${disabled ? 'text-kumo-danger' : 'text-kumo-strong'}`}>
                                {entry.label}
                              </span>
                              {entry.host && entry.host !== entry.label && (
                                <span className={`shrink-0 font-mono text-[11px] ${disabled ? 'text-kumo-danger/80' : 'text-kumo-subtle'}`}>
                                  {entry.host}
                                </span>
                              )}
                            </div>
                            <ProxyRuntimeMeta proxy={proxy} state={proxyRuntimeStates[proxy]} />
                          </div>
                        ) : (
                          <div className={`truncate text-sm ${disabled ? 'text-kumo-danger' : 'text-kumo-subtle'}`}>空代理（点击编辑）</div>
                        )}
                      </div>
                    )}
                    <Button
                      shape="square"
                      size="sm"
                      variant={editing ? 'primary' : 'secondary'}
                      aria-label="编辑代理"
                      onClick={() => setEditingProxyIndex(editing ? -1 : index)}
                      title={editing ? '完成编辑' : '编辑代理'}
                      icon={editing ? <Check className="h-3.5 w-3.5" /> : <Edit className="h-3.5 w-3.5" />}
                    />
                    <Button
                      shape="square"
                      size="sm"
                      variant="secondary-destructive"
                      aria-label="删除代理"
                      onClick={() => removeEndpointProxy(index)}
                      title="删除代理"
                      icon={<Trash className="h-3.5 w-3.5" />}
                    />
                  </div>
                );
              })}
              {!endpointForm.proxyPool?.length && (
                <div className="rounded-md border border-dashed border-kumo-line py-8 text-center text-xs text-kumo-subtle">
                  暂无代理。点击上方「添加代理」「导入文件」或「订阅链接导入」开始配置。
                </div>
              )}
              {manualProxyEntries.length > PROXY_PREVIEW_LIMIT && (
                <p className="text-xs text-kumo-subtle">
                  仅预览前 {PROXY_PREVIEW_LIMIT} 条，共 {manualProxyEntries.length} 条（保存后全部生效）。
                </p>
              )}
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:px-5">
            <Dialog.Close
              render={props => (
                <Button size="sm" {...props} variant="secondary">
                  完成
                </Button>
              )}
            />
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 2. Gateway Key Dialogs */}
      <Dialog.Root open={gatewayKeyDialogOpen} onOpenChange={setGatewayKeyDialogOpen}>
        <Dialog className="!w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            {editingGatewayKey ? '编辑 API 密钥' : '新建 API 密钥'}
          </Dialog.Title>

          <div className="space-y-4">
            <Input
              size="sm"
              label="名称"
              value={gatewayKeyForm.name}
              onChange={e => setGatewayKeyForm({ ...gatewayKeyForm, name: e.target.value })}
              placeholder="如：生产环境、Open WebUI"
              className="w-full text-sm text-kumo-strong"
            />

            <div className="space-y-1.5">
              <Label>
                过期时间
                <span className="font-normal text-kumo-subtle">（可选）</span>
              </Label>
              <div className="flex flex-wrap items-center gap-1.5">
                {[
                  { label: '1 天', days: 1 },
                  { label: '14 天', days: 14 },
                  { label: '30 天', days: 30 },
                  { label: '永久', days: 0 },
                ].map(preset => (
                  <Button
                    key={preset.label}
                    size="xs"
                    variant={
                      (preset.days === 0 && !gatewayKeyForm.expiresAt) ||
                      (preset.days > 0 &&
                        gatewayKeyForm.expiresAt &&
                        Math.abs(
                          new Date(gatewayKeyForm.expiresAt).getTime() -
                            (Date.now() + preset.days * 24 * 60 * 60 * 1000)
                        ) < 60 * 1000)
                        ? 'primary'
                        : 'outline'
                    }
                    onClick={() => applyGatewayKeyExpiryPreset(preset.days)}
                  >
                    {preset.label}
                  </Button>
                ))}
              </div>
              <div className="flex items-center gap-1.5">
                <Popover>
                  <Popover.Trigger
                    render={
                      <Button
                        size="sm"
                        variant="outline"
                        icon={CalendarDotsIcon}
                        className="min-w-0 flex-1 justify-start font-normal"
                      />
                    }
                  >
                    <span className="truncate">
                      {gatewayKeyForm.expiresAt
                        ? formatDateTime(gatewayKeyForm.expiresAt)
                        : '永不过期'}
                    </span>
                  </Popover.Trigger>
                  <Popover.Content className="p-3">
                    <DatePicker
                      size="sm"
                      mode="single"
                      selected={parseLocalDateTime(gatewayKeyForm.expiresAt)}
                      onChange={updateGatewayKeyExpiryDate}
                    />
                    {gatewayKeyForm.expiresAt && (
                      <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() =>
                            setGatewayKeyForm(current => ({ ...current, expiresAt: '' }))
                          }
                        >
                          清除
                        </Button>
                      </div>
                    )}
                  </Popover.Content>
                </Popover>
                <Select
                  size="sm"
                  aria-label="过期小时"
                  disabled={!gatewayKeyForm.expiresAt}
                  value={gatewayKeyForm.expiresAt.slice(11, 13)}
                  onValueChange={value => updateGatewayKeyExpiryTime('hour', value)}
                  items={GATEWAY_EXPIRY_HOURS}
                  className="w-[3.25rem] shrink-0"
                />
                <span className="shrink-0 text-sm text-kumo-subtle">:</span>
                <Select
                  size="sm"
                  aria-label="过期分钟"
                  disabled={!gatewayKeyForm.expiresAt}
                  value={gatewayKeyForm.expiresAt.slice(14, 16)}
                  onValueChange={value => updateGatewayKeyExpiryTime('minute', value)}
                  items={GATEWAY_EXPIRY_MINUTES}
                  className="w-[3.25rem] shrink-0"
                />
              </div>
            </div>

            <Collapsible.Root
              open={gatewayKeyAdvancedOpen}
              onOpenChange={setGatewayKeyAdvancedOpen}
            >
              <Collapsible.DefaultTrigger>高级过滤</Collapsible.DefaultTrigger>
              <Collapsible.DefaultPanel className="mt-2">
                <div className="space-y-4">
                  <div className="space-y-1.5">
                    <Label>
                      允许的模型（白名单）
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <MultiSelectPopover
                        triggerLabel="选择模型"
                        searchPlaceholder="搜索模型…"
                        emptyText="暂无可用模型"
                        options={allModels.map(m => ({ value: m.id, label: m.id }))}
                        selected={gatewayKeyForm.allowedModels || []}
                        onToggle={(value, checked) => toggleGatewayKeyListItem('allowedModels', value, checked)}
                        onClear={() => setGatewayKeyForm(current => ({ ...current, allowedModels: [] }))}
                      />
                      {(gatewayKeyForm.allowedModels || []).map(model => (
                        <Badge
                          key={model}
                          variant="outline"
                          className="max-w-full gap-1 font-mono !text-[11px] font-medium"
                        >
                          <span className="truncate">{model}</span>
                          <Button
                            size="xs"
                            shape="square"
                            variant="ghost"
                            aria-label={`移除 ${model}`}
                            onClick={() => removeGatewayKeyListItem('allowedModels', model)}
                            icon={<X className="h-3 w-3" />}
                          />
                        </Badge>
                      ))}
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <Label>
                      允许的端点（白名单）
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <MultiSelectPopover
                        triggerLabel="选择端点"
                        searchPlaceholder="搜索端点…"
                        emptyText="暂无可用端点"
                        options={endpoints.map(ep => ({ value: ep.id, label: ep.name || ep.id }))}
                        selected={gatewayKeyForm.allowedEndpoints || []}
                        onToggle={(value, checked) => toggleGatewayKeyListItem('allowedEndpoints', value, checked)}
                        onClear={() => setGatewayKeyForm(current => ({ ...current, allowedEndpoints: [] }))}
                      />
                      {(gatewayKeyForm.allowedEndpoints || []).map(endpointId => {
                        const endpointLabel = endpoints.find(ep => ep.id === endpointId)?.name || endpointId;
                        return (
                          <Badge
                            key={endpointId}
                            variant="outline"
                            className="max-w-full gap-1 !text-[11px] font-medium"
                          >
                            <span className="truncate">{endpointLabel}</span>
                            <Button
                              size="xs"
                              shape="square"
                              variant="ghost"
                              aria-label={`移除 ${endpointLabel}`}
                              onClick={() => removeGatewayKeyListItem('allowedEndpoints', endpointId)}
                              icon={<X className="h-3 w-3" />}
                            />
                          </Badge>
                        );
                      })}
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <Label>
                    Token 配额
                    <span className="font-normal text-kumo-subtle">（可选）</span>
                  </Label>
                    <Input
                      size="sm"
                      type="number"
                      min="0"
                      value={gatewayKeyForm.maxTokensQuota}
                      aria-label="Token 配额"
                      onChange={e =>
                        setGatewayKeyForm({ ...gatewayKeyForm, maxTokensQuota: e.target.value })
                      }
                      placeholder="0 = 不限制"
                      className="w-full font-mono text-[0.85em] text-kumo-strong"
                    />
                  </div>
                </div>
              </Collapsible.DefaultPanel>
            </Collapsible.Root>

            {gatewayKeyFormError && (
              <p className="text-sm font-semibold text-kumo-danger">{gatewayKeyFormError}</p>
            )}
            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button
                size="sm"
                variant="primary"
                disabled={gatewayKeySaving}
                onClick={saveGatewayKey}
              >
                {gatewayKeySaving ? '保存中...' : '保存密钥'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!newGatewayKey} onOpenChange={open => !open && setNewGatewayKey(null)}>
        <Dialog className="!w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            API 密钥已创建
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
            可立即复制，也可稍后从 API 密钥列表查看并复制。
          </Dialog.Description>
          <div className="space-y-4">
            <p className="text-sm font-medium text-kumo-strong">
              {newGatewayKey?.name || 'API Key'}
            </p>
            <ClipboardText
              size="sm"
              text={newGatewayKey?.apiKey || ''}
              className="min-w-0 w-full"
              tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制' }}
              labels={{ copyAction: '复制 API Key' }}
            />
            <div className="flex justify-end">
              <Dialog.Close
                render={props => (
                  <Button size="sm" variant="primary" {...props}>
                    我已保存
                  </Button>
                )}
              />
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 3. Health Check Config Dialog */}
      <Dialog.Root open={healthCheckModal} onOpenChange={setHealthCheckModal}>
        <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong mb-1">
            模型健康检测
          </Dialog.Title>
          <Dialog.Description className="text-sm text-kumo-subtle mb-4">
            按设定并发逐批发送轻量请求，测试每个模型的可用性与延迟。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="bg-kumo-warning/10 border border-kumo-warning/20 text-kumo-warning px-3 py-2 text-sm space-y-1">
              <p className="font-semibold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                警告
              </p>
              <p>批量检测会发送真实请求；并发数越高，越容易触发供应商限流、风控或短时失败。</p>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">检测方式</span>
              <StatusBadge tone="info">后端批量检测</StatusBadge>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">超时限制</span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="sm"
                  aria-label="健康检测超时限制"
                  type="number"
                  value={healthCheckForm.timeout}
                  onChange={e =>
                    setHealthCheckForm({ ...healthCheckForm, timeout: Number(e.target.value) })
                  }
                  min={1}
                  max={60}
                  className="w-16 text-kumo-strong text-sm px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">秒</span>
              </div>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">并发数</span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="sm"
                  aria-label="健康检测并发数"
                  type="number"
                  value={healthCheckForm.concurrency}
                  onChange={e =>
                    setHealthCheckForm({
                      ...healthCheckForm,
                      concurrency: Number(e.target.value),
                    })
                  }
                  min={1}
                  max={30}
                  className="w-16 text-kumo-strong text-sm px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">个请求</span>
              </div>
            </div>

            <p className="text-xs text-kumo-subtle">
              默认并发 {DEFAULT_MODEL_HEALTH_CONCURRENCY}、超时{' '}
              {DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS} 秒；批量检测全部启用端点上的模型，完成后统一回填结果。
            </p>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button
                size="sm"
                variant="primary"
                disabled={modelHealthBatchLoading}
                onClick={startBatchHealthCheck}
              >
                {modelHealthBatchLoading ? '检测中...' : '开始检测'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default OpenAIPage;
