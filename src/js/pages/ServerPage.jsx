import React, { useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback } from 'react';
import useStore from '../store.js';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Popover } from '@cloudflare/kumo/components/popover';
import { ChartLegend, ChartPalette, ClipboardText, LayerCard, Loader, Meter, Tabs, Toolbar } from '@cloudflare/kumo';
import SiteFontTimeseriesChart from '../components/SiteFontTimeseriesChart.jsx';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AnimatedCollapse, DeferredRender } from '../components/AnimatedCollapse.jsx';
import CountryFlag from '../components/CountryFlag.jsx';
import QuickCommandBar from '../components/server/QuickCommandBar.jsx';
import SftpPanel from '../components/server/SftpPanel.jsx';
import ForwardPanel from '../components/forward/ForwardPanel.jsx';
import ServerLocationMap from '../components/server/ServerLocationMap.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import {
  ChartBoundaryBox,
  ChartWarmupSkeleton,
  AppCard,
  ResponsiveSearchInput,
  ScrollableTable,
  SectionCard,
  stickyTabsBaseClass,
  TabBarOverflowActions,
} from '../components/ui/AppPrimitives.jsx';
import { PublicPageBrandIcon } from '../components/public/PublicPageIconPicker.jsx';
import { formatUptime, formatFileSize, formatDateTime, maskAddress, parseSpeed } from '../modules/utils.js';
import { FLOW_UNIT_BADGE_CLASS, getFlowUnitClassName } from '../modules/flowUnits.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { canOpenTerminal, hasSshEndpoint, isAgentServer, resolveTerminalProtocol, resolveTerminalSocketTransport } from '../modules/serverTerminal.js';
import { canOpenRemoteDesktop, remoteDesktopPath } from '../modules/remoteDesktop.js';
import { readSftpFile, writeSftpFile } from '../modules/server-sftp.js';
import { formatDockerContainerPorts } from '../modules/docker-format.js';
import { summarizeDockerContainers } from '../modules/dockerSummary.js';
import {
  formatDockerPruneResult,
  isDockerImagePruneCandidate,
  normalizeDockerTaskResult,
  summarizeDockerTaskMessage,
} from '../modules/dockerTasks.js';
import {
  buildAgentInstallCommand,
  buildAgentInstallEndpoint,
  getAgentInstallExecutionHint,
  isWindowsAgentInstallOs,
} from '../modules/agentInstall.js';
import {
  areRealtimeValuesEqual,
  mergePolledServerAccount,
  resolveServerDisplayStatus,
  mergeRealtimeDiskInfo,
  resolveServerMetricsHealth,
  resolveRealtimeMetricsCache,
  reuseRealtimeValueIfEqual,
} from '../modules/serverRealtime.js';
import {
  SERVER_CHART_HISTORY_LIMIT,
  SERVER_CHART_HISTORY_WINDOW_MS,
  SERVER_REALTIME_SAMPLE_INTERVAL_MS,
  formatSqliteUTCDateTime,
  getGpuMemPercent,
  normalizeChartMetricRecords,
  normalizeMetricRecords,
  toTimestamp,
} from '../modules/serverChartMetrics.js';
import * as echarts from 'echarts/core';
import { LineChart, MapChart, ScatterChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import io from 'socket.io-client';
import {
  Server,
  LayoutDashboard,
  Terminal as TerminalIcon,
  DesktopDisplay,
  Cloud,
  Globe,
  Activity,
  RefreshCw,
  ArrowRight,
  Box,
  Send,
  Shield,
  FolderOpen,
  HardDrive,
  Database,
  TrendingUp,
  Settings,
  Plus,
  Trash,
  Play,
  Pause,
  Square,
  Key,
  Folder,
  FileText,
  Save,
  RotateCw,
  Search,
  Copy,
  ExternalLink,
  Upload,
  Download,
  Edit,
  X,
  Reboot,
  ChevronDown,
  ChevronUp,
  Menu,
  Star,
  Shuffle
} from '../components/Icons.jsx';

echarts.use([
  LineChart,
  MapChart,
  ScatterChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

const SERVER_LIST_VIEW_STORAGE_KEY = 'server_list_view_mode_v2';
const SERVER_COMPACT_COLUMNS_STORAGE_KEY = 'server_compact_visible_columns';
const SERVER_STATUS_SYNC_INTERVAL_MS = 15000;
const SERVER_METRIC_FLUSH_DELAY_MS = 350;
const SERVER_METRIC_MIN_RENDER_INTERVAL_MS = SERVER_REALTIME_SAMPLE_INTERVAL_MS;
const SERVER_CARD_METRICS_TTL_MS = 60 * 1000;
const SERVER_NETWORK_QUALITY_TTL_MS = 2 * 60 * 1000;
const HOST_COMPACT_COLUMNS = [
  { id: 'status', label: '状态' },
  { id: 'name', label: '名称', required: true },
  { id: 'country', label: '位置' },
  { id: 'uptime', label: '在线' },
  { id: 'load', label: '负载' },
  { id: 'speed', label: '网速' },
  { id: 'traffic', label: '流量' },
  { id: 'cpu', label: 'CPU' },
  { id: 'memory', label: '内存' },
  { id: 'disk', label: '硬盘' },
  { id: 'remaining', label: '到期' },
  { id: 'quotaRemaining', label: '余量' },
  { id: 'actions', label: '', required: true },
];
const HOST_COMPACT_COLUMN_IDS = HOST_COMPACT_COLUMNS.map(column => column.id);
const HOST_COMPACT_DEFAULT_VISIBLE_COLUMNS = Array.from(new Set([
  ...HOST_COMPACT_COLUMN_IDS,
  'quotaRemaining',
]));
const HOST_COMPACT_COLUMN_WIDTHS = {
  status: 74,
  name: 112,
  country: 71,
  uptime: 71,
  load: 71,
  speed: 228,
  traffic: 228,
  cpu: 112,
  memory: 112,
  disk: 112,
  quotaRemaining: 112,
  remaining: 112,
  actions: 40,
};
const HOST_COMPACT_ADAPTIVE_COLUMNS = new Set(['cpu', 'memory', 'disk', 'remaining', 'quotaRemaining']);
const HOST_COMPACT_HEADER_BOX_CLASS = {
  status: 'w-[58px] justify-center',
  name: 'w-[96px] justify-center',
  country: 'w-[55px] justify-center',
  uptime: 'w-[55px] justify-center',
  load: 'w-[55px] justify-center',
  speed: 'w-full min-w-[208px] justify-center',
  traffic: 'w-full min-w-[208px] justify-center',
  cpu: 'w-full min-w-[96px] justify-center',
  memory: 'w-full min-w-[96px] justify-center',
  disk: 'w-full min-w-[96px] justify-center',
  quotaRemaining: 'w-full min-w-[96px] justify-center',
  remaining: 'w-full min-w-[96px] justify-center',
  actions: 'w-[34px] justify-center',
};
const COMPACT_INLINE_BOX_CLASS = 'border border-kumo-interact/70 shadow-none';
const COMPACT_INLINE_SUBBOX_CLASS = 'border border-kumo-interact/70 shadow-none';
const COMPACT_STICKY_ACTION_CLASS = 'border-l border-kumo-interact/60 before:!w-1 before:!-left-1';
const COMPACT_ACTION_BUTTON_CLASS = '!shadow-none';
const SERVER_SECTION_HEADER_CLASS = 'flex min-h-[56px] items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/20 px-4 py-3.5';
const SERVER_SECONDARY_BAR_CLASS = 'flex min-h-[46px] flex-wrap items-center gap-2 rounded-md border border-kumo-line/90 bg-kumo-base px-3 py-2 cq-lg:justify-between';
const SERVER_SECONDARY_TABS_GROUP_CLASS = 'flex min-w-0 flex-nowrap items-center gap-1.5 overflow-x-auto whitespace-nowrap p-0.5 scrollbar-thin cq-sm:gap-2';
const MANAGEMENT_CARD_ICON_CLASS = 'h-3.5 w-3.5 shrink-0 text-brand';
const SERVER_MODULE_TAB_ICON_CLASS = 'h-3.5 w-3.5 shrink-0';
const COMPACT_EXPAND_EXIT_MS = 230;
const SERVER_CHART_SERIES_DEFER_MS = 44;
const SERVER_CHART_RENDER_DEFER_MS = 88;
const SERVER_CHART_ANIMATION_MS = 90;
const SERVER_CHART_UPDATE_ANIMATION_MS = 70;
const SERVER_FAST_CHART_UPDATE_BEHAVIOR = { lazyUpdate: false };
const SERVER_NETWORK_QUALITY_REFRESH_MS = 60 * 1000;
const SERVER_NETWORK_QUALITY_CHART_UPDATE_BEHAVIOR = { lazyUpdate: true };

function ServerConnectionActions({
  remoteDesktopAvailable,
  terminalLabel,
  terminalDisabled,
  onOpenRemoteDesktop,
  onOpenTerminal,
  buttonClassName = '',
}) {
  const terminalButton = (
    <Button
      shape="square"
      size="sm"
      variant="secondary"
      className={buttonClassName}
      title={terminalDisabled ? '终端不可用' : terminalLabel}
      aria-label={terminalDisabled ? '终端不可用' : terminalLabel}
      icon={<TerminalIcon className="h-3.5 w-3.5" />}
      onClick={onOpenTerminal}
      disabled={terminalDisabled}
    />
  );

  if (!remoteDesktopAvailable) return terminalButton;

  return (
    <Popover>
      <Popover.Trigger
        render={(
          <Button
            shape="square"
            size="sm"
            variant="secondary"
            className={buttonClassName}
            title="连接操作"
            aria-label="连接操作"
            icon={<Menu className="h-3.5 w-3.5" />}
          />
        )}
      />
      <Popover.Content side="left" align="center" className="w-44 p-2" onClick={event => event.stopPropagation()}>
        <Popover.Title className="mb-2 text-xs font-semibold text-kumo-strong">连接操作</Popover.Title>
        <div className="grid gap-1">
          <Button size="sm" variant="secondary" className="w-full justify-start" icon={<DesktopDisplay className="h-3.5 w-3.5" />} onClick={onOpenRemoteDesktop}>
            远程桌面
          </Button>
          <Button size="sm" variant="secondary" className="w-full justify-start" icon={<TerminalIcon className="h-3.5 w-3.5" />} onClick={onOpenTerminal} disabled={terminalDisabled}>
            {terminalLabel}
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  );
}
const SERVER_FAST_CHART_ANIMATION_OPTIONS = {
  animation: true,
  animationDuration: SERVER_CHART_ANIMATION_MS,
  animationDurationUpdate: SERVER_CHART_UPDATE_ANIMATION_MS,
};
const SERVER_STATIC_CHART_ANIMATION_OPTIONS = {
  animation: false,
  animationDuration: 0,
  animationDurationUpdate: 0,
};

const createEmptyServerStatusPageForm = () => ({
  id: null,
  title: '',
  slug: '',
  domain: '',
  description: '',
  public: true,
  hideHosts: true,
  showTraffic: true,
  showCharts: true,
  showOnDashboard: true,
  publicIconId: '',
  cacheSeconds: 300,
  serverIds: [],
});

const normalizeServerStatusSlug = (value, fallback = 'servers') => {
  const text = String(value || fallback).trim().toLowerCase();
  const slug = text.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return slug || fallback;
};

const normalizeServerStatusDomain = (value) => (
  String(value || '')
    .trim()
    .replace(/^https?:\/\//i, '')
    .split('/')[0]
    .replace(/\/+$/g, '')
    .toLowerCase()
);

const patchFastTimeseriesAnimation = (option, animationOptions = SERVER_FAST_CHART_ANIMATION_OPTIONS) => {
  if (!option || typeof option !== 'object' || Array.isArray(option)) return option;

  const nextGrid = {
    ...(option.grid && typeof option.grid === 'object' && !Array.isArray(option.grid) ? option.grid : {}),
    top: 4,
    right: 4,
    bottom: 10,
    left: 8,
    containLabel: true,
  };
  const nextXAxis = {
    ...(option.xAxis && typeof option.xAxis === 'object' && !Array.isArray(option.xAxis) ? option.xAxis : {}),
    axisLabel: {
      ...(option.xAxis?.axisLabel && typeof option.xAxis.axisLabel === 'object' ? option.xAxis.axisLabel : {}),
      margin: 6,
      hideOverlap: true,
    },
    axisTick: {
      ...(option.xAxis?.axisTick && typeof option.xAxis.axisTick === 'object' ? option.xAxis.axisTick : {}),
      show: false,
    },
  };
  const nextYAxis = {
    ...(option.yAxis && typeof option.yAxis === 'object' && !Array.isArray(option.yAxis) ? option.yAxis : {}),
    axisLabel: {
      ...(option.yAxis?.axisLabel && typeof option.yAxis.axisLabel === 'object' ? option.yAxis.axisLabel : {}),
      margin: 6,
    },
    axisTick: {
      ...(option.yAxis?.axisTick && typeof option.yAxis.axisTick === 'object' ? option.yAxis.axisTick : {}),
      show: false,
    },
  };

  return {
    ...option,
    grid: nextGrid,
    xAxis: nextXAxis,
    yAxis: nextYAxis,
    animation: animationOptions.animation === false ? false : option.animation === false ? false : true,
    animationDuration: animationOptions.animationDuration,
    animationDurationUpdate: animationOptions.animationDurationUpdate,
    animationEasing: 'cubicOut',
    animationEasingUpdate: 'cubicOut',
  };
};

const createFastTimeseriesEcharts = (baseEcharts, animationOptions) => (
  {
    ...baseEcharts,
    init(...args) {
      const chart = baseEcharts.init(...args);
      const setOption = chart.setOption.bind(chart);
      chart.setOption = (option, ...setOptionArgs) => setOption(
        patchFastTimeseriesAnimation(option, animationOptions),
        ...setOptionArgs
      );
      return chart;
    },
  }
);

const METRICS_COLLECT_INTERVAL_TABS = [1, 2, 5, 10, 15, 30, 60].map(value => ({
  value: String(value),
  label: `${value}m`,
}));
const METRICS_RETENTION_TABS = [7, 30, 60, 90, 180].map(value => ({
  value: String(value),
  label: `${value}天`,
}));

function ServerModuleTabLabel({ icon: Icon, children, short, badge = null }) {
  return (
    <span className="inline-flex h-5 items-center gap-1.5 whitespace-nowrap">
      <Icon className={SERVER_MODULE_TAB_ICON_CLASS} />
      <span className="hidden cq-sm:inline">{children}</span>
      <span className="cq-sm:hidden">{short || children}</span>
      {badge !== null && (
        <span className="inline-flex h-4 min-w-4 items-center justify-center rounded bg-brand/10 px-1 text-[10px] font-bold leading-none text-brand">
          {badge}
        </span>
      )}
    </span>
  );
}

const getInitialServerListViewMode = () => {
  if (typeof window === 'undefined') return 'cards';
  const saved = window.localStorage.getItem(SERVER_LIST_VIEW_STORAGE_KEY);
  if (saved === 'compact' || saved === 'cards') return saved;
  return window.matchMedia?.('(min-width: 768px)').matches ? 'compact' : 'cards';
};

const getInitialCompactVisibleColumns = () => {
  if (typeof window === 'undefined') return HOST_COMPACT_DEFAULT_VISIBLE_COLUMNS;
  try {
    const saved = JSON.parse(window.localStorage.getItem(SERVER_COMPACT_COLUMNS_STORAGE_KEY) || '[]');
    const valid = Array.isArray(saved) ? saved.filter(id => HOST_COMPACT_COLUMN_IDS.includes(id)) : [];
    const required = HOST_COMPACT_COLUMNS.filter(column => column.required).map(column => column.id);
    return Array.from(new Set([
      ...required,
      ...(valid.length > 0 ? valid : HOST_COMPACT_DEFAULT_VISIBLE_COLUMNS),
      'quotaRemaining',
    ]));
  } catch (error) {
    return HOST_COMPACT_DEFAULT_VISIBLE_COLUMNS;
  }
};


const getOSIconClass = (platform) => {
  const baseClass = 'shrink-0 text-base leading-none';
  if (!platform) return `fas fa-server ${baseClass} text-kumo-subtle`;
  const p = platform.toLowerCase();
  if (p.includes('debian')) return `si si-debian si--color ${baseClass}`;
  if (p.includes('ubuntu')) return `si si-ubuntu si--color ${baseClass}`;
  if (p.includes('centos')) return `si si-centos si--color ${baseClass}`;
  if (p.includes('alpine')) return `si si-alpinelinux si--color ${baseClass}`;
  if (p.includes('redhat') || p.includes('rhel')) return `si si-redhat si--color ${baseClass}`;
  if (p.includes('fedora')) return `si si-fedora si--color ${baseClass}`;
  if (p.includes('rocky')) return `si si-rockylinux si--color ${baseClass}`;
  if (p.includes('alma')) return `si si-almalinux si--color ${baseClass}`;
  if (p.includes('arch')) return `si si-archlinux si--color ${baseClass}`;
  if (p.includes('windows')) return `fab fa-windows ${baseClass} app-os-windows`;
  if (p.includes('darwin') || p.includes('mac')) return `si si-apple si--color ${baseClass}`;
  return `si si-linux si--color ${baseClass}`;
};

function CompactMetricBarComponent({ label, value, valueClassName, barClassName, color, width = '0%' }) {
  let resolvedWidth = typeof width === 'number' ? `${width}%` : String(width);
  if (!resolvedWidth.endsWith('%') && /^\d+(\.\d+)?$/.test(resolvedWidth)) {
    resolvedWidth = `${resolvedWidth}%`;
  }

  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2 py-1 cq-sm:w-14 cq-sm:border-0 cq-sm:bg-transparent cq-sm:px-0 cq-sm:py-0">
      <div className="flex min-w-0 items-center justify-between gap-1">
        <span className="truncate">{label}</span>
        <span className={`shrink-0 font-bold ${color ? '' : valueClassName}`} style={color ? { color } : undefined}>{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className={`h-full transition-[width] duration-300 ease-out ${color ? '' : barClassName}`} style={{ width: resolvedWidth, backgroundColor: color || undefined }}></div>
      </div>
    </div>
  );
}

const CompactMetricBar = React.memo(CompactMetricBarComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.valueClassName === next.valueClassName
  && prev.barClassName === next.barClassName
  && prev.color === next.color
  && prev.width === next.width
));

function DenseUsageMeterComponent({ label, value, detail, indicatorClassName = '!bg-none !bg-brand', muted = false }) {
  const percent = clampPercent(toNumber(value, 0));
  const resolvedIndicatorClassName = muted ? '!bg-none !bg-kumo-subtle/55' : indicatorClassName;
  return (
    <div className={`relative h-8 min-w-[96px] w-full rounded-md bg-kumo-recessed/45 ${COMPACT_INLINE_BOX_CLASS}`}>
      <div className="absolute left-1.5 right-1.5 top-[3px] grid h-4 grid-cols-[minmax(0,1fr)_auto] items-center gap-1 text-[10px] leading-4 text-kumo-default">
        <span className="min-w-0 truncate text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-[11px] font-semibold leading-4 ${muted ? 'text-kumo-subtle' : 'text-kumo-default'}`}>{detail || `${Math.round(percent)}%`}</span>
      </div>
      <div className="absolute bottom-1 left-1.5 right-1.5 h-1.5 overflow-hidden rounded-full bg-kumo-fill">
        <div
          className={`absolute inset-y-0 left-0 rounded-full transition-[width] duration-300 ease-out ${resolvedIndicatorClassName}`}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

const DenseUsageMeter = React.memo(DenseUsageMeterComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.detail === next.detail
  && prev.indicatorClassName === next.indicatorClassName
  && prev.muted === next.muted
));

function DenseLifecycleMeterComponent({ lifecycle, muted = false }) {
  return (
    <DenseUsageMeter
      label="剩余"
      value={lifecycle.remainingPercent}
      detail={lifecycle.label}
      indicatorClassName={lifecycle.indicatorClassName}
      muted={muted}
    />
  );
}

const DenseLifecycleMeter = React.memo(DenseLifecycleMeterComponent, (prev, next) => (
  prev.lifecycle?.remainingPercent === next.lifecycle?.remainingPercent
  && prev.lifecycle?.label === next.lifecycle?.label
  && prev.lifecycle?.indicatorClassName === next.lifecycle?.indicatorClassName
  && prev.muted === next.muted
));

function FlowUnitBadge({ unit, muted = false }) {
  return (
    <span className={`${FLOW_UNIT_BADGE_CLASS} ${muted ? 'border-kumo-line/70 bg-kumo-recessed text-kumo-subtle' : getFlowUnitClassName(unit)}`}>
      {unit || 'B'}
    </span>
  );
}

function FlowArrow({ children, muted = false }) {
  return (
    <span className={`inline-flex h-5 w-5 items-center justify-center rounded-[4px] bg-kumo-recessed/70 text-[14px] font-bold leading-none ${muted ? 'text-kumo-subtle' : 'text-kumo-default'} ${COMPACT_INLINE_SUBBOX_CLASS}`}>
      {children}
    </span>
  );
}

const formatDenseFlowValue = (value) => {
  const numericValue = Number.parseFloat(String(value ?? '0').replace(/,/g, ''));
  return Number.isFinite(numericValue) ? numericValue.toFixed(1) : '0.0';
};

function DenseTrafficCell({ left, leftUnit, right, rightUnit, leftTitle, rightTitle, muted = false }) {
  const leftValue = formatDenseFlowValue(left);
  const rightValue = formatDenseFlowValue(right);
  return (
    <div className={`flex h-8 w-full min-w-[208px] shrink-0 items-center justify-center gap-1 overflow-hidden rounded-md bg-kumo-recessed/35 px-2 text-[14px] leading-none tabular-nums ${muted ? 'text-kumo-subtle' : 'text-kumo-strong'} ${COMPACT_INLINE_BOX_CLASS}`}>
      <span className="min-w-0 flex-1 truncate text-right" title={leftTitle || `${left}${leftUnit}`}>{leftValue}</span>
      <FlowUnitBadge unit={leftUnit} muted={muted} />
      <FlowArrow muted={muted}>&darr;</FlowArrow>
      <span aria-hidden="true" className={`-my-px mx-0.5 w-px self-stretch shrink-0 ${muted ? 'bg-kumo-line/70' : 'bg-kumo-interact/80'}`}></span>
      <FlowArrow muted={muted}>&uarr;</FlowArrow>
      <FlowUnitBadge unit={rightUnit} muted={muted} />
      <span className="min-w-0 flex-1 truncate text-left" title={rightTitle || `${right}${rightUnit}`}>{rightValue}</span>
    </div>
  );
}

function DenseDetailChip({ label, value, className = '', valueClassName = 'text-kumo-strong' }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  return (
    <div className={`flex h-7 min-w-0 items-center overflow-hidden rounded-md border border-kumo-line/60 bg-kumo-recessed/20 px-2.5 text-[11px] shadow-none ${className}`}>
      <span className="shrink-0 font-medium text-kumo-subtle">{label}</span>
      <span aria-hidden="true" className="mx-2 h-3 w-px shrink-0 bg-kumo-line/70"></span>
      <span className={`min-w-0 flex-1 truncate text-right font-semibold tabular-nums ${valueClassName}`} title={String(displayValue)}>{displayValue}</span>
    </div>
  );
}

const EXPANDED_SECTION_ACCENTS = {
  brand: 'bg-brand',
  success: 'bg-kumo-success',
  warning: 'bg-kumo-warning',
  info: 'bg-kumo-info',
  danger: 'bg-kumo-danger',
};

const EXPANDED_VALUE_TONES = {
  default: 'text-kumo-strong',
  brand: 'text-brand',
  success: 'text-kumo-success',
  warning: 'text-kumo-warning',
  info: 'text-kumo-info',
  danger: 'text-kumo-danger',
};

function ExpandedSection({ title, tone = 'brand', action, className = '', children }) {
  return (
    <AppCard as="section" padding="none" className={`min-w-0 overflow-hidden p-1.5 ${className}`}>
      <div className="mb-2 flex min-w-0 items-center justify-between gap-2">
        <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-bold text-kumo-strong">
          <span className={`h-3 w-1 shrink-0 rounded-full ${EXPANDED_SECTION_ACCENTS[tone] || EXPANDED_SECTION_ACCENTS.brand}`}></span>
          <span className="truncate">{title}</span>
        </h4>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {children}
    </AppCard>
  );
}

function ExpandedProgressMetricComponent({
  label,
  value,
  detail,
  caption,
  indicatorClassName = '!bg-none !bg-brand',
  valueClassName = 'text-kumo-strong',
  muted = false,
}) {
  const percent = clampPercent(toNumber(value, 0));
  const displayValue = detail || `${Math.round(percent)}%`;
  const resolvedIndicatorClassName = muted ? '!bg-none !bg-kumo-subtle/55' : indicatorClassName;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2.5 py-2">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <span className="text-[11px] font-medium text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-sm font-bold tabular-nums ${muted ? 'text-kumo-subtle' : valueClassName}`} title={String(displayValue)}>{displayValue}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-base">
        <div
          className={`h-full rounded-full transition-[width] duration-300 ease-out ${resolvedIndicatorClassName}`}
          style={{ width: `${percent}%` }}
        ></div>
      </div>
      {caption && (
        <div className="min-w-0 break-words text-[10px] font-medium leading-4 text-kumo-subtle" title={String(caption)}>
          {caption}
        </div>
      )}
    </div>
  );
}

const ExpandedProgressMetric = React.memo(ExpandedProgressMetricComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.detail === next.detail
  && prev.caption === next.caption
  && prev.indicatorClassName === next.indicatorClassName
  && prev.valueClassName === next.valueClassName
  && prev.muted === next.muted
));

function TrafficTotalSummary({ txTotal, rxTotal, quota, compact = false }) {
  const remainingPercent = quota ? clampPercent(100 - quota.percent) : 0;
  const unlimited = !!quota?.unlimited;

  if (compact) {
    return (
      <div className="grid min-w-0 grid-cols-2 gap-1 rounded-md bg-kumo-recessed/20 p-1 cq-sm:flex cq-sm:h-full cq-sm:flex-col cq-sm:justify-center cq-sm:gap-1">
        <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1">
          <div className="text-[10px] font-semibold leading-none text-kumo-subtle">累计上行</div>
          <div className="mt-0.5 truncate text-xs font-bold tabular-nums text-kumo-info" title={txTotal?.text || '-'}>
            {txTotal?.text || '-'}
          </div>
        </div>
        <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1">
          <div className="text-[10px] font-semibold leading-none text-kumo-subtle">累计下行</div>
          <div className="mt-0.5 truncate text-xs font-bold tabular-nums text-kumo-success" title={rxTotal?.text || '-'}>
            {rxTotal?.text || '-'}
          </div>
        </div>
        {quota && (
          <div className="col-span-2 min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1">
            <Meter
              label="剩余流量"
              value={unlimited ? 100 : remainingPercent}
              min={0}
              max={100}
              customValue={unlimited ? '∞' : `${remainingPercent.toFixed(remainingPercent >= 10 ? 0 : 1)}%`}
              className="gap-0.5 text-[10px] font-semibold leading-none text-kumo-subtle"
              trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
              indicatorClassName={`!h-full !bg-none ${unlimited ? '!bg-kumo-info' : quota.overLimit ? '!bg-kumo-danger' : quota.nearAlert ? '!bg-kumo-warning' : '!bg-kumo-info'}`}
            />
          </div>
        )}
      </div>
    );
  }

  const itemClassName = 'px-2.5 py-2';
  const valueClassName = 'text-sm';

  return (
    <div className="grid min-w-0 grid-cols-2 gap-1.5 cq-sm:grid-cols-1">
      <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 ${itemClassName}`}>
        <div className="text-[10px] font-semibold text-kumo-subtle">累计上行</div>
        <div className={`mt-0.5 truncate font-bold tabular-nums text-kumo-info ${valueClassName}`} title={txTotal?.text || '-'}>
          {txTotal?.text || '-'}
        </div>
      </div>
      <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 ${itemClassName}`}>
        <div className="text-[10px] font-semibold text-kumo-subtle">累计下行</div>
        <div className={`mt-0.5 truncate font-bold tabular-nums text-kumo-success ${valueClassName}`} title={rxTotal?.text || '-'}>
          {rxTotal?.text || '-'}
        </div>
      </div>
      {quota && (
        <div className={`col-span-2 min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 cq-sm:col-span-1 ${itemClassName}`}>
          <Meter
            label="剩余流量"
            value={unlimited ? 100 : remainingPercent}
            min={0}
            max={100}
            customValue={unlimited ? '∞' : `${remainingPercent.toFixed(remainingPercent >= 10 ? 0 : 1)}%`}
            className="gap-1 text-[10px] font-semibold text-kumo-subtle"
            trackClassName="!h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-base"
            indicatorClassName={`!h-full !bg-none ${unlimited ? '!bg-kumo-info' : quota.overLimit ? '!bg-kumo-danger' : quota.nearAlert ? '!bg-kumo-warning' : '!bg-kumo-info'}`}
          />
        </div>
      )}
    </div>
  );
}

function ExpandedInfoChipComponent({ label, value, className = '', valueClassName = 'text-kumo-strong' }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  return (
    <div className={`flex min-h-8.5 min-w-0 items-center justify-between gap-2.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-1 text-[11px] ${className}`}>
      <span className="shrink-0 font-medium text-kumo-subtle">{label}</span>
      <span className={`min-w-0 truncate text-right font-semibold tabular-nums ${valueClassName}`} title={String(displayValue)}>{displayValue}</span>
    </div>
  );
}

const ExpandedInfoChip = React.memo(ExpandedInfoChipComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.className === next.className
  && prev.valueClassName === next.valueClassName
));

const getSystemOverviewChipClassName = (kind = 'default') => {
  switch (kind) {
    case 'wide':
      return 'cq-sm:col-span-2 cq-xl:col-span-3';
    case 'medium':
      return 'cq-sm:col-span-2';
    default:
      return '';
  }
};

function ExpandedStatTileComponent({ label, value, caption, tone = 'default', className = '', captionClassName = '', inline = false }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  if (inline) {
    return (
      <div className={`flex min-w-0 items-center justify-between gap-2.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-2 ${className}`}>
        <span className="shrink-0 text-[11px] font-medium text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-sm font-bold tabular-nums ${EXPANDED_VALUE_TONES[tone] || EXPANDED_VALUE_TONES.default}`} title={String(displayValue)}>
          {displayValue}
        </span>
      </div>
    );
  }
  return (
    <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-2 ${className}`}>
      <div className="text-[10px] font-medium text-kumo-subtle">{label}</div>
      <div className={`mt-1 truncate text-sm font-bold tabular-nums ${EXPANDED_VALUE_TONES[tone] || EXPANDED_VALUE_TONES.default}`} title={String(displayValue)}>
        {displayValue}
      </div>
      {caption && (
        <div className={`mt-1 truncate text-[10px] font-medium text-kumo-subtle ${captionClassName}`} title={String(caption)}>
          {caption}
        </div>
      )}
    </div>
  );
}

const ExpandedStatTile = React.memo(ExpandedStatTileComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.caption === next.caption
  && prev.tone === next.tone
  && prev.className === next.className
  && prev.inline === next.inline
  && prev.captionClassName === next.captionClassName
));

function NetworkQualitySummaryStrip({ summary = [] }) {
  if (!summary.length) return null;

  return (
    <div
      className="grid min-w-0 gap-1"
      style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(min(12rem, 100%), 1fr))' }}
    >
      {summary.map(item => {
        const tone = getNetworkQualityTone(item);
        const avgLatency = toNumber(item.avgLatency, 0);
        const latestValue = avgLatency > 0
          ? formatLatencyValue(avgLatency)
          : '失败';
        const caption = `抖动 ${formatLatencyValue(item.jitterMs)} · 丢包 ${toNumber(item.lossRate, 0).toFixed(1)}%`;

        return (
          <div
            key={item.name}
            className="grid min-h-6 min-w-0 grid-cols-[auto_auto_minmax(0,1fr)] items-baseline gap-x-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/15 px-2 py-1 text-[10px] leading-none"
          >
            <span className="shrink-0 font-semibold text-kumo-subtle">{item.name}</span>
            <span className={`shrink-0 text-xs font-bold tabular-nums ${getNetworkQualityToneClass(tone)}`} title={`24h 平均 ${latestValue}`}>
              {latestValue}
            </span>
            <span className="min-w-0 truncate font-medium text-kumo-subtle" title={caption}>
              {caption}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function TrendSeriesLabel({ name, color }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-[11px] font-medium leading-none text-kumo-subtle">
      <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: color }}></span>
      <span className="truncate">{name}</span>
    </span>
  );
}

function ExpandedTrendChartCard({ title, tone = 'brand', legend, compact = false, className = '', children }) {
  const accentClassName = EXPANDED_SECTION_ACCENTS[tone] || EXPANDED_SECTION_ACCENTS.brand;
  const headerHeightClassName = compact ? 'min-h-2' : 'min-h-2';
  const legendGapClassName = compact ? 'gap-x-2 gap-y-0.5' : 'gap-x-2.5 gap-y-0.5';

  return (
    <ChartBoundaryBox className={`min-w-0 overflow-hidden rounded-lg border border-kumo-line/90 bg-kumo-base p-1.5 shadow-none ${compact ? 'rounded-md' : ''} h-full ${className}`}>
      {(tooltipBoundary) => (
        <div className="flex h-full min-w-0 flex-col">
          <div className={`grid min-w-0 grid-cols-[minmax(0,max-content)_minmax(0,1fr)] items-center gap-2 overflow-hidden ${headerHeightClassName}`}>
            <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-bold text-kumo-strong">
              <span className={`h-3 w-1 shrink-0 rounded-full ${accentClassName}`}></span>
              <span className="truncate">{title}</span>
            </h4>
            {legend && (
              <div className="flex min-w-0 justify-end overflow-hidden">
                <div className={`flex min-w-0 flex-wrap items-center justify-end text-[11px] leading-none ${legendGapClassName}`}>
                  {legend}
                </div>
              </div>
            )}
          </div>
          <div className="mt-1 min-w-0 shrink-0">
            {typeof children === 'function' ? children(tooltipBoundary) : children}
          </div>
        </div>
      )}
    </ChartBoundaryBox>
  );
}

const getExpandedInfoGridClassName = (dense = false) => (
  `grid min-w-0 grid-cols-1 ${dense ? 'gap-1.5' : 'gap-2'} cq-lg:grid-cols-2`
);

const getExpandedTrendGridClassName = (compact = false, dense = false) => (
  `grid min-w-0 grid-cols-1 ${compact || dense ? 'gap-1.5' : 'gap-2'} cq-lg:grid-cols-2`
);

const getExpandedCardSpanClassName = (index, total) => (
  index === total - 1 && total % 2 === 1 ? 'h-full cq-lg:col-span-2' : 'h-full'
);

function NetworkQualityPanel({
  serverName,
  quality = {},
  series = [],
  hasData = false,
  unsupported = false,
  chartHeight,
  isDarkMode,
  chartEcharts,
  isCompactViewport,
  onCollect,
  className = '',
  compact = false,
}) {
  const summary = Array.isArray(quality.summary) ? quality.summary : [];
  const networkQualityBodyHeight = chartHeight;

  return (
    <ChartBoundaryBox className={`min-w-0 overflow-hidden rounded-lg border border-kumo-line/90 bg-kumo-base p-1.5 shadow-none ${className}`}>
      {(tooltipBoundary) => (
        <div className="flex min-w-0 flex-col gap-1.5">
          <div className={`flex min-w-0 flex-wrap items-center justify-between gap-2 ${compact ? 'min-h-2' : ''}`}>
            <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-bold text-kumo-strong">
              <span className="h-3 w-1 shrink-0 rounded-full bg-brand"></span>
              <span className="truncate">网络波动 24h</span>
            </h4>
            <div className="flex shrink-0 items-center gap-2">
              {quality.updatedAt && (
                <span className="text-[10px] font-medium text-kumo-subtle">
                  {formatNetworkQualityChartTime(quality.updatedAt)} 更新
                </span>
              )}
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                title="立即采样"
                aria-label="立即采样"
                icon={<RefreshCw className="h-3.5 w-3.5" />}
                className="h-7 w-7 p-0"
                disabled={!!quality.loading}
                onClick={(event) => {
                  event.stopPropagation();
                  onCollect?.();
                }}
              />
            </div>
          </div>

          {quality.error && !unsupported && (
            <div className="rounded-md border border-kumo-warning/30 bg-kumo-warning/10 px-2 py-1.5 text-[11px] font-medium text-kumo-warning">
              {quality.error}
            </div>
          )}

          {quality.loading && !hasData ? (
            <ChartWarmupSkeleton height={networkQualityBodyHeight} bars={3} />
          ) : (
            <>
              {summary.length > 0 && (
                <NetworkQualitySummaryStrip summary={summary} />
              )}

              {hasData ? (
                <SiteFontTimeseriesChart
                  echarts={chartEcharts}
                  data={series}
                  height={chartHeight}
                  isDarkMode={isDarkMode}
                  gradient
                  loading={!!quality.loading}
                  tooltipBoundary={tooltipBoundary ?? undefined}
                  xAxisTickCount={isCompactViewport ? 3 : 6}
                  yAxisTickCount={isCompactViewport ? 3 : 4}
                  xAxisTickFormat={formatNetworkQualityChartTime}
                  yAxisTickFormat={formatLatencyAxis}
                  tooltipValueFormat={formatLatencyValue}
                  optionUpdateBehavior={SERVER_NETWORK_QUALITY_CHART_UPDATE_BEHAVIOR}
                  ariaDescription={`${serverName} 24 小时网络延迟波动`}
                />
              ) : (
                <div
                  className="flex items-center justify-center rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-3 text-center text-xs font-medium text-kumo-subtle"
                  style={{ height: networkQualityBodyHeight }}
                >
                  {unsupported ? '当前 Agent 版本暂不支持，升级后显示 24h 网络波动' : '暂无 24h 网络质量采样'}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </ChartBoundaryBox>
  );
}

function CompactExpandedRow({ open, colSpan, children }) {
  return (
    <Table.Row className="border-b border-kumo-line/80 bg-kumo-canvas/45">
      <Table.Cell colSpan={colSpan} className="!p-0">
        <AnimatedCollapse open={open} keepMounted>
          {children}
        </AnimatedCollapse>
      </Table.Cell>
    </Table.Row>
  );
}

function CompactColumnMenu({ menu, visibleColumns, onToggle, onShowAll, onClose }) {
  if (!menu.open) return null;

  return (
    <div
      className="fixed z-50 w-52 rounded-md border border-kumo-line bg-kumo-control p-2 text-xs"
      style={{ left: menu.x, top: menu.y }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
    >
      <div className="mb-2 flex items-center justify-between gap-2 border-b border-kumo-line pb-2">
        <span className="font-bold text-kumo-strong">显示列</span>
        <Button type="button" size="sm" variant="ghost" onClick={onShowAll}>
          全部
        </Button>
      </div>
      <div className="flex max-h-72 flex-col gap-1.5 overflow-y-auto pr-1">
        {HOST_COMPACT_COLUMNS.map(column => (
          <Checkbox
            key={column.id}
            label={column.label}
            checked={visibleColumns.includes(column.id)}
            disabled={column.required}
            onCheckedChange={(checked) => onToggle(column.id, Boolean(checked))}
          />
        ))}
      </div>
      <div className="mt-2 border-t border-kumo-line pt-2 text-right">
        <Button type="button" size="sm" variant="secondary" onClick={onClose}>
          关闭
        </Button>
      </div>
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

const LOCATION_COUNTRY_CODE_MAP = {
  'united states': 'us',
  usa: 'us',
  'u.s.': 'us',
  america: 'us',
  'united kingdom': 'gb',
  uk: 'gb',
  britain: 'gb',
  england: 'gb',
  germany: 'de',
  france: 'fr',
  netherlands: 'nl',
  japan: 'jp',
  singapore: 'sg',
  hongkong: 'hk',
  'hong kong': 'hk',
  china: 'cn',
  taiwan: 'tw',
  korea: 'kr',
  canada: 'ca',
  australia: 'au',
  '美国': 'us',
  '英国': 'gb',
  '德国': 'de',
  '法国': 'fr',
  '荷兰': 'nl',
  '日本': 'jp',
  '新加坡': 'sg',
  '香港': 'hk',
  '中国': 'cn',
  '台湾': 'tw',
  '韩国': 'kr',
  '加拿大': 'ca',
  '澳大利亚': 'au',
};

const inferCountryCodeFromLocation = (value) => {
  const text = String(value || '').trim();
  if (/^[a-z]{2}$/i.test(text)) return text;
  const normalized = text.toLowerCase();
  return Object.entries(LOCATION_COUNTRY_CODE_MAP).find(([name]) => normalized.includes(name))?.[1] || '';
};

const cleanCountryDisplayCode = (value) => {
  const text = String(value || '').trim();
  if (!text || text.toLowerCase() === 'auto') return '';
  return text;
};

const firstLocationText = (...values) => {
  for (const value of values) {
    const text = String(value || '').trim();
    if (text && text.toLowerCase() !== 'auto') return text;
  }
  return '';
};

const firstLocationNumber = (...values) => {
  for (const value of values) {
    if (value === null || value === undefined || value === '') continue;
    const number = Number(value);
    if (Number.isFinite(number)) return number;
  }
  return undefined;
};

const getFlagCountry = (server) => {
  const configuredCountry = cleanCountryDisplayCode(server.country);
  if (configuredCountry) {
    return configuredCountry;
  }
  return (
    cleanCountryDisplayCode(server.country_code) ||
    cleanCountryDisplayCode(server.countryCode) ||
    cleanCountryDisplayCode(server.info?.country_code) ||
    cleanCountryDisplayCode(server.info?.countryCode) ||
    inferCountryCodeFromLocation(server.resolved_country) ||
    inferCountryCodeFromLocation(server.location) ||
    inferCountryCodeFromLocation(server.info?.location) ||
    ''
  );
};

const normalizeLocationDisplayText = (value) => {
  const text = String(value || '').trim();
  if (!text) return '';
  return /^[a-z]{2,3}$/i.test(text) ? text.toUpperCase() : text;
};

const getServerLocationText = (server) => {
  return normalizeLocationDisplayText(
    cleanCountryDisplayCode(server.countryCode) ||
    cleanCountryDisplayCode(server.country_code) ||
    cleanCountryDisplayCode(server.info?.countryCode) ||
    cleanCountryDisplayCode(server.info?.country_code) ||
    getFlagCountry(server),
  );
};

const getServerLocationTitle = (server) => (
  server.location ||
  server.resolved_country ||
  server.info?.location ||
  server.region ||
  server.info?.region ||
  getServerLocationText(server)
);

const getKumoToken = (tokenName, fallback) => {
  if (typeof window === 'undefined') return fallback;
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(tokenName).trim();
  return value || fallback;
};

const getKumoTerminalTheme = () => ({
  background: getKumoToken('--app-terminal-bg', getKumoToken('--color-kumo-neutral-1000', '#050505')),
  foreground: getKumoToken('--app-terminal-fg', getKumoToken('--color-kumo-neutral-50', '#f8f8f8')),
  cursor: getKumoToken('--color-brand', 'Highlight'),
});

const toNumber = (value, fallback = 0) => {
  if (value === null || value === undefined || value === '') return fallback;
  const parsed = typeof value === 'number' ? value : parseFloat(String(value).replace(/[^\d.-]/g, ''));
  return Number.isFinite(parsed) ? parsed : fallback;
};

const DOCKER_STATS_HISTORY_LIMIT = 48;
const DOCKER_STATS_MOCK_POINTS = 24;
const SERVER_CHART_COALESCE_WINDOW_MS = 500;
const SERVER_NETWORK_QUALITY_MAX_POINTS = 240;
const SERVER_EXPAND_INTERACTION_GUARD_MS = 420;
const EMPTY_METRIC_RECORDS = Object.freeze([]);
const serverMetricsHistoryCache = new Map();
const serverMetricDisplayCache = new Map();

const isPageVisible = () => (
  typeof document === 'undefined' || document.visibilityState === 'visible'
);

const getMetricsSocketUrl = () => {
  const explicitUrl = import.meta.env?.VITE_METRICS_SOCKET_URL;
  if (explicitUrl) return explicitUrl;

  return '/metrics';
};

const areServerValuesEqual = areRealtimeValuesEqual;

const areServerSnapshotsEqual = (a, b) => {
  if (a === b) return true;
  if (!a || !b) return false;

  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  for (const key of keys) {
    if (!areServerValuesEqual(a[key], b[key])) return false;
  }
  return true;
};

const formatMetricTooltipValue = (value) => {
  const number = toNumber(value, NaN);
  return Number.isFinite(number) ? number.toFixed(1) : '-';
};

const getMetricSeries = (records, specs, options = {}) => {
  const seriesRecords = options.normalized ? records : normalizeChartMetricRecords(records);
  return specs.map(spec => ({
    name: spec.name,
    color: spec.color,
    data: seriesRecords.map(record => [record._ts, record._gap ? null : spec.value(record)]),
  }));
};

const getLatestMetricValue = (records, valueGetter, formatter = value => String(value)) => {
  const normalized = normalizeMetricRecords(records);
  if (normalized.length === 0) return '-';
  const latest = normalized[normalized.length - 1];
  return formatter(valueGetter(latest));
};

const formatChartTime = (timestamp) => {
  const d = new Date(timestamp);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`;
};

const formatCompactChartTime = (timestamp) => {
  const d = new Date(timestamp);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`;
};

const formatPercentAxis = (value) => {
  const number = toNumber(value, 0);
  return `${Number.isInteger(number) ? number : number.toFixed(1)}%`;
};

const formatCompactPercentAxis = (value) => `${Math.round(toNumber(value, 0))}%`;

const formatNumberAxis = (value) => {
  const number = toNumber(value, 0);
  return Number.isInteger(number) ? String(number) : number.toFixed(1);
};

const formatCompactNumberAxis = (value) => {
  const number = toNumber(value, 0);
  const abs = Math.abs(number);
  if (abs >= 1000) return `${(number / 1000).toFixed(abs >= 10000 ? 0 : 1)}k`;
  return `${Math.round(number)}`;
};

const formatBytesSpeed = (bytes) => {
  const value = toNumber(bytes, 0);
  if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB/s`;
  if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB/s`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB/s`;
  return `${Math.round(value)} B/s`;
};

const formatBytesValue = (bytes) => {
  const value = Math.max(0, toNumber(bytes, 0));
  if (value >= 1024 * 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024 * 1024)).toFixed(2)} TB`;
  if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${Math.round(value)} B`;
};

const formatCompactBytesSpeed = (bytes) => {
  const value = toNumber(bytes, 0);
  const abs = Math.abs(value);
  const sign = value < 0 ? '-' : '';
  if (abs >= 1024 * 1024 * 1024) return `${sign}${(abs / (1024 * 1024 * 1024)).toFixed(abs >= 10 * 1024 * 1024 * 1024 ? 0 : 1)}G/s`;
  if (abs >= 1024 * 1024) return `${sign}${(abs / (1024 * 1024)).toFixed(abs >= 10 * 1024 * 1024 ? 0 : 1)}M/s`;
  if (abs >= 1024) return `${sign}${(abs / 1024).toFixed(abs >= 10 * 1024 ? 0 : 1)}K/s`;
  return `${Math.round(value)}B/s`;
};

const clampPercent = (value) => Math.max(0, Math.min(100, value));

const firstPositiveNumber = (values = []) => {
  for (const value of values) {
    const parsed = toNumber(value, NaN);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  return 0;
};

const parseTemperatureValue = (value) => {
  if (value === null || value === undefined || value === '') return null;
  const parsed = typeof value === 'number'
    ? value
    : parseFloat(String(value).replace(/[^\d.-]/g, ''));
  if (!Number.isFinite(parsed) || parsed <= 0 || parsed > 130) return null;
  return parsed;
};

const collectTemperatureReadings = (input, parentName = '') => {
  if (input === null || input === undefined) return [];

  const entries = Array.isArray(input)
    ? input.map(sensor => ({ key: '', sensor }))
    : typeof input === 'object'
      ? Object.entries(input).map(([key, sensor]) => ({ key, sensor }))
      : [{ key: '', sensor: input }];

  const readings = [];
  const scalarKeys = new Set([
    'name',
    'Name',
    'label',
    'Label',
    'sensor',
    'Sensor',
    'type',
    'Type',
    'temperature',
    'Temperature',
    'temp',
    'Temp',
    'current',
    'Current',
    'value',
    'Value',
    'entries',
    'Sensors',
    'sensors',
    'values',
    'children',
  ]);

  for (const { key, sensor } of entries) {
    if (sensor === null || sensor === undefined) continue;

    const keyName = Number.isInteger(Number(key)) ? '' : key;
    const scopedName = [parentName, keyName].filter(Boolean).join(' ');

    if (typeof sensor !== 'object') {
      const value = parseTemperatureValue(sensor);
      if (value !== null) readings.push({ name: scopedName, value });
      continue;
    }

    const ownName = [
      scopedName,
      sensor.name ?? sensor.Name ?? sensor.label ?? sensor.Label ?? sensor.sensor ?? sensor.Sensor ?? sensor.type ?? sensor.Type,
    ].filter(Boolean).join(' ');
    const value = parseTemperatureValue(
      sensor.temperature ?? sensor.Temperature ?? sensor.temp ?? sensor.Temp ?? sensor.current ?? sensor.Current ?? sensor.value ?? sensor.Value,
    );
    if (value !== null) readings.push({ name: ownName, value });

    for (const nestedKey of ['entries', 'Sensors', 'sensors', 'values', 'children']) {
      readings.push(...collectTemperatureReadings(sensor[nestedKey], ownName));
    }

    for (const [nestedKey, nestedValue] of Object.entries(sensor)) {
      if (scalarKeys.has(nestedKey)) continue;
      if (nestedValue && typeof nestedValue === 'object') {
        readings.push(...collectTemperatureReadings(nestedValue, [ownName, nestedKey].filter(Boolean).join(' ')));
      }
    }
  }

  return readings;
};

const getCpuTemperatureRank = (name) => {
  const normalized = String(name || '').toLowerCase();
  if (/gpu|nvidia|radeon|nvme|ssd|hdd|disk|drive|battery|fan|ambient/.test(normalized)) return 0;
  if (/package|tctl|tdie|x86_pkg|cpu package/.test(normalized)) return 5;
  if (/\bcpu\b|cpu_thermal/.test(normalized)) return 4;
  if (/core\s*\d+|coretemp|k10temp/.test(normalized)) return 3;
  if (/thermal/.test(normalized)) return 1;
  return 0;
};

const getGpuTemp = (record = {}) => firstPositiveNumber([
  record.gpu_temp,
  record.gpuTemperature,
  record.gpu_temperature,
  record.gpu_temperature_celsius,
  record.gpu_temp_c,
  record.gpuTemp,
  record.gpu?.Temp,
  record.gpu?.temp,
  record.gpu?.Temperature,
  record.gpu?.temperature,
]);

const getCpuTemp = (record = {}) => {
  const explicitSources = [
    record.cpu_temp,
    record.cpuTemp,
    record.cpu_temperature,
    record.cpuTemperature,
    record.cpu_temperature_celsius,
    record.cpuTemperatureCelsius,
    record.cpu_temp_c,
    record.cpu?.Temperature,
    record.cpu?.Temp,
    record.cpu?.temp,
    record.cpu?.temperature,
  ];

  for (const source of explicitSources) {
    const explicit = parseTemperatureValue(source);
    if (explicit !== null) return explicit;
  }

  const readings = [
    ...collectTemperatureReadings(record.temperatures),
    ...collectTemperatureReadings(record.temperature_sensors),
    ...collectTemperatureReadings(record.temperatureSensors),
    ...collectTemperatureReadings(record.sensors),
    ...collectTemperatureReadings(record.thermal),
    ...collectTemperatureReadings(record.cpu?.temperatures, 'CPU'),
    ...collectTemperatureReadings(record.cpu?.temperature_sensors, 'CPU'),
    ...collectTemperatureReadings(record.cpu?.temperatureSensors, 'CPU'),
    ...collectTemperatureReadings(record.cpu?.sensors, 'CPU'),
    ...collectTemperatureReadings(record.cpu?.thermal, 'CPU'),
  ];

  const ranked = readings
    .map(reading => ({ ...reading, rank: getCpuTemperatureRank(reading.name) }))
    .filter(reading => reading.rank > 0)
    .sort((a, b) => (b.rank - a.rank) || (b.value - a.value));

  if (ranked.length > 0) return ranked[0].value;

  const usable = readings.filter(reading => getCpuTemperatureRank(reading.name) !== 0);
  return usable.length === 1 ? usable[0].value : 0;
};

const parseSpeedToBytes = (speedStr) => {
  if (!speedStr) return 0;
  const match = String(speedStr).trim().match(/^([0-9.]+)\s*([A-Za-z/]+)$/);
  if (!match) return 0;
  const val = parseFloat(match[1]);
  const unit = match[2].toLowerCase();
  if (unit.startsWith('g')) return val * 1024 * 1024 * 1024;
  if (unit.startsWith('m')) return val * 1024 * 1024;
  if (unit.startsWith('k')) return val * 1024;
  return val;
};

const parseMemoryUsagePercent = (metrics = {}, info = {}) => {
  const explicit = toNumber(metrics.mem_percent ?? metrics.mem_usage_percent, NaN);
  if (Number.isFinite(explicit)) return explicit;

  const infoUsage = toNumber(info?.memory?.Usage, NaN);
  if (Number.isFinite(infoUsage)) return infoUsage;

  const memUsage = metrics.mem_usage || metrics.mem;
  if (typeof memUsage === 'string') {
    const match = memUsage.match(/(\d+(?:\.\d+)?)\/(\d+(?:\.\d+)?)MB/i);
    if (match) {
      const used = parseFloat(match[1]);
      const total = parseFloat(match[2]);
      if (Number.isFinite(used) && Number.isFinite(total) && total > 0) {
        return (used / total) * 100;
      }
    }
  }

  return 0;
};

const parseGpuMemoryValue = (metrics = {}, index) => {
  if (index === 0 && metrics.gpu_mem_used !== undefined) return metrics.gpu_mem_used;
  if (index === 1 && metrics.gpu_mem_total !== undefined) return metrics.gpu_mem_total;

  const raw = metrics.gpu_mem;
  if (typeof raw !== 'string' || !raw.includes('/')) return 0;
  const parts = raw.replace(/MB/gi, '').split('/');
  return toNumber(parts[index], 0);
};

const getCachedServerMetricHistory = (serverId) => {
  const cached = serverMetricsHistoryCache.get(String(serverId));
  return cached && cached.length > 0 ? cached : null;
};

const mergeServerMetricHistory = (serverId, ...recordGroups) => {
  const id = String(serverId || '');
  if (!id) return [];

  let minTimestamp = 0;
  const groups = recordGroups.flatMap(group => {
    if (Array.isArray(group)) return [group];
    if (group && typeof group === 'object' && Array.isArray(group.records)) {
      minTimestamp = Math.max(minTimestamp, toTimestamp(group.minTimestamp, 0));
      return [group.records];
    }
    return [];
  });

  const combined = groups
    .flatMap(group => group)
    .map(record => {
      if (!record) return null;
      const ts = toTimestamp(record._ts || record.recorded_at || record.timestamp || record.time, NaN);
      if (!Number.isFinite(ts)) return null;
      return {
        ...record,
        _ts: ts,
        recorded_at: record.recorded_at || new Date(ts).toISOString(),
      };
    })
    .filter(Boolean)
    .filter(record => !minTimestamp || record._ts >= minTimestamp)
    .sort((a, b) => a._ts - b._ts);

  const coalesced = [];
  combined.forEach(record => {
    const last = coalesced[coalesced.length - 1];
    if (last && Math.abs(record._ts - last._ts) < SERVER_CHART_COALESCE_WINDOW_MS) {
      coalesced[coalesced.length - 1] = {
        ...last,
        ...record,
        _ts: record._ts,
        recorded_at: record.recorded_at,
      };
      return;
    }
    coalesced.push(record);
  });

  const newestTs = coalesced[coalesced.length - 1]?._ts || Date.now();
  const windowStart = newestTs - SERVER_CHART_HISTORY_WINDOW_MS;
  const trimmed = coalesced
    .filter(record => record._ts >= windowStart)
    .slice(-SERVER_CHART_HISTORY_LIMIT);
  serverMetricsHistoryCache.set(id, trimmed);
  return trimmed;
};

const buildMetricHistoryRecord = (metrics = {}, info = {}, timestamp = Date.now()) => {
  const ts = toTimestamp(timestamp, Date.now());
  const gpuMemUsed = parseGpuMemoryValue(metrics, 0);
  const gpuMemTotal = parseGpuMemoryValue(metrics, 1);
  const metricCpu = metrics.cpu && typeof metrics.cpu === 'object' ? metrics.cpu : {};
  const infoCpu = info?.cpu && typeof info.cpu === 'object' ? info.cpu : {};
  const metricGpu = metrics.gpu && typeof metrics.gpu === 'object' ? metrics.gpu : {};
  const infoGpu = info?.gpu && typeof info.gpu === 'object' ? info.gpu : {};

  return {
    recorded_at: new Date(ts).toISOString(),
    cpu_usage: toNumber(metrics.cpu_usage ?? metrics.cpu, 0),
    cpu_temp: getCpuTemp({ ...metrics, cpu: { ...infoCpu, ...metricCpu } }),
    cpu_power: toNumber(metrics.cpu_power ?? metrics.cpu_power_w ?? info?.cpu?.Power, 0),
    mem_usage: parseMemoryUsagePercent(metrics, info),
    gpu_usage: metrics.gpu_usage !== undefined
      ? toNumber(metrics.gpu_usage, 0)
      : (typeof metrics.gpu === 'number' ? metrics.gpu : (info?.gpu ? toNumber(info.gpu.Usage, 0) : null)),
    gpu_mem_percent: getGpuMemPercent({
      gpu_mem_percent: metrics.gpu_mem_percent !== undefined ? metrics.gpu_mem_percent : info?.gpu?.Percent,
      gpu_mem_used: gpuMemUsed,
      gpu_mem_total: gpuMemTotal,
    }),
    gpu_mem_used: gpuMemUsed,
    gpu_mem_total: gpuMemTotal,
    gpu_power: metrics.gpu_power !== undefined ? toNumber(metrics.gpu_power, 0) : (info?.gpu ? toNumber(info.gpu.Power, 0) : 0),
    gpu_temp: getGpuTemp({ ...metrics, gpu: { ...infoGpu, ...metricGpu } }),
    net_rx: parseSpeedToBytes(metrics.network?.rx_speed),
    net_tx: parseSpeedToBytes(metrics.network?.tx_speed),
    _ts: ts,
  };
};

const normalizeByteText = (value, fallback = '0 B') => {
  if (value === null || value === undefined) return fallback;
  const raw = String(value).trim();
  if (!raw || raw === '-' || raw.toLowerCase() === 'nan') return fallback;

  const match = raw.replace(/,/g, '').replace(/\/s$/i, '').match(/^([0-9]+(?:\.[0-9]+)?)\s*([KMGTPE]?B?)$/i);
  if (!match) return fallback;

  const amount = Number(match[1]);
  if (!Number.isFinite(amount)) return fallback;

  const unit = (match[2] || 'B').toUpperCase();
  const normalizedUnit = unit.length === 1 && unit !== 'B' ? `${unit}B` : unit;
  return `${match[1]} ${normalizedUnit}`;
};

const getByteParts = (value) => {
  const text = normalizeByteText(value);
  return {
    ...parseSpeed(text),
    text,
  };
};

const TRAFFIC_QUOTA_UNITS = {
  GB: 1024 * 1024 * 1024,
  TB: 1024 * 1024 * 1024 * 1024,
  PB: 1024 * 1024 * 1024 * 1024 * 1024,
};

const TRAFFIC_CYCLE_OPTIONS = [
  { value: 'none', label: '不重置' },
  { value: 'calendar_month', label: '自然月' },
  { value: 'monthly', label: '每月指定日' },
  { value: 'custom', label: '自定义范围' },
];

const normalizeTrafficCycleDayInput = (value) => {
  const day = Math.round(toNumber(value, 1));
  return Math.min(28, Math.max(1, day));
};

const bytesToTrafficQuotaForm = (bytes) => {
  const value = toNumber(bytes, 0);
  if (value <= 0) return { value: '', unit: 'TB' };
  const unit = value >= TRAFFIC_QUOTA_UNITS.PB
    ? 'PB'
    : value >= TRAFFIC_QUOTA_UNITS.TB
      ? 'TB'
      : 'GB';
  const amount = value / TRAFFIC_QUOTA_UNITS[unit];
  return {
    value: Number.isInteger(amount) ? String(amount) : amount.toFixed(3).replace(/0+$/, '').replace(/\.$/, ''),
    unit,
  };
};

const trafficQuotaInputToBytes = (value, unit = 'TB') => {
  const amount = toNumber(value, 0);
  if (amount <= 0) return 0;
  return Math.round(amount * (TRAFFIC_QUOTA_UNITS[unit] || TRAFFIC_QUOTA_UNITS.TB));
};

const normalizeTrafficAlertPercentInput = (value) => {
  const percent = toNumber(value, 100);
  return Math.min(100, Math.max(1, percent));
};

const getTrafficQuota = (server = {}) => {
  const network = server.info?.network || {};
  const limit = toNumber(network.traffic_limit_bytes ?? server.traffic_limit_bytes, 0);
  const rawUsed = toNumber(network.traffic_used_bytes, NaN);
  const rxBytes = toNumber(network.rx_total_bytes, NaN);
  const txBytes = toNumber(network.tx_total_bytes, NaN);
  const parsedRx = getByteParts(network.rx_total).bytes;
  const parsedTx = getByteParts(network.tx_total).bytes;
  const used = Number.isFinite(rawUsed)
    ? rawUsed
    : (Number.isFinite(rxBytes) && Number.isFinite(txBytes) ? rxBytes + txBytes : parsedRx + parsedTx);
  if (limit <= 0) {
    return {
      limit,
      used: Math.max(0, used),
      remaining: Infinity,
      percent: 0,
      barPercent: 0,
      alertPercent: 100,
      overLimit: false,
      nearAlert: false,
      alertEnabled: false,
      unlimited: true,
      usedText: network.traffic_used || formatBytesValue(used),
      limitText: '∞',
      remainingText: '无限',
    };
  }
  const percent = limit > 0 ? (Math.max(0, used) / limit) * 100 : 0;
  const alertPercent = toNumber(network.traffic_alert_percent ?? server.traffic_alert_percent, 100);
  const overLimit = percent >= 100;
  const nearAlert = percent >= Math.min(100, alertPercent);

  return {
    limit,
    used: Math.max(0, used),
    remaining: Math.max(0, limit - Math.max(0, used)),
    percent,
    barPercent: clampPercent(percent),
    alertPercent,
    overLimit,
    nearAlert,
    alertEnabled: Boolean(network.traffic_alert_enabled ?? server.traffic_alert_enabled),
    usedText: network.traffic_used || formatBytesValue(used),
    limitText: network.traffic_limit || formatBytesValue(limit),
    remainingText: formatBytesValue(Math.max(0, limit - Math.max(0, used))),
  };
};

const EMPTY_SERIES = [];
const EMPTY_SERVER_METRIC_DISPLAY = {
  records: EMPTY_METRIC_RECORDS,
  chartRecords: EMPTY_METRIC_RECORDS,
  cpuColor: '',
  memColor: '',
  cpuTempColor: '',
  gpuColor: '',
  vramColor: '',
  powerColor: '',
  gpuTempColor: '',
  diskColor: '',
  txColor: '',
  rxColor: '',
  cpuMemSeries: EMPTY_SERIES,
  gpuSeries: EMPTY_SERIES,
  netSeries: EMPTY_SERIES,
};

const getServerMetricDisplay = (serverId, metricsSource, isExpanded, isDarkMode) => {
  const id = String(serverId || '');
  const source = Array.isArray(metricsSource) ? metricsSource : EMPTY_METRIC_RECORDS;
  const cached = serverMetricDisplayCache.get(id);
  if (
    cached &&
    cached.source === source &&
    cached.isExpanded === isExpanded &&
    cached.isDarkMode === isDarkMode
  ) {
    return cached.value;
  }

  if (!isExpanded) {
    const value = {
      ...EMPTY_SERVER_METRIC_DISPLAY,
      records: normalizeMetricRecords(source),
      cpuColor: ChartPalette.semantic('Success', isDarkMode),
      memColor: ChartPalette.categorical(0, isDarkMode),
      cpuTempColor: ChartPalette.semantic('Attention', isDarkMode),
      gpuColor: ChartPalette.categorical(1, isDarkMode),
      vramColor: ChartPalette.categorical(3, isDarkMode),
      powerColor: ChartPalette.categorical(4, isDarkMode),
      gpuTempColor: ChartPalette.semantic('Attention', isDarkMode),
      diskColor: ChartPalette.semantic('Warning', isDarkMode),
      txColor: ChartPalette.categorical(0, isDarkMode),
      rxColor: ChartPalette.semantic('Success', isDarkMode),
    };
    serverMetricDisplayCache.set(id, {
      source,
      isExpanded,
      isDarkMode,
      value,
    });
    if (serverMetricDisplayCache.size > 300) {
      const firstKey = serverMetricDisplayCache.keys().next().value;
      serverMetricDisplayCache.delete(firstKey);
    }
    return value;
  }

  const records = normalizeMetricRecords(source);
  const chartRecords = isExpanded ? normalizeChartMetricRecords(records) : EMPTY_METRIC_RECORDS;
  const cpuColor = ChartPalette.semantic('Success', isDarkMode);
  const memColor = ChartPalette.categorical(0, isDarkMode);
  const cpuTempColor = ChartPalette.semantic('Attention', isDarkMode);
  const gpuColor = ChartPalette.categorical(1, isDarkMode);
  const vramColor = ChartPalette.categorical(3, isDarkMode);
  const powerColor = ChartPalette.categorical(4, isDarkMode);
  const gpuTempColor = ChartPalette.semantic('Attention', isDarkMode);
  const diskColor = ChartPalette.semantic('Warning', isDarkMode);
  const txColor = ChartPalette.categorical(0, isDarkMode);
  const rxColor = ChartPalette.semantic('Success', isDarkMode);
  const value = {
    records,
    chartRecords,
    cpuColor,
    memColor,
    cpuTempColor,
    gpuColor,
    vramColor,
    powerColor,
    gpuTempColor,
    diskColor,
    txColor,
    rxColor,
    cpuMemSeries: isExpanded ? getMetricSeries(chartRecords, [
      { name: 'CPU (%)', color: cpuColor, value: r => toNumber(r.cpu_usage, 0) },
      { name: '内存 (%)', color: memColor, value: r => toNumber(r.mem_usage, 0) },
      { name: 'CPU 温度 (°C)', color: cpuTempColor, value: getCpuTemp },
    ], { normalized: true }) : EMPTY_METRIC_RECORDS,
    gpuSeries: isExpanded ? getMetricSeries(chartRecords, [
      { name: 'GPU', color: gpuColor, value: r => toNumber(r.gpu_usage, 0) },
      { name: 'VRAM', color: vramColor, value: getGpuMemPercent },
      { name: '功耗 (W)', color: powerColor, value: r => toNumber(r.gpu_power, 0) },
      { name: 'Temp (°C)', color: gpuTempColor, value: getGpuTemp },
    ], { normalized: true }) : EMPTY_METRIC_RECORDS,
    netSeries: isExpanded ? getMetricSeries(chartRecords, [
      { name: '上传', color: txColor, value: r => toNumber(r.net_tx, 0) },
      { name: '下载', color: rxColor, value: r => toNumber(r.net_rx, 0) },
    ], { normalized: true }) : EMPTY_METRIC_RECORDS,
  };

  serverMetricDisplayCache.set(id, {
    source,
    isExpanded,
    isDarkMode,
    value,
  });

  if (serverMetricDisplayCache.size > 300) {
    const firstKey = serverMetricDisplayCache.keys().next().value;
    serverMetricDisplayCache.delete(firstKey);
  }

  return value;
};

const getTempColorClass = (temp) => {
  const value = toNumber(temp, 0);
  if (value >= 80) return 'text-kumo-danger';
  if (value >= 65) return 'text-kumo-warning';
  if (value > 0) return 'text-kumo-success';
  return 'text-kumo-subtle';
};

const getHostAddress = (server, mode = 'normal') => {
  const host = server?.host && server.host !== '0.0.0.0' ? server.host : server?.info?.ip;
  if (!host) return '-';
  const address = server?.host ? `${host}:${server.port || 22}` : host;
  return mode === 'normal' ? address : maskAddress(address, mode);
};

const getServerMonitorModeLabel = (server = {}) => {
  const transports = Array.isArray(server.terminal_transports) ? server.terminal_transports : [];
  if (
    server.agent_online === true ||
    transports.includes('agent') ||
    isAgentServer(server) ||
    server.monitor_mode === 'agent'
  ) {
    return 'Agent';
  }

  if (server.ssh_configured || transports.includes('ssh') || server.monitor_mode === 'ssh') {
    return 'SSH';
  }

  return '-';
};

const isServerOnline = (server = {}) => (
  server.status === 'online' || server.agent_online === true || server.agent_connected === true
);

const getGpuModelText = (gpu) => {
  if (!gpu) return '';
  if (typeof gpu === 'string') return gpu;
  if (Array.isArray(gpu)) {
    const names = gpu
      .map(item => {
        if (!item) return '';
        if (typeof item === 'string') return item;
        if (typeof item === 'object') return item.Model || item.model || item.name || item.Name || '';
        return '';
      })
      .filter(Boolean);
    return names.join(' / ');
  }
  if (typeof gpu === 'object') {
    return gpu.Model || gpu.model || gpu.name || gpu.Name || '';
  }
  return '';
};

const formatResponseTime = (value) => {
  const ms = toNumber(value, NaN);
  return Number.isFinite(ms) && ms > 0 ? `${Math.round(ms)}ms` : '-';
};

const formatLatencyValue = (value) => {
  const ms = toNumber(value, NaN);
  if (!Number.isFinite(ms)) return '-';
  if (ms >= 1000) return `${(ms / 1000).toFixed(ms >= 10000 ? 0 : 1)}s`;
  return `${Math.round(ms)}ms`;
};

const formatLatencyAxis = (value) => {
  const ms = toNumber(value, NaN);
  if (!Number.isFinite(ms)) return '';
  if (ms >= 1000) return `${(ms / 1000).toFixed(ms >= 10000 ? 0 : 1)}s`;
  return `${Math.round(ms)}`;
};

const formatNetworkQualityChartTime = (timestamp) => {
  const d = new Date(timestamp);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
};

const getNetworkQualityTone = (summary = {}) => {
  const lossRate = toNumber(summary.lossRate, 0);
  const jitterMs = toNumber(summary.jitterMs, 0);
  const avgLatency = toNumber(summary.avgLatency, 0);
  if (!summary.latest || summary.latest.success === false || lossRate >= 5 || avgLatency >= 600) return 'danger';
  if (lossRate >= 1 || jitterMs >= 120 || avgLatency >= 250) return 'warning';
  return 'success';
};

const getNetworkQualityToneClass = (tone) => (
  tone === 'danger'
    ? 'text-kumo-danger'
    : tone === 'warning'
      ? 'text-kumo-warning'
      : 'text-kumo-success'
);

const isNetworkQualityUnsupportedError = (message = '') => {
  const text = String(message || '');
  return text.includes('不支持的任务类型')
    || /unsupported\s+task\s+type/i.test(text)
    || /task\s+type:\s*40/i.test(text);
};

const buildNetworkQualitySeries = (quality, isDarkMode) => {
  const series = Array.isArray(quality?.series) ? quality.series : [];
  return series.map((target, index) => ({
    name: target.name,
    color: index === 0
      ? ChartPalette.categorical(1, isDarkMode)
      : index === 1
        ? ChartPalette.semantic('Success', isDarkMode)
        : index === 2
          ? ChartPalette.semantic('Warning', isDarkMode)
          : ChartPalette.categorical(index + 2, isDarkMode),
    data: (Array.isArray(target.data) ? target.data : (target.points || []))
      .map(point => {
        if (Array.isArray(point)) {
          return [
            toNumber(point[0], NaN),
            point[1] === null || point[1] === undefined ? null : toNumber(point[1], null),
          ];
        }

        if (point && typeof point === 'object' && ('timestamp' in point || 'value' in point)) {
          return [
            toNumber(point.timestamp, NaN),
            point.value === null || point.value === undefined ? null : toNumber(point.value, null),
          ];
        }

        return [
          new Date(point?.checked_at).getTime(),
          point?.latency_ms === null || point?.latency_ms === undefined ? null : toNumber(point.latency_ms, null),
        ];
      })
      .filter(point => Number.isFinite(point[0])),
  }));
};

const getPrimaryLoadValue = (load) => {
  const match = String(load || '').match(/-?\d+(?:\.\d+)?/);
  return match ? match[0] : '-';
};

const formatUptimeDaysOnly = (uptime) => {
  const formatted = formatUptime(uptime);
  if (!formatted || formatted === '-') return '-';
  const text = String(formatted);
  const match = text.match(/(\d+)\s*天/);
  if (match) return `${match[1]}天`;
  if (/(刚刚|时|分|秒)/.test(text)) return '0天';
  return text;
};

const formatDateInputValue = (value) => {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toISOString().slice(0, 10);
};

const normalizeExpiryInputValue = (value) => {
  if (!value) return null;
  const date = new Date(`${value}T23:59:59`);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
};

const normalizeStartInputValue = (value) => {
  if (!value) return null;
  const date = new Date(`${value}T00:00:00`);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
};

const formatServerRemainingTime = (expiresAt) => {
  if (!expiresAt) return '永久';
  const expiry = new Date(expiresAt).getTime();
  if (!Number.isFinite(expiry)) return '永久';
  const diff = expiry - Date.now();
  if (diff <= 0) return '已过期';

  return `${Math.max(1, Math.ceil(diff / 86400000))}天`;
};

const getServerRemainingToneClass = (expiresAt) => {
  if (!expiresAt) return 'text-kumo-success';
  const expiry = new Date(expiresAt).getTime();
  if (!Number.isFinite(expiry)) return 'text-kumo-success';
  const diff = expiry - Date.now();
  if (diff <= 0) return 'text-kumo-danger';
  if (diff <= 7 * 86400000) return 'text-kumo-warning';
  return 'text-kumo-success';
};

const DAY_MS = 86400000;

const toDateMs = (value) => {
  if (!value) return NaN;
  const time = new Date(value).getTime();
  return Number.isFinite(time) ? time : NaN;
};

const getServerLifecycle = (server = {}) => {
  const startsAt = server.starts_at || server.created_at || '';
  const expiresAt = server.expires_at || '';
  const startMs = toDateMs(startsAt);
  const expiryMs = toDateMs(expiresAt);
  const now = Date.now();

  if (!Number.isFinite(expiryMs)) {
    return {
      startsAt,
      expiresAt,
      label: '永久',
      remainingDays: null,
      remainingPercent: 100,
      elapsedPercent: 0,
      toneClass: 'text-kumo-success',
      indicatorClassName: '!bg-none !bg-kumo-success',
      expired: false,
    };
  }

  const totalMs = Number.isFinite(startMs) && expiryMs > startMs ? expiryMs - startMs : NaN;
  const remainingMs = Math.max(0, expiryMs - now);
  const remainingDays = Math.ceil(remainingMs / DAY_MS);
  const elapsedPercent = Number.isFinite(totalMs)
    ? clampPercent(((now - startMs) / totalMs) * 100)
    : (remainingMs > 0 ? 0 : 100);
  const remainingPercent = Number.isFinite(totalMs)
    ? clampPercent((remainingMs / totalMs) * 100)
    : (remainingMs > 0 ? 100 : 0);
  const toneClass = getServerRemainingToneClass(expiresAt);

  return {
    startsAt,
    expiresAt,
    label: formatServerRemainingTime(expiresAt),
    remainingDays,
    remainingPercent,
    elapsedPercent,
    toneClass,
    indicatorClassName: toneClass.includes('danger')
      ? '!bg-none !bg-kumo-danger'
      : toneClass.includes('warning')
        ? '!bg-none !bg-kumo-warning'
        : '!bg-none !bg-kumo-success',
    expired: remainingMs <= 0,
  };
};

const asArray = (value) => (Array.isArray(value) ? value : []);

const normalizeDockerOverviewServer = (server = {}) => {
  const serverId = String(server.serverId || server.id || '');
  const resources = server.resources || {};
  const docker = server.docker || {};
  const containers = asArray(resources.containers).length > 0
    ? asArray(resources.containers)
    : asArray(docker.containers);
  const images = asArray(resources.images);
  const networks = asArray(resources.networks);
  const volumes = asArray(resources.volumes);
  const stats = asArray(resources.stats);
  const composeProjects = asArray(resources.composeProjects || resources.compose?.projects);
  const hasDockerResources = containers.length > 0 || images.length > 0 || networks.length > 0 || volumes.length > 0 || stats.length > 0 || composeProjects.length > 0;

  return {
    ...server,
    id: serverId,
    name: server.serverName || server.name || '未知主机',
    docker: {
      ...docker,
      installed: !!docker.installed || hasDockerResources,
      containers,
    },
    resources: {
      ...resources,
      containers,
      images,
      networks,
      volumes,
      stats,
      composeProjects,
    },
  };
};

const getComposeProjectName = (project = {}) => (
  project.Name || project.name || project.Project || project.project || '-'
);

const normalizeDockerStringList = (value) => {
  if (Array.isArray(value)) {
    return value.map(item => String(item || '').trim()).filter(Boolean);
  }
  return String(value || '')
    .split(',')
    .map(item => item.trim())
    .filter(Boolean);
};

const getComposeConfigFileList = (project = {}) => normalizeDockerStringList(
  project.ConfigFiles || project.configFiles || project.config_files || project.config_file || project.configFile || project.Files || project.files || ''
);

const getComposeConfigFiles = (project = {}) => getComposeConfigFileList(project).join(', ');

const getComposePrimaryConfigFile = (project = {}) => getComposeConfigFileList(project)[0] || '';

const getComposeWorkingDir = (project = {}) => (
  project.WorkingDir || project.workingDir || project.working_dir || project.ProjectDir || project.projectDir || '-'
);

const getComposeStatus = (project = {}) => (
  String(project.Status || project.status || '-')
);

const getDockerImageRepository = (image = {}) => {
  const repoTags = Array.isArray(image.RepoTags || image.repoTags) ? (image.RepoTags || image.repoTags) : [];
  const firstRepoTag = repoTags.find(Boolean) || '';
  const repoFromTag = firstRepoTag && firstRepoTag !== '<none>:<none>' ? firstRepoTag.split(':').slice(0, -1).join(':') : '';
  return image.repository || image.Repository || image.repo || image.Repo || repoFromTag || '<none>';
};

const getDockerImageTag = (image = {}) => {
  const repoTags = Array.isArray(image.RepoTags || image.repoTags) ? (image.RepoTags || image.repoTags) : [];
  const firstRepoTag = repoTags.find(Boolean) || '';
  if (image.tag || image.Tag) return image.tag || image.Tag;
  if (firstRepoTag && firstRepoTag.includes(':')) return firstRepoTag.split(':').pop();
  return '-';
};

const getDockerImageId = (image = {}) => (
  image.id || image.Id || image.ID || image.imageId || image.ImageID || ''
);

const getDockerImageSize = (image = {}) => (
  image.size || image.Size || image.virtualSize || image.VirtualSize || '-'
);

const getDockerNetworkName = (network = {}) => (
  network.name || network.Name || '-'
);

const getDockerNetworkId = (network = {}) => (
  network.id || network.Id || network.ID || network.networkId || ''
);

const getDockerNetworkDriver = (network = {}) => (
  network.driver || network.Driver || '-'
);

const getDockerNetworkScope = (network = {}) => (
  network.scope || network.Scope || '-'
);

const getDockerVolumeName = (volume = {}) => (
  volume.name || volume.Name || '-'
);

const getDockerVolumeDriver = (volume = {}) => (
  volume.driver || volume.Driver || '-'
);

const getDockerVolumeScope = (volume = {}) => (
  volume.scope || volume.Scope || '-'
);

const formatComposeStatusLabel = (status = '') => {
  const normalized = String(status || '').trim();
  const lower = normalized.toLowerCase();
  const count = normalized.match(/\((\d+)\)/)?.[1];
  if (lower.includes('running')) return count ? `运行中 ${count} 个服务` : '运行中';
  if (lower.includes('exited') || lower.includes('stopped')) return count ? `已停止 ${count} 个服务` : '已停止';
  if (lower.includes('paused')) return count ? `已暂停 ${count} 个服务` : '已暂停';
  return normalized || '-';
};

const getDockerTaskStateLabel = (state = '') => {
  const normalized = String(state || '').toLowerCase();
  if (normalized === 'success' || normalized === 'succeeded') return '成功';
  if (normalized === 'failed' || normalized === 'error') return '失败';
  if (normalized === 'timeout') return '超时';
  if (normalized === 'cancelled' || normalized === 'canceled') return '已取消';
  if (normalized === 'pending' || normalized === 'queued') return '排队中';
  if (normalized === 'running' || normalized === 'processing') return '执行中';
  return state || '执行中';
};

const getDockerTaskActionLabel = (action = '') => {
  const labels = {
    'container.checkUpdates': '检测容器更新',
    'container.update': '更新容器',
    'container.start': '启动容器',
    'container.stop': '停止容器',
    'container.restart': '重启容器',
    'container.pause': '暂停容器',
    'container.unpause': '恢复容器',
    'container.delete': '删除容器',
    'compose.up': '启动 Compose 项目',
    'compose.down': '停止 Compose 项目',
    'compose.restart': '重启 Compose 项目',
    'compose.pull': '升级 Compose 项目',
    'compose.update': '更新 Compose 编排',
    'image.prune': '清理镜像',
    'image.remove': '删除镜像',
    'network.prune': '清理网络',
    'network.remove': '删除网络',
    'volume.prune': '清理存储卷',
    'volume.remove': '删除存储卷',
  };
  return labels[action] || action || '-';
};

const getDockerOverviewScope = (tab) => {
  const scopes = {
    containers: 'containers',
    compose: 'compose',
    images: 'images',
    networks: 'networks',
    volumes: 'volumes',
    stats: 'stats',
  };
  return scopes[tab] || 'containers';
};

const getDockerOverviewResourceCount = (server = {}, tab = '') => {
  const resources = server.resources || {};
  if (tab === 'compose') return asArray(resources.composeProjects).length;
  if (tab === 'images') return asArray(resources.images).length;
  if (tab === 'networks') return asArray(resources.networks).length;
  if (tab === 'volumes') return asArray(resources.volumes).length;
  if (tab === 'stats') return asArray(resources.stats).length;
  return asArray(resources.containers).length;
};

const isDockerOverviewHostVisible = (server = {}, tab = '') => (
  isServerOnline(server) && (!!server.docker?.installed || getDockerOverviewResourceCount(server, tab) > 0)
);

const isDockerMockPreviewEnabled = () => (
  typeof window !== 'undefined'
  && import.meta.env?.DEV
  && new URLSearchParams(window.location.search).has('mockDocker')
);

const DOCKER_LOG_TAIL_ITEMS = [
  { value: '100', label: '100 行' },
  { value: '200', label: '200 行' },
  { value: '500', label: '500 行' },
  { value: '1000', label: '1000 行' },
];

const createMockDockerOverview = import.meta.env.DEV ? () => {
  const servers = [
    {
      id: 'mock-hk',
      name: '香港',
      status: 'online',
      docker: { installed: true },
      resources: {
        containers: [
          { id: 'ca0c6b6aced2', name: 'siyuan1', image: 'demoshang/siyuan:latest', state: 'running', status: 'Up 3 days', ports: '-' },
          { id: '76cb1d7413f9', name: 'siyuan', image: 'demoshang/siyuan:latest', state: 'running', status: 'Up 9 days', ports: '6806:6806/tcp' },
          { id: 'd47c255e2d48', name: 'api-monitor', image: 'iwvw/api-monitor:dev', state: 'running', status: 'Up 2 hours', ports: '3000:3000/tcp' },
          { id: '3b671d99cb57', name: 'vertex2api', image: 'iwvw/vertex2api:main', state: 'running', status: 'Up 12 hours', ports: '2156:2156/tcp' },
          { id: '9561e5097b35', name: 'mongo', image: 'mongo:latest', state: 'running', status: 'Up 22 days', ports: '27017/tcp' },
          { id: 'b201e1bcdd78', name: 'openresty', image: '1panel/openresty:1.29.2.5-0-noble', state: 'running', status: 'Up 22 days', ports: '80:80/tcp, 443:443/tcp' },
          { id: '6679aee4b9d1', name: 'halowebui', image: 'ghcr.io/ztx888/halowebui:slim', state: 'running', status: 'Up 8 days', ports: '3770:8080/tcp' },
          { id: 'a2d735c4b8f0', name: 'wordai', image: 'iwvw/wordai:latest', state: 'running', status: 'Up 5 days', ports: '8380:80/tcp' },
          { id: 'a8c2ff1900df', name: 'uptime-kuma', image: 'louislam/uptime-kuma:2', state: 'running', status: 'Up 17 days', ports: '3001:3001/tcp' },
          { id: 'c782442c1f12', name: 'redis', image: 'redis:alpine', state: 'running', status: 'Up 17 days', ports: '6379/tcp' },
          { id: 'ea19ab776a20', name: 'mx-server', image: 'innei/mx-server:11.0.5', state: 'running', status: 'Up 4 days', ports: '2333:2333/tcp' },
          { id: '93ab2010df58', name: 'shiro', image: 'innei/shiroi:latest', state: 'paused', status: 'Paused', ports: '2323:2323/tcp' },
        ],
        images: [
          { id: 'sha256:api', repository: 'iwvw/api-monitor', tag: 'dev', size: '218MB', created: '2026-07-04' },
          { id: 'sha256:redis', repository: 'redis', tag: 'alpine', size: '42MB', created: '2026-06-29' },
        ],
        networks: [
          { id: 'net-front', name: 'frontend', driver: 'bridge', scope: 'local' },
          { id: 'net-db', name: 'database', driver: 'bridge', scope: 'local' },
        ],
        volumes: [
          { name: 'mongo_data', driver: 'local', mountpoint: '/var/lib/docker/volumes/mongo_data/_data' },
          { name: 'kuma_data', driver: 'local', mountpoint: '/var/lib/docker/volumes/kuma_data/_data' },
        ],
        stats: [
          { container_id: 'd47c255e2d48', name: 'api-monitor', cpu_percent: '1.8%', mem_usage: '122MiB / 1.9GiB', mem_percent: '6.2%', net_io: '12MB / 8MB', block_io: '55MB / 4MB' },
          { container_id: '9561e5097b35', name: 'mongo', cpu_percent: '0.7%', mem_usage: '346MiB / 1.9GiB', mem_percent: '17.8%', net_io: '4MB / 5MB', block_io: '210MB / 40MB' },
        ],
        composeProjects: [
          { Name: 'edge-stack', Status: 'running(4)', ConfigFiles: '/srv/edge/docker-compose.yml', WorkingDir: '/srv/edge' },
        ],
      },
    },
    {
      id: 'mock-sg',
      name: '新加坡',
      status: 'online',
      docker: { installed: true },
      resources: {
        containers: [
          { id: 'f1c1f2a10111', name: 'gateway', image: 'nginx:1.27-alpine', state: 'running', status: 'Up 4 days', ports: '8080:80/tcp' },
          { id: 'f1c1f2a10222', name: 'worker', image: 'iwvw/worker:main', state: 'running', status: 'Up 2 days', ports: '-' },
          { id: 'f1c1f2a10333', name: 'archive-db', image: 'postgres:16-alpine', state: 'exited', status: 'Exited (0) 3 hours ago', ports: '5432/tcp' },
        ],
        images: [
          { id: 'sha256:nginx-sg', repository: 'nginx', tag: '1.27-alpine', size: '74MB', created: '2026-07-02' },
          { id: 'sha256:worker-sg', repository: 'iwvw/worker', tag: 'main', size: '156MB', created: '2026-07-03' },
        ],
        networks: [
          { id: 'net-edge-sg', name: 'edge', driver: 'bridge', scope: 'local' },
        ],
        volumes: [
          { name: 'archive_pgdata', driver: 'local', mountpoint: '/var/lib/docker/volumes/archive_pgdata/_data' },
        ],
        stats: [
          { container_id: 'f1c1f2a10111', name: 'gateway', cpu_percent: '0.9%', mem_usage: '86MiB / 1.9GiB', mem_percent: '4.4%', net_io: '6MB / 9MB', block_io: '18MB / 7MB' },
          { container_id: 'f1c1f2a10222', name: 'worker', cpu_percent: '1.4%', mem_usage: '214MiB / 1.9GiB', mem_percent: '11.1%', net_io: '8MB / 5MB', block_io: '72MB / 13MB' },
        ],
        composeProjects: [],
      },
    },
  ];

  const updateChecks = [
    { serverId: 'mock-hk', containerId: 'd47c255e2d48', containerName: 'api-monitor', image: 'iwvw/api-monitor:dev', currentDigest: 'sha256:local-api', latestDigest: 'sha256:remote-api', hasUpdate: true },
    { serverId: 'mock-hk', containerId: '9561e5097b35', containerName: 'mongo', image: 'mongo:latest', currentDigest: 'sha256:local-mongo', latestDigest: 'sha256:remote-mongo', hasUpdate: true },
    { serverId: 'mock-hk', containerId: 'ca0c6b6aced2', containerName: 'siyuan1', image: 'demoshang/siyuan:latest', currentDigest: 'sha256:siyuan', latestDigest: 'sha256:siyuan', hasUpdate: false },
    { serverId: 'mock-sg', containerId: 'f1c1f2a10222', containerName: 'worker', image: 'iwvw/worker:main', currentDigest: 'sha256:local-worker', latestDigest: 'sha256:remote-worker', hasUpdate: true },
  ];

  return {
    servers: servers.map(normalizeDockerOverviewServer),
    updateChecks,
  };
} : () => ({ servers: [], updateChecks: [] });

const hasServerDockerInstalled = (server = {}) => {
	const docker = server.info?.docker || {};
	const containers = asArray(docker.containers);
	return isServerOnline(server) && (
    !!docker.installed
    || containers.length > 0
    || toNumber(docker.runningCount ?? docker.running, 0) > 0
    || toNumber(docker.stoppedCount ?? docker.stopped, 0) > 0
  );
};

const getDockerStatName = (stat = {}) => (
  stat.name || stat.Name || stat.container_name || stat.container || stat.container_id || stat.ID || '-'
);

const getDockerStatCpuPercent = (stat = {}) => clampPercent(toNumber(stat.cpu_percent ?? stat.CPUPerc, 0));

const getDockerStatMemPercent = (stat = {}) => clampPercent(toNumber(stat.mem_percent ?? stat.MemPerc, 0));

const getDockerStatMemUsage = (stat = {}) => stat.mem_usage || stat.MemUsage || '-';

const getDockerStatNetIo = (stat = {}) => stat.net_io || stat.NetIO || '-';

const getDockerStatBlockIo = (stat = {}) => stat.block_io || stat.BlockIO || '-';

const parseDockerByteValue = (value) => {
  if (value === null || value === undefined || value === '') return 0;
  if (typeof value === 'number') return Number.isFinite(value) ? value : 0;

  const text = String(value).trim();
  const match = text.match(/(-?\d+(?:\.\d+)?)\s*([kmgtp]?i?b)?/i);
  if (!match) return toNumber(text, 0);

  const number = Number.parseFloat(match[1]);
  if (!Number.isFinite(number)) return 0;
  const unit = String(match[2] || '').toLowerCase();
  const multipliers = {
    b: 1,
    kb: 1000,
    mb: 1000 ** 2,
    gb: 1000 ** 3,
    tb: 1000 ** 4,
    kib: 1024,
    mib: 1024 ** 2,
    gib: 1024 ** 3,
    tib: 1024 ** 4,
  };
  return number * (multipliers[unit] || 1);
};

const parseDockerIoPair = (value) => {
  const [first = '', second = ''] = String(value || '').split('/');
  return [parseDockerByteValue(first), parseDockerByteValue(second)];
};

const getDockerStatKey = (stat = {}) => (
  String(stat.serverId || '') + ':' + String(stat.container_id || stat.ID || stat.id || getDockerStatName(stat))
);

const normalizeDockerStatSnapshot = (stat = {}, index = 0, timestamp = Date.now()) => {
  const [netIn, netOut] = parseDockerIoPair(getDockerStatNetIo(stat));
  const [blockRead, blockWrite] = parseDockerIoPair(getDockerStatBlockIo(stat));
  return {
    key: getDockerStatKey(stat) || `docker-stat-${index}`,
    name: getDockerStatName(stat),
    serverId: String(stat.serverId || ''),
    serverName: stat.serverName || '',
    timestamp,
    cpu: getDockerStatCpuPercent(stat),
    memory: getDockerStatMemPercent(stat),
    netIn,
    netOut,
    blockRead,
    blockWrite,
  };
};

const createDockerStatsSeedHistory = (stats = [], timestamp = Date.now()) => {
  const interval = 30 * 1000;
  return Array.from({ length: DOCKER_STATS_MOCK_POINTS }, (_, pointIndex) => {
    const pointTime = timestamp - (DOCKER_STATS_MOCK_POINTS - 1 - pointIndex) * interval;
    const progress = pointIndex / Math.max(1, DOCKER_STATS_MOCK_POINTS - 1);
    return stats.map((stat, statIndex) => {
      const base = normalizeDockerStatSnapshot(stat, statIndex, pointTime);
      const wave = Math.sin((pointIndex + statIndex) * 0.75) * 0.18;
      const pulse = pointIndex % 9 === statIndex % 5 ? 0.22 : 0;
      const factor = Math.max(0.2, 0.82 + wave + pulse + progress * 0.08);
      return {
        ...base,
        cpu: clampPercent(base.cpu * factor),
        memory: clampPercent(base.memory * (0.96 + Math.sin(pointIndex / 5 + statIndex) * 0.025)),
        netIn: base.netIn * factor,
        netOut: base.netOut * Math.max(0.2, 0.9 + Math.cos((pointIndex + statIndex) * 0.6) * 0.16),
        blockRead: base.blockRead * Math.max(0.2, 0.88 + Math.sin((pointIndex + statIndex) * 0.35) * 0.12),
        blockWrite: base.blockWrite * Math.max(0.2, 0.86 + Math.cos((pointIndex + statIndex) * 0.4) * 0.14),
      };
    });
  });
};

const buildDockerStatSeries = (history = [], metric, isDarkMode) => {
  const seriesMap = new Map();
  history.forEach(snapshot => {
    snapshot.forEach((item, index) => {
      if (!seriesMap.has(item.key)) {
        seriesMap.set(item.key, {
          name: item.name,
          color: ChartPalette.categorical(index, isDarkMode),
          data: [],
        });
      }
      seriesMap.get(item.key).data.push([item.timestamp, item[metric] ?? 0]);
    });
  });
  return Array.from(seriesMap.values()).filter(series => series.data.some(([, value]) => toNumber(value, 0) > 0));
};

const buildDockerPairSeries = (history = [], metrics) => metrics.map(metric => ({
  name: metric.name,
  color: metric.color,
  data: history.map(snapshot => {
    const total = snapshot.reduce((sum, item) => sum + toNumber(item[metric.key], 0), 0);
    return [snapshot[0]?.timestamp || Date.now(), total];
  }),
})).filter(series => series.data.some(([, value]) => value > 0));


function ServerPage() {
  const {
    setMainActiveTab,
    theme,
    publicApiUrl,
    serverIpDisplayMode: storedServerIpDisplayMode,
    agentDownloadUrl: storedAgentDownloadUrl,
  } = useStore();
  const { isArmed, confirmPress } = useConfirmPress();
  const isCompactViewport = useMediaQuery('(max-width: 640px)');
  const isDenseViewport = useMediaQuery('(max-width: 1120px)');
  const expandedMainChartHeight = isCompactViewport ? 88 : isDenseViewport ? 100 : 112;
  const expandedSubChartHeight = isCompactViewport ? 72 : isDenseViewport ? 80 : 88;
  const expandedTrendChartHeight = isCompactViewport ? 96 : isDenseViewport ? 108 : 120;
  const compactExpandedChartHeight = isCompactViewport ? 96 : isDenseViewport ? 112 : 122;
  const compactNetworkQualityChartHeight = isCompactViewport ? 104 : isDenseViewport ? 118 : 130;
  const networkQualityChartHeight = isCompactViewport ? 108 : isDenseViewport ? 132 : 148;
  const expandedChartXAxisTickCount = isCompactViewport ? 3 : isDenseViewport ? 4 : 5;
  const expandedChartYAxisTickCount = isCompactViewport ? 3 : 4;
  const compactExpandedYAxisTickCount = 3;
  const expandedChartXAxisTickFormat = isCompactViewport ? formatCompactChartTime : formatChartTime;
  const expandedPercentAxisTickFormat = isCompactViewport ? formatCompactPercentAxis : formatPercentAxis;
  const expandedNumberAxisTickFormat = isCompactViewport ? formatCompactNumberAxis : formatNumberAxis;
  const expandedSpeedAxisTickFormat = isCompactViewport ? formatCompactBytesSpeed : formatBytesSpeed;
  const fastTimeseriesEcharts = useMemo(() => createFastTimeseriesEcharts(echarts), []);
  const staticTimeseriesEcharts = useMemo(() => createFastTimeseriesEcharts(echarts, SERVER_STATIC_CHART_ANIMATION_OPTIONS), []);


  const [serverCurrentTab, setServerCurrentTab] = useState('list'); // 'list', 'status-pages', 'docker', 'management', 'terminal', 'forward'
  const forwardPanelRef = useRef(null);

  // 主机列表状态
  const [serverList, setServerList] = useState([]);
  const [serverLoading, setServerLoading] = useState(false);
  const [serverStatusPages, setServerStatusPages] = useState([]);
  const [serverStatusPagesLoading, setServerStatusPagesLoading] = useState(false);
  const [serverStatusPageForm, setServerStatusPageForm] = useState(() => createEmptyServerStatusPageForm());
  const [serverSearchText, setServerSearchText] = useState('');
  const [serverStatusFilter, setServerStatusFilter] = useState('all');
  const [serverListViewMode, setServerListViewMode] = useState(getInitialServerListViewMode);
  const [serverMapOpen, setServerMapOpen] = useState(false);
  const [compactVisibleColumns, setCompactVisibleColumns] = useState(getInitialCompactVisibleColumns);
  const [compactColumnMenu, setCompactColumnMenu] = useState({ open: false, x: 0, y: 0 });
  const [expandedServers, setExpandedServers] = useState([]);
  const [renderedCompactExpandedServers, setRenderedCompactExpandedServers] = useState([]);
  const [chartSeriesReadyServers, setChartSeriesReadyServers] = useState([]);
  const [expandedDockerPanels, setExpandedDockerPanels] = useState([]);
  const [draggedServerId, setDraggedServerId] = useState(null);

  // 凭据状态
  const [serverCredentials, setServerCredentials] = useState([]);

  // 拨测目标状态
  const [networkTargets, setNetworkTargets] = useState([]);
  const [showNetworkTargetModal, setShowNetworkTargetModal] = useState(false);
  const [networkTargetModalMode, setNetworkTargetModalMode] = useState('add'); // 'add' | 'edit'
  const [networkTargetForm, setNetworkTargetForm] = useState({
    id: null,
    name: '',
    host: '',
    port: 80,
    type: 'tcp',
    enabled: true,
    order_index: 0,
  });

  // 各种模态框控制
  const [showServerModal, setShowServerModal] = useState(false);
  const [serverModalMode, setServerModalMode] = useState('add'); // 'add' | 'edit'
  const [serverForm, setServerForm] = useState({
    id: null,
    name: '',
    host: '',
    port: 22,
    username: '',
    authType: 'password',
    password: '',
    privateKey: '',
    passphrase: '',
    tagsInput: '',
    description: '',
    startsAt: '',
    expiresAt: '',
    trafficLimitValue: '',
    trafficLimitUnit: 'TB',
    trafficLimitMode: 'total',
    trafficAlertEnabled: false,
    trafficAlertPercent: 100,
    trafficCycleType: 'none',
    trafficCycleDay: 1,
    trafficCycleStart: '',
    trafficCycleEnd: '',
    monitorMode: 'agent'
  });
  const [selectedCredentialId, setSelectedCredentialId] = useState('');
  const [serverModalSaving, setServerModalSaving] = useState(false);
  const [serverModalError, setServerModalError] = useState('');


  const [serverAddMode, setServerAddMode] = useState('ssh'); // 'ssh' | 'agent'
  const [quickDeployName, setQuickDeployName] = useState('');
  const [quickDeployResult, setQuickDeployResult] = useState(null);
  const [agentInstallOS, setAgentInstallOS] = useState('linux');
  const [showAgentModal, setShowAgentModal] = useState(false);
  const [agentModalData, setAgentModalData] = useState(null);
  const [agentForceSsh, setAgentForceSsh] = useState(false);
  const [agentInstalling, setAgentInstalling] = useState(false);
  const [agentInstallLoading, setAgentInstallLoading] = useState(false);
  const [agentInstallLog, setAgentInstallLog] = useState('');
  const [agentInstallResult, setAgentInstallResult] = useState(null);
  const [showBatchAgentModal, setShowBatchAgentModal] = useState(false);
  const [selectedBatchServers, setSelectedBatchServers] = useState([]);
  const [batchInstallResults, setBatchInstallResults] = useState([]);
  const [batchAgentForceSsh, setBatchAgentForceSsh] = useState(false);

  // 导入/导出
  const [showImportServerModal, setShowImportServerModal] = useState(false);
  const [importPreview, setImportPreview] = useState(null);
  const [importModalError, setImportModalError] = useState('');
  const [importModalSaving, setImportModalSaving] = useState(false);
  const [serverBatchText, setServerBatchText] = useState('');
  const [serverBatchError, setServerBatchError] = useState('');
  const [serverBatchSuccess, setServerBatchSuccess] = useState('');
  const [serverAddingBatch, setServerAddingBatch] = useState(false);
  const [serverIpDisplayMode, setServerIpDisplayMode] = useState(storedServerIpDisplayMode || 'normal'); // 'normal', 'masked', 'hidden'
  const [serverSettingsSaving, setServerSettingsSaving] = useState(false);
  const [serverSettingsForm, setServerSettingsForm] = useState({
    agentDownloadUrl: storedAgentDownloadUrl || '',
  });

  const [networkQualityByServer, setNetworkQualityByServer] = useState({});

  // Docker 状态
  const [dockerOverviewServers, setDockerOverviewServers] = useState([]);
  const [dockerOverviewLoading, setDockerOverviewLoading] = useState(false);
  const [dockerSubTab, setDockerSubTab] = useState('containers'); // 'containers', 'compose', 'images', 'networks', 'volumes', 'stats'
  const [dockerViewMode, setDockerViewMode] = useState('table'); // 'table', 'grid'
  const [dockerSearchQuery, setDockerSearchQuery] = useState('');
  const [dockerContainerStateFilter, setDockerContainerStateFilter] = useState('all');
  const [dockerSelectedServer, setDockerSelectedServer] = useState('');
  const [expandedDockerOverviewServers, setExpandedDockerOverviewServers] = useState(null);
  // 未手动展开/收起过任何主机区块（null = 自动模式）时，默认展开每个列表的第一台主机。
  const isDockerHostExpanded = useCallback((serverId, firstVisibleServerId) => (
    expandedDockerOverviewServers === null
      ? firstVisibleServerId === serverId
      : expandedDockerOverviewServers.includes(serverId)
  ), [expandedDockerOverviewServers]);
  const [showDockerTaskDetails, setShowDockerTaskDetails] = useState(false);
  const [showDockerLogPanel, setShowDockerLogPanel] = useState(false);
  const [dockerTasks, setDockerTasks] = useState([]);
  const [dockerTaskStreamConnected, setDockerTaskStreamConnected] = useState(false);
  const [dockerTaskStreamError, setDockerTaskStreamError] = useState('');
  const [dockerResourceLoading, setDockerResourceLoading] = useState(false);
  const [dockerImages, setDockerImages] = useState([]);
  const [dockerNetworks, setDockerNetworks] = useState([]);
  const [dockerVolumes, setDockerVolumes] = useState([]);
  const [dockerStats, setDockerStats] = useState([]);
  const [dockerStatsHistory, setDockerStatsHistory] = useState([]);
  const [dockerComposeProjects, setDockerComposeProjects] = useState([]);
  const [dockerUpdateChecks, setDockerUpdateChecks] = useState({});
  const [dockerUpdateCheckLoading, setDockerUpdateCheckLoading] = useState({});
  const [dockerActionPending, setDockerActionPending] = useState({});
  const [dockerSelectedContainerKeys, setDockerSelectedContainerKeys] = useState([]);
  const [dockerBulkUpdateChecking, setDockerBulkUpdateChecking] = useState(false);
  const [dockerBulkUpdateCheckServers, setDockerBulkUpdateCheckServers] = useState({});
  const [showDockerCreateModal, setShowDockerCreateModal] = useState(false);
  const [dockerLogsModalOpen, setDockerLogsModalOpen] = useState(false);
  const [dockerLogsContent, setDockerLogsContent] = useState('');
  const [dockerLogsLoading, setDockerLogsLoading] = useState(false);
  const [dockerLogsContainer, setDockerLogsContainer] = useState(null);
  const [dockerLogsServer, setDockerLogsServer] = useState(null);
  const [dockerLogsTail, setDockerLogsTail] = useState(200);
  const [dockerComposeEditor, setDockerComposeEditor] = useState(null);

  useEffect(() => {
    if (dockerStats.length === 0) {
      setDockerStatsHistory([]);
      return;
    }

    const timestamp = Date.now();
    const nextSnapshot = dockerStats.map((stat, index) => normalizeDockerStatSnapshot(stat, index, timestamp));
    setDockerStatsHistory(prev => {
      if (prev.length === 0 && isDockerMockPreviewEnabled()) {
        return createDockerStatsSeedHistory(dockerStats, timestamp);
      }
      return [...prev, nextSnapshot].slice(-DOCKER_STATS_HISTORY_LIMIT);
    });
  }, [dockerStats]);

  // Agent 升级
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeLog, setUpgradeLog] = useState('');
  const [upgradeBatchSnapshot, setUpgradeBatchSnapshot] = useState(null);
  const [upgradeProgress, setUpgradeProgress] = useState(0);
  const [upgradeFallbackSsh, setUpgradeFallbackSsh] = useState(false);

  // SSH 终端会话
  const [sshSessions, setSshSessions] = useState([]);
  const [activeSSHSessionId, setActiveSSHSessionId] = useState('');
  const [visibleSessionIds, setVisibleSessionIds] = useState([]);
  const [sshViewLayout, setSshViewLayout] = useState('single'); // 'single', 'split-h', 'split-v', 'grid-v', 'grid'
  const [sshSplitSide, setSshSplitSide] = useState('');
  const [sshGroupState, setSshGroupState] = useState(null);
  const [sshSyncEnabled, setSshSyncEnabled] = useState(false);
  const [draggedSessionId, setDraggedSessionId] = useState(null);
  const [dropHint, setDropHint] = useState('');
  const [dropTargetId, setDropTargetId] = useState(null);


  const [activeTerminalSidebar, setActiveTerminalSidebar] = useState(null);
  const [sshIdeFullscreen, setSshIdeFullscreen] = useState(false);

  // SFTP 状态
  const [sftpCurrentPath, setSftpCurrentPath] = useState('/');
  const [sftpServerId, setSftpServerId] = useState('');

  // 凭据编辑/新增
  const [showAddCredentialModal, setShowAddCredentialModal] = useState(false);
  const [credForm, setCredForm] = useState({
    name: '',
    username: 'root',
    auth_type: 'password',
    password: '',
    private_key: '',
    passphrase: ''
  });

  // 终端持久化实例仓库与 WebSocket 连接引用
  const terminalDOMElements = useRef({});
  const sshSessionRefs = useRef({});
  const warehouseRef = useRef(null);
  const serverModalPortalRef = useRef(null);
  const credentialModalPortalRef = useRef(null);
  const dockerTaskStreamRef = useRef(null);
  const dockerRefreshTimerRef = useRef(null);
  const dockerTaskMetaRef = useRef({});
  const terminalResizeTimers = useRef({});
  const upgradeLogViewportRef = useRef(null);
  const upgradeLogLineKeysRef = useRef(new Set());
  const socketRef = useRef(null);
  const pendingMetricUpdatesRef = useRef([]);
  const metricFlushTimerRef = useRef(null);
  const cardMetricsRequestRef = useRef(new Map());
  const cardMetricsLoadedAtRef = useRef(new Map());
  const networkQualityLoadedAtRef = useRef(new Map());
  const serverListSyncInFlightRef = useRef(false);
  const networkQualityRequestRef = useRef(new Set());
  const serverStatusByIdRef = useRef(new Map());
  const expandedServersRef = useRef([]);
  const expandInteractionUntilRef = useRef(new Map());
  const visibleSessionIdsRef = useRef([]);
  const activeTerminalStatusServerIdRef = useRef('');
  const sshSyncEnabledRef = useRef(false);
  const sftpPathByServerRef = useRef({});

  const showServerStatusSidebar = activeTerminalSidebar === 'status';
  const showSftpSidebar = activeTerminalSidebar === 'sftp';
  const showCommandSidebar = activeTerminalSidebar === 'commands';

  useEffect(() => {
    setServerIpDisplayMode(storedServerIpDisplayMode || 'normal');
  }, [storedServerIpDisplayMode]);

  useEffect(() => {
    setServerSettingsForm(prev => ({
      ...prev,
      agentDownloadUrl: storedAgentDownloadUrl || '',
    }));
  }, [storedAgentDownloadUrl]);

  const getSettingsAuthHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
  }), []);

  const handleServerIpDisplayModeChange = useCallback((value) => {
    setServerIpDisplayMode(String(value));
  }, []);

  const saveServerModuleSettings = useCallback(async () => {
    const patch = {
      serverIpDisplayMode,
      agentDownloadUrl: String(serverSettingsForm.agentDownloadUrl || '').trim(),
    };
    setServerSettingsSaving(true);
    try {
      const response = await fetch('/api/settings', {
        method: 'PATCH',
        headers: getSettingsAuthHeaders(),
        body: JSON.stringify(patch),
      });
      const result = await response.json();
      if (!response.ok || !result.success) {
        throw new Error(result.error || '保存主机偏好失败');
      }
      useStore.setState(patch);
      toast.success('主机偏好已保存');
    } catch (error) {
      toast.error(error.message || '保存主机偏好失败');
    } finally {
      setServerSettingsSaving(false);
    }
  }, [getSettingsAuthHeaders, serverIpDisplayMode, serverSettingsForm.agentDownloadUrl]);

  useEffect(() => {
    visibleSessionIdsRef.current = visibleSessionIds;
  }, [visibleSessionIds]);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(SERVER_LIST_VIEW_STORAGE_KEY, serverListViewMode);
    }
  }, [serverListViewMode]);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(SERVER_COMPACT_COLUMNS_STORAGE_KEY, JSON.stringify(compactVisibleColumns));
    }
  }, [compactVisibleColumns]);

  useEffect(() => {
    expandedServersRef.current = expandedServers;
    setRenderedCompactExpandedServers(prev => Array.from(new Set([...prev, ...expandedServers])));

    const timeout = setTimeout(() => {
      setRenderedCompactExpandedServers(prev => prev.filter(id => expandedServers.includes(id)));
    }, COMPACT_EXPAND_EXIT_MS);

    return () => clearTimeout(timeout);
  }, [expandedServers]);

  useEffect(() => {
    if (expandInteractionUntilRef.current.size === 0) return undefined;
    const timer = window.setInterval(() => {
      const now = Date.now();
      let changed = false;
      expandInteractionUntilRef.current.forEach((until, serverId) => {
        if (until > now) return;
        expandInteractionUntilRef.current.delete(serverId);
        changed = true;
      });
      if (!changed) return;
    }, 250);

    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    setChartSeriesReadyServers(prev => prev.filter(id => expandedServers.includes(id)));
    if (expandedServers.length === 0) return undefined;

    const timeout = setTimeout(() => {
      setChartSeriesReadyServers(prev => (
        Array.from(new Set([
          ...prev.filter(id => expandedServers.includes(id)),
          ...expandedServers,
        ]))
      ));
    }, SERVER_CHART_SERIES_DEFER_MS);

    return () => clearTimeout(timeout);
  }, [expandedServers]);

  useEffect(() => {
    if (!compactColumnMenu.open) return undefined;
    const closeMenu = () => setCompactColumnMenu(prev => ({ ...prev, open: false }));
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') closeMenu();
    };
    window.addEventListener('click', closeMenu);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('click', closeMenu);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [compactColumnMenu.open]);

  useEffect(() => {
    const timer = setTimeout(() => syncTerminalDOM(), 80);
    return () => clearTimeout(timer);
  }, [visibleSessionIds, sshViewLayout, showServerStatusSidebar, showSftpSidebar]);

  useLayoutEffect(() => {
    if (serverCurrentTab !== 'terminal') return undefined;

    visibleSessionIdsRef.current = visibleSessionIds;
    const restore = () => restoreVisibleTerminalSurfaces({ focus: true });
    restore();

    if (typeof window === 'undefined') return undefined;
    let frameOne = 0;
    let frameTwo = 0;
    const timerOne = window.setTimeout(restore, 50);
    const timerTwo = window.setTimeout(restore, 180);
    frameOne = window.requestAnimationFrame(() => {
      restore();
      frameTwo = window.requestAnimationFrame(restore);
    });

    return () => {
      window.clearTimeout(timerOne);
      window.clearTimeout(timerTwo);
      if (frameOne) window.cancelAnimationFrame(frameOne);
      if (frameTwo) window.cancelAnimationFrame(frameTwo);
    };
  }, [serverCurrentTab, visibleSessionIds, sshViewLayout, activeTerminalSidebar]);

  useEffect(() => {
    if (serverCurrentTab === 'terminal') return undefined;
    const timer = setTimeout(() => saveTerminalsToWarehouse(), 0);
    return () => clearTimeout(timer);
  }, [serverCurrentTab]);

  useEffect(() => {
    if (showSftpSidebar && activeSSHSessionId) {
      syncSftpToSession(activeSSHSessionId);
    }
  }, [activeSSHSessionId, showSftpSidebar]);

  useEffect(() => {
    sshSyncEnabledRef.current = sshSyncEnabled;
  }, [sshSyncEnabled]);

  useEffect(() => {
    const el = upgradeLogViewportRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [upgradeLog]);

  useEffect(() => {
    const activeSession = sshSessions.find(session => session.id === activeSSHSessionId);
    activeTerminalStatusServerIdRef.current = (
      serverCurrentTab === 'terminal' && showServerStatusSidebar && activeSession?.server?.id
        ? String(activeSession.server.id)
        : ''
    );
  }, [activeSSHSessionId, serverCurrentTab, showServerStatusSidebar, sshSessions]);

  // 终端持久化实例仓库与 WebSocket 连接引用

  useEffect(() => {
    loadServerList();
    loadCredentials();
    if (isDockerMockPreviewEnabled()) {
      setServerCurrentTab('docker');
      setDockerSubTab('containers');
    }
    const connectTimer = setTimeout(() => {
      connectMetricsStream();
    }, 0);
    const serverListSyncTimer = setInterval(() => {
      if (document.hidden) return;
      loadServerList({ silent: true });
    }, SERVER_STATUS_SYNC_INTERVAL_MS);

    // 每 1500 毫秒同步刷新一次累积的指标更新，保证所有主机行同步更新，避免刷新参差不齐
    metricFlushTimerRef.current = setInterval(() => {
      if (pendingMetricUpdatesRef.current.length > 0) {
        const queued = pendingMetricUpdatesRef.current;
        pendingMetricUpdatesRef.current = [];
        handleMetricUpdateBatch(queued);
      }
    }, 1500);

    return () => {

      clearTimeout(connectTimer);
      clearInterval(serverListSyncTimer);
      if (socketRef.current) {
        socketRef.current.removeAllListeners();
        socketRef.current.disconnect();
        socketRef.current = null;
      }
      if (dockerTaskStreamRef.current) {
        dockerTaskStreamRef.current.close();
      }
      if (dockerRefreshTimerRef.current) {
        clearTimeout(dockerRefreshTimerRef.current);
        dockerRefreshTimerRef.current = null;
      }
      if (metricFlushTimerRef.current) {
        clearInterval(metricFlushTimerRef.current);
        metricFlushTimerRef.current = null;
      }
      pendingMetricUpdatesRef.current = [];
      Object.values(terminalResizeTimers.current).forEach(timer => clearTimeout(timer));
      terminalResizeTimers.current = {};

      Object.keys(sshSessionRefs.current).forEach(id => {
        const session = sshSessionRefs.current[id];
        if (session.heartbeatInterval) clearInterval(session.heartbeatInterval);
        if (session.inputDisposable) session.inputDisposable.dispose();
        if (session.ws) session.ws.close();
        if (session.resizeObserver) session.resizeObserver.disconnect();
        if (session.terminal) session.terminal.dispose();
      });
    };
  }, []);


  useEffect(() => {
    useStore.setState({ serverList });
  }, [serverList]);

  useEffect(() => {
    serverStatusByIdRef.current = new Map(serverList.map(server => [String(server.id), server.status]));
  }, [serverList]);

  const dockerHostOptions = useMemo(() => (
    serverList
      .filter(hasServerDockerInstalled)
      .map(server => ({ value: String(server.id), label: server.name || server.id }))
  ), [serverList]);
  const dockerHostOptionKey = useMemo(() => dockerHostOptions.map(item => item.value).join('|'), [dockerHostOptions]);

  useEffect(() => {
    if (!dockerSelectedServer) return;
    const stillAvailable = dockerHostOptions.some(item => item.value === dockerSelectedServer);
    if (!stillAvailable) {
      setDockerSelectedServer('');
    }
  }, [dockerHostOptions, dockerSelectedServer]);

  const mergeTerminalCapabilities = (server, agentOnline) => {
    const hasSshTransport =
      server.ssh_configured === true ||
      server.supports_ssh === true ||
      server.terminal_transports?.includes('ssh') ||
      hasSshEndpoint(server);
    const terminalTransports = agentOnline ? ['agent'] : [];
    if (hasSshTransport) terminalTransports.push('ssh');

    return {
      agent_online: agentOnline,
      terminal_transports: terminalTransports,
      preferred_terminal_transport: terminalTransports[0] || null,
    };
  };

  // 载入主机列表
  const loadServerList = async (options = {}) => {
    const { silent = false } = options;
    if (silent && serverListSyncInFlightRef.current) return;

    if (silent) serverListSyncInFlightRef.current = true;
    if (!silent) setServerLoading(true);
    try {
      const response = await fetch('/api/server/accounts', { cache: 'no-store' });
      const data = await response.json();
      if (data.success) {
        const accounts = Array.isArray(data.data) ? data.data : [];
        setServerList(prev => {
          const prevMap = new Map(prev.map(s => [s.id, s]));
          let changed = prev.length !== accounts.length;
          const updated = accounts.map(server => {
            const existing = prevMap.get(server.id);
            const cachedMetrics = existing?.metricsCache || getCachedServerMetricHistory(server.id);
            const next = mergePolledServerAccount(existing, server, {
              silent,
              cachedMetrics: cachedMetrics || null,
            });
            if (existing && areServerSnapshotsEqual(existing, next)) {
              return existing;
            }
            changed = true;
            return next;
          });
          if (!changed) return prev;
          return updated;
        });
        const onlineServerIds = accounts
				.filter(server => isServerOnline(server) || server.is_online === true)
          .map(server => server.id)
          .filter(Boolean);
        if (onlineServerIds.length > 0) {
          const prefetch = () => {
            onlineServerIds.forEach(serverId => {
              loadCardMetrics(serverId, { silent: true }).catch(error => {
                console.error('[Server] Failed to prefetch metric history:', error);
              });
            });
          };
          if (typeof window !== 'undefined' && typeof window.requestIdleCallback === 'function') {
            window.requestIdleCallback(prefetch, { timeout: 1500 });
          } else {
            setTimeout(prefetch, 0);
          }
        }
      }
    } catch (error) {
      console.error('加载主机列表失败:', error);
      if (!silent) toast.error('加载主机列表失败');
    } finally {
      if (silent) serverListSyncInFlightRef.current = false;
      if (!silent) setServerLoading(false);
    }
  };

  const refreshServerLocationsAndList = async () => {
    setServerLoading(true);
    try {
      const response = await fetch('/api/server/accounts/refresh-locations', {
        method: 'POST',
        cache: 'no-store',
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.success === false) {
        throw new Error(data.error || data.message || '刷新地理位置失败');
      }
      await loadServerList({ silent: true });
      const updated = Number(data.data?.updated ?? data.updated ?? 0);
      toast.success(updated > 0 ? `已刷新 ${updated} 台主机地理位置` : '地理位置已是最新缓存');
    } catch (error) {
      console.error('刷新地理位置失败:', error);
      toast.error(error.message || '刷新地理位置失败');
    } finally {
      setServerLoading(false);
    }
  };

  const loadServerStatusPages = async () => {
    setServerStatusPagesLoading(true);
    try {
      const response = await fetch('/api/server/status-pages');
      const data = await response.json();
      if (data.success && Array.isArray(data.data)) {
        setServerStatusPages(data.data);
      }
    } catch (error) {
      toast.error('载入主机状态页失败');
    } finally {
      setServerStatusPagesLoading(false);
    }
  };

  const getServerStatusPageBaseOrigin = () => {
    const configured = String(publicApiUrl || '').trim().replace(/\/+$/g, '');
    return configured || window.location.origin;
  };

  const getServerStatusPageUrl = (pageOrForm, mode = 'servers') => {
    const slug = normalizeServerStatusSlug(pageOrForm?.slug || pageOrForm?.title || 'servers');
    return `${getServerStatusPageBaseOrigin()}/${mode}/${encodeURIComponent(slug)}`;
  };

  const getServerStatusDomainUrl = (pageOrForm) => {
    const domain = normalizeServerStatusDomain(pageOrForm?.domain);
    return domain ? `https://${domain}` : '';
  };

  const copyServerStatusUrl = async (value) => {
    if (!value) {
      toast.warning('没有可复制的地址');
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      toast.success('公开地址已复制');
    } catch (error) {
      toast.error('复制失败');
    }
  };

  const resetServerStatusPageForm = () => setServerStatusPageForm(createEmptyServerStatusPageForm());

  const editServerStatusPage = (page) => {
    const config = page.config || {};
    setServerStatusPageForm({
      id: page.id,
      title: page.title || '',
      slug: page.slug || '',
      domain: page.domain || '',
      description: page.description || '',
      public: page.public !== false,
      hideHosts: config.hideHosts !== false,
      showTraffic: config.showTraffic !== false,
      showCharts: config.showCharts !== false,
      showOnDashboard: !!config.showOnDashboard,
      publicIconId: String(config.publicIconId || '').trim(),
      cacheSeconds: page.cacheSeconds || 300,
      serverIds: Array.isArray(page.serverIds) ? page.serverIds : [],
    });
  };

  const toggleServerStatusPageServer = (serverId, checked) => {
    setServerStatusPageForm(prev => {
      const ids = new Set(prev.serverIds);
      if (checked) ids.add(serverId);
      else ids.delete(serverId);
      return { ...prev, serverIds: Array.from(ids) };
    });
  };

  const saveServerStatusPage = async () => {
    const title = serverStatusPageForm.title.trim();
    if (!title) {
      toast.warning('请填写状态页名称');
      return;
    }
    if (serverStatusPageForm.serverIds.length === 0) {
      toast.warning('请至少绑定一台主机');
      return;
    }
    setServerStatusPagesLoading(true);
    try {
      const payload = {
        title,
        slug: normalizeServerStatusSlug(serverStatusPageForm.slug || title),
        domain: normalizeServerStatusDomain(serverStatusPageForm.domain),
        description: serverStatusPageForm.description.trim(),
        public: !!serverStatusPageForm.public,
        cacheSeconds: Math.max(30, Number(serverStatusPageForm.cacheSeconds) || 300),
        serverIds: serverStatusPageForm.serverIds,
        config: {
          hideHosts: !!serverStatusPageForm.hideHosts,
          showTraffic: !!serverStatusPageForm.showTraffic,
          showCharts: !!serverStatusPageForm.showCharts,
          showOnDashboard: !!serverStatusPageForm.showOnDashboard,
          ...(serverStatusPageForm.publicIconId ? { publicIconId: serverStatusPageForm.publicIconId } : {}),
        },
      };
      const isEdit = !!serverStatusPageForm.id;
      const response = await fetch(isEdit ? `/api/server/status-pages/${serverStatusPageForm.id}` : '/api/server/status-pages', {
        method: isEdit ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await response.json();
      if (!response.ok || data.success === false) throw new Error(data.error || '保存主机状态页失败');
      toast.success(isEdit ? '主机状态页已更新' : '主机状态页已创建');
      resetServerStatusPageForm();
      await loadServerStatusPages();
    } catch (error) {
      toast.error(error.message || '保存主机状态页失败');
    } finally {
      setServerStatusPagesLoading(false);
    }
  };

  const deleteServerStatusPage = async (page) => {
    if (!confirmPress(`status-page.delete::${page.id}`, `删除主机状态页「${page.title || page.slug}」`)) return;
    setServerStatusPagesLoading(true);
    try {
      const response = await fetch(`/api/server/status-pages/${page.id}`, { method: 'DELETE' });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.success === false) throw new Error(data.error || '删除主机状态页失败');
      toast.success('主机状态页已删除');
      if (serverStatusPageForm.id === page.id) resetServerStatusPageForm();
      await loadServerStatusPages();
    } catch (error) {
      toast.error(error.message || '删除主机状态页失败');
    } finally {
      setServerStatusPagesLoading(false);
    }
  };

  // 载入预设凭据
  const loadCredentials = async () => {
    try {
      const response = await fetch('/api/server/credentials');
      const data = await response.json();
      if (data.success) {
        setServerCredentials(data.data || []);
      }
    } catch (e) {
      console.error('加载凭据失败:', e);
    }
  };

  const loadNetworkTargets = async () => {
    try {
      const response = await fetch('/api/server/network-quality/targets');
      const data = await response.json();
      if (data.success) {
        setNetworkTargets(data.data || []);
      }
    } catch (e) {
      console.error('加载拨测目标失败:', e);
      toast.show({ message: '加载拨测目标失败', variant: 'error' });
    }
  };

  const saveNetworkTarget = async (e) => {
    e?.preventDefault();
    try {
      const url = networkTargetModalMode === 'add'
        ? '/api/server/network-quality/targets'
        : `/api/server/network-quality/targets/${networkTargetForm.id}`;
      const method = networkTargetModalMode === 'add' ? 'POST' : 'PUT';
      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(networkTargetForm),
      });
      const data = await response.json();
      if (data.success) {
        toast.show({ message: networkTargetModalMode === 'add' ? '新增成功' : '保存成功', variant: 'success' });
        setShowNetworkTargetModal(false);
        loadNetworkTargets();
      } else {
        toast.show({ message: data.message || '操作失败', variant: 'error' });
      }
    } catch (err) {
      console.error('保存拨测目标失败:', err);
      toast.show({ message: '保存拨测目标失败', variant: 'error' });
    }
  };

  const deleteNetworkTarget = async (id) => {
    if (!confirmPress(`network-target.delete::${id}`, '删除网络拨测目标')) {
      return;
    }
    try {
      const response = await fetch(`/api/server/network-quality/targets/${id}`, { method: 'DELETE' });
      const data = await response.json();
      if (data.success) {
        toast.show({ message: '删除成功', variant: 'success' });
        loadNetworkTargets();
      } else {
        toast.show({ message: data.message || '删除失败', variant: 'error' });
      }
    } catch (err) {
      console.error('删除拨测目标失败:', err);
      toast.show({ message: '删除拨测目标失败', variant: 'error' });
    }
  };

  const toggleNetworkTargetEnabled = async (target) => {
    try {
      const updated = {
        name: target.name,
        host: target.host,
        port: target.port,
        type: target.type,
        enabled: !target.enabled,
        order_index: target.order_index ?? 0,
      };
      const response = await fetch(`/api/server/network-quality/targets/${target.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updated),
      });
      const data = await response.json();
      if (data.success) {
        loadNetworkTargets();
      } else {
        toast.show({ message: data.message || '操作失败', variant: 'error' });
      }
    } catch (err) {
      console.error('更新目标状态失败:', err);
      toast.show({ message: '更新目标状态失败', variant: 'error' });
    }
  };

  useEffect(() => {
    if (serverCurrentTab === 'management') {
      loadNetworkTargets();
    }
  }, [serverCurrentTab]);

  const applyServerStatusSnapshot = (items = []) => {
    if (!Array.isArray(items) || items.length === 0) return;

    const statusMap = new Map();
    items.forEach(item => {
      const id = item?.serverId ?? item?.id;
      if (id !== undefined && id !== null) {
        statusMap.set(String(id), item);
      }
    });

    if (statusMap.size === 0) return;

    setServerList(prev => {
      let changed = false;
      const updated = prev.map(server => {
        const item = statusMap.get(String(server.id));
        if (!item) return server;

        const rawStatus = item.status || (item.agent_online === true ? 'online' : 'offline');
        const status = rawStatus === 'suspect' ? 'interrupted' : rawStatus;
        const agentOnline = typeof item.agent_online === 'boolean'
          ? item.agent_online
          : status === 'online';
        const hasResponseTime =
          Object.prototype.hasOwnProperty.call(item, 'responseTime') ||
          Object.prototype.hasOwnProperty.call(item, 'response_time');
        const responseTime = Object.prototype.hasOwnProperty.call(item, 'responseTime')
          ? item.responseTime
          : item.response_time;

        const next = {
          ...server,
          ...mergeTerminalCapabilities(server, agentOnline),
          status,
          metrics_health: item.metrics_health || server.metrics_health,
          metrics_stale: item.metrics_stale ?? server.metrics_stale ?? false,
          metrics_last_seen: item.metrics_last_seen || item.metricsLastSeen || server.metrics_last_seen,
          metrics_last_seen_at: item.metrics_last_seen_at || item.metricsLastSeenAt || server.metrics_last_seen_at || 0,
          metrics_age_ms: item.metrics_age_ms ?? item.metricsAgeMs ?? server.metrics_age_ms ?? 0,
          response_time: hasResponseTime ? responseTime : server.response_time,
          error: status === 'offline' ? (item.error || null) : null,
          last_seen: item.lastSeen || item.last_seen || server.last_seen,
        };
        if (areServerSnapshotsEqual(server, next)) {
          return server;
        }
        changed = true;
        return next;
      });
      if (!changed) return prev;
      return updated;
    });
  };


  const connectMetricsStream = () => {
    try {
      const socket = io(getMetricsSocketUrl(), {
        reconnection: true,
        reconnectionDelay: 1000,
        reconnectionDelayMax: 5000,
        reconnectionAttempts: Infinity,
        transports: ['websocket', 'polling']
      });

      socket.on('metrics:update', data => {
        if (data && data.serverId && data.metrics) {
          enqueueMetricUpdates([data]);
        }
      });

      socket.on('metrics:batch', dataArray => {
        if (Array.isArray(dataArray)) {
          enqueueMetricUpdates(dataArray);
        }
      });

      socket.on('server:status', data => {
        if (data && data.serverId) {
          applyServerStatusSnapshot([data]);
        }
      });

      socket.on('server:list', data => {
        applyServerStatusSnapshot(data);
      });

      socketRef.current = socket;
    } catch (err) {
      console.error('[Metrics] Socket.IO 初始化失败:', err);
    }
  };

  const enqueueMetricUpdates = (updates = []) => {
    const validUpdates = updates.filter(data => data && data.serverId && data.metrics);
    if (validUpdates.length === 0) return;

    pendingMetricUpdatesRef.current.push(...validUpdates);
  };

  // 处理实时推送的主机指标
  const handleMetricUpdateBatch = (updates = []) => {
    const latestByServer = new Map();
    updates.forEach(update => {
      if (!update?.serverId || !update?.metrics) return;
      const key = String(update.serverId);
      const current = latestByServer.get(key);
      const ts = toTimestamp(update.timestamp, Date.now());
      if (!current || ts >= (current.timestamp || 0)) {
        latestByServer.set(key, { ...update, timestamp: ts });
      }
    });
    if (latestByServer.size === 0) return;

    if (!isPageVisible()) {
      latestByServer.forEach(data => {
        const serverId = data.serverId;
        const now = data.timestamp || Date.now();
        const historyRecord = buildMetricHistoryRecord(data.metrics, null, now);
        mergeServerMetricHistory(
          serverId,
          getCachedServerMetricHistory(serverId) || [],
          [historyRecord]
        );
      });
      return;
    }

    setServerList(prev => {
      let changed = false;
      const updated = prev.map(server => {
        const data = latestByServer.get(String(server.id));
        if (!data) return server;

        const { serverId, metrics, timestamp } = data;
        const now = timestamp || Date.now();
        const historyRecord = buildMetricHistoryRecord(metrics, server.info || null, now);
        mergeServerMetricHistory(
          serverId,
          getCachedServerMetricHistory(serverId) || [],
          [historyRecord]
        );


        const lastUpdate = server.lastMetricUpdateTime || 0;
        const isTerminalStatusVisible = activeTerminalStatusServerIdRef.current === String(server.id);
        const isExpanded = expandedServersRef.current.includes(server.id) || isTerminalStatusVisible;
        const interactionGuardUntil = expandInteractionUntilRef.current.get(String(server.id)) || 0;
        const inExpandInteractionGuard = interactionGuardUntil > now;
        if (lastUpdate > 0 && (now - lastUpdate) < SERVER_METRIC_MIN_RENDER_INTERVAL_MS) {
          if (!isExpanded) return server;
          if (!isTerminalStatusVisible && (now - lastUpdate) < SERVER_REALTIME_SAMPLE_INTERVAL_MS) return server;
        }

        const existingCache = server.metricsCache || getCachedServerMetricHistory(serverId) || [];
        const cache = mergeServerMetricHistory(
          serverId,
          getCachedServerMetricHistory(serverId) || [],
          existingCache,
          [historyRecord]
        );

        const previousInfo = server.info || {
          cpu: { Load: '-', Usage: '0%', Cores: '-' },
          memory: { Used: '-', Total: '-', Usage: '0%' },
          disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
          network: { connections: 0, rx_speed: '0 B/s', tx_speed: '0 B/s', rx_total: '0 B', tx_total: '0 B' },
          docker: { installed: false, containers: [] }
        };
        const info = { ...previousInfo };

        // CPU
        const logicalCores = parseInt(metrics.logical_cores) || parseInt(metrics.cores) || parseInt(previousInfo.cpu?.LogicalCores) || parseInt(previousInfo.cpu?.Cores) || 0;
        const physicalCores = parseInt(metrics.physical_cores) || parseInt(previousInfo.cpu?.PhysicalCores) || logicalCores || 0;
        const metricCpu = metrics.cpu && typeof metrics.cpu === 'object' ? metrics.cpu : {};
        const existingCpu = previousInfo.cpu && typeof previousInfo.cpu === 'object' ? previousInfo.cpu : {};
        const resolvedCpuTemp = getCpuTemp({ ...metrics, cpu: { ...existingCpu, ...metricCpu } });
        info.cpu = reuseRealtimeValueIfEqual(previousInfo.cpu, {
          Model: metrics.cpu_model || metricCpu.Model || existingCpu.Model || '',
          Load: metrics.load || '-',
          Usage: metrics.cpu_usage || '0%',
          Cores: logicalCores || previousInfo.cpu?.Cores || '-',
          LogicalCores: logicalCores || previousInfo.cpu?.LogicalCores || previousInfo.cpu?.Cores || '-',
          PhysicalCores: physicalCores || previousInfo.cpu?.PhysicalCores || previousInfo.cpu?.Cores || '-',
          Temp: resolvedCpuTemp > 0 ? resolvedCpuTemp : (previousInfo.cpu?.Temp || 0),
          Power: metrics.cpu_power || metrics.cpu_power_w || previousInfo.cpu?.Power || ''
        });

        // Memory
        if (metrics.mem_usage) {
          const memMatch = String(metrics.mem_usage).match(/(\d+)\/(\d+)MB/);
          if (memMatch) {
            const used = parseInt(memMatch[1]);
            const total = parseInt(memMatch[2]);
            info.memory = reuseRealtimeValueIfEqual(previousInfo.memory, {
              Used: used + ' MB',
              Total: total + ' MB',
              Usage: Math.round((used / total) * 100) + '%'
            });
          }
        }

        // Disk
        if (
          metrics.disk_usage !== undefined && metrics.disk_usage !== null
        ) {
          info.disk = mergeRealtimeDiskInfo(previousInfo.disk, metrics);
        } else if (metrics.disk_used !== undefined || metrics.disk_total !== undefined) {
          info.disk = mergeRealtimeDiskInfo(previousInfo.disk, {
            ...metrics,
            disk_usage: metrics.disk_usage ?? previousInfo.disk?.[0]?.usage ?? '0%',
          });
        } else {
          info.disk = previousInfo.disk;
        }

        // Network
        if (metrics.network) {
          info.network = reuseRealtimeValueIfEqual(previousInfo.network, { ...previousInfo.network, ...metrics.network });
        }

        // GPU
        if (
          metrics.gpu !== undefined ||
          metrics.gpu_usage !== undefined ||
          metrics.gpu_mem !== undefined ||
          metrics.gpu_mem_percent !== undefined ||
          metrics.gpu_model !== undefined ||
          metrics.gpu_power !== undefined ||
          metrics.gpu_temp !== undefined
        ) {
          const existingGpu = info.gpu || {};
          const pushedGpu = typeof metrics.gpu === 'object' && metrics.gpu !== null ? metrics.gpu : {};
          const pushedGpuUsage = typeof metrics.gpu === 'number' ? `${metrics.gpu.toFixed(1)}%` : undefined;
          const pushedGpuPercent = pushedGpu.Percent !== undefined
            ? pushedGpu.Percent
            : (
              metrics.gpu_mem_percent !== undefined ||
                metrics.gpu_mem_used !== undefined ||
                metrics.gpu_mem_total !== undefined
                ? getGpuMemPercent(metrics)
                : existingGpu.Percent
            );
          const resolvedGpuUsage = pushedGpu.Usage ||
            (typeof metrics.gpu_usage === 'number' ? `${metrics.gpu_usage.toFixed(1)}%` : metrics.gpu_usage) ||
            pushedGpuUsage ||
            existingGpu.Usage ||
            '0%';
          info.gpu = reuseRealtimeValueIfEqual(previousInfo.gpu, {
            Model: pushedGpu.Model || metrics.gpu_model || existingGpu.Model || '',
            Usage: resolvedGpuUsage,
            Memory: pushedGpu.Memory || metrics.gpu_mem || existingGpu.Memory || '',
            Power: pushedGpu.Power || metrics.gpu_power || existingGpu.Power || '',
            Temp: pushedGpu.Temp !== undefined ? pushedGpu.Temp : (metrics.gpu_temp !== undefined ? metrics.gpu_temp : (existingGpu.Temp || 0)),
            Percent: pushedGpuPercent !== undefined ? pushedGpuPercent : 0,
          });
        }

        // Docker
        if (metrics.docker) {
          info.docker = reuseRealtimeValueIfEqual(previousInfo.docker, {
            installed: !!metrics.docker.installed,
            runningCount: metrics.docker.running || 0,
            stoppedCount: metrics.docker.stopped || 0,
            containers: Array.isArray(metrics.docker.containers) ? metrics.docker.containers : []
          });
        }

        info.platform = metrics.platform || previousInfo.platform;
        info.platformVersion = metrics.platformVersion || previousInfo.platformVersion;
        info.uptime = metrics.uptime || previousInfo.uptime;
        info.country_code = cleanCountryDisplayCode(firstLocationText(metrics.country_code, metrics.country, previousInfo.country_code, previousInfo.countryCode));
        info.countryCode = info.country_code || cleanCountryDisplayCode(previousInfo.countryCode);
        info.location = firstLocationText(metrics.location, metrics.resolved_country, metrics.region, previousInfo.location);
        info.region = firstLocationText(metrics.region, previousInfo.region);
        const infoLatitude = firstLocationNumber(metrics.latitude, metrics.lat, previousInfo.latitude, previousInfo.lat);
        const infoLongitude = firstLocationNumber(metrics.longitude, metrics.lon, previousInfo.longitude, previousInfo.lon);
        if (infoLatitude !== undefined && !(infoLatitude === 0 && infoLongitude === 0)) {
          info.latitude = infoLatitude;
        }
        if (infoLongitude !== undefined && !(infoLatitude === 0 && infoLongitude === 0)) {
          info.longitude = infoLongitude;
        }

        const nextInfo = reuseRealtimeValueIfEqual(server.info, info);
        const nextMetricsCache = resolveRealtimeMetricsCache(server.metricsCache, cache, { isExpanded });
        if (inExpandInteractionGuard) return server;
        const nextCountryCode = cleanCountryDisplayCode(firstLocationText(metrics.country_code, metrics.country, server.countryCode, server.country_code));
        const nextLocation = firstLocationText(metrics.location, metrics.resolved_country, metrics.region, server.location);
        const nextLatitude = firstLocationNumber(metrics.latitude, metrics.lat, server.latitude);
        const nextLongitude = firstLocationNumber(metrics.longitude, metrics.lon, server.longitude);
        const nextServer = {
          ...server,
          ...mergeTerminalCapabilities(server, true),
          info: nextInfo,
          countryCode: nextCountryCode || server.countryCode,
          location: nextLocation || server.location,
          region: firstLocationText(metrics.region, server.region),
          latitude: nextLatitude === 0 && nextLongitude === 0 ? server.latitude : (nextLatitude ?? server.latitude),
          longitude: nextLatitude === 0 && nextLongitude === 0 ? server.longitude : (nextLongitude ?? server.longitude),
          status: 'online',
          error: null,
          metricsCache: nextMetricsCache,
          metrics_health: 'fresh',
          metrics_stale: false,
          metrics_last_seen: new Date(now).toISOString(),
          metrics_last_seen_at: now,
          metrics_age_ms: 0,
          lastMetricUpdateTime: server.lastMetricUpdateTime || 0
        };
        if (areServerSnapshotsEqual(server, nextServer)) {
          return server;
        }

        changed = true;
        return {
          ...nextServer,
          lastMetricUpdateTime: now,
        };
      });
      if (!changed) return prev;
      return updated;
    });
  };

  const loadCardMetrics = async (serverId, options = {}) => {
    const { silent = false, force = false } = options;
    const requestKey = String(serverId);
    const cached = getCachedServerMetricHistory(serverId);
    const loadedAt = cardMetricsLoadedAtRef.current.get(requestKey) || 0;
    if (!force && silent && cached?.length && Date.now() - loadedAt < SERVER_CARD_METRICS_TTL_MS) {
      return cached;
    }
    const inFlight = cardMetricsRequestRef.current.get(requestKey);
    if (inFlight) return inFlight;

    setServerList(prev => {
      let changed = false;
      const updated = prev.map(s => {
        if (s.id !== serverId) return s;
        const next = {
          ...s,
          metricsCache: s.metricsCache || cached || null,
          metricsLoading: silent && (s.metricsCache?.length || cached?.length) ? false : true,
          error: silent ? s.error : null,
        };
        if (areServerSnapshotsEqual(s, next)) return s;
        changed = true;
        return next;
      });
      return changed ? updated : prev;
    });

    const request = (async () => {
      const now = Date.now();
      const historyStart = now - SERVER_CHART_HISTORY_WINDOW_MS;
      const params = new URLSearchParams({
        serverId,
        page: 1,
        pageSize: SERVER_CHART_HISTORY_LIMIT,
        startTime: formatSqliteUTCDateTime(historyStart),
        endTime: formatSqliteUTCDateTime(now),
        highPrecision: 'true'
      });
      const response = await fetch(`/api/server/metrics/history?${params}`);
      const data = await response.json();
      if (!data.success) {
        throw new Error(data.error || '指标历史加载失败');
      }
      const sorted = [...(data.data || [])].sort((a, b) => new Date(a.recorded_at) - new Date(b.recorded_at));
      const merged = mergeServerMetricHistory(
        serverId,
        { records: sorted, minTimestamp: historyStart },
        getCachedServerMetricHistory(serverId) || []
      );
      cardMetricsLoadedAtRef.current.set(requestKey, Date.now());
      setServerList(prev => {
        let changed = false;
        const updated = prev.map(s => {
          if (s.id !== serverId) return s;
          const next = { ...s, metricsCache: merged, metricsLoading: false, error: null };
          if (areServerSnapshotsEqual(s, next)) return s;
          changed = true;
          return next;
        });
        return changed ? updated : prev;
      });
      return merged;
    })();

    cardMetricsRequestRef.current.set(requestKey, request);
    try {
      return await request;
    } catch (e) {
      setServerList(prev => {
        let changed = false;
        const updated = prev.map(s => {
          if (s.id !== serverId) return s;
          const next = { ...s, metricsLoading: false, error: silent ? s.error : e.message };
          if (areServerSnapshotsEqual(s, next)) return s;
          changed = true;
          return next;
        });
        return changed ? updated : prev;
      });
      return [];
    } finally {
      cardMetricsRequestRef.current.delete(requestKey);
    }
  };

  const loadNetworkQuality = useCallback(async (serverId, options = {}) => {
    const { silent = false, collect = false } = options;
    const serverKey = String(serverId);
    const requestKey = `${serverKey}:${collect ? 'collect' : 'read'}`;
    const loadedAt = networkQualityLoadedAtRef.current.get(serverKey) || 0;
    if (!collect && silent && Date.now() - loadedAt < SERVER_NETWORK_QUALITY_TTL_MS) {
      return null;
    }
    if (networkQualityRequestRef.current.has(requestKey)) {
      return null;
    }
    networkQualityRequestRef.current.add(requestKey);

    setNetworkQualityByServer(prev => {
      const next = {
        ...(prev[serverId] || {}),
        loading: !silent || !(prev[serverId]?.sampleCount > 0),
        error: null,
      };
      if (areServerValuesEqual(prev[serverId], next)) return prev;
      return {
        ...prev,
        [serverId]: next,
      };
    });

    try {
      const params = new URLSearchParams({
        hours: '24',
        maxPointsPerTarget: String(SERVER_NETWORK_QUALITY_MAX_POINTS),
      });
      const response = await fetch(
        collect
          ? `/api/server/network-quality/${serverId}/collect`
          : `/api/server/network-quality/${serverId}?${params}`,
        collect
          ? {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              hours: 24,
              maxPointsPerTarget: SERVER_NETWORK_QUALITY_MAX_POINTS,
            }),
          }
          : undefined
      );
      const data = await response.json();
      if (!data.success) {
        throw new Error(data.error || '网络质量数据加载失败');
      }

      const quality = data.data || {};
      networkQualityLoadedAtRef.current.set(serverKey, Date.now());
      setNetworkQualityByServer(prev => {
        const next = {
          ...quality,
          loading: false,
          error: null,
        };
        if (areServerValuesEqual(prev[serverId], next)) return prev;
        return {
          ...prev,
          [serverId]: next,
        };
      });
      return quality;
    } catch (error) {
      setNetworkQualityByServer(prev => ({
        ...prev,
        [serverId]: {
          ...(prev[serverId] || {}),
          loading: false,
          error: error.message,
        },
      }));
      return null;
    } finally {
      networkQualityRequestRef.current.delete(requestKey);
    }
  }, []);

  useEffect(() => {
    if (expandedServers.length === 0) return undefined;

    const timer = setInterval(() => {
      if (!isPageVisible()) return;
      expandedServers.forEach(serverId => {
        if (serverStatusByIdRef.current.get(String(serverId)) !== 'online') return;
        loadNetworkQuality(serverId, { silent: true });
      });
    }, SERVER_NETWORK_QUALITY_REFRESH_MS);

    return () => clearInterval(timer);
  }, [expandedServers, loadNetworkQuality]);

  const loadServerInfo = async (serverId, options = {}) => {
    const { force = false, silent = false } = options;
    if (!silent) {
      setServerList(prev => prev.map(s => s.id === serverId ? { ...s, loading: true, error: null } : s));
    }

    try {
      const response = await fetch('/api/server/info', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, force }),
      });
      const data = await response.json();
      if (!data.success) {
        throw new Error(data.error || data.message || '主机详情加载失败');
      }
      const info = data.data || data;
      setServerList(prev => prev.map(s => s.id === serverId ? { ...s, info, loading: false, error: null } : s));

      if (data.is_cached && !force) {
        setTimeout(() => loadServerInfo(serverId, { force: true, silent: true }), 300);
      }
      return info;
    } catch (e) {
      setServerList(prev => prev.map(s => s.id === serverId ? { ...s, loading: false, error: e.message } : s));
      return null;
    }
  };

  const refreshServerInfo = async (serverId) => {
    const server = serverList.find(s => s.id === serverId);
    if (!server || server.loading) return;

    const waitForMetricsFlush = async (delayMs = 650) => {
      if (typeof window === 'undefined') {
        return new Promise(resolve => setTimeout(resolve, delayMs));
      }
      return new Promise(resolve => window.setTimeout(resolve, delayMs));
    };

    const [, firstHistory] = await Promise.all([
      loadServerInfo(serverId, { force: true }),
      loadCardMetrics(serverId, { silent: true, force: true }),
      loadNetworkQuality(serverId, { silent: true, collect: true }),
    ]);

    if (!firstHistory || firstHistory.length < 3) {
      await waitForMetricsFlush();
      await loadCardMetrics(serverId, { silent: true, force: true });
    }

    toast.success('主机详情已刷新');
  };

  // 切换折叠卡片并加载详情与历史数据
  const toggleServerExpand = (serverId) => {
    const server = serverList.find(s => s.id === serverId);
    if (!server) return;

		if (!isServerOnline(server)) {
      toast.warning('主机未在线，无法查看详情');
      return;
    }

    if (expandedServers.includes(serverId)) {
      setExpandedServers(prev => prev.filter(id => id !== serverId));
      expandInteractionUntilRef.current.delete(String(serverId));
    } else {
      setExpandedServers(prev => [...prev, serverId]);
      expandInteractionUntilRef.current.set(String(serverId), Date.now() + SERVER_EXPAND_INTERACTION_GUARD_MS);
      const hydrateExpandedServer = async () => {
        await Promise.all([
          server.info ? Promise.resolve(server.info) : loadServerInfo(serverId, { force: false }),
          loadCardMetrics(serverId, { silent: !!server.info }),
        ]);
        window.setTimeout(() => {
          loadNetworkQuality(serverId, { silent: true }).catch(() => { });
        }, Math.max(90, SERVER_CHART_RENDER_DEFER_MS));
      };
      const runHydration = () => {
        hydrateExpandedServer().catch(error => {
          console.error('[Server] Failed to hydrate expanded row:', error);
        });
      };

      if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
        window.requestAnimationFrame(runHydration);
      } else {
        setTimeout(runHydration, 0);
      }
    }
  };



  const openAddServerModal = () => {
    setServerAddMode('ssh');
    setQuickDeployName('');
    setQuickDeployResult(null);
    setAgentInstallOS('linux');
    setServerForm({
      id: null,
      name: '',
      host: '',
      port: 22,
      username: 'root',
      authType: 'password',
      password: '',
      privateKey: '',
      passphrase: '',
      tagsInput: '',
      description: '',
      startsAt: '',
      expiresAt: '',
      trafficLimitValue: '',
      trafficLimitUnit: 'TB',
      trafficAlertEnabled: false,
      trafficAlertPercent: 100,
      trafficCycleType: 'none',
      trafficCycleDay: 1,
      trafficCycleStart: '',
      trafficCycleEnd: '',
      monitorMode: 'agent'
    });
    setSelectedCredentialId('');
    setServerModalMode('add');
    setServerModalError('');
    setShowServerModal(true);
  };

  const openEditServerModal = (server) => {
    const trafficQuotaForm = bytesToTrafficQuotaForm(server.traffic_limit_bytes);
    setServerForm({
      id: server.id,
      name: server.name,
      host: server.host || '',
      port: server.port || 22,
      username: server.username || 'root',
      authType: server.auth_type === 'key' ? 'privateKey' : 'password',
      password: '',
      privateKey: '',
      passphrase: '',
      tagsInput: Array.isArray(server.tags) ? server.tags.join(',') : '',
      description: server.description || '',
      startsAt: formatDateInputValue(server.starts_at || server.created_at),
      expiresAt: formatDateInputValue(server.expires_at),
      trafficLimitValue: trafficQuotaForm.value,
      trafficLimitUnit: trafficQuotaForm.unit,
      trafficLimitMode: server.traffic_limit_mode || 'total',
      trafficAlertEnabled: Boolean(server.traffic_alert_enabled),
      trafficAlertPercent: normalizeTrafficAlertPercentInput(server.traffic_alert_percent),
      trafficCycleType: server.traffic_cycle_type || 'none',
      trafficCycleDay: normalizeTrafficCycleDayInput(server.traffic_cycle_day),
      trafficCycleStart: formatDateInputValue(server.traffic_cycle_start),
      trafficCycleEnd: formatDateInputValue(server.traffic_cycle_end),
      monitorMode: server.monitor_mode || 'agent'
    });
    setServerAddMode('ssh');
    setSelectedCredentialId('');
    setServerModalMode('edit');
    setServerModalError('');
    setShowServerModal(true);
  };

  const applyCredential = (credId) => {
    const normalizedCredId = String(credId ?? '');
    setSelectedCredentialId(normalizedCredId);

    if (!normalizedCredId) return;

    const cred = serverCredentials.find(c => String(c.id) === normalizedCredId);
    if (!cred) return;

    setServerForm(prev => ({
      ...prev,
      username: cred.username,
      authType: cred.auth_type === 'key' ? 'privateKey' : 'password',
      password: cred.password || '',
      privateKey: cred.private_key || '',
      passphrase: cred.passphrase || ''
    }));
  };

  const testServerConnection = async () => {
    if (!serverForm.host || !serverForm.username) {
      setServerModalError('请填写连接地址和用户名');
      return;
    }
    setServerModalSaving(true);
    setServerModalError('');
    toast.info('正在测试连接...');

    try {
      const response = await fetch('/api/server/test-connection', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: serverForm.host,
          port: serverForm.port,
          username: serverForm.username,
          auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
          password: serverForm.password,
          private_key: serverForm.privateKey,
          passphrase: serverForm.passphrase
        })
      });
      const data = await response.json();
      if (data.success) {
        toast.success('连接测试成功');
      } else {
        setServerModalError('测试连接失败: ' + data.message);
        toast.error('测试连接失败');
      }
    } catch (e) {
      setServerModalError('测试连接请求异常: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const ensureTrafficAlertRule = async () => {
    try {
      const [rulesRes, channelsRes] = await Promise.all([
        fetch('/api/notification/rules'),
        fetch('/api/notification/channels'),
      ]);
      const rulesData = await rulesRes.json().catch(() => ({}));
      const channelsData = await channelsRes.json().catch(() => ({}));
      const rules = Array.isArray(rulesData.data) ? rulesData.data : [];
      if (rules.some(rule => rule.source_module === 'server' && rule.event_type === 'traffic_high')) {
        return true;
      }
      const channels = (Array.isArray(channelsData.data) ? channelsData.data : [])
        .filter(channel => channel.enabled !== false && channel.enabled !== 0)
        .map(channel => channel.id)
        .filter(Boolean);
      if (channels.length === 0) {
        toast.warning('请先配置通知渠道后再创建流量告警规则');
        return false;
      }
      const res = await fetch('/api/notification/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: '主机流量超额',
          source_module: 'server',
          event_type: 'traffic_high',
          severity: 'warning',
          enabled: true,
          channels,
          suppression: { count: 1, minutes: 30 },
          title_template: '⚠️ {{serverName}} 流量超额',
          message_template: '主机 {{serverName}} 流量已使用 {{traffic_percent}}%，阈值 {{threshold}}%。\n已用：{{traffic_used}}\n配额：{{traffic_limit}}',
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '自动创建流量告警规则失败');
      toast.success('已自动创建流量告警规则');
      return true;
    } catch (error) {
      toast.warning(error.message || '流量告警规则自动创建失败');
      return false;
    }
  };

  const testTrafficAlert = async () => {
    if (!serverForm.id) {
      toast.warning('请先保存主机后再测试报警');
      return;
    }
    const trafficLimitBytes = trafficQuotaInputToBytes(serverForm.trafficLimitValue, serverForm.trafficLimitUnit);
    if (trafficLimitBytes <= 0) {
      toast.warning('请先设置总流量配额');
      return;
    }
    setServerModalSaving(true);
    try {
      const ruleReady = await ensureTrafficAlertRule();
      if (!ruleReady) return;
      const response = await fetch(`/api/server/accounts/${serverForm.id}/test-traffic-alert`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          traffic_alert_percent: normalizeTrafficAlertPercentInput(serverForm.trafficAlertPercent),
        }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || data.success === false) throw new Error(data.error || data.message || '报警测试失败');
      toast.success('流量报警测试已发送');
    } catch (error) {
      toast.error(error.message || '报警测试失败');
    } finally {
      setServerModalSaving(false);
    }
  };

  const saveServer = async () => {
    const isAgentForm = serverForm.monitorMode === 'agent' || serverAddMode === 'agent';
    const hasSshEndpoint = Boolean(serverForm.host?.trim() && serverForm.username?.trim());
    if (!serverForm.name?.trim()) {
      setServerModalError('请填写主机名称');
      return;
    }
    if (!isAgentForm && !hasSshEndpoint) {
      setServerModalError('请填写连接地址和用户名');
      return;
    }
    if (serverForm.trafficCycleType === 'custom' && serverForm.trafficCycleStart && serverForm.trafficCycleEnd) {
      const cycleStart = new Date(`${serverForm.trafficCycleStart}T00:00:00`).getTime();
      const cycleEnd = new Date(`${serverForm.trafficCycleEnd}T23:59:59`).getTime();
      if (Number.isFinite(cycleStart) && Number.isFinite(cycleEnd) && cycleEnd < cycleStart) {
        setServerModalError('流量周期结束时间不能早于开始时间');
        return;
      }
    }
    setServerModalSaving(true);
    setServerModalError('');

    try {
      const tags = serverForm.tagsInput ? serverForm.tagsInput.split(',').map(t => t.trim()).filter(Boolean) : [];
      const trafficLimitBytes = trafficQuotaInputToBytes(serverForm.trafficLimitValue, serverForm.trafficLimitUnit);
      const trafficAlertEnabled = trafficLimitBytes > 0 && Boolean(serverForm.trafficAlertEnabled);
      const payload = {
        name: serverForm.name.trim(),
        host: serverForm.host?.trim() || '',
        port: serverForm.port,
        username: serverForm.username?.trim() || 'agent',
        auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
        tags,
        description: serverForm.description,
        starts_at: normalizeStartInputValue(serverForm.startsAt),
        expires_at: normalizeExpiryInputValue(serverForm.expiresAt),
        traffic_limit_bytes: trafficLimitBytes,
        traffic_limit_mode: serverForm.trafficLimitMode || 'total',
        traffic_alert_enabled: trafficAlertEnabled,
        traffic_alert_percent: normalizeTrafficAlertPercentInput(serverForm.trafficAlertPercent),
        traffic_cycle_type: serverForm.trafficCycleType || 'none',
        traffic_cycle_day: normalizeTrafficCycleDayInput(serverForm.trafficCycleDay),
        traffic_cycle_start: serverForm.trafficCycleType === 'custom' ? normalizeStartInputValue(serverForm.trafficCycleStart) : null,
        traffic_cycle_end: serverForm.trafficCycleType === 'custom' ? normalizeExpiryInputValue(serverForm.trafficCycleEnd) : null,
        monitor_mode: isAgentForm ? 'agent' : 'ssh'
      };

      if (serverForm.authType === 'password' && serverForm.password) {
        payload.password = serverForm.password;
      }
      if (serverForm.authType === 'privateKey' && serverForm.privateKey) {
        payload.private_key = serverForm.privateKey;
        payload.passphrase = serverForm.passphrase;
      }

      const url = serverModalMode === 'add' ? '/api/server/accounts' : `/api/server/accounts/${serverForm.id}`;
      const method = serverModalMode === 'add' ? 'POST' : 'PUT';

      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const data = await response.json();
      if (data.success) {
        toast.success(serverModalMode === 'add' ? '主机添加成功' : '主机更新成功');
        if (trafficAlertEnabled) {
          await ensureTrafficAlertRule();
        }
        setShowServerModal(false);
        loadServerList();
      } else {
        setServerModalError(data.error || '保存失败');
      }
    } catch (e) {
      setServerModalError('保存异常: ' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const generateQuickInstallCommand = async () => {
    const name = quickDeployName.trim();
    if (!name) {
      setServerModalError('请输入主机名称');
      return;
    }

    setServerModalSaving(true);
    setServerModalError('');
    setQuickDeployResult(null);

    try {
      const response = await fetch('/api/server/agent/quick-install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      });
      const data = await response.json();

      if (data.success) {
        setQuickDeployResult(data.data);
        toast.success(data.data?.isNew ? 'Agent 主机已创建' : 'Agent 安装命令已生成');
        loadServerList();
      } else {
        setServerModalError(data.error || '生成 Agent 安装命令失败');
      }
    } catch (e) {
      setServerModalError('Agent 快速安装请求失败：' + e.message);
    } finally {
      setServerModalSaving(false);
    }
  };

  const showAgentInstallModal = async (serverId) => {
    const server = serverList.find(s => s.id === serverId);
    if (!server) {
      toast.error('主机不存在');
      return;
    }

    setAgentInstallLoading(true);
    setAgentInstallLog('');
    setAgentInstallResult(null);
    setAgentInstalling(false);
    setAgentForceSsh(false);
    setAgentModalData({
      serverId,
      serverName: server.name,
      installCommand: '',
      winInstallCommand: '',
      apiUrl: '',
      agentKey: '',
    });
    setShowAgentModal(true);

    try {
      const response = await fetch(`/api/server/agent/command/${serverId}`);
      const data = await response.json();
      if (data.success) {
        setAgentModalData(prev => ({ ...prev, ...(data.data || {}) }));
      } else {
        toast.error('获取 Agent 安装命令失败: ' + (data.error || data.message || '未知错误'));
      }
    } catch (e) {
      toast.error('获取 Agent 安装命令失败');
    } finally {
      setAgentInstallLoading(false);
    }
  };

  const normalizeAgentOrigin = (value) => {
    const raw = String(value || '').trim().replace(/\/+$/, '');
    if (!raw) return '';

    try {
      const url = new URL(/^https?:\/\//i.test(raw) ? raw : `https://${raw}`);
      return url.origin;
    } catch (e) {
      return '';
    }
  };

  const getAgentPublicBaseApiUrl = () => {
    const configuredOrigin = normalizeAgentOrigin(publicApiUrl);
    if (configuredOrigin) return configuredOrigin;
    return window.location.origin;
  };

  const getAgentBaseApiUrl = () => {
    const normalizeOrigin = (value) => {
      const raw = String(value || '').trim().replace(/\/+$/, '');
      if (!raw) return '';

      try {
        const url = new URL(/^https?:\/\//i.test(raw) ? raw : `https://${raw}`);
        return url.origin;
      } catch (e) {
        return '';
      }
    };

    const configuredOrigin = normalizeOrigin(publicApiUrl);
    if (configuredOrigin) return configuredOrigin;

    const modalOrigin = normalizeOrigin(agentModalData?.apiUrl);
    if (modalOrigin) return modalOrigin;

    return window.location.origin;
  };

  const getAgentInstallProtocol = () => {
    try {
      return new URL(getAgentBaseApiUrl()).protocol === 'http:' ? 'http' : 'https';
    } catch (_) {
      return 'https';
    }
  };

  const getAgentInstallEndpoint = (osType = agentInstallOS) => {
    return buildAgentInstallEndpoint({
      baseUrl: getAgentBaseApiUrl(),
      serverId: agentModalData?.serverId,
      agentKey: agentModalData?.agentKey,
      protocol: getAgentInstallProtocol(),
      osType,
    });
  };

  const getAgentInstallCommand = (osType = agentInstallOS) => {
    if (!agentModalData) return '';
    if (!agentModalData.agentKey) {
      return isWindowsAgentInstallOs(osType)
        ? agentModalData.winInstallCommand || ''
        : agentModalData.installCommand || '';
    }

    return buildAgentInstallCommand({
      baseUrl: getAgentBaseApiUrl(),
      serverId: agentModalData.serverId,
      agentKey: agentModalData.agentKey,
      protocol: getAgentInstallProtocol(),
      osType,
    });
  };

  const regenerateAgentKey = async () => {
    try {
      const response = await fetch('/api/server/agent/regenerate-key', { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success('Agent 全局密钥已重新生成');
        if (agentModalData?.serverId) {
          await showAgentInstallModal(agentModalData.serverId);
        }
      } else {
        toast.error('重新生成失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('重新生成 Agent 密钥失败');
    }
  };

  const waitForAgentRestart = async (serverId, initialConnectedAt, timeoutMs = 180000) => {
    const start = Date.now();
    await new Promise(resolve => setTimeout(resolve, 3000));

    while (Date.now() - start < timeoutMs) {
      try {
        const response = await fetch(`/api/server/agent/connection-info/${serverId}`);
        const data = await response.json();
        if (data.status === 'online') {
          const connectedAt = data.connectedAt || 0;
          if (initialConnectedAt === 0 || connectedAt > initialConnectedAt) {
            return true;
          }
        }
      } catch (e) {
        // transient network errors are ignored during verification
      }
      await new Promise(resolve => setTimeout(resolve, 2000));
    }
    return false;
  };

  const autoInstallAgent = async (serverId) => {
    setAgentInstalling(true);
    setAgentInstallResult(null);
    setAgentInstallLog('正在连接服务器并安装 Agent...\n');

    let initialConnectedAt = 0;
    try {
      const response = await fetch(`/api/server/agent/connection-info/${serverId}`);
      const data = await response.json();
      if (data.status === 'online') initialConnectedAt = data.connectedAt || 0;
    } catch (e) {
      // pre-check is best-effort
    }

    try {
      const response = await fetch(`/api/server/agent/auto-install/${serverId}?protocol=${encodeURIComponent(getAgentInstallProtocol())}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ force_ssh: agentForceSsh, base_url: getAgentPublicBaseApiUrl() }),
      });
      const data = await response.json();
      if (data.success) {
        setAgentInstallLog(prev => `${prev}${data.output || ''}\n\n安装/升级指令执行成功，正在验证 Agent 是否重新连接...`);
        const ok = await waitForAgentRestart(serverId, initialConnectedAt);
        if (ok) {
          setAgentInstallLog(prev => `${prev}\nAgent 新连接已建立，安装/升级成功。`);
          setAgentInstallResult('success');
          toast.success('Agent 已就绪');
          loadServerList();
        } else {
          setAgentInstallLog(prev => `${prev}\nAgent 未能在 180 秒内重建连接，可能仍在启动中。`);
          setAgentInstallResult('warning');
        }
      } else {
        setAgentInstallLog(prev => `${prev}${data.output || ''}\n\n安装失败: ${data.details || data.error || '未知错误'}`);
        setAgentInstallResult('error');
        toast.error('Agent 安装失败');
      }
    } catch (e) {
      setAgentInstallLog(prev => `${prev}\n网络错误: ${e.message}`);
      setAgentInstallResult('error');
    } finally {
      setAgentInstalling(false);
    }
  };

  const uninstallAgent = async (serverId) => {
    if (!(await dialog.confirm({
      title: '卸载 Agent',
      message: '从目标主机上卸载 Agent？',
      confirmText: '卸载',
      cancelText: '取消',
    }))) return;

    setAgentInstallLoading(true);
    try {
      let response = await fetch(`/api/server/agent/uninstall/${serverId}`, { method: 'POST' });
      let data = await response.json();
      if (response.status === 409 && data.data?.can_force_detach) {
        const confirmed = await dialog.confirm({
          title: '仅断开 Agent 关联',
          message: `${data.error}。继续操作只会断开面板关联，无法确认目标主机上的 Agent 程序已经删除。`,
          confirmText: '仅断开关联',
          cancelText: '取消',
          variant: 'destructive',
        });
        if (!confirmed) return;
        response = await fetch(`/api/server/agent/uninstall/${serverId}?force=1`, { method: 'POST' });
        data = await response.json();
      }
      if (response.ok && data.success) {
        toast.success(data.message || 'Agent 已卸载');
        setShowAgentModal(false);
        loadServerList();
      } else {
        toast.error('卸载失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('卸载 Agent 请求失败');
    } finally {
      setAgentInstallLoading(false);
    }
  };

  const deleteServer = async (serverId) => {
    const server = serverList.find(item => item.id === serverId);
    if (!confirmPress(`server.delete::${serverId}`, `删除主机「${server?.name || server?.host || `#${serverId}`}」`)) return;
    try {
      let response = await fetch(`/api/server/accounts/${serverId}`, { method: 'DELETE' });
      let data = await response.json();
      if (response.status === 409 && data.data?.can_force_delete) {
        const dependencies = data.data.dependencies || {};
        const dependencyText = [
          dependencies.nodes ? `${dependencies.nodes} 个节点` : '',
          dependencies.runtimes ? `${dependencies.runtimes} 个代理程序` : '',
          dependencies.tunnels ? `${dependencies.tunnels} 个 Tunnel` : '',
          dependencies.status_pages ? `${dependencies.status_pages} 个状态页关联` : '',
        ].filter(Boolean).join('、');
        const confirmed = await dialog.confirm({
          title: '强制移除离线主机',
          message: `${data.error}${dependencyText ? `。关联资源：${dependencyText}` : ''}。继续后会停止发布节点并清理全部面板关联，但离线主机本地可能残留 Agent 或代理服务。`,
          confirmText: '强制移除',
          cancelText: '取消',
          variant: 'destructive',
        });
        if (!confirmed) return;
        response = await fetch(`/api/server/accounts/${serverId}?force=1`, { method: 'DELETE' });
        data = await response.json();
      }
      if (!response.ok || !data.success) {
        throw new Error(data.error || '删除任务提交失败');
      }
      const taskId = data.data?.task_id;
      if (!taskId) {
        toast.success(data.message || '主机删除成功');
        await loadServerList();
        return;
      }
      toast.show({
        type: 'info',
        title: '正在级联删除主机',
        description: '系统将依次停止发布节点、清理代理资源、卸载 Agent，并删除面板关联。',
        isManual: true,
        duration: 5000,
      });
      const deadline = Date.now() + 10 * 60 * 1000;
      while (Date.now() < deadline) {
        const taskResponse = await fetch(`/api/server/tasks/${taskId}`, { cache: 'no-store' });
        const taskPayload = await taskResponse.json();
        if (!taskResponse.ok) throw new Error(taskPayload.error || '无法读取删除任务状态');
        const task = taskPayload.data || taskPayload;
        if (task.status === 'completed') {
          toast.success(typeof task.data === 'string' ? task.data : '主机级联删除完成');
          await loadServerList();
          return;
        }
        if (task.status === 'failed' || task.status === 'cancelled') {
          throw new Error(task.error || '主机级联删除失败');
        }
        await new Promise(resolve => window.setTimeout(resolve, 1000));
      }
      throw new Error('删除任务等待超时，请稍后刷新查看最终状态');
    } catch (e) {
      toast.error(`删除失败: ${e.message || '未知错误'}`);
    }
  };

  const runServerPowerAction = async (serverId, action) => {
    const actionText = action === 'reboot' ? '重启' : '关机';
    const confirmed = await dialog.confirm({
      title: `${actionText}主机`,
      message: action === 'shutdown'
        ? '此操作不可逆，请确认当前没有关键任务。'
        : '重启过程中连接会短暂中断。',
      confirmText: actionText,
      cancelText: '取消',
      variant: action === 'shutdown' ? 'destructive' : 'default',
    });
    if (!confirmed) return;

    try {
      const response = await fetch('/api/server/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, action }),
      });
      const data = await response.json();
      if (data.success) {
        toast.success(`${actionText}命令已发送`);
      } else {
        toast.error(`${actionText}失败: ${data.message || data.error || '未知错误'}`);
      }
    } catch (e) {
      toast.error(`${actionText}请求失败`);
    }
  };

  const saveServerOrder = async (orderedList) => {
    const orderData = orderedList.map((server, index) => ({
      id: server.id,
      order_index: index,
    }));

    try {
      const response = await fetch('/api/server/accounts/reorder', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ orderData }),
      });
      const data = await response.json();
      if (!data.success) {
        toast.error('排序保存失败: ' + (data.error || data.message || '未知错误'));
        loadServerList();
      }
    } catch (e) {
      toast.error('排序保存请求失败');
      loadServerList();
    }
  };

  const handleServerDragStart = (server, event) => {
    if (serverSearchText.trim() || serverStatusFilter !== 'all' || expandedServers.includes(server.id)) {
      event.preventDefault();
      return;
    }
    setDraggedServerId(server.id);
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', String(server.id));
  };

  const handleServerDragOver = (event) => {
    if (!draggedServerId || serverSearchText.trim() || serverStatusFilter !== 'all') return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  };

  const handleServerDrop = async (targetServerId, event) => {
    event.preventDefault();
    if (!draggedServerId || draggedServerId === targetServerId) return;

    const fromIndex = serverList.findIndex(s => s.id === draggedServerId);
    const toIndex = serverList.findIndex(s => s.id === targetServerId);
    if (fromIndex < 0 || toIndex < 0) return;

    const next = [...serverList];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    setServerList(next);
    setDraggedServerId(null);
    await saveServerOrder(next);
  };

  const toggleDockerPanel = (serverId) => {
    setExpandedDockerPanels(prev => (
      prev.includes(serverId)
        ? prev.filter(id => id !== serverId)
        : [...prev, serverId]
    ));
  };

  const getDockerContainerState = (container) => {
    const state = String(container?.state ?? container?.State ?? '').toLowerCase();
    const status = String(container?.status ?? container?.Status ?? '').toLowerCase();
    if (state === 'paused' || status.includes('paused')) return 'paused';
    if (state === 'restarting' || status.includes('restarting')) return 'restarting';
    if (state === 'running' || (status.includes('up') && !status.includes('paused'))) return 'running';
    if (state === 'dead' || status.includes('dead')) return 'dead';
    if (state === 'exited' || state === 'created' || state === 'stopped' || status.includes('exited')) return 'stopped';
    return state || 'unknown';
  };

  const getDockerContainerId = (container) => (
    String(container?.id || container?.ID || container?.Id || container?.container_id || container?.ContainerID || '')
  );

  const getDockerContainerName = (container) => {
    const names = container?.names || container?.Names;
    if (Array.isArray(names) && names.length > 0) return String(names[0]).replace(/^\/+/, '');
    return String(container?.name || container?.Name || container?.container_name || getDockerContainerId(container).slice(0, 12) || '-').replace(/^\/+/, '');
  };

  const getDockerContainerImage = (container) => (
    String(container?.image || container?.Image || container?.imageName || container?.ImageName || '-')
  );

  const getDockerContainerPorts = formatDockerContainerPorts;

  const getDockerUpdateCheckKey = (serverId, value) => {
    const normalized = String(value || '').trim();
    return normalized ? `${serverId}::${normalized}` : '';
  };

  const getDockerUpdateAliases = (serverId, result = {}) => {
    const aliases = new Set();
    const add = (value) => {
      const text = String(value || '').trim().replace(/^\/+/, '');
      if (!text || text === '-') return;
      aliases.add(text);
      if (text.length > 12) aliases.add(text.slice(0, 12));
    };

    add(result.containerId);
    add(result.container_id);
    add(result.containerName);
    add(result.container_name);
    add(result.name);
    add(result.image);

    return Array.from(aliases)
      .map(value => getDockerUpdateCheckKey(serverId, value))
      .filter(Boolean);
  };

  const normalizeDockerUpdateCheck = (serverId, result = {}, fallback = {}) => ({
    serverId,
    containerId: String(result.container_id || result.containerId || fallback.containerId || ''),
    containerName: String(result.container_name || result.containerName || result.name || fallback.containerName || '').replace(/^\/+/, ''),
    image: String(result.image || fallback.image || ''),
    currentDigest: String(result.current_digest || result.currentDigest || ''),
    latestDigest: String(result.latest_digest || result.latestDigest || ''),
    hasUpdate: !!(result.has_update ?? result.hasUpdate),
    error: result.error || '',
    checkedAt: Date.now(),
  });

  const storeDockerUpdateChecks = (serverId, results, fallback = {}) => {
    const list = Array.isArray(results) ? results : (results ? [results] : []);
    if (list.length === 0) return;

    setDockerUpdateChecks(prev => {
      const next = { ...prev };
      list.forEach(result => {
        const normalized = normalizeDockerUpdateCheck(serverId, result, fallback);
        getDockerUpdateAliases(serverId, normalized).forEach(key => {
          next[key] = normalized;
        });
      });
      return next;
    });
  };

  const getDockerContainerUpdateCheck = (serverId, container) => {
    const containerId = getDockerContainerId(container);
    const containerName = getDockerContainerName(container);
    const containerImage = getDockerContainerImage(container);
    const keys = getDockerUpdateAliases(serverId, {
      containerId,
      containerName,
      image: containerImage,
    });

    return keys.map(key => dockerUpdateChecks[key]).find(Boolean) || null;
  };

  const isDockerContainerUpdateChecking = (serverId, container) => {
    const containerId = getDockerContainerId(container);
    const containerName = getDockerContainerName(container);
    const containerImage = getDockerContainerImage(container);
    const keys = getDockerUpdateAliases(serverId, {
      containerId,
      containerName,
      image: containerImage,
    });

    return !!dockerBulkUpdateCheckServers[serverId] || keys.some(key => dockerUpdateCheckLoading[key]);
  };

  const getDockerActionKey = (serverId, action, payload = {}) => [
    String(serverId || ''),
    String(action || ''),
    String(payload.containerId || payload.containerName || payload.image || payload.name || payload.project || payload.projectName || '*'),
  ].join('::');

  const clearDockerActionPending = (serverId, action, payload = null) => {
    if (!serverId || !action) return;
    const exactKey = payload ? getDockerActionKey(serverId, action, payload) : '';
    const prefix = `${serverId}::${action}::`;
    setDockerActionPending(prev => {
      let changed = false;
      const next = { ...prev };
      if (exactKey) {
        if (next[exactKey]) {
          delete next[exactKey];
          changed = true;
        }
      } else {
        Object.keys(next).forEach(key => {
          if (key.startsWith(prefix)) {
            delete next[key];
            changed = true;
          }
        });
      }
      return changed ? next : prev;
    });
  };

  const isDockerActionPending = (serverId, action, payload = {}) => !!dockerActionPending[getDockerActionKey(serverId, action, payload)];

  const getDockerContainerSelectionKey = (serverId, container = {}) => {
    const containerId = String(container?.containerId || container?.container_id || getDockerContainerId(container) || '');
    const containerName = String(container?.containerName || container?.container_name || container?.name || container?.Name || '').replace(/^\/+/, '');
    const image = String(container?.image || container?.Image || '');
    return [String(serverId || ''), containerId || containerName || image || '*'].join('::');
  };

  const getDockerTaskTargetLabel = (payload = {}) => (
    String(
      payload.containerName
      || payload.container_name
      || payload.name
      || payload.project
      || payload.projectName
      || payload.image
      || payload.containerId
      || payload.container_id
      || ''
    ).replace(/^\/+/, '')
  );

  const getDockerTaskDisplayTitle = (task = {}) => {
    const payload = task.payload || {};
    const targetName = task.targetName || getDockerTaskTargetLabel(payload);
    const actionLabel = getDockerTaskActionLabel(task.action || task.command || task.type);

    const sId = task.serverId || payload.serverId || payload.server_id;
    const serverMatch = (dockerOverviewServers || []).find(s => String(s.id) === String(sId))
      || (serverList || []).find(s => String(s.id) === String(sId));
    const hostName = task.serverName || payload.serverName || serverMatch?.name;

    const hostPrefix = hostName ? `[${hostName}] ` : '';
    const mainTitle = targetName ? `${actionLabel} ${targetName}` : actionLabel;
    return `${hostPrefix}${mainTitle}`;
  };

  const getDockerTaskStateVariant = (state = '') => {
    const normalized = String(state || '').toLowerCase();
    if (normalized === 'success' || normalized === 'succeeded') return 'success';
    if (normalized === 'failed' || normalized === 'error' || normalized === 'timeout') return 'error';
    if (normalized === 'cancelled' || normalized === 'canceled') return 'neutral';
    return 'warning';
  };

  const isDockerTaskFinalState = (state = '') => ['success', 'succeeded', 'failed', 'error', 'timeout', 'cancelled', 'canceled'].includes(String(state || '').toLowerCase());

  const decorateDockerTask = (task = {}) => {
    const meta = dockerTaskMetaRef.current[task.taskId] || {};
    const payload = task.payload || meta.payload || {};
    const sId = task.serverId || meta.serverId || payload.serverId || payload.server_id;
    const serverMatch = (dockerOverviewServers || []).find(s => String(s.id) === String(sId))
      || (serverList || []).find(s => String(s.id) === String(sId));
    const serverName = task.serverName || meta.serverName || payload.serverName || serverMatch?.name;

    return normalizeDockerTaskResult({
      ...meta,
      ...task,
      payload,
      action: task.action || meta.action,
      serverId: sId,
      serverName,
      targetName: task.targetName || meta.targetName || getDockerTaskTargetLabel(payload),
      silent: task.silent ?? meta.silent,
    });
  };

  const rememberDockerTaskMeta = (taskId, meta = {}) => {
    if (!taskId) return;
    const payload = meta.payload || {};
    const normalized = {
      ...meta,
      payload,
      targetName: meta.targetName || getDockerTaskTargetLabel(payload),
      submittedAt: Date.now(),
    };
    dockerTaskMetaRef.current = {
      ...dockerTaskMetaRef.current,
      [taskId]: normalized,
    };
    setDockerTasks(prev => {
      let found = false;
      const next = prev.map(task => {
        if (task.taskId !== taskId) return task;
        found = true;
        return decorateDockerTask({ ...task, ...normalized, taskId });
      });
      if (found) return next;
      return [{
        taskId,
        state: 'queued',
        progress: 0,
        domain: 'docker',
        ...normalized,
      }, ...prev].slice(0, 30);
    });
  };

  const appendDockerInlineTask = (task = {}) => {
    const taskId = task.taskId || `inline-${task.action || 'docker'}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const payload = task.payload || {};
    const normalized = decorateDockerTask({
      ...task,
      taskId,
      payload,
      progress: task.progress ?? (isDockerTaskFinalState(task.state) ? 100 : 0),
      domain: task.domain || 'docker',
      targetName: task.targetName || getDockerTaskTargetLabel(payload),
    });
    setDockerTasks(prev => [normalized, ...prev.filter(item => item.taskId !== taskId)].slice(0, 30));
    return taskId;
  };

  const markDockerContainerUpdateComplete = (serverId, payload = {}) => {
    const normalized = {
      serverId,
      containerId: String(payload.containerId || payload.container_id || ''),
      containerName: String(payload.containerName || payload.container_name || '').replace(/^\/+/, ''),
      image: String(payload.image || ''),
      hasUpdate: false,
      updatedAt: Date.now(),
      checkedAt: Date.now(),
    };
    setDockerUpdateChecks(prev => {
      const next = { ...prev };
      getDockerUpdateAliases(serverId, normalized).forEach(key => {
        next[key] = normalized;
      });
      return next;
    });
    const selectionKey = getDockerContainerSelectionKey(serverId, payload);
    setDockerSelectedContainerKeys(prev => prev.filter(key => key !== selectionKey));
  };

  const getDockerActionProgress = (serverId, action, payload = {}) => {
    const actionKey = getDockerActionKey(serverId, action, payload);
    const task = dockerTasks
      .map(decorateDockerTask)
      .find(item => (
        item.serverId === serverId
        && item.action === action
        && getDockerActionKey(item.serverId, item.action, item.payload || {}) === actionKey
        && !isDockerTaskFinalState(item.state)
      ));
    return task ? clampPercent(toNumber(task.progress, 0)) : 0;
  };

  const isDockerUpdateConfirmActive = isArmed;

  const confirmDockerUpdatePress = confirmPress;

  const handleDockerContainerUpdate = (payload = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    const targetName = getDockerTaskTargetLabel(payload) || '当前容器';
    const confirmKey = `container.update::${getDockerContainerSelectionKey(serverId, payload)}`;
    if (!confirmDockerUpdatePress(confirmKey, `更新容器 ${targetName}`)) return;
    submitDockerTask('container.update', payload, { skipConfirm: true });
  };

  const getDockerUpdateBadge = (check) => {
    if (!check) return { variant: 'neutral', label: '未检测', title: '尚未检测镜像更新' };
    if (check.error) return { variant: 'error', label: '失败', title: check.error };
    if (check.hasUpdate) return { variant: 'warning', label: '可更新', title: '远端镜像摘要与本地不一致' };
    if (check.updatedAt) return { variant: 'success', label: '已更新', title: '容器已提交更新，等待下次检测确认镜像摘要' };
    if (check.currentDigest && check.latestDigest) return { variant: 'success', label: '已最新', title: '本地镜像已是远端最新摘要' };
    return { variant: 'neutral', label: '已检测', title: '远端或本地摘要不完整，无法严格判断' };
  };

  const getDockerUpdateResultError = (result = {}) => String(result.error || '').trim();

  const getDockerStateBadge = (state) => {
    if (state === 'running') return { variant: 'success', label: '运行' };
    if (state === 'paused' || state === 'restarting') return { variant: 'warning', label: state === 'paused' ? '暂停' : '重启中' };
    if (state === 'stopped' || state === 'dead') return { variant: 'error', label: state === 'dead' ? '异常' : '停止' };
    return { variant: 'neutral', label: '未知' };
  };

  const openBatchAgentModal = () => {
    setSelectedBatchServers(serverList.filter(canSshDeployAgent).map(server => server.id));
    setBatchInstallResults([]);
    setBatchAgentForceSsh(false);
    setShowBatchAgentModal(true);
  };

  const selectAllBatchServers = () => {
    setSelectedBatchServers(serverList.filter(canSshDeployAgent).map(server => server.id));
  };

  const canSshDeployAgent = (server = {}) => {
    if (server.capabilities?.ssh_configured || server.capabilities?.supports_ssh) return true;
    const host = String(server.host || '').trim();
    if (!host || host === '0.0.0.0') return false;
    const username = String(server.username || '').trim();
    if (!username) return false;
    if (server.auth_type === 'key') return Boolean(server.private_key || server.capabilities?.has_private_key);
    return Boolean(server.password || server.capabilities?.has_password);
  };

  const toggleBatchServerSelection = (serverId) => {
    setSelectedBatchServers(prev => (
      prev.includes(serverId)
        ? prev.filter(id => id !== serverId)
        : [...prev, serverId]
    ));
  };

  const updateBatchResult = (serverId, patch) => {
    setBatchInstallResults(prev => prev.map(item => (
      item.serverId === serverId ? { ...item, ...patch } : item
    )));
  };

  const normalizeAgentBatchStatus = (status) => {
    if (status === 'queued') return 'waiting';
    if (status === 'running') return 'processing';
    if (status === 'succeeded') return 'success';
    return status || 'waiting';
  };

  const mapAgentBatchItems = (items = []) => items.map(item => ({
    serverId: item.serverId,
    serverName: item.serverName || item.serverId,
    status: normalizeAgentBatchStatus(item.status),
    error: item.error || '',
    log: item.log || [],
  }));

  const isAgentBatchDone = (batch) => ['succeeded', 'failed'].includes(batch?.status);

  const pollAgentBatch = async (batchId, onSnapshot, intervalMs = 2000) => {
    while (batchId) {
      const response = await fetch(`/api/server/agent/batch/${batchId}`);
      const payload = await response.json();
      if (!payload.success) throw new Error(payload.error || '获取批任务状态失败');
      const batch = payload.data;
      onSnapshot(batch);
      if (isAgentBatchDone(batch)) return batch;
      await new Promise(resolve => setTimeout(resolve, intervalMs));
    }
    return null;
  };

  const runBatchAgentInstall = async () => {
    if (selectedBatchServers.length === 0 || agentInstallLoading) return;

    const initialResults = selectedBatchServers.map(id => {
      const server = serverList.find(s => s.id === id);
      return {
        serverId: id,
        serverName: server?.name || '未知主机',
        status: 'waiting',
        error: '',
      };
    });
    setBatchInstallResults(initialResults);
    setAgentInstallLoading(true);

    try {
      const response = await fetch(`/api/server/agent/batch-install?protocol=${encodeURIComponent(getAgentInstallProtocol())}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverIds: selectedBatchServers,
          force_ssh: batchAgentForceSsh,
          base_url: getAgentPublicBaseApiUrl(),
          concurrency: 16,
        }),
      });
      const payload = await response.json();
      if (!payload.success) throw new Error(payload.error || '创建批量部署任务失败');
      setBatchInstallResults(mapAgentBatchItems(payload.data.items));
      const finalBatch = await pollAgentBatch(payload.data.id, batch => {
        setBatchInstallResults(mapAgentBatchItems(batch.items));
      });
      if (finalBatch?.status === 'succeeded') {
        toast.success('批量 Agent 部署完成');
      } else {
        toast.warning('批量 Agent 部署完成，部分主机失败');
      }
      loadServerList();
    } catch (e) {
      toast.error(`批量 Agent 部署失败: ${e.message}`);
      setBatchInstallResults(prev => prev.map(item => (
        item.status === 'success' ? item : { ...item, status: 'failed', error: e.message }
      )));
    } finally {
      setAgentInstallLoading(false);
    }
  };

  const openUpgradeModal = () => {
    setUpgradeLog('');
    upgradeLogLineKeysRef.current = new Set();
    setUpgradeBatchSnapshot(null);
    setUpgradeProgress(0);
    setUpgrading(false);
    setUpgradeFallbackSsh(false);
    setShowUpgradeModal(true);
  };

	const getAgentUpgradeTargets = () => serverList.filter(server => (
		isServerOnline(server) || (upgradeFallbackSsh && canSshDeployAgent(server))
	));

  const getUpgradeBatchStatusLabel = (status) => {
    switch (status) {
      case 'queued':
        return '排队中';
      case 'running':
        return '执行中';
      case 'verifying':
        return '验证中';
      case 'succeeded':
        return '已完成';
      case 'failed':
        return '部分失败';
      default:
        return status || '待执行';
    }
  };

  const getUpgradeItemStatusLabel = (status) => {
    switch (status) {
      case 'queued':
        return '等待';
      case 'running':
        return '执行中';
      case 'verifying':
        return '验证中';
      case 'succeeded':
        return '成功';
      case 'failed':
        return '失败';
      default:
        return status || '未知';
    }
  };

  const resetUpgradeLog = (lines = []) => {
    upgradeLogLineKeysRef.current = new Set();
    const normalized = lines.map(line => String(line || '').trimEnd()).filter(Boolean);
    normalized.forEach((line, index) => upgradeLogLineKeysRef.current.add(`initial:${index}:${line}`));
    setUpgradeLog(normalized.length > 0 ? `${normalized.join('\n')}\n` : '');
  };

  const appendUpgradeLogEvents = (events = []) => {
    const nextLines = [];
    events.forEach(event => {
      if (!event) return;
      const key = String(event.key || event.line || '');
      const line = String(event.line || '').trimEnd();
      if (!key || !line || upgradeLogLineKeysRef.current.has(key)) return;
      upgradeLogLineKeysRef.current.add(key);
      nextLines.push(line);
    });
    if (nextLines.length === 0) return;
    setUpgradeLog(prev => `${prev || ''}${nextLines.join('\n')}\n`);
  };

  const appendUpgradeBatchSnapshot = (batch) => {
    if (!batch?.id) return;
    const summary = batch.summary || {};
    const total = batch.items?.length || 0;
    const done = (summary.succeeded || 0) + (summary.failed || 0);
    const events = [
      {
        key: `${batch.id}:status:${batch.status}:${done}:${total}:${summary.succeeded || 0}:${summary.failed || 0}`,
        line: `批任务 ${batch.id} ${getUpgradeBatchStatusLabel(batch.status)}，进度 ${done}/${total}，成功 ${summary.succeeded || 0}，失败 ${summary.failed || 0}`,
      },
    ];

    (batch.items || []).forEach(item => {
      const serverLabel = item.serverName || item.serverId || '未知主机';
      (item.log || []).forEach((line, index) => {
        events.push({
          key: `${batch.id}:${item.serverId}:log:${index}:${line}`,
          line: `[${serverLabel}] ${line}`,
        });
      });
      if (item.status === 'failed' || item.status === 'succeeded') {
        events.push({
          key: `${batch.id}:${item.serverId}:final:${item.status}:${item.error || ''}`,
          line: `[${serverLabel}] ${getUpgradeItemStatusLabel(item.status)}${item.error ? `: ${item.error}` : ''}`,
        });
      }
    });

    appendUpgradeLogEvents(events);
  };

  const performOneKeyUpgrade = async () => {
    if (upgrading) return;

    setUpgrading(true);
    setUpgradeBatchSnapshot(null);
    resetUpgradeLog(['开始批量升级任务...']);
    setUpgradeProgress(0);

    const targetServers = getAgentUpgradeTargets();

    if (targetServers.length === 0) {
      appendUpgradeLogEvents([{ key: 'no-targets', line: '没有检测到在线的 Agent 主机。' }]);
      setUpgrading(false);
      return;
    }

    appendUpgradeLogEvents([
      { key: 'targets', line: `目标 Agent: ${targetServers.length} 台。` },
      { key: `options:${upgradeFallbackSsh}`, line: `服务端批任务并发限制: 16。${upgradeFallbackSsh ? 'SSH 保底已开启。' : 'SSH 保底未开启。'}` },
    ]);

    try {
      const response = await fetch(`/api/server/agent/batch-upgrade?protocol=${encodeURIComponent(getAgentInstallProtocol())}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverIds: targetServers.map(server => server.id),
          force_ssh: false,
          fallback_ssh: upgradeFallbackSsh,
          base_url: getAgentPublicBaseApiUrl(),
          concurrency: 16,
        }),
      });
      const payload = await response.json();
      if (!payload.success) throw new Error(payload.error || '创建批量升级任务失败');

      const finalBatch = await pollAgentBatch(payload.data.id, batch => {
        setUpgradeBatchSnapshot(batch);
        const summary = batch.summary || {};
        const total = batch.items?.length || targetServers.length;
        const done = (summary.succeeded || 0) + (summary.failed || 0);
        setUpgradeProgress(total > 0 ? Math.round((done / total) * 100) : 0);
        appendUpgradeBatchSnapshot(batch);
      });
      setUpgradeBatchSnapshot(finalBatch || null);
      setUpgradeProgress(100);
      if (finalBatch?.status === 'succeeded') {
        appendUpgradeLogEvents([{ key: 'final:succeeded', line: '所有目标 Agent 均已完成升级并重新上线。' }]);
        toast.success('Agent 批量升级完成');
      } else {
        appendUpgradeLogEvents([{ key: 'final:failed', line: '批量升级完成，部分 Agent 失败。' }]);
        toast.warning('Agent 批量升级完成，部分失败');
      }
      loadServerList();
    } catch (e) {
      appendUpgradeLogEvents([{ key: `error:${e.message}`, line: `批量升级失败: ${e.message}` }]);
      toast.error(`批量升级失败: ${e.message}`);
    } finally {
      setUpgrading(false);
    }
  };

  // 触发所有主机探测
  const probeAllServers = async () => {
    toast.info('正在向所有在线 Agent 下发网络探测请求...');
    try {
      const response = await fetch('/api/server/check-all', { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success(data.message || '探测任务下发成功');
        loadServerList();
      }
    } catch (e) {
      toast.error('探测下发失败');
    }
  };

  // 双击就地重命名
  const startRenameServer = async (server) => {
    const newName = await dialog.prompt({
      message: '输入新的服务器名称',
      defaultValue: server.name,
    });
    if (newName && newName.trim() && newName !== server.name) {
      renameServer(server.id, newName.trim());
    }
  };

  const renameServer = async (serverId, name) => {
    try {
      const response = await fetch(`/api/server/accounts/${serverId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      });
      const data = await response.json();
      if (data.success) {
        toast.success('重命名成功');
        loadServerList();
      }
    } catch (e) {
      toast.error('重命名请求异常');
    }
  };

  // -------------------- 批量导入导出 --------------------

  const exportServers = async () => {
    try {
      const response = await fetch('/api/server/accounts');
      const data = await response.json();
      if (data.success) {
        const clean = data.data.map(s => ({
          name: s.name,
          host: s.host,
          port: s.port,
          username: s.username,
          auth_type: s.auth_type,
          password: s.password,
          private_key: s.private_key,
          passphrase: s.passphrase,
          tags: s.tags,
          description: s.description,
          country: s.country,
          resolved_country: s.resolved_country,
          starts_at: s.starts_at,
          expires_at: s.expires_at,
          monitor_mode: s.monitor_mode
        }));
        const blob = new Blob([JSON.stringify(clean, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `servers_export_${new Date().toISOString().slice(0, 10)}.json`;
        a.click();
        URL.revokeObjectURL(url);
        toast.success('主机配置导出成功');
      }
    } catch (e) {
      toast.error('导出主机失败');
    }
  };

  const openImportServerModal = () => {
    setImportPreview(null);
    setImportModalError('');
    setImportModalSaving(false);
    setShowImportServerModal(true);
  };

  const processImportFile = (file) => {
    if (!file.name.endsWith('.json')) {
      setImportModalError('仅支持导入 .json 格式文件');
      return;
    }
    const reader = new FileReader();
    reader.onload = (e) => {
      try {
        const parsed = JSON.parse(e.target.result);
        if (!Array.isArray(parsed)) {
          setImportModalError('文件解析失败：结构必须是主机数组');
          return;
        }
        setImportPreview(parsed);
        setImportModalError('');
      } catch (err) {
        setImportModalError('JSON 文件解析失败: ' + err.message);
      }
    };
    reader.readAsText(file);
  };

  const confirmImportServers = async () => {
    if (!importPreview || importPreview.length === 0) return;
    setImportModalSaving(true);
    try {
      const response = await fetch('/api/server/accounts/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ servers: importPreview })
      });
      const data = await response.json();
      if (data.success) {
        toast.success(`成功导入 ${data.imported || 0} 台主机`);
        setShowImportServerModal(false);
        setImportPreview(null);
        loadServerList();
      } else {
        setImportModalError(data.error || '导入失败');
      }
    } catch (e) {
      setImportModalError('导入服务器发生网络异常');
    } finally {
      setImportModalSaving(false);
    }
  };

  const batchAddServers = async () => {
    if (!serverBatchText.trim()) return;
    setServerAddingBatch(true);
    setServerBatchError('');
    setServerBatchSuccess('');

    try {
      const lines = serverBatchText.split('\n');
      const servers = [];
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;
        const parts = trimmed.split(',');
        if (parts.length >= 2) {
          servers.push({
            name: parts[0].trim(),
            host: parts[1].trim(),
            port: parseInt(parts[2]) || 22,
            username: parts[3]?.trim() || 'root',
            auth_type: 'password',
            password: parts[4]?.trim() || ''
          });
        }
      }
      if (servers.length === 0) {
        setServerBatchError('未识别出任何有效主机行。格式: 名称,IP,端口,用户名,密码');
        setServerAddingBatch(false);
        return;
      }

      const response = await fetch('/api/server/accounts/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ servers })
      });
      const data = await response.json();
      if (data.success) {
        setServerBatchSuccess(`批量成功导入 ${servers.length} 台主机`);
        setServerBatchText('');
        loadServerList();
      } else {
        setServerBatchError(data.error || '批量导入失败');
      }
    } catch (e) {
      setServerBatchError('批量添加异常: ' + e.message);
    } finally {
      setServerAddingBatch(false);
    }
  };



  const addCredential = async () => {
    if (!credForm.name || !credForm.username) {
      toast.warning('凭据名称与用户名必填');
      return;
    }
    try {
      const response = await fetch('/api/server/credentials', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(credForm)
      });
      const data = await response.json();
      if (data.success) {
        toast.success('新建凭据成功');
        setShowAddCredentialModal(false);
        loadCredentials();
      }
    } catch (e) {
      toast.error('保存凭据异常');
    }
  };

  const deleteCredential = async (id) => {
    const credential = serverCredentials.find(item => item.id === id);
    if (!confirmPress(`credential.delete::${id}`, `删除主机凭据「${credential?.name || credential?.username || `#${id}`}」`)) return;
    try {
      const response = await fetch(`/api/server/credentials/${id}`, { method: 'DELETE' });
      const data = await response.json();
      if (data.success) {
        toast.success('删除成功');
        loadCredentials();
      }
    } catch (e) {
      toast.error('删除凭据请求异常');
    }
  };

  const setDefaultCredential = async (id) => {
    try {
      const response = await fetch(`/api/server/credentials/${id}/default`, { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success('默认凭据设置成功');
        loadCredentials();
      }
    } catch (e) {
      toast.error('默认凭据设置失败');
    }
  };
  useEffect(() => {
    if (serverCurrentTab === 'docker') {
      ensureDockerTaskStream();
      loadDockerResources();
    }
  }, [serverCurrentTab, dockerSubTab, dockerHostOptionKey]);

  const ensureDockerTaskStream = () => {
    if (dockerTaskStreamRef.current) return;

    try {
      const stream = new EventSource('/api/server/v2/tasks/stream');
      dockerTaskStreamRef.current = stream;

      stream.addEventListener('ready', () => {
        setDockerTaskStreamConnected(true);
        setDockerTaskStreamError('');
      });

      stream.addEventListener('task.update', event => {
        try {
          const task = decorateDockerTask(JSON.parse(event.data));
          if (task && task.domain === 'docker') {
            setDockerTasks(prev => {
              const updated = prev.filter(t => t.taskId !== task.taskId);
              updated.unshift(task);
              if (updated.length > 30) updated.pop();
              return updated;
            });
            if (['success', 'failed', 'timeout', 'cancelled'].includes(task.state)) {
              clearDockerActionPending(task.serverId, task.action, task.payload);
            }
            // 任务成功完成时，如果正在当前标签页，触发静默刷新列表
            if (task.state === 'success') {
              if (task.action === 'container.update') {
                markDockerContainerUpdateComplete(task.serverId, task.payload);
              }
              if (!task.silent) {
                toast.success(`${getDockerTaskDisplayTitle(task)} 执行成功`);
              }
              scheduleDockerResourceRefresh(150);
            } else if (task.state === 'failed') {
              if (!task.silent) {
                toast.error(`${getDockerTaskDisplayTitle(task)} 失败: ${task.error || ''}`);
              }
            }
          }
        } catch (e) {
          console.error(e);
        }
      });

      stream.onerror = () => {
        setDockerTaskStreamConnected(false);
        setDockerTaskStreamError('任务流重连中...');
      };
    } catch (e) {
      console.error(e);
    }
  };

  const getDockerTaskConfirmation = (action, payload = {}) => {
    const targetName = payload.containerName || payload.image || payload.name || payload.project || payload.containerId || 'Docker 资源';
    const confirmations = {
      'container.stop': {
        title: '停止容器',
        message: `停止容器 ${targetName}？正在运行的服务会中断。`,
        confirmText: '停止',
        variant: 'danger',
      },
      'container.restart': {
        title: '重启容器',
        message: `重启容器 ${targetName}？服务会短暂中断。`,
        confirmText: '重启',
        variant: 'danger',
      },
      'container.delete': {
        title: '\u5220\u9664\u5bb9\u5668',
        message: `\u6c38\u4e45\u5220\u9664\u5bb9\u5668 ${targetName}\uff1f\u6b64\u64cd\u4f5c\u65e0\u6cd5\u64a4\u9500\u3002`,
        confirmText: '\u5220\u9664',
        variant: 'danger',
        deleteResource: true,
      },
      'compose.down': {
        title: '停止 Compose 项目',
        message: `停止 Compose 项目 ${targetName}？相关服务会中断。`,
        confirmText: '停止项目',
        variant: 'danger',
      },
      'image.prune': {
        title: '清理未使用镜像',
        message: '清理该主机上未被容器引用的 Docker 镜像？',
        confirmText: '清理镜像',
        variant: 'danger',
      },
      'network.prune': {
        title: '清理未使用网络',
        message: '清理该主机上未被容器使用的自定义 Docker 网络？',
        confirmText: '清理网络',
        variant: 'danger',
      },
      'volume.prune': {
        title: '清理未使用存储卷',
        message: '清理该主机上未被容器使用的 Docker 数据卷？',
        confirmText: '清理存储卷',
        variant: 'danger',
      },
    };

    if (['image.remove', 'network.remove', 'volume.remove'].includes(action)) {
      return {
        deleteResource: true,
        resourceType: action === 'image.remove' ? 'Docker 镜像' : action === 'network.remove' ? 'Docker 网络' : 'Docker 存储卷',
        resourceName: targetName,
      };
    }

    return confirmations[action] || null;
  };

  const getDockerProxyContainerAction = (action) => ({
    'container.start': 'start',
    'container.stop': 'stop',
    'container.restart': 'restart',
    'container.pause': 'pause',
    'container.unpause': 'unpause',
  }[action] || '');

  const getDockerProxyRequest = (serverId, action, payload = {}) => {
    const containerAction = getDockerProxyContainerAction(action);
    if (containerAction) {
      const containerId = payload.containerId;
      if (!containerId) throw new Error('Missing container ID');
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}/${containerAction}`,
        options: { method: 'POST' },
      };
    }
    if (action === 'container.delete') {
      const containerId = payload.containerId;
      if (!containerId) throw new Error('Missing container ID');
      return {
        url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}`,
        options: { method: 'DELETE' },
      };
    }
    if (action === 'image.prune') {
      return { url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/images/prune`, options: { method: 'POST' } };
    }
    if (action === 'image.remove') {
      const image = payload.image || payload.imageId || payload.id;
      if (!image) throw new Error('Missing image ID');
      const params = new URLSearchParams({ image });
      return { url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/images?${params.toString()}`, options: { method: 'DELETE' } };
    }
    if (action === 'network.prune') {
      return { url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/networks/prune`, options: { method: 'POST' } };
    }
    if (action === 'network.remove') {
      if (!payload.name) throw new Error('Missing network name');
      return { url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/networks/${encodeURIComponent(payload.name)}`, options: { method: 'DELETE' } };
    }
    if (action === 'volume.prune') {
      return { url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/volumes/prune`, options: { method: 'POST' } };
    }
    if (action === 'volume.remove') {
      if (!payload.name) throw new Error('Missing volume name');
      return { url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/volumes/${encodeURIComponent(payload.name)}`, options: { method: 'DELETE' } };
    }
    if (action.startsWith('compose.')) {
      const composeAction = action.split('.')[1];
      if (['up', 'down', 'restart', 'pull', 'update'].includes(composeAction)) {
        const project = payload.project || payload.projectName || payload.name;
        if (!project) throw new Error('Missing compose project');
        const configFile = payload.config_file || payload.configFile || payload.configFiles || payload.ConfigFiles || '';
        return {
          url: `/api/server/v2/docker/${encodeURIComponent(serverId)}/stacks/${encodeURIComponent(project)}/${composeAction}`,
          options: {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ config_file: configFile }),
          },
        };
      }
    }
    return null;
  };

  const scheduleDockerResourceRefresh = (delayMs = 650) => {
    if (dockerRefreshTimerRef.current) clearTimeout(dockerRefreshTimerRef.current);
    dockerRefreshTimerRef.current = setTimeout(() => {
      dockerRefreshTimerRef.current = null;
      loadDockerResources({ silent: true });
    }, delayMs);
  };

  const submitDockerTask = async (action, payload = {}, options = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    if (!serverId) {
      toast.warning('请先选择一台主机');
      return { ok: false };
    }
    const pendingKey = getDockerActionKey(serverId, action, payload);
    if (dockerActionPending[pendingKey]) {
      toast.info('该 Docker 操作正在执行中');
      return { ok: false };
    }
    if (!options.skipConfirm) {
      const confirmation = getDockerTaskConfirmation(action, payload);
      if (confirmation?.deleteResource) {
        if (!(await dialog.deleteResource(confirmation))) return { ok: false };
      } else if (confirmation && !(await dialog.confirm(confirmation))) {
        return { ok: false };
      }
    }

    if (isDockerMockPreviewEnabled()) {
      const taskId = `mock-${action}-${Date.now()}`;
      const mockTask = {
        taskId,
        state: 'success',
        progress: 100,
        action,
        domain: 'docker',
        serverId,
        payload,
        targetName: getDockerTaskTargetLabel(payload),
        silent: options.silent,
        message: `${payload.containerName || payload.project || payload.image || payload.name || 'Docker 资源'} 模拟执行成功`,
      };
      setDockerTasks(prev => [mockTask, ...prev].slice(0, 30));
      if (action === 'container.update') {
        markDockerContainerUpdateComplete(serverId, payload);
      }
      if (!options.silent) toast.success(`Mock 模式: ${getDockerTaskDisplayTitle(mockTask)}已完成`);
      return { ok: true, taskId };
    }

    setDockerActionPending(prev => ({ ...prev, [pendingKey]: true }));
    try {
      const resourceProxyRequest = getDockerProxyRequest(serverId, action, payload);
      if (resourceProxyRequest && !getDockerProxyContainerAction(action)) {
        const res = await fetch(resourceProxyRequest.url, resourceProxyRequest.options);
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.success) {
          throw new Error(data.error || data.message || 'Docker operation failed');
        }
        const pruneActions = ['image.prune', 'network.prune', 'volume.prune'];
        appendDockerInlineTask({
          state: 'success',
          action,
          serverId,
          payload,
          silent: options.silent,
          message: pruneActions.includes(action) ? (data?.data?.message || data?.message || '') : (data?.message || `${getDockerTaskActionLabel(action)}执行成功`),
        });
        if (!options.silent) {
          toast.success(pruneActions.includes(action) ? formatDockerPruneResult(action, data) : `${getDockerTaskActionLabel(action)}执行成功`);
        }
        clearDockerActionPending(serverId, action, payload);
        scheduleDockerResourceRefresh(400);
        return { ok: true, data };
      }

      const proxyAction = getDockerProxyContainerAction(action);
      if (proxyAction) {
        const containerId = payload.containerId;
        if (!containerId) {
          throw new Error('缺少容器 ID');
        }
        const res = await fetch(`/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}/${proxyAction}`, {
          method: 'POST',
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.success) {
          throw new Error(data.error || data.message || 'Docker 操作失败');
        }
        appendDockerInlineTask({
          state: 'success',
          action,
          serverId,
          payload,
          silent: options.silent,
          message: data?.message || `${getDockerTaskActionLabel(action)}执行成功`,
        });
        if (!options.silent) toast.success(`${getDockerTaskActionLabel(action)}执行成功`);
        clearDockerActionPending(serverId, action, payload);
        scheduleDockerResourceRefresh(400);
        return { ok: true };
      }

      const res = await fetch('/api/server/v2/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId,
          domain: 'docker',
          action,
          payload
        })
      });
      const data = await res.json();
      if (data.success) {
        const taskId = data.taskId || data.data?.taskId;
        rememberDockerTaskMeta(taskId, {
          serverId,
          action,
          payload,
          silent: options.silent,
        });
        if (!options.silent) {
          toast.info(`${getDockerTaskDisplayTitle({ action, payload })} 已提交，正在等待后台调度`);
        }
        return { ok: true, taskId };
      } else {
        toast.error('提交失败: ' + data.error);
        setDockerActionPending(prev => {
          const next = { ...prev };
          delete next[pendingKey];
          return next;
        });
        return { ok: false };
      }
    } catch (e) {
      appendDockerInlineTask({
        state: 'failed',
        action,
        serverId,
        payload,
        silent: options.silent,
        error: e?.message || '服务下发异常',
        message: e?.message || '服务下发异常',
      });
      toast.error(e?.message || '服务下发异常');
      setDockerActionPending(prev => {
        const next = { ...prev };
        delete next[pendingKey];
        return next;
      });
      return { ok: false };
    }
  };

  const loadDockerResources = async (options = {}) => {
    if (isDockerMockPreviewEnabled()) {
      const mock = createMockDockerOverview();
      setDockerOverviewServers(mock.servers);
      setDockerImages(mock.servers.flatMap(s => s.resources.images.map(img => ({ ...img, serverName: s.name, serverId: s.id }))));
      setDockerNetworks(mock.servers.flatMap(s => s.resources.networks.map(n => ({ ...n, serverName: s.name, serverId: s.id }))));
      setDockerVolumes(mock.servers.flatMap(s => s.resources.volumes.map(v => ({ ...v, serverName: s.name, serverId: s.id }))));
      setDockerStats(mock.servers.flatMap(s => s.resources.stats.map(stat => ({ ...stat, serverName: s.name, serverId: s.id }))));
      setDockerComposeProjects(mock.servers.flatMap(s => s.resources.composeProjects.map(p => ({ ...p, serverName: s.name, serverId: s.id }))));
      mock.updateChecks.forEach(check => storeDockerUpdateChecks(check.serverId, [check], check));
      setDockerTasks(prev => (prev.length > 0 ? prev : [{
        taskId: 'mock-check-updates',
        state: 'success',
        progress: 100,
        action: 'container.checkUpdates',
        domain: 'docker',
        message: JSON.stringify(mock.updateChecks.map(check => ({
          container_id: check.containerId,
          container_name: check.containerName,
          image: check.image,
          current_digest: check.currentDigest,
          latest_digest: check.latestDigest,
          has_update: check.hasUpdate,
        }))),
      }]));
      setDockerTaskStreamConnected(true);
      setDockerResourceLoading(false);
      return;
    }
    setDockerResourceLoading(true);
    try {
      const params = new URLSearchParams();
      params.set('scope', getDockerOverviewScope(dockerSubTab));

      const response = await fetch(`/api/server/v2/docker/overview?${params.toString()}`);
      const data = await response.json();
      if (data.success) {
        const servers = (data.data?.servers || [])
          .map(normalizeDockerOverviewServer)
          .filter(s => s.docker?.installed);
        setDockerOverviewServers(servers);

        setDockerImages(servers.flatMap(s => s.resources.images.map(img => ({ ...img, serverName: s.name, serverId: s.id }))));
        setDockerNetworks(servers.flatMap(s => s.resources.networks.map(n => ({ ...n, serverName: s.name, serverId: s.id }))));
        setDockerVolumes(servers.flatMap(s => s.resources.volumes.map(v => ({ ...v, serverName: s.name, serverId: s.id }))));
        setDockerStats(servers.flatMap(s => s.resources.stats.map(stat => ({ ...stat, serverName: s.name, serverId: s.id }))));
        setDockerComposeProjects(servers.flatMap(s => s.resources.composeProjects.map(p => ({ ...p, serverName: s.name, serverId: s.id }))));
      } else {
        setDockerOverviewServers([]);
        setDockerImages([]);
        setDockerNetworks([]);
        setDockerVolumes([]);
        setDockerStats([]);
        setDockerComposeProjects([]);
      }
    } catch (e) {
      console.error(e);
      if (!options.silent) {
        setDockerOverviewServers([]);
        setDockerImages([]);
        setDockerNetworks([]);
        setDockerVolumes([]);
        setDockerStats([]);
        setDockerComposeProjects([]);
      }
    } finally {
      setDockerResourceLoading(false);
    }
  };

  const dockerContainerManagementServers = useMemo(() => (
    dockerOverviewServers
      .map(server => ({
        ...server,
        resources: {
          ...server.resources,
          containers: asArray(server.resources?.containers),
        },
      }))
      .filter(server => asArray(server.resources?.containers).length > 0)
  ), [dockerOverviewServers]);

  const visibleDockerContainerServers = useMemo(() => {
    const query = dockerSearchQuery.trim().toLowerCase();
    const filterContainers = (server) => {
      const containers = asArray(server.resources?.containers).filter(container => {
        const state = getDockerContainerState(container);
        const updateCheck = getDockerContainerUpdateCheck(server.id, container);
        if (dockerContainerStateFilter === 'updatable') {
          if (!updateCheck?.hasUpdate) return false;
        } else if (dockerContainerStateFilter !== 'all' && state !== dockerContainerStateFilter) {
          return false;
        }
        if (!query) return true;
        return [
          getDockerContainerName(container),
          getDockerContainerImage(container),
          getDockerContainerId(container),
          getDockerContainerPorts(container),
          server.name,
        ].some(value => String(value || '').toLowerCase().includes(query));
      });
      return {
        ...server,
        resources: {
          ...server.resources,
          containers,
        },
      };
    };

    const servers = dockerOverviewServers.map(filterContainers);
    return servers.filter(server => asArray(server.resources?.containers).length > 0);
  }, [dockerOverviewServers, dockerSearchQuery, dockerContainerStateFilter]);

  const dockerStatsChartData = useMemo(() => {
    const isDarkMode = theme === 'dark';
    const netInColor = ChartPalette.categorical(0, isDarkMode);
    const netOutColor = ChartPalette.semantic('Success', isDarkMode);
    const readColor = ChartPalette.semantic('Warning', isDarkMode);
    const writeColor = ChartPalette.categorical(3, isDarkMode);
    return {
      cpu: buildDockerStatSeries(dockerStatsHistory, 'cpu', isDarkMode),
      memory: buildDockerStatSeries(dockerStatsHistory, 'memory', isDarkMode),
      network: buildDockerPairSeries(dockerStatsHistory, [
        { key: 'netIn', name: '接收', color: netInColor },
        { key: 'netOut', name: '发送', color: netOutColor },
      ]),
      disk: buildDockerPairSeries(dockerStatsHistory, [
        { key: 'blockRead', name: '读取', color: readColor },
        { key: 'blockWrite', name: '写入', color: writeColor },
      ]),
    };
  }, [dockerStatsHistory, theme]);

  const dockerStatsSummary = useMemo(() => {
    const latest = dockerStatsHistory[dockerStatsHistory.length - 1] || [];
    return latest.reduce((summary, item) => ({
      cpu: summary.cpu + toNumber(item.cpu, 0),
      memory: summary.memory + toNumber(item.memory, 0),
      netIn: summary.netIn + toNumber(item.netIn, 0),
      netOut: summary.netOut + toNumber(item.netOut, 0),
      blockRead: summary.blockRead + toNumber(item.blockRead, 0),
      blockWrite: summary.blockWrite + toNumber(item.blockWrite, 0),
    }), { cpu: 0, memory: 0, netIn: 0, netOut: 0, blockRead: 0, blockWrite: 0 });
  }, [dockerStatsHistory]);

  const dockerResourceSummary = useMemo(() => ({
    compose: dockerComposeProjects.length,
    images: dockerImages.length,
    networks: dockerNetworks.length,
    volumes: dockerVolumes.length,
    stats: dockerStats.length,
  }), [dockerComposeProjects.length, dockerImages.length, dockerNetworks.length, dockerVolumes.length, dockerStats.length]);

  const renderDockerSimpleLogCard = (className = '') => {
    const tasks = dockerTasks.map(decorateDockerTask);
    const visibleTasks = tasks.slice(0, 8);
    return (
      <LayerCard className={`overflow-hidden p-0 ${className}`}>
        <LayerCard.Secondary className="flex min-h-[52px] items-center justify-between gap-2 px-3 py-3.5">
          <span className="inline-flex min-w-0 items-center gap-2 text-xs font-bold text-kumo-strong">
            <Activity className="h-4 w-4 shrink-0 text-brand" />
            日志
          </span>
          <span className="flex shrink-0 items-center gap-1.5">
            <Badge variant={dockerTaskStreamConnected ? 'success' : 'warning'} appearance="dot">
              {dockerTaskStreamConnected ? '实时连接' : '重连中'}
            </Badge>
            <Badge variant="neutral">{tasks.length} 条</Badge>
            <Button
              size="sm"
              variant="secondary"
              icon={showDockerLogPanel ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
              onClick={() => setShowDockerLogPanel(prev => !prev)}
              aria-label={showDockerLogPanel ? '收起日志' : '展开日志'}
              title={showDockerLogPanel ? '收起日志' : '展开日志'}
            >
              {showDockerLogPanel ? '收起' : '展开'}
            </Button>
          </span>
        </LayerCard.Secondary>
        {showDockerLogPanel && (
          <LayerCard.Primary className="p-3">
            {tasks.length === 0 ? (
              <div className="text-xs text-kumo-subtle">暂无后台任务</div>
            ) : (
              <div className="flex max-h-72 flex-col gap-1.5 overflow-y-auto pr-1 scrollbar-thin">
                {visibleTasks.map(task => {
                  const progress = clampPercent(toNumber(task.progress, 0));
                  const showProgress = !isDockerTaskFinalState(task.state) && progress > 0;
                  const summary = summarizeDockerTaskMessage(task);
                  return (
                    <div key={task.taskId} className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-2">
                      <div className="flex min-w-0 items-center gap-2">
                        <Badge variant={getDockerTaskStateVariant(task.state)} appearance="dot">
                          {getDockerTaskStateLabel(task.state)}
                        </Badge>
                        <span className="min-w-0 flex-1 truncate text-[11px] font-semibold text-kumo-strong" title={getDockerTaskDisplayTitle(task)}>
                          {getDockerTaskDisplayTitle(task)}
                        </span>
                        {showProgress && <span className="shrink-0 text-[10px] font-semibold text-brand">{progress}%</span>}
                      </div>
                      {showProgress && (
                        <Meter
                          label={`${getDockerTaskDisplayTitle(task)}进度`}
                          value={progress}
                          showValue={false}
                          className="mt-1.5 gap-0"
                          trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
                          indicatorClassName="!h-full !bg-none !bg-brand"
                        />
                      )}
                      {summary && (
                        <div className="mt-1 truncate text-[11px] text-kumo-subtle" title={summary}>
                          {summary}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </LayerCard.Primary>
        )}
      </LayerCard>
    );
  };

  const renderDockerResourceSideRail = ({
    title,
    icon = <Settings className="h-4 w-4 shrink-0 text-brand" />,
    hosts = [],
    totalCount = 0,
    countLabel = '数量',
    summaryItems = [],
    actions = null,
    getHostCount = (server) => getDockerOverviewResourceCount(server, dockerSubTab),
    getHostBadges = () => [],
    renderHostAction = null,
  }) => {
    const statItems = [
      { label: countLabel, value: totalCount, className: 'text-kumo-strong' },
      ...summaryItems,
      { label: '主机', value: hosts.length, className: 'text-kumo-subtle' },
    ].slice(0, 4);
    return (
      <div className="flex min-w-0 flex-col gap-3 cq-xl:sticky cq-xl:top-0 cq-xl:self-start">
        <LayerCard className="overflow-hidden p-0">
          <LayerCard.Secondary className="flex min-h-[52px] items-center justify-between gap-2 px-3 py-3.5">
            <span className="inline-flex min-w-0 items-center gap-2 text-xs font-bold text-kumo-strong">
              <Settings className="h-4 w-4 shrink-0 text-brand" />
              管理
            </span>
            <span className="flex shrink-0 items-center gap-1.5">
              <Badge variant="neutral">{hosts.length} 主机</Badge>
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                icon={<RefreshCw className={`h-3.5 w-3.5 ${dockerResourceLoading ? 'animate-spin' : ''}`} />}
                disabled={dockerResourceLoading}
                onClick={loadDockerResources}
                aria-label="刷新 Docker 数据"
                title="刷新 Docker 数据"
              />
              {actions}
            </span>
          </LayerCard.Secondary>
          <LayerCard.Primary className="space-y-3 p-3">
            <div className="grid grid-cols-4 gap-1.5">
              {statItems.map(item => (
                <div key={item.label} className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                  <div className="text-[10px] text-kumo-subtle">{item.label}</div>
                  <div className={`mt-0.5 truncate text-sm font-bold ${item.className || 'text-kumo-strong'}`}>{item.value}</div>
                </div>
              ))}
            </div>

            <div className="flex flex-col gap-2">
              {hosts.map(server => {
                const isOpen = isDockerHostExpanded(server.id, hosts[0]?.id);
                const hostBadges = getHostBadges(server);
                return (
                  <div key={`${title}-${server.id}`} className={`overflow-hidden rounded-md border ${isOpen ? 'border-brand/55 bg-brand/5' : 'border-kumo-line/80 bg-kumo-base'}`}>
                      <div
                        role="button"
                        tabIndex={0}
                        onClick={() => focusDockerResourceHost(server.id, hosts[0]?.id)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault();
                            focusDockerResourceHost(server.id, hosts[0]?.id);
                          }
                        }}
                        className="flex min-h-10 cursor-pointer items-center justify-between gap-2 px-2.5 py-2"
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          {React.cloneElement(icon, { className: 'h-3.5 w-3.5 shrink-0 text-brand' })}
                          <span className="min-w-0 truncate text-xs font-bold text-kumo-strong">{server.name}</span>
                        </span>
                        <span className="flex shrink-0 items-center gap-1.5">
                          <Badge variant="neutral">{getHostCount(server)}</Badge>
                          {hostBadges.slice(0, 1).map(badge => (
                            <Badge key={badge.label} variant={badge.variant || 'neutral'} appearance={badge.appearance}>
                              {badge.label}
                            </Badge>
                          ))}
                        </span>
                      </div>

                      {(hostBadges.length > 0 || renderHostAction) && (
                        <AnimatedCollapse open={isOpen} keepMounted>
                          <div className="border-t border-kumo-line/70 px-2.5 py-2">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <div className="flex flex-wrap gap-1.5">
                                {hostBadges.map(badge => (
                                  <Badge key={badge.label} variant={badge.variant || 'neutral'} appearance={badge.appearance}>
                                    {badge.label}
                                  </Badge>
                                ))}
                              </div>
                              {renderHostAction?.(server)}
                            </div>
                          </div>
                        </AnimatedCollapse>
                      )}
                    </div>
                );
              })}
            </div>
          </LayerCard.Primary>
        </LayerCard>
        {renderDockerSimpleLogCard()}
      </div>
    );
  };

  const getDockerContainerSummary = (containers = [], serverId = '') => {
    const list = asArray(containers);
    return list.reduce((summary, container) => {
      const state = getDockerContainerState(container);
      if (state === 'running') summary.running += 1;
      else if (state === 'paused') summary.paused += 1;
      else summary.stopped += 1;
      const updateCheck = serverId ? getDockerContainerUpdateCheck(serverId, container) : null;
      if (updateCheck?.hasUpdate) summary.updatable += 1;
      return summary;
    }, { total: list.length, running: 0, paused: 0, stopped: 0, updatable: 0 });
  };

  const getUpdatableDockerContainers = (servers, serverId = '') => (
    servers.flatMap(server => {
      if (serverId && String(server.id) !== String(serverId)) return [];
      return asArray(server.resources?.containers)
        .filter(container => getDockerContainerUpdateCheck(server.id, container)?.hasUpdate)
        .map(container => ({
          server,
          container,
          payload: {
            serverId: server.id,
            containerId: getDockerContainerId(container),
            containerName: getDockerContainerName(container),
            image: getDockerContainerImage(container),
          },
        }));
    })
  );

  const getVisibleUpdatableDockerContainers = () => (
    getUpdatableDockerContainers(visibleDockerContainerServers)
  );

  const visibleUpdatableDockerContainers = useMemo(
    () => getVisibleUpdatableDockerContainers(),
    [visibleDockerContainerServers, dockerUpdateChecks]
  );
  const dockerContainerManagementUpdatableContainers = useMemo(
    () => getUpdatableDockerContainers(dockerContainerManagementServers),
    [dockerContainerManagementServers, dockerUpdateChecks]
  );
  const dockerSelectedContainerKeySet = useMemo(() => new Set(dockerSelectedContainerKeys), [dockerSelectedContainerKeys]);
  const selectedUpdatableDockerContainers = useMemo(() => (
    visibleUpdatableDockerContainers.filter(item => (
      dockerSelectedContainerKeySet.has(getDockerContainerSelectionKey(item.server.id, item.payload))
    ))
  ), [visibleUpdatableDockerContainers, dockerSelectedContainerKeySet]);
  const visibleUpdatableDockerContainerKeySet = useMemo(() => new Set(
    visibleUpdatableDockerContainers.map(item => getDockerContainerSelectionKey(item.server.id, item.payload))
  ), [visibleUpdatableDockerContainers]);

  useEffect(() => {
    setDockerSelectedContainerKeys(prev => prev.filter(key => visibleUpdatableDockerContainerKeySet.has(key)));
  }, [visibleUpdatableDockerContainerKeySet]);
  const dockerActiveBatchUpdateTargets = selectedUpdatableDockerContainers.length > 0
    ? selectedUpdatableDockerContainers
    : visibleUpdatableDockerContainers;
  const dockerActiveBatchUpdateScopeName = selectedUpdatableDockerContainers.length > 0 ? '选中容器' : '当前筛选';
  const dockerActiveBatchUpdateConfirmKey = useMemo(() => (
    `batch.update::${dockerActiveBatchUpdateScopeName}::${dockerActiveBatchUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`
  ), [dockerActiveBatchUpdateScopeName, dockerActiveBatchUpdateTargets]);
  const dockerManagementBatchUpdateConfirmKey = useMemo(() => (
    `batch.update::全部主机::${dockerContainerManagementUpdatableContainers.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`
  ), [dockerContainerManagementUpdatableContainers]);

  const hasDockerResourceData = dockerOverviewServers.length > 0
    || dockerImages.length > 0
    || dockerNetworks.length > 0
    || dockerVolumes.length > 0
    || dockerStats.length > 0
    || dockerComposeProjects.length > 0;
  const showDockerBlockingLoading = dockerResourceLoading && !hasDockerResourceData;
  const dockerContainerTotals = visibleDockerContainerServers.reduce((totals, server) => {
    const summary = getDockerContainerSummary(server.resources?.containers, server.id);
    totals.total += summary.total;
    totals.running += summary.running;
    totals.paused += summary.paused;
    totals.stopped += summary.stopped;
    totals.updatable += summary.updatable;
    return totals;
  }, { total: 0, running: 0, paused: 0, stopped: 0, updatable: 0 });
  const dockerContainerManagementTotals = dockerContainerManagementServers.reduce((totals, server) => {
    const summary = getDockerContainerSummary(server.resources?.containers, server.id);
    totals.total += summary.total;
    totals.running += summary.running;
    totals.paused += summary.paused;
    totals.stopped += summary.stopped;
    totals.updatable += summary.updatable;
    return totals;
  }, { total: 0, running: 0, paused: 0, stopped: 0, updatable: 0 });

  useEffect(() => {
    if (dockerSubTab !== 'containers') return;
    const hostIds = visibleDockerContainerServers.map(server => server.id);
    setExpandedDockerOverviewServers(prev => (prev === null ? prev : prev.filter(id => hostIds.includes(id))));
  }, [dockerSubTab, visibleDockerContainerServers]);

  useEffect(() => {
    if (!['compose', 'images', 'networks', 'volumes', 'stats'].includes(dockerSubTab)) return;
    const hostIds = dockerOverviewServers
      .filter(server => isDockerOverviewHostVisible(server, dockerSubTab))
      .map(server => server.id);
    setExpandedDockerOverviewServers(prev => (prev === null ? prev : prev.filter(id => hostIds.includes(id))));
  }, [dockerSubTab, dockerOverviewServers]);

  const toggleDockerOverviewServer = (serverId, autoExpandedServerId) => {
    setExpandedDockerOverviewServers(prev => {
      if (prev === null) {
        // 自动模式：点击自动展开的第一台 = 收起（进入手动全收起）；
        // 点击其它 = 只展开点击的那台（类似手风琴，第一台随之收起）。
        return autoExpandedServerId === serverId ? [] : [serverId];
      }
      return prev.includes(serverId)
        ? prev.filter(id => id !== serverId)
        : [...prev, serverId];
    });
  };

  const toggleDockerContainerSelection = (selectionKey, checked) => {
    setDockerSelectedContainerKeys(prev => {
      if (checked) return prev.includes(selectionKey) ? prev : [...prev, selectionKey];
      return prev.filter(key => key !== selectionKey);
    });
  };

  const selectDockerContainerScope = (serverId, filter = 'all') => {
    setDockerSelectedServer(serverId || '');
    setDockerContainerStateFilter(filter);
    if (serverId) setExpandedDockerOverviewServers([serverId]);
  };

  const renderDockerFilterChip = (serverId, filter, label, variant = 'neutral', title = '') => {
    const active = String(dockerSelectedServer || '') === String(serverId || '')
      && dockerContainerStateFilter === filter;
    const toneClass = active
      ? ''
      : variant === 'success'
        ? '!border-kumo-success/45 !bg-kumo-success/10 !text-kumo-success hover:!bg-kumo-success/15'
        : variant === 'warning'
          ? '!border-kumo-warning/55 !bg-kumo-warning/12 !text-kumo-warning hover:!bg-kumo-warning/18'
          : variant === 'error'
            ? '!border-kumo-danger/50 !bg-kumo-danger/10 !text-kumo-danger hover:!bg-kumo-danger/15'
            : '!border-kumo-interact/70 !bg-kumo-recessed/35 !text-kumo-strong hover:!bg-kumo-recessed/55';
    return (
      <Button
        type="button"
        size="sm"
        variant={active ? 'primary' : 'secondary'}
        onClick={(event) => {
          event.stopPropagation();
          selectDockerContainerScope(serverId, filter);
        }}
        title={title || label}
        className={`!h-7 !rounded-full !px-3 !text-xs !font-semibold !shadow-none ${toneClass}`}
      >
        {label}
      </Button>
    );
  };

  const batchUpdateVisibleDockerContainers = async (serverId = '', useAllServers = false, explicitTargets = null, options = {}) => {
    const sourceServers = serverId || useAllServers ? dockerContainerManagementServers : visibleDockerContainerServers;
    const targets = Array.isArray(explicitTargets) ? explicitTargets : getUpdatableDockerContainers(sourceServers, serverId);
    const scopeName = serverId
      ? (
        dockerContainerManagementServers.find(server => String(server.id) === String(serverId))?.name
        || visibleDockerContainerServers.find(server => String(server.id) === String(serverId))?.name
        || '当前主机'
      )
      : options.scopeName || (useAllServers ? '全部主机' : '当前筛选');
    if (targets.length === 0) {
      toast.warning(`${scopeName}下没有已检测出的可更新容器`);
      return;
    }
    const confirmKey = options.confirmKey || `batch.update::${scopeName}::${targets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`;
    if (!options.skipDoubleConfirm && !confirmDockerUpdatePress(confirmKey, `更新${scopeName} ${targets.length} 个容器`)) return;

    const queue = [...targets];
    let submitted = 0;
    let failed = 0;
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length > 0) {
        const item = queue.shift();
        try {
          const result = await submitDockerTask('container.update', item.payload, { skipConfirm: true, silent: true });
          if (result?.ok) submitted += 1;
          else failed += 1;
        } catch (error) {
          failed += 1;
        }
      }
    });
    await Promise.all(workers);

    if (failed > 0) {
      toast.warning(`已提交 ${submitted} 个更新任务，${failed} 个提交失败`);
    } else {
      toast.success(`已提交 ${submitted} 个容器更新任务`);
    }
  };

  const setDockerUpdateLoadingForAliases = (serverId, fallback, loading) => {
    const aliases = getDockerUpdateAliases(serverId, fallback);
    if (aliases.length === 0) return;

    setDockerUpdateCheckLoading(prev => {
      const next = { ...prev };
      aliases.forEach(key => {
        if (loading) {
          next[key] = true;
        } else {
          delete next[key];
        }
      });
      return next;
    });
  };

  const checkDockerUpdatesForServer = async (server, container = null, options = {}) => {
    const serverId = typeof server === 'object' ? server?.id : server;
    if (!serverId) {
      toast.warning('请先选择一台主机');
      return { ok: false, error: 'missing serverId', results: [] };
    }

    const fallback = container ? {
      containerId: getDockerContainerId(container),
      containerName: getDockerContainerName(container),
      image: getDockerContainerImage(container),
    } : {};

    setDockerUpdateLoadingForAliases(serverId, fallback, true);

    try {
      const response = await fetch('/api/server/docker/check-update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId,
          containerId: fallback.containerId || '',
        }),
      });
      const data = await response.json();
      if (!response.ok || !data.success) {
        throw new Error(data.error || '检查更新失败');
      }

      const results = Array.isArray(data.data) ? data.data : (data.data ? [data.data] : []);
      if (results.length > 0) {
        storeDockerUpdateChecks(serverId, results, fallback);
      } else if (container) {
        storeDockerUpdateChecks(serverId, [{
          ...fallback,
          error: '未返回检测结果',
        }], fallback);
      }

      const failedResults = results.filter(result => getDockerUpdateResultError(result));
      if (!options.silent) {
        const updateCount = results.filter(item => item?.has_update || item?.hasUpdate).length;
        const hostTag = server?.name ? `[${server.name}] ` : '';
        if (failedResults.length > 0) {
          const target = failedResults[0]?.container_name || failedResults[0]?.containerName || fallback.containerName || '容器';
          toast.error(`${hostTag}${target} 检测失败：${getDockerUpdateResultError(failedResults[0])}`);
        } else if (updateCount > 0) {
          toast.warning(`${hostTag}检测完成，发现 ${updateCount} 个可更新镜像`);
        } else {
          toast.success(`${hostTag}检测完成，暂无可更新镜像`);
        }
      }

      return { ok: failedResults.length === 0, error: getDockerUpdateResultError(failedResults[0]), results };
    } catch (error) {
      if (container) {
        storeDockerUpdateChecks(serverId, [{
          ...fallback,
          error: error.message || '检查更新失败',
        }], fallback);
      }
      if (!options.silent) {
        toast.error(error.message || '检查更新失败');
      }
      return { ok: false, error: error.message || '检查更新失败', results: [] };
    } finally {
      setDockerUpdateLoadingForAliases(serverId, fallback, false);
    }
  };

  const checkDockerUpdatesForServers = async (servers, emptyMessage = '当前没有可检测的 Docker 容器') => {
    const targets = servers.filter(server => asArray(server.resources?.containers).length > 0);
    if (targets.length === 0) {
      toast.warning(emptyMessage);
      return;
    }

    setDockerBulkUpdateChecking(true);
    setDockerBulkUpdateCheckServers(Object.fromEntries(targets.map(server => [server.id, true])));

    try {
      const results = await Promise.all(
        targets.map(async (server) => {
          try {
            return await checkDockerUpdatesForServer(server, null, { silent: true });
          } catch (error) {
            return { ok: false, error: error?.message || '检查失败', results: [] };
          } finally {
            setDockerBulkUpdateCheckServers(prev => {
              const next = { ...prev };
              delete next[server.id];
              return next;
            });
          }
        })
      );
      const finished = results.length > 0 ? results : targets.map(() => ({ ok: false, error: '检查失败', results: [] }));
      const updateCount = finished.reduce(
        (sum, item) => sum + item.results.filter(result => result?.has_update || result?.hasUpdate).length,
        0
      );
      const failedCount = finished.filter(item => !item.ok).length;

      const targetTag = targets.length === 1 && targets[0]?.name ? `[${targets[0].name}] ` : '';
      if (failedCount > 0) {
        toast.warning(`${targetTag}检测完成，${updateCount} 个可更新，${failedCount} 台主机检测失败`);
      } else if (updateCount > 0) {
        toast.warning(`${targetTag}检测完成，发现 ${updateCount} 个可更新镜像`);
      } else {
        toast.success(`${targetTag}检测完成，暂无可更新镜像`);
      }
    } finally {
      setDockerBulkUpdateChecking(false);
      setDockerBulkUpdateCheckServers({});
    }
  };

  const checkVisibleDockerUpdates = async () => (
    checkDockerUpdatesForServers(visibleDockerContainerServers, '当前筛选下没有可检测的 Docker 容器')
  );

  const checkAllDockerUpdates = async () => (
    checkDockerUpdatesForServers(dockerContainerManagementServers, '当前没有可检测的 Docker 容器')
  );

  const renderDockerEmptyState = (message) => (
    <AppCard padding="none" className="p-10 text-center text-xs text-kumo-subtle">
      {message}
    </AppCard>
  );

  const renderDockerHostResourceSection = ({
    server,
    icon,
    count = 0,
    countLabel = '项',
    badges = [],
    actions = null,
    children,
    isFirstVisible = false,
  }) => {
    const isOpen = isDockerHostExpanded(server.id, isFirstVisible ? server.id : null);
    return (
      <div id={`docker-resource-section-${server.id}`} className="scroll-mt-24 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
          <div
            role="button"
            tabIndex={0}
            onClick={() => toggleDockerOverviewServer(server.id, isFirstVisible ? server.id : null)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault();
                toggleDockerOverviewServer(server.id, isFirstVisible ? server.id : null);
              }
            }}
            className="flex min-h-[52px] cursor-pointer flex-wrap items-center justify-between gap-2 border-b border-kumo-line/70 px-3 py-3.5"
          >
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              {icon}
              <span className="min-w-0 truncate text-xs font-bold text-kumo-strong">{server.name}</span>
              <Badge variant="neutral">{count} {countLabel}</Badge>
              {badges.map((badge) => (
                <Badge key={badge.label} variant={badge.variant || 'neutral'} appearance={badge.appearance}>
                  {badge.label}
                </Badge>
              ))}
            </div>
            <div className="flex shrink-0 items-center gap-1.5">
              {actions}
              <Button
                size="sm"
                variant="secondary"
                icon={isOpen ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                onClick={(event) => {
                  event.stopPropagation();
                  toggleDockerOverviewServer(server.id, isFirstVisible ? server.id : null);
                }}
              >
                {isOpen ? '收起' : '展开'}
              </Button>
            </div>
          </div>
          <AnimatedCollapse open={isOpen} keepMounted>
            {children}
          </AnimatedCollapse>
      </div>
    );
  };

  const focusDockerResourceHost = (serverId, autoExpandedServerId) => {
    if (!serverId) return;
    setExpandedDockerOverviewServers(prev => (prev === null && autoExpandedServerId === serverId ? [] : [serverId]));
    window.setTimeout(() => {
      const target = document.getElementById(`docker-resource-section-${serverId}`);
      target?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }, 80);
  };

  const submitDockerPruneTask = async (action, payload, confirmKey, confirmLabel, options = {}) => {
    if (!confirmDockerUpdatePress(confirmKey, confirmLabel)) return { ok: false };
    return submitDockerTask(action, payload, { ...options, skipConfirm: true });
  };

  const pruneDockerImagesForHosts = async (hosts, options = {}) => {
    const targets = asArray(hosts).filter(server => server?.id);
    if (targets.length === 0) {
      toast.warning('当前没有可清理镜像的主机');
      return;
    }
    const confirmKey = options.confirmKey || `image.prune.all::${targets.map(server => server.id).join('|')}`;
    if (!confirmDockerUpdatePress(confirmKey, `清理 ${targets.length} 台主机未使用镜像`)) return;

    let ok = 0;
    let failed = 0;
    const queue = [...targets];
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length > 0) {
        const server = queue.shift();
        const result = await submitDockerTask('image.prune', { serverId: server.id }, { skipConfirm: true, silent: true });
        if (result?.ok) ok += 1;
        else failed += 1;
      }
    });
    await Promise.all(workers);

    if (failed > 0) {
      toast.warning(`已清理 ${ok} 台主机镜像，${failed} 台提交失败`);
    } else {
      toast.success(`已提交 ${ok} 台主机镜像清理`);
    }
  };

  const pruneDockerNetworksForHosts = async (hosts, options = {}) => {
    const targets = asArray(hosts).filter(server => server?.id);
    if (targets.length === 0) {
      toast.warning('当前没有可清理网络的主机');
      return;
    }
    const confirmKey = options.confirmKey || `network.prune.all::${targets.map(server => server.id).join('|')}`;
    if (!confirmDockerUpdatePress(confirmKey, `清理 ${targets.length} 台主机未使用网络`)) return;

    let ok = 0;
    let failed = 0;
    const queue = [...targets];
    const workers = Array.from({ length: Math.min(3, queue.length) }, async () => {
      while (queue.length > 0) {
        const server = queue.shift();
        const result = await submitDockerTask('network.prune', { serverId: server.id }, { skipConfirm: true, silent: true });
        if (result?.ok) ok += 1;
        else failed += 1;
      }
    });
    await Promise.all(workers);

    if (failed > 0) {
      toast.warning(`已清理 ${ok} 台主机网络，${failed} 台提交失败`);
    } else {
      toast.success(`已提交 ${ok} 台主机网络清理`);
    }
  };

  const openDockerComposeConfig = async (server, project, mode = 'view') => {
    const projectName = getComposeProjectName(project);
    const configFiles = getComposeConfigFileList(project);
    const path = configFiles[0] || '';
    const baseEditor = {
      mode,
      serverId: server?.id || '',
      serverName: server?.name || '-',
      projectName,
      status: formatComposeStatusLabel(getComposeStatus(project)),
      workingDir: getComposeWorkingDir(project),
      configFiles,
      path,
      content: '',
      originalContent: '',
      loading: Boolean(path),
      saving: false,
      saved: false,
      updating: false,
      error: path ? '' : '未找到 Compose 配置文件路径',
    };
    setDockerComposeEditor(baseEditor);
    if (!path) return;

    if (isDockerMockPreviewEnabled()) {
      const mockContent = [
        'services:',
        `  ${projectName}:`,
        '    image: nginx:1.27-alpine',
        '    restart: unless-stopped',
        '    ports:',
        '      - "8080:80"',
        '',
      ].join('\n');
      setDockerComposeEditor(prev => prev ? {
        ...prev,
        content: mockContent,
        originalContent: mockContent,
        loading: false,
      } : prev);
      return;
    }

    try {
      const data = await readSftpFile(server.id, path, 1024 * 1024);
      const content = data.data || '';
      setDockerComposeEditor(prev => prev ? {
        ...prev,
        content,
        originalContent: content,
        loading: false,
        error: '',
      } : prev);
    } catch (error) {
      setDockerComposeEditor(prev => prev ? {
        ...prev,
        loading: false,
        error: error.message || '读取 Compose 配置失败',
      } : prev);
      toast.error(error.message || '读取 Compose 配置失败');
    }
  };

  const requestCloseDockerComposeEditor = async () => {
    if (dockerComposeEditor?.mode === 'edit' && dockerComposeEditor.content !== dockerComposeEditor.originalContent) {
      const ok = await dialog.confirm({
        title: '放弃未保存修改',
        message: `Compose 配置 ${dockerComposeEditor.path || ''} 还有未保存内容。`,
        confirmText: '放弃修改',
        cancelText: '继续编辑',
        variant: 'danger',
      });
      if (!ok) return;
    }
    setDockerComposeEditor(null);
  };

  const saveDockerComposeConfig = async () => {
    if (!dockerComposeEditor?.path || dockerComposeEditor.mode !== 'edit') return;
    setDockerComposeEditor(prev => prev ? { ...prev, saving: true, error: '' } : prev);
    if (isDockerMockPreviewEnabled()) {
      setDockerComposeEditor(prev => prev ? {
        ...prev,
        saving: false,
        mode: 'view',
        originalContent: prev.content,
        saved: true,
      } : prev);
      toast.success('Mock 模式: Compose 配置已保存');
      return;
    }

    try {
      await writeSftpFile(dockerComposeEditor.serverId, dockerComposeEditor.path, dockerComposeEditor.content);
      setDockerComposeEditor(prev => prev ? {
        ...prev,
        saving: false,
        mode: 'view',
        originalContent: prev.content,
        saved: true,
      } : prev);
      toast.success('Compose 配置已保存');
      scheduleDockerResourceRefresh(500);
    } catch (error) {
      setDockerComposeEditor(prev => prev ? {
        ...prev,
        saving: false,
        error: error.message || '保存 Compose 配置失败',
      } : prev);
      toast.error(error.message || '保存 Compose 配置失败');
    }
  };




  const openSSHTerminal = (server) => {
    if (!server) return;


    const existing = sshSessions.find(s => s.server.id === server.id);
    if (existing) {
      switchToSSHTab(existing.id);
      return;
    }

    let terminalServer = server;
    let type = resolveTerminalProtocol(server);
    if (!type && hasSshEndpoint(server)) {
      terminalServer = {
        ...server,
        ssh_configured: true,
        supports_ssh: true,
        preferred_terminal_transport: 'ssh',
      };
      type = 'ssh';
    }
    if (!type) {
      toast.warning('该主机当前没有可用的终端传输');
      return;
    }

    const sessionId = 'session_' + Date.now();

    const newSession = {
      id: sessionId,
      server: terminalServer,
      type,
      connected: false,
      name: terminalServer.name
    };

    // 归还现有所有 xterm 节点到 warehouse 仓库
    saveTerminalsToWarehouse();

    setSshSessions(prev => [...prev, newSession]);

    if (sshViewLayout !== 'single') {
      // 如果处于分屏中，切回单屏（挂起当前组）
      setVisibleSessionIds([sessionId]);
      setSshViewLayout('single');
    } else {
      setVisibleSessionIds([sessionId]);
    }

    setActiveSSHSessionId(sessionId);
    setServerCurrentTab('terminal');

    // 延迟实例化 xterm.js
    setTimeout(() => initSessionTerminal(sessionId, newSession), 200);
  };

  // 切换标签
  const loadDockerContainerLogs = async (server, container, tail = 200) => {
    const serverId = server?.id || server;
    if (!serverId || !container) return;

    const containerId = getDockerContainerId(container);
    const containerName = getDockerContainerName(container);

    setDockerLogsLoading(true);
    setDockerLogsContent('\u6b63\u5728\u8fde\u63a5\u4e3b\u673a\u5e76\u83b7\u53d6\u5bb9\u5668\u65e5\u5fd7...\n');

    try {
      const params = new URLSearchParams();
      params.set('tail', String(Number(tail) || 200));
      const res = await fetch(`/api/server/v2/docker/${encodeURIComponent(serverId)}/containers/${encodeURIComponent(containerId)}/logs?${params.toString()}`, {
        cache: 'no-store',
      });
      const text = await res.text();
      if (!res.ok) {
        let message = text;
        try {
          const payload = JSON.parse(text);
          message = payload.error || payload.message || message;
        } catch (_) { }
        throw new Error(message || '\u83b7\u53d6\u65e5\u5fd7\u5931\u8d25');
      }
      setDockerLogsContent(text || '\u6ca1\u6709\u65e5\u5fd7\u8f93\u51fa');
      setDockerLogsLoading(false);
    } catch (err) {
      setDockerLogsContent(`\u9519\u8bef\u003a\u0020${err.message || '\u670d\u52a1\u5f02\u5e38'}`);
      setDockerLogsLoading(false);
    }
  };

  const switchToSSHTab = (sessionId) => {
    saveTerminalsToWarehouse();

    setServerCurrentTab('terminal');
    setActiveSSHSessionId(sessionId);
    if (showSftpSidebar) syncSftpToSession(sessionId);

    if (sshGroupState && sshGroupState.ids.includes(sessionId)) {
      // 如果属于原分屏组，恢复该分屏状态
      const knownSessionIds = new Set(sshSessions.map(session => session.id));
      const groupIds = sshGroupState.ids.filter(id => knownSessionIds.has(id));
      if (groupIds.length > 1 && groupIds.includes(sessionId)) {
        const nextLayout = getTerminalLayoutForSessionIds(groupIds, sshGroupState.layout);
        const nextSide = nextLayout === 'single' ? '' : sshGroupState.side;
        visibleSessionIdsRef.current = groupIds;
        setVisibleSessionIds(groupIds);
        setSshViewLayout(nextLayout);
        setSshSplitSide(nextSide);
        setSshGroupState({ ids: groupIds, layout: nextLayout, side: nextSide });
      } else {
        visibleSessionIdsRef.current = [sessionId];
        setVisibleSessionIds([sessionId]);
        setSshViewLayout('single');
        setSshSplitSide('');
        setSshGroupState(null);
      }
    } else {
      visibleSessionIdsRef.current = [sessionId];
      setVisibleSessionIds([sessionId]);
      setSshViewLayout('single');
      setSshSplitSide('');
    }

    setTimeout(() => {
      syncTerminalDOM();
      const instance = sshSessionRefs.current[sessionId];
      if (instance?.terminal) instance.terminal.focus();
    }, 100);
  };

  const activateSSHSession = (sessionId) => {
    setActiveSSHSessionId(sessionId);
    if (showSftpSidebar) syncSftpToSession(sessionId);
    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[sessionId]?.terminal?.focus();
    }, 80);
  };

  const syncSftpToSession = (sessionId) => {
    const session = sshSessions.find(item => item.id === sessionId);
    const serverId = session?.server?.id;
    if (!serverId) return;
    const lastPath = sftpPathByServerRef.current[serverId] || '.';
    if (sftpServerId === serverId && sftpCurrentPath === lastPath) return;
    setSftpServerId(serverId);
    setSftpCurrentPath(lastPath);
  };

  const sendTerminalCommand = (sessionId, command, options = {}) => {
    const text = String(command || '').trim();
    if (!sessionId || !text) return;

    const targetIds = Array.isArray(options.targetSessionIds) && options.targetSessionIds.length > 0
      ? options.targetSessionIds
      : [sessionId];
    const appendNewline = options.appendNewline !== false;
    const payload = appendNewline ? `${text}\r` : text;
    const sendToSession = (targetId) => {
      const target = sshSessionRefs.current[targetId];
      if (target?.ws?.readyState === WebSocket.OPEN) {
        target.ws.send(JSON.stringify({ type: 'input', data: payload }));
        target.terminal?.focus();
      }
    };

    targetIds.forEach(sendToSession);
    if (targetIds.length === 1 && appendNewline && sshSyncEnabledRef.current && visibleSessionIdsRef.current.includes(sessionId)) {
      visibleSessionIdsRef.current.forEach(targetId => {
        if (targetId !== sessionId) sendToSession(targetId);
      });
    }
  };

  const runQuickCommand = (command, options = {}) => {
    sendTerminalCommand(activeSSHSessionId, command, options);
  };

  const getTerminalLayoutForSessionIds = (ids, preferredLayout = sshViewLayout) => {
    if (ids.length <= 1) return 'single';
    if (ids.length > 2) return 'grid';
    return preferredLayout === 'split-v' ? 'split-v' : 'split-h';
  };

  const fitTerminalSession = (sessionId, notifyBackend = true) => {
    const inst = sshSessionRefs.current[sessionId];
    const termEl = terminalDOMElements.current[sessionId];
    if (!inst?.fit || !inst?.terminal || !termEl) return;

    const rect = termEl.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0 || termEl.parentElement === warehouseRef.current) return;

    const prevCols = inst.terminal.cols;
    const prevRows = inst.terminal.rows;
    try {
      inst.fit.fit();
      inst.terminal?.refresh?.(0, Math.max(0, inst.terminal.rows - 1));
    } catch (e) {
      return;
    }

    const cols = inst.terminal.cols;
    const rows = inst.terminal.rows;
    const sizeChanged = prevCols !== cols || prevRows !== rows;
    const backendSizeChanged = inst.lastResizeCols !== cols || inst.lastResizeRows !== rows;
    if (notifyBackend && (sizeChanged || backendSizeChanged) && inst.ws?.readyState === WebSocket.OPEN) {
      inst.lastResizeCols = cols;
      inst.lastResizeRows = rows;
      inst.ws.send(JSON.stringify({ type: 'resize', cols, rows }));
    }
  };

  const scheduleTerminalFit = (sessionId, delay = 60) => {
    if (!sessionId) return;
    if (terminalResizeTimers.current[sessionId]) {
      clearTimeout(terminalResizeTimers.current[sessionId]);
    }
    terminalResizeTimers.current[sessionId] = setTimeout(() => {
      delete terminalResizeTimers.current[sessionId];
      fitTerminalSession(sessionId);
    }, delay);
  };


  const saveTerminalsToWarehouse = () => {
    const warehouse = warehouseRef.current;
    if (!warehouse) return;

    sshSessions.forEach(session => {
      const el = terminalDOMElements.current[session.id];
      if (el && el.parentElement !== warehouse) {
        warehouse.appendChild(el);
      }
    });
  };

  const getTerminalPtyId = (session) => (
    session?.sessionMeta?.attachToPtyId ||
    session?.sessionMeta?.ptyId ||
    session?.attachToPtyId ||
    session?.ptyId ||
    session?.id ||
    ''
  );

  const createSSHSocket = (sessionId, sessionMeta, terminal) => {
    if (typeof WebSocket !== 'undefined') {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const params = new URLSearchParams({
        server_id: String(sessionMeta.server.id),
        session_id: sessionId,
        transport: resolveTerminalSocketTransport(
			sessionMeta.server,
			sessionMeta.server.preferred_terminal_transport || sessionMeta.type || 'auto',
		),
        cols: String(sshSessionRefs.current[sessionId]?.terminal?.cols || 120),
        rows: String(sshSessionRefs.current[sessionId]?.terminal?.rows || 32),
      });
      if (sessionMeta.attachToPtyId || sessionMeta.ptyId) {
        params.set('pty_id', sessionMeta.attachToPtyId || sessionMeta.ptyId);
        params.set('attach', '1');
      }
      if (sessionMeta.containerName) {
        params.set('container', sessionMeta.containerName);
      }
      const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh?${params.toString()}`);

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data);
          if (message.type === 'data') {
            const inst = sshSessionRefs.current[sessionId];
            if (inst) inst.hasTerminalData = true;
            terminal.write(message.data || '');
          } else if (message.type === 'status' && (message.data === 'connected' || message.data === 'connected_legacy' || message.data === 'attached')) {
            const connectedTransport = message.transport || sessionMeta.server.preferred_terminal_transport || sessionMeta.type || 'ssh';
            const inst = sshSessionRefs.current[sessionId];
            if (inst && !inst.connectionBannerCleared && !inst.hasTerminalData) {
              terminal.clear();
              inst.connectionBannerCleared = true;
              scheduleTerminalFit(sessionId, 30);
            }
            if (inst) {
              inst.connected = true;
              inst.transport = connectedTransport;
            }
            setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: true, transport: connectedTransport } : s));
          } else if (message.type === 'error') {
            terminal.writeln(`\r\n\x1b[1;31m${message.data || 'SSH connection failed'}\x1b[0m`);
          }
        } catch {
          terminal.write(String(event.data || ''));
        }
      };
      ws.onerror = () => {
        terminal.writeln('\r\n\x1b[1;31mSSH 终端连接失败。\x1b[0m');
      };
      ws.onclose = () => {
        const inst = sshSessionRefs.current[sessionId];
        if (inst) inst.connected = false;
        setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
        terminal.writeln('\r\n\x1b[1;33mSSH 终端连接已关闭。\x1b[0m');
      };
      return ws;
    }

    terminal.writeln('\n\x1b[1;33mSSH 终端 WebSocket 已随 Node 后端迁移退役，当前 Go 后端仅保留 Agent /socket.io/ 实时通道。\x1b[0m');
    setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
    return {
      readyState: WebSocket.CLOSED,
      send: () => { },
      close: () => { },
      onclose: null,
      onerror: null,
    };
  };


  const initSessionTerminal = (sessionId, sessionMeta) => {
    if (sshSessionRefs.current[sessionId]) return;


    const container = document.createElement('div');
    container.id = 'ssh-terminal-' + sessionId;
    container.className = 'app-terminal-surface h-full w-full overflow-hidden';
    terminalDOMElements.current[sessionId] = container;
    if (!warehouseRef.current) return;
    warehouseRef.current.appendChild(container);
    const terminalTheme = getKumoTerminalTheme();

    const terminal = new Terminal({
      cursorBlink: true,
      cursorStyle: 'bar',
      fontSize: 14,
      fontFamily: 'Consolas, "Courier New", monospace',
      theme: terminalTheme,
      scrollback: 10000,
      allowProposedApi: true
    });

    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());

    terminal.open(container);
    terminal.writeln(`\x1b[1;33m正在尝试建立与 ${sessionMeta.server.name} 的连接...\x1b[0m`);

    const resizeObserver = typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(() => scheduleTerminalFit(sessionId, 80))
      : null;
    if (resizeObserver) resizeObserver.observe(container);

    sshSessionRefs.current[sessionId] = {
      id: sessionId,
      terminal,
      fit: fitAddon,
      ws: null,
      connected: false,
      sessionMeta,
      inputDisposable: null,
      heartbeatInterval: null,
      resizeObserver,
      lastResizeCols: 0,
      lastResizeRows: 0,
      lastSeq: 0,
      connectionBannerCleared: false,
      hasTerminalData: false
    };

    const ws = createSSHSocket(sessionId, sessionMeta, terminal);
    sshSessionRefs.current[sessionId].ws = ws;

    const inputDisposable = terminal.onData(data => {
      const currentWs = sshSessionRefs.current[sessionId]?.ws;
      if (currentWs?.readyState === WebSocket.OPEN) {
        currentWs.send(JSON.stringify({ type: 'input', data }));
      }

      if (sshSyncEnabledRef.current && visibleSessionIdsRef.current.includes(sessionId)) {
        const sourcePtyId = getTerminalPtyId(sshSessionRefs.current[sessionId]);
        visibleSessionIdsRef.current.forEach(targetId => {
          if (targetId === sessionId) return;
          const targetSession = sshSessionRefs.current[targetId];
          if (sourcePtyId && getTerminalPtyId(targetSession) === sourcePtyId) return;
          if (targetSession?.ws && targetSession.ws.readyState === WebSocket.OPEN) {
            targetSession.ws.send(JSON.stringify({ type: 'input', data }));
          }
        });
      }
    });
    sshSessionRefs.current[sessionId].inputDisposable = inputDisposable;

    // 初始化时同步 DOM
    setTimeout(() => {
      syncTerminalDOM();
      scheduleTerminalFit(sessionId, 40);
    }, 100);
  };

  // 重新计算并挂载活动终端到 static slots 静态槽位上
  const syncTerminalDOM = () => {
    const slots = visibleSessionIdsRef.current;
    slots.forEach((id, index) => {
      const slotDiv = document.getElementById(`ssh-slot-idx-${index}`);
      const termEl = terminalDOMElements.current[id];
      if (slotDiv && termEl && termEl.parentElement !== slotDiv) {
        slotDiv.appendChild(termEl);
      }
      if (slotDiv && termEl && termEl.parentElement === slotDiv) {
        const instance = sshSessionRefs.current[id];
        try {
          instance?.terminal?.refresh?.(0, Math.max(0, (instance.terminal?.rows || 1) - 1));
        } catch (e) {
          // xterm can throw while its viewport is between detached and attached states.
        }
        scheduleTerminalFit(id, 40);
      }
    });
  };

  const restoreVisibleTerminalSurfaces = ({ focus = false } = {}) => {
    syncTerminalDOM();
    visibleSessionIdsRef.current.forEach(id => {
      const instance = sshSessionRefs.current[id];
      if (!instance?.terminal) return;
      fitTerminalSession(id, false);
      try {
        instance.terminal.refresh?.(0, Math.max(0, instance.terminal.rows - 1));
      } catch (e) {
        // Safe no-op while the terminal viewport is being reparented.
      }
    });
    if (focus && activeSSHSessionId) {
      sshSessionRefs.current[activeSSHSessionId]?.terminal?.focus();
    }
  };

  // 关闭终端会话
  const closeSSHSession = (sessionId) => {
    saveTerminalsToWarehouse();

    const session = sshSessionRefs.current[sessionId];
    if (session) {
      if (terminalResizeTimers.current[sessionId]) {
        clearTimeout(terminalResizeTimers.current[sessionId]);
        delete terminalResizeTimers.current[sessionId];
      }
      if (session.heartbeatInterval) clearInterval(session.heartbeatInterval);
      if (session.inputDisposable) session.inputDisposable.dispose();
      if (session.resizeObserver) session.resizeObserver.disconnect();
      if (session.ws) {
        if (session.ws.readyState === WebSocket.OPEN) {
          session.ws.send(JSON.stringify({ type: 'disconnect' }));
        }
        session.ws.close();
      }
      if (session.terminal) session.terminal.dispose();
      delete sshSessionRefs.current[sessionId];
    }

    const termEl = terminalDOMElements.current[sessionId];
    if (termEl) {
      termEl.remove();
      delete terminalDOMElements.current[sessionId];
    }

    const remaining = sshSessions.filter(s => s.id !== sessionId);
    const remainingIds = new Set(remaining.map(s => s.id));
    const remainsVisible = visibleSessionIds.filter(id => id !== sessionId && remainingIds.has(id));
    const nextActiveId = activeSSHSessionId === sessionId
      ? (remainsVisible[remainsVisible.length - 1] || remaining[remaining.length - 1]?.id || '')
      : activeSSHSessionId;
    const nextVisibleIds = remainsVisible.length > 0 ? remainsVisible : (nextActiveId ? [nextActiveId] : []);
    const nextLayout = getTerminalLayoutForSessionIds(nextVisibleIds, sshViewLayout);
    const nextSide = nextLayout === 'single' ? '' : sshSplitSide;

    setSshSessions(remaining);
    visibleSessionIdsRef.current = nextVisibleIds;
    setVisibleSessionIds(nextVisibleIds);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(nextVisibleIds.length > 1 ? { ids: nextVisibleIds, layout: nextLayout, side: nextSide } : null);

    if (remaining.length === 0) {
      setActiveSSHSessionId('');
      setServerCurrentTab('list');
    } else {
      setActiveSSHSessionId(nextActiveId);
      setServerCurrentTab('terminal');
      if (showSftpSidebar && nextActiveId) syncSftpToSession(nextActiveId);
      setTimeout(() => {
        syncTerminalDOM();
        sshSessionRefs.current[nextActiveId]?.terminal?.focus();
      }, 100);
    }
  };

  const removeSSHSessionFromView = (sessionId) => {
    if (visibleSessionIds.length <= 1) {
      closeSSHSession(sessionId);
      return;
    }

    const nextVisibleIds = visibleSessionIds.filter(id => id !== sessionId);
    const nextLayout = getTerminalLayoutForSessionIds(nextVisibleIds, sshViewLayout);
    const nextSide = nextLayout === 'single' ? '' : sshSplitSide;
    const nextActiveId = activeSSHSessionId === sessionId
      ? (nextVisibleIds[nextVisibleIds.length - 1] || '')
      : activeSSHSessionId;

    visibleSessionIdsRef.current = nextVisibleIds;
    setVisibleSessionIds(nextVisibleIds);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(nextVisibleIds.length > 1 ? { ids: nextVisibleIds, layout: nextLayout, side: nextSide } : null);
    setActiveSSHSessionId(nextActiveId);
    if (showSftpSidebar && nextActiveId) syncSftpToSession(nextActiveId);

    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[nextActiveId]?.terminal?.focus();
    }, 100);
  };

  const toggleTerminalSidebar = (sidebar, options = {}) => {
    setActiveTerminalSidebar((current) => {
      const next = current === sidebar ? null : sidebar;
      if (next === 'sftp' && options.serverId) {
        setSftpServerId(options.serverId);
        setSftpCurrentPath(sftpPathByServerRef.current[options.serverId] || '.');
      }
      return next;
    });
  };

  // 重新连接
  const reconnectSSHSession = (sessionId) => {
    const session = sshSessionRefs.current[sessionId];
    if (!session) return;

    if (session.heartbeatInterval) {
      clearInterval(session.heartbeatInterval);
      session.heartbeatInterval = null;
    }
    if (session.ws) {
      session.ws.onclose = null;
      session.ws.close();
    }

    session.terminal.writeln(`\r\n\x1b[1;33m正在恢复终端会话...\x1b[0m`);
    setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));

    const nextWs = createSSHSocket(sessionId, session.sessionMeta, session.terminal);
    session.ws = nextWs;
  };

  // -------------------- 拖拽放置分屏逻辑 --------------------
  const handleTerminalDragStart = (event, id) => {
    if (event?.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      event.dataTransfer.setData('text/plain', id);
    }
    setDraggedSessionId(id);
    setDropTargetId(null);
    setDropHint('');
  };

  const handleTerminalDragOver = (e, targetId) => {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    const sourceId = draggedSessionId || e.dataTransfer?.getData('text/plain') || '';
    if (!sourceId || sourceId === targetId) {
      setDropTargetId(null);
      setDropHint('');
      return;
    }
    setDropTargetId(targetId);
    setDropHint(getDropPosition(e));
  };

  const handleTerminalDragLeave = (e, targetId) => {
    const nextTarget = e.relatedTarget;
    if (nextTarget instanceof Node && e.currentTarget.contains(nextTarget)) return;
    if (dropTargetId === targetId) {
      setDropTargetId(null);
      setDropHint('');
    }
  };

  const getSplitSourceSessionId = (targetId) => {
    if (draggedSessionId && draggedSessionId !== targetId) return draggedSessionId;
    if (activeSSHSessionId && activeSSHSessionId !== targetId) return activeSSHSessionId;
    return (
      sshSessions.find(session => session.id !== targetId && !visibleSessionIds.includes(session.id))?.id ||
      sshSessions.find(session => session.id !== targetId)?.id ||
      ''
    );
  };

  const getDropPosition = (event) => {
    const rect = event.currentTarget.getBoundingClientRect();
    const x = rect.width ? (event.clientX - rect.left) / rect.width : 0.5;
    const y = rect.height ? (event.clientY - rect.top) / rect.height : 0.5;
    if (y < 0.22) return 'top';
    if (y > 0.78) return 'bottom';
    return x < 0.5 ? 'left' : 'right';
  };

  const getTerminalDropPreviewStyle = (position) => {
    const gap = '0.5rem';
    switch (position) {
      case 'left':
        return { left: gap, right: '50%', top: gap, bottom: gap };
      case 'right':
        return { left: '50%', right: gap, top: gap, bottom: gap };
      case 'top':
        return { left: gap, right: gap, top: gap, bottom: '50%' };
      case 'bottom':
        return { left: gap, right: gap, top: '50%', bottom: gap };
      default:
        return { left: gap, right: gap, top: gap, bottom: gap };
    }
  };

  const updateDockerComposeDeployment = async () => {
    if (!dockerComposeEditor?.serverId || !dockerComposeEditor?.projectName || !dockerComposeEditor?.path) return;
    setDockerComposeEditor(prev => prev ? { ...prev, updating: true, error: '' } : prev);
    const payload = {
      serverId: dockerComposeEditor.serverId,
      project: dockerComposeEditor.projectName,
      config_file: dockerComposeEditor.configFiles?.join(', ') || dockerComposeEditor.path,
    };
    const result = await submitDockerTask('compose.update', payload, { silent: true });
    setDockerComposeEditor(prev => prev ? { ...prev, updating: false, saved: result?.ok ? false : prev.saved } : prev);
    if (result?.ok) {
      toast.success('编排已更新，并已强制拉取镜像');
      scheduleDockerResourceRefresh(500);
    }
  };

  const openRemoteDesktop = (server) => {
    if (!canOpenRemoteDesktop(server)) {
      toast.warning('远程桌面仅支持在线且已升级的 Windows Agent');
      return;
    }
    window.open(remoteDesktopPath(server.id), '_blank', 'noopener,noreferrer');
  };

  const getTerminalCopyBaseName = (session) => {
    const name = String(session?.name || session?.server?.name || '终端').trim();
    return name.replace(/\s+(?:共享|复制(?:\s*\d+)?)$/u, '') || '终端';
  };

  const getNextTerminalCopyName = (session) => {
    const baseName = getTerminalCopyBaseName(session);
    const pattern = new RegExp(`^${baseName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s+(?:共享|复制(?:\\s*(\\d+))?)$`, 'u');
    const copyNumbers = sshSessions
      .map(item => String(item.name || '').trim().match(pattern))
      .filter(Boolean)
      .map(match => Number(match[1] || 1));
    return `${baseName} 复制 ${Math.max(0, ...copyNumbers) + 1}`;
  };

  const createAttachedTerminalView = (targetId, position = 'right') => {
    const sourceSession = sshSessions.find(session => session.id === targetId);
    if (!sourceSession) return '';

    const sourceRef = sshSessionRefs.current[targetId];
    const attachToPtyId = getTerminalPtyId(sourceRef || sourceSession);
    if (!attachToPtyId) return '';

    const sessionId = `session_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`;
    const newSession = {
      ...sourceSession,
      id: sessionId,
      connected: false,
      attachToPtyId,
      name: getNextTerminalCopyName(sourceSession),
    };

    saveTerminalsToWarehouse();
    setSshSessions(prev => [...prev, newSession]);

    const currentVisibleIds = visibleSessionIds.length > 0 ? visibleSessionIds : [targetId];
    const updated = currentVisibleIds.filter(id => id !== sessionId);
    const idx = Math.max(updated.indexOf(targetId), 0);
    let nextLayout = 'single';
    let nextSide = '';

    if (position === 'left' || position === 'top') {
      updated.splice(idx, 0, sessionId);
    } else if (position === 'center') {
      updated[idx] = sessionId;
    } else {
      updated.splice(idx + 1, 0, sessionId);
    }

    if (position !== 'center') {
      nextLayout = updated.length > 2 ? 'grid' : (position === 'top' || position === 'bottom' ? 'split-v' : 'split-h');
      nextSide = position;
    }

    visibleSessionIdsRef.current = updated;
    setVisibleSessionIds(updated);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(updated.length > 1 ? { ids: updated, layout: nextLayout, side: nextSide } : null);
    setActiveSSHSessionId(sessionId);
    if (showSftpSidebar) syncSftpToSession(targetId);
    setDraggedSessionId(null);
    setDropTargetId(null);
    setDropHint('');

    setTimeout(() => initSessionTerminal(sessionId, newSession), 200);
    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[sessionId]?.terminal?.focus();
    }, 320);

    return sessionId;
  };

  const triggerSplitPane = (targetId, position, sourceId = getSplitSourceSessionId(targetId)) => {
    if (!sourceId || sourceId === targetId) {
      createAttachedTerminalView(targetId, position);
      return;
    }

    const currentVisibleIds = visibleSessionIds.length > 0 ? visibleSessionIds : [targetId];
    const updated = currentVisibleIds.filter(id => id !== sourceId);
    const idx = Math.max(updated.indexOf(targetId), 0);
    let nextLayout = 'single';
    let nextSide = '';

    if (position === 'center') {
      updated[idx] = sourceId;
    } else {
      if (position === 'left' || position === 'top') {
        updated.splice(idx, 0, sourceId);
      } else {
        updated.splice(idx + 1, 0, sourceId);
      }
      nextLayout = updated.length > 2 ? 'grid' : (position === 'top' || position === 'bottom' ? 'split-v' : 'split-h');
      nextSide = position;
    }

    visibleSessionIdsRef.current = updated;
    setVisibleSessionIds(updated);
    setSshViewLayout(nextLayout);
    setSshSplitSide(nextSide);
    setSshGroupState(updated.length > 1 ? { ids: updated, layout: nextLayout, side: nextSide } : null);
    setActiveSSHSessionId(sourceId);
    if (showSftpSidebar) syncSftpToSession(sourceId);
    setDraggedSessionId(null);
    setDropTargetId(null);
    setDropHint('');

    setTimeout(() => {
      syncTerminalDOM();
      sshSessionRefs.current[sourceId]?.terminal?.focus();
    }, 100);
  };

  // -------------------- 计算过滤列表 --------------------

  const filteredServers = useMemo(() => {
    let list = serverList;
    const normalizedFilter = serverStatusFilter === 'offline' ? 'offline' : 'all';
    if (normalizedFilter !== 'all') {
      list = list.filter(s => resolveServerDisplayStatus(s).state === normalizedFilter);
    }
    if (serverSearchText.trim()) {
      const query = serverSearchText.toLowerCase();
      list = list.filter(s =>
        s.name.toLowerCase().includes(query) ||
        (s.host && s.host.toLowerCase().includes(query)) ||
        (s.tags && s.tags.some(t => t.toLowerCase().includes(query)))
      );
    }
    return list;
  }, [serverList, serverStatusFilter, serverSearchText]);


  const statsSummary = useMemo(() => {
    const total = serverList.length;
    const counts = serverList.reduce((acc, server) => {
      const state = resolveServerDisplayStatus(server).state;
      acc[state] = (acc[state] || 0) + 1;
      return acc;
    }, {});
		const online = counts.online || 0;
    const interrupted = counts.interrupted || 0;
    const degraded = counts.degraded || 0;
    const offline = counts.offline || 0;
    return { total, online, interrupted, degraded, offline, warning: interrupted + degraded };
  }, [serverList]);

  const visibleCompactColumnDefs = useMemo(() => (
    HOST_COMPACT_COLUMNS.filter(column => compactVisibleColumns.includes(column.id))
  ), [compactVisibleColumns]);
  const visibleCompactColumnWidths = useMemo(() => (
    visibleCompactColumnDefs.map(column => HOST_COMPACT_COLUMN_WIDTHS[column.id] || 120)
  ), [visibleCompactColumnDefs]);

  const isCompactColumnVisible = (columnId) => compactVisibleColumns.includes(columnId);

  const toggleCompactColumn = (columnId, visible) => {
    const column = HOST_COMPACT_COLUMNS.find(item => item.id === columnId);
    if (!column || column.required) return;
    setCompactVisibleColumns(prev => {
      if (visible) return prev.includes(columnId) ? prev : [...prev, columnId];
      return prev.filter(id => id !== columnId);
    });
  };

  const showAllCompactColumns = () => setCompactVisibleColumns(HOST_COMPACT_COLUMN_IDS);

  const openCompactColumnMenu = (event) => {
    event.preventDefault();
    setCompactColumnMenu({
      open: true,
      x: Math.min(event.clientX, window.innerWidth - 224),
      y: Math.min(event.clientY, window.innerHeight - 360),
    });
  };

  const renderServerContextMenu = (server) => (
    <ContextMenu.Portal>
      <ContextMenu.Positioner sideOffset={6}>
        <ContextMenu.Popup className="z-50 min-w-40 overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5 text-kumo-default outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95">
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
						disabled={!isServerOnline(server) || server.loading}
            onClick={(event) => {
              event.stopPropagation();
              refreshServerInfo(server.id);
            }}
          >
            <RefreshCw className="h-4 w-4" />
            <span>刷新详情</span>
          </ContextMenu.Item>
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
            onClick={(event) => {
              event.stopPropagation();
              showAgentInstallModal(server.id);
            }}
          >
            <Shield className="h-4 w-4" />
            <span>部署 Agent</span>
          </ContextMenu.Item>
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
            onClick={(event) => {
              event.stopPropagation();
              openEditServerModal(server);
            }}
          >
            <Edit className="h-4 w-4" />
            <span>编辑主机</span>
          </ContextMenu.Item>
          <ContextMenu.Separator className="mx-1 my-1 h-px bg-kumo-line" />
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
						disabled={!isServerOnline(server)}
            onClick={(event) => {
              event.stopPropagation();
              runServerPowerAction(server.id, 'reboot');
            }}
          >
            <Reboot className="h-4 w-4" />
            <span>重启主机</span>
          </ContextMenu.Item>
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm text-kumo-danger outline-hidden select-none focus:text-kumo-danger focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-danger/5 data-highlighted:text-kumo-danger"
						disabled={!isServerOnline(server)}
            onClick={(event) => {
              event.stopPropagation();
              runServerPowerAction(server.id, 'shutdown');
            }}
          >
            <X className="h-4 w-4" />
            <span>关闭主机</span>
          </ContextMenu.Item>
          <ContextMenu.Separator className="mx-1 my-1 h-px bg-kumo-line" />
          <ContextMenu.Item
            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm text-kumo-danger outline-hidden select-none focus:text-kumo-danger focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-danger/5 data-highlighted:text-kumo-danger"
            onClick={(event) => {
              event.stopPropagation();
              deleteServer(server.id);
            }}
          >
            <Trash className="h-4 w-4" />
            <span>删除主机</span>
          </ContextMenu.Item>
        </ContextMenu.Popup>
      </ContextMenu.Positioner>
    </ContextMenu.Portal>
  );

  return (
    <div
      className={
        serverCurrentTab === 'terminal'
          ? 'flex h-full min-h-0 w-full min-w-0 flex-1 flex-col gap-3 overflow-visible'
          : 'flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4'
      }
    >
      <CompactColumnMenu
        menu={compactColumnMenu}
        visibleColumns={compactVisibleColumns}
        onToggle={toggleCompactColumn}
        onShowAll={showAllCompactColumns}
        onClose={() => setCompactColumnMenu(prev => ({ ...prev, open: false }))}
      />
      {/* 顶部标签导航 */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={serverCurrentTab}
          onValueChange={(value) => {
            if (serverCurrentTab === 'terminal' && value !== 'terminal') {
              saveTerminalsToWarehouse();
            }
            setServerCurrentTab(value);
            if (value === 'status-pages') loadServerStatusPages();
          }}
          tabs={[
            { value: 'list', label: <ServerModuleTabLabel icon={Server} short="主机">主机</ServerModuleTabLabel> },
            { value: 'docker', label: <ServerModuleTabLabel icon={Box} short="容器">容器</ServerModuleTabLabel> },
            { value: 'forward', label: <ServerModuleTabLabel icon={Shuffle} short="转发">端口转发</ServerModuleTabLabel> },
            { value: 'status-pages', label: <ServerModuleTabLabel icon={Globe} short="公开">公开</ServerModuleTabLabel> },
            { value: 'management', label: <ServerModuleTabLabel icon={Settings} short="管理">管理</ServerModuleTabLabel> },
            ...(sshSessions.length > 0
              ? [{
                value: 'terminal',
                label: <ServerModuleTabLabel icon={TerminalIcon} short="终端" badge={sshSessions.length}>终端</ServerModuleTabLabel>,
              }]
              : []),
          ]}
        />

        {/* 右侧快速连接 */}
        <div className="flex shrink-0 items-center gap-2">
          {serverCurrentTab === 'forward' && (
            <Button size="sm" variant="primary" onClick={() => forwardPanelRef.current?.openCreate()}>
              <Plus className="h-3.5 w-3.5" />
              创建转发规则
            </Button>
          )}
          {serverCurrentTab === 'list' && (
            <Toolbar size="sm" aria-label="导出导入主机配置" className="shrink-0">
              <Toolbar.Button onClick={exportServers} aria-label="导出主机配置" icon={<Upload className="h-3.5 w-3.5" />}>
                <span className="hidden cq-sm:inline">导出</span>
              </Toolbar.Button>
              <Toolbar.Button onClick={openImportServerModal} aria-label="导入主机配置" icon={<Download className="h-3.5 w-3.5" />}>
                <span className="hidden cq-sm:inline">导入</span>
              </Toolbar.Button>
            </Toolbar>
          )}
          <TabBarOverflowActions
          items={
            serverCurrentTab === 'list'
              ? [
                  {
                    key: 'upgrade-agent',
                    label: '升级 Agent',
                    title: '升级所有在线 Agent',
                    icon: <Upload className="w-3.5 h-3.5" />,
                    onClick: openUpgradeModal,
                  },
                  {
                    key: 'batch-deploy',
                    label: '批量部署',
                    title: '批量部署 Agent',
                    icon: <Shield className="w-3.5 h-3.5" />,
                    onClick: openBatchAgentModal,
                  },
                  {
                    key: 'refresh',
                    label: '刷新列表',
                    title: '刷新列表和地理位置',
                    icon: <RefreshCw className="w-3.5 h-3.5" />,
                    onClick: refreshServerLocationsAndList,
                    loading: serverLoading,
                  },
                  {
                    key: 'add',
                    label: '新增主机',
                    icon: <Plus className="w-3.5 h-3.5" />,
                    onClick: openAddServerModal,
                    variant: 'primary',
                  },
                ]
              : []
          }
        />
        </div>
      </div>

      {serverCurrentTab === 'status-pages' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(24rem,0.9fr)_minmax(0,1.1fr)]">
          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
              <div className="min-w-0">
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                  <Globe className="h-4 w-4" />
                  {serverStatusPageForm.id ? '编辑主机状态页' : '新建主机状态页'}
                </h3>
              </div>
              {serverStatusPageForm.id && (
                <Button size="sm" variant="secondary" shape="square" icon={<X className="h-3.5 w-3.5" />} onClick={resetServerStatusPageForm} aria-label="取消编辑" />
              )}
            </LayerCard.Secondary>

            <LayerCard.Primary className="space-y-4 p-4">
              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Input size="sm" label="名称" value={serverStatusPageForm.title} onChange={(event) => setServerStatusPageForm(prev => ({ ...prev, title: event.target.value, slug: prev.slug || normalizeServerStatusSlug(event.target.value) }))} placeholder="基础设施状态" />
                <Input size="sm" label="Slug" value={serverStatusPageForm.slug} onChange={(event) => setServerStatusPageForm(prev => ({ ...prev, slug: normalizeServerStatusSlug(event.target.value) }))} placeholder="infra" />
                <Input size="sm" label="自定义域名" value={serverStatusPageForm.domain} onChange={(event) => setServerStatusPageForm(prev => ({ ...prev, domain: normalizeServerStatusDomain(event.target.value) }))} placeholder="status.example.com" />
                <Input size="sm" label="缓存秒数" type="number" min="30" value={serverStatusPageForm.cacheSeconds} onChange={(event) => setServerStatusPageForm(prev => ({ ...prev, cacheSeconds: event.target.value }))} />
                <div className="cq-sm:col-span-2">
                  <Textarea size="sm" label="说明" value={serverStatusPageForm.description} onChange={(event) => setServerStatusPageForm(prev => ({ ...prev, description: event.target.value }))} placeholder="可选说明" rows={3} />
                </div>
              </div>

              <div className="grid gap-2 cq-sm:grid-cols-2">
                {[
                  ['public', '公开访问', '关闭后公开 API 和单页都会不可用。'],
                  ['hideHosts', '隐藏地址', '公开页不显示主机 IP 或连接地址。'],
                  ['showTraffic', '显示流量', '展示已用流量和流量上限。'],
                  ['showCharts', '历史指标', '公开接口下发最近指标历史。'],
                  ['showOnDashboard', '首页快捷卡片', '在仪表盘显示跳转到此状态页的快捷入口。'],
                ].map(([key, title, desc]) => (
                  <div key={key} className="flex items-center justify-between gap-3 rounded-md border border-kumo-line bg-kumo-recessed/30 p-3">
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-kumo-strong">{title}</div>
                      <div className="mt-1 text-xs text-kumo-subtle">{desc}</div>
                    </div>
                    <Switch checked={!!serverStatusPageForm[key]} onCheckedChange={(checked) => setServerStatusPageForm(prev => ({ ...prev, [key]: checked }))} />
                  </div>
                ))}
              </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between gap-2">
                <div className="text-sm font-semibold text-kumo-strong">绑定主机</div>
                <Button size="sm" variant="secondary" onClick={() => setServerStatusPageForm(prev => ({ ...prev, serverIds: serverList.map(item => item.id) }))} disabled={serverList.length === 0}>全选</Button>
              </div>
              <div className="max-h-64 overflow-y-auto rounded-lg border border-kumo-line bg-kumo-base p-2 scrollbar-thin">
                {serverList.length === 0 ? (
                  <div className="p-4 text-center text-xs text-kumo-subtle">暂无主机实例。</div>
                ) : (
                  <div className="grid gap-1.5">
                    {serverList.map((server) => (
                      <label key={server.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-kumo-recessed">
                        <Checkbox checked={serverStatusPageForm.serverIds.includes(server.id)} onCheckedChange={(checked) => toggleServerStatusPageServer(server.id, checked)} aria-label={`绑定 ${server.name}`} />
                        <span className="min-w-0 flex-1 truncate text-sm text-kumo-strong">{server.name}</span>
                        <span className="hidden max-w-[12rem] truncate font-mono text-[10px] text-kumo-subtle cq-sm:block">{server.host || server.id}</span>
                      </label>
                    ))}
                  </div>
                )}
              </div>
            </div>

            <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-xs text-kumo-subtle">
              <div className="font-semibold text-kumo-strong">预览地址</div>
              <div className="mt-2 space-y-1.5">
                {[getServerStatusPageUrl(serverStatusPageForm, 'servers'), getServerStatusPageUrl(serverStatusPageForm, 's'), getServerStatusDomainUrl(serverStatusPageForm)]
                  .filter(Boolean)
                  .map((url) => (
                    <ClipboardText
                      key={url}
                      size="sm"
                      text={url}
                      className="w-full"
                      tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }}
                      labels={{ copyAction: '复制状态页地址' }}
                    />
                  ))}
              </div>
            </div>

              <div className="flex flex-wrap justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={resetServerStatusPageForm}>重置</Button>
                <Button size="sm" variant="primary" loading={serverStatusPagesLoading} onClick={saveServerStatusPage} icon={<Save className="h-3.5 w-3.5" />}>{serverStatusPageForm.id ? '保存状态页' : '创建状态页'}</Button>
              </div>
            </LayerCard.Primary>
          </LayerCard>

          <LayerCard className="overflow-hidden p-0">
            <LayerCard.Secondary className={SERVER_SECTION_HEADER_CLASS}>
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-kumo-strong"><Globe className="h-4 w-4" />已发布状态页</h3>
              </div>
              <Button size="sm" variant="secondary" icon={<RotateCw className="h-3.5 w-3.5" />} onClick={loadServerStatusPages} loading={serverStatusPagesLoading}>刷新</Button>
            </LayerCard.Secondary>

            <LayerCard.Primary className="p-4">
              {serverStatusPages.length === 0 ? (
              <div className="flex min-h-56 flex-col items-center justify-center rounded-lg border border-dashed border-kumo-line text-center text-sm text-kumo-subtle">
                <Globe className="mb-3 h-8 w-8 opacity-40" />
                暂无主机状态页。
              </div>
            ) : (
              <div className="grid gap-3">
                {serverStatusPages.map((page) => {
                  const statusUrl = getServerStatusPageUrl(page, 'servers');
                  const compactUrl = getServerStatusPageUrl(page, 's');
                  const domainUrl = getServerStatusDomainUrl(page);
                  return (
                    <div key={page.id} className="rounded-lg border border-kumo-line bg-kumo-base p-3">
                      <div className="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-start cq-sm:justify-between">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand">
                              <PublicPageBrandIcon pageKind="server" config={page.config} iconClassName="h-4 w-4" customIconClassName="h-4 w-4" />
                            </span>
                            <span className="truncate text-sm font-bold text-kumo-strong">{page.title || page.slug}</span>
                            <span className={`rounded px-2 py-0.5 text-[10px] font-semibold ${page.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>{page.public ? '公开' : '私有'}</span>
                            <span className="rounded bg-kumo-recessed px-2 py-0.5 font-mono text-[10px] text-kumo-subtle">{page.cacheSeconds || 300}s</span>
                          </div>
                          <div className="mt-1 truncate font-mono text-xs text-kumo-subtle">{page.slug}</div>
                          {page.description && <div className="mt-2 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{page.description}</div>}
                        </div>
                        <div className="flex shrink-0 flex-wrap gap-2">
                          <Button size="sm" variant="secondary" shape="square" icon={<Edit className="h-3.5 w-3.5" />} onClick={() => editServerStatusPage(page)} aria-label="编辑主机状态页" />
                          <Button size="sm" variant="secondary" shape="square" icon={<ExternalLink className="h-3.5 w-3.5" />} onClick={() => window.open(statusUrl, '_blank', 'noopener,noreferrer')} aria-label="打开主机状态页" />
                          <Button size="sm" variant="secondary" shape="square" icon={<Copy className="h-3.5 w-3.5" />} onClick={() => copyServerStatusUrl(statusUrl)} aria-label="复制主机状态页地址" />
                          <Button size="sm" variant={isArmed(`status-page.delete::${page.id}`) ? 'destructive' : 'secondary-destructive'} shape="square" icon={<Trash className="h-3.5 w-3.5" />} onClick={() => deleteServerStatusPage(page)} aria-label="删除主机状态页" />
                        </div>
                      </div>
                      <div className="mt-3 grid gap-2">
                        {[statusUrl, compactUrl, domainUrl].filter(Boolean).map((url) => (
                          <ClipboardText
                            key={url}
                            size="sm"
                            text={url}
                            className="w-full"
                            tooltip={{ text: '复制地址', copiedText: '地址已复制', side: 'top' }}
                            labels={{ copyAction: '复制状态页地址' }}
                          />
                        ))}
                      </div>
                    </div>
                  );
                })}
              </div>
              )}
            </LayerCard.Primary>
          </LayerCard>
        </div>
      )}

      {/* ==================== 端口转发 ==================== */}
      {serverCurrentTab === 'forward' && (
        <ForwardPanel ref={forwardPanelRef} />
      )}

      {/* ==================== 1. 主机管理 ==================== */}
      {serverCurrentTab === 'list' && (
        <div className="flex flex-col gap-4">
          {/* 控制过滤器栏 */}
          <div className={SERVER_SECONDARY_BAR_CLASS}>
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                className="w-auto cq-sm:w-fit"
                listClassName="w-auto cq-sm:w-fit"
                value={serverStatusFilter === 'offline' ? 'offline' : 'all'}
                onValueChange={setServerStatusFilter}
                tabs={[
                  { value: 'all', label: `全部 (${statsSummary.total})` },
                  { value: 'offline', label: `离线 (${statsSummary.offline})` },
                ]}
              />
              <Tabs
                {...TOOL_TABS_PROPS}
                className="w-auto cq-sm:w-fit"
                listClassName="w-auto cq-sm:w-fit"
                value={serverListViewMode}
                onValueChange={setServerListViewMode}
                tabs={[
                  { value: 'cards', label: <span title="卡片视图" aria-label="卡片视图"><LayoutDashboard className="h-3.5 w-3.5" /></span> },
                  { value: 'compact', label: <span title="表格视图" aria-label="表格视图"><Menu className="h-3.5 w-3.5" /></span> },
                ]}
              />
              <Button
                size="sm"
                shape="square"
                variant="secondary"
                className={serverMapOpen ? 'border-brand/50 text-brand' : ''}
                icon={<Globe className="h-3.5 w-3.5" />}
                aria-label={serverMapOpen ? '切回主机列表' : '切换到主机地图'}
                title={serverMapOpen ? '切回主机列表' : '切换到主机地图'}
                onClick={() => setServerMapOpen(prev => !prev)}
              />
            </div>

            <ResponsiveSearchInput
              value={serverSearchText}
              onChange={e => setServerSearchText(e.target.value)}
              placeholder="搜索主机名称、IP 或标签..."
              ariaLabel="搜索主机"
              className="cq-lg:w-72"
            />
          </div>

          {/* 列表渲染 */}
          {serverMapOpen ? (
            serverLoading && filteredServers.length === 0 ? (
              <div className="w-full overflow-hidden rounded-lg border border-kumo-line/70 bg-kumo-base" style={{ height: 'calc(100vh - 260px)' }}>
                <SkeletonLine className="h-full w-full rounded-none" />
              </div>
            ) : (
              <ServerLocationMap
                echarts={echarts}
                servers={filteredServers}
                resolveStatus={(server) => resolveServerDisplayStatus(server).state}
                title="主机地图"
                subtitle="当前筛选中的主机地理分布"
                height="calc(100vh - 260px)"
              />
            )
          ) : serverLoading && serverList.length === 0 ? (
            <AppCard padding="none" className="flex flex-col items-center justify-center gap-2 p-12 text-kumo-subtle">
              <Loader size={24} />
              <p className="text-xs">正在加载主机列表...</p>
            </AppCard>
          ) : filteredServers.length === 0 ? (
            <AppCard padding="none" className="flex flex-col items-center justify-center gap-1.5 p-16 text-kumo-subtle">
              <span className="text-xl">🔍</span>
              <p className="text-xs">未找到符合当前条件的主机节点</p>
            </AppCard>
          ) : (
            <div className="flex flex-col gap-3">
              {serverListViewMode === 'compact' ? (
                <div className="w-full max-w-full overflow-hidden rounded-md border border-kumo-interact/60 bg-kumo-base">
                  <ScrollableTable
                    layout="fixed"
                    widths={visibleCompactColumnWidths}
                    className="text-xs [&_td]:border-kumo-interact/45 [&_th]:border-kumo-interact/50"
                    wrapperClassName="w-full max-w-full overflow-x-auto scrollbar-thin"
                  >
                    <colgroup>
                      {visibleCompactColumnWidths.map((width, index) => (
                        <col
                          key={`${visibleCompactColumnDefs[index]?.id || index}-${width}`}
                          style={HOST_COMPACT_ADAPTIVE_COLUMNS.has(visibleCompactColumnDefs[index]?.id) ? undefined : { width }}
                        />
                      ))}
                    </colgroup>
                    <Table.Header sticky variant="compact">
                      <Table.Row onContextMenu={openCompactColumnMenu}>
                        {visibleCompactColumnDefs.map(column => (
                          <Table.Head
                            key={column.id}
                            sticky={column.id === 'actions' ? 'right' : undefined}
                            className={`!px-[2px] !py-2 text-center text-[10px] whitespace-nowrap ${column.id === 'actions' ? `!pl-[1px] !pr-[2px] ${COMPACT_STICKY_ACTION_CLASS}` : ''}`}
                          >
                            <div className={`mx-auto flex items-center ${HOST_COMPACT_HEADER_BOX_CLASS[column.id] || 'justify-center'}`}>
                              {column.label}
                            </div>
                          </Table.Head>
                        ))}
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {filteredServers.map(server => {
                        const country = getFlagCountry(server);
                        const locationText = getServerLocationText(server);
                        const locationTitle = getServerLocationTitle(server);
                        const isExpanded = expandedServers.includes(server.id);
                        const shouldRenderExpandedRow = isExpanded || renderedCompactExpandedServers.includes(server.id);
                        const isChartSeriesReady = chartSeriesReadyServers.includes(server.id);
                        const isDarkMode = theme === 'dark';
                        const {
                          records,
                          cpuColor,
                          memColor,
                          cpuTempColor,
                          gpuColor,
                          vramColor,
                          powerColor,
                          gpuTempColor,
                          diskColor,
                          txColor,
                          rxColor,
                          cpuMemSeries,
                          gpuSeries,
                          netSeries,
                        } = getServerMetricDisplay(server.id, server.metricsCache, isExpanded && isChartSeriesReady, isDarkMode);
                        const hasGpuData = !!getGpuModelText(server.info?.gpu) || records.some(r => (
                          (r.gpu_usage !== null && r.gpu_usage !== undefined && toNumber(r.gpu_usage, 0) > 0)
                          || getGpuTemp(r) > 0
                        ));
                        const tx = parseSpeed(server.info?.network?.tx_speed);
                        const rx = parseSpeed(server.info?.network?.rx_speed);
                        const txTotal = getByteParts(server.info?.network?.tx_total);
                        const rxTotal = getByteParts(server.info?.network?.rx_total);
                        const trafficQuota = getTrafficQuota(server);
                        const disk = server.info?.disk?.[0] || {};
                        const diskUsage = clampPercent(toNumber(disk.usage, 0));
                        const memUsage = clampPercent(toNumber(server.info?.memory?.Usage, 0));
                        const cpuUsage = clampPercent(toNumber(server.info?.cpu?.Usage, 0));
                        const cpuTemp = toNumber(server.info?.cpu?.Temp, 0);
                        const terminalProtocol = resolveTerminalProtocol(server);
                        const effectiveTerminalProtocol = terminalProtocol || (hasSshEndpoint(server) ? 'ssh' : null);
                        const terminalLabel = effectiveTerminalProtocol === 'agent' ? 'Agent 隧道终端' : 'SSH 终端';
                        const chartLoading = !!server.metricsLoading && records.length === 0;
                        const physicalCores = server.info?.cpu?.PhysicalCores || server.info?.cpu?.Cores;
                        const logicalCores = server.info?.cpu?.LogicalCores;
                        const coreText = physicalCores && logicalCores && physicalCores !== logicalCores
                          ? `${physicalCores}核 / ${logicalCores}线程`
                          : `${physicalCores || '-'}核`;
                        const dockerSummary = summarizeDockerContainers(server.info?.docker, getDockerContainerState);
                        const lifecycle = getServerLifecycle(server);
                        const networkQuality = networkQualityByServer[server.id] || {};
                        const networkQualitySeries = isExpanded ? buildNetworkQualitySeries(networkQuality, isDarkMode) : [];
                        const hasNetworkQualityData = isExpanded && networkQualitySeries.some(series => series.data.length > 0);
                        const networkQualityUnsupported = isExpanded && (
                          !!networkQuality.unsupported
                          || isNetworkQualityUnsupportedError(networkQuality.error || networkQuality.unsupportedMessage)
                        );
                        const metricsHealth = resolveServerMetricsHealth(server);
                        const rowMuted = !isServerOnline(server);

                        return (
                          <React.Fragment key={server.id}>
                            <ContextMenu.Root>
                              <ContextMenu.Trigger
                                render={(
                                  <Table.Row
                                    variant={isExpanded ? 'selected' : 'default'}
                                    className="cursor-pointer border-b border-kumo-line/80 hover:bg-kumo-recessed/15"
                                    onClick={() => toggleServerExpand(server.id)}
                                  >
                                    {isCompactColumnVisible('status') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <Badge
                                          variant={metricsHealth.variant}
                                          appearance="dot"
                                          title={metricsHealth.stale ? 'Agent 连接存在，但最近未收到有效指标上报' : undefined}
                                        >
                                          {metricsHealth.label}
                                        </Badge>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('name') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <div className={`mx-auto flex w-[96px] items-center gap-2 ${rowMuted ? 'text-kumo-subtle' : ''}`}>
                                          <i className={`${getOSIconClass(server.info?.platform)} ${rowMuted ? 'opacity-60 grayscale' : ''}`}></i>
                                          <div className="min-w-0">
                                            <div className={`truncate font-bold ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`} title={server.name}>{server.name}</div>
                                            {/* {(server.tags || []).filter(t => t !== 'Agent').length > 0 && (
                                        <div className="flex min-w-0 gap-1">
                                          {(server.tags || []).filter(t => t !== 'Agent').slice(0, 2).map(tag => (
                                            <span key={tag} className="truncate rounded bg-kumo-recessed/60 px-1 text-[9px] font-bold text-kumo-subtle" title={tag}>
                                              {tag}
                                            </span>
                                          ))}
                                        </div>
                                      )} */}
                                          </div>
                                        </div>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('country') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <div className="mx-auto flex w-[55px] items-center justify-center gap-1.5">
                                          {locationText ? (
                                            <>
                                              {country && <CountryFlag preferSvg countryCode={country} className={`h-3.5 w-5 shrink-0 !rounded-[2px] text-sm ${rowMuted ? 'opacity-60 grayscale' : ''}`} />}
                                              <span className={`truncate font-semibold uppercase ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`} title={locationTitle}>{locationText}</span>
                                            </>
                                          ) : (
                                            <span className="font-semibold text-kumo-subtle">-</span>
                                          )}
                                        </div>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('uptime') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <span className={`inline-flex w-[55px] justify-center font-semibold tabular-nums ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'}`}>
                                          {formatUptimeDaysOnly(server.info?.uptime || server.info?.system?.Uptime)}
                                        </span>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('load') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 text-center whitespace-nowrap">
                                        <code className={`rounded-md bg-kumo-recessed/50 px-2 py-1 font-mono text-xs font-semibold ${rowMuted ? 'text-kumo-subtle' : 'text-kumo-strong'} ${COMPACT_INLINE_BOX_CLASS}`}>
                                          {getPrimaryLoadValue(server.info?.cpu?.Load)}
                                        </code>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('speed') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseTrafficCell
                                          left={rx.num}
                                          leftUnit={rx.unit}
                                          right={tx.num}
                                          rightUnit={tx.unit}
                                          leftTitle={server.info?.network?.rx_speed || '0 B/s'}
                                          rightTitle={server.info?.network?.tx_speed || '0 B/s'}
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('traffic') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseTrafficCell
                                          left={rxTotal.num}
                                          leftUnit={rxTotal.unit}
                                          right={txTotal.num}
                                          rightUnit={txTotal.unit}
                                          leftTitle={rxTotal.text}
                                          rightTitle={txTotal.text}
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('cpu') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="CPU"
                                          value={cpuUsage}
                                          detail={`${Math.round(cpuUsage)}%`}
                                          indicatorClassName="!bg-none !bg-kumo-success"
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('memory') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="Mem"
                                          value={memUsage}
                                          detail={`${Math.round(memUsage)}%`}
                                          indicatorClassName="!bg-none !bg-kumo-info"
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('disk') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="Disk"
                                          value={diskUsage}
                                          detail={`${Math.round(diskUsage)}%`}
                                          indicatorClassName="!bg-none !bg-kumo-warning"
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('remaining') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <div title={lifecycle.expiresAt ? `${formatDateTime(lifecycle.startsAt)} - ${formatDateTime(lifecycle.expiresAt)}，剩余 ${Math.round(lifecycle.remainingPercent)}%` : '永久'}>
                                          <DenseLifecycleMeter lifecycle={lifecycle} muted={rowMuted} />
                                        </div>
                                      </Table.Cell>
                                    )}
                                    {isCompactColumnVisible('quotaRemaining') && (
                                      <Table.Cell className="!px-[2px] !py-1.5 whitespace-nowrap">
                                        <DenseUsageMeter
                                          label="余量"
                                          value={trafficQuota.unlimited ? 100 : Math.max(0, 100 - trafficQuota.percent)}
                                          detail={trafficQuota.unlimited ? '无限' : trafficQuota.remainingText}
                                          indicatorClassName={trafficQuota.overLimit ? '!bg-none !bg-kumo-danger' : '!bg-none !bg-kumo-info'}
                                          muted={rowMuted}
                                        />
                                      </Table.Cell>
                                    )}
                                      {isCompactColumnVisible('actions') && (
                                        <Table.Cell sticky="right" className={`!py-1.5 !pl-[1px] !pr-[2px] text-center whitespace-nowrap ${COMPACT_STICKY_ACTION_CLASS}`}>
                                          <div className="flex items-center justify-center gap-1" onClick={event => event.stopPropagation()}>
                                            <ServerConnectionActions
                                              remoteDesktopAvailable={canOpenRemoteDesktop(server)}
                                              terminalLabel={terminalLabel}
                                              terminalDisabled={!canOpenTerminal(server) && !hasSshEndpoint(server)}
                                              onOpenRemoteDesktop={() => openRemoteDesktop(server)}
                                              onOpenTerminal={() => openSSHTerminal(server)}
                                              buttonClassName={COMPACT_ACTION_BUTTON_CLASS}
                                            />
                                          </div>
                                        </Table.Cell>
                                      )}
                                  </Table.Row>
                                )}
                              />
                              {renderServerContextMenu(server)}
                            </ContextMenu.Root>

                            {shouldRenderExpandedRow && (
                              <CompactExpandedRow open={isExpanded} colSpan={visibleCompactColumnDefs.length}>
                                <div className={`flex flex-col ${isDenseViewport ? 'gap-1.5 p-1.5' : 'gap-2 p-2'}`}>
                                  {server.loading && !server.info ? (
                                    <div className="space-y-2 py-5">
                                      <SkeletonLine className="mx-auto h-4 w-1/3" />
                                      <SkeletonLine className="mx-auto h-4 w-1/2" />
                                    </div>
                                  ) : server.error ? (
                                    <div className="rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs font-semibold text-kumo-danger">
                                      {server.error}
                                    </div>
                                  ) : (
                                    <>
                                      <div className="grid grid-cols-1 gap-1.5 cq-sm:grid-cols-2 cq-lg:grid-cols-[repeat(auto-fit,minmax(154px,1fr))]">
                                        <DenseDetailChip label="核心" value={coreText} />
                                        <DenseDetailChip label="Agent 版本" value={server.info?.agentVersion || '未报告'} valueClassName="font-mono text-kumo-strong" />
                                        <DenseDetailChip label="内存" value={`${server.info?.memory?.Used || '-'} / ${server.info?.memory?.Total || '-'}`} />
                                        <DenseDetailChip label="连接" value={server.info?.network?.connections || 0} />
                                        <DenseDetailChip label="Docker" value={server.info?.docker?.installed ? `${dockerSummary.running}/${dockerSummary.total} 运行` : '未安装'} />
                                        <DenseDetailChip label="生命周期" value={lifecycle.expiresAt ? `${lifecycle.label} / ${Math.round(lifecycle.remainingPercent)}%` : '永久'} valueClassName={lifecycle.toneClass} />
                                        <DenseDetailChip label="模式" value={getServerMonitorModeLabel(server)} />
                                      </div>

                                      <div className="flex min-w-0 flex-col gap-2">
                                        <div className={getExpandedTrendGridClassName(true, isDenseViewport)}>
                                          <ExpandedTrendChartCard
                                            title="CPU / 内存趋势"
                                            tone="success"
                                            compact
                                            className={getExpandedCardSpanClassName(0, 3)}
                                            legend={(
                                              <>
                                                <ChartLegend.SmallItem name="CPU" color={cpuColor} value={`${Math.round(cpuUsage)}%`} loading={chartLoading} />
                                                <ChartLegend.SmallItem name="内存" color={memColor} value={`${Math.round(memUsage)}%`} loading={chartLoading} />
                                                <ChartLegend.SmallItem name="温度" color={cpuTempColor} value={getLatestMetricValue(records, getCpuTemp, v => `${v.toFixed(1)}°C`)} loading={chartLoading} />
                                              </>
                                            )}
                                          >
                                            {(tooltipBoundary) => (
                                              <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={compactExpandedChartHeight} />}>
                                                <SiteFontTimeseriesChart
                                                  echarts={fastTimeseriesEcharts}
                                                  data={cpuMemSeries}
                                                  height={compactExpandedChartHeight}
                                                  isDarkMode={isDarkMode}
                                                  gradient
                                                  loading={chartLoading}
                                                  tooltipBoundary={tooltipBoundary ?? undefined}
                                                  xAxisTickCount={expandedChartXAxisTickCount}
                                                  yAxisTickCount={compactExpandedYAxisTickCount}
                                                  xAxisTickFormat={expandedChartXAxisTickFormat}
                                                  yAxisTickFormat={expandedNumberAxisTickFormat}
                                                  tooltipValueFormat={formatMetricTooltipValue}
                                                  optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                                  ariaDescription={`${server.name} CPU 与内存使用趋势`}
                                                />
                                              </DeferredRender>
                                            )}
                                          </ExpandedTrendChartCard>

                                          <ExpandedTrendChartCard
                                            title={hasGpuData ? 'GPU 趋势' : '网络趋势'}
                                            tone={hasGpuData ? 'warning' : 'info'}
                                            compact
                                            className={getExpandedCardSpanClassName(1, hasGpuData ? 3 : 2)}
                                            legend={(
                                              <>
                                                {hasGpuData && <TrendSeriesLabel name="GPU" color={gpuColor} />}
                                                {hasGpuData && <TrendSeriesLabel name="显存" color={vramColor} />}
                                                {hasGpuData && <TrendSeriesLabel name="功耗" color={powerColor} />}
                                                {hasGpuData && <TrendSeriesLabel name="温度" color={gpuTempColor} />}
                                                {!hasGpuData && <ChartLegend.SmallItem name="上行" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} loading={chartLoading} />}
                                                {!hasGpuData && <ChartLegend.SmallItem name="下行" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} loading={chartLoading} />}
                                              </>
                                            )}
                                          >
                                            {(tooltipBoundary) => (
                                              <div className={`grid min-w-0 gap-1.5 ${hasGpuData ? 'grid-cols-1' : 'cq-sm:grid-cols-[minmax(0,1fr)_8.5rem]'}`}>
                                                <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={compactExpandedChartHeight} />}>
                                                  <SiteFontTimeseriesChart
                                                    echarts={fastTimeseriesEcharts}
                                                    data={hasGpuData ? gpuSeries : netSeries}
                                                    height={compactExpandedChartHeight}
                                                    isDarkMode={isDarkMode}
                                                    gradient
                                                    loading={chartLoading}
                                                    tooltipBoundary={tooltipBoundary ?? undefined}
                                                    xAxisTickCount={expandedChartXAxisTickCount}
                                                    yAxisTickCount={compactExpandedYAxisTickCount}
                                                    xAxisTickFormat={expandedChartXAxisTickFormat}
                                                    yAxisTickFormat={hasGpuData ? expandedNumberAxisTickFormat : formatCompactBytesSpeed}
                                                    tooltipValueFormat={hasGpuData ? formatMetricTooltipValue : formatBytesSpeed}
                                                    optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                                    ariaDescription={`${server.name} 主机精简趋势`}
                                                  />
                                                </DeferredRender>
                                                {!hasGpuData && <TrafficTotalSummary txTotal={txTotal} rxTotal={rxTotal} quota={trafficQuota} compact />}
                                              </div>
                                            )}
                                          </ExpandedTrendChartCard>

                                          {hasGpuData && (
                                            <ExpandedTrendChartCard
                                              title="网络趋势"
                                              tone="info"
                                              compact
                                              className={getExpandedCardSpanClassName(2, 3)}
                                              legend={(
                                                <>
                                                  <ChartLegend.SmallItem name="上行" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} loading={chartLoading} />
                                                  <ChartLegend.SmallItem name="下行" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} loading={chartLoading} />
                                                </>
                                              )}
                                            >
                                              {(tooltipBoundary) => (
                                                <div className="grid min-w-0 gap-1.5 cq-sm:grid-cols-[minmax(0,1fr)_8.5rem]">
                                                  <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={compactExpandedChartHeight} />}>
                                                    <SiteFontTimeseriesChart
                                                      echarts={fastTimeseriesEcharts}
                                                      data={netSeries}
                                                      height={compactExpandedChartHeight}
                                                      isDarkMode={isDarkMode}
                                                      gradient
                                                      loading={chartLoading}
                                                      tooltipBoundary={tooltipBoundary ?? undefined}
                                                      xAxisTickCount={expandedChartXAxisTickCount}
                                                      yAxisTickCount={compactExpandedYAxisTickCount}
                                                      xAxisTickFormat={expandedChartXAxisTickFormat}
                                                      yAxisTickFormat={formatCompactBytesSpeed}
                                                      tooltipValueFormat={formatBytesSpeed}
                                                      optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                                      ariaDescription={`${server.name} 网络精简趋势`}
                                                    />
                                                  </DeferredRender>
                                                  <TrafficTotalSummary txTotal={txTotal} rxTotal={rxTotal} quota={trafficQuota} compact />
                                                </div>
                                              )}
                                            </ExpandedTrendChartCard>
                                          )}

                                        </div>

                                        <NetworkQualityPanel
                                          serverName={server.name}
                                          quality={networkQuality}
                                          series={networkQualitySeries}
                                          hasData={hasNetworkQualityData}
                                          unsupported={networkQualityUnsupported}
                                          chartHeight={compactNetworkQualityChartHeight}
                                          isDarkMode={isDarkMode}
                                          chartEcharts={staticTimeseriesEcharts}
                                          isCompactViewport={isCompactViewport}
                                          onCollect={() => loadNetworkQuality(server.id, { collect: true })}
                                          compact
                                          className="min-w-0"
                                        />
                                      </div>
                                    </>
                                  )}
                                </div>
                              </CompactExpandedRow>
                            )}
                          </React.Fragment>
                        );
                      })}
                    </Table.Body>
                  </ScrollableTable>
                </div>
              ) : (
                filteredServers.map(server => {
                  const country = getFlagCountry(server);
                  const locationText = getServerLocationText(server);
                  const isExpanded = expandedServers.includes(server.id);
                  const isChartSeriesReady = chartSeriesReadyServers.includes(server.id);
                  const isDarkMode = theme === 'dark';
                  const {
                    records,
                    cpuColor,
                    memColor,
                    cpuTempColor,
                    gpuColor,
                    vramColor,
                    powerColor,
                    gpuTempColor,
                    diskColor,
                    txColor,
                    rxColor,
                    cpuMemSeries,
                    gpuSeries,
                    netSeries,
                  } = getServerMetricDisplay(server.id, server.metricsCache, isExpanded && isChartSeriesReady, isDarkMode);
                  const hasGpuData = !!getGpuModelText(server.info?.gpu) || records.some(r => (
                    (r.gpu_usage !== null && r.gpu_usage !== undefined && toNumber(r.gpu_usage, 0) > 0)
                    || getGpuTemp(r) > 0
                  ));
                  const tx = parseSpeed(server.info?.network?.tx_speed);
                  const rx = parseSpeed(server.info?.network?.rx_speed);
                  const dockerSummary = summarizeDockerContainers(server.info?.docker, getDockerContainerState);
                  const dockerContainers = dockerSummary.containers;
                  const dockerExpanded = expandedDockerPanels.includes(server.id);
                  const runningContainers = dockerSummary.running;
                  const pausedContainers = dockerSummary.paused;
                  const stoppedContainers = dockerSummary.stopped;
                  const lifecycle = getServerLifecycle(server);
                  const canDrag = !serverSearchText.trim() && serverStatusFilter === 'all' && !isExpanded;
                  const txTotal = getByteParts(server.info?.network?.tx_total);
                  const rxTotal = getByteParts(server.info?.network?.rx_total);
                  const trafficQuota = getTrafficQuota(server);
                  const chartLoading = !!server.metricsLoading && records.length === 0;
                  const terminalProtocol = resolveTerminalProtocol(server);
                  const effectiveTerminalProtocol = terminalProtocol || (hasSshEndpoint(server) ? 'ssh' : null);
                  const terminalLabel = effectiveTerminalProtocol === 'agent' ? 'Agent 隧道终端' : 'SSH 终端';
                  const primaryDisk = server.info?.disk?.[0];
                  const cpuUsage = clampPercent(toNumber(server.info?.cpu?.Usage, 0));
                  const memUsage = clampPercent(toNumber(server.info?.memory?.Usage, 0));
                  const diskUsage = clampPercent(toNumber(primaryDisk?.usage, 0));
                  const cpuTemp = toNumber(server.info?.cpu?.Temp, 0);
                  const cpuPower = toNumber(server.info?.cpu?.Power, 0);
                  const gpuUsage = clampPercent(toNumber(server.info?.gpu?.Usage, 0));
                  const gpuTemp = toNumber(server.info?.gpu?.Temp, 0);
                  const gpuMemPercent = clampPercent(toNumber(server.info?.gpu?.Percent, 0));
                  const physicalCores = server.info?.cpu?.PhysicalCores || server.info?.cpu?.Cores;
                  const logicalCores = server.info?.cpu?.LogicalCores;
                  const coreText = physicalCores && logicalCores && physicalCores !== logicalCores
                    ? `${physicalCores}核 / ${logicalCores}线程`
                    : `${physicalCores || '-'} 核`;
                  const networkQuality = networkQualityByServer[server.id] || {};
                  const networkQualitySeries = isExpanded ? buildNetworkQualitySeries(networkQuality, isDarkMode) : [];
                  const hasNetworkQualityData = isExpanded && networkQualitySeries.some(series => series.data.length > 0);
                  const networkQualityUnsupported = isExpanded && (
                    !!networkQuality.unsupported
                    || isNetworkQualityUnsupportedError(networkQuality.error || networkQuality.unsupportedMessage)
                  );
                  const metricsHealth = resolveServerMetricsHealth(server);
                  const rowMuted = !isServerOnline(server);

                  return (
                    <ContextMenu.Root key={server.id}>
                      <ContextMenu.Trigger
                        draggable={canDrag}
                        onDragStart={(event) => handleServerDragStart(server, event)}
                        onDragOver={handleServerDragOver}
                        onDrop={(event) => handleServerDrop(server.id, event)}
                        onDragEnd={() => setDraggedServerId(null)}
                        className={`bg-kumo-base border rounded-lg transition-all duration-200 ${isExpanded ? 'border-brand/70  ring-1 ring-brand/20' : 'border-kumo-line/90  hover:border-kumo-interact '} ${draggedServerId === server.id ? 'opacity-50' : ''}`}
                      >
                        <div
                          onClick={() => toggleServerExpand(server.id)}
                          className="grid min-h-[56px] grid-cols-[minmax(0,1fr)_2.25rem] items-center gap-x-3 gap-y-2 px-3 py-2.5 cursor-pointer cq-sm:flex cq-sm:flex-nowrap cq-sm:justify-between cq-sm:gap-2.5 cq-sm:py-2"
                        >
                          <div className="order-1 flex min-w-0 items-center gap-3">
                            <span className="relative flex h-2 w-2 rounded-full">
                              <span className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${metricsHealth.dotClassName}`}></span>
                              <span className={`relative inline-flex rounded-full h-2 w-2 ${metricsHealth.dotClassName}`}></span>
                            </span>

                            <div className="flex flex-col min-w-0 gap-1">
                              <div className="flex items-center gap-2 min-w-0">
                                <i className={getOSIconClass(server.info?.platform)}></i>
                                <span
                                  onDoubleClick={e => {
                                    e.stopPropagation();
                                    startRenameServer(server);
                                  }}
                                  className="text-xs font-bold text-kumo-strong truncate hover:text-brand"
                                >
                                  {country && (
                                    <CountryFlag countryCode={country} className="mr-1.5 h-3 w-4 align-[-1px] text-xs" />
                                  )}
                                  {server.name}
                                </span>
                                {server.tags && server.tags.filter(t => t !== 'Agent').map(t => (
                                  <span key={t} className="px-1.5 py-0.5 rounded text-[9px] font-bold bg-kumo-recessed/60 text-kumo-subtle">
                                    {t}
                                  </span>
                                ))}
                                {metricsHealth.stale && (
                                  <Badge
                                    variant={metricsHealth.variant}
                                    appearance="dot"
                                    title="Agent 连接存在，但最近未收到有效指标上报"
                                  >
                                    {metricsHealth.label}
                                  </Badge>
                                )}
                              </div>
                            </div>
                          </div>

                          <div className="contents cq-sm:order-2 cq-sm:ml-auto cq-sm:flex cq-sm:shrink-0 cq-sm:flex-nowrap cq-sm:items-center cq-sm:gap-2.5">
							{isServerOnline(server) && server.info && (
                              <div className="order-3 col-span-2 grid w-full grid-cols-4 gap-1.5 text-[10px] font-semibold text-kumo-subtle cq-sm:order-none cq-sm:col-span-1 cq-sm:flex cq-sm:h-9 cq-sm:w-auto cq-sm:items-center cq-sm:gap-2.5">
                                {hasGpuData && (
                                  <CompactMetricBar
                                    label="GPU"
                                    value={`${parseInt(server.info.gpu?.Usage || '0')}%`}
                                    valueClassName="text-kumo-warning"
                                    barClassName="bg-kumo-warning"
                                    color={gpuColor}
                                    width={server.info.gpu?.Usage || '0%'}
                                  />
                                )}
                                <CompactMetricBar
                                  label="CPU"
                                  value={`${parseInt(server.info.cpu?.Usage || '0')}%`}
                                  valueClassName="text-kumo-success"
                                  barClassName="bg-kumo-success"
                                  color={cpuColor}
                                  width={server.info.cpu?.Usage || '0%'}
                                />
                                <CompactMetricBar
                                  label="Mem"
                                  value={`${parseInt(server.info.memory?.Usage || '0')}%`}
                                  valueClassName="text-kumo-info"
                                  barClassName="bg-kumo-info"
                                  color={memColor}
                                  width={server.info.memory?.Usage || '0%'}
                                />
                                {server.info.disk?.[0] && (
                                  <CompactMetricBar
                                    label="Disk"
                                    value={`${parseInt(server.info.disk[0].usage || '0')}%`}
                                    valueClassName="text-kumo-warning"
                                    barClassName="bg-kumo-warning"
                                    color={diskColor}
                                    width={server.info.disk[0].usage || '0%'}
                                  />
                                )}
                                <div className="hidden cq-sm:block">
                                  <CompactMetricBar
                                    label="剩余"
                                    value={lifecycle.expiresAt ? `${Math.round(lifecycle.remainingPercent)}%` : '永久'}
                                    valueClassName={lifecycle.toneClass}
                                    barClassName={lifecycle.expired ? 'bg-kumo-danger' : lifecycle.remainingPercent <= 20 ? 'bg-kumo-warning' : 'bg-kumo-success'}
                                    width={`${lifecycle.remainingPercent}%`}
                                  />
                                </div>
                                {!hasGpuData && server.info.network && (
                                  <div className="flex min-w-0 flex-col justify-center rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2 py-1 font-mono leading-[1.2] tabular-nums cq-sm:hidden">
                                    <span className="truncate text-kumo-info">&uarr; {tx.num}{tx.unit}</span>
                                    <span className="truncate text-kumo-success">&darr; {rx.num}{rx.unit}</span>
                                  </div>
                                )}
                                {server.info.network && (
                                  <div className="hidden h-8 w-[126px] shrink-0 flex-col justify-center gap-px rounded-md border border-kumo-line bg-kumo-recessed/35 px-[5px] py-0 text-[10px] font-bold leading-[1.2] tabular-nums cq-sm:flex">
                                    <span className="flex items-center justify-between whitespace-nowrap font-mono text-kumo-info">
                                      <span className="flex flex-1 items-center">
                                        <span className="w-2 text-center opacity-70">&uarr;</span>
                                        <span className="ml-1 w-[30px] text-right">{tx.num}</span>
                                        <span className="ml-0.5 w-3 text-left opacity-80">{tx.unit}</span>
                                      </span>
                                      <span className="w-[62px] truncate text-right opacity-85" title={txTotal.text}>{txTotal.num}{txTotal.unit}</span>
                                    </span>
                                    <span className="flex items-center justify-between whitespace-nowrap font-mono text-kumo-success">
                                      <span className="flex flex-1 items-center">
                                        <span className="w-2 text-center opacity-70">&darr;</span>
                                        <span className="ml-1 w-[30px] text-right">{rx.num}</span>
                                        <span className="ml-0.5 w-3 text-left opacity-80">{rx.unit}</span>
                                      </span>
                                      <span className="w-[62px] truncate text-right opacity-85" title={rxTotal.text}>{rxTotal.num}{rxTotal.unit}</span>
                                    </span>
                                  </div>
                                )}
                              </div>
                            )}

                            <div className="order-2 flex items-center justify-end gap-1.5 cq-sm:order-none" onClick={e => e.stopPropagation()}>
                              <ServerConnectionActions
                                remoteDesktopAvailable={canOpenRemoteDesktop(server)}
                                terminalLabel={terminalLabel}
                                terminalDisabled={!canOpenTerminal(server) && !hasSshEndpoint(server)}
                                onOpenRemoteDesktop={() => openRemoteDesktop(server)}
                                onOpenTerminal={() => openSSHTerminal(server)}
                                buttonClassName="h-9 w-9 p-0 cq-sm:h-8 cq-sm:w-8"
                              />
                            </div>
                          </div>
                        </div>

                        <AnimatedCollapse open={isExpanded} keepMounted>
                          <div className={`rounded-b-lg border-t border-kumo-line/90 bg-kumo-canvas/45 ${isDenseViewport ? 'p-1.5' : 'p-1.5 cq-sm:p-2'}`}>
                            {server.loading && !server.info ? (
                              <div className="space-y-2 py-5">
                                <SkeletonLine className="h-4 w-1/3 mx-auto" />
                                <SkeletonLine className="h-4 w-1/2 mx-auto" />
                              </div>
                            ) : server.error ? (
                              <div className="rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs font-semibold text-kumo-danger">
                                {server.error}
                              </div>
                            ) : (
                              <div className={`flex flex-col ${isDenseViewport ? 'gap-1.5' : 'gap-2'}`}>
                                <ExpandedSection title="资源状态" tone="brand">
                                  <div className="grid grid-cols-2 gap-1.5 cq-lg:grid-cols-4">
                                    <ExpandedProgressMetric
                                      label="CPU"
                                      value={cpuUsage}
                                      detail={`${Math.round(cpuUsage)}%`}
                                      caption={`${coreText}${cpuTemp > 0 ? ` · ${Math.round(cpuTemp)}°C` : ''}${cpuPower > 0 ? ` · ${cpuPower.toFixed(1)}W` : ''}`}
                                      indicatorClassName="!bg-none !bg-kumo-success"
                                      valueClassName="text-kumo-success"
                                      muted={rowMuted}
                                    />
                                    <ExpandedProgressMetric
                                      label="内存"
                                      value={memUsage}
                                      detail={`${Math.round(memUsage)}%`}
                                      caption={`${server.info?.memory?.Used || '-'} / ${server.info?.memory?.Total || '-'}`}
                                      indicatorClassName="!bg-none !bg-kumo-info"
                                      valueClassName="text-kumo-info"
                                      muted={rowMuted}
                                    />
                                    <ExpandedProgressMetric
                                      label="磁盘"
                                      value={diskUsage}
                                      detail={primaryDisk ? `${Math.round(diskUsage)}%` : '-'}
                                      caption={primaryDisk ? `${primaryDisk.used || '-'} / ${primaryDisk.total || '-'}` : '未上报'}
                                      indicatorClassName="!bg-none !bg-kumo-warning"
                                      valueClassName="text-kumo-warning"
                                      muted={rowMuted}
                                    />
                                    <ExpandedProgressMetric
                                      label="剩余"
                                      value={lifecycle.remainingPercent}
                                      detail={lifecycle.label}
                                      caption={lifecycle.expiresAt ? `${formatDateTime(lifecycle.startsAt)} - ${formatDateTime(lifecycle.expiresAt)}` : '长期有效'}
                                      indicatorClassName={lifecycle.indicatorClassName}
                                      valueClassName={lifecycle.toneClass}
                                      muted={rowMuted}
                                    />
                                  </div>
                                </ExpandedSection>

                                <div className="flex min-w-0 flex-col gap-2">
                                  <div className={getExpandedInfoGridClassName(isDenseViewport)}>
                                    <ExpandedSection title="系统概览" tone="success" className={getExpandedCardSpanClassName(0, 1)}>
                                      <div className="grid grid-cols-1 gap-1.5 cq-sm:grid-cols-3 cq-xl:grid-cols-4">
                                        <ExpandedInfoChip label="系统" value={server.info?.platform || server.info?.platformVersion || server.info?.system?.Kernel || '-'} />
                                        {/* <ExpandedInfoChip label="版本" value={server.info?.platformVersion || server.info?.system?.Kernel || '-'} /> */}
                                        <ExpandedInfoChip label="CPU 型号" value={server.info?.cpu?.Model || server.metadata?.cpu_model || server.metadata?.cpu_name || server.metadata?.processor || '-'} />
                                        <ExpandedInfoChip label="核心" value={coreText} />
                                        <ExpandedInfoChip label="GPU 型号" value={getGpuModelText(server.info?.gpu) || server.metadata?.gpu_model || server.metadata?.gpu_name || getGpuModelText(server.metadata?.gpu) || '-'} />
                                        <ExpandedInfoChip label="负载" value={server.info?.cpu?.Load || '-'} valueClassName="font-mono text-kumo-strong" />
                                        <ExpandedInfoChip label="在线" value={formatUptimeDaysOnly(server.info?.uptime || server.info?.system?.Uptime)} />
                                        {/* <ExpandedInfoChip label="延迟" value={formatResponseTime(server.response_time)} valueClassName="text-kumo-success" /> */}
                                        <ExpandedInfoChip label="Agent 版本" value={server.info?.agentVersion || '-'} />
                                        {/* <ExpandedInfoChip label="模式" value={getServerMonitorModeLabel(server)} /> */}
                                        <ExpandedInfoChip label="地址" value={getHostAddress(server, serverIpDisplayMode)} valueClassName="font-mono text-kumo-strong" />
                                      </div>
                                    </ExpandedSection>


                                    <ExpandedSection title="网络" tone="info" className={getExpandedCardSpanClassName(1, 3)}>
                                      <div className="grid grid-cols-2 gap-1.5">
                                        <ExpandedStatTile label="上传" value={server.info?.network?.tx_speed || '0 B/s'} tone="info" inline />
                                        <ExpandedStatTile label="下载" value={server.info?.network?.rx_speed || '0 B/s'} tone="success" inline />
                                        <ExpandedInfoChip label="累计上行" value={txTotal.text} valueClassName="text-kumo-info" />
                                        <ExpandedInfoChip label="累计下行" value={rxTotal.text} valueClassName="text-kumo-success" />
                                        <ExpandedInfoChip label="连接" value={server.info?.network?.connections || 0} />
                                        {trafficQuota && (
                                          <ExpandedInfoChip
                                            label="剩余流量"
                                            value={trafficQuota.unlimited ? '无限' : trafficQuota.overLimit ? '已超限' : trafficQuota.remainingText || `${trafficQuota.percent.toFixed(trafficQuota.percent >= 10 ? 0 : 1)}%`}
                                            valueClassName={trafficQuota.unlimited ? 'text-kumo-info' : trafficQuota.overLimit ? 'text-kumo-danger' : trafficQuota.nearAlert ? 'text-kumo-warning' : 'text-kumo-info'}
                                          />
                                        )}
                                      </div>
                                    </ExpandedSection>

                                    <ExpandedTrendChartCard
                                      title="网络趋势"
                                      tone="info"
                                      className={getExpandedCardSpanClassName(1, 3)}
                                      legend={(
                                        <>
                                          <ChartLegend.SmallItem name="上行" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} loading={chartLoading} />
                                          <ChartLegend.SmallItem name="下行" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} loading={chartLoading} />
                                        </>
                                      )}
                                    >
                                      {(tooltipBoundary) => (
                                        <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={expandedTrendChartHeight} />}>
                                          <SiteFontTimeseriesChart
                                            echarts={fastTimeseriesEcharts}
                                            data={netSeries}
                                            height={expandedTrendChartHeight}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={chartLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            xAxisTickCount={expandedChartXAxisTickCount}
                                            yAxisTickCount={expandedChartYAxisTickCount}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={expandedSpeedAxisTickFormat}
                                            tooltipValueFormat={formatBytesSpeed}
                                            optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                            ariaDescription={`${server.name} 网络上传与下载速度趋势`}
                                          />
                                        </DeferredRender>
                                      )}
                                    </ExpandedTrendChartCard>
                                  </div>

                                  <div className={getExpandedTrendGridClassName(false, isDenseViewport)}>
                                    <ExpandedTrendChartCard
                                      title="CPU / 内存趋势"
                                      tone="success"
                                      className={getExpandedCardSpanClassName(0, hasGpuData ? 2 : 1)}
                                      legend={(
                                        <>
                                          <ChartLegend.SmallItem name="CPU" color={cpuColor} value={`${Math.round(cpuUsage)}%`} loading={chartLoading} />
                                          <ChartLegend.SmallItem name="内存" color={memColor} value={`${Math.round(memUsage)}%`} loading={chartLoading} />
                                          <ChartLegend.SmallItem name="温度" color={cpuTempColor} value={getLatestMetricValue(records, getCpuTemp, v => `${v.toFixed(1)}°C`)} loading={chartLoading} />
                                        </>
                                      )}
                                    >
                                      {(tooltipBoundary) => (
                                        <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={expandedTrendChartHeight} />}>
                                          <SiteFontTimeseriesChart
                                            echarts={fastTimeseriesEcharts}
                                            data={cpuMemSeries}
                                            height={expandedTrendChartHeight}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={chartLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            xAxisTickCount={expandedChartXAxisTickCount}
                                            yAxisTickCount={expandedChartYAxisTickCount}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={expandedNumberAxisTickFormat}
                                            tooltipValueFormat={formatMetricTooltipValue}
                                            optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                            ariaDescription={`${server.name} CPU 与内存使用趋势`}
                                          />
                                        </DeferredRender>
                                      )}
                                    </ExpandedTrendChartCard>

                                    {hasGpuData && (
                                      <ExpandedTrendChartCard
                                        title="GPU 趋势"
                                        tone="warning"
                                        className={getExpandedCardSpanClassName(1, 2)}
                                        legend={(
                                          <>
                                            <TrendSeriesLabel name="GPU" color={gpuColor} />
                                            <TrendSeriesLabel name="显存" color={vramColor} />
                                            <TrendSeriesLabel name="功耗" color={powerColor} />
                                            <TrendSeriesLabel name="温度" color={gpuTempColor} />
                                          </>
                                        )}
                                      >
                                        {(tooltipBoundary) => (
                                          <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={expandedTrendChartHeight} />}>
                                            <SiteFontTimeseriesChart
                                              echarts={fastTimeseriesEcharts}
                                              data={gpuSeries}
                                              height={expandedTrendChartHeight}
                                              isDarkMode={isDarkMode}
                                              gradient
                                              loading={chartLoading}
                                              tooltipBoundary={tooltipBoundary ?? undefined}
                                              xAxisTickCount={expandedChartXAxisTickCount}
                                              yAxisTickCount={expandedChartYAxisTickCount}
                                              xAxisTickFormat={expandedChartXAxisTickFormat}
                                              yAxisTickFormat={expandedNumberAxisTickFormat}
                                              tooltipValueFormat={formatMetricTooltipValue}
                                              optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                              ariaDescription={`${server.name} GPU 使用率、显存与功耗趋势`}
                                            />
                                          </DeferredRender>
                                        )}
                                      </ExpandedTrendChartCard>
                                    )}
                                  </div>
                                </div>

                                <NetworkQualityPanel
                                  serverName={server.name}
                                  quality={networkQuality}
                                  series={networkQualitySeries}
                                  hasData={hasNetworkQualityData}
                                  unsupported={networkQualityUnsupported}
                                  chartHeight={networkQualityChartHeight}
                                  isDarkMode={isDarkMode}
                                  chartEcharts={staticTimeseriesEcharts}
                                  isCompactViewport={isCompactViewport}
                                  onCollect={() => loadNetworkQuality(server.id, { collect: true })}
                                />

                                {server.info?.docker?.installed && (
                                  <AppCard padding="none" className="overflow-hidden">
                                    <Button
                                      type="button"
                                      variant="ghost" size="sm"
                                      className="h-8 w-full justify-between px-3 text-left"
                                      onClick={(event) => {
                                        event.stopPropagation();
                                        toggleDockerPanel(server.id);
                                      }}
                                    >
                                      <span className="text-xs font-bold text-kumo-strong">Docker 容器</span>
                                      <span className="flex min-w-0 items-center gap-1.5">
                                        <Badge variant="success" appearance="dot">{runningContainers} 运行</Badge>
                                        {pausedContainers > 0 && <Badge variant="warning" appearance="dot">{pausedContainers} 暂停</Badge>}
                                        {stoppedContainers > 0 && <Badge variant="error" appearance="dot">{stoppedContainers} 停止</Badge>}
                                        {dockerExpanded ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                                      </span>
                                    </Button>

                                    <AnimatedCollapse open={dockerExpanded} keepMounted>
                                      {dockerContainers.length > 0 ? (
                                        <div className="grid grid-cols-1 divide-y divide-kumo-line cq-xl:grid-cols-2 cq-xl:divide-x cq-xl:divide-y-0">
                                          {dockerContainers.map(c => {
                                            const state = getDockerContainerState(c);
                                            const stateBadge = getDockerStateBadge(state);
                                            const containerId = getDockerContainerId(c);
                                            const containerName = getDockerContainerName(c);
                                            const containerImage = getDockerContainerImage(c);
                                            const updateCheck = getDockerContainerUpdateCheck(server.id, c);
                                            const updateBadge = getDockerUpdateBadge(updateCheck);
                                            const updateChecking = isDockerContainerUpdateChecking(server.id, c);
                                            const toggleAction = state === 'running' ? 'container.pause' : state === 'paused' ? 'container.unpause' : 'container.start';
                                            const togglePayload = { serverId: server.id, containerId, containerName, image: containerImage };
                                            const togglePending = isDockerActionPending(server.id, toggleAction, togglePayload);
                                            const restartPayload = { serverId: server.id, containerId, containerName, image: containerImage };
                                            const restartPending = isDockerActionPending(server.id, 'container.restart', restartPayload);
                                            const updatePayload = { serverId: server.id, containerId, containerName, image: containerImage };
                                            const updatePending = isDockerActionPending(server.id, 'container.update', updatePayload);
                                            const updateProgress = getDockerActionProgress(server.id, 'container.update', updatePayload);
                                            const updateConfirmKey = `container.update::${getDockerContainerSelectionKey(server.id, updatePayload)}`;
                                            return (
                                              <div key={containerId || `${server.id}-${containerName}`} className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-3 py-2 text-xs hover:bg-kumo-recessed/20">
                                                <div className="flex min-w-0 items-center gap-2">
                                                  <Badge variant={stateBadge.variant} appearance="dot" className="shrink-0">{stateBadge.label}</Badge>
                                                  <div className="min-w-0">
                                                    <div className="truncate font-semibold text-kumo-strong" title={containerName}>{containerName}</div>
                                                    <div className="truncate font-mono text-[10px] text-kumo-subtle" title={containerImage}>{containerImage}</div>
                                                    {(updateCheck || updateChecking || updatePending) && (
                                                      <div className="mt-1">
                                                        {updatePending ? (
                                                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-brand">
                                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                                            更新中{updateProgress > 0 ? ` ${updateProgress}%` : ''}
                                                          </span>
                                                        ) : updateChecking ? (
                                                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-kumo-subtle">
                                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                                            检测中
                                                          </span>
                                                        ) : (
                                                          <Badge variant={updateBadge.variant} appearance="dot" title={updateBadge.title}>
                                                            {updateBadge.label}
                                                          </Badge>
                                                        )}
                                                      </div>
                                                    )}
                                                  </div>
                                                </div>
                                                <div className="flex shrink-0 items-center gap-1">
                                                  <Button
                                                    shape="square" size="sm"
                                                    variant="secondary"
                                                    aria-label={state === 'running' ? '暂停容器' : state === 'paused' ? '恢复容器' : '启动容器'}
                                                    title={state === 'running' ? '暂停' : state === 'paused' ? '恢复' : '启动'}
                                                    icon={togglePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : state === 'running' ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                                                    disabled={togglePending || restartPending || updatePending}
                                                    onClick={(event) => {
                                                      event.stopPropagation();
                                                      submitDockerTask(toggleAction, togglePayload);
                                                    }}
                                                  />
                                                  <Button
                                                    shape="square" size="sm"
                                                    variant="secondary"
                                                    aria-label="重启容器"
                                                    title="重启"
                                                    icon={restartPending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
                                                    disabled={togglePending || restartPending || updatePending}
                                                    onClick={(event) => {
                                                      event.stopPropagation();
                                                      submitDockerTask('container.restart', restartPayload);
                                                    }}
                                                  />
                                                  <Button
                                                    shape="square" size="sm"
                                                    variant="secondary"
                                                    aria-label="检测镜像更新"
                                                    title="检测更新"
                                                    icon={updateChecking ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                                                    disabled={updateChecking}
                                                    onClick={(event) => {
                                                      event.stopPropagation();
                                                      checkDockerUpdatesForServer(server, c);
                                                    }}
                                                  />
                                                  <Button
                                                    shape="square" size="sm"
                                                    variant={isDockerUpdateConfirmActive(updateConfirmKey) ? 'secondary-destructive' : 'primary'}
                                                    aria-label={isDockerUpdateConfirmActive(updateConfirmKey) ? '再次确认更新容器' : '一键更新容器'}
                                                    title={isDockerUpdateConfirmActive(updateConfirmKey) ? '再次点击确认更新' : '一键更新'}
                                                    icon={updatePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                                                    disabled={togglePending || restartPending || updatePending}
                                                    onClick={(event) => {
                                                      event.stopPropagation();
                                                      handleDockerContainerUpdate(updatePayload);
                                                    }}
                                                  />
                                                </div>
                                              </div>
                                            );
                                          })}
                                        </div>
                                      ) : (
                                        <div className="px-3 py-4 text-center text-xs text-kumo-subtle">暂无容器</div>
                                      )}
                                    </AnimatedCollapse>
                                  </AppCard>
                                )}
                              </div>
                            )}
                          </div>
                        </AnimatedCollapse>
                      </ContextMenu.Trigger>

                      {renderServerContextMenu(server)}
                    </ContextMenu.Root>
                  );
                }))}
            </div>
          )}
        </div>
      )}

      {/* ==================== 3. Docker 控制台 ==================== */}
      {serverCurrentTab === 'docker' && (
        <div className="flex flex-col gap-4">
          <div className={SERVER_SECONDARY_BAR_CLASS}>
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                value={dockerSubTab}
                onValueChange={setDockerSubTab}
                tabs={[
                  { value: 'containers', label: <span className="inline-flex items-center gap-1.5"><Box className="w-3.5 h-3.5" />容器</span> },
                  { value: 'compose', label: <span className="inline-flex items-center gap-1.5"><FolderOpen className="w-3.5 h-3.5" />Compose</span> },
                  { value: 'images', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="w-3.5 h-3.5" />镜像</span> },
                  { value: 'networks', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-3.5 h-3.5" />网络</span> },
                  { value: 'volumes', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="w-3.5 h-3.5" />存储卷</span> },

                ]}
              />
            </div>

            <div className="flex min-w-0 flex-wrap items-center justify-start gap-2 cq-lg:justify-end">
              {dockerSubTab === 'containers' && (
                <>
                  <Input
                    size="sm"
                    aria-label="搜索 Docker 容器"
                    value={dockerSearchQuery}
                    onChange={event => setDockerSearchQuery(event.target.value)}
                    placeholder="搜索容器 / 镜像 / 端口"
                    className="h-6.5 w-full min-w-[12rem] cq-sm:w-52"
                  />
                  <Select
                    aria-label="筛选容器状态"
                    size="sm"
                    value={dockerContainerStateFilter}
                    onValueChange={(value) => setDockerContainerStateFilter(String(value))}
                    className="h-6.5 w-28"
                    items={[
                      { value: 'all', label: '全部状态' },
                      { value: 'running', label: '运行' },
                      { value: 'paused', label: '暂停' },
                      { value: 'stopped', label: '停止' },
                      { value: 'updatable', label: '可更新' },
                    ]}
                  />
                </>
              )}
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                icon={<RefreshCw className={`h-3.5 w-3.5 ${dockerResourceLoading ? 'animate-spin' : ''}`} />}
                disabled={dockerResourceLoading}
                aria-label="刷新 Docker 数据"
                title="刷新 Docker 数据"
                onClick={loadDockerResources}
              />
              {dockerSubTab === 'containers' && (
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<RefreshCw className={`h-3.5 w-3.5 ${dockerBulkUpdateChecking ? 'animate-spin' : ''}`} />}
                  disabled={dockerBulkUpdateChecking || visibleDockerContainerServers.length === 0}
                  onClick={checkVisibleDockerUpdates}
                >
                  检测可更新
                </Button>
              )}
              {dockerSubTab === 'containers' && (
                <Button
                  size="sm"
                  variant={isDockerUpdateConfirmActive(dockerActiveBatchUpdateConfirmKey) ? 'secondary-destructive' : 'primary'}
                  icon={<Upload className="h-3.5 w-3.5" />}
                  disabled={dockerActiveBatchUpdateTargets.length === 0}
                  onClick={() => batchUpdateVisibleDockerContainers('', false, dockerActiveBatchUpdateTargets, {
                    scopeName: dockerActiveBatchUpdateScopeName,
                    confirmKey: dockerActiveBatchUpdateConfirmKey,
                  })}
                  title={dockerActiveBatchUpdateTargets.length > 0 ? `${dockerActiveBatchUpdateScopeName}下可更新的容器` : '请先检测出可更新容器'}
                >
                  {isDockerUpdateConfirmActive(dockerActiveBatchUpdateConfirmKey)
                    ? '再次确认'
                    : selectedUpdatableDockerContainers.length > 0
                      ? `更新选中 ${selectedUpdatableDockerContainers.length}`
                      : `一键更新 ${visibleUpdatableDockerContainers.length > 0 ? visibleUpdatableDockerContainers.length : ''}`}
                </Button>
              )}
            </div>
          </div>

          {/* Docker 任务中心 */}
          {dockerSubTab !== 'containers' && dockerSubTab === 'task-center' && dockerTasks.length > 0 && (
            <AppCard padding="none" className="bg-kumo-recessed p-2.5 text-xs text-kumo-default">
                {(() => {
                  const latestTask = dockerTasks[0];
                  const progress = clampPercent(toNumber(latestTask.progress, 0));
                  const showProgress = !['success', 'failed', 'timeout', 'cancelled'].includes(latestTask.state) && progress > 0;
                  const stateVariant = latestTask.state === 'success' ? 'success' : latestTask.state === 'failed' ? 'error' : 'warning';
                  return (
                    <>
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex min-w-0 items-center gap-2">
                          <Activity className="h-3.5 w-3.5 shrink-0 text-brand" />
                          <span className="shrink-0 font-bold text-brand">Docker 任务</span>
                          <Badge variant={stateVariant} appearance="dot">{getDockerTaskStateLabel(latestTask.state)}</Badge>
                          <span className="min-w-0 truncate text-kumo-subtle">{getDockerTaskActionLabel(latestTask.action)}</span>
                          <span className="min-w-0 truncate text-kumo-subtle">{summarizeDockerTaskMessage(latestTask)}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <Badge variant={dockerTaskStreamConnected ? 'success' : 'warning'} appearance="dot">
                            {dockerTaskStreamConnected ? '实时连接' : '重连中'}
                          </Badge>
                          <Button size="sm" variant="secondary" icon={showDockerTaskDetails ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />} onClick={() => setShowDockerTaskDetails(v => !v)}>
                            {showDockerTaskDetails ? '收起' : `详情 ${dockerTasks.length}`}
                          </Button>
                        </div>
                      </div>
                      {showProgress && (
                        <Meter
                          label="Docker 任务进度"
                          value={progress}
                          showValue={false}
                          className="mt-2 gap-0"
                          trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
                          indicatorClassName="!h-full !bg-none !bg-brand"
                        />
                      )}
                    </>
                  );
                })()}
                <AnimatedCollapse open={showDockerTaskDetails} keepMounted>
                <div className="mt-2 max-h-36 overflow-y-auto border-t border-kumo-line pt-2">
                  <div className="flex flex-col gap-1.5">
                    {dockerTasks.slice(0, 12).map(t => {
                      const progress = clampPercent(toNumber(t.progress, 0));
                      const showProgress = !['success', 'failed', 'timeout', 'cancelled'].includes(t.state) && progress > 0;
                      const stateVariant = t.state === 'success' ? 'success' : t.state === 'failed' ? 'error' : 'warning';
                      return (
                        <div key={t.taskId} className="grid min-w-0 grid-cols-[7rem_minmax(0,1fr)_minmax(0,1.25fr)] items-center gap-2 text-[11px]">
                          <Badge variant={stateVariant} appearance="dot">{getDockerTaskStateLabel(t.state)}</Badge>
                          <span className="truncate font-semibold text-kumo-strong" title={getDockerTaskActionLabel(t.action)}>{getDockerTaskActionLabel(t.action)}</span>
                          <span className="truncate text-kumo-subtle" title={String(t.message || '')}>
                            {showProgress ? `${progress}% · ` : ''}{summarizeDockerTaskMessage(t)}
                          </span>
                        </div>
                      );
                    })}
                  </div>
                </div>
                </AnimatedCollapse>
            </AppCard>
          )}

          {/* 内容区域 */}
          {showDockerBlockingLoading ? (
            <AppCard padding="none" className="overflow-hidden p-4">
              <div className="space-y-4">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="space-y-2">
                    <SkeletonLine className="w-1/4 h-5" />
                    <SkeletonLine className="w-full h-12 rounded-lg" />
                  </div>
                ))}
              </div>
            </AppCard>
          ) : (
            <div className="flex flex-col gap-4">
              {/* 1. 容器管理 */}
              {dockerSubTab === 'containers' && (
                <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                  <div className="flex min-w-0 flex-col gap-3 cq-xl:order-2">
                    {dockerOverviewServers.length === 0 ? (
                      renderDockerEmptyState('未检测到可用的 Docker 主机')
                    ) : visibleDockerContainerServers.length === 0 ? (
                      renderDockerEmptyState('当前筛选下没有容器')
                    ) : (
                      visibleDockerContainerServers.map(server => {
                        const summary = getDockerContainerSummary(server.resources?.containers, server.id);
                        const isOpen = isDockerHostExpanded(server.id, visibleDockerContainerServers[0]?.id);
                        const hostUpdateTargets = getUpdatableDockerContainers([server], server.id);
                        const hostUpdateConfirmKey = `host.update::${server.id}::${hostUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`;
                        return (
                        <div key={server.id} className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
                            <div
                              role="button"
                              tabIndex={0}
                              onClick={() => toggleDockerOverviewServer(server.id, visibleDockerContainerServers[0]?.id)}
                              onKeyDown={(event) => {
                                if (event.key === 'Enter' || event.key === ' ') {
                                  event.preventDefault();
                                  toggleDockerOverviewServer(server.id, visibleDockerContainerServers[0]?.id);
                                }
                              }}
                              className="flex min-h-[52px] cursor-pointer flex-wrap items-center justify-between gap-2 border-b border-kumo-line/70 px-3 py-3.5"
                            >
                              <div className="flex min-w-0 items-center gap-2">
                                <Box className="h-4 w-4 shrink-0 text-brand" />
                                <span className="truncate text-xs font-bold text-kumo-strong">{server.name}</span>
                                {renderDockerFilterChip(server.id, 'all', `${summary.total} 容器`, 'neutral', `${server.name} 全部容器`)}
                                {renderDockerFilterChip(server.id, 'running', `${summary.running} 运行`, 'success', `${server.name} 运行容器`)}
                                {summary.paused > 0 && renderDockerFilterChip(server.id, 'paused', `${summary.paused} 暂停`, 'warning', `${server.name} 暂停容器`)}
                                {summary.stopped > 0 && renderDockerFilterChip(server.id, 'stopped', `${summary.stopped} 停止`, 'error', `${server.name} 停止容器`)}
                                {summary.updatable > 0 && renderDockerFilterChip(server.id, 'updatable', `${summary.updatable} 可更新`, 'warning', `${server.name} 可更新容器`)}
                              </div>
                              <div className="flex items-center gap-1.5">
                                {summary.updatable > 0 && (
                                  <Button
                                    size="sm"
                                    variant={isDockerUpdateConfirmActive(hostUpdateConfirmKey) ? 'secondary-destructive' : 'primary'}
                                    icon={<Upload className="h-3.5 w-3.5" />}
                                    onClick={(event) => {
                                      event.stopPropagation();
                                      batchUpdateVisibleDockerContainers(server.id, false, hostUpdateTargets, {
                                        confirmKey: hostUpdateConfirmKey,
                                      });
                                    }}
                                  >
                                    {isDockerUpdateConfirmActive(hostUpdateConfirmKey) ? '再次确认' : '一键更新'}
                                  </Button>
                                )}
                                <Button
                                  size="sm"
                                  variant="secondary"
                                  icon={isOpen ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                                  onClick={(event) => {
                                    event.stopPropagation();
                                    toggleDockerOverviewServer(server.id, visibleDockerContainerServers[0]?.id);
                                  }}
                                >
                                  {isOpen ? '收起' : '展开'}
                                </Button>
                              </div>
                            </div>

                          <AnimatedCollapse open={isOpen} keepMounted>
                          {server.resources.containers.length === 0 ? (
                            <div className="p-8 text-center text-xs text-kumo-subtle">
                              暂无容器
                            </div>
                          ) : (
                            <div className="divide-y divide-kumo-line">
                              {server.resources.containers.map(c => {
                                const state = getDockerContainerState(c);
                                const stateBadge = getDockerStateBadge(state);
                                const containerId = getDockerContainerId(c);
                                const containerName = getDockerContainerName(c);
                                const containerImage = getDockerContainerImage(c);
                                const containerPorts = getDockerContainerPorts(c);
                                const updateCheck = getDockerContainerUpdateCheck(server.id, c);
                                const updateBadge = getDockerUpdateBadge(updateCheck);
                                const updateChecking = isDockerContainerUpdateChecking(server.id, c);
                                const toggleAction = state === 'running' ? 'container.stop' : 'container.start';
                                const togglePayload = { serverId: server.id, containerId, containerName, image: containerImage };
                                const togglePending = isDockerActionPending(server.id, toggleAction, togglePayload);
                                const restartPayload = { serverId: server.id, containerId, containerName, image: containerImage };
                                const restartPending = isDockerActionPending(server.id, 'container.restart', restartPayload);
                                const updatePayload = { serverId: server.id, containerId, containerName, image: containerImage };
                                const updatePending = isDockerActionPending(server.id, 'container.update', updatePayload);
                                const updateProgress = getDockerActionProgress(server.id, 'container.update', updatePayload);
                                const updateConfirmKey = `container.update::${getDockerContainerSelectionKey(server.id, updatePayload)}`;
                                const selectionKey = getDockerContainerSelectionKey(server.id, updatePayload);
                                const canSelectForUpdate = !!updateCheck?.hasUpdate && !updatePending;
                                return (
                                  <div key={containerId || `${server.id}-${containerName}`} className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4">
                                    <Checkbox
                                      checked={dockerSelectedContainerKeySet.has(selectionKey)}
                                      disabled={!canSelectForUpdate}
                                      onCheckedChange={(checked) => toggleDockerContainerSelection(selectionKey, Boolean(checked))}
                                      aria-label={`选择更新 ${containerName}`}
                                    />
                                    <div className="min-w-0 flex-1">
                                      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                        <span className="min-w-0 truncate text-xs font-bold leading-5 text-kumo-strong" title={containerName}>{containerName}</span>
                                        <span className="min-w-0 truncate font-mono text-[10px] text-kumo-subtle" title={containerImage}>{containerImage}</span>
                                      </div>
                                      <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                        <Badge variant={stateBadge.variant} appearance="dot">{stateBadge.label}</Badge>
                                        {updatePending ? (
                                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-brand">
                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                            更新中{updateProgress > 0 ? ` ${updateProgress}%` : ''}
                                          </span>
                                        ) : updateChecking ? (
                                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-kumo-subtle">
                                            <RefreshCw className="h-3 w-3 animate-spin" />
                                            检测中
                                          </span>
                                        ) : (
                                          <Badge variant={updateBadge.variant} appearance="dot" title={updateBadge.title}>
                                            {updateBadge.label}
                                          </Badge>
                                        )}
                                        {containerPorts && <span className="truncate font-mono" title={containerPorts}>{containerPorts}</span>}
                                      </div>
                                      {updatePending && updateProgress > 0 && (
                                        <Meter
                                          label={`${containerName} 更新进度`}
                                          value={updateProgress}
                                          showValue={false}
                                          className="mt-1.5 w-28 gap-0"
                                          trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-recessed"
                                          indicatorClassName="!h-full !bg-none !bg-brand"
                                        />
                                      )}
                                    </div>
                                    <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                                      <Button
                                        shape="square" size="sm"
                                        variant={state === 'running' ? 'secondary-destructive' : 'secondary'}
                                        icon={togglePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : state === 'running' ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                                        aria-label={state === 'running' ? '停止容器' : '启动容器'}
                                        disabled={togglePending || restartPending || updatePending}
                                        onClick={() => submitDockerTask(toggleAction, togglePayload)}
                                        title={state === 'running' ? '停止' : '启动'}
                                      />
                                      <Button
                                        shape="square" size="sm"
                                        variant="secondary"
                                        icon={restartPending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
                                        aria-label="重启容器"
                                        disabled={togglePending || restartPending || updatePending}
                                        onClick={() => submitDockerTask('container.restart', restartPayload)}
                                        title="重启"
                                      />
                                      <Button
                                        shape="square" size="sm"
                                        variant="secondary"
                                        icon={updateChecking ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                                        aria-label="检测镜像更新"
                                        disabled={updateChecking}
                                        onClick={() => checkDockerUpdatesForServer(server, c)}
                                        title="检测更新"
                                      />
                                      <Button
                                        shape="square" size="sm"
                                        variant={isDockerUpdateConfirmActive(updateConfirmKey) ? 'secondary-destructive' : 'primary'}
                                        icon={updatePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                                        aria-label={isDockerUpdateConfirmActive(updateConfirmKey) ? '再次确认更新容器' : '一键更新容器'}
                                        disabled={togglePending || restartPending || updatePending}
                                        onClick={() => handleDockerContainerUpdate(updatePayload)}
                                        title={isDockerUpdateConfirmActive(updateConfirmKey) ? '再次点击确认更新' : '一键更新'}
                                      />
                                    </div>
                                  </div>
                                );
                              })}
                            </div>
                          )}
                          </AnimatedCollapse>
                        </div>
                        );
                      })
                    )}
                  </div>

                  <div className="flex min-w-0 flex-col gap-3 cq-xl:order-1 cq-xl:sticky cq-xl:top-0 cq-xl:self-start">
                    {renderDockerSimpleLogCard('order-3')}

                    <LayerCard className="order-1 overflow-hidden p-0">
                      <LayerCard.Secondary className="flex min-h-[52px] items-center justify-between gap-2 px-3 py-3.5">
                        <span className="inline-flex min-w-0 items-center gap-2 text-xs font-bold text-kumo-strong">
                          <Settings className="h-4 w-4 shrink-0 text-brand" />
                          管理
                        </span>
                        <span className="flex shrink-0 items-center gap-1.5">
                          <Badge variant="neutral">{dockerContainerManagementServers.length} 主机</Badge>
                          <Button
                            shape="square"
                            size="sm"
                            variant="secondary"
                            icon={<RefreshCw className={`h-3.5 w-3.5 ${dockerBulkUpdateChecking ? 'animate-spin' : ''}`} />}
                            disabled={dockerBulkUpdateChecking || dockerContainerManagementServers.length === 0}
                            onClick={checkAllDockerUpdates}
                            aria-label="检测可更新"
                            title="检测全部主机可更新"
                          />
                          <Button
                            shape="square"
                            size="sm"
                            variant={isDockerUpdateConfirmActive(dockerManagementBatchUpdateConfirmKey) ? 'secondary-destructive' : 'primary'}
                            icon={<Upload className="h-3.5 w-3.5" />}
                            disabled={dockerContainerManagementUpdatableContainers.length === 0}
                            onClick={() => batchUpdateVisibleDockerContainers('', true, null, {
                              confirmKey: dockerManagementBatchUpdateConfirmKey,
                            })}
                            aria-label={isDockerUpdateConfirmActive(dockerManagementBatchUpdateConfirmKey) ? '再次确认一键更新' : '一键更新'}
                            title={dockerContainerManagementUpdatableContainers.length > 0
                              ? isDockerUpdateConfirmActive(dockerManagementBatchUpdateConfirmKey)
                                ? '再次点击确认更新全部主机'
                                : `一键更新全部主机 ${dockerContainerManagementUpdatableContainers.length} 个容器`
                              : '请先检测出可更新容器'}
                          />
                        </span>
                      </LayerCard.Secondary>
                      <LayerCard.Primary className="space-y-3 p-3">
                        <div className="grid grid-cols-4 gap-1.5">
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">容器</div>
                            <div className="mt-0.5 text-sm font-bold text-kumo-strong">{dockerContainerManagementTotals.total}</div>
                          </div>
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">运行</div>
                            <div className="mt-0.5 text-sm font-bold text-kumo-success">{dockerContainerManagementTotals.running}</div>
                          </div>
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">停止</div>
                            <div className="mt-0.5 text-sm font-bold text-kumo-subtle">{dockerContainerManagementTotals.stopped}</div>
                          </div>
                          <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                            <div className="text-[10px] text-kumo-subtle">更新</div>
                            <div className="mt-0.5 text-sm font-bold text-kumo-warning">{dockerContainerManagementTotals.updatable}</div>
                          </div>
                        </div>

                        <div className="flex flex-col gap-2">
                          {dockerContainerManagementServers.map(server => {
                            const summary = getDockerContainerSummary(server.resources?.containers, server.id);
                            const isOpen = isDockerHostExpanded(server.id, dockerContainerManagementServers[0]?.id);
                            const managementHostUpdateTargets = getUpdatableDockerContainers([server], server.id);
                            const managementHostUpdateConfirmKey = `management.host.update::${server.id}::${managementHostUpdateTargets.map(item => getDockerContainerSelectionKey(item.server.id, item.payload)).join('|')}`;
                            return (
                              <div key={`management-${server.id}`} className={`overflow-hidden rounded-md border ${isOpen ? 'border-brand/55 bg-brand/5' : 'border-kumo-line/80 bg-kumo-base'}`}>
                                  <div
                                    role="button"
                                    tabIndex={0}
                                    onClick={() => toggleDockerOverviewServer(server.id, dockerContainerManagementServers[0]?.id)}
                                    onKeyDown={(event) => {
                                      if (event.key === 'Enter' || event.key === ' ') {
                                        event.preventDefault();
                                        toggleDockerOverviewServer(server.id, dockerContainerManagementServers[0]?.id);
                                      }
                                    }}
                                    className="flex min-h-10 cursor-pointer items-center justify-between gap-2 px-2.5 py-2"
                                  >
                                    <span className="flex min-w-0 items-center gap-2">
                                      <Box className="h-3.5 w-3.5 shrink-0 text-brand" />
                                      <span className="min-w-0 truncate text-xs font-bold text-kumo-strong">{server.name}</span>
                                    </span>
                                    <span className="flex shrink-0 items-center gap-1.5">
                                      {renderDockerFilterChip(server.id, 'all', String(summary.total), 'neutral', `${server.name} 全部容器`)}
                                      {summary.updatable > 0 && renderDockerFilterChip(server.id, 'updatable', String(summary.updatable), 'warning', `${server.name} 可更新容器`)}
                                    </span>
                                  </div>

                                  <AnimatedCollapse open={isOpen} keepMounted>
                                    <div className="border-t border-kumo-line/70 px-2.5 py-2">
                                      <div className="flex flex-wrap items-center justify-between gap-2">
                                        <div className="flex flex-wrap gap-1.5">
                                          {renderDockerFilterChip(server.id, 'running', `${summary.running} 运行`, 'success', `${server.name} 运行容器`)}
                                          {summary.paused > 0 && renderDockerFilterChip(server.id, 'paused', `${summary.paused} 暂停`, 'warning', `${server.name} 暂停容器`)}
                                          {summary.stopped > 0 && renderDockerFilterChip(server.id, 'stopped', `${summary.stopped} 停止`, 'error', `${server.name} 停止容器`)}
                                          {summary.updatable > 0 && renderDockerFilterChip(server.id, 'updatable', `${summary.updatable} 可更新`, 'warning', `${server.name} 可更新容器`)}
                                        </div>
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant={isDockerUpdateConfirmActive(managementHostUpdateConfirmKey) ? 'secondary-destructive' : 'primary'}
                                          icon={<Upload className="h-3.5 w-3.5" />}
                                          disabled={summary.updatable === 0}
                                          onClick={(event) => {
                                            event.stopPropagation();
                                            batchUpdateVisibleDockerContainers(server.id, false, managementHostUpdateTargets, {
                                              confirmKey: managementHostUpdateConfirmKey,
                                            });
                                          }}
                                          aria-label={isDockerUpdateConfirmActive(managementHostUpdateConfirmKey) ? `再次确认更新 ${server.name}` : `更新 ${server.name} 可更新容器`}
                                          title={summary.updatable > 0
                                            ? isDockerUpdateConfirmActive(managementHostUpdateConfirmKey)
                                              ? `再次点击确认更新 ${server.name}`
                                              : `更新 ${server.name} 的 ${summary.updatable} 个可更新容器`
                                            : `${server.name} 暂无可更新容器`}
                                        />
                                      </div>
                                    </div>
                                  </AnimatedCollapse>
                                </div>
                            );
                          })}
                        </div>
                      </LayerCard.Primary>
                    </LayerCard>
                </div>
                </div>
              )}

              {/* 2. Compose */}
              {dockerSubTab === 'compose' && (() => {
                const hosts = dockerOverviewServers.filter(server => isDockerOverviewHostVisible(server, 'compose'));
                const runningProjects = dockerComposeProjects.filter(item => getComposeStatus(item).includes('running')).length;
                return (
                  <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                    {renderDockerResourceSideRail({
                      title: 'Compose 项目',
                      icon: <FolderOpen className="h-4 w-4 shrink-0 text-brand" />,
                      hosts,
                      totalCount: dockerComposeProjects.length,
                      countLabel: '项目',
                      summaryItems: [
                        { label: '运行', value: runningProjects, className: 'text-kumo-success' },
                        { label: '停止', value: Math.max(0, dockerComposeProjects.length - runningProjects), className: 'text-kumo-subtle' },
                      ],
                      getHostCount: server => asArray(server.resources?.composeProjects).length,
                      getHostBadges: server => {
                        const projects = asArray(server.resources?.composeProjects);
                        const running = projects.filter(project => getComposeStatus(project).toLowerCase().includes('running')).length;
                        return running > 0 ? [{ label: `${running} 运行`, variant: 'success', appearance: 'dot' }] : [];
                      },
                    })}
                    <div className="flex min-w-0 flex-col gap-3">
                      {hosts.length === 0 ? (
                        renderDockerEmptyState('未检测到可用的 Docker 主机')
                      ) : (
                        hosts.map(server => {
                          const projects = asArray(server.resources?.composeProjects);
                          const running = projects.filter(project => getComposeStatus(project).toLowerCase().includes('running')).length;
                          return renderDockerHostResourceSection({
                            server,
                            isFirstVisible: server.id === hosts[0]?.id,
                            icon: <FolderOpen className="h-4 w-4 shrink-0 text-brand" />,
                            count: projects.length,
                            countLabel: '项目',
                            badges: running > 0 ? [{ label: `${running} 运行`, variant: 'success', appearance: 'dot' }] : [],
                            children: projects.length === 0 ? (
                              <div className="p-8 text-center text-xs text-kumo-subtle">
                                暂无 Compose 项目
                              </div>
                            ) : (
                              <div className="divide-y divide-kumo-line">
                                {projects.map(proj => {
                                  const projectName = getComposeProjectName(proj);
                                  const configFiles = getComposeConfigFiles(proj);
                                  const primaryConfigFile = getComposePrimaryConfigFile(proj);
                                  const workingDir = getComposeWorkingDir(proj);
                                  const status = getComposeStatus(proj);
                                  const statusLower = status.toLowerCase();
                                  const statusLabel = formatComposeStatusLabel(status);
                                  const composePayload = { serverId: server.id, project: projectName, config_file: configFiles };
                                  const composeUpPending = isDockerActionPending(server.id, 'compose.up', composePayload);
                                  const composeDownPending = isDockerActionPending(server.id, 'compose.down', composePayload);
                                  const composeRestartPending = isDockerActionPending(server.id, 'compose.restart', composePayload);
                                  const composePullPending = isDockerActionPending(server.id, 'compose.pull', composePayload);
                                  const composeBusy = composeUpPending || composeDownPending || composeRestartPending || composePullPending;
                                  return (
                                    <div key={`${server.id}-${projectName}-${configFiles}`} className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4">
                                      <div className="min-w-0 flex-1">
                                        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                          <span className="truncate text-xs font-bold leading-5 text-kumo-strong">{projectName}</span>
                                          <Badge variant={statusLower.includes('running') ? 'success' : 'error'} appearance="dot">{statusLabel}</Badge>
                                        </div>
                                        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                          <span className="min-w-0 truncate font-mono" title={configFiles}>配置: {configFiles || '-'}</span>
                                          <span className="min-w-0 truncate font-mono" title={workingDir}>目录: {workingDir || '-'}</span>
                                        </div>
                                      </div>
                                      <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant="secondary"
                                          icon={<FileText className="h-3.5 w-3.5" />}
                                          onClick={() => openDockerComposeConfig(server, proj, 'view')}
                                          disabled={!primaryConfigFile}
                                          aria-label={`查看 ${projectName} 配置`}
                                          title={primaryConfigFile ? '查看配置' : '未找到配置文件'}
                                        />
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant="secondary"
                                          icon={<Edit className="h-3.5 w-3.5" />}
                                          onClick={() => openDockerComposeConfig(server, proj, 'edit')}
                                          disabled={!primaryConfigFile}
                                          aria-label={`修改 ${projectName} 配置`}
                                          title={primaryConfigFile ? '修改配置' : '未找到配置文件'}
                                        />
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant="secondary"
                                          icon={composePullPending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                                          disabled={composeBusy}
                                          onClick={() => submitDockerTask('compose.pull', composePayload)}
                                          aria-label={`升级 ${projectName}`}
                                          title="升级镜像"
                                        />
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant="secondary"
                                          icon={composeRestartPending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
                                          disabled={composeBusy}
                                          onClick={() => submitDockerTask('compose.restart', composePayload)}
                                          aria-label={`重启 ${projectName}`}
                                          title="重启项目"
                                        />
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant="primary"
                                          icon={composeUpPending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                                          disabled={composeBusy}
                                          onClick={() => submitDockerTask('compose.up', composePayload)}
                                          aria-label={`启动 ${projectName}`}
                                          title="启动项目"
                                        />
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant="secondary"
                                          icon={composeDownPending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />}
                                          disabled={composeBusy}
                                          onClick={() => submitDockerTask('compose.down', composePayload)}
                                          aria-label={`停止 ${projectName}`}
                                          title="停止项目"
                                        />
                                      </div>
                                    </div>
                                  );
                                })}
                              </div>
                            ),
                          });
                        })
                      )}
                    </div>
                  </div>
                );
              })()}

              {/* 3. 镜像管理 */}
              {dockerSubTab === 'images' && (() => {
                const hosts = dockerOverviewServers.filter(server => isDockerOverviewHostVisible(server, 'images'));
                const allImagePruneConfirmKey = `image.prune.all::${hosts.map(server => server.id).join('|')}`;
                const pruneCandidateCount = dockerImages.filter(isDockerImagePruneCandidate).length;
                return (
                  <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                    {renderDockerResourceSideRail({
                      title: '镜像',
                      icon: <HardDrive className="h-4 w-4 shrink-0 text-brand" />,
                      hosts,
                      totalCount: dockerImages.length,
                      countLabel: '镜像',
                      summaryItems: [
                        { label: '仓库', value: new Set(dockerImages.map(item => getDockerImageRepository(item))).size },
                        { label: '可清理', value: pruneCandidateCount, className: pruneCandidateCount > 0 ? 'text-kumo-warning' : 'text-kumo-success' },
                      ],
                      actions: (
                        <Button
                          size="sm"
                          variant={isDockerUpdateConfirmActive(allImagePruneConfirmKey) ? 'secondary-destructive' : 'primary'}
                          icon={<Trash className="h-3.5 w-3.5" />}
                          disabled={hosts.length === 0}
                          onClick={() => pruneDockerImagesForHosts(hosts, { confirmKey: allImagePruneConfirmKey })}
                          aria-label={isDockerUpdateConfirmActive(allImagePruneConfirmKey) ? '再次确认一键清理镜像' : '一键清理镜像'}
                          title={isDockerUpdateConfirmActive(allImagePruneConfirmKey) ? '再次点击确认清理全部主机镜像' : '一键清理全部主机未使用镜像'}
                        >
                          {isDockerUpdateConfirmActive(allImagePruneConfirmKey) ? '再次确认' : '一键清理'}
                        </Button>
                      ),
                      getHostCount: server => asArray(server.resources?.images).length,
                      getHostBadges: server => {
                        const images = asArray(server.resources?.images);
                        const pruneCandidates = images.filter(isDockerImagePruneCandidate).length;
                        return images.length > 0 ? [
                          { label: `${new Set(images.map(img => getDockerImageRepository(img))).size} 仓库` },
                          ...(pruneCandidates > 0 ? [{ label: `${pruneCandidates} 可清理`, variant: 'warning' }] : []),
                        ] : [];
                      },
                      renderHostAction: server => {
                        const prunePayload = { serverId: server.id };
                        const prunePending = isDockerActionPending(server.id, 'image.prune', prunePayload);
                        const pruneConfirmKey = `image.prune::${server.id}`;
                        return (
                          <Button
                            shape="square"
                            size="sm"
                            variant={isDockerUpdateConfirmActive(pruneConfirmKey) ? 'secondary-destructive' : 'secondary'}
                            icon={prunePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                            disabled={prunePending}
                            onClick={() => submitDockerPruneTask('image.prune', prunePayload, pruneConfirmKey, `清理 ${server.name} 未使用镜像`)}
                            aria-label={isDockerUpdateConfirmActive(pruneConfirmKey) ? `再次确认清理 ${server.name} 镜像` : `清理 ${server.name} 镜像`}
                            title={isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次点击确认清理' : '清理未使用镜像'}
                          />
                        );
                      },
                    })}
                    <div className="flex min-w-0 flex-col gap-3">
                      {hosts.length === 0 ? (
                        renderDockerEmptyState('未检测到可用的 Docker 主机')
                      ) : (
                        hosts.map(server => {
                          const images = asArray(server.resources?.images);
                          const prunePayload = { serverId: server.id };
                          const prunePending = isDockerActionPending(server.id, 'image.prune', prunePayload);
                          const pruneConfirmKey = `image.prune.section::${server.id}`;
                          const pruneCandidates = images.filter(isDockerImagePruneCandidate).length;
                          return renderDockerHostResourceSection({
                            server,
                            isFirstVisible: server.id === hosts[0]?.id,
                            icon: <HardDrive className="h-4 w-4 shrink-0 text-brand" />,
                            count: images.length,
                            countLabel: '镜像',
                            badges: images.length > 0 ? [
                              { label: `${new Set(images.map(img => getDockerImageRepository(img))).size} 仓库` },
                              ...(pruneCandidates > 0 ? [{ label: `${pruneCandidates} 可清理`, variant: 'warning' }] : []),
                            ] : [],
                            actions: (
                              <Button
                                size="sm"
                                variant={isDockerUpdateConfirmActive(pruneConfirmKey) ? 'secondary-destructive' : 'secondary'}
                                icon={prunePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                                disabled={prunePending}
                                onClick={(event) => {
                                  event.stopPropagation();
                                  submitDockerPruneTask('image.prune', prunePayload, pruneConfirmKey, `清理 ${server.name} 未使用镜像`);
                                }}
                              >
                                {isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次确认' : '清理镜像'}
                              </Button>
                            ),
                            children: images.length === 0 ? (
                              <div className="p-8 text-center text-xs text-kumo-subtle">
                                暂无镜像
                              </div>
                            ) : (
                              <div className="divide-y divide-kumo-line">
                                {images.map((img, i) => {
                                  const repository = getDockerImageRepository(img);
                                  const tag = getDockerImageTag(img);
                                  const imageId = getDockerImageId(img);
                                  const imageRef = imageId || (tag && tag !== '-' ? `${repository}:${tag}` : repository);
                                  const removePayload = { serverId: server.id, image: imageRef };
                                  const removePending = isDockerActionPending(server.id, 'image.remove', removePayload);
                                  return (
                                    <div key={`${server.id}-${imageId || repository}-${i}`} className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4">
                                      <div className="min-w-0 flex-1">
                                        <div className="truncate text-xs font-bold leading-5 text-kumo-strong" title={repository}>{repository}</div>
                                        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                          <Badge variant="secondary" className="font-mono text-[10px]">{tag}</Badge>
                                          <span className="shrink-0">{getDockerImageSize(img)}</span>
                                        </div>
                                      </div>
                                      <Button
                                        shape="square"
                                        size="sm"
                                        variant={isArmed(`docker.image.remove::${server.id}::${imageRef}`) ? 'destructive' : 'secondary-destructive'}
                                        aria-label={`删除 ${repository || '镜像'}`}
                                        disabled={removePending}
                                        onClick={() => {
                                          if (!confirmPress(`docker.image.remove::${server.id}::${imageRef}`, `删除镜像「${repository || '镜像'}」`)) return;
                                          submitDockerTask('image.remove', removePayload, { skipConfirm: true });
                                        }}
                                        className="text-kumo-danger"
                                        title="删除镜像"
                                      >
                                        {removePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                                      </Button>
                                    </div>
                                  );
                                })}
                              </div>
                            ),
                          });
                        })
                      )}
                    </div>
                  </div>
                );
              })()}

              {/* 4. 网络管理 */}
              {dockerSubTab === 'networks' && (() => {
                const hosts = dockerOverviewServers.filter(server => isDockerOverviewHostVisible(server, 'networks'));
                const allNetworkPruneConfirmKey = `network.prune.all::${hosts.map(server => server.id).join('|')}`;
                return (
                  <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                    {renderDockerResourceSideRail({
                      title: '网络',
                      icon: <Globe className="h-4 w-4 shrink-0 text-brand" />,
                      hosts,
                      totalCount: dockerNetworks.length,
                      countLabel: '网络',
                      summaryItems: [
                        { label: '驱动', value: new Set(dockerNetworks.map(item => getDockerNetworkDriver(item))).size },
                      ],
                      actions: (
                        <Button
                          size="sm"
                          variant={isDockerUpdateConfirmActive(allNetworkPruneConfirmKey) ? 'secondary-destructive' : 'primary'}
                          icon={<Trash className="h-3.5 w-3.5" />}
                          disabled={hosts.length === 0}
                          onClick={() => pruneDockerNetworksForHosts(hosts, { confirmKey: allNetworkPruneConfirmKey })}
                          aria-label={isDockerUpdateConfirmActive(allNetworkPruneConfirmKey) ? '再次确认一键清理网络' : '一键清理网络'}
                          title={isDockerUpdateConfirmActive(allNetworkPruneConfirmKey) ? '再次点击确认清理全部主机网络' : '一键清理全部主机未使用网络'}
                        >
                          {isDockerUpdateConfirmActive(allNetworkPruneConfirmKey) ? '再次确认' : '一键清理'}
                        </Button>
                      ),
                      getHostCount: server => asArray(server.resources?.networks).length,
                      getHostBadges: server => {
                        const networks = asArray(server.resources?.networks);
                        return networks.length > 0 ? [{ label: `${new Set(networks.map(network => getDockerNetworkDriver(network))).size} 驱动` }] : [];
                      },
                      renderHostAction: server => {
                        const prunePayload = { serverId: server.id };
                        const prunePending = isDockerActionPending(server.id, 'network.prune', prunePayload);
                        const pruneConfirmKey = `network.prune::${server.id}`;
                        return (
                          <Button
                            shape="square"
                            size="sm"
                            variant={isDockerUpdateConfirmActive(pruneConfirmKey) ? 'secondary-destructive' : 'secondary'}
                            icon={prunePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                            disabled={prunePending}
                            onClick={() => submitDockerPruneTask('network.prune', prunePayload, pruneConfirmKey, `清理 ${server.name} 未使用网络`)}
                            aria-label={isDockerUpdateConfirmActive(pruneConfirmKey) ? `再次确认清理 ${server.name} 网络` : `清理 ${server.name} 网络`}
                            title={isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次点击确认清理' : '清理未使用网络'}
                          />
                        );
                      },
                    })}
                    <div className="flex min-w-0 flex-col gap-3">
                      {hosts.length === 0 ? (
                        renderDockerEmptyState('未检测到可用的 Docker 主机')
                      ) : (
                        hosts.map(server => {
                          const networks = asArray(server.resources?.networks);
                          const prunePayload = { serverId: server.id };
                          const prunePending = isDockerActionPending(server.id, 'network.prune', prunePayload);
                          const pruneConfirmKey = `network.prune.section::${server.id}`;
                          return renderDockerHostResourceSection({
                            server,
                            isFirstVisible: server.id === hosts[0]?.id,
                            icon: <Globe className="h-4 w-4 shrink-0 text-brand" />,
                            count: networks.length,
                            countLabel: '网络',
                            badges: networks.length > 0 ? [{ label: `${new Set(networks.map(network => getDockerNetworkDriver(network))).size} 驱动` }] : [],
                            actions: (
                              <Button
                                size="sm"
                                variant={isDockerUpdateConfirmActive(pruneConfirmKey) ? 'secondary-destructive' : 'secondary'}
                                icon={prunePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                                disabled={prunePending}
                                onClick={(event) => {
                                  event.stopPropagation();
                                  submitDockerPruneTask('network.prune', prunePayload, pruneConfirmKey, `清理 ${server.name} 未使用网络`);
                                }}
                              >
                                {isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次确认' : '清理网络'}
                              </Button>
                            ),
                            children: networks.length === 0 ? (
                              <div className="p-8 text-center text-xs text-kumo-subtle">
                                暂无 Docker 网络
                              </div>
                            ) : (
                              <div className="divide-y divide-kumo-line">
                                {networks.map((network, i) => {
                                  const networkName = getDockerNetworkName(network);
                                  const networkId = getDockerNetworkId(network);
                                  const isBuiltinNetwork = ['bridge', 'host', 'none'].includes(networkName);
                                  const removePayload = { serverId: server.id, name: networkName };
                                  const removePending = isDockerActionPending(server.id, 'network.remove', removePayload);
                                  return (
                                    <div key={`${server.id}-${networkId || networkName}-${i}`} className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4">
                                      <div className="min-w-0 flex-1">
                                        <div className="truncate text-xs font-bold leading-5 text-kumo-strong" title={networkName}>{networkName}</div>
                                        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                          <span className="truncate font-mono">{networkId || '-'}</span>
                                          <Badge variant="teal" className="font-mono text-[10px]">{getDockerNetworkDriver(network)}</Badge>
                                          <Badge variant="neutral" className="font-mono text-[10px]">{getDockerNetworkScope(network)}</Badge>
                                        </div>
                                      </div>
                                      <Button
                                        shape="square"
                                        size="sm"
                                        variant={isArmed(`docker.network.remove::${server.id}::${networkName}`) ? 'destructive' : 'secondary-destructive'}
                                        aria-label={`删除 ${networkName || '网络'}`}
                                        disabled={isBuiltinNetwork || removePending}
                                        onClick={() => {
                                          if (!confirmPress(`docker.network.remove::${server.id}::${networkName}`, `删除网络「${networkName || '网络'}」`)) return;
                                          submitDockerTask('network.remove', removePayload, { skipConfirm: true });
                                        }}
                                        className="text-kumo-danger disabled:opacity-40"
                                        title={isBuiltinNetwork ? '内置网络不可删除' : '删除网络'}
                                      >
                                        {removePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                                      </Button>
                                    </div>
                                  );
                                })}
                              </div>
                            ),
                          });
                        })
                      )}
                    </div>
                  </div>
                );
              })()}

              {/* 5. 存储卷管理 */}
              {dockerSubTab === 'volumes' && (() => {
                const hosts = dockerOverviewServers.filter(server => isDockerOverviewHostVisible(server, 'volumes'));
                return (
                  <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                    {renderDockerResourceSideRail({
                      title: '存储卷',
                      icon: <Database className="h-4 w-4 shrink-0 text-brand" />,
                      hosts,
                      totalCount: dockerVolumes.length,
                      countLabel: '存储卷',
                      summaryItems: [
                        { label: '驱动', value: new Set(dockerVolumes.map(item => getDockerVolumeDriver(item))).size },
                      ],
                      getHostCount: server => asArray(server.resources?.volumes).length,
                      getHostBadges: server => {
                        const volumes = asArray(server.resources?.volumes);
                        return volumes.length > 0 ? [{ label: `${new Set(volumes.map(volume => getDockerVolumeDriver(volume))).size} 驱动` }] : [];
                      },
                      renderHostAction: server => {
                        const prunePayload = { serverId: server.id };
                        const prunePending = isDockerActionPending(server.id, 'volume.prune', prunePayload);
                        const pruneConfirmKey = `volume.prune::${server.id}`;
                        return (
                          <Button
                            shape="square"
                            size="sm"
                            variant={isDockerUpdateConfirmActive(pruneConfirmKey) ? 'secondary-destructive' : 'secondary'}
                            icon={prunePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                            disabled={prunePending}
                            onClick={() => submitDockerPruneTask('volume.prune', prunePayload, pruneConfirmKey, `清理 ${server.name} 未使用存储卷`)}
                            aria-label={isDockerUpdateConfirmActive(pruneConfirmKey) ? `再次确认清理 ${server.name} 存储卷` : `清理 ${server.name} 存储卷`}
                            title={isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次点击确认清理' : '清理未使用存储卷'}
                          />
                        );
                      },
                    })}
                    <div className="flex min-w-0 flex-col gap-3">
                      {hosts.length === 0 ? (
                        renderDockerEmptyState('未检测到可用的 Docker 主机')
                      ) : (
                        hosts.map(server => {
                          const volumes = asArray(server.resources?.volumes);
                          const prunePayload = { serverId: server.id };
                          const prunePending = isDockerActionPending(server.id, 'volume.prune', prunePayload);
                          const pruneConfirmKey = `volume.prune.section::${server.id}`;
                          return renderDockerHostResourceSection({
                            server,
                            isFirstVisible: server.id === hosts[0]?.id,
                            icon: <Database className="h-4 w-4 shrink-0 text-brand" />,
                            count: volumes.length,
                            countLabel: '存储卷',
                            badges: volumes.length > 0 ? [{ label: `${new Set(volumes.map(volume => getDockerVolumeDriver(volume))).size} 驱动` }] : [],
                            actions: (
                              <Button
                                size="sm"
                                variant={isDockerUpdateConfirmActive(pruneConfirmKey) ? 'secondary-destructive' : 'secondary'}
                                icon={prunePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                                disabled={prunePending}
                                onClick={(event) => {
                                  event.stopPropagation();
                                  submitDockerPruneTask('volume.prune', prunePayload, pruneConfirmKey, `清理 ${server.name} 未使用存储卷`);
                                }}
                              >
                                {isDockerUpdateConfirmActive(pruneConfirmKey) ? '再次确认' : '清理存储卷'}
                              </Button>
                            ),
                            children: volumes.length === 0 ? (
                              <div className="p-8 text-center text-xs text-kumo-subtle">
                                暂无 Docker 存储卷
                              </div>
                            ) : (
                              <div className="divide-y divide-kumo-line">
                                {volumes.map((volume, i) => {
                                  const volumeName = getDockerVolumeName(volume);
                                  const removePayload = { serverId: server.id, name: volumeName };
                                  const removePending = isDockerActionPending(server.id, 'volume.remove', removePayload);
                                  return (
                                    <div key={`${server.id}-${volumeName}-${i}`} className="flex min-w-0 items-center gap-3 px-3 py-2.5 cq-sm:px-4">
                                      <div className="min-w-0 flex-1">
                                        <div className="truncate text-xs font-bold leading-5 text-kumo-strong" title={volumeName}>{volumeName}</div>
                                        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-kumo-subtle">
                                          <Badge variant="teal" className="font-mono text-[10px]">{getDockerVolumeDriver(volume)}</Badge>
                                          <Badge variant="neutral" className="font-mono text-[10px]">{getDockerVolumeScope(volume)}</Badge>
                                        </div>
                                      </div>
                                      <Button
                                        shape="square"
                                        size="sm"
                                        variant={isArmed(`docker.volume.remove::${server.id}::${volumeName}`) ? 'destructive' : 'secondary-destructive'}
                                        aria-label={`删除 ${volumeName || '存储卷'}`}
                                        disabled={removePending}
                                        onClick={() => {
                                          if (!confirmPress(`docker.volume.remove::${server.id}::${volumeName}`, `删除存储卷「${volumeName || '存储卷'}」`)) return;
                                          submitDockerTask('volume.remove', removePayload, { skipConfirm: true });
                                        }}
                                        className="text-kumo-danger"
                                        title="删除存储卷"
                                      >
                                        {removePending ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : <Trash className="h-3.5 w-3.5" />}
                                      </Button>
                                    </div>
                                  );
                                })}
                              </div>
                            ),
                          });
                        })
                      )}
                    </div>
                  </div>
                );
              })()}

              {/* 6. 实时统计 */}
              {dockerSubTab === 'stats' && (() => {
                const hosts = dockerOverviewServers.filter(server => isDockerOverviewHostVisible(server, 'stats'));
                return (
                  <div className="grid min-w-0 gap-4 cq-xl:grid-cols-[22rem_minmax(0,1fr)]">
                    {renderDockerResourceSideRail({
                      title: '实时统计',
                      icon: <Activity className="h-4 w-4 shrink-0 text-brand" />,
                      hosts,
                      totalCount: dockerStats.length,
                      countLabel: '容器',
                      summaryItems: [
                        { label: 'CPU', value: `${dockerStatsSummary.cpu.toFixed(1)}%`, className: 'text-kumo-success' },
                        { label: '内存', value: `${dockerStatsSummary.memory.toFixed(1)}%`, className: 'text-kumo-info' },
                        { label: '网络', value: `${formatBytesValue(dockerStatsSummary.netIn)} / ${formatBytesValue(dockerStatsSummary.netOut)}` },
                        { label: '磁盘', value: `${formatBytesValue(dockerStatsSummary.blockRead)} / ${formatBytesValue(dockerStatsSummary.blockWrite)}` },
                      ],
                      getHostCount: server => asArray(server.resources?.stats).length,
                      getHostBadges: server => {
                        const serverHistory = dockerStatsHistory
                          .map(snapshot => snapshot.filter(item => item.serverId === server.id))
                          .filter(snapshot => snapshot.length > 0);
                        const latest = serverHistory[serverHistory.length - 1] || [];
                        const summary = latest.reduce((acc, item) => ({
                          cpu: acc.cpu + toNumber(item.cpu, 0),
                          memory: acc.memory + toNumber(item.memory, 0),
                        }), { cpu: 0, memory: 0 });
                        return latest.length > 0 ? [
                          { label: `CPU ${summary.cpu.toFixed(1)}%`, variant: 'success', appearance: 'dot' },
                          { label: `内存 ${summary.memory.toFixed(1)}%`, variant: 'info', appearance: 'dot' },
                        ] : [];
                      },
                    })}
                    <div className="flex min-w-0 flex-col gap-3">
                      {hosts.length === 0 ? (
                        renderDockerEmptyState('未检测到可用的 Docker 主机')
                      ) : (
                        hosts.map(server => {
                          const currentStats = asArray(server.resources?.stats);
                          const serverHistory = dockerStatsHistory
                            .map(snapshot => snapshot.filter(item => item.serverId === server.id))
                            .filter(snapshot => snapshot.length > 0);
                          const latest = serverHistory[serverHistory.length - 1] || [];
                          const summary = latest.reduce((acc, item) => ({
                            cpu: acc.cpu + toNumber(item.cpu, 0),
                            memory: acc.memory + toNumber(item.memory, 0),
                            netIn: acc.netIn + toNumber(item.netIn, 0),
                            netOut: acc.netOut + toNumber(item.netOut, 0),
                            blockRead: acc.blockRead + toNumber(item.blockRead, 0),
                            blockWrite: acc.blockWrite + toNumber(item.blockWrite, 0),
                          }), { cpu: 0, memory: 0, netIn: 0, netOut: 0, blockRead: 0, blockWrite: 0 });
                          const isDarkMode = theme === 'dark';
                          const netInColor = ChartPalette.categorical(0, isDarkMode);
                          const netOutColor = ChartPalette.semantic('Success', isDarkMode);
                          const readColor = ChartPalette.semantic('Warning', isDarkMode);
                          const writeColor = ChartPalette.categorical(3, isDarkMode);
                          const charts = [
                            {
                              key: 'cpu',
                              title: 'CPU 使用率',
                              description: '容器 CPU 趋势',
                              data: buildDockerStatSeries(serverHistory, 'cpu', isDarkMode),
                              value: `${summary.cpu.toFixed(1)}%`,
                              yAxisTickFormat: formatPercentAxis,
                              tooltipValueFormat: value => `${toNumber(value, 0).toFixed(2)}%`,
                            },
                            {
                              key: 'memory',
                              title: '内存使用率',
                              description: '容器内存趋势',
                              data: buildDockerStatSeries(serverHistory, 'memory', isDarkMode),
                              value: `${summary.memory.toFixed(1)}%`,
                              yAxisTickFormat: formatPercentAxis,
                              tooltipValueFormat: value => `${toNumber(value, 0).toFixed(2)}%`,
                            },
                            {
                              key: 'network',
                              title: '网络 I/O',
                              description: '容器网络流量',
                              data: buildDockerPairSeries(serverHistory, [
                                { key: 'netIn', name: '接收', color: netInColor },
                                { key: 'netOut', name: '发送', color: netOutColor },
                              ]),
                              value: `${formatBytesValue(summary.netIn)} / ${formatBytesValue(summary.netOut)}`,
                              yAxisTickFormat: formatBytesValue,
                              tooltipValueFormat: formatBytesValue,
                            },
                            {
                              key: 'disk',
                              title: '磁盘 I/O',
                              description: '容器磁盘流量',
                              data: buildDockerPairSeries(serverHistory, [
                                { key: 'blockRead', name: '读取', color: readColor },
                                { key: 'blockWrite', name: '写入', color: writeColor },
                              ]),
                              value: `${formatBytesValue(summary.blockRead)} / ${formatBytesValue(summary.blockWrite)}`,
                              yAxisTickFormat: formatBytesValue,
                              tooltipValueFormat: formatBytesValue,
                            },
                          ];
                          return renderDockerHostResourceSection({
                            server,
                            isFirstVisible: server.id === hosts[0]?.id,
                            icon: <Activity className="h-4 w-4 shrink-0 text-brand" />,
                            count: currentStats.length,
                            countLabel: '容器',
                            badges: currentStats.length > 0 ? [
                              { label: `CPU ${summary.cpu.toFixed(1)}%`, variant: 'success', appearance: 'dot' },
                              { label: `内存 ${summary.memory.toFixed(1)}%`, variant: 'info', appearance: 'dot' },
                            ] : [],
                            children: currentStats.length === 0 ? (
                              <div className="p-8 text-center text-xs text-kumo-subtle">
                                暂无 Docker 资源统计
                              </div>
                            ) : (
                              <div className="grid gap-3 cq-xl:grid-cols-2">
                                {charts.map(chart => (
                                  <ChartBoundaryBox key={chart.key} className="min-w-0 rounded-md border border-kumo-line bg-kumo-base p-3">
                                    {(tooltipBoundary) => (
                                      <div className="min-w-0">
                                        <div className="mb-3 flex min-w-0 items-start justify-between gap-3">
                                          <div className="min-w-0">
                                            <div className="truncate text-sm font-bold text-kumo-strong">{chart.title}</div>
                                            <div className="mt-0.5 truncate text-xs text-kumo-subtle">{chart.description}</div>
                                          </div>
                                          <Badge variant="neutral">{chart.value}</Badge>
                                        </div>
                                        {chart.data.length === 0 ? (
                                          <div className="flex h-[220px] items-center justify-center rounded-md border border-dashed border-kumo-line text-center text-xs text-kumo-subtle">
                                            暂无可绘制数据
                                          </div>
                                        ) : (
                                          <SiteFontTimeseriesChart
                                            echarts={fastTimeseriesEcharts}
                                            data={chart.data}
                                            height={220}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={dockerResourceLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            tooltipFollowCursor="x"
                                            xAxisTickCount={isCompactViewport ? 3 : 5}
                                            yAxisTickCount={4}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={chart.yAxisTickFormat}
                                            tooltipValueFormat={chart.tooltipValueFormat}
                                            optionUpdateBehavior={SERVER_FAST_CHART_UPDATE_BEHAVIOR}
                                            ariaDescription={`${server.name} ${chart.title}`}
                                          />
                                        )}
                                      </div>
                                    )}
                                  </ChartBoundaryBox>
                                ))}
                              </div>
                            ),
                          });
                        })
                      )}
                    </div>
                  </div>
                );
              })()}
            </div>
          )}
        </div>
      )}

      {/* ==================== 4. 后台管理 ==================== */}
      {serverCurrentTab === 'management' && (
        <div className="flex flex-col gap-4">
            <div className="grid grid-cols-1 items-start gap-4 cq-xl:grid-cols-2">
              <div className="flex min-w-0 flex-col gap-4">
            <SectionCard
              title="主机偏好"
              icon={<Settings className={MANAGEMENT_CARD_ICON_CLASS} />}
              actions={(
                <Button
                  size="sm"
                  variant="primary"
                  onClick={saveServerModuleSettings}
                  loading={serverSettingsSaving}
                  icon={<Save className="h-3.5 w-3.5" />}
                >
                  保存偏好
                </Button>
              )}
              bodyPadding="none"
            >
              <div className="divide-y divide-kumo-line/80">
                <div className="grid gap-3 px-4 py-3.5 cq-lg:grid-cols-[minmax(0,1fr)_minmax(14rem,22rem)] cq-lg:items-center">
                  <div className="min-w-0">
                    <div className="text-xs font-semibold leading-5 text-kumo-strong">主机地址显示</div>
                  </div>
                  <div className="flex justify-start cq-lg:justify-end">
                    <Select
                      size="sm"
                      aria-label="主机地址显示"
                      value={serverIpDisplayMode}
                      onValueChange={handleServerIpDisplayModeChange}
                      items={[
                        { value: 'normal', label: '明文' },
                        { value: 'masked', label: '打码' },
                        { value: 'hidden', label: '隐藏' },
                      ]}
                      className="w-full cq-lg:w-auto"
                    />
                  </div>
                </div>
                <div className="grid gap-3 px-4 py-3.5 cq-lg:grid-cols-[minmax(0,1fr)_minmax(18rem,1fr)] cq-lg:items-center">
                  <div className="min-w-0">
                    <div className="text-xs font-semibold leading-5 text-kumo-strong">Agent 下载目录</div>
                    <div className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
                      留空使用主控端内置 /agent 目录；自定义时填目录 URL，不填文件名。
                    </div>
                  </div>
                  <div className="flex justify-start cq-lg:justify-end">
                    <Input
                      size="sm"
                      aria-label="Agent 下载目录"
                      value={serverSettingsForm.agentDownloadUrl}
                      onChange={(event) => setServerSettingsForm(prev => ({ ...prev, agentDownloadUrl: event.target.value }))}
                      placeholder="https://cdn.example.com/agent"
                      className="w-full cq-lg:w-auto"
                    />
                  </div>
                </div>
              </div>
            </SectionCard>

            <SectionCard
              title="批量录入"
              icon={<FolderOpen className={MANAGEMENT_CARD_ICON_CLASS} />}
              meta={<span className="text-xs font-semibold text-kumo-subtle">CSV</span>}
              bodyPadding="sm"
              bodyClassName="flex flex-col gap-2"
            >
                <CodeEditor
                  label="主机列表"
                  language="text"
                  value={serverBatchText}
                  onChange={setServerBatchText}
                  placeholder="名称,IP,端口,用户名,密码 
例如: prod-server,192.168.1.10,22,root,password"
                  minHeight="7rem"
                />
                {serverBatchError && <Badge variant="error">{serverBatchError}</Badge>}
                {serverBatchSuccess && <Badge variant="success">{serverBatchSuccess}</Badge>}
                <Button
                  size="sm"
                  variant="primary"
                  onClick={batchAddServers}
                  loading={serverAddingBatch}
                  disabled={!serverBatchText.trim()}
                  icon={<Plus className="h-3.5 w-3.5" />}
                  className="w-fit"
                >
                  批量录入
                </Button>
            </SectionCard>

            <SectionCard
              title="配置迁移"
              icon={<Database className={MANAGEMENT_CARD_ICON_CLASS} />}
              meta={<span className="text-xs font-semibold text-kumo-subtle">JSON</span>}
              bodyPadding="sm"
              bodyClassName="flex flex-col gap-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between"
            >
                <div className="min-w-0 space-y-1">
                  <div className="text-xs font-medium text-kumo-strong">主机配置</div>
                  <div className="max-w-xl text-xs leading-5 text-kumo-subtle">
                    包含连接、认证、标签等配置。
                  </div>
                </div>
                <div className="flex w-full flex-col gap-2 cq-sm:w-auto cq-sm:flex-row cq-sm:items-center">
                <Toolbar size="sm" aria-label="导出导入主机配置" className="w-full justify-center cq-sm:w-auto">
                  <Toolbar.Button
                    onClick={exportServers}
                    aria-label="导出主机配置备份"
                    icon={<Upload className="h-3.5 w-3.5" />}
                    className="w-full cq-sm:w-auto"
                  >
                    <span className="hidden cq-sm:inline">导出</span>
                  </Toolbar.Button>
                  <Toolbar.Button
                    onClick={openImportServerModal}
                    aria-label="导入主机配置"
                    icon={<Download className="h-3.5 w-3.5" />}
                    className="w-full cq-sm:w-auto"
                  >
                    <span className="hidden cq-sm:inline">导入</span>
                  </Toolbar.Button>
                </Toolbar>
                </div>
            </SectionCard>
              </div>

              <div className="flex min-w-0 flex-col gap-4">
            <SectionCard
              title="SSH 凭据库"
              icon={<Key className={MANAGEMENT_CARD_ICON_CLASS} />}
              actions={(
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => setShowAddCredentialModal(true)}
                  icon={<Plus className="h-3.5 w-3.5" />}
                >
                  添加
                </Button>
              )}
              bodyPadding="none"
            >
                {serverCredentials.length === 0 ? (
                  <div className="px-3 py-8 text-center text-xs text-kumo-subtle">暂无预设访问凭据</div>
                ) : (
                  <div className="max-h-64 overflow-auto">
                    <Table layout="fixed">
                      <colgroup>
                        <col style={{ width: 50 }} />
                        <col style={{ width: 120 }} />
                        <col style={{ width: 86 }} />
                      </colgroup>
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head className="text-left">名称</Table.Head>
                          <Table.Head className="text-center">用户</Table.Head>
                          <Table.Head className="app-table-action">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {serverCredentials.map(cred => (
                          <Table.Row key={cred.id} className="border-b border-kumo-line/80 hover:bg-kumo-recessed/10 text-center">
                            <Table.Cell className="whitespace-nowrap">
                              {/* <div className="flex justify-center"> */}
                                <div className="flex min-w-0 items-center gap-2">
                                  <span className="truncate font-semibold text-kumo-strong" title={cred.name}>{cred.name}</span>
                                  {cred.is_default && <Badge variant="success" appearance="dot">默认</Badge>}
                                </div>
                              {/* </div> */}
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap font-mono text-[11px] text-kumo-subtle" title={cred.username}>
                              {cred.username}
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap text-center">
                              <div className="inline-flex items-center justify-center gap-1">
                                {!cred.is_default && (
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="secondary"
                                    aria-label="设为默认凭据"
                                    onClick={() => setDefaultCredential(cred.id)}
                                    icon={<Star className="h-3.5 w-3.5" />}
                                    title="设为默认"
                                  />
                                )}
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant={isArmed(`credential.delete::${cred.id}`) ? 'destructive' : 'secondary-destructive'}
                                  aria-label="删除凭据"
                                  onClick={() => deleteCredential(cred.id)}
                                  icon={<Trash className="h-3.5 w-3.5" />}
                                  title="删除"
                                />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  </div>
                )}
            </SectionCard>

            <SectionCard
              title="网络拨测目标"
              icon={<Globe className={MANAGEMENT_CARD_ICON_CLASS} />}
              actions={(
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => {
                    setNetworkTargetModalMode('add');
                    setNetworkTargetForm({
                      id: null,
                      name: '',
                      host: '',
                      port: 80,
                      type: 'tcp',
                      enabled: true,
                      order_index: 0,
                    });
                    setShowNetworkTargetModal(true);
                  }}
                  icon={<Plus className="h-3.5 w-3.5" />}
                >
                  添加
                </Button>
              )}
              bodyPadding="none"
            >
                {networkTargets.length === 0 ? (
                  <div className="px-3 py-8 text-center text-xs text-kumo-subtle">暂无监测目标</div>
                ) : (
                  <div className="max-h-64 overflow-auto">
                    <Table layout="fixed">
                      <colgroup>
                        <col style={{ width: 80 }} />
                        <col style={{ width: 150 }} />
                        <col style={{ width: 60 }} />
                        <col style={{ width: 86 }} />
                      </colgroup>
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head className="text-center">名称</Table.Head>
                          <Table.Head className="text-center">节点地址</Table.Head>
                          <Table.Head className="text-center">状态</Table.Head>
                          <Table.Head className="app-table-action">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {networkTargets.map(target => (
                          <Table.Row key={target.id} className="border-b border-kumo-line/80 hover:bg-kumo-recessed/10 text-center">
                            <Table.Cell className="whitespace-nowrap">
                              <span className="font-semibold text-kumo-strong truncate block" title={target.name}>
                                {target.name}
                              </span>
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap font-mono text-[11px] text-kumo-subtle truncate text-center" title={`${target.host}:${target.port}`}>
                              {target.host}:{target.port} ({target.type})
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap">
                              <div className="flex justify-center">
                                <Checkbox
                                  checked={!!target.enabled}
                                  onChange={() => toggleNetworkTargetEnabled(target)}
                                  aria-label={`${target.name} 启用状态`}
                                />
                              </div>
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap text-center">
                              <div className="inline-flex items-center justify-center gap-1">
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary"
                                  aria-label="编辑目标"
                                  onClick={() => {
                                    setNetworkTargetModalMode('edit');
                                    setNetworkTargetForm({
                                      id: target.id,
                                      name: target.name,
                                      host: target.host,
                                      port: target.port,
                                      type: target.type || 'tcp',
                                      enabled: !!target.enabled,
                                      order_index: target.order_index ?? 0,
                                    });
                                    setShowNetworkTargetModal(true);
                                  }}
                                  icon={<Edit className="h-3.5 w-3.5" />}
                                  title="编辑"
                                />
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant={isArmed(`network-target.delete::${target.id}`) ? 'destructive' : 'secondary-destructive'}
                                  aria-label="删除目标"
                                  onClick={() => deleteNetworkTarget(target.id)}
                                  icon={<Trash className="h-3.5 w-3.5" />}
                                  title="删除"
                                />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  </div>
                )}
            </SectionCard>
              </div>
            </div>
        </div>
      )}

      {/* ==================== 5. SSH 终端 (多分屏支持) ==================== */}
      {serverCurrentTab === 'terminal' && (() => {
        const activeSession = sshSessions.find(s => s.id === activeSSHSessionId);
        const activeServer = serverList.find(server => String(server.id) === String(activeSession?.server?.id)) || activeSession?.server;
        const activeInfo = activeServer?.info || {};
        const activePrimaryDisk = activeInfo.disk?.[0];
        const terminalCpuUsage = clampPercent(toNumber(activeInfo.cpu?.Usage, 0));
        const terminalMemUsage = clampPercent(toNumber(activeInfo.memory?.Usage, 0));
        const terminalDiskUsage = clampPercent(toNumber(activePrimaryDisk?.usage, 0));
        const terminalGpuUsage = clampPercent(toNumber(activeInfo.gpu?.Usage, 0));
        const terminalCpuTemp = toNumber(activeInfo.cpu?.Temp, 0);
        const terminalCpuPower = toNumber(activeInfo.cpu?.Power, 0);
        const terminalGpuTemp = toNumber(activeInfo.gpu?.Temp, 0);
        const terminalPhysicalCores = activeInfo.cpu?.PhysicalCores || activeInfo.cpu?.Cores;
        const terminalLogicalCores = activeInfo.cpu?.LogicalCores;
        const terminalCoreText = terminalPhysicalCores && terminalLogicalCores && terminalPhysicalCores !== terminalLogicalCores
          ? `${terminalPhysicalCores}核 / ${terminalLogicalCores}线程`
          : `${terminalPhysicalCores || '-'} 核`;
        const terminalGpuModel = getGpuModelText(activeInfo.gpu);
        const terminalHasGpu = !!terminalGpuModel || terminalGpuUsage > 0 || terminalGpuTemp > 0;
        const terminalTxTotal = getByteParts(activeInfo.network?.tx_total);
        const terminalRxTotal = getByteParts(activeInfo.network?.rx_total);
        return (
          <AppCard padding="none" className="flex h-full min-h-0 w-full flex-1 flex-col overflow-visible">
            <div className="flex min-h-11 items-center justify-between gap-3 rounded-t-[inherit] border-b border-kumo-line bg-kumo-base px-3 py-2 text-xs">
              <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto scrollbar-thin">
                {sshSessions.map(sess => (
                  <div
                    key={sess.id}
                    draggable
                    onDragStart={event => handleTerminalDragStart(event, sess.id)}
                    onDragEnd={() => {
                      setDraggedSessionId(null);
                      setDropTargetId(null);
                      setDropHint('');
                    }}
                    className={`flex h-7 shrink-0 items-center rounded-md border ${activeSSHSessionId === sess.id
                      ? 'border-brand/60 bg-kumo-recessed text-kumo-strong'
                      : 'border-kumo-line bg-kumo-base text-kumo-subtle'
                      }`}
                  >
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        if (visibleSessionIds.includes(sess.id)) {
                          activateSSHSession(sess.id);
                        } else {
                          switchToSSHTab(sess.id);
                        }
                      }}
                      className="h-full min-w-0 justify-start px-2 text-[11px] font-semibold text-inherit"
                    >
                      <span className={`h-1.5 w-1.5 rounded-full ${sess.connected ? 'bg-kumo-success' : 'bg-kumo-warning'}`}></span>
                      <span className="max-w-28 truncate">{sess.name}</span>
                      <span className="text-[9px] uppercase text-kumo-subtle">{sess.type}</span>
                    </Button>
                    <Button
                      shape="square" size="sm"
                      variant="ghost"
                      icon={<X className="h-3 w-3" />}
                      aria-label="关闭终端会话"
                      title="关闭终端会话"
                      onClick={e => {
                        e.stopPropagation();
                        closeSSHSession(sess.id);
                      }}
                      className="hover:text-kumo-danger"
                    />
                  </div>
                ))}
              </div>

              <div className="flex shrink-0 items-center gap-1.5">
                <Button
                  size="sm"
                  variant={sshSyncEnabled ? 'primary' : 'secondary'}
                  icon={<Send className="h-3.5 w-3.5" />}
                  onClick={() => setSshSyncEnabled(prev => !prev)}
                  title="同步输入"
                >
                  同步{sshSyncEnabled ? '开' : '关'}
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  icon={<RefreshCw className="h-3.5 w-3.5" />}
                  onClick={() => reconnectSSHSession(activeSSHSessionId)}
                  disabled={!activeSSHSessionId}
                  title="恢复连接"
                >
                  重连
                </Button>
              </div>
            </div>

            <div className="flex min-h-0 flex-1 overflow-hidden rounded-b-[inherit]">
              <div className="flex min-w-0 flex-1 flex-col">
                <div className="relative flex min-h-0 flex-1 overflow-hidden">
                  <div
                    className={`grid min-h-0 min-w-0 flex-1 gap-1.5 overflow-hidden bg-kumo-recessed p-1.5 transition-[margin] duration-200 ${activeTerminalSidebar ? 'mr-[clamp(18rem,24vw,26rem)]' : ''} ${sshViewLayout === 'split-h' ? 'grid-cols-[minmax(0,1fr)_minmax(0,1fr)] grid-rows-1' :
                      sshViewLayout === 'split-v' ? 'grid-cols-1 grid-rows-[minmax(0,1fr)_minmax(0,1fr)]' :
                        sshViewLayout === 'grid' ? 'grid-cols-[minmax(0,1fr)_minmax(0,1fr)] grid-rows-[minmax(0,1fr)_minmax(0,1fr)]' : 'grid-cols-1 grid-rows-1'
                      }`}
                  >
                    {visibleSessionIds.map((id, index) => {
                      const slotSession = sshSessions.find(s => s.id === id);
                      const showDropPreview = dropTargetId === id && dropHint && draggedSessionId && draggedSessionId !== id;
                      return (
                        <div
                          key={id}
                          onMouseDown={() => activateSSHSession(id)}
                          onDragOver={e => handleTerminalDragOver(e, id)}
                          onDragLeave={e => handleTerminalDragLeave(e, id)}
                          onDrop={e => {
                            e.preventDefault();
                            const sourceId = draggedSessionId || e.dataTransfer?.getData('text/plain') || undefined;
                            triggerSplitPane(id, getDropPosition(e), sourceId);
                            setDropTargetId(null);
                            setDropHint('');
                          }}
                          className={`relative flex h-full min-h-0 w-full min-w-0 flex-col overflow-hidden rounded-md border bg-kumo-base ${activeSSHSessionId === id ? 'border-kumo-interact' : 'border-kumo-line'
                            }`}
                        >
                          <div
                            draggable
                            onDragStart={event => handleTerminalDragStart(event, id)}
                            className="flex h-8 cursor-move select-none items-center justify-between border-b border-kumo-line bg-kumo-base px-2 text-[10px] text-kumo-subtle"
                          >
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              onClick={() => activateSSHSession(id)}
                              className="h-6 min-w-0 justify-start px-0 text-[10px] font-semibold text-kumo-strong"
                            >
                              <TerminalIcon className="h-3.5 w-3.5 shrink-0" />
                              <span className="truncate">{slotSession?.name || '终端'}</span>
                            </Button>
                            <div className="flex items-center gap-1">
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => createAttachedTerminalView(id, 'right')}
                                title="复制终端"
                              >
                                复制
                              </Button>
                              <Button
                                shape="square" size="sm"
                                variant="ghost"
                                icon={<X className="h-3 w-3" />}
                                aria-label="关闭终端"
                                title="关闭终端"
                                onClick={event => {
                                  event.stopPropagation();
                                  removeSSHSessionFromView(id);
                                }}
                                className="hover:text-kumo-danger"
                              />
                            </div>
                          </div>
                          <div id={`ssh-slot-idx-${index}`} className="app-terminal-surface min-h-0 flex-1 overflow-hidden" />
                          {showDropPreview && (
                            <div
                              className="pointer-events-none absolute z-20 rounded-md border border-dashed border-brand bg-brand/10 ring-1 ring-brand/25"
                              style={getTerminalDropPreviewStyle(dropHint)}
                            />
                          )}
                        </div>
                      );
                    })}
                  </div>

                  {activeTerminalSidebar && (
                    <div className="absolute bottom-0 right-0 top-0 z-10 flex min-h-0 w-[clamp(18rem,24vw,26rem)] flex-col overflow-hidden border-l border-kumo-line bg-kumo-base shadow-[-12px_0_24px_-24px_rgba(0,0,0,0.5)]">
                      {showServerStatusSidebar && (
                        <div className="flex h-full min-h-0 flex-col p-2.5 text-xs">
                          <div className="mb-2.5 flex items-center justify-between border-b border-kumo-line pb-2">
                            <span className="text-[11px] font-bold text-kumo-strong">资源监控</span>
                            <Button
                              shape="square" size="sm"
                              variant="ghost"
                              icon={<X className="h-3 w-3" />}
                              aria-label="关闭资源监控"
                              title="关闭资源监控"
                              onClick={() => setActiveTerminalSidebar(null)}
                            />
                          </div>
                          <div className="min-h-0 space-y-2 overflow-y-auto px-1 pb-1 pr-2">
                            <div className="min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-2">
                              <div className="flex min-w-0 items-center gap-2">
                                <i className={getOSIconClass(activeInfo.platform)}></i>
                                <div className="min-w-0">
                                  <div className="truncate text-xs font-bold text-kumo-strong">{activeSession?.name || activeServer?.name || '-'}</div>
                                  <div className="mt-1 truncate font-mono text-[10px] text-kumo-subtle">{getHostAddress(activeServer, serverIpDisplayMode) || activeServer?.host || 'Agent'}</div>
                                </div>
                              </div>
                            </div>

                            <ExpandedSection title="资源状态" tone="success">
                              <div className="grid grid-cols-2 gap-1.5">
                                <ExpandedProgressMetric
                                  label="CPU"
                                  value={terminalCpuUsage}
                                  detail={`${Math.round(terminalCpuUsage)}%`}
                                  caption={`${terminalCoreText}${terminalCpuTemp > 0 ? ` · ${Math.round(terminalCpuTemp)}°C` : ''}${terminalCpuPower > 0 ? ` · ${terminalCpuPower.toFixed(1)}W` : ''}`}
                                  indicatorClassName="!bg-none !bg-kumo-success"
                                  valueClassName="text-kumo-success"
                                />
                                <ExpandedProgressMetric
                                  label="内存"
                                  value={terminalMemUsage}
                                  detail={`${Math.round(terminalMemUsage)}%`}
                                  caption={`${activeInfo.memory?.Used || '-'} / ${activeInfo.memory?.Total || '-'}`}
                                  indicatorClassName="!bg-none !bg-kumo-info"
                                  valueClassName="text-kumo-info"
                                />
                                {activePrimaryDisk && (
                                  <ExpandedProgressMetric
                                    label="磁盘"
                                    value={terminalDiskUsage}
                                    detail={`${Math.round(terminalDiskUsage)}%`}
                                    caption={`${activePrimaryDisk.used || '-'} / ${activePrimaryDisk.total || '-'}`}
                                    indicatorClassName="!bg-none !bg-kumo-warning"
                                    valueClassName="text-kumo-warning"
                                  />
                                )}
                                {terminalHasGpu && (
                                  <ExpandedProgressMetric
                                    label="GPU"
                                    value={terminalGpuUsage}
                                    detail={`${Math.round(terminalGpuUsage)}%`}
                                    caption={`${terminalGpuModel || 'GPU'}${terminalGpuTemp > 0 ? ` · ${Math.round(terminalGpuTemp)}°C` : ''}`}
                                    indicatorClassName="!bg-none !bg-kumo-warning"
                                    valueClassName="text-kumo-warning"
                                  />
                                )}
                              </div>
                            </ExpandedSection>

                            <ExpandedSection title="系统概览" tone="brand">
                              <div className="grid grid-cols-1 gap-1.5">
                                <ExpandedInfoChip label="系统" value={activeInfo.platform || activeInfo.platformVersion || activeInfo.system?.Kernel || '-'} />
                                <ExpandedInfoChip label="CPU 型号" value={activeInfo.cpu?.Model || activeServer?.metadata?.cpu_model || activeServer?.metadata?.cpu_name || '-'} />
                                <ExpandedInfoChip label="负载" value={activeInfo.cpu?.Load || '-'} valueClassName="font-mono text-kumo-strong" />
                                <ExpandedInfoChip label="在线" value={formatUptimeDaysOnly(activeInfo.uptime || activeInfo.system?.Uptime)} />
                                <ExpandedInfoChip label="Agent 版本" value={activeInfo.agentVersion || '-'} />
                              </div>
                            </ExpandedSection>

                            {activeInfo.network && (
                              <ExpandedSection title="网络" tone="info">
                                <div className="grid grid-cols-2 gap-1.5">
                                  <ExpandedStatTile label="上传" value={activeInfo.network.tx_speed || '0 B/s'} tone="info" />
                                  <ExpandedStatTile label="下载" value={activeInfo.network.rx_speed || '0 B/s'} tone="success" />
                                  <ExpandedInfoChip label="累计上行" value={terminalTxTotal.text} valueClassName="text-kumo-info" className="col-span-2" />
                                  <ExpandedInfoChip label="累计下行" value={terminalRxTotal.text} valueClassName="text-kumo-success" className="col-span-2" />
                                  <ExpandedInfoChip label="连接" value={activeInfo.network.connections || 0} className="col-span-2" />
                                </div>
                              </ExpandedSection>
                            )}
                          </div>
                        </div>
                      )}
                      {showSftpSidebar && (
                        <div className="flex h-full min-h-0 overflow-hidden">
                          <SftpPanel
                            serverId={activeServer?.id || sftpServerId}
                            serverName={activeServer?.name}
                            initialPath={sftpPathByServerRef.current[activeServer?.id] || sftpCurrentPath || '.'}
                            onClose={() => setActiveTerminalSidebar(null)}
                            onPathChange={(serverId, path) => {
                              sftpPathByServerRef.current[serverId] = path;
                              setSftpServerId(serverId);
                              setSftpCurrentPath(path);
                            }}
                          />
                        </div>
                      )}
                      {showCommandSidebar && (
                        <div className="flex h-full min-h-0 overflow-hidden">
                          <QuickCommandBar
                            activeServer={activeServer}
                            activeSessionId={activeSSHSessionId}
                            sessions={sshSessions}
                            visibleSessionIds={visibleSessionIds}
                            syncEnabled={sshSyncEnabled}
                            onRunCommand={(command, options) => runQuickCommand(command, options)}
                          />
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </div>
              <div className="flex w-11 shrink-0 flex-col items-center gap-3 border-l border-kumo-line bg-kumo-base py-3 text-kumo-subtle">
                <Button
                  shape="square" size="sm"
                  variant={showServerStatusSidebar ? 'secondary' : 'ghost'}
                  icon={<Activity className="h-4 w-4" />}
                  aria-label="资源监控"
                  title="资源监控"
                  onClick={() => toggleTerminalSidebar('status')}
                />
                <Button
                  shape="square" size="sm"
                  variant={showSftpSidebar ? 'secondary' : 'ghost'}
                  icon={<FolderOpen className="h-4 w-4" />}
                  aria-label="SFTP 文件浏览"
                  title="SFTP 文件浏览"
                  onClick={() => {
                    const serverId = sshSessions.find(s => s.id === activeSSHSessionId)?.server.id;
                    toggleTerminalSidebar('sftp', { serverId });
                  }}
                />
                <Button
                  shape="square" size="sm"
                  variant={showCommandSidebar ? 'secondary' : 'ghost'}
                  icon={<TerminalIcon className="h-4 w-4" />}
                  aria-label="命令片段"
                  title="命令片段"
                  onClick={() => toggleTerminalSidebar('commands')}
                />
              </div>
            </div>
          </AppCard>
        );
      })()}

      {/* ==================== xterm.js 实例静默挂载的仓库 ==================== */}
      <div ref={warehouseRef} className="hidden absolute -top-[9999px]" id="ssh-terminal-warehouse"></div>

      {/* ==================== 模态框: 添加与编辑服务器 ==================== */}
      <Dialog.Root open={showServerModal} onOpenChange={setShowServerModal}>
        <Dialog size="xl" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[48rem] flex-col overflow-hidden p-0 cq-sm:max-w-[calc(100vw-3rem)]">
          <div ref={serverModalPortalRef} className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
                {serverModalMode === 'add' ? '新增主机实例' : '编辑主机实例'}
              </Dialog.Title>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square" size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-w-0 flex flex-1 flex-col gap-3 overflow-y-auto p-4 text-xs">
              {serverModalMode === 'add' && (
                <Tabs
                  {...TOOL_TABS_PROPS}
                  value={serverAddMode}
                  onValueChange={(value) => {
                    setServerAddMode(value);
                    setServerModalError('');
                  }}
                  tabs={[
                    { value: 'ssh', label: 'SSH' },
                    { value: 'agent', label: 'Agent' },
                  ]}
                />
              )}

              {serverModalMode === 'edit' || serverAddMode === 'ssh' ? (
                <>
                  <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)]">
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">主机名称 (别名)</label>
                      <Input size="sm"
                        aria-label="主机名称"
                        type="text"
                        value={serverForm.name}
                        onChange={e => setServerForm(prev => ({ ...prev, name: e.target.value }))}
                        placeholder="生产数据库-01"
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">开始时间</label>
                      <Input size="sm"
                        aria-label="开始时间"
                        type="date"
                        value={serverForm.startsAt}
                        onChange={e => setServerForm(prev => ({ ...prev, startsAt: e.target.value }))}
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">到期时间</label>
                      <Input size="sm"
                        aria-label="到期时间"
                        type="date"
                        value={serverForm.expiresAt}
                        onChange={e => setServerForm(prev => ({ ...prev, expiresAt: e.target.value }))}
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                  </div>

                  <section className="rounded-lg border border-kumo-line bg-kumo-recessed/20 p-3.5">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <h3 className="font-bold text-kumo-strong">流量配置</h3>
                      <span className="text-[11px] text-kumo-subtle">配额、报警与重置周期</span>
                    </div>

                    <div className="grid grid-cols-1 gap-3 cq-lg:grid-cols-[minmax(0,1.35fr)_minmax(18rem,0.85fr)]">
                      <div className="grid min-w-0 grid-cols-1 gap-3 cq-sm:grid-cols-[minmax(0,1fr)_8rem]">
                        <div className="flex min-w-0 flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">流量配额</label>
                          <div className="grid grid-cols-[minmax(0,1fr)_4.5rem] gap-2">
                            <Input size="sm"
                              aria-label="总流量配额"
                              type="number"
                              min="0"
                              step="0.001"
                              value={serverForm.trafficLimitValue}
                              onChange={e => setServerForm(prev => ({ ...prev, trafficLimitValue: e.target.value }))}
                              placeholder="留空则不限额"
                              className="px-3 py-2 text-kumo-strong"
                            />
                            <Select size="sm"
                              aria-label="流量配额单位"
                              value={serverForm.trafficLimitUnit}
                              onValueChange={(value) => setServerForm(prev => ({ ...prev, trafficLimitUnit: String(value) }))}
                              className="px-3 py-2"
                              items={[
                                { value: 'GB', label: 'GB' },
                                { value: 'TB', label: 'TB' },
                                { value: 'PB', label: 'PB' },
                              ]}
                            />
                          </div>
                        </div>
                        <div className="flex min-w-0 flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">统计范围</label>
                          <Select size="sm"
                            aria-label="配额方向"
                            value={serverForm.trafficLimitMode}
                            onValueChange={(value) => setServerForm(prev => ({ ...prev, trafficLimitMode: String(value) }))}
                            className="px-3 py-2"
                            items={[{ value: 'total', label: '总流量' }, { value: 'upload', label: '上行' }, { value: 'download', label: '下行' }]}
                          />
                        </div>
                      </div>

                      <div className="flex min-w-0 flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">流量报警</label>
                        <div className="grid min-w-0 grid-cols-[auto_minmax(4.75rem,1fr)_auto_auto] items-center gap-2">
                          <div className="whitespace-nowrap">
                            <Checkbox
                              label="启用"
                              checked={Boolean(serverForm.trafficAlertEnabled)}
                              disabled={trafficQuotaInputToBytes(serverForm.trafficLimitValue, serverForm.trafficLimitUnit) <= 0}
                              onCheckedChange={(checked) => setServerForm(prev => ({ ...prev, trafficAlertEnabled: Boolean(checked) }))}
                            />
                          </div>
                          <Input size="sm"
                            aria-label="报警阈值百分比"
                            type="number"
                            min="1"
                            max="100"
                            step="1"
                            value={serverForm.trafficAlertPercent}
                            disabled={!serverForm.trafficAlertEnabled || trafficQuotaInputToBytes(serverForm.trafficLimitValue, serverForm.trafficLimitUnit) <= 0}
                            onChange={e => setServerForm(prev => ({ ...prev, trafficAlertPercent: e.target.value }))}
                            onBlur={() => setServerForm(prev => ({ ...prev, trafficAlertPercent: normalizeTrafficAlertPercentInput(prev.trafficAlertPercent) }))}
                            className="min-w-0 px-3 py-2 text-kumo-strong"
                          />
                          <span className="text-kumo-subtle">%</span>
                          <Button
                            type="button"
                            size="sm"
                            variant="secondary"
                            disabled={serverModalSaving || !serverForm.trafficAlertEnabled || trafficQuotaInputToBytes(serverForm.trafficLimitValue, serverForm.trafficLimitUnit) <= 0}
                            onClick={testTrafficAlert}
                            className="whitespace-nowrap px-3 py-1.5 text-xs font-semibold"
                          >
                            测试
                          </Button>
                        </div>
                      </div>
                    </div>

                    <div className="mt-3 grid grid-cols-1 gap-3 border-t border-kumo-line pt-3 cq-sm:grid-cols-3">
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">流量周期</label>
                        <Select size="sm"
                          aria-label="流量周期"
                          value={serverForm.trafficCycleType}
                          onValueChange={(value) => setServerForm(prev => ({ ...prev, trafficCycleType: String(value) }))}
                          className="px-3 py-2"
                          items={TRAFFIC_CYCLE_OPTIONS}
                        />
                      </div>
                      {serverForm.trafficCycleType === 'monthly' && (
                        <div className="flex flex-col gap-1.5 cq-sm:col-span-2">
                          <label className="font-semibold text-kumo-subtle">账单日</label>
                          <Input size="sm"
                            aria-label="每月流量重置日"
                            type="number"
                            min="1"
                            max="28"
                            step="1"
                            value={serverForm.trafficCycleDay}
                            onChange={e => setServerForm(prev => ({ ...prev, trafficCycleDay: e.target.value }))}
                            onBlur={() => setServerForm(prev => ({ ...prev, trafficCycleDay: normalizeTrafficCycleDayInput(prev.trafficCycleDay) }))}
                            className="px-3 py-2 text-kumo-strong"
                          />
                        </div>
                      )}
                      {serverForm.trafficCycleType === 'custom' && (
                        <div className="grid grid-cols-1 gap-3 cq-sm:col-span-2 cq-sm:grid-cols-2">
                          <div className="flex flex-col gap-1.5">
                            <label className="font-semibold text-kumo-subtle">周期开始</label>
                            <Input size="sm"
                              aria-label="流量周期开始"
                              type="date"
                              value={serverForm.trafficCycleStart}
                              onChange={e => setServerForm(prev => ({ ...prev, trafficCycleStart: e.target.value }))}
                              className="px-3 py-2 text-kumo-strong"
                            />
                          </div>
                          <div className="flex flex-col gap-1.5">
                            <label className="font-semibold text-kumo-subtle">周期结束</label>
                            <Input size="sm"
                              aria-label="流量周期结束"
                              type="date"
                              value={serverForm.trafficCycleEnd}
                              onChange={e => setServerForm(prev => ({ ...prev, trafficCycleEnd: e.target.value }))}
                              className="px-3 py-2 text-kumo-strong"
                            />
                          </div>
                        </div>
                      )}
                      {(serverForm.trafficCycleType === 'calendar_month' || serverForm.trafficCycleType === 'none') && (
                        <div className="rounded-md border border-kumo-line bg-kumo-surface px-3 py-2 text-xs text-kumo-subtle cq-sm:col-span-2">
                          {serverForm.trafficCycleType === 'calendar_month' ? '每月 1 日作为新流量周期。' : '不设置重置周期，按累计流量显示。'}
                        </div>
                      )}
                    </div>
                  </section>

                  <section className="rounded-lg border border-kumo-line bg-kumo-recessed/20 p-3.5">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <h3 className="font-bold text-kumo-strong">连接与验证</h3>
                      <span className="text-[11px] text-kumo-subtle">SSH 登录信息</span>
                    </div>

                  <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-3">
                    <div className="flex flex-col gap-1.5 cq-sm:col-span-2">
                      <label className="font-semibold text-kumo-subtle">连接地址（IP 或域名）</label>
                      <Input size="sm"
                        aria-label="连接地址"
                        type="text"
                        value={serverForm.host}
                        onChange={e => setServerForm(prev => ({ ...prev, host: e.target.value }))}
                        placeholder="12.34.56.78"
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">端口</label>
                      <Input size="sm"
                        aria-label="端口"
                        type="number"
                        value={serverForm.port}
                        onChange={e => setServerForm(prev => ({ ...prev, port: parseInt(e.target.value) || 22 }))}
                        placeholder="22"
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                  </div>

                  <div className="mt-3 flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">凭据预设</label>
                    <Select size="sm"
                      aria-label="选择凭据预设"
                      value={selectedCredentialId}
                      onValueChange={applyCredential}
                      placeholder="手动录入"
                      className="w-full min-w-0 px-3 py-2"
                      items={[
                        { value: '', label: '手动录入' },
                        ...serverCredentials.map(c => ({
                          value: String(c.id),
                          label: `${c.name} (${c.username})`,
                        })),
                      ]}
                    />
                  </div>

                  <div className="mt-3 flex flex-col gap-3 border-t border-kumo-line pt-3">
                    <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2">
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">登录用户名</label>
                        <Input size="sm"
                          aria-label="登录用户名"
                          type="text"
                          value={serverForm.username}
                          onChange={e => setServerForm(prev => ({ ...prev, username: e.target.value }))}
                          placeholder="root"
                          className="px-3 py-2 text-kumo-strong"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">身份验证方案</label>
                        <div className="flex flex-wrap gap-2">
                          <Button size="sm"
                            variant={serverForm.authType === 'password' ? 'primary' : 'secondary'}
                            onClick={() => setServerForm(prev => ({ ...prev, authType: 'password' }))}
                          >
                            密码验证
                          </Button>
                          <Button size="sm"
                            variant={serverForm.authType === 'privateKey' ? 'primary' : 'secondary'}
                            onClick={() => setServerForm(prev => ({ ...prev, authType: 'privateKey' }))}
                          >
                            密钥证书
                          </Button>
                        </div>
                      </div>
                    </div>

                    {serverForm.authType === 'password' ? (
                      <div className="flex flex-col gap-1.5">
                        <label className="font-semibold text-kumo-subtle">连接密码</label>
                        <Input size="sm"
                          aria-label="连接密码"
                          type="text"
                          value={serverForm.password}
                          onChange={e => setServerForm(prev => ({ ...prev, password: e.target.value }))}
                          placeholder={serverModalMode === 'edit' ? '****** (留空不修改)' : '登录密码'}
                          autoComplete="off"
                          data-1p-ignore
                          data-lpignore="true"
                          data-bwignore="true"
                          data-form-type="other"
                          spellCheck={false}
                          className="px-3 py-2 text-kumo-strong"
                        />
                      </div>
                    ) : (
                      <div className="flex flex-col gap-3">
                        <div className="flex flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">私钥证书</label>
                          <CodeEditor
                            label="证书密钥"
                            language="text"
                            value={serverForm.privateKey}
                            onChange={privateKey => setServerForm(prev => ({ ...prev, privateKey }))}
                            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                            minHeight="8rem"
                          />
                        </div>
                        <div className="flex flex-col gap-1.5">
                          <label className="font-semibold text-kumo-subtle">密钥口令（如有）</label>
                          <Input size="sm"
                            aria-label="密钥口令"
                            type="text"
                            value={serverForm.passphrase}
                            onChange={e => setServerForm(prev => ({ ...prev, passphrase: e.target.value }))}
                            placeholder="Key Passphrase"
                            autoComplete="off"
                            data-1p-ignore
                            data-lpignore="true"
                            data-bwignore="true"
                            data-form-type="other"
                            spellCheck={false}
                            className="px-3 py-2 text-kumo-strong"
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="mt-3 flex flex-col gap-1.5 border-t border-kumo-line pt-3">
                    <label className="font-semibold text-kumo-subtle">主机标签（使用英文逗号分隔）</label>
                    <Input size="sm"
                      aria-label="自定义主机标签"
                      type="text"
                      value={serverForm.tagsInput}
                      onChange={e => setServerForm(prev => ({ ...prev, tagsInput: e.target.value }))}
                      placeholder="生产环境,数据库,美国"
                      className="px-3 py-2 text-kumo-strong"
                    />
                  </div>
                  </section>

                </>
              ) : (
                <div className="flex flex-col gap-4">
                  <Input
                    label="主机名称" size="sm"
                    value={quickDeployName}
                    onChange={(e) => setQuickDeployName(e.target.value)}
                    placeholder="prod-agent-01"
                  />

                  <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-[11px] leading-relaxed text-kumo-subtle">
                    Agent 模式会创建或复用主机记录，并生成目标机器上的安装命令。
                  </div>
                  {quickDeployResult && (
                    <div className="flex flex-col gap-3">
                      <Select
                        label="安装目标" size="sm"
                        value={agentInstallOS}
                        onValueChange={setAgentInstallOS}
                        items={[
                          { value: 'linux', label: 'Linux / macOS' },
                          { value: 'win', label: 'Windows PowerShell' },
                        ]}
                      />
                      <ClipboardText
                        size="sm"
                        text={isWindowsAgentInstallOs(agentInstallOS) ? quickDeployResult.winInstallCommand || '' : quickDeployResult.installCommand || ''}
                        className="w-full"
                        tooltip={{ text: '复制命令', copiedText: '安装命令已复制', side: 'top' }}
                        labels={{ copyAction: '复制安装命令' }}
                      />
                      <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2 text-[11px] leading-relaxed text-kumo-subtle">
                        {getAgentInstallExecutionHint(agentInstallOS)}
                      </div>
                      <div className="grid grid-cols-1 gap-2 text-[11px] text-kumo-subtle cq-sm:grid-cols-2">
                        <AppCard padding="none" className="p-2">
                          <div className="font-semibold text-kumo-strong">主机 ID</div>
                          <div className="mt-1 font-mono">{quickDeployResult.serverId}</div>
                        </AppCard>
                        <AppCard padding="none" className="p-2">
                          <div className="font-semibold text-kumo-strong">API 地址</div>
                          <div className="mt-1 truncate font-mono" title={quickDeployResult.apiUrl}>{quickDeployResult.apiUrl}</div>
                        </AppCard>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {serverModalError && (
                <div className="text-xs text-kumo-danger font-bold bg-kumo-danger/10 border border-kumo-danger/20 p-2.5 rounded">
                  {serverModalError}
                </div>
              )}
            </div>

            <div className="flex flex-col-reverse gap-2.5 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:flex-row cq-sm:justify-end">
              {serverModalMode === 'add' && serverAddMode === 'agent' ? (
                <>
                  <Button
                    type="button" size="sm"
                    variant="primary"
                    loading={serverModalSaving}
                    onClick={generateQuickInstallCommand}
                    className="w-full cq-sm:w-auto"
                  >
                    生成 Agent 安装命令
                  </Button>
                </>
              ) : null}
              <Button size="sm"
                variant="secondary"
                onClick={testServerConnection}
                disabled={serverModalSaving}
                className={`w-full px-3.5 py-1.5 text-xs font-semibold cq-sm:w-auto ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                连接测试
              </Button>
              <Button size="sm"
                variant="primary"
                onClick={saveServer}
                disabled={serverModalSaving}
                className={`w-full px-4 py-1.5 text-kumo-inverse text-xs font-bold cq-sm:w-auto ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                {serverModalSaving ? '保存中...' : '保存'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框: 凭据预设新增 ==================== */}
      <Dialog.Root open={showAddCredentialModal} onOpenChange={setShowAddCredentialModal}>
        <Dialog size="sm" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 !w-[min(24rem,calc(100vw-2rem))] !max-w-[min(24rem,calc(100vw-2rem))]">
          <div ref={credentialModalPortalRef} className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
                新增 SSH 验证凭据
              </Dialog.Title>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square" size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-w-0 p-4 flex flex-col gap-4 overflow-y-auto text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">凭据别名</label>
                <Input size="sm"
                  aria-label="凭据别名"
                  type="text"
                  value={credForm.name}
                  onChange={e => setCredForm(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="美国节点通用 root 秘钥"
                  className="px-3 py-2 text-kumo-strong"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">用户登录名</label>
                <Input size="sm"
                  aria-label="用户登录名"
                  type="text"
                  value={credForm.username}
                  onChange={e => setCredForm(prev => ({ ...prev, username: e.target.value }))}
                  placeholder="root"
                  className="px-3 py-2 text-kumo-strong"
                />
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">登录凭据模式</label>
                <Select size="sm"
                  aria-label="登录凭据模式"
                  value={credForm.auth_type}
                  onValueChange={(value) => setCredForm(prev => ({ ...prev, auth_type: String(value) }))}
                  className="w-full min-w-0 px-3 py-2"
                  items={[
                    { value: 'password', label: '明文密码' },
                    { value: 'key', label: '私钥证书 (RSA / OpenSSH)' },
                  ]}
                />
              </div>

              {credForm.auth_type === 'password' ? (
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">默认登录密码</label>
                  <Input size="sm"
                    aria-label="默认登录密码"
                    type="text"
                    value={credForm.password}
                    onChange={e => setCredForm(prev => ({ ...prev, password: e.target.value }))}
                    autoComplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    data-bwignore="true"
                    data-form-type="other"
                    spellCheck={false}
                    className="px-3 py-2 text-kumo-strong"
                  />
                </div>
              ) : (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">PEM 私钥证书内容</label>
                    <CodeEditor
                      label="PEM 私钥证书内容"
                      language="text"
                      value={credForm.private_key}
                      onChange={private_key => setCredForm(prev => ({ ...prev, private_key }))}
                      placeholder="-----BEGIN RSA PRIVATE KEY-----"
                      minHeight="8rem"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">证书保护密码短语 (口令)</label>
                    <Input size="sm"
                      aria-label="证书保护密码短语"
                      type="text"
                      value={credForm.passphrase}
                      onChange={e => setCredForm(prev => ({ ...prev, passphrase: e.target.value }))}
                      placeholder="Passphrase"
                      autoComplete="off"
                      data-1p-ignore
                      data-lpignore="true"
                      data-bwignore="true"
                      data-form-type="other"
                      spellCheck={false}
                      className="px-3 py-2 text-kumo-strong"
                    />
                  </div>
                </div>
              )}
            </div>

            <div className="flex flex-col-reverse gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 text-xs cq-sm:flex-row cq-sm:justify-end">
              <Button size="sm" variant="secondary" onClick={() => setShowAddCredentialModal(false)} className="w-full cq-sm:w-auto">取消</Button>
              <Button size="sm" variant="primary" onClick={addCredential} className="w-full text-kumo-inverse font-bold cq-sm:w-auto">保存</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框: 导入主机备份 ==================== */}
      <Dialog.Root open={showImportServerModal} onOpenChange={setShowImportServerModal}>
        <Dialog size="sm" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 !w-[min(24rem,calc(100vw-2rem))] !max-w-[min(24rem,calc(100vw-2rem))]">
          <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
              导入主机备份配置
            </Dialog.Title>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  aria-label="关闭"
                  className="shrink-0"
                />
              )}
            />
          </div>

          <div className="min-w-0 p-4 flex flex-col gap-4 overflow-y-auto text-xs">
            <div className="flex flex-col gap-1.5">
              <label className="font-semibold text-kumo-subtle font-medium">选择备份 JSON 文件</label>
              <Input size="sm"
                aria-label="选择备份 JSON 文件"
                type="file"
                onChange={e => {
                  const f = e.target.files[0];
                  if (f) processImportFile(f);
                }}
                className="px-3 py-2"
              />
            </div>

            {importPreview && (
              <div className="bg-kumo-success/10 border border-kumo-success/20 p-2.5 rounded text-xs text-kumo-success font-bold">
                ✓ 已识别 {importPreview.length} 台主机，确认恢复？
              </div>
            )}

            {importModalError && (
              <div className="text-xs text-kumo-danger font-bold bg-kumo-danger/10 border border-kumo-danger/20 p-2.5 rounded">
                {importModalError}
              </div>
            )}
          </div>

          <div className="flex flex-col-reverse gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 text-xs cq-sm:flex-row cq-sm:justify-end">
            <Button size="sm" variant="secondary" onClick={() => setShowImportServerModal(false)} className="w-full cq-sm:w-auto">取消</Button>
            <Button size="sm"
              variant="primary"
              onClick={confirmImportServers}
              disabled={importModalSaving || !importPreview}
              className="w-full text-kumo-inverse font-bold disabled:opacity-50 cq-sm:w-auto"
            >
              {importModalSaving ? '恢复中...' : '确认恢复导入'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框: 单机 Agent 部署 ==================== */}
      <Dialog.Root
        open={showAgentModal}
        onOpenChange={(open) => {
          setShowAgentModal(open);
          if (!open) setAgentModalData(null);
        }}
      >
        <Dialog size="xl" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-lg:!w-[min(72rem,calc(100vw-3rem))] cq-lg:!max-w-[min(72rem,calc(100vw-3rem))]">
          <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
              部署 Agent
            </Dialog.Title>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  aria-label="关闭"
                  className="shrink-0"
                />
              )}
            />
          </div>

          <div className="flex-1 overflow-y-auto p-4 text-xs text-kumo-default">
            {agentInstallLoading && !agentModalData?.installCommand ? (
              <div className="space-y-3 py-8">
                <SkeletonLine className="h-4 w-1/3 mx-auto" />
                <SkeletonLine className="h-20 w-full" />
                <SkeletonLine className="h-4 w-2/3 mx-auto" />
              </div>
            ) : agentModalData ? (
              <div className="flex flex-col gap-4">
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">目标主机</div>
                  <div className="mt-1 font-semibold text-kumo-strong">{agentModalData.serverName}</div>
                </div>

                <div className="flex flex-col gap-3">
                  <div className="grid gap-3 cq-md:grid-cols-[minmax(0,1fr)_auto] cq-md:items-center">
                    <div className="font-semibold text-kumo-subtle">安装命令</div>
                    <div className="min-w-0">
                      <Tabs
                        {...TOOL_TABS_PROPS}
                        className="w-fit max-w-full"
                        listClassName="w-fit max-w-full"
                        value={agentInstallOS}
                        onValueChange={setAgentInstallOS}
                        tabs={[
                          { value: 'linux', label: 'Linux' },
                          { value: 'win', label: 'Windows' },
                        ]}
                      />
                    </div>
                  </div>

                  <ClipboardText
                    size="sm"
                    text={getAgentInstallCommand(agentInstallOS)}
                    className="w-full"
                    tooltip={{ text: '复制', copiedText: 'Agent 安装命令已复制', side: 'top' }}
                    labels={{ copyAction: '复制 Agent 安装命令' }}
                  />
                  <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2 text-[11px] leading-relaxed text-kumo-subtle">
                    {getAgentInstallExecutionHint(agentInstallOS)}
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-2">
                  <LinkButton size="sm"
                    variant="secondary"
                    href={`${getAgentBaseApiUrl() || agentModalData.apiUrl}/agent/${isWindowsAgentInstallOs(agentInstallOS) ? 'agent-windows-amd64.exe' : 'agent-linux-amd64'}`}
                    target="_blank"
                    rel="noreferrer"
                    external
                    icon={<Download className="h-3.5 w-3.5" />}
                    className="w-full justify-center"
                  >
                    下载 {isWindowsAgentInstallOs(agentInstallOS) ? 'Windows x64' : 'Linux x64'} Agent
                  </LinkButton>
                </div>

                {agentModalData.agentKey && (
                  <ClipboardText
                    size="sm"
                    text={!isWindowsAgentInstallOs(agentInstallOS)
                      ? `chmod +x agent-linux-amd64 && ./agent-linux-amd64 service install --url ${getAgentBaseApiUrl()} --key ${agentModalData.agentKey}`
                      : `.\\agent-windows-amd64.exe service install --url ${getAgentBaseApiUrl()} --key ${agentModalData.agentKey}`}
                    className="w-full"
                    tooltip={{ text: '复制', copiedText: '手动安装命令已复制', side: 'top' }}
                    labels={{ copyAction: '复制手动安装命令' }}
                  />
                )}

                {agentInstallLog && (
                  <CodeEditor
                    label="安装日志"
                    language="text"
                    value={agentInstallLog}
                    readOnly
                    minHeight="10rem"
                    className={`${agentInstallResult === 'success'
                      ? 'ring-kumo-success/40'
                      : agentInstallResult === 'error'
                        ? 'ring-kumo-danger/40'
                        : agentInstallResult === 'warning'
                          ? 'ring-kumo-warning/40'
                          : ''
                      }`}
                  />
                )}
              </div>
            ) : (
              <div className="py-8 text-center text-xs text-kumo-subtle">未加载 Agent 数据</div>
            )}
          </div>

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button" size="sm"
                variant="secondary-destructive"
                icon={<Trash className="h-3.5 w-3.5" />}
                disabled={!agentModalData || agentInstallLoading || agentInstalling}
                onClick={() => agentModalData && uninstallAgent(agentModalData.serverId)}
              >
                卸载
              </Button>
              <Button
                type="button" size="sm"
                variant="secondary"
                icon={<Key className="h-3.5 w-3.5" />}
                disabled={!agentModalData || agentInstallLoading || agentInstalling}
                onClick={regenerateAgentKey}
              >
                重新生成 Key
              </Button>
            </div>
            <div className="flex flex-col-reverse gap-2 cq-sm:flex-row cq-sm:flex-wrap cq-sm:items-center cq-sm:justify-end cq-sm:gap-3">
              {!isWindowsAgentInstallOs(agentInstallOS) && canSshDeployAgent(serverList.find((server) => server.id === agentModalData?.serverId)) && <Checkbox
                label="Linux SSH 覆盖"
                checked={agentForceSsh}
                disabled={agentInstallLoading || agentInstalling}
                onCheckedChange={(checked) => setAgentForceSsh(Boolean(checked))}
              />}
              <Button
                type="button" size="sm"
                variant="secondary"
                onClick={() => setShowAgentModal(false)}
                className="w-full cq-sm:w-auto"
              >
                关闭
              </Button>
              <Button
                type="button" size="sm"
                variant="primary"
                icon={<Play className="h-3.5 w-3.5" />}
                loading={agentInstalling}
                disabled={!agentModalData || agentInstallLoading}
                onClick={() => agentModalData && autoInstallAgent(agentModalData.serverId)}
                className="w-full cq-sm:w-auto"
              >
                一键安装
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框: 批量 Agent 部署 ==================== */}
      <Dialog.Root
        open={showBatchAgentModal}
        onOpenChange={(open) => {
          if (!open && agentInstallLoading) return;
          setShowBatchAgentModal(open);
        }}
      >
        <Dialog size="xl" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 !w-[min(64rem,calc(100vw-2rem))] !max-w-[min(64rem,calc(100vw-2rem))]">
          <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
              批量部署 Agent
            </Dialog.Title>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  aria-label="关闭"
                  disabled={agentInstallLoading}
                  className="shrink-0"
                />
              )}
            />
          </div>

          <div className="flex-1 overflow-y-auto p-4 text-xs text-kumo-default">
            <div className="flex flex-col gap-4">
              <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3 text-kumo-subtle">
                已选择 {selectedBatchServers.length} / {serverList.length} 台主机。
              </div>

              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="font-semibold text-kumo-strong">目标主机</div>
                <div className="flex items-center gap-2">
                  <Button type="button" size="sm" variant="secondary" onClick={selectAllBatchServers} disabled={agentInstallLoading}>
                    全选
                  </Button>
                  <Button type="button" size="sm" variant="secondary" onClick={() => setSelectedBatchServers([])} disabled={agentInstallLoading}>
                    清空
                  </Button>
                </div>
              </div>

              <div className="grid grid-cols-1 gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3 cq-sm:grid-cols-2 cq-md:grid-cols-3">
                {serverList.map(server => {
                  const deployable = canSshDeployAgent(server);
                  return (
                    <AppCard key={server.id} padding="none" className="p-2">
                      <Checkbox
                        label={
                          <span className="inline-flex min-w-0 items-center gap-1.5">
                            {getFlagCountry(server) && (
                              <CountryFlag countryCode={getFlagCountry(server)} className="h-3 w-4 text-xs" />
                            )}
                            <span className="truncate font-semibold text-kumo-strong">{server.name}</span>
                            {!deployable && <span className="shrink-0 text-[10px] font-medium text-kumo-subtle">无 SSH</span>}
                          </span>
                        }
                        checked={selectedBatchServers.includes(server.id)}
                        disabled={agentInstallLoading || !deployable}
                        onCheckedChange={(checked) => {
                          setSelectedBatchServers(prev => {
                            if (checked) return prev.includes(server.id) ? prev : [...prev, server.id];
                            return prev.filter(id => id !== server.id);
                          });
                        }}
                      />
                    </AppCard>
                  );
                })}
              </div>

              {batchInstallResults.length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="font-semibold text-kumo-strong">部署进度</div>
                  <div className="overflow-hidden rounded-md border border-kumo-line">
                    {batchInstallResults.map(result => (
                      <div key={result.serverId} className="flex items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-3 py-2 last:border-b-0">
                        <div className="min-w-0">
                          <div className="truncate font-semibold text-kumo-strong">{result.serverName}</div>
                          {result.error && <div className="truncate text-[11px] text-kumo-danger" title={result.error}>{result.error}</div>}
                        </div>
                        <span className={`shrink-0 rounded px-2 py-0.5 text-[11px] font-semibold ${result.status === 'success'
                          ? 'bg-kumo-success/10 text-kumo-success'
                          : result.status === 'failed'
                            ? 'bg-kumo-danger/10 text-kumo-danger'
                            : result.status === 'verifying'
                              ? 'bg-kumo-warning/10 text-kumo-warning'
                              : result.status === 'processing'
                                ? 'bg-brand/10 text-brand'
                                : 'bg-kumo-recessed text-kumo-subtle'
                          }`}>
                          {result.status === 'waiting'
                            ? '等待'
                            : result.status === 'processing'
                              ? '处理中'
                              : result.status === 'verifying'
                                ? '验证中'
                                : result.status === 'success'
                                  ? '成功'
                                  : '失败'}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
            <div className="flex flex-wrap items-center gap-3 text-xs text-kumo-subtle">
              <Checkbox
                label="强制 SSH 覆盖"
                checked={batchAgentForceSsh}
                disabled={agentInstallLoading}
                onCheckedChange={(checked) => setBatchAgentForceSsh(Boolean(checked))}
              />
              {agentInstallLoading && <span>任务执行中</span>}
            </div>
            <div className="flex flex-col-reverse gap-2 cq-sm:flex-row cq-sm:justify-end">
              <Button
                type="button" size="sm"
                variant="secondary"
                disabled={agentInstallLoading}
                onClick={() => setShowBatchAgentModal(false)}
                className="w-full cq-sm:w-auto"
              >
                关闭
              </Button>
              <Button
                type="button" size="sm"
                variant="primary"
                icon={<Play className="h-3.5 w-3.5" />}
                loading={agentInstallLoading}
                disabled={selectedBatchServers.length === 0}
                onClick={runBatchAgentInstall}
                className="w-full cq-sm:w-auto"
              >
                开始部署
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框: Agent 一键升级 ==================== */}
      <Dialog.Root
        open={showUpgradeModal}
        onOpenChange={(open) => {
          setShowUpgradeModal(open);
        }}
      >
        <Dialog size="xl" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 !w-[min(64rem,calc(100vw-2rem))] !max-w-[min(64rem,calc(100vw-2rem))]">
          <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
              升级 Agent
            </Dialog.Title>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  aria-label="关闭"
                  className="shrink-0"
                />
              )}
            />
          </div>

          <div className="flex-1 overflow-y-auto p-4 text-xs text-kumo-default">
            <div className="flex flex-col gap-4">
              <div className="grid grid-cols-1 gap-2 cq-sm:grid-cols-2">
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">目标 Agent</div>
                  <div className="mt-1 text-lg font-bold text-kumo-strong">{upgradeBatchSnapshot?.items?.length || getAgentUpgradeTargets().length}</div>
                </div>
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">状态</div>
                  <div className="mt-1 font-semibold text-kumo-strong">
                    {upgrading
                      ? getUpgradeBatchStatusLabel(upgradeBatchSnapshot?.status || 'running')
                      : upgradeBatchSnapshot
                        ? getUpgradeBatchStatusLabel(upgradeBatchSnapshot.status)
                        : upgradeProgress >= 100
                          ? '已完成'
                          : '待执行'}
                  </div>
                </div>
              </div>

              {upgradeProgress > 0 && (
                <Meter
                  label="升级进度"
                  value={upgradeProgress}
                  min={0}
                  max={100}
                  customValue={`${upgradeProgress}%`}
                />
              )}

              <div className="grid min-w-0 gap-4 cq-md:grid-cols-[minmax(18rem,0.85fr)_minmax(0,1.15fr)] cq-md:items-stretch">
                <div className="min-w-0 space-y-1.5">
                  <div className="text-xs font-semibold text-kumo-strong">主机状态</div>
                  {upgradeBatchSnapshot?.items?.length > 0 ? (
                    <div className="max-h-[24rem] overflow-auto rounded-md border border-kumo-line scrollbar-thin">
                      {upgradeBatchSnapshot.items.map(item => (
                        <div key={item.serverId} className="flex min-w-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-3 py-2 last:border-b-0">
                          <div className="min-w-0">
                            <div className="truncate text-xs font-semibold text-kumo-strong">{item.serverName || item.serverId}</div>
                            {(item.error || item.log?.at?.(-1)) && (
                              <div className={`mt-0.5 truncate text-[11px] ${item.error ? 'text-kumo-danger' : 'text-kumo-subtle'}`} title={item.error || item.log.at(-1)}>
                                {item.error || item.log.at(-1)}
                              </div>
                            )}
                          </div>
                          <Badge
                            variant={item.status === 'succeeded'
                              ? 'success'
                              : item.status === 'failed'
                                ? 'danger'
                                : item.status === 'verifying'
                                  ? 'warning'
                                  : 'info'}
                          >
                            {getUpgradeItemStatusLabel(item.status)}
                          </Badge>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <AppCard padding="none" className="min-h-56 p-3 text-kumo-subtle">等待开始升级。</AppCard>
                  )}
                </div>

                <div className="min-w-0 space-y-1.5">
                  <div className="text-xs font-semibold text-kumo-strong">升级日志</div>
                  {upgradeLog ? (
                    <pre
                      ref={upgradeLogViewportRef}
                      className="min-h-56 max-h-[24rem] overflow-auto whitespace-pre-wrap break-words rounded-md border border-kumo-line bg-kumo-recessed/25 p-3 font-mono text-[11px] leading-5 text-kumo-strong scrollbar-thin"
                    >
                      {upgradeLog}
                    </pre>
                  ) : (
                    <AppCard padding="none" className="min-h-56 p-3 text-kumo-subtle">将对在线 Agent 下发后台自升级任务。</AppCard>
                  )}
                </div>
              </div>
            </div>
          </div>

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
            <div className="flex flex-wrap items-center gap-3">
              <Checkbox
                label="Linux SSH 保底"
                checked={upgradeFallbackSsh}
                disabled={upgrading}
                onCheckedChange={(checked) => setUpgradeFallbackSsh(Boolean(checked))}
              />
            </div>
            <div className="flex flex-col-reverse gap-2 cq-sm:flex-row cq-sm:justify-end">
              <Button
                type="button" size="sm"
                variant="secondary"
                onClick={() => setShowUpgradeModal(false)}
                className="w-full cq-sm:w-auto"
              >
                {upgrading ? '后台运行' : '关闭'}
              </Button>
              <Button
                type="button" size="sm"
                variant="primary"
                icon={<Upload className="h-3.5 w-3.5" />}
                loading={upgrading}
                disabled={upgrading || getAgentUpgradeTargets().length === 0}
                onClick={performOneKeyUpgrade}
                className="w-full cq-sm:w-auto"
              >
                开始升级
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={Boolean(dockerComposeEditor)}
        onOpenChange={(open) => {
          if (!open) requestCloseDockerComposeEditor();
        }}
      >
        <Dialog size="xl" className="@container flex h-[min(78dvh,760px)] max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:min-w-[56rem] cq-sm:max-w-[calc(100vw-3rem)]">
          <div className="flex min-w-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/35 px-4 py-3">
            <Dialog.Title className="flex min-w-0 items-center gap-2 truncate text-sm font-bold text-kumo-strong">
              <FolderOpen className="h-4 w-4 shrink-0 text-brand" />
              <span className="truncate">{dockerComposeEditor?.mode === 'edit' ? '修改 Compose 配置' : '查看 Compose 配置'}</span>
            </Dialog.Title>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  className="shrink-0"
                />
              )}
            />
          </div>

          <div className="grid min-h-0 flex-1 gap-3 overflow-hidden p-4 text-xs cq-lg:grid-cols-[18rem_minmax(0,1fr)]">
            <LayerCard className="flex min-h-0 flex-col overflow-hidden p-0">
              <LayerCard.Secondary className="flex min-h-[52px] items-center justify-between gap-2 px-3 py-3.5">
                <span className="inline-flex min-w-0 items-center gap-2 text-xs font-bold text-kumo-strong">
                  <Settings className="h-4 w-4 shrink-0 text-brand" />
                  项目信息
                </span>
                <Badge variant={dockerComposeEditor?.status?.includes('运行') ? 'success' : 'neutral'} appearance="dot">
                  {dockerComposeEditor?.status || '-'}
                </Badge>
              </LayerCard.Secondary>
              <LayerCard.Primary className="flex flex-1 flex-col space-y-2 p-3">
                <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                  <div className="text-[10px] text-kumo-subtle">主机</div>
                  <div className="mt-0.5 truncate text-sm font-bold text-kumo-strong">{dockerComposeEditor?.serverName || '-'}</div>
                </div>
                <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                  <div className="text-[10px] text-kumo-subtle">项目</div>
                  <div className="mt-0.5 truncate text-sm font-bold text-kumo-strong">{dockerComposeEditor?.projectName || '-'}</div>
                </div>
                <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                  <div className="text-[10px] text-kumo-subtle">工作目录</div>
                  <div className="mt-0.5 truncate font-mono text-[11px] text-kumo-strong" title={dockerComposeEditor?.workingDir}>{dockerComposeEditor?.workingDir || '-'}</div>
                </div>
                <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2 py-1.5">
                  <div className="text-[10px] text-kumo-subtle">配置文件</div>
                  <div className="mt-0.5 flex flex-col gap-1">
                    {(dockerComposeEditor?.configFiles?.length ? dockerComposeEditor.configFiles : ['-']).map(path => (
                      <span key={path} className="truncate font-mono text-[11px] text-kumo-strong" title={path}>{path}</span>
                    ))}
                  </div>
                </div>
                {dockerComposeEditor?.mode === 'view' && dockerComposeEditor?.path && (
                  <Button
                    size="sm"
                    variant="primary"
                    icon={<Edit className="h-3.5 w-3.5" />}
                    onClick={() => setDockerComposeEditor(prev => prev ? { ...prev, mode: 'edit' } : prev)}
                    className="w-full justify-center"
                  >
                    修改配置
                  </Button>
                )}
              </LayerCard.Primary>
            </LayerCard>

            <div className="flex min-h-0 min-w-0 flex-col gap-2">
              {dockerComposeEditor?.error && (
                <Badge variant="error">{dockerComposeEditor.error}</Badge>
              )}
              <CodeEditor
                label="Compose 配置内容"
                language="yaml"
                value={dockerComposeEditor?.loading ? '正在读取 Compose 配置...' : dockerComposeEditor?.content || ''}
                onChange={content => setDockerComposeEditor(prev => prev ? { ...prev, content } : prev)}
                readOnly={dockerComposeEditor?.mode !== 'edit' || dockerComposeEditor?.loading}
                className="min-h-0 flex-1"
                minHeight="0"
              />
            </div>
          </div>

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
            <div className="min-w-0 truncate text-[11px] text-kumo-subtle">
              {dockerComposeEditor?.path || '未选择配置文件'}
            </div>
            <div className="flex flex-col-reverse gap-2 cq-sm:flex-row cq-sm:justify-end">
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={requestCloseDockerComposeEditor}
                className="w-full cq-sm:w-auto"
              >
                关闭
              </Button>
              {dockerComposeEditor?.mode === 'edit' && (
                <Button
                  type="button"
                  size="sm"
                  variant="primary"
                  icon={<Save className="h-3.5 w-3.5" />}
                  loading={dockerComposeEditor?.saving}
                  disabled={dockerComposeEditor?.loading || !dockerComposeEditor?.path}
                  onClick={saveDockerComposeConfig}
                  className="w-full cq-sm:w-auto"
                >
                  保存
                </Button>
              )}
              {dockerComposeEditor?.mode === 'view' && dockerComposeEditor?.saved && (
                <Button
                  type="button"
                  size="sm"
                  variant="primary"
                  icon={<Upload className="h-3.5 w-3.5" />}
                  loading={dockerComposeEditor?.updating}
                  disabled={dockerComposeEditor?.updating || !dockerComposeEditor?.path}
                  onClick={updateDockerComposeDeployment}
                  className="w-full cq-sm:w-auto"
                >
                  更新编排
                </Button>
              )}
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root
        open={dockerLogsModalOpen}
        onOpenChange={(open) => {
          setDockerLogsModalOpen(open);
          if (!open) {
            setDockerLogsContent('');
            setDockerLogsLoading(false);
          }
        }}
      >
        <Dialog size="lg" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:min-w-[48rem] cq-sm:max-w-[calc(100vw-3rem)]">
          <div className="flex min-w-0 items-center justify-between gap-3 bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong flex items-center gap-2">
              <FileText className="h-4 w-4 text-brand" />
              <span>容器日志: {dockerLogsContainer ? getDockerContainerName(dockerLogsContainer) : ''}</span>
            </Dialog.Title>
            <Dialog.Close
              aria-label="关闭"
              render={(props) => (
                <Button
                  {...props}
                  type="button"
                  variant="secondary"
                  shape="square" size="sm"
                  icon={<X className="h-3.5 w-3.5" />}
                  className="shrink-0"
                />
              )}
            />
          </div>

          <div className="flex-1 overflow-y-auto p-4 text-xs font-mono bg-kumo-recessed text-kumo-default flex flex-col gap-3 min-h-96">
            <div className="flex-1 rounded border border-kumo-line bg-kumo-canvas/15 p-2 overflow-auto max-h-[50vh] whitespace-pre-wrap select-text font-mono text-[11px] leading-relaxed">
              {dockerLogsContent}
            </div>
          </div>

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
            <div className="flex items-center gap-3">
              <Select
                size="sm"
                label="日志行数"
                value={String(dockerLogsTail)}
                onValueChange={(value) => {
                  const val = Number(value);
                  setDockerLogsTail(val);
                  loadDockerContainerLogs(dockerLogsServer, dockerLogsContainer, val);
                }}
                disabled={dockerLogsLoading}
                items={DOCKER_LOG_TAIL_ITEMS}
              />
              {dockerLogsLoading && (
                <span className="inline-flex items-center gap-1 text-[11px] text-kumo-subtle">
                  <RefreshCw className="h-3 w-3 animate-spin" />
                  更新中...
                </span>
              )}
            </div>
            <div className="flex flex-col-reverse gap-2 cq-sm:flex-row cq-sm:justify-end">
              <Button
                type="button" size="sm"
                variant="secondary"
                onClick={() => {
                  loadDockerContainerLogs(dockerLogsServer, dockerLogsContainer, dockerLogsTail);
                }}
                disabled={dockerLogsLoading}
                icon={<RefreshCw className={`h-3.5 w-3.5 ${dockerLogsLoading ? 'animate-spin' : ''}`} />}
                className="w-full cq-sm:w-auto"
              >
                刷新
              </Button>
              <Button
                type="button" size="sm"
                variant="primary"
                onClick={() => {
                  setDockerLogsModalOpen(false);
                }}
                className="w-full cq-sm:w-auto"
              >
                关闭
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
      <Dialog.Root open={showNetworkTargetModal} onOpenChange={setShowNetworkTargetModal}>
        <Dialog size="sm" className="@container flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 !w-[min(24rem,calc(100vw-2rem))] !max-w-[min(24rem,calc(100vw-2rem))]">
          <form onSubmit={saveNetworkTarget} className="flex flex-1 flex-col overflow-hidden">
            <div className="flex shrink-0 items-center justify-between border-b border-kumo-line/80 px-4 py-3">
              <Dialog.Title className="min-w-0 truncate text-sm font-bold text-kumo-strong">
                {networkTargetModalMode === 'add' ? '添加拨测目标' : '编辑拨测目标'}
              </Dialog.Title>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square" size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    className="shrink-0"
                  />
                )}
              />
            </div>
            <div className="flex-1 overflow-auto p-4 flex flex-col gap-3">
              <Input
                size="sm"
                label="目标名称"
                required
                value={networkTargetForm.name}
                onChange={e => setNetworkTargetForm(prev => ({ ...prev, name: e.target.value }))}
                placeholder="例如: 电信"
              />
              <Input
                size="sm"
                label="节点地址 (IP / Host)"
                required
                value={networkTargetForm.host}
                onChange={e => setNetworkTargetForm(prev => ({ ...prev, host: e.target.value }))}
                placeholder="如：hb-ct-v4.ip.zstaticcdn.com"
              />
              <div className="grid gap-3 cq-sm:grid-cols-[minmax(0,1fr)_9rem]">
                <Input
                  size="sm"
                  label="端口"
                  type="number"
                  required
                  value={networkTargetForm.port}
                  onChange={e => setNetworkTargetForm(prev => ({ ...prev, port: parseInt(e.target.value) || 0 }))}
                />
                <Select
                  size="sm"
                  label="协议"
                  value={networkTargetForm.type}
                  onValueChange={(value) => setNetworkTargetForm(prev => ({ ...prev, type: String(value) }))}
                  className="w-full min-w-0"
                  items={[
                    { label: 'TCP', value: 'tcp' },
                    { label: 'UDP', value: 'udp' }
                  ]}
                />
              </div>
              <Input
                size="sm"
                label="排序权重"
                type="number"
                value={networkTargetForm.order_index}
                onChange={e => setNetworkTargetForm(prev => ({ ...prev, order_index: parseInt(e.target.value) || 0 }))}
              />
              <div className="flex items-center gap-2 mt-2">
                <Checkbox
                  checked={networkTargetForm.enabled}
                  onChange={() => setNetworkTargetForm(prev => ({ ...prev, enabled: !prev.enabled }))}
                  id="target-enabled-checkbox"
                />
                <label htmlFor="target-enabled-checkbox" className="text-xs font-semibold text-kumo-strong select-none cursor-pointer">
                  是否启用该探测目标
                </label>
              </div>
            </div>
            <div className="flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line/80 px-4 py-3 bg-kumo-canvas/50">
              <Button type="button" size="sm" variant="secondary" onClick={() => setShowNetworkTargetModal(false)}>
                取消
              </Button>
              <Button size="sm" variant="primary" type="submit">
                保存
              </Button>
            </div>
          </form>
        </Dialog>
      </Dialog.Root>

    </div>
  );
}

export default ServerPage;
