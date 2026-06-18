import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import useStore from '../store.js';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { ChartLegend, ChartPalette, ClipboardText, LayerCard, Meter, Tabs, TimeseriesChart } from '@cloudflare/kumo';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AnimatedCollapse, DeferredRender } from '../components/AnimatedCollapse.jsx';
import CountryFlag from '../components/CountryFlag.jsx';
import QuickCommandBar from '../components/server/QuickCommandBar.jsx';
import SftpPanel from '../components/server/SftpPanel.jsx';
import {
  ChartBoundaryBox,
  ChartWarmupSkeleton,
  ScrollableTable,
} from '../components/ui/AppPrimitives.jsx';
import useTableResize from '../composables/useTableResize.js';
import { formatUptime, formatFileSize, formatDateTime, maskAddress, parseSpeed } from '../modules/utils.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { canOpenTerminal, hasSshEndpoint, isAgentServer, resolveTerminalProtocol } from '../modules/serverTerminal.js';
import { formatDockerContainerPorts } from '../modules/docker-format.js';
import {
  buildAgentInstallCommand,
  buildAgentInstallEndpoint,
  getAgentInstallExecutionHint,
  isWindowsAgentInstallOs,
} from '../modules/agentInstall.js';
import {
  areRealtimeValuesEqual,
  mergePolledServerAccount,
  mergeRealtimeDiskInfo,
  resolveRealtimeMetricsCache,
  reuseRealtimeValueIfEqual,
} from '../modules/serverRealtime.js';
import {
  SERVER_CHART_HISTORY_LIMIT,
  SERVER_CHART_HISTORY_WINDOW_MS,
  SERVER_REALTIME_SAMPLE_INTERVAL_MS,
  formatSqliteUTCDateTime,
  normalizeChartMetricRecords,
  normalizeMetricRecords,
  toTimestamp,
} from '../modules/serverChartMetrics.js';
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
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import io from 'socket.io-client';
import {
  Server,
  Terminal as TerminalIcon,
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
  HardDrive,
  Database,
  TrendingUp,
  Settings,
  Plus,
  Trash,
  Play,
  Pause,
  Key,
  Folder,
  FileText,
  Save,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  Reboot,
  ChevronDown,
  ChevronUp,
  Star
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

const SERVER_LIST_VIEW_STORAGE_KEY = 'server_list_view_mode';
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
  { id: 'speed', label: '【下行】网速【上行】' },
  { id: 'traffic', label: '【下载】 流量 【上传】' },
  { id: 'cpu', label: 'CPU' },
  { id: 'memory', label: '内存' },
  { id: 'disk', label: '硬盘' },
  { id: 'remaining', label: '剩余' },
  { id: 'actions', label: '操作', required: true },
];
const HOST_COMPACT_COLUMN_IDS = HOST_COMPACT_COLUMNS.map(column => column.id);
const HOST_COMPACT_COLUMN_WIDTHS = {
  status: 74,
  name: 112,
  country: 80,
  uptime: 80,
  load: 80,
  speed: 236,
  traffic: 236,
  cpu: 112,
  memory: 112,
  disk: 112,
  remaining: 112,
  actions: 76,
};
const HOST_COMPACT_ADAPTIVE_COLUMNS = new Set(['cpu', 'memory', 'disk', 'remaining']);
const HOST_COMPACT_HEADER_BOX_CLASS = {
  status: 'w-[58px] justify-center',
  name: 'w-[96px] justify-center',
  country: 'w-[64px] justify-center',
  uptime: 'w-[64px] justify-center',
  load: 'w-[64px] justify-center',
  speed: 'w-[216px] justify-center',
  traffic: 'w-[216px] justify-center',
  cpu: 'w-full min-w-[96px] justify-center',
  memory: 'w-full min-w-[96px] justify-center',
  disk: 'w-full min-w-[96px] justify-center',
  remaining: 'w-full min-w-[96px] justify-center',
  actions: 'w-[64px] justify-center',
};
const COMPACT_INLINE_BOX_CLASS = 'border border-kumo-interact/70 shadow-none';
const COMPACT_INLINE_SUBBOX_CLASS = 'border border-kumo-interact/70 shadow-none';
const COMPACT_STICKY_ACTION_CLASS = 'border-l border-kumo-interact/60 before:!w-1 before:!-left-1';
const COMPACT_ACTION_BUTTON_CLASS = '!shadow-none';
const SERVER_SECONDARY_BAR_CLASS = 'flex flex-col gap-2 rounded-md border border-kumo-line/90 bg-kumo-base px-2 py-1.5 lg:flex-row lg:items-center lg:justify-between';
const SERVER_SECONDARY_TABS_GROUP_CLASS = 'flex min-w-0 flex-nowrap items-center gap-1.5 overflow-x-auto whitespace-nowrap p-0.5 scrollbar-thin sm:gap-2';
const HOST_FILTER_TABS_CLASS = 'w-fit max-w-full !ring !ring-inset !ring-kumo-interact/50';
const HOST_FILTER_TABS_LIST_CLASS = 'w-fit max-w-full';
const HOST_FILTER_TABS_INDICATOR_CLASS = '!shadow-none !ring-0 border border-kumo-interact/60';
const HOST_TOOLBAR_BUTTON_CLASS = '!h-6.5 !rounded-md !shadow-none';
const HOST_TOOLBAR_SELECT_CLASS = '!h-6.5 !app-card app-card-md px-2.5 py-1 text-xs focus:outline-none';
const MANAGEMENT_CARD_CLASS = 'self-start overflow-hidden p-0 shadow-none';
const MANAGEMENT_CARD_HEADER_CLASS = 'flex h-9 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/20 px-3';
const MANAGEMENT_CARD_TITLE_CLASS = 'flex min-w-0 items-center gap-2 text-sm font-bold text-kumo-strong';
const MANAGEMENT_CARD_ICON_CLASS = 'h-3.5 w-3.5 shrink-0 text-kumo-brand';
const MANAGEMENT_CARD_BODY_CLASS = 'p-3';
const SERVER_MODULE_TAB_ICON_CLASS = 'h-3.5 w-3.5 shrink-0';
const COMPACT_EXPAND_EXIT_MS = 230;
const SERVER_CHART_SERIES_DEFER_MS = 44;
const SERVER_CHART_RENDER_DEFER_MS = 88;
const SERVER_CHART_ANIMATION_MS = 90;
const SERVER_CHART_UPDATE_ANIMATION_MS = 70;
const SERVER_FAST_CHART_UPDATE_BEHAVIOR = { lazyUpdate: false };
const SERVER_NETWORK_QUALITY_REFRESH_MS = 60 * 1000;
const SERVER_NETWORK_QUALITY_CHART_UPDATE_BEHAVIOR = { lazyUpdate: true };
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
      <span className="hidden sm:inline">{children}</span>
      <span className="sm:hidden">{short || children}</span>
      {badge !== null && (
        <span className="inline-flex h-4 min-w-4 items-center justify-center rounded bg-kumo-brand/10 px-1 text-[10px] font-bold leading-none text-kumo-brand">
          {badge}
        </span>
      )}
    </span>
  );
}

const getInitialServerListViewMode = () => {
  if (typeof window === 'undefined') return 'cards';
  return window.localStorage.getItem(SERVER_LIST_VIEW_STORAGE_KEY) === 'compact' ? 'compact' : 'cards';
};

const getInitialCompactVisibleColumns = () => {
  if (typeof window === 'undefined') return HOST_COMPACT_COLUMN_IDS;
  try {
    const saved = JSON.parse(window.localStorage.getItem(SERVER_COMPACT_COLUMNS_STORAGE_KEY) || '[]');
    const valid = Array.isArray(saved) ? saved.filter(id => HOST_COMPACT_COLUMN_IDS.includes(id)) : [];
    const required = HOST_COMPACT_COLUMNS.filter(column => column.required).map(column => column.id);
    return Array.from(new Set([...required, ...(valid.length > 0 ? valid : HOST_COMPACT_COLUMN_IDS)]));
  } catch (error) {
    return HOST_COMPACT_COLUMN_IDS;
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
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2 py-1 sm:w-14 sm:border-0 sm:bg-transparent sm:px-0 sm:py-0">
      <div className="flex min-w-0 items-center justify-between gap-1">
        <span className="truncate">{label}</span>
        <span className={`shrink-0 font-bold ${color ? '' : valueClassName}`} style={color ? { color } : undefined}>{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className={`h-full ${color ? '' : barClassName}`} style={{ width, backgroundColor: color || undefined }}></div>
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

function DenseUsageMeterComponent({ label, value, detail, indicatorClassName = '!bg-none !bg-kumo-brand' }) {
  const percent = clampPercent(toNumber(value, 0));
  return (
    <div className={`flex h-8 min-w-[96px] w-full items-center rounded-md bg-kumo-recessed/45 px-1.5 ${COMPACT_INLINE_BOX_CLASS}`}>
      <Meter
        label={label}
        value={percent}
        min={0}
        max={100}
        customValue={detail || `${Math.round(percent)}%`}
        className="gap-0.5 text-[10px] leading-none text-kumo-default"
        trackClassName="!h-1.5 overflow-hidden rounded-full border border-kumo-interact/60 bg-kumo-base"
        indicatorClassName={`!h-full rounded-full ${indicatorClassName}`}
      />
    </div>
  );
}

const DenseUsageMeter = React.memo(DenseUsageMeterComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.detail === next.detail
  && prev.indicatorClassName === next.indicatorClassName
));

function DenseLifecycleMeterComponent({ lifecycle }) {
  return (
    <DenseUsageMeter
      label="剩余"
      value={lifecycle.remainingPercent}
      detail={lifecycle.label}
      indicatorClassName={lifecycle.indicatorClassName}
    />
  );
}

const DenseLifecycleMeter = React.memo(DenseLifecycleMeterComponent, (prev, next) => (
  prev.lifecycle?.remainingPercent === next.lifecycle?.remainingPercent
  && prev.lifecycle?.label === next.lifecycle?.label
  && prev.lifecycle?.indicatorClassName === next.lifecycle?.indicatorClassName
));

const getFlowUnitClassName = (unit) => {
  const normalized = String(unit || 'B').toUpperCase();
  if (normalized === 'K') return 'border-kumo-info/65 bg-kumo-info/25 text-kumo-info';
  if (normalized === 'M') return 'border-kumo-success/65 bg-kumo-success/25 text-kumo-success';
  if (normalized === 'G') return 'border-kumo-warning/65 bg-kumo-warning/25 text-kumo-warning';
  if (normalized === 'T') return 'border-kumo-brand/65 bg-kumo-brand/20 text-kumo-brand';
  return 'border-kumo-interact/70 bg-kumo-recessed/70 text-kumo-default';
};

function FlowUnitBadge({ unit }) {
  return (
    <span className={`inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] px-1 font-mono text-[11px] font-bold leading-none ${getFlowUnitClassName(unit)} ${COMPACT_INLINE_SUBBOX_CLASS}`}>
      {unit || 'B'}
    </span>
  );
}

function FlowArrow({ children }) {
  return (
    <span className={`inline-flex h-5 w-5 items-center justify-center rounded-[5px] bg-kumo-recessed/70 text-[13px] font-bold leading-none text-kumo-default ${COMPACT_INLINE_SUBBOX_CLASS}`}>
      {children}
    </span>
  );
}

const formatDenseFlowValue = (value) => {
  const numericValue = Number.parseFloat(String(value ?? '0').replace(/,/g, ''));
  return Number.isFinite(numericValue) ? numericValue.toFixed(1) : '0.0';
};

function DenseTrafficCell({ left, leftUnit, right, rightUnit, leftTitle, rightTitle }) {
  const leftValue = formatDenseFlowValue(left);
  const rightValue = formatDenseFlowValue(right);
  return (
    <div className={`flex h-8 w-[216px] shrink-0 items-center justify-center gap-1 overflow-hidden rounded-md bg-kumo-recessed/35 px-2 font-mono text-[13px] font-bold leading-none text-kumo-strong tabular-nums ${COMPACT_INLINE_BOX_CLASS}`}>
      <span className="min-w-0 flex-1 truncate text-right" title={leftTitle || `${left}${leftUnit}`}>{leftValue}</span>
      <FlowUnitBadge unit={leftUnit} />
      <FlowArrow>&darr;</FlowArrow>
      <span aria-hidden="true" className="-my-px mx-0.5 w-px self-stretch shrink-0 bg-kumo-interact/80"></span>
      <FlowArrow>&uarr;</FlowArrow>
      <FlowUnitBadge unit={rightUnit} />
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
  brand: 'bg-kumo-brand',
  success: 'bg-kumo-success',
  warning: 'bg-kumo-warning',
  info: 'bg-kumo-info',
  danger: 'bg-kumo-danger',
};

const EXPANDED_VALUE_TONES = {
  default: 'text-kumo-strong',
  brand: 'text-kumo-brand',
  success: 'text-kumo-success',
  warning: 'text-kumo-warning',
  info: 'text-kumo-info',
  danger: 'text-kumo-danger',
};

function ExpandedSection({ title, tone = 'brand', action, className = '', children }) {
  return (
    <section className={`min-w-0 overflow-hidden app-card p-1.5 ${className}`}>
      <div className="mb-2 flex min-w-0 items-center justify-between gap-2">
        <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-bold text-kumo-strong">
          <span className={`h-3 w-1 shrink-0 rounded-full ${EXPANDED_SECTION_ACCENTS[tone] || EXPANDED_SECTION_ACCENTS.brand}`}></span>
          <span className="truncate">{title}</span>
        </h4>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {children}
    </section>
  );
}

function ExpandedProgressMetricComponent({
  label,
  value,
  detail,
  caption,
  indicatorClassName = '!bg-none !bg-kumo-brand',
  valueClassName = 'text-kumo-strong',
}) {
  const percent = clampPercent(toNumber(value, 0));
  const displayValue = detail || `${Math.round(percent)}%`;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2.5 py-2">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <span className="text-[11px] font-medium text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-sm font-bold tabular-nums ${valueClassName}`} title={String(displayValue)}>{displayValue}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-base">
        <div
          className={`h-full rounded-full transition-[width] duration-300 ${indicatorClassName}`}
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
));

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
      return 'sm:col-span-2 xl:col-span-3';
    case 'medium':
      return 'sm:col-span-2';
    default:
      return '';
  }
};

function ExpandedStatTileComponent({ label, value, caption, tone = 'default', className = '' }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  return (
    <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-2 ${className}`}>
      <div className="text-[10px] font-medium text-kumo-subtle">{label}</div>
      <div className={`mt-1 truncate text-sm font-bold tabular-nums ${EXPANDED_VALUE_TONES[tone] || EXPANDED_VALUE_TONES.default}`} title={String(displayValue)}>
        {displayValue}
      </div>
      {caption && (
        <div className="mt-1 truncate text-[10px] font-medium text-kumo-subtle" title={String(caption)}>
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
));

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
    <ChartBoundaryBox className={`min-w-0 overflow-hidden app-card ${compact ? 'app-card-md' : ''} p-1.5 h-full ${className}`}>
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
  `grid min-w-0 grid-cols-1 ${dense ? 'gap-1.5' : 'gap-2'} lg:grid-cols-2`
);

const getExpandedTrendGridClassName = (compact = false, dense = false) => (
  `grid min-w-0 grid-cols-1 ${compact || dense ? 'gap-1.5' : 'gap-2'} lg:grid-cols-2`
);

const getExpandedCardSpanClassName = (index, total) => (
  index === total - 1 && total % 2 === 1 ? 'h-full lg:col-span-2' : 'h-full'
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
    <ChartBoundaryBox className={`min-w-0 overflow-hidden app-card p-1.5 ${className}`}>
      {(tooltipBoundary) => (
        <div className="flex min-w-0 flex-col gap-2">
          <div className={`flex min-w-0 flex-wrap items-center justify-between gap-2 ${compact ? 'min-h-2' : ''}`}>
            <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-bold text-kumo-strong">
              <span className="h-3 w-1 shrink-0 rounded-full bg-kumo-brand"></span>
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
                <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                  {summary.map(item => {
                    const tone = getNetworkQualityTone(item);
                    const latestValue = item.latest?.success
                      ? formatLatencyValue(item.latest.latencyMs)
                      : '失败';
                    return (
                      <ExpandedStatTile
                        key={item.name}
                        label={item.name}
                        value={latestValue}
                        caption={`抖动 ${formatLatencyValue(item.jitterMs)} · 丢包 ${toNumber(item.lossRate, 0).toFixed(1)}%`}
                        tone={tone}
                        className={getNetworkQualityToneClass(tone)}
                      />
                    );
                  })}
                </div>
              )}

              {hasData ? (
                <TimeseriesChart
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
                  ariaDescription={`${serverName} 24 hour network latency fluctuation`}
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
};

const inferCountryCodeFromLocation = (value) => {
  const text = String(value || '').trim();
  if (/^[a-z]{2}$/i.test(text)) return text;
  const normalized = text.toLowerCase();
  return Object.entries(LOCATION_COUNTRY_CODE_MAP).find(([name]) => normalized.includes(name))?.[1] || '';
};

const getFlagCountry = (server) => {
  if (server.country && server.country !== 'auto') {
    return server.country;
  }
  return (
    server.country_code ||
    server.info?.country_code ||
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
  if (server.country && server.country !== 'auto') {
    return String(server.country).toUpperCase();
  }
  return normalizeLocationDisplayText(
    server.location ||
    server.region ||
    server.resolved_country ||
    server.info?.location ||
    server.info?.region ||
    server.info?.resolved_country ||
    server.info?.country_code ||
    server.country_code ||
    ''
  );
};

const getKumoToken = (tokenName, fallback) => {
  if (typeof window === 'undefined') return fallback;
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(tokenName).trim();
  return value || fallback;
};

const getKumoTerminalTheme = () => ({
  background: getKumoToken('--app-terminal-bg', getKumoToken('--color-kumo-neutral-1000', '#050505')),
  foreground: getKumoToken('--app-terminal-fg', getKumoToken('--color-kumo-neutral-50', '#f8f8f8')),
  cursor: getKumoToken('--color-kumo-brand', 'Highlight'),
});

const toNumber = (value, fallback = 0) => {
  if (value === null || value === undefined || value === '') return fallback;
  const parsed = typeof value === 'number' ? value : parseFloat(String(value).replace(/[^\d.-]/g, ''));
  return Number.isFinite(parsed) ? parsed : fallback;
};

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

const getGpuMemPercent = (record = {}) => {
  if (record.gpu_mem_percent !== null && record.gpu_mem_percent !== undefined) {
    return clampPercent(toNumber(record.gpu_mem_percent, 0));
  }

  const used = toNumber(record.gpu_mem_used, NaN);
  const total = toNumber(record.gpu_mem_total, NaN);
  if (Number.isFinite(used) && Number.isFinite(total) && total > 0) {
    return clampPercent((used / total) * 100);
  }

  return Number.isFinite(used) && used >= 0 && used <= 100 ? used : 0;
};

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
      { name: 'Memory (%)', color: memColor, value: r => toNumber(r.mem_usage, 0) },
      { name: 'CPU Temp (°C)', color: cpuTempColor, value: getCpuTemp },
    ], { normalized: true }) : EMPTY_METRIC_RECORDS,
    gpuSeries: isExpanded ? getMetricSeries(chartRecords, [
      { name: 'GPU', color: gpuColor, value: r => toNumber(r.gpu_usage, 0) },
      { name: 'VRAM', color: vramColor, value: getGpuMemPercent },
      { name: 'Power (W)', color: powerColor, value: r => toNumber(r.gpu_power, 0) },
      { name: 'Temp (°C)', color: gpuTempColor, value: getGpuTemp },
    ], { normalized: true }) : EMPTY_METRIC_RECORDS,
    netSeries: isExpanded ? getMetricSeries(chartRecords, [
      { name: 'Upload', color: txColor, value: r => toNumber(r.net_tx, 0) },
      { name: 'Download', color: rxColor, value: r => toNumber(r.net_rx, 0) },
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
  const latestLatency = toNumber(summary.latest?.latencyMs ?? summary.avgLatency, 0);
  if (!summary.latest || summary.latest.success === false || lossRate >= 5 || latestLatency >= 600) return 'danger';
  if (lossRate >= 1 || jitterMs >= 120 || latestLatency >= 250) return 'warning';
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

const getComposeConfigFiles = (project = {}) => (
  project.ConfigFiles || project.configFiles || project.config_file || project.configFile || ''
);

const getComposeStatus = (project = {}) => (
  String(project.Status || project.status || '-')
);

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

const hasServerDockerInstalled = (server = {}) => {
  const docker = server.info?.docker || {};
  const containers = asArray(docker.containers);
  return server.status === 'online' && (
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


function ServerPage() {
  const { setMainActiveTab, theme, publicApiUrl } = useStore();
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
  

  const [serverCurrentTab, setServerCurrentTab] = useState('list'); // 'list', 'history', 'docker', 'management', 'terminal'
  
  // 主机列表状态
  const [serverList, setServerList] = useState([]);
  const [serverLoading, setServerLoading] = useState(false);
  const [serverSearchText, setServerSearchText] = useState('');
  const [serverStatusFilter, setServerStatusFilter] = useState('all');
  const [serverListViewMode, setServerListViewMode] = useState(getInitialServerListViewMode);
  const [compactVisibleColumns, setCompactVisibleColumns] = useState(getInitialCompactVisibleColumns);
  const [compactColumnMenu, setCompactColumnMenu] = useState({ open: false, x: 0, y: 0 });
  const [expandedServers, setExpandedServers] = useState([]);
  const [renderedCompactExpandedServers, setRenderedCompactExpandedServers] = useState([]);
  const [chartSeriesReadyServers, setChartSeriesReadyServers] = useState([]);
  const [expandedDockerPanels, setExpandedDockerPanels] = useState([]);
  const [draggedServerId, setDraggedServerId] = useState(null);
  
  // 凭据状态
  const [serverCredentials, setServerCredentials] = useState([]);
  
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
    country: 'auto',
    startsAt: '',
    expiresAt: '',
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
  const [agentInstallProtocol, setAgentInstallProtocol] = useState('https');
  const [agentInstallHostType, setAgentInstallHostType] = useState('domain');
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
  const [serverIpDisplayMode, setServerIpDisplayMode] = useState('normal'); // 'normal', 'masked', 'hidden'
  
  // 历史趋势
  const [metricsHistoryTimeRange, setMetricsHistoryTimeRange] = useState('1h'); // '1h', '6h', '24h', '7d', 'all'
  const [metricsHistoryList, setMetricsHistoryList] = useState([]);
  const [metricsHistoryTotal, setMetricsHistoryTotal] = useState(0);
  const [metricsHistoryFilter, setMetricsHistoryFilter] = useState({ serverId: '' });
  const [metricsHistoryLoading, setMetricsHistoryLoading] = useState(false);
  const [metricsHistoryPagination, setMetricsHistoryPagination] = useState({ page: 1, pageSize: 30, totalPages: 1 });
  const [showMetricsCharts, setShowMetricsCharts] = useState(true);
  const [expandedMetricsServers, setExpandedMetricsServers] = useState([]);
  const [metricsCollectorStatus, setMetricsCollectorStatus] = useState(null);
  const [networkQualityByServer, setNetworkQualityByServer] = useState({});
  
  // 全局采集参数
  const [metricsCollectInterval, setMetricsCollectInterval] = useState(5);
  const [monitorConfig, setMonitorConfig] = useState({ metrics_retention_days: 30 });
  
  // Docker 状态
  const [dockerOverviewServers, setDockerOverviewServers] = useState([]);
  const [dockerOverviewLoading, setDockerOverviewLoading] = useState(false);
  const [dockerSubTab, setDockerSubTab] = useState('containers'); // 'containers', 'compose', 'images', 'networks', 'volumes', 'stats'
  const [dockerViewMode, setDockerViewMode] = useState('table'); // 'table', 'grid'
  const [dockerSearchQuery, setDockerSearchQuery] = useState('');
  const [dockerContainerStateFilter, setDockerContainerStateFilter] = useState('all');
  const [dockerSelectedServer, setDockerSelectedServer] = useState('');
  const [dockerTasks, setDockerTasks] = useState([]);
  const [dockerTaskStreamConnected, setDockerTaskStreamConnected] = useState(false);
  const [dockerTaskStreamError, setDockerTaskStreamError] = useState('');
  const [dockerResourceLoading, setDockerResourceLoading] = useState(false);
  const [dockerImages, setDockerImages] = useState([]);
  const [dockerNetworks, setDockerNetworks] = useState([]);
  const [dockerVolumes, setDockerVolumes] = useState([]);
  const [dockerStats, setDockerStats] = useState([]);
  const [dockerComposeProjects, setDockerComposeProjects] = useState([]);
  const [dockerUpdateChecks, setDockerUpdateChecks] = useState({});
  const [dockerUpdateCheckLoading, setDockerUpdateCheckLoading] = useState({});
  const [dockerBulkUpdateChecking, setDockerBulkUpdateChecking] = useState(false);
  const [dockerBulkUpdateCheckServers, setDockerBulkUpdateCheckServers] = useState({});
  const [showDockerCreateModal, setShowDockerCreateModal] = useState(false);
  
  // Agent 升级
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeLog, setUpgradeLog] = useState('');
  const [upgradeProgress, setUpgradeProgress] = useState(0);
  const [forceUpgrade, setForceUpgrade] = useState(false);
  const [upgradeFallbackSsh, setUpgradeFallbackSsh] = useState(true);
  
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
  const terminalResizeTimers = useRef({});
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
  const sshSyncEnabledRef = useRef(false);
  const sftpPathByServerRef = useRef({});

  const [historyColWidths, startHistoryResize] = useTableResize([180, 150, 100, 100, 100, 150]);
  const [dockerColWidths, startDockerResize] = useTableResize([180, 220, 100, 100, 180, 132]);
  const [imagesColWidths, startImagesResize] = useTableResize([250, 100, 100, 150, 100]);
  const [networksColWidths, startNetworksResize] = useTableResize([180, 180, 120, 120, 150, 100]);
  const [volumesColWidths, startVolumesResize] = useTableResize([240, 140, 120, 150, 100]);
  const [statsColWidths, startStatsResize] = useTableResize([180, 120, 160, 120, 120, 150]);
  const showServerStatusSidebar = activeTerminalSidebar === 'status';
  const showSftpSidebar = activeTerminalSidebar === 'sftp';
  const showCommandSidebar = activeTerminalSidebar === 'commands';

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

  useEffect(() => {
    if (showSftpSidebar && activeSSHSessionId) {
      syncSftpToSession(activeSSHSessionId);
    }
  }, [activeSSHSessionId, showSftpSidebar]);

  useEffect(() => {
    sshSyncEnabledRef.current = sshSyncEnabled;
  }, [sshSyncEnabled]);
  
  // 终端持久化实例仓库与 WebSocket 连接引用
  
  useEffect(() => {
    loadServerList();
    loadCredentials();
    const connectTimer = setTimeout(() => {
      connectMetricsStream();
    }, 0);
    const serverListSyncTimer = setInterval(() => {
      loadServerList({ silent: true });
    }, SERVER_STATUS_SYNC_INTERVAL_MS);
    
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
      if (metricFlushTimerRef.current) {
        clearTimeout(metricFlushTimerRef.current);
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

  const syncStoreServerList = () => {};

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
          syncStoreServerList(updated);
          return updated;
        });
        const onlineServerIds = accounts
          .filter(server => server.status === 'online' || server.is_online === true)
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

        const status = item.status || (item.agent_online === true ? 'online' : 'offline');
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
      syncStoreServerList(updated);
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
    if (metricFlushTimerRef.current) return;

    metricFlushTimerRef.current = setTimeout(() => {
      const queued = pendingMetricUpdatesRef.current;
      pendingMetricUpdatesRef.current = [];
      metricFlushTimerRef.current = null;
      handleMetricUpdateBatch(queued);
    }, SERVER_METRIC_FLUSH_DELAY_MS);
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
        const isExpanded = expandedServersRef.current.includes(server.id);
        const interactionGuardUntil = expandInteractionUntilRef.current.get(String(server.id)) || 0;
        const inExpandInteractionGuard = interactionGuardUntil > now;
        if (lastUpdate > 0 && (now - lastUpdate) < SERVER_METRIC_MIN_RENDER_INTERVAL_MS) {
          if (!isExpanded) return server;
          if ((now - lastUpdate) < SERVER_REALTIME_SAMPLE_INTERVAL_MS) return server;
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
          info.gpu = reuseRealtimeValueIfEqual(previousInfo.gpu, {
            Model: pushedGpu.Model || metrics.gpu_model || existingGpu.Model || '',
            Usage: pushedGpu.Usage || metrics.gpu_usage || pushedGpuUsage || existingGpu.Usage || '0%',
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

        const nextInfo = reuseRealtimeValueIfEqual(server.info, info);
        const nextMetricsCache = resolveRealtimeMetricsCache(server.metricsCache, cache, { isExpanded });
        if (inExpandInteractionGuard) return server;
        const nextServer = {
          ...server,
          ...mergeTerminalCapabilities(server, true),
          info: nextInfo,
          status: 'online',
          error: null,
          metricsCache: nextMetricsCache,
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
      syncStoreServerList(updated);
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
    
    if (server.status !== 'online') {
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
          loadNetworkQuality(serverId, { silent: true }).catch(() => {});
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
      country: 'auto',
      startsAt: '',
      expiresAt: '',
      monitorMode: 'agent'
    });
    setSelectedCredentialId('');
    setServerModalMode('add');
    setServerModalError('');
    setShowServerModal(true);
  };
  
  const openEditServerModal = (server) => {
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
      country: server.country || 'auto',
      startsAt: formatDateInputValue(server.starts_at || server.created_at),
      expiresAt: formatDateInputValue(server.expires_at),
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
    toast.info('正在测试连接中...');
    
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
        toast.success('主机删除成功');
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
    setServerModalSaving(true);
    setServerModalError('');
    
    try {
      const tags = serverForm.tagsInput ? serverForm.tagsInput.split(',').map(t => t.trim()).filter(Boolean) : [];
      const payload = {
        name: serverForm.name.trim(),
        host: serverForm.host?.trim() || '',
        port: serverForm.port,
        username: serverForm.username?.trim() || 'agent',
        auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
        tags,
        description: serverForm.description,
        country: serverForm.country,
        starts_at: normalizeStartInputValue(serverForm.startsAt),
        expires_at: normalizeExpiryInputValue(serverForm.expiresAt),
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

  const getAgentBaseApiUrl = () => {
    if (!agentModalData) return '';

    const normalizeOrigin = (value) => {
      const raw = String(value || '').trim().replace(/\/+$/, '');
      if (!raw) return '';

      try {
        const url = new URL(raw.startsWith('http') ? raw : `${agentInstallProtocol}://${raw}`);
        url.protocol = `${agentInstallProtocol}:`;
        return url.origin;
      } catch (e) {
        return '';
      }
    };

    const configuredOrigin = normalizeOrigin(publicApiUrl);
    if (configuredOrigin) return configuredOrigin;

    const modalOrigin = normalizeOrigin(agentModalData.apiUrl);
    if (modalOrigin) return modalOrigin;

    return normalizeOrigin(`${agentInstallProtocol}://${window.location.host}`);
  };

  const getAgentInstallEndpoint = (osType = agentInstallOS) => {
    return buildAgentInstallEndpoint({
      baseUrl: getAgentBaseApiUrl(),
      serverId: agentModalData?.serverId,
      agentKey: agentModalData?.agentKey,
      protocol: agentInstallProtocol || 'https',
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
      protocol: agentInstallProtocol || 'https',
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

  const waitForAgentRestart = async (serverId, initialConnectedAt, timeoutMs = 90000) => {
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
      const response = await fetch(`/api/server/agent/auto-install/${serverId}?protocol=${encodeURIComponent(agentInstallProtocol)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ force_ssh: agentForceSsh }),
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
          setAgentInstallLog(prev => `${prev}\nAgent 未能在 90 秒内重建连接，可能仍在启动中。`);
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
      message: '确定要从目标主机上卸载 Agent 吗？',
      confirmText: '卸载',
      cancelText: '取消',
    }))) return;

    setAgentInstallLoading(true);
    try {
      const response = await fetch(`/api/server/agent/uninstall/${serverId}`, { method: 'POST' });
      const data = await response.json();
      if (data.success) {
        toast.success('Agent 已卸载');
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
    if (!(await dialog.deleteResource({
      resourceType: '主机',
      resourceName: server?.name || server?.host || `#${serverId}`,
    }))) return;
    try {
      const response = await fetch(`/api/server/accounts/${serverId}`, { method: 'DELETE' });
      const data = await response.json();
      if (data.success) {
        toast.success('主机删除成功');
        loadServerList();
      } else {
        toast.error('删除失败: ' + data.error);
      }
    } catch (e) {
      toast.error('删除请求失败');
    }
  };

  const runServerPowerAction = async (serverId, action) => {
    const actionText = action === 'reboot' ? '重启' : '关机';
    const confirmed = await dialog.confirm({
      title: `${actionText}主机`,
      message: action === 'shutdown'
        ? '确定要关闭这台主机吗？此操作不可逆，请确认当前没有关键任务。'
        : '确定要重启这台主机吗？',
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

  const getDockerUpdateBadge = (check) => {
    if (!check) return { variant: 'neutral', label: '未检测', title: '尚未检测镜像更新' };
    if (check.error) return { variant: 'error', label: '失败', title: check.error };
    if (check.hasUpdate) return { variant: 'warning', label: '可更新', title: '远端镜像摘要与本地不一致' };
    if (check.currentDigest && check.latestDigest) return { variant: 'success', label: '已最新', title: '本地镜像已是远端最新摘要' };
    return { variant: 'neutral', label: '已检测', title: '远端或本地摘要不完整，无法严格判断' };
  };

  const getDockerStateBadge = (state) => {
    if (state === 'running') return { variant: 'success', label: '运行' };
    if (state === 'paused' || state === 'restarting') return { variant: 'warning', label: state === 'paused' ? '暂停' : '重启中' };
    if (state === 'stopped' || state === 'dead') return { variant: 'error', label: state === 'dead' ? '异常' : '停止' };
    return { variant: 'neutral', label: '未知' };
  };

  const openBatchAgentModal = () => {
    setSelectedBatchServers([]);
    setBatchInstallResults([]);
    setBatchAgentForceSsh(false);
    setShowBatchAgentModal(true);
  };

  const selectAllBatchServers = () => {
    setSelectedBatchServers(serverList.map(server => server.id));
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

    const initialStates = new Map();
    for (const item of initialResults) {
      try {
        const res = await fetch(`/api/server/agent/connection-info/${item.serverId}`);
        const data = await res.json();
        initialStates.set(item.serverId, data.status === 'online' ? (data.connectedAt || 0) : 0);
      } catch (e) {
        initialStates.set(item.serverId, 0);
      }
    }

    const pendingVerification = [];
    await Promise.all(initialResults.map(async item => {
      updateBatchResult(item.serverId, { status: 'processing' });
      try {
        const response = await fetch(`/api/server/agent/auto-install/${item.serverId}?protocol=${encodeURIComponent(agentInstallProtocol)}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ force_ssh: batchAgentForceSsh }),
        });
        const data = await response.json();
        if (data.success) {
          updateBatchResult(item.serverId, { status: 'verifying' });
          pendingVerification.push(item);
        } else {
          updateBatchResult(item.serverId, { status: 'failed', error: data.error || '安装失败' });
        }
      } catch (e) {
        updateBatchResult(item.serverId, { status: 'failed', error: e.message });
      }
    }));

    if (pendingVerification.length > 0) {
      const startTime = Date.now();
      const timeoutMs = 90000;
      await new Promise(resolve => setTimeout(resolve, 3000));

      while (pendingVerification.length > 0 && Date.now() - startTime < timeoutMs) {
        for (let i = pendingVerification.length - 1; i >= 0; i--) {
          const item = pendingVerification[i];
          const initialConnectedAt = initialStates.get(item.serverId) || 0;
          try {
            const res = await fetch(`/api/server/agent/connection-info/${item.serverId}`);
            const data = await res.json();
            const connectedAt = data.connectedAt || 0;
            if (data.status === 'online' && (initialConnectedAt === 0 || connectedAt >= initialConnectedAt)) {
              updateBatchResult(item.serverId, { status: 'success', error: '' });
              pendingVerification.splice(i, 1);
            }
          } catch (e) {
            // keep waiting
          }
        }
        if (pendingVerification.length > 0) {
          await new Promise(resolve => setTimeout(resolve, 2000));
        }
      }

      pendingVerification.forEach(item => {
        updateBatchResult(item.serverId, { status: 'failed', error: '验证超时: Agent 未能在 90 秒内重建连接' });
      });
    }

    setAgentInstallLoading(false);
    toast.info('批量 Agent 部署任务已完成');
    loadServerList();
  };

  const openUpgradeModal = () => {
    setUpgradeLog('');
    setUpgradeProgress(0);
    setUpgrading(false);
    setForceUpgrade(false);
    setUpgradeFallbackSsh(true);
    setShowUpgradeModal(true);
  };

  const getAgentUpgradeTargets = () => serverList.filter(s =>
    s.status === 'online' || s.monitor_mode === 'agent' || s.host === '0.0.0.0'
  );

  const performOneKeyUpgrade = async () => {
    if (upgrading) return;

    setUpgrading(true);
    setUpgradeLog('开始批量升级任务...\n');
    setUpgradeProgress(0);

    const appendLog = (line) => setUpgradeLog(prev => prev + line);
    const targetServers = getAgentUpgradeTargets();

    if (targetServers.length === 0) {
      appendLog('没有检测到在线的 Agent 主机。\n');
      setUpgrading(false);
      return;
    }

    appendLog(`Detected ${targetServers.length} online agents.\n`);
    appendLog('正在获取初始连接状态..\n');

    const initialStates = new Map();
    for (const server of targetServers) {
      try {
        const response = await fetch(`/api/server/agent/connection-info/${server.id}`);
        const data = await response.json();
        initialStates.set(server.id, data.status === 'online' ? (data.connectedAt || 0) : 0);
      } catch (e) {
        initialStates.set(server.id, 0);
      }
    }

    let successCount = 0;
    let failCount = 0;
    for (let i = 0; i < targetServers.length; i++) {
      const server = targetServers[i];
      appendLog(`[${i + 1}/${targetServers.length}] Sending upgrade command to ${server.name}... `);
      try {
        const response = await fetch(`/api/server/agent/auto-install/${server.id}?protocol=${encodeURIComponent(agentInstallProtocol)}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ force_ssh: forceUpgrade }),
        });
        const data = await response.json();
        if (data.success) {
          successCount++;
          appendLog('Sent.\n');
        } else {
          failCount++;
          appendLog(`Failed: ${data.error || '未知错误'}\n`);
        }
      } catch (e) {
        failCount++;
        appendLog(`Network Error: ${e.message}\n`);
      }
      setUpgradeProgress(Math.round(((i + 1) / targetServers.length) * 50));
      await new Promise(resolve => setTimeout(resolve, 200));
    }

    appendLog(`\n指令下发完成: 成功 ${successCount} 台，失败 ${failCount} 台。${upgradeFallbackSsh ? ' (策略: 开启 SSH 保底)' : ''}\n`);
    appendLog('正在验证 Agent 重启状态，限时 30 秒...\n');

    const monitorMap = new Map(targetServers.map(server => [server.id, 'pending']));
    const monitorStartTime = Date.now();
    await new Promise(resolve => setTimeout(resolve, 5000));

    while (Date.now() - monitorStartTime <= 30000) {
      let allDone = true;
      for (const [serverId, status] of monitorMap.entries()) {
        if (status === 'ok') continue;
        try {
          const response = await fetch(`/api/server/agent/connection-info/${serverId}`);
          const data = await response.json();
          const oldConnectedAt = initialStates.get(serverId) || 0;
          if (data.status === 'online' && (oldConnectedAt === 0 || (data.connectedAt || 0) > oldConnectedAt)) {
            const serverName = targetServers.find(s => s.id === serverId)?.name || serverId;
            appendLog(`   [${serverName}] 已重新上线 (v${data.version || '?'})\n`);
            monitorMap.set(serverId, 'ok');
          } else {
            allDone = false;
          }
        } catch (e) {
          allDone = false;
        }
      }

      setUpgradeProgress(50 + Math.min(50, Math.round(((Date.now() - monitorStartTime) / 30000) * 50)));
      if (allDone) {
        appendLog('\n所有目标 Agent 均已完成升级并重新上线。\n');
        setUpgradeProgress(100);
        setUpgrading(false);
        loadServerList();
        return;
      }
      await new Promise(resolve => setTimeout(resolve, 3000));
    }

    appendLog('\n监控超时，部分 Agent 未能按时上线。\n');
    if (upgradeFallbackSsh) {
      const timeoutServers = targetServers.filter(server => monitorMap.get(server.id) !== 'ok');
      if (timeoutServers.length > 0) {
        appendLog(`触发 SSH 保底策略：${timeoutServers.length} 台主机开始强制覆盖安装。\n`);
        for (const server of timeoutServers) {
          appendLog(`   [${server.name}] SSH 覆盖安装... `);
          try {
            const response = await fetch(`/api/server/agent/auto-install/${server.id}?protocol=${encodeURIComponent(agentInstallProtocol)}`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ force_ssh: true }),
            });
            const data = await response.json();
            appendLog(data.success ? '已下发\n' : `失败: ${data.error || '未知错误'}\n`);
          } catch (e) {
            appendLog(`网络错误: ${e.message}\n`);
          }
        }
      }
    } else {
      appendLog('请检查网络或手动使用 SSH 重新部署。\n');
    }

    setUpgradeProgress(100);
    setUpgrading(false);
    loadServerList();
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
        a.download = `servers_export_${new Date().toISOString().slice(0,10)}.json`;
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
    if (!(await dialog.deleteResource({
      resourceType: '主机凭据',
      resourceName: credential?.name || credential?.username || `#${id}`,
    }))) return;
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
  
      
  
  const updateMetricsCollectInterval = async (val) => {
    const nextValue = Math.max(1, Math.round(toNumber(val, 5)));
    setMetricsCollectInterval(nextValue);
    try {
      await fetch('/api/server/monitor/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ metrics_collect_interval: nextValue * 60 })
      });
      toast.success('指标采集时间间隔更新成功');
    } catch (e) {
      console.error(e);
    }
  };
  
  const updateMetricsRetentionDays = async (value) => {
    const nextValue = Math.max(1, Math.min(180, Math.round(toNumber(value, 30))));
    setMonitorConfig({ metrics_retention_days: nextValue });
    try {
      await fetch('/api/server/monitor/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ metrics_retention_days: nextValue })
      });
    } catch (e) {
      console.error(e);
    }
  };
  
  
  
  useEffect(() => {
    if (serverCurrentTab === 'history') {
      loadMetricsHistory(1);
    }
  }, [serverCurrentTab, metricsHistoryTimeRange, metricsHistoryFilter.serverId]);
  
  const loadMetricsHistory = async (page = 1) => {
    setMetricsHistoryLoading(true);
    try {
      let startTime = null;
      const now = Date.now();
      if (metricsHistoryTimeRange === '1h') startTime = new Date(now - 60*60*1000).toISOString();
      else if (metricsHistoryTimeRange === '6h') startTime = new Date(now - 6*60*60*1000).toISOString();
      else if (metricsHistoryTimeRange === '24h') startTime = new Date(now - 24*60*60*1000).toISOString();
      else if (metricsHistoryTimeRange === '7d') startTime = new Date(now - 7*24*60*60*1000).toISOString();
      
      const params = new URLSearchParams({
        page,
        pageSize: 15,
        serverId: metricsHistoryFilter.serverId
      });
      if (startTime) params.append('startTime', startTime);
      
      const response = await fetch(`/api/server/metrics/history?${params}`);
      const data = await response.json();
      if (data.success) {
        setMetricsHistoryList(data.data || []);
        setMetricsHistoryTotal(data.pagination?.total || 0);
        setMetricsHistoryPagination({
          page: data.pagination?.page || 1,
          pageSize: data.pagination?.pageSize || 15,
          totalPages: data.pagination?.totalPages || 1
        });
      }
      
      // 读取采集器状态
      const statusRes = await fetch('/api/server/monitor/status');
      const statusData = await statusRes.json();
      if (statusData.success) {
        setMetricsCollectorStatus(statusData.status);
        if (statusData.status?.interval) {
          setMetricsCollectInterval(Math.floor(statusData.status.interval / 60000));
        }
      }
    } catch (e) {
      console.error(e);
    } finally {
      setMetricsHistoryLoading(false);
    }
  };
  
  const triggerManualCollect = async () => {
    toast.info('正在发送指令手动采集历史指标点...');
    try {
      const res = await fetch('/api/server/monitor/collect', { method: 'POST' });
      const data = await res.json();
      if (data.success) {
        toast.success('就绪：新指标点采集完成！');
        loadMetricsHistory(1);
      }
    } catch (e) {
      toast.error('采集失败');
    }
  };
  
  const clearMetricsHistory = async () => {
    if (!(await dialog.confirm('确定要清除数据库中存储的所有的历史监控记录吗？'))) return;
    try {
      const res = await fetch('/api/server/metrics/history', { method: 'DELETE' });
      const data = await res.json();
      if (data.success) {
        toast.success('监控历史记录清空成功');
        loadMetricsHistory(1);
      }
    } catch (e) {
      toast.error('清空失败');
    }
  };
  
  // 按主机对历史数据进行分类展示
  const groupedMetricsHistory = useMemo(() => {
    const groups = {};
    metricsHistoryList.forEach(rec => {
      const serverId = rec.server_id || 'unknown';
      if (!groups[serverId]) groups[serverId] = [];
      groups[serverId].push(rec);
    });
    return groups;
  }, [metricsHistoryList]);
  
  
  
  useEffect(() => {
    if (serverCurrentTab === 'docker') {
      ensureDockerTaskStream();
      loadDockerResources();
    }
  }, [serverCurrentTab, dockerSubTab, dockerSelectedServer, dockerHostOptionKey]);
  
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
          const task = JSON.parse(event.data);
          if (task && task.domain === 'docker') {
            setDockerTasks(prev => {
              const updated = prev.filter(t => t.taskId !== task.taskId);
              updated.unshift(task);
              if (updated.length > 30) updated.pop();
              return updated;
            });
            // 任务成功完成时，如果正在当前标签页，触发静默刷新列表
            if (task.state === 'success') {
              toast.success(`任务 [${task.action}] 执行成功`);
              setTimeout(() => loadDockerResources(), 800);
            } else if (task.state === 'failed') {
              toast.error(`任务 [${task.action}] 失败: ${task.error || ''}`);
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
        message: `确定要停止容器 ${targetName} 吗？正在运行的服务会中断。`,
        confirmText: '停止',
        variant: 'danger',
      },
      'container.restart': {
        title: '重启容器',
        message: `确定要重启容器 ${targetName} 吗？服务会短暂中断。`,
        confirmText: '重启',
        variant: 'danger',
      },
      'container.update': {
        title: '一键更新容器',
        message: `确定要更新容器 ${targetName} 吗？该操作会拉取镜像并重建容器。`,
        confirmText: '开始更新',
        variant: 'danger',
      },
      'compose.down': {
        title: '停止 Compose 项目',
        message: `确定要停止 Compose 项目 ${targetName} 吗？相关服务会中断。`,
        confirmText: '停止项目',
        variant: 'danger',
      },
      'image.prune': {
        title: '清理未使用镜像',
        message: '确定要清理该主机上的未使用 Docker 镜像吗？未被容器引用的镜像会被删除。',
        confirmText: '清理镜像',
        variant: 'danger',
      },
      'network.prune': {
        title: '清理未使用网络',
        message: '确定要清理该主机上的未使用 Docker 网络吗？未被容器使用的自定义网络会被删除。',
        confirmText: '清理网络',
        variant: 'danger',
      },
      'volume.prune': {
        title: '清理未使用存储卷',
        message: '确定要清理该主机上的未使用 Docker 存储卷吗？未被容器使用的数据卷会被删除。',
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

  const submitDockerTask = async (action, payload = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    if (!serverId) {
      toast.warning('请先选择一台主机');
      return;
    }
    const confirmation = getDockerTaskConfirmation(action, payload);
    if (confirmation?.deleteResource) {
      if (!(await dialog.deleteResource(confirmation))) return;
    } else if (confirmation && !(await dialog.confirm(confirmation))) {
      return;
    }
    try {
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
        toast.info('Docker 管理任务提交成功，正在等待后台调度');
      } else {
        toast.error('提交失败: ' + data.error);
      }
    } catch (e) {
      toast.error('服务下发异常');
    }
  };
  
  const loadDockerResources = async () => {
    setDockerResourceLoading(true);
    try {
      const params = new URLSearchParams();
      params.set('scope', getDockerOverviewScope(dockerSubTab));
      if (dockerSelectedServer) {
        params.set('serverId', dockerSelectedServer);
      }

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
      setDockerOverviewServers([]);
      setDockerImages([]);
      setDockerNetworks([]);
      setDockerVolumes([]);
      setDockerStats([]);
      setDockerComposeProjects([]);
    } finally {
      setDockerResourceLoading(false);
    }
  };

  const visibleDockerContainerServers = useMemo(() => {
    if (dockerSelectedServer) return dockerOverviewServers;
    return dockerOverviewServers.filter(server => asArray(server.resources?.containers).length > 0);
  }, [dockerOverviewServers, dockerSelectedServer]);

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

      if (!options.silent) {
        const updateCount = results.filter(item => item?.has_update || item?.hasUpdate).length;
        if (updateCount > 0) {
          toast.warning(`检测完成，发现 ${updateCount} 个可更新镜像`);
        } else {
          toast.success('检测完成，暂无可更新镜像');
        }
      }

      return { ok: true, results };
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

  const checkVisibleDockerUpdates = async () => {
    const targets = visibleDockerContainerServers.filter(server => asArray(server.resources?.containers).length > 0);
    if (targets.length === 0) {
      toast.warning('当前没有可检测的 Docker 容器');
      return;
    }

    setDockerBulkUpdateChecking(true);
    setDockerBulkUpdateCheckServers(Object.fromEntries(targets.map(server => [server.id, true])));

    try {
      const settled = await Promise.allSettled(
        targets.map(server => checkDockerUpdatesForServer(server, null, { silent: true }).finally(() => {
          setDockerBulkUpdateCheckServers(prev => {
            const next = { ...prev };
            delete next[server.id];
            return next;
          });
        }))
      );
      const finished = settled.map(item => item.status === 'fulfilled'
        ? item.value
        : { ok: false, error: item.reason?.message || '检查失败', results: [] });
      const updateCount = finished.reduce(
        (sum, item) => sum + item.results.filter(result => result?.has_update || result?.hasUpdate).length,
        0
      );
      const failedCount = finished.filter(item => !item.ok).length;

      if (failedCount > 0) {
        toast.warning(`检测完成，${updateCount} 个可更新，${failedCount} 台主机检测失败`);
      } else if (updateCount > 0) {
        toast.warning(`检测完成，发现 ${updateCount} 个可更新镜像`);
      } else {
        toast.success('检测完成，暂无可更新镜像');
      }
    } finally {
      setDockerBulkUpdateChecking(false);
      setDockerBulkUpdateCheckServers({});
    }
  };

  const renderDockerEmptyState = (message) => (
    <div className="app-card p-10 text-center text-xs text-kumo-subtle">
      {message}
    </div>
  );
  

  

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

  const createSSHSocket = (sessionId, sessionMeta, terminal) => {
    if (typeof WebSocket !== 'undefined') {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const params = new URLSearchParams({
        server_id: String(sessionMeta.server.id),
        session_id: sessionId,
        cols: String(sshSessionRefs.current[sessionId]?.terminal?.cols || 120),
        rows: String(sshSessionRefs.current[sessionId]?.terminal?.rows || 32),
      });
      const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh?${params.toString()}`);

      ws.onopen = () => {
        terminal.writeln('\r\n\x1b[1;32mConnecting SSH terminal...\x1b[0m');
      };
      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data);
          if (message.type === 'data') {
            terminal.write(message.data || '');
          } else if (message.type === 'status' && message.data === 'connected') {
            setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: true } : s));
            terminal.writeln('\r\n\x1b[1;32mSSH terminal connected.\x1b[0m');
          } else if (message.type === 'error') {
            terminal.writeln(`\r\n\x1b[1;31m${message.data || 'SSH connection failed'}\x1b[0m`);
          }
        } catch {
          terminal.write(String(event.data || ''));
        }
      };
      ws.onerror = () => {
        terminal.writeln('\r\n\x1b[1;31mSSH terminal connection error.\x1b[0m');
      };
      ws.onclose = () => {
        setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
        terminal.writeln('\r\n\x1b[1;33mSSH terminal connection closed.\x1b[0m');
      };
      return ws;
    }

    terminal.writeln('\n\x1b[1;33mSSH 终端 WebSocket 已随 Node 后端迁移退役，当前 Go 后端仅保留 Agent /socket.io/ 实时通道。\x1b[0m');
    setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
    return {
      readyState: WebSocket.CLOSED,
      send: () => {},
      close: () => {},
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
      lastSeq: 0
    };

    const ws = createSSHSocket(sessionId, sessionMeta, terminal);
    sshSessionRefs.current[sessionId].ws = ws;

    const inputDisposable = terminal.onData(data => {
      const currentWs = sshSessionRefs.current[sessionId]?.ws;
      if (currentWs?.readyState === WebSocket.OPEN) {
        currentWs.send(JSON.stringify({ type: 'input', data }));
      }

      if (sshSyncEnabledRef.current && visibleSessionIdsRef.current.includes(sessionId)) {
        visibleSessionIdsRef.current.forEach(targetId => {
          if (targetId === sessionId) return;
          const targetSession = sshSessionRefs.current[targetId];
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
        scheduleTerminalFit(id, 120);
      }
    });
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

  const triggerSplitPane = (targetId, position, sourceId = getSplitSourceSessionId(targetId)) => {
    if (!sourceId || sourceId === targetId) return;

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
    if (serverStatusFilter !== 'all') {
      list = list.filter(s => s.status === serverStatusFilter);
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
    const online = serverList.filter(s => s.status === 'online').length;
    const offline = total - online;
    return { total, online, offline };
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
  
  return (
    <div
      className={
        serverCurrentTab === 'terminal'
          ? 'flex h-[calc(100dvh-80px)] min-h-0 w-full min-w-0 flex-col gap-3 overflow-hidden px-1 sm:h-[calc(100dvh-88px)] lg:h-[calc(100dvh-92px)]'
          : 'flex w-full flex-col gap-3 px-1'
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
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <div className="min-w-0 w-full min-[450px]:w-auto">
          <Tabs
            {...MODULE_TABS_PROPS}
            value={serverCurrentTab}
            onValueChange={setServerCurrentTab}
            tabs={[
              { value: 'list', label: <ServerModuleTabLabel icon={Server} short="主机">主机管理</ServerModuleTabLabel> },
              { value: 'history', label: <ServerModuleTabLabel icon={History} short="趋势">历史趋势</ServerModuleTabLabel> },
              { value: 'docker', label: <ServerModuleTabLabel icon={Box}>Docker</ServerModuleTabLabel> },
              { value: 'management', label: <ServerModuleTabLabel icon={Settings} short="管理">后台管理</ServerModuleTabLabel> },
              ...(sshSessions.length > 0
                ? [{
                    value: 'terminal',
                    label: <ServerModuleTabLabel icon={TerminalIcon} short="SSH" badge={sshSessions.length}>SSH 终端</ServerModuleTabLabel>,
                  }]
                : []),
            ]}
          />
        </div>
        
        {/* 右侧快速连接 */}
        <div className="flex w-full items-center justify-end gap-2 min-[450px]:w-auto">
          {serverCurrentTab === 'list' && (
            <div className="flex items-center justify-end gap-2">
              <Button size="sm"
                variant="secondary"
                icon={<Upload className="w-3.5 h-3.5" />}
                onClick={openUpgradeModal}
                title="升级所有在线 Agent"
                className="hidden md:inline-flex"
              >
                升级 Agent
              </Button>
              <Button size="sm"
                variant="secondary"
                icon={<Shield className="w-3.5 h-3.5" />}
                onClick={openBatchAgentModal}
                title="批量部署 Agent"
                className="hidden md:inline-flex"
              >
                批量部署
              </Button>
              <Button
                shape="square" size="sm"
                variant="secondary"
                icon={<RefreshCw className="w-3.5 h-3.5" />}
                onClick={loadServerList}
                loading={serverLoading}
                title="刷新列表"
                aria-label="刷新列表"
              />
              <Button size="sm"
                variant="secondary"
                icon={<Upload className="w-3.5 h-3.5" />}
                onClick={exportServers}
                title="导出主机配置"
                className="hidden md:inline-flex"
              >
                导出
              </Button>
              <Button size="sm"
                variant="secondary"
                icon={<Download className="w-3.5 h-3.5" />}
                onClick={openImportServerModal}
                title="导入主机配置"
                className="hidden md:inline-flex"
              >
                导入
              </Button>
              <Button size="sm"
                variant="secondary"
                icon={<RotateCw className="w-3.5 h-3.5" />}
                onClick={probeAllServers}
                title="触发所有主机探测"
                className="hidden sm:inline-flex"
              >
                探测
              </Button>
              <Button size="sm"
                variant="primary"
                icon={<Plus className="w-3.5 h-3.5" />}
                onClick={openAddServerModal}
              >
                新增主机
              </Button>
            </div>
          )}
        </div>
      </div>
      
      {/* ==================== 1. 主机管理 ==================== */}
      {serverCurrentTab === 'list' && (
        <div className="flex flex-col gap-4">
          {/* 控制过滤器栏 */}
          <div className={SERVER_SECONDARY_BAR_CLASS}>
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                className={HOST_FILTER_TABS_CLASS}
                listClassName={HOST_FILTER_TABS_LIST_CLASS}
                indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                value={serverStatusFilter}
                onValueChange={setServerStatusFilter}
                tabs={[
                  { value: 'all', label: `全部 (${statsSummary.total})` },
                  { value: 'online', label: `在线 (${statsSummary.online})` },
                  { value: 'offline', label: `离线 (${statsSummary.offline})` },
                ]}
              />
              <Tabs
                {...TOOL_TABS_PROPS}
                className={HOST_FILTER_TABS_CLASS}
                listClassName={HOST_FILTER_TABS_LIST_CLASS}
                indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                value={serverIpDisplayMode}
                onValueChange={setServerIpDisplayMode}
                tabs={[
                  { value: 'normal', label: '明文' },
                  { value: 'masked', label: '打码' },
                  { value: 'hidden', label: '隐藏' },
                ]}
              />
              <Tabs
                {...TOOL_TABS_PROPS}
                className={HOST_FILTER_TABS_CLASS}
                listClassName={HOST_FILTER_TABS_LIST_CLASS}
                indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                value={serverListViewMode}
                onValueChange={setServerListViewMode}
                tabs={[
                  { value: 'cards', label: '卡片' },
                  { value: 'compact', label: '表格' },
                ]}
              />
            </div>
            
            <div className="relative w-full lg:w-72">
              <Search className="pointer-events-none absolute left-3 top-1/2 z-1 h-3.5 w-3.5 -translate-y-1/2 text-kumo-subtle" />
              <Input
                type="text"
                placeholder="搜索主机名称、IP 或标签..."
                aria-label="搜索主机" size="sm"
                value={serverSearchText}
                onChange={e => setServerSearchText(e.target.value)}
                className="w-full pl-9"
              />
            </div>
          </div>
          
          {/* 列表渲染 */}
          {serverLoading && serverList.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-12 app-card app-card-md text-kumo-subtle gap-2">
              <div className="w-6 h-6 border-2 border-kumo-brand border-t-transparent rounded-full animate-spin"></div>
              <p className="text-xs">正在连接并加载主机结构中...</p>
            </div>
          ) : filteredServers.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-16 app-card app-card-md text-kumo-subtle gap-1.5">
              <span className="text-xl">🔍</span>
              <p className="text-xs">未找到符合当前条件的主机节点</p>
            </div>
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
                            className={`!px-2 !py-2 text-center text-[10px] whitespace-nowrap ${column.id === 'actions' ? `!pl-1 !pr-2 ${COMPACT_STICKY_ACTION_CLASS}` : ''}`}
                          >
                            <div className={`flex items-center ${HOST_COMPACT_HEADER_BOX_CLASS[column.id] || 'justify-center'}`}>
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
                        const disk = server.info?.disk?.[0] || {};
                        const diskUsage = clampPercent(toNumber(disk.usage, 0));
                        const memUsage = clampPercent(toNumber(server.info?.memory?.Usage, 0));
                        const cpuUsage = clampPercent(toNumber(server.info?.cpu?.Usage, 0));
                        const cpuTemp = toNumber(server.info?.cpu?.Temp, 0);
                        const terminalProtocol = resolveTerminalProtocol(server);
                        const effectiveTerminalProtocol = terminalProtocol || (hasSshEndpoint(server) ? 'ssh' : null);
                        const terminalLabel = effectiveTerminalProtocol === 'agent' ? 'Agent 终端' : 'SSH 终端';
                        const chartLoading = !!server.metricsLoading && records.length === 0;
                        const physicalCores = server.info?.cpu?.PhysicalCores || server.info?.cpu?.Cores;
                        const logicalCores = server.info?.cpu?.LogicalCores;
                        const coreText = physicalCores && logicalCores && physicalCores !== logicalCores
                          ? `${physicalCores}核 / ${logicalCores}线程`
                          : `${physicalCores || '-'}核`;
                        const dockerContainers = server.info?.docker?.containers || [];
                        const runningContainers = dockerContainers.filter(c => getDockerContainerState(c) === 'running').length;
                        const lifecycle = getServerLifecycle(server);
                        const canRefresh = server.status === 'online' && !server.loading;
                        const networkQuality = networkQualityByServer[server.id] || {};
                        const networkQualitySeries = isExpanded ? buildNetworkQualitySeries(networkQuality, isDarkMode) : [];
                        const hasNetworkQualityData = isExpanded && networkQualitySeries.some(series => series.data.length > 0);
                        const networkQualityUnsupported = isExpanded && (
                          !!networkQuality.unsupported
                          || isNetworkQualityUnsupportedError(networkQuality.error || networkQuality.unsupportedMessage)
                        );

                        return (
                          <React.Fragment key={server.id}>
                            <Table.Row
                              variant={isExpanded ? 'selected' : 'default'}
                              className="cursor-pointer border-b border-kumo-line/80 hover:bg-kumo-recessed/15"
                              onClick={() => toggleServerExpand(server.id)}
                              onContextMenu={(event) => {
                                event.preventDefault();
                                event.stopPropagation();
                                openEditServerModal(server);
                              }}
                            >
                              {isCompactColumnVisible('status') && (
                                <Table.Cell className="!px-2 !py-1.5 text-center whitespace-nowrap">
                                  <Badge variant={server.status === 'online' ? 'success' : 'error'} appearance="dot">
                                    {server.status === 'online' ? '在线' : '离线'}
                                  </Badge>
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('name') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <div className="flex w-[96px] items-center gap-2">
                                    <i className={getOSIconClass(server.info?.platform)}></i>
                                    <div className="min-w-0">
                                      <div className="truncate font-bold text-kumo-strong" title={server.name}>{server.name}</div>
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
                                <Table.Cell className="!px-2 !py-1.5 text-center whitespace-nowrap">
                                  <div className="flex w-[64px] items-center justify-center gap-1.5">
                                    {locationText ? (
                                      <>
                                        {country && <CountryFlag preferSvg countryCode={country} className="h-3.5 w-5 shrink-0 !rounded-[2px] text-sm" />}
                                        <span className="truncate font-semibold uppercase text-kumo-strong" title={locationText}>{locationText}</span>
                                      </>
                                    ) : (
                                      <span className="font-semibold text-kumo-subtle">-</span>
                                    )}
                                  </div>
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('uptime') && (
                                <Table.Cell className="!px-2 !py-1.5 text-center whitespace-nowrap">
                                  <span className="font-semibold tabular-nums text-kumo-strong">
                                    {formatUptimeDaysOnly(server.info?.uptime || server.info?.system?.Uptime)}
                                  </span>
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('load') && (
                                <Table.Cell className="!px-2 !py-1.5 text-center whitespace-nowrap">
                                  <code className={`rounded-md bg-kumo-recessed/50 px-1.5 py-1 font-mono text-[10px] text-kumo-strong ${COMPACT_INLINE_BOX_CLASS}`}>
                                    {getPrimaryLoadValue(server.info?.cpu?.Load)}
                                  </code>
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('speed') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <DenseTrafficCell
                                    left={rx.num}
                                    leftUnit={rx.unit}
                                    right={tx.num}
                                    rightUnit={tx.unit}
                                    leftTitle={server.info?.network?.rx_speed || '0 B/s'}
                                    rightTitle={server.info?.network?.tx_speed || '0 B/s'}
                                  />
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('traffic') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <DenseTrafficCell
                                    left={rxTotal.num}
                                    leftUnit={rxTotal.unit}
                                    right={txTotal.num}
                                    rightUnit={txTotal.unit}
                                    leftTitle={rxTotal.text}
                                    rightTitle={txTotal.text}
                                  />
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('cpu') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <DenseUsageMeter
                                    label="CPU"
                                    value={cpuUsage}
                                    detail={`${Math.round(cpuUsage)}%`}
                                    indicatorClassName="!bg-none !bg-kumo-success"
                                  />
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('memory') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <DenseUsageMeter
                                    label="Mem"
                                    value={memUsage}
                                    detail={`${Math.round(memUsage)}%`}
                                    indicatorClassName="!bg-none !bg-kumo-info"
                                  />
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('disk') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <DenseUsageMeter
                                    label="Disk"
                                    value={diskUsage}
                                    detail={`${Math.round(diskUsage)}%`}
                                    indicatorClassName="!bg-none !bg-kumo-warning"
                                  />
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('remaining') && (
                                <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap">
                                  <div title={lifecycle.expiresAt ? `${formatDateTime(lifecycle.startsAt)} - ${formatDateTime(lifecycle.expiresAt)}，剩余 ${Math.round(lifecycle.remainingPercent)}%` : '永久'}>
                                    <DenseLifecycleMeter lifecycle={lifecycle} />
                                  </div>
                                </Table.Cell>
                              )}
                              {isCompactColumnVisible('actions') && (
                                <Table.Cell sticky="right" className={`!py-1.5 !pl-1 !pr-2 text-center whitespace-nowrap ${COMPACT_STICKY_ACTION_CLASS}`}>
                                  <div className="flex items-center justify-center gap-1" onClick={event => event.stopPropagation()}>
                                    <Button
                                      shape="square" size="sm"
                                      variant="secondary"
                                      className={COMPACT_ACTION_BUTTON_CLASS}
                                      title="刷新详情"
                                      aria-label="刷新详情"
                                      icon={<RefreshCw className="h-3.5 w-3.5" />}
                                      onClick={() => refreshServerInfo(server.id)}
                                      disabled={!canRefresh}
                                    />
                                    <Button
                                      shape="square" size="sm"
                                      variant="secondary"
                                      className={COMPACT_ACTION_BUTTON_CLASS}
                                      title={effectiveTerminalProtocol ? terminalLabel : '终端不可用'}
                                      aria-label={effectiveTerminalProtocol ? terminalLabel : '终端不可用'}
                                      icon={<TerminalIcon className="h-3.5 w-3.5" />}
                                      onClick={() => openSSHTerminal(server)}
                                      disabled={!canOpenTerminal(server) && !hasSshEndpoint(server)}
                                    />
                                  </div>
                                </Table.Cell>
                              )}
                            </Table.Row>

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
                                          <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2 lg:grid-cols-[repeat(auto-fit,minmax(154px,1fr))]">
                                            <DenseDetailChip label="核心" value={coreText} />
                                            <DenseDetailChip label="CPU 温度" value={cpuTemp > 0 ? `${Math.round(cpuTemp)}°C` : '-'} valueClassName={getTempColorClass(cpuTemp)} />
                                            <DenseDetailChip label="内存" value={`${server.info?.memory?.Used || '-'} / ${server.info?.memory?.Total || '-'}`} />
                                            <DenseDetailChip label="连接" value={server.info?.network?.connections || 0} />
                                            <DenseDetailChip label="Docker" value={server.info?.docker?.installed ? `${runningContainers}/${dockerContainers.length} 运行` : '未安装'} />
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
                                                  <ChartLegend.SmallItem name="CPU" color={cpuColor} value={`${Math.round(cpuUsage)}%`} />
                                                  <ChartLegend.SmallItem name="Memory" color={memColor} value={`${Math.round(memUsage)}%`} />
                                                  <ChartLegend.SmallItem name="Temp" color={cpuTempColor} value={getLatestMetricValue(records, getCpuTemp, v => `${v.toFixed(1)}°C`)} />
                                                </>
                                              )}
                                            >
                                              {(tooltipBoundary) => (
                                                <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={compactExpandedChartHeight} />}>
                                                  <TimeseriesChart
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
                                                    ariaDescription={`${server.name} CPU and memory usage trend`}
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
                                                  {hasGpuData && <TrendSeriesLabel name="VRAM" color={vramColor} />}
                                                  {hasGpuData && <TrendSeriesLabel name="Power" color={powerColor} />}
                                                  {hasGpuData && <TrendSeriesLabel name="Temp" color={gpuTempColor} />}
                                                  {!hasGpuData && <ChartLegend.SmallItem name="Upload" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} />}
                                                  {!hasGpuData && <ChartLegend.SmallItem name="Download" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} />}
                                                </>
                                              )}
                                            >
                                              {(tooltipBoundary) => (
                                                <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={compactExpandedChartHeight} />}>
                                                  <TimeseriesChart
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
                                                    ariaDescription={`${server.name} compact host trend`}
                                                  />
                                                </DeferredRender>
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
                                                    <ChartLegend.SmallItem name="Upload" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} />
                                                    <ChartLegend.SmallItem name="Download" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} />
                                                  </>
                                                )}
                                              >
                                                {(tooltipBoundary) => (
                                                  <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={compactExpandedChartHeight} />}>
                                                    <TimeseriesChart
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
                                                      ariaDescription={`${server.name} compact network trend`}
                                                    />
                                                  </DeferredRender>
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
                const dockerContainers = server.info?.docker?.containers || [];
                const dockerExpanded = expandedDockerPanels.includes(server.id);
                const runningContainers = dockerContainers.filter(c => getDockerContainerState(c) === 'running').length;
                const pausedContainers = dockerContainers.filter(c => getDockerContainerState(c) === 'paused').length;
                const stoppedContainers = Math.max(0, dockerContainers.length - runningContainers - pausedContainers);
                const lifecycle = getServerLifecycle(server);
                const canDrag = !serverSearchText.trim() && serverStatusFilter === 'all' && !isExpanded;
                const txTotal = getByteParts(server.info?.network?.tx_total);
                const rxTotal = getByteParts(server.info?.network?.rx_total);
                const chartLoading = !!server.metricsLoading && records.length === 0;
                const terminalProtocol = resolveTerminalProtocol(server);
                const effectiveTerminalProtocol = terminalProtocol || (hasSshEndpoint(server) ? 'ssh' : null);
                const terminalLabel = effectiveTerminalProtocol === 'agent' ? 'Agent 终端' : 'SSH 终端';
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
                
                return (
                  <ContextMenu.Root key={server.id}>
                    <ContextMenu.Trigger
                    draggable={canDrag}
                    onDragStart={(event) => handleServerDragStart(server, event)}
                    onDragOver={handleServerDragOver}
                    onDrop={(event) => handleServerDrop(server.id, event)}
                    onDragEnd={() => setDraggedServerId(null)}
                    className={`bg-kumo-base border rounded-lg transition-all duration-200 ${isExpanded ? 'border-kumo-brand/70  ring-1 ring-kumo-brand/20' : 'border-kumo-line/90  hover:border-kumo-interact '} ${draggedServerId === server.id ? 'opacity-50' : ''}`}
                  >
                    <div
                      onClick={() => toggleServerExpand(server.id)}
                      className="grid min-h-[56px] grid-cols-[minmax(0,1fr)_2.25rem] items-center gap-x-3 gap-y-2 px-3 py-2.5 cursor-pointer sm:flex sm:flex-nowrap sm:justify-between sm:gap-2.5 sm:py-2"
                    >
                      <div className="order-1 flex min-w-0 items-center gap-3">
                        <span className="relative flex h-2 w-2 rounded-full">
                          <span className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${server.status === 'online' ? 'bg-kumo-success' : 'bg-kumo-danger'}`}></span>
                          <span className={`relative inline-flex rounded-full h-2 w-2 ${server.status === 'online' ? 'bg-kumo-success' : 'bg-kumo-danger'}`}></span>
                        </span>
                        
                        <div className="flex flex-col min-w-0 gap-1">
                          <div className="flex items-center gap-2 min-w-0">
                            <i className={getOSIconClass(server.info?.platform)}></i>
                            <span
                              onDoubleClick={e => {
                                e.stopPropagation();
                                startRenameServer(server);
                              }}
                              className="text-xs font-bold text-kumo-strong truncate hover:text-kumo-brand"
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
                          </div>
                        </div>
                      </div>
                      
                      <div className="contents sm:order-2 sm:ml-auto sm:flex sm:shrink-0 sm:flex-nowrap sm:items-center sm:gap-2.5">
                        {server.status === 'online' && server.info && (
                          <div className="order-3 col-span-2 grid w-full grid-cols-5 gap-1.5 text-[10px] font-semibold text-kumo-subtle sm:order-none sm:col-span-1 sm:flex sm:h-9 sm:w-auto sm:items-center sm:gap-2.5">
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
                            <CompactMetricBar
                              label="剩余"
                              value={lifecycle.expiresAt ? `${Math.round(lifecycle.remainingPercent)}%` : '永久'}
                              valueClassName={lifecycle.toneClass}
                              barClassName={lifecycle.expired ? 'bg-kumo-danger' : lifecycle.remainingPercent <= 20 ? 'bg-kumo-warning' : 'bg-kumo-success'}
                              width={`${lifecycle.remainingPercent}%`}
                            />
                            {!hasGpuData && server.info.network && (
                              <div className="flex min-w-0 flex-col justify-center rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2 py-1 font-mono leading-[1.2] tabular-nums sm:hidden">
                                <span className="truncate text-kumo-info">&uarr; {tx.num}{tx.unit}</span>
                                <span className="truncate text-kumo-success">&darr; {rx.num}{rx.unit}</span>
                              </div>
                            )}
                            {server.info.network && (
                              <div className="hidden h-8 w-[126px] shrink-0 flex-col justify-center gap-px rounded-md border border-kumo-line bg-kumo-recessed/35 px-[5px] py-0 text-[10px] font-bold leading-[1.2] tabular-nums sm:flex">
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
                        
                        <div className="order-2 flex items-center justify-end gap-1.5 sm:order-none" onClick={e => e.stopPropagation()}>
                          <Button
                            shape="square" size="sm"
                            variant="secondary"
                            title={effectiveTerminalProtocol ? terminalLabel : '终端不可用'}
                            aria-label={effectiveTerminalProtocol ? terminalLabel : '终端不可用'}
                            icon={<TerminalIcon className="w-3.5 h-3.5" />}
                            onClick={() => openSSHTerminal(server)}
                            disabled={!canOpenTerminal(server) && !hasSshEndpoint(server)}
                            className="h-9 w-9 p-0 sm:h-8 sm:w-8"
                          />
                        </div>
                      </div>
                    </div>
                    
                    <AnimatedCollapse open={isExpanded} keepMounted>
                      <div className={`rounded-b-lg border-t border-kumo-line/90 bg-kumo-canvas/45 ${isDenseViewport ? 'p-1.5' : 'p-1.5 sm:p-2'}`}>
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
                              <div className="grid grid-cols-2 gap-1.5 lg:grid-cols-4">
                                <ExpandedProgressMetric
                                  label="CPU"
                                  value={cpuUsage}
                                  detail={`${Math.round(cpuUsage)}%`}
                                  caption={`${coreText}${cpuTemp > 0 ? ` · ${Math.round(cpuTemp)}°C` : ''}${cpuPower > 0 ? ` · ${cpuPower.toFixed(1)}W` : ''}`}
                                  indicatorClassName="!bg-none !bg-kumo-success"
                                  valueClassName="text-kumo-success"
                                />
                                <ExpandedProgressMetric
                                  label="内存"
                                  value={memUsage}
                                  detail={`${Math.round(memUsage)}%`}
                                  caption={`${server.info?.memory?.Used || '-'} / ${server.info?.memory?.Total || '-'}`}
                                  indicatorClassName="!bg-none !bg-kumo-info"
                                  valueClassName="text-kumo-info"
                                />
                                <ExpandedProgressMetric
                                  label="磁盘"
                                  value={diskUsage}
                                  detail={primaryDisk ? `${Math.round(diskUsage)}%` : '-'}
                                  caption={primaryDisk ? `${primaryDisk.used || '-'} / ${primaryDisk.total || '-'}` : '未上报'}
                                  indicatorClassName="!bg-none !bg-kumo-warning"
                                  valueClassName="text-kumo-warning"
                                />
                                <ExpandedProgressMetric
                                  label="剩余"
                                  value={lifecycle.remainingPercent}
                                  detail={lifecycle.label}
                                  caption={lifecycle.expiresAt ? `${formatDateTime(lifecycle.startsAt)} - ${formatDateTime(lifecycle.expiresAt)}` : '长期有效'}
                                  indicatorClassName={lifecycle.indicatorClassName}
                                  valueClassName={lifecycle.toneClass}
                                />
                              </div>
                            </ExpandedSection>

                            <div className="flex min-w-0 flex-col gap-2">
                              <div className={getExpandedInfoGridClassName(isDenseViewport)}>
                                <ExpandedSection title="系统概览" tone="success" className={getExpandedCardSpanClassName(0, 1)}>
                                  <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3 xl:grid-cols-4">
                                    <ExpandedInfoChip label="系统" value={server.info?.platform || '-' || server.info?.platformVersion || server.info?.system?.Kernel || '-'} />
                                    {/* <ExpandedInfoChip label="版本" value={server.info?.platformVersion || server.info?.system?.Kernel || '-'} /> */}
                                    <ExpandedInfoChip label="CPU 型号" value={server.info?.cpu?.Model || server.metadata?.cpu_model || server.metadata?.cpu_name || server.metadata?.processor || '-'} />
                                    <ExpandedInfoChip label="核心" value={coreText} />
                                    <ExpandedInfoChip label="GPU 型号" value={getGpuModelText(server.info?.gpu) || server.metadata?.gpu_model || server.metadata?.gpu_name || getGpuModelText(server.metadata?.gpu) || '-'}/>
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
                                    <ExpandedStatTile label="上传" value={server.info?.network?.tx_speed || '0 B/s'} caption={`累计 ${txTotal.text}`} tone="info" />
                                    <ExpandedStatTile label="下载" value={server.info?.network?.rx_speed || '0 B/s'} caption={`累计 ${rxTotal.text}`} tone="success" />
                                    <ExpandedInfoChip label="连接" value={server.info?.network?.connections || 0} />
                                    <ExpandedInfoChip label="总量" value={`↑ ${txTotal.text} / ↓ ${rxTotal.text}`} className="min-w-0" />
                                  </div>
                                </ExpandedSection>

                                <ExpandedTrendChartCard
                                  title="网络趋势"
                                  tone="info"
                                  className={getExpandedCardSpanClassName(1, 3)}
                                  legend={(
                                    <>
                                      <ChartLegend.SmallItem name="上行" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} />
                                      <ChartLegend.SmallItem name="下行" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} />
                                    </>
                                  )}
                                >
                                  {(tooltipBoundary) => (
                                    <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={expandedTrendChartHeight} />}>
                                      <TimeseriesChart
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
                                        ariaDescription={`${server.name} network upload and download speed trend`}
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
                                      <ChartLegend.SmallItem name="CPU" color={cpuColor} value={`${Math.round(cpuUsage)}%`} />
                                      <ChartLegend.SmallItem name="内存" color={memColor} value={`${Math.round(memUsage)}%`} />
                                      <ChartLegend.SmallItem name="温度" color={cpuTempColor} value={getLatestMetricValue(records, getCpuTemp, v => `${v.toFixed(1)}°C`)} />
                                    </>
                                  )}
                                >
                                  {(tooltipBoundary) => (
                                    <DeferredRender open={isExpanded} delay={SERVER_CHART_RENDER_DEFER_MS} fallback={<ChartWarmupSkeleton height={expandedTrendChartHeight} />}>
                                      <TimeseriesChart
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
                                        ariaDescription={`${server.name} CPU and memory usage trend`}
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
                                        <TimeseriesChart
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
                                          ariaDescription={`${server.name} GPU usage, VRAM, and power trend`}
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
                              <div className="overflow-hidden app-card">
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
                                    <Badge variant="success" appearance="dot">{runningContainers || server.info.docker.runningCount || 0} 运行</Badge>
                                    {pausedContainers > 0 && <Badge variant="warning" appearance="dot">{pausedContainers} 暂停</Badge>}
                                    {(stoppedContainers > 0 || server.info.docker.stoppedCount > 0) && <Badge variant="error" appearance="dot">{stoppedContainers || server.info.docker.stoppedCount} 停止</Badge>}
                                    {dockerExpanded ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                                  </span>
                                </Button>

                                <AnimatedCollapse open={dockerExpanded} keepMounted>
                                  {dockerContainers.length > 0 ? (
                                    <div className="grid grid-cols-1 divide-y divide-kumo-line xl:grid-cols-2 xl:divide-x xl:divide-y-0">
                                      {dockerContainers.map(c => {
                                        const state = getDockerContainerState(c);
                                        const stateBadge = getDockerStateBadge(state);
                                        const containerId = getDockerContainerId(c);
                                        const containerName = getDockerContainerName(c);
                                        const containerImage = getDockerContainerImage(c);
                                        const updateCheck = getDockerContainerUpdateCheck(server.id, c);
                                        const updateBadge = getDockerUpdateBadge(updateCheck);
                                        const updateChecking = isDockerContainerUpdateChecking(server.id, c);
                                        return (
                                          <div key={containerId || `${server.id}-${containerName}`} className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-3 py-2 text-xs hover:bg-kumo-recessed/20">
                                            <div className="flex min-w-0 items-center gap-2">
                                              <Badge variant={stateBadge.variant} appearance="dot" className="shrink-0">{stateBadge.label}</Badge>
                                              <div className="min-w-0">
                                                <div className="truncate font-semibold text-kumo-strong" title={containerName}>{containerName}</div>
                                                <div className="truncate font-mono text-[10px] text-kumo-subtle" title={containerImage}>{containerImage}</div>
                                                {(updateCheck || updateChecking) && (
                                                  <div className="mt-1">
                                                    {updateChecking ? (
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
                                                icon={state === 'running' ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                                                onClick={(event) => {
                                                  event.stopPropagation();
                                                  const action = state === 'running' ? 'container.pause' : state === 'paused' ? 'container.unpause' : 'container.start';
                                                  submitDockerTask(action, { serverId: server.id, containerId, containerName, image: containerImage });
                                                }}
                                              />
                                              <Button
                                                shape="square" size="sm"
                                                variant="secondary"
                                                aria-label="重启容器"
                                                title="重启"
                                                icon={<RotateCw className="h-3.5 w-3.5" />}
                                                onClick={(event) => {
                                                  event.stopPropagation();
                                                  submitDockerTask('container.restart', { serverId: server.id, containerId, containerName, image: containerImage });
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
                                                variant="primary"
                                                aria-label="一键更新容器"
                                                title="一键更新"
                                                icon={<Upload className="h-3.5 w-3.5" />}
                                                onClick={(event) => {
                                                  event.stopPropagation();
                                                  submitDockerTask('container.update', { serverId: server.id, containerId, containerName, image: containerImage });
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
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    </AnimatedCollapse>
                    </ContextMenu.Trigger>

                    <ContextMenu.Portal>
                      <ContextMenu.Positioner sideOffset={6}>
                        <ContextMenu.Popup className="z-50 min-w-40 overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5 text-kumo-default outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95">
                          <ContextMenu.Item
                            className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none focus:text-kumo-default focus:ring-kumo-focus/50 focus-visible:ring-2 focus-visible:ring-kumo-brand data-disabled:pointer-events-none data-disabled:opacity-50 data-highlighted:bg-kumo-overlay"
                            disabled={server.status !== 'online' || server.loading}
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
                            disabled={server.status !== 'online'}
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
                            disabled={server.status !== 'online'}
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
                  </ContextMenu.Root>
                );
              }))}
            </div>
          )}
        </div>
      )}
      
      {/* ==================== 2. 历史趋势 ==================== */}
      {serverCurrentTab === 'history' && (
        <div className="flex flex-col gap-4">
          <div className={SERVER_SECONDARY_BAR_CLASS}>
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                className={HOST_FILTER_TABS_CLASS}
                listClassName={HOST_FILTER_TABS_LIST_CLASS}
                indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                value={metricsHistoryTimeRange}
                onValueChange={setMetricsHistoryTimeRange}
                tabs={[
                  { value: '1h', label: '1h' },
                  { value: '6h', label: '6h' },
                  { value: '24h', label: '24h' },
                  { value: '7d', label: '7d' },
                ]}
              />
              
              <Button size="sm"
                variant="secondary"
                onClick={triggerManualCollect}
                className={`flex items-center gap-1 text-xs font-semibold ${HOST_TOOLBAR_BUTTON_CLASS}`}
              >
                立即采集
              </Button>
              
              <Button size="sm"
                variant="secondary-destructive"
                onClick={clearMetricsHistory}
                className={`flex items-center gap-1 text-kumo-danger font-semibold ${HOST_TOOLBAR_BUTTON_CLASS}`}
              >
                清空数据
              </Button>
            </div>
            
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">过滤主机</span>
              <Select
                aria-label="过滤主机" size="sm"
                value={metricsHistoryFilter.serverId}
                onValueChange={(value) => setMetricsHistoryFilter({ serverId: String(value) })}
                placeholder="全部主机"
                className={HOST_TOOLBAR_SELECT_CLASS}
                items={[
                  { value: '', label: '全部主机' },
                  ...serverList.map(s => ({ value: String(s.id), label: s.name || s.id })),
                ]}
              />
            </div>
          </div>
          
          {/* 采集器参数微型概览 */}
          {metricsCollectorStatus && (
            <div className="flex flex-wrap gap-4 text-xs font-semibold text-kumo-subtle bg-kumo-base border border-kumo-line p-3 rounded-lg">
              <div className="flex items-center gap-1.5">
                <span className={`w-2 h-2 rounded-full ${metricsCollectorStatus.isRunning ? 'bg-kumo-success' : 'bg-kumo-danger'}`}></span>
                <span>采集状态: {metricsCollectorStatus.isRunning ? '运行中' : '停止'}</span>
              </div>
              <div className="border-l border-kumo-line pl-3">
                <span>采集间隔: {metricsCollectInterval} 分钟</span>
              </div>
              <div className="border-l border-kumo-line pl-3">
                <span>最大保留天数: {monitorConfig.metrics_retention_days} 天</span>
              </div>
            </div>
          )}
          
          {/* 数据大列表表格 */}
          <div className="app-card overflow-hidden">
            {metricsHistoryLoading ? (
              <Table layout="fixed">
                <colgroup>
                  {historyColWidths.map((width, idx) => (
                    <col key={idx} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/45 border-b border-kumo-line font-bold text-kumo-strong">
                    <Table.Head className="p-3">记录时间</Table.Head>
                    <Table.Head className="p-3">主机</Table.Head>
                    <Table.Head className="p-3 text-center">CPU 使用率</Table.Head>
                    <Table.Head className="p-3 text-center">内存使用率</Table.Head>
                    <Table.Head className="p-3 text-center">磁盘使用率</Table.Head>
                    <Table.Head className="p-3">系统负载</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {[...Array(5)].map((_, idx) => (
                    <Table.Row key={idx} className="border-b border-kumo-line">
                      <Table.Cell className="p-3"><SkeletonLine className="w-32 h-4" /></Table.Cell>
                      <Table.Cell className="p-3"><SkeletonLine className="w-24 h-4" /></Table.Cell>
                      <Table.Cell className="p-3 text-center"><SkeletonLine className="w-12 h-4 mx-auto" /></Table.Cell>
                      <Table.Cell className="p-3 text-center"><SkeletonLine className="w-12 h-4 mx-auto" /></Table.Cell>
                      <Table.Cell className="p-3 text-center"><SkeletonLine className="w-12 h-4 mx-auto" /></Table.Cell>
                      <Table.Cell className="p-3"><SkeletonLine className="w-16 h-4" /></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            ) : metricsHistoryList.length === 0 ? (
              <div className="p-16 text-center text-xs text-kumo-subtle">
                暂无历史记录指标
              </div>
            ) : (
              <Table layout="fixed">
                <colgroup>
                  {historyColWidths.map((width, idx) => (
                    <col key={idx} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/45 border-b border-kumo-line font-bold text-kumo-strong">
                    <Table.Head className="p-3 relative">
                      记录时间
                      <Table.ResizeHandle onMouseDown={(e) => startHistoryResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="p-3 relative">
                      主机
                      <Table.ResizeHandle onMouseDown={(e) => startHistoryResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="p-3 text-center relative">
                      CPU 使用率
                      <Table.ResizeHandle onMouseDown={(e) => startHistoryResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="p-3 text-center relative">
                      内存使用率
                      <Table.ResizeHandle onMouseDown={(e) => startHistoryResize(3, e)} />
                    </Table.Head>
                    <Table.Head className="p-3 text-center relative">
                      磁盘使用率
                      <Table.ResizeHandle onMouseDown={(e) => startHistoryResize(4, e)} />
                    </Table.Head>
                    <Table.Head className="p-3 relative">
                      系统负载
                      <Table.ResizeHandle onMouseDown={(e) => startHistoryResize(5, e)} />
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {metricsHistoryList.map(rec => (
                    <Table.Row key={rec.id} className="border-b border-kumo-line hover:bg-kumo-recessed/15">
                      <Table.Cell className="p-3 font-semibold text-kumo-strong">{formatDateTime(rec.recorded_at)}</Table.Cell>
                      <Table.Cell className="p-3">{rec.server_name}</Table.Cell>
                      <Table.Cell className="p-3 text-center font-bold text-kumo-success">{rec.cpu_usage?.toFixed(1)}%</Table.Cell>
                      <Table.Cell className="p-3 text-center font-bold text-kumo-info">{rec.mem_usage?.toFixed(1)}%</Table.Cell>
                      <Table.Cell className="p-3 text-center">{rec.disk_usage?.toFixed(1)}%</Table.Cell>
                      <Table.Cell className="p-3"><code className="bg-kumo-recessed px-1.5 py-0.5 rounded font-mono text-[10px]">{rec.cpu_load || '-'}</code></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            )}
          </div>
        </div>
      )}
      
      {/* ==================== 3. Docker 控制台 ==================== */}
      {serverCurrentTab === 'docker' && (
        <div className="flex flex-col gap-4">
          <div className={SERVER_SECONDARY_BAR_CLASS}>
            <div className={SERVER_SECONDARY_TABS_GROUP_CLASS}>
              <Tabs
                {...TOOL_TABS_PROPS}
                className={HOST_FILTER_TABS_CLASS}
                listClassName={HOST_FILTER_TABS_LIST_CLASS}
                indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                value={dockerSubTab}
                onValueChange={setDockerSubTab}
                tabs={[
                  { value: 'containers', label: <span className="inline-flex items-center gap-1.5"><Box className="w-3.5 h-3.5" />容器</span> },
                  { value: 'compose', label: <span className="inline-flex items-center gap-1.5"><FolderOpen className="w-3.5 h-3.5" />Compose</span> },
                  { value: 'images', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="w-3.5 h-3.5" />镜像</span> },
                  { value: 'networks', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-3.5 h-3.5" />网络</span> },
                  { value: 'volumes', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="w-3.5 h-3.5" />存储卷</span> },
                  { value: 'stats', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />实时统计</span> },
                ]}
              />
            </div>
            
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">选择主机</span>
              <Select
                aria-label="选择 Docker 主机" size="sm"
                value={dockerSelectedServer}
                onValueChange={(value) => setDockerSelectedServer(String(value))}
                placeholder="全部 Docker 主机"
                className={HOST_TOOLBAR_SELECT_CLASS}
                items={[
                  { value: '', label: '全部 Docker 主机' },
                  ...dockerHostOptions,
                ]}
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
            </div>
          </div>
          
          {/* Docker 任务中心 */}
          {dockerTasks.length > 0 && (
            <div className="app-subcard bg-kumo-recessed p-3 rounded-lg text-xs font-mono text-kumo-default flex flex-col gap-1.5">
              <div className="flex justify-between border-b border-kumo-line pb-1.5 mb-1">
                <span className="inline-flex items-center gap-1.5 font-bold text-kumo-brand"><Activity className="h-3.5 w-3.5" />后台 Docker 任务流水</span>
                <span className="text-[10px] text-kumo-subtle">SSE 实时长连接</span>
              </div>
              <div className="max-h-24 overflow-y-auto flex flex-col gap-1">
                {dockerTasks.map(t => {
                  const progress = clampPercent(toNumber(t.progress, 0));
                  const showProgress = !['success', 'failed', 'timeout', 'cancelled'].includes(t.state) && progress > 0;
                  return (
                  <div key={t.taskId} className="flex flex-col gap-1">
                    <div className="flex justify-between gap-4">
                      <span className={t.state === 'success' ? 'text-kumo-success' : t.state === 'failed' ? 'text-kumo-danger' : 'text-kumo-warning'}>
                        [{t.state?.toUpperCase()}] {t.action}
                      </span>
                      <span className="text-kumo-subtle">{showProgress ? `${progress}% · ` : ''}{t.message}</span>
                    </div>
                    {showProgress && (
                      <Meter
                        label="Docker 任务进度"
                        value={progress}
                        showValue={false}
                        className="gap-0"
                        trackClassName="!h-1 overflow-hidden rounded-full bg-kumo-base"
                        indicatorClassName="!h-full !bg-none !bg-kumo-brand"
                      />
                    )}
                  </div>
                  );
                })}
              </div>
            </div>
          )}
          
          {/* 内容区域 */}
          {dockerResourceLoading ? (
            <div className="app-card overflow-hidden p-4">
              <div className="space-y-4">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="space-y-2">
                    <SkeletonLine className="w-1/4 h-5" />
                    <SkeletonLine className="w-full h-12 rounded-lg" />
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-4">
              {/* 1. 容器管理 */}
              {dockerSubTab === 'containers' && (
                <div className="flex flex-col gap-3">
                  {dockerOverviewServers.length === 0 ? (
                    renderDockerEmptyState('未检测到可用的 Docker 主机')
                  ) : visibleDockerContainerServers.length === 0 ? (
                    renderDockerEmptyState('当前筛选下没有容器')
                  ) : (
                    visibleDockerContainerServers.map(server => (
                      <div key={server.id} className="app-card overflow-hidden">
                        <div className="bg-kumo-recessed/35 p-3 border-b border-kumo-line flex items-center justify-between">
                          <span className="text-xs font-bold text-kumo-strong flex items-center gap-2">
                            <Box className="h-3.5 w-3.5" /> {server.name}
                          </span>
                          <span className="px-1.5 py-0.5 rounded text-[10px] bg-kumo-brand/10 text-kumo-brand font-bold">
                            {server.resources.containers.length} 个容器
                          </span>
                        </div>

                        <div className="p-2">
                          {server.resources.containers.length === 0 ? (
                            <div className="p-8 text-center text-xs text-kumo-subtle">
                              当前主机没有容器
                            </div>
                          ) : (
                          <ScrollableTable layout="fixed" widths={dockerColWidths}>
                            <colgroup>
                              {dockerColWidths.map((width, idx) => (
                                <col key={idx} style={{ width }} />
                              ))}
                            </colgroup>
                            <Table.Header>
                              <Table.Row className="border-b border-kumo-line text-kumo-subtle font-bold">
                                <Table.Head className="p-2 relative">
                                  名称
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(0, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 relative">
                                  镜像
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(1, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 text-center relative">
                                  更新
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(2, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 relative">
                                  状态
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(3, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 relative">
                                  端口映射
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(4, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 text-right relative">
                                  操作
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(5, e)} />
                                </Table.Head>
                              </Table.Row>
                            </Table.Header>
                            <Table.Body>
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
                                return (
                                <Table.Row key={containerId || `${server.id}-${containerName}`} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                                  <Table.Cell className="p-2 font-bold text-kumo-strong truncate" title={containerName}>{containerName}</Table.Cell>
                                  <Table.Cell className="p-2 truncate" title={containerImage}>{containerImage}</Table.Cell>
                                  <Table.Cell className="p-2 text-center">
                                    {updateChecking ? (
                                      <span className="inline-flex items-center justify-center gap-1 text-[10px] font-semibold text-kumo-subtle">
                                        <RefreshCw className="h-3 w-3 animate-spin" />
                                        检测中
                                      </span>
                                    ) : (
                                      <Badge variant={updateBadge.variant} appearance="dot" title={updateBadge.title}>
                                        {updateBadge.label}
                                      </Badge>
                                    )}
                                  </Table.Cell>
                                  <Table.Cell className="p-2">
                                    <Badge variant={stateBadge.variant} appearance="dot">{stateBadge.label}</Badge>
                                  </Table.Cell>
                                  <Table.Cell className="p-2 font-mono text-[11px] text-kumo-subtle truncate" title={containerPorts}>{containerPorts}</Table.Cell>
                                  <Table.Cell className="p-2 text-right">
                                    <div className="flex items-center justify-end gap-1.5">
                                      <Button
                                        shape="square" size="sm"
                                        variant={state === 'running' ? 'secondary-destructive' : 'secondary'}
                                        icon={state === 'running' ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                                        aria-label={state === 'running' ? '停止容器' : '启动容器'}
                                        onClick={() => submitDockerTask(toggleAction, { serverId: server.id, containerId, containerName, image: containerImage })}
                                        title={state === 'running' ? '停止' : '启动'}
                                      />
                                      <Button
                                        shape="square" size="sm"
                                        variant="secondary"
                                        icon={<RotateCw className="h-3.5 w-3.5" />}
                                        aria-label="重启容器"
                                        onClick={() => submitDockerTask('container.restart', { serverId: server.id, containerId, containerName, image: containerImage })}
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
                                        variant="primary"
                                        icon={<Upload className="h-3.5 w-3.5" />}
                                        aria-label="一键更新容器"
                                        onClick={() => submitDockerTask('container.update', { serverId: server.id, containerId, containerName, image: containerImage })}
                                        title="一键更新"
                                      />
                                    </div>
                                  </Table.Cell>
                                </Table.Row>
                                );
                              })}
                            </Table.Body>
                          </ScrollableTable>
                          )}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              )}
              
              {/* 2. Compose */}
              {dockerSubTab === 'compose' && (
                <div className="app-card overflow-hidden p-3.5">
                  {dockerComposeProjects.length === 0 ? (
                    <div className="p-12 text-center text-xs text-kumo-subtle">
                      当前主机中未检索到 Compose 项目
                    </div>
                  ) : (
                    <div className="flex flex-col gap-3">
                      {dockerComposeProjects.map(proj => {
                        const projectName = getComposeProjectName(proj);
                        const configFiles = getComposeConfigFiles(proj);
                        const status = getComposeStatus(proj);
                        const composePayload = { serverId: proj.serverId, project: projectName, configFile: configFiles };
                        return (
                          <div key={`${proj.serverId}-${projectName}-${configFiles}`} className="flex justify-between items-center p-3 border border-kumo-line rounded-lg bg-kumo-canvas/15 hover:border-kumo-brand/50">
                            <div className="flex flex-col gap-0.5 min-w-0">
                              <span className="text-xs font-bold text-kumo-strong truncate">{projectName}</span>
                              <span className="text-[10px] text-kumo-subtle truncate max-w-[400px]" title={configFiles}>{configFiles || '-'}</span>
                            </div>

                            <div className="flex items-center gap-3">
                              <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${status.includes('running') ? 'bg-kumo-success/15 text-kumo-success' : 'bg-kumo-danger/15 text-kumo-danger'}`}>
                                {status}
                              </span>
                              <div className="flex gap-1">
                                <Button size="sm"
                                  variant="primary"
                                  onClick={() => submitDockerTask('compose.up', composePayload)}
                                  className="text-kumo-inverse text-[10px] font-semibold"
                                >
                                  Up 启动
                                </Button>
                                <Button size="sm"
                                  variant="secondary"
                                  onClick={() => submitDockerTask('compose.down', composePayload)}
                                  className="text-kumo-subtle text-[10px]"
                                >
                                  Down 停止
                                </Button>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              )}

              {/* 3. 镜像管理 */}
              {dockerSubTab === 'images' && (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-end">
                    <Button
                      size="sm"
                      variant="secondary-destructive"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      disabled={!dockerSelectedServer}
                      title={dockerSelectedServer ? '清理未使用镜像' : '请先选择单台 Docker 主机'}
                      onClick={() => submitDockerTask('image.prune')}
                    >
                      清理未使用网络
                    </Button>
                  </div>
                  <div className="app-card overflow-hidden p-2">
                    {dockerImages.length === 0 ? (
                      <div className="p-12 text-center text-xs text-kumo-subtle">当前主机中未检索到镜像</div>
                    ) : (
                      <ScrollableTable layout="fixed" widths={imagesColWidths}>
                      <colgroup>
                        {imagesColWidths.map((width, idx) => (
                          <col key={idx} style={{ width }} />
                        ))}
                      </colgroup>
                      <Table.Header>
                        <Table.Row className="bg-kumo-recessed/25 border-b border-kumo-line font-bold">
                          <Table.Head className="p-2.5 relative">
                            镜像仓库
                            <Table.ResizeHandle onMouseDown={(e) => startImagesResize(0, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            标签
                            <Table.ResizeHandle onMouseDown={(e) => startImagesResize(1, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            大小
                            <Table.ResizeHandle onMouseDown={(e) => startImagesResize(2, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            所在主机
                            <Table.ResizeHandle onMouseDown={(e) => startImagesResize(3, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 text-right relative">
                            操作
                            <Table.ResizeHandle onMouseDown={(e) => startImagesResize(4, e)} />
                          </Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {dockerImages.map((img, i) => (
                          <Table.Row key={`${img.serverId}-${img.id}-${i}`} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                            <Table.Cell className="p-2.5 font-bold text-kumo-strong truncate">{img.repository}</Table.Cell>
                            <Table.Cell className="p-2.5"><span className="px-1.5 py-0.5 rounded bg-kumo-recessed font-mono text-[10px]">{img.tag}</span></Table.Cell>
                            <Table.Cell className="p-2.5 text-kumo-subtle truncate">{img.size}</Table.Cell>
                            <Table.Cell className="p-2.5 truncate">{img.serverName}</Table.Cell>
                            <Table.Cell className="p-2.5 text-right">
                              <Button
                                shape="square" size="sm"
                                variant="ghost"
                                aria-label="删除镜像"
                                onClick={() => submitDockerTask('image.remove', { serverId: img.serverId, image: img.id })}
                                className="text-kumo-danger"
                              >
                                <Trash className="h-3.5 w-3.5" />
                              </Button>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                      </ScrollableTable>
                    )}
                  </div>
                </div>
              )}

              {/* 4. 网络管理 */}
              {dockerSubTab === 'networks' && (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-end">
                    <Button
                      size="sm"
                      variant="secondary-destructive"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      disabled={!dockerSelectedServer}
                      title={dockerSelectedServer ? '清理未使用网络' : '请先选择单台 Docker 主机'}
                      onClick={() => submitDockerTask('network.prune')}
                    >
                      清理未使用网络
                    </Button>
                  </div>
                  <div className="app-card overflow-hidden p-2">
                    {dockerNetworks.length === 0 ? (
                      <div className="p-12 text-center text-xs text-kumo-subtle">当前主机中未检索到 Docker 网络</div>
                    ) : (
                      <ScrollableTable layout="fixed" widths={networksColWidths}>
                      <colgroup>
                        {networksColWidths.map((width, idx) => (
                          <col key={idx} style={{ width }} />
                        ))}
                      </colgroup>
                      <Table.Header>
                        <Table.Row className="bg-kumo-recessed/25 border-b border-kumo-line font-bold">
                          <Table.Head className="p-2.5 relative">
                            名称
                            <Table.ResizeHandle onMouseDown={(e) => startNetworksResize(0, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            ID
                            <Table.ResizeHandle onMouseDown={(e) => startNetworksResize(1, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            驱动
                            <Table.ResizeHandle onMouseDown={(e) => startNetworksResize(2, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            范围
                            <Table.ResizeHandle onMouseDown={(e) => startNetworksResize(3, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            所在主机
                            <Table.ResizeHandle onMouseDown={(e) => startNetworksResize(4, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 text-right relative">
                            操作
                            <Table.ResizeHandle onMouseDown={(e) => startNetworksResize(5, e)} />
                          </Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {dockerNetworks.map((network, i) => {
                          const isBuiltinNetwork = ['bridge', 'host', 'none'].includes(network.name);
                          return (
                            <Table.Row key={`${network.serverId}-${network.id || network.name}-${i}`} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                              <Table.Cell className="p-2.5 font-bold text-kumo-strong truncate">{network.name}</Table.Cell>
                              <Table.Cell className="p-2.5 font-mono text-[11px] text-kumo-subtle truncate">{network.id || '-'}</Table.Cell>
                              <Table.Cell className="p-2.5">{network.driver || '-'}</Table.Cell>
                              <Table.Cell className="p-2.5">{network.scope || '-'}</Table.Cell>
                              <Table.Cell className="p-2.5 truncate">{network.serverName}</Table.Cell>
                              <Table.Cell className="p-2.5 text-right">
                                <Button
                                  shape="square" size="sm"
                                  variant="ghost"
                                  aria-label="删除网络"
                                  disabled={isBuiltinNetwork}
                                  onClick={() => submitDockerTask('network.remove', { serverId: network.serverId, name: network.name })}
                                  className="text-kumo-danger disabled:opacity-40"
                                >
                                  <Trash className="h-3.5 w-3.5" />
                                </Button>
                              </Table.Cell>
                            </Table.Row>
                          );
                        })}
                      </Table.Body>
                      </ScrollableTable>
                    )}
                  </div>
                </div>
              )}

              {/* 5. 存储卷管理 */}
              {dockerSubTab === 'volumes' && (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-end">
                    <Button
                      size="sm"
                      variant="secondary-destructive"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      disabled={!dockerSelectedServer}
                      title={dockerSelectedServer ? '清理未使用存储卷' : '请先选择单台 Docker 主机'}
                      onClick={() => submitDockerTask('volume.prune')}
                    >
                      清理未使用网络
                    </Button>
                  </div>
                  <div className="app-card overflow-hidden p-2">
                    {dockerVolumes.length === 0 ? (
                      <div className="p-12 text-center text-xs text-kumo-subtle">当前主机中未检索到 Docker 存储卷</div>
                    ) : (
                      <ScrollableTable layout="fixed" widths={volumesColWidths}>
                      <colgroup>
                        {volumesColWidths.map((width, idx) => (
                          <col key={idx} style={{ width }} />
                        ))}
                      </colgroup>
                      <Table.Header>
                        <Table.Row className="bg-kumo-recessed/25 border-b border-kumo-line font-bold">
                          <Table.Head className="p-2.5 relative">
                            名称
                            <Table.ResizeHandle onMouseDown={(e) => startVolumesResize(0, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            驱动
                            <Table.ResizeHandle onMouseDown={(e) => startVolumesResize(1, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            范围
                            <Table.ResizeHandle onMouseDown={(e) => startVolumesResize(2, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            所在主机
                            <Table.ResizeHandle onMouseDown={(e) => startVolumesResize(3, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 text-right relative">
                            操作
                            <Table.ResizeHandle onMouseDown={(e) => startVolumesResize(4, e)} />
                          </Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {dockerVolumes.map((volume, i) => (
                          <Table.Row key={`${volume.serverId}-${volume.name}-${i}`} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                            <Table.Cell className="p-2.5 font-bold text-kumo-strong truncate">{volume.name}</Table.Cell>
                            <Table.Cell className="p-2.5">{volume.driver || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5">{volume.scope || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 truncate">{volume.serverName}</Table.Cell>
                            <Table.Cell className="p-2.5 text-right">
                              <Button
                                shape="square" size="sm"
                                variant="ghost"
                                aria-label="删除存储卷"
                                onClick={() => submitDockerTask('volume.remove', { serverId: volume.serverId, name: volume.name })}
                                className="text-kumo-danger"
                              >
                                <Trash className="h-3.5 w-3.5" />
                              </Button>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                      </ScrollableTable>
                    )}
                  </div>
                </div>
              )}

              {/* 6. 实时统计 */}
              {dockerSubTab === 'stats' && (
                <div className="app-card overflow-hidden p-2">
                  {dockerStats.length === 0 ? (
                    <div className="p-12 text-center text-xs text-kumo-subtle">当前主机中未检索到 Docker 资源统计</div>
                  ) : (
                    <div className="flex flex-col gap-3">
                      <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                        {dockerStats.map((stat, i) => {
                          const name = getDockerStatName(stat);
                          const cpuPercent = getDockerStatCpuPercent(stat);
                          const memPercent = getDockerStatMemPercent(stat);
                          return (
                            <div key={`${stat.serverId}-${stat.container_id || stat.ID || name}-${i}-visual`} className="rounded-md border border-kumo-line bg-kumo-recessed/20 p-3">
                              <div className="mb-2 flex min-w-0 items-center justify-between gap-2">
                                <span className="truncate text-xs font-bold text-kumo-strong" title={name}>{name}</span>
                                <span className="shrink-0 text-[10px] font-semibold text-kumo-subtle">{stat.serverName}</span>
                              </div>
                              <div className="grid gap-2">
                                <Meter
                                  label="CPU"
                                  value={cpuPercent}
                                  customValue={`${cpuPercent.toFixed(1)}%`}
                                  className="gap-1 text-[11px]"
                                  trackClassName="!h-1.5 overflow-hidden rounded-full bg-kumo-base"
                                  indicatorClassName="!h-full !bg-none !bg-kumo-success"
                                />
                                <Meter
                                  label="内存"
                                  value={memPercent}
                                  customValue={`${memPercent.toFixed(1)}% · ${getDockerStatMemUsage(stat)}`}
                                  className="gap-1 text-[11px]"
                                  trackClassName="!h-1.5 overflow-hidden rounded-full bg-kumo-base"
                                  indicatorClassName="!h-full !bg-none !bg-kumo-info"
                                />
                              </div>
                              <div className="mt-2 grid grid-cols-2 gap-2 text-[10px] text-kumo-subtle">
                                <span className="truncate font-mono" title={getDockerStatNetIo(stat)}>NET {getDockerStatNetIo(stat)}</span>
                                <span className="truncate text-right font-mono" title={getDockerStatBlockIo(stat)}>IO {getDockerStatBlockIo(stat)}</span>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                      <ScrollableTable layout="fixed" widths={statsColWidths}>
                      <colgroup>
                        {statsColWidths.map((width, idx) => (
                          <col key={idx} style={{ width }} />
                        ))}
                      </colgroup>
                      <Table.Header>
                        <Table.Row className="bg-kumo-recessed/25 border-b border-kumo-line font-bold">
                          <Table.Head className="p-2.5 relative">
                            容器
                            <Table.ResizeHandle onMouseDown={(e) => startStatsResize(0, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            CPU
                            <Table.ResizeHandle onMouseDown={(e) => startStatsResize(1, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            内存
                            <Table.ResizeHandle onMouseDown={(e) => startStatsResize(2, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            内存占比
                            <Table.ResizeHandle onMouseDown={(e) => startStatsResize(3, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            网络 IO
                            <Table.ResizeHandle onMouseDown={(e) => startStatsResize(4, e)} />
                          </Table.Head>
                          <Table.Head className="p-2.5 relative">
                            所在主机
                            <Table.ResizeHandle onMouseDown={(e) => startStatsResize(5, e)} />
                          </Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {dockerStats.map((stat, i) => (
                          <Table.Row key={`${stat.serverId}-${stat.container_id || stat.name}-${i}`} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                            <Table.Cell className="p-2.5 font-bold text-kumo-strong truncate" title={stat.container_id || stat.ID || getDockerStatName(stat)}>{getDockerStatName(stat)}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-kumo-success">{stat.cpu_percent || stat.CPUPerc || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-kumo-default">{getDockerStatMemUsage(stat)}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-kumo-info">{stat.mem_percent || stat.MemPerc || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-[11px] text-kumo-subtle truncate">{getDockerStatNetIo(stat)}</Table.Cell>
                            <Table.Cell className="p-2.5 truncate">{stat.serverName}</Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                      </ScrollableTable>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}
      
      {/* ==================== 4. 后台管理 ==================== */}
      {serverCurrentTab === 'management' && (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
            
            <LayerCard className={MANAGEMENT_CARD_CLASS}>
              <div className={MANAGEMENT_CARD_HEADER_CLASS}>
                <div className={MANAGEMENT_CARD_TITLE_CLASS}>
                  <Activity className={MANAGEMENT_CARD_ICON_CLASS} />
                  <span className="truncate">监控采集</span>
                </div>
                <span className="text-xs font-semibold text-kumo-subtle">{metricsCollectInterval}m</span>
              </div>
              <div className={`${MANAGEMENT_CARD_BODY_CLASS} flex flex-col gap-3`}>
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-semibold text-kumo-subtle">采集间隔</span>
                  <Tabs
                    {...TOOL_TABS_PROPS}
                    className={HOST_FILTER_TABS_CLASS}
                    listClassName={HOST_FILTER_TABS_LIST_CLASS}
                    indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                    value={String(metricsCollectInterval)}
                    onValueChange={(value) => updateMetricsCollectInterval(Number(value))}
                    tabs={METRICS_COLLECT_INTERVAL_TABS}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-xs font-semibold text-kumo-subtle">保留天数</span>
                  <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_112px] sm:items-center">
                    <Tabs
                      {...TOOL_TABS_PROPS}
                      className={HOST_FILTER_TABS_CLASS}
                      listClassName={HOST_FILTER_TABS_LIST_CLASS}
                      indicatorClassName={HOST_FILTER_TABS_INDICATOR_CLASS}
                      value={String(monitorConfig.metrics_retention_days)}
                      onValueChange={updateMetricsRetentionDays}
                      tabs={METRICS_RETENTION_TABS}
                    />
                    <Input
                      size="sm"
                      aria-label="历史数据保留天数"
                      type="number"
                      min="1"
                      max="180"
                      value={monitorConfig.metrics_retention_days}
                      onChange={(event) => updateMetricsRetentionDays(event.target.value)}
                    />
                  </div>
                </div>
              </div>
            </LayerCard>
            
            <LayerCard className={MANAGEMENT_CARD_CLASS}>
              <div className={MANAGEMENT_CARD_HEADER_CLASS}>
                <div className={MANAGEMENT_CARD_TITLE_CLASS}>
                  <FolderOpen className={MANAGEMENT_CARD_ICON_CLASS} />
                  <span className="truncate">批量录入</span>
                </div>
                <span className="text-xs font-semibold text-kumo-subtle">CSV</span>
              </div>
              <div className={`${MANAGEMENT_CARD_BODY_CLASS} flex flex-col gap-2`}>
                <Textarea
                  label="主机列表"
                  aria-label="批量快速添加主机"
                  value={serverBatchText}
                  onChange={e => setServerBatchText(e.target.value)}
                  placeholder="名称,IP,端口,用户名,密码\n例如: prod-server,192.168.1.10,22,root,password"
                  className="h-24 font-mono text-xs"
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
              </div>
            </LayerCard>
          </div>
          
          <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
            
            <LayerCard className={MANAGEMENT_CARD_CLASS}>
              <div className={MANAGEMENT_CARD_HEADER_CLASS}>
                <div className={MANAGEMENT_CARD_TITLE_CLASS}>
                  <Key className={MANAGEMENT_CARD_ICON_CLASS} />
                  <span className="truncate">SSH 凭据库</span>
                </div>
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => setShowAddCredentialModal(true)}
                  icon={<Plus className="h-3.5 w-3.5" />}
                >
                  添加
                </Button>
              </div>
              <div className="p-0">
                {serverCredentials.length === 0 ? (
                  <div className="px-3 py-8 text-center text-xs text-kumo-subtle">暂无预设访问凭据</div>
                ) : (
                  <div className="max-h-64 overflow-auto">
                    <Table layout="fixed">
                      <colgroup>
                        <col />
                        <col style={{ width: 120 }} />
                        <col style={{ width: 86 }} />
                      </colgroup>
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head>名称</Table.Head>
                          <Table.Head>用户</Table.Head>
                          <Table.Head className="text-right">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {serverCredentials.map(cred => (
                          <Table.Row key={cred.id} className="border-b border-kumo-line/80 hover:bg-kumo-recessed/10">
                            <Table.Cell className="whitespace-nowrap">
                              <div className="flex min-w-0 items-center gap-2">
                                <span className="truncate font-semibold text-kumo-strong" title={cred.name}>{cred.name}</span>
                                {cred.is_default && <Badge variant="success" appearance="dot">默认</Badge>}
                              </div>
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap font-mono text-[11px] text-kumo-subtle" title={cred.username}>
                              {cred.username}
                            </Table.Cell>
                            <Table.Cell className="whitespace-nowrap text-right">
                              <div className="inline-flex items-center justify-end gap-1">
                                {!cred.is_default && (
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="ghost"
                                    aria-label="设为默认凭据"
                                    onClick={() => setDefaultCredential(cred.id)}
                                    icon={<Star className="h-3.5 w-3.5" />}
                                    title="设为默认"
                                  />
                                )}
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="ghost"
                                  aria-label="删除凭据"
                                  onClick={() => deleteCredential(cred.id)}
                                  icon={<Trash className="h-3.5 w-3.5" />}
                                  title="删除"
                                  className="text-kumo-danger"
                                />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  </div>
                )}
              </div>
            </LayerCard>
            
            <LayerCard className={MANAGEMENT_CARD_CLASS}>
              <div className={MANAGEMENT_CARD_HEADER_CLASS}>
                <div className={MANAGEMENT_CARD_TITLE_CLASS}>
                  <Database className={MANAGEMENT_CARD_ICON_CLASS} />
                  <span className="truncate">配置迁移</span>
                </div>
                <span className="text-xs font-semibold text-kumo-subtle">JSON</span>
              </div>
              <div className={`${MANAGEMENT_CARD_BODY_CLASS} flex flex-wrap items-center gap-2`}>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={exportServers}
                  icon={<Download className="h-3.5 w-3.5" />}
                >
                  导出备份
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={openImportServerModal}
                  icon={<Upload className="h-3.5 w-3.5" />}
                >
                  导入配置
                </Button>
              </div>
            </LayerCard>
          </div>
        </div>
      )}
      
      {/* ==================== 5. SSH 终端 (多分屏支持) ==================== */}
      {serverCurrentTab === 'terminal' && (() => {
        const activeSession = sshSessions.find(s => s.id === activeSSHSessionId);
        const activeServer = activeSession?.server;
        const activeInfo = activeServer?.info || {};
        const isTerminalDarkMode = theme === 'dark';
        const terminalCpuColor = ChartPalette.semantic('Success', isTerminalDarkMode);
        const terminalMemColor = ChartPalette.categorical(0, isTerminalDarkMode);
        const terminalDiskColor = ChartPalette.semantic('Warning', isTerminalDarkMode);
        const terminalGpuColor = ChartPalette.categorical(1, isTerminalDarkMode);
        const resourceMetrics = [
          {
            label: 'CPU',
            value: activeInfo.cpu?.Usage || '-',
            width: activeInfo.cpu?.Usage || '0%',
            valueClassName: 'text-kumo-success',
            barClassName: 'bg-kumo-success',
            color: terminalCpuColor,
          },
          {
            label: 'Mem',
            value: activeInfo.memory?.Usage || '-',
            width: activeInfo.memory?.Usage || '0%',
            valueClassName: 'text-kumo-info',
            barClassName: 'bg-kumo-info',
            color: terminalMemColor,
          },
          {
            label: 'Disk',
            value: activeInfo.disk?.[0]?.usage || '-',
            width: activeInfo.disk?.[0]?.usage || '0%',
            valueClassName: 'text-kumo-warning',
            barClassName: 'bg-kumo-warning',
            color: terminalDiskColor,
          },
          {
            label: 'GPU',
            value: activeInfo.gpu?.Usage || '-',
            width: activeInfo.gpu?.Usage || '0%',
            valueClassName: 'text-kumo-warning',
            barClassName: 'bg-kumo-warning',
            color: terminalGpuColor,
          },
        ];
        return (
          <div className="flex min-h-0 w-full flex-1 flex-col overflow-hidden app-card">
            <div className="flex min-h-11 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-3 py-2 text-xs">
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
                    className={`flex h-7 shrink-0 items-center rounded-md border ${
                      activeSSHSessionId === sess.id
                        ? 'border-kumo-brand/60 bg-kumo-recessed text-kumo-strong'
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

            <div className="flex min-h-0 flex-1">
              <div className="flex min-w-0 flex-1 flex-col">
                <div className="flex min-h-0 flex-1">
                  <div
                    className={`grid min-w-0 flex-1 gap-1.5 bg-kumo-recessed p-1.5 ${
                      sshViewLayout === 'split-h' ? 'grid-cols-2' :
                      sshViewLayout === 'split-v' ? 'grid-rows-2' :
                      sshViewLayout === 'grid' ? 'grid-cols-2 grid-rows-2' : 'grid-cols-1'
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
                          className={`relative flex h-full w-full flex-col overflow-hidden rounded-md border bg-kumo-base ${
                            activeSSHSessionId === id ? 'border-kumo-interact' : 'border-kumo-line'
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
                              <span className="truncate">{slotSession?.name || 'Terminal'}</span>
                            </Button>
                            <div className="flex items-center gap-1">
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => triggerSplitPane(id, 'right')}
                                title="左右分屏"
                              >
                                Split
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
                              className="pointer-events-none absolute z-20 rounded-md border border-dashed border-kumo-brand bg-kumo-brand/10 ring-1 ring-kumo-brand/25"
                              style={getTerminalDropPreviewStyle(dropHint)}
                            />
                          )}
                        </div>
                      );
                    })}
                  </div>

                  {activeTerminalSidebar && (
                  <div className="w-[clamp(18rem,24vw,26rem)] shrink-0 border-l border-kumo-line bg-kumo-base">
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
                          <div className="mb-2.5 min-w-0 rounded-md border border-kumo-line bg-kumo-recessed/25 p-2">
                            <div className="truncate text-[11px] font-semibold text-kumo-strong">{activeSession?.name || '-'}</div>
                            <div className="mt-1 truncate font-mono text-[10px] text-kumo-subtle">{activeServer?.host || 'Agent'}</div>
                          </div>
                          <div className="flex flex-col gap-2.5">
                            {resourceMetrics.map(metric => {
                              const label = metric.label === 'Mem' ? 'Memory' : metric.label;
                              return (
                                <div key={metric.label} className="min-w-0">
                                  <div className="mb-1 flex items-center justify-between gap-2 text-[10px]">
                                    <span className="font-semibold text-kumo-subtle">{label}</span>
                                    <span className={`font-mono font-bold ${metric.color ? '' : metric.valueClassName}`} style={metric.color ? { color: metric.color } : undefined}>{metric.value}</span>
                                  </div>
                                  <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line bg-kumo-recessed">
                                    <div className={`h-full ${metric.color ? '' : metric.barClassName}`} style={{ width: metric.width, backgroundColor: metric.color || undefined }} />
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                          {activeInfo.network && (
                            <div className="mt-3 flex flex-col gap-2 border-t border-kumo-line pt-3 text-[10px]">
                              <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 p-2">
                                <div className="text-kumo-subtle">Upload</div>
                                <div className="mt-1 truncate font-mono font-semibold text-kumo-info">{activeInfo.network.tx_speed || '-'}</div>
                              </div>
                              <div className="rounded-md border border-kumo-line bg-kumo-recessed/30 p-2">
                                <div className="text-kumo-subtle">Download</div>
                                <div className="mt-1 truncate font-mono font-semibold text-kumo-success">{activeInfo.network.rx_speed || '-'}</div>
                              </div>
                            </div>
                          )}
                        </div>
                      )}
                      {showSftpSidebar && (
                        <div className="h-full min-h-0">
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
                        <div className="h-full min-h-0">
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
          </div>
        );
      })()}
      
      {/* ==================== xterm.js 实例静默挂载的仓库 ==================== */}
      <div ref={warehouseRef} className="hidden absolute -top-[9999px]" id="ssh-terminal-warehouse"></div>
      
      {/* ==================== 模态框: 添加与编辑服务器 ==================== */}
      <Dialog.Root open={showServerModal} onOpenChange={setShowServerModal}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 sm:min-w-[32rem] sm:max-w-[calc(100vw-3rem)]">
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
            
      <div className="min-w-0 p-4 flex-1 overflow-y-auto flex flex-col gap-4 text-xs">
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
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
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
                  <label className="font-semibold text-kumo-subtle">地区 / 归属国家 (Flags)</label>
                  <Select size="sm"
                    aria-label="地区归属国家"
                    value={serverForm.country}
                    onValueChange={(value) => setServerForm(prev => ({ ...prev, country: String(value) }))}
                    className="px-3 py-2"
                    items={[
                      { value: 'auto', label: '自动探测' },
                      { value: 'CN', label: '中国 (CN)' },
                      { value: 'US', label: '美国 (US)' },
                      { value: 'HK', label: '香港 (HK)' },
                      { value: 'JP', label: '日本 (JP)' },
                      { value: 'SG', label: '新加坡 (SG)' },
                    ]}
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

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <div className="flex flex-col gap-1.5 sm:col-span-2">
                  <label className="font-semibold text-kumo-subtle">连接地址 (IP / Host)</label>
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

              <div className="flex flex-col gap-2">
                <label className="font-semibold text-kumo-subtle">选择凭据预设进行快速填充</label>
                <Select size="sm"
                  aria-label="选择凭据预设"
                  value={selectedCredentialId}
                  onValueChange={applyCredential}
                  placeholder="-- 手动录入 --"
                  className="w-full min-w-0 px-3 py-2"
                  items={[
                    { value: '', label: '-- 手动录入 --' },
                    ...serverCredentials.map(c => ({
                      value: String(c.id),
                      label: `${c.name} (${c.username})`,
                    })),
                  ]}
                />
              </div>
              
              <div className="border-t border-kumo-line pt-3 flex flex-col gap-3">
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
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
                    <div className="flex flex-wrap gap-2 py-1">
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
                        秘钥证书
                      </Button>
                    </div>
                  </div>
                </div>
                
                {serverForm.authType === 'password' ? (
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">连接密码</label>
                    <Input size="sm"
                      aria-label="连接密码"
                      type="password"
                      value={serverForm.password}
                      onChange={e => setServerForm(prev => ({ ...prev, password: e.target.value }))}
                      placeholder={serverModalMode === 'edit' ? '****** (留空不修改)' : '登录密码'}
                      className="px-3 py-2 text-kumo-strong"
                    />
                  </div>
                ) : (
                  <div className="flex flex-col gap-3">
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">证书密钥 (Private Key)</label>
                      <Textarea
                        aria-label="证书密钥"
                        value={serverForm.privateKey}
                        onChange={e => setServerForm(prev => ({ ...prev, privateKey: e.target.value }))}
                        placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                        className="w-full h-20 p-2 text-xs font-mono"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="font-semibold text-kumo-subtle">密钥口令 (密码保护短语，若有)</label>
                      <Input size="sm"
                        aria-label="密钥口令"
                        type="password"
                        value={serverForm.passphrase}
                        onChange={e => setServerForm(prev => ({ ...prev, passphrase: e.target.value }))}
                        placeholder="Key Passphrase"
                        className="px-3 py-2 text-kumo-strong"
                      />
                    </div>
                  </div>
                )}
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle">自定义主机标签 (逗号分隔)</label>
                <Input size="sm"
                  aria-label="自定义主机标签"
                  type="text"
                  value={serverForm.tagsInput}
                  onChange={e => setServerForm(prev => ({ ...prev, tagsInput: e.target.value }))}
                  placeholder="Production,Database,US"
                  className="px-3 py-2 text-kumo-strong"
                />
              </div>
              
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
                        container={serverModalPortalRef}
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
                        <span className="font-semibold text-kumo-strong">执行环境提示</span>
                        {getAgentInstallExecutionHint(agentInstallOS)}
                      </div>
                      <div className="grid grid-cols-1 gap-2 text-[11px] text-kumo-subtle sm:grid-cols-2">
                        <div className="app-card app-card-md p-2">
                          <div className="font-semibold text-kumo-strong">主机 ID</div>
                          <div className="mt-1 font-mono">{quickDeployResult.serverId}</div>
                        </div>
                        <div className="app-card app-card-md p-2">
                          <div className="font-semibold text-kumo-strong">API 地址</div>
                          <div className="mt-1 truncate font-mono" title={quickDeployResult.apiUrl}>{quickDeployResult.apiUrl}</div>
                        </div>
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
            
            <div className="flex flex-col-reverse gap-2.5 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 sm:flex-row sm:justify-end">
              {serverModalMode === 'add' && serverAddMode === 'agent' ? (
                <>
                  <Button
                    type="button" size="sm"
                    variant="primary"
                    loading={serverModalSaving}
                    onClick={generateQuickInstallCommand}
                    className="w-full sm:w-auto"
                  >
                    生成 Agent 安装命令
                  </Button>
                </>
              ) : null}
              <Button size="sm"
                variant="secondary"
                onClick={testServerConnection}
                disabled={serverModalSaving}
                className={`w-full px-3.5 py-1.5 text-xs font-semibold sm:w-auto ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                连接测试
              </Button>
              <Button size="sm"
                variant="primary"
                onClick={saveServer}
                disabled={serverModalSaving}
                className={`w-full px-4 py-1.5 text-kumo-inverse text-xs font-bold sm:w-auto ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                {serverModalSaving ? '保存中...' : '确认保存'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
      
      {/* ==================== 模态框: 凭据预设新增 ==================== */}
      <Dialog.Root open={showAddCredentialModal} onOpenChange={setShowAddCredentialModal}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 sm:min-w-96 sm:max-w-[calc(100vw-3rem)]">
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
                    type="password"
                    value={credForm.password}
                    onChange={e => setCredForm(prev => ({ ...prev, password: e.target.value }))}
                    placeholder="输入密码"
                    className="px-3 py-2 text-kumo-strong"
                  />
                </div>
              ) : (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">PEM 私钥证书内容</label>
                    <Textarea
                      aria-label="PEM 私钥证书内容"
                      value={credForm.private_key}
                      onChange={e => setCredForm(prev => ({ ...prev, private_key: e.target.value }))}
                      placeholder="-----BEGIN RSA PRIVATE KEY-----"
                      className="w-full h-24 p-2 text-xs font-mono"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">证书保护密码短语 (口令)</label>
                    <Input size="sm"
                      aria-label="证书保护密码短语"
                      type="password"
                      value={credForm.passphrase}
                      onChange={e => setCredForm(prev => ({ ...prev, passphrase: e.target.value }))}
                    placeholder="Passphrase"
                    className="px-3 py-2 text-kumo-strong"
                  />
                  </div>
                </div>
              )}
            </div>
            
            <div className="flex flex-col-reverse gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 text-xs sm:flex-row sm:justify-end">
              <Button size="sm" variant="secondary" onClick={() => setShowAddCredentialModal(false)} className="w-full sm:w-auto">取消</Button>
              <Button size="sm" variant="primary" onClick={addCredential} className="w-full text-kumo-inverse font-bold sm:w-auto">确认保存</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
      
      {/* ==================== 模态框: 导入主机备份 ==================== */}
      <Dialog.Root open={showImportServerModal} onOpenChange={setShowImportServerModal}>
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 sm:min-w-96 sm:max-w-[calc(100vw-3rem)]">
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
                  ✓ 识别就绪：检测到 {importPreview.length} 个有效的服务器拓扑信息，确认开始恢复？
                </div>
              )}
              
              {importModalError && (
                <div className="text-xs text-kumo-danger font-bold bg-kumo-danger/10 border border-kumo-danger/20 p-2.5 rounded">
                  {importModalError}
                </div>
              )}
            </div>
            
            <div className="flex flex-col-reverse gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 text-xs sm:flex-row sm:justify-end">
              <Button size="sm" variant="secondary" onClick={() => setShowImportServerModal(false)} className="w-full sm:w-auto">取消</Button>
              <Button size="sm"
                variant="primary"
                onClick={confirmImportServers}
                disabled={importModalSaving || !importPreview}
                className="w-full text-kumo-inverse font-bold disabled:opacity-50 sm:w-auto"
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
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 sm:min-w-[32rem] sm:max-w-[calc(100vw-3rem)]">
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
                <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
                    <div className="font-semibold text-kumo-subtle">安装命令</div>
                    <div className="grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-3">
                      <Tabs
                        {...TOOL_TABS_PROPS}
                        className="w-fit max-w-full"
                        listClassName="w-fit max-w-full"
                        value={agentInstallProtocol}
                        onValueChange={setAgentInstallProtocol}
                        tabs={[
                          { value: 'https', label: 'HTTPS' },
                          { value: 'http', label: 'HTTP' },
                        ]}
                      />
                      <Tabs
                        {...TOOL_TABS_PROPS}
                        className="w-fit max-w-full"
                        listClassName="w-fit max-w-full"
                        value={agentInstallHostType}
                        onValueChange={setAgentInstallHostType}
                        tabs={[
                          { value: 'domain', label: '域名' },
                          { value: 'ip', label: 'IP' },
                        ]}
                      />
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
                    <span className="font-semibold text-kumo-strong">执行环境提示</span>
                    {getAgentInstallExecutionHint(agentInstallOS)}
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  <LinkButton size="sm"
                    variant="secondary"
                    href={`${getAgentBaseApiUrl() || agentModalData.apiUrl}/agent/agent-linux-amd64`}
                    target="_blank"
                    rel="noreferrer"
                    external
                    icon={<Download className="h-3.5 w-3.5" />}
                    className="w-full justify-center"
                  >
                    Linux x64
                  </LinkButton>
                  <LinkButton size="sm"
                    variant="secondary"
                    href={`${getAgentBaseApiUrl() || agentModalData.apiUrl}/agent/agent-windows-amd64.exe`}
                    target="_blank"
                    rel="noreferrer"
                    external
                    icon={<Download className="h-3.5 w-3.5" />}
                    className="w-full justify-center"
                  >
                    Windows x64
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
                  <Textarea
                    label="安装日志"
                    value={agentInstallLog}
                    readOnly
                    className={`agent-command-textarea min-h-40 resize-none font-mono text-[11px] leading-5 ${
                      agentInstallResult === 'success'
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

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
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
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-end sm:gap-3">
              <Checkbox
                label="强制 SSH 覆盖"
                checked={agentForceSsh}
                disabled={agentInstallLoading || agentInstalling}
                onCheckedChange={(checked) => setAgentForceSsh(Boolean(checked))}
              />
              <Button
                type="button" size="sm"
                variant="secondary"
                onClick={() => setShowAgentModal(false)}
                className="w-full sm:w-auto"
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
                className="w-full sm:w-auto"
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
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 sm:min-w-[48rem] sm:max-w-[calc(100vw-3rem)]">
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

              <div className="grid grid-cols-1 gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3 sm:grid-cols-2 lg:grid-cols-3">
                {serverList.map(server => (
                  <div key={server.id} className="app-card app-card-md p-2">
                    <Checkbox
                      label={
                        <span className="inline-flex min-w-0 items-center gap-1.5">
                          {getFlagCountry(server) && (
                            <CountryFlag countryCode={getFlagCountry(server)} className="h-3 w-4 text-xs" />
                          )}
                          <span className="truncate font-semibold text-kumo-strong">{server.name}</span>
                        </span>
                      }
                      checked={selectedBatchServers.includes(server.id)}
                      disabled={agentInstallLoading}
                      onCheckedChange={(checked) => {
                        setSelectedBatchServers(prev => {
                          if (checked) return prev.includes(server.id) ? prev : [...prev, server.id];
                          return prev.filter(id => id !== server.id);
                        });
                      }}
                    />
                  </div>
                ))}
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
                        <span className={`shrink-0 rounded px-2 py-0.5 text-[11px] font-semibold ${
                          result.status === 'success'
                            ? 'bg-kumo-success/10 text-kumo-success'
                            : result.status === 'failed'
                              ? 'bg-kumo-danger/10 text-kumo-danger'
                              : result.status === 'verifying'
                                ? 'bg-kumo-warning/10 text-kumo-warning'
                                : result.status === 'processing'
                                  ? 'bg-kumo-brand/10 text-kumo-brand'
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

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-wrap items-center gap-3 text-xs text-kumo-subtle">
              <Checkbox
                label="强制 SSH 覆盖"
                checked={batchAgentForceSsh}
                disabled={agentInstallLoading}
                onCheckedChange={(checked) => setBatchAgentForceSsh(Boolean(checked))}
              />
              {agentInstallLoading && <span>任务执行中</span>}
            </div>
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <Button
                type="button" size="sm"
                variant="secondary"
                disabled={agentInstallLoading}
                onClick={() => setShowBatchAgentModal(false)}
                className="w-full sm:w-auto"
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
                className="w-full sm:w-auto"
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
        <Dialog size="sm" className="flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 sm:min-w-[32rem] sm:max-w-[calc(100vw-3rem)]">
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
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">目标 Agent</div>
                  <div className="mt-1 text-lg font-bold text-kumo-strong">{getAgentUpgradeTargets().length}</div>
                </div>
                <div className="rounded-md border border-kumo-line bg-kumo-recessed/35 p-3">
                  <div className="text-[11px] font-medium text-kumo-subtle">状态</div>
                  <div className="mt-1 font-semibold text-kumo-strong">{upgrading ? '执行中' : upgradeProgress >= 100 ? '已完成' : '待执行'}</div>
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

              {upgradeLog ? (
                <Textarea
                  label="升级日志"
                  value={upgradeLog}
                  readOnly
                  className="min-h-56 font-mono text-[11px]"
                />
              ) : (
                <div className="app-card app-card-md p-3 text-kumo-subtle">
                  将对在线 Agent 或 Agent 模式主机下发升级任务。
                </div>
              )}
            </div>
          </div>

          <div className="flex flex-col gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex flex-wrap items-center gap-3">
              <Checkbox
                label="强制覆盖"
                checked={forceUpgrade}
                disabled={upgrading}
                onCheckedChange={(checked) => setForceUpgrade(Boolean(checked))}
              />
              <Checkbox
                label="SSH 保底"
                checked={upgradeFallbackSsh}
                disabled={upgrading}
                onCheckedChange={(checked) => setUpgradeFallbackSsh(Boolean(checked))}
              />
            </div>
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <Button
                type="button" size="sm"
                variant="secondary"
                onClick={() => setShowUpgradeModal(false)}
                className="w-full sm:w-auto"
              >
                {upgrading ? '后台运行' : '关闭'}
              </Button>
              <Button
                type="button" size="sm"
                variant="primary"
                icon={<Upload className="h-3.5 w-3.5" />}
                loading={upgrading}
                disabled={getAgentUpgradeTargets().length === 0}
                onClick={performOneKeyUpgrade}
                className="w-full sm:w-auto"
              >
                开始升级
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

    </div>
  );
}

export default ServerPage;


