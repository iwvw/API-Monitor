import React, { useState, useEffect, useRef, useMemo } from 'react';
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
import { ChartLegend, ChartPalette, ClipboardText, Meter, Tabs, TimeseriesChart } from '@cloudflare/kumo';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AnimatedCollapse, DeferredRender } from '../components/AnimatedCollapse.jsx';
import useTableResize from '../composables/useTableResize.js';
import { formatUptime, formatFileSize, formatDateTime, maskAddress, parseSpeed } from '../modules/utils.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { canOpenTerminal, isAgentServer, resolveTerminalProtocol } from '../modules/serverTerminal.js';
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
import { io } from 'socket.io-client';
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

const getTableMinWidth = (widths) => widths.reduce((total, width) => total + (Number(width) || 0), 0);

function ScrollableTable({ widths, style, ...props }) {
  return (
    <div className="overflow-x-auto scrollbar-thin">
      <Table
        {...props}
        style={{ minWidth: getTableMinWidth(widths), ...style }}
      />
    </div>
  );
}

function ChartBoundaryBox({ className = '', children }) {
  const [boundary, setBoundary] = useState(null);
  return (
    <div ref={setBoundary} className={className}>
      {typeof children === 'function' ? children(boundary) : children}
    </div>
  );
}

function ChartWarmupSkeleton({ height = 130 }) {
  return (
    <div
      aria-hidden="true"
      className="flex flex-col justify-end gap-2 overflow-hidden rounded-md border border-kumo-line/70 bg-kumo-recessed/35 p-3"
      style={{ height }}
    >
      <SkeletonLine className="h-3 w-1/3" />
      <SkeletonLine className="h-16 w-full rounded" />
      <div className="grid grid-cols-4 gap-2">
        <SkeletonLine className="h-2 w-full" />
        <SkeletonLine className="h-2 w-full" />
        <SkeletonLine className="h-2 w-full" />
        <SkeletonLine className="h-2 w-full" />
      </div>
    </div>
  );
}

// OS 平台图标及颜色计算
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
  if (p.includes('windows')) return `fab fa-windows ${baseClass} text-[#0078D4]`;
  if (p.includes('darwin') || p.includes('mac')) return `si si-apple si--color ${baseClass}`;
  return `si si-linux si--color ${baseClass}`;
};

function CompactMetricBar({ label, value, valueClassName, barClassName, color, width = '0%' }) {
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

const getFlagCountry = (server) => {
  if (server.country && server.country !== 'auto') {
    return server.country;
  }
  return server.resolved_country || server.info?.resolved_country || '';
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

const toTimestamp = (value, fallback) => {
  if (typeof value === 'number') {
    return value > 100000000000 ? value : value * 1000;
  }
  const parsed = value ? new Date(value).getTime() : NaN;
  return Number.isFinite(parsed) ? parsed : fallback;
};

const SERVER_REALTIME_SAMPLE_INTERVAL_MS = 1500;
const SERVER_CHART_HISTORY_LIMIT = 180;
const SERVER_CHART_COALESCE_WINDOW_MS = 500;
const SERVER_CHART_JITTER_TOLERANCE_MS = 650;
const SERVER_CHART_STALE_GAP_MS = SERVER_REALTIME_SAMPLE_INTERVAL_MS * 3;
const EMPTY_METRIC_RECORDS = Object.freeze([]);
const serverMetricsHistoryCache = new Map();
const serverMetricDisplayCache = new Map();

const normalizeMetricRecords = (records = []) => {
  const now = Date.now();
  const list = Array.isArray(records) ? records : [];
  return list
    .map((record, index) => {
      const fallbackTime = now - (list.length - index - 1) * SERVER_REALTIME_SAMPLE_INTERVAL_MS;
      const timestamp = toTimestamp(record._ts || record.recorded_at || record.timestamp || record.time, fallbackTime);
      if (!Number.isFinite(timestamp)) return null;
      return {
        ...record,
        _ts: timestamp,
      };
    })
    .filter(Boolean)
    .sort((a, b) => a._ts - b._ts);
};

const normalizeChartMetricRecords = (records = []) => {
  const normalized = normalizeMetricRecords(records);
  if (normalized.length <= 2) return normalized;

  const output = [];
  let segment = [];

  const flushSegment = () => {
    if (segment.length === 0) return;

    if (segment.length < 3) {
      output.push(...segment);
      segment = [];
      return;
    }

    const first = segment[0];
    const last = segment[segment.length - 1];
    const averageInterval = (last._ts - first._ts) / (segment.length - 1);
    const shouldSnap = Math.abs(averageInterval - SERVER_REALTIME_SAMPLE_INTERVAL_MS) <= SERVER_CHART_JITTER_TOLERANCE_MS;

    if (!shouldSnap) {
      output.push(...segment);
      segment = [];
      return;
    }

    const anchor = last._ts;
    segment.forEach((record, index) => {
      output.push({
        ...record,
        _rawTs: record._ts,
        _ts: anchor - (segment.length - index - 1) * SERVER_REALTIME_SAMPLE_INTERVAL_MS,
      });
    });
    segment = [];
  };

  normalized.forEach(record => {
    const last = segment[segment.length - 1];
    if (last && record._ts - last._ts >= SERVER_CHART_STALE_GAP_MS) {
      flushSegment();
      const previous = output[output.length - 1];
      if (previous && !previous._gap) {
        const gapTs = previous._ts + SERVER_REALTIME_SAMPLE_INTERVAL_MS;
        if (gapTs < record._ts) {
          output.push({ _ts: gapTs, _gap: true });
        }
      }
    }
    segment.push(record);
  });
  flushSegment();

  return output
    .filter((record, index, list) => index === 0 || record._ts > list[index - 1]._ts)
    .slice(-SERVER_CHART_HISTORY_LIMIT);
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

  const normalized = recordGroups
    .flatMap(group => (Array.isArray(group) ? group : []))
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
    .sort((a, b) => a._ts - b._ts);

  const coalesced = [];
  normalized.forEach(record => {
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

  const trimmed = coalesced.slice(-SERVER_CHART_HISTORY_LIMIT);
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

const formatResponseTime = (value) => {
  const ms = toNumber(value, NaN);
  return Number.isFinite(ms) && ms > 0 ? `${Math.round(ms)}ms` : '-';
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

// ==================== 主 React 组件 ====================
function ServerPage() {
  const { setMainActiveTab, theme, publicApiUrl } = useStore();
  const isCompactViewport = useMediaQuery('(max-width: 640px)');
  const expandedMainChartHeight = isCompactViewport ? 112 : 150;
  const expandedSubChartHeight = isCompactViewport ? 104 : 130;
  const expandedChartXAxisTickCount = isCompactViewport ? 3 : 5;
  const expandedChartYAxisTickCount = isCompactViewport ? 3 : 4;
  const expandedChartXAxisTickFormat = isCompactViewport ? formatCompactChartTime : formatChartTime;
  const expandedPercentAxisTickFormat = isCompactViewport ? formatCompactPercentAxis : formatPercentAxis;
  const expandedNumberAxisTickFormat = isCompactViewport ? formatCompactNumberAxis : formatNumberAxis;
  const expandedSpeedAxisTickFormat = isCompactViewport ? formatCompactBytesSpeed : formatBytesSpeed;
  
  // 核心标签页状态
  const [serverCurrentTab, setServerCurrentTab] = useState('list'); // 'list', 'history', 'docker', 'management', 'terminal'
  
  // 主机列表状态
  const [serverList, setServerList] = useState([]);
  const [serverLoading, setServerLoading] = useState(false);
  const [serverSearchText, setServerSearchText] = useState('');
  const [serverStatusFilter, setServerStatusFilter] = useState('all');
  const [expandedServers, setExpandedServers] = useState([]);
  const [serverCardViews, setServerCardViews] = useState({});
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
    country: 'auto'
  });
  const [selectedCredentialId, setSelectedCredentialId] = useState('');
  const [serverModalSaving, setServerModalSaving] = useState(false);
  const [serverModalError, setServerModalError] = useState('');
  
  // 快速部署 (Agent) 模式
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
  const [quickCommandInput, setQuickCommandInput] = useState('');
  
  // 终端侧边栏控制
  const [showSftpSidebar, setShowSftpSidebar] = useState(false);
  const [showSnippetsSidebar, setShowSnippetsSidebar] = useState(false);
  const [showServerStatusSidebar, setShowServerStatusSidebar] = useState(false);
  const [sshIdeFullscreen, setSshIdeFullscreen] = useState(false);
  
  // SFTP 状态
  const [sftpFiles, setSftpFiles] = useState([]);
  const [sftpCurrentPath, setSftpCurrentPath] = useState('/');
  const [sftpBreadcrumbs, setSftpBreadcrumbs] = useState([]);
  const [sftpLoading, setSftpLoading] = useState(false);
  const [sftpError, setSftpError] = useState('');
  const [sftpServerId, setSftpServerId] = useState('');
  const [sftpUploading, setSftpUploading] = useState(false);
  const [showSftpEditorModal, setShowSftpEditorModal] = useState(false);
  const [sftpEditFile, setSftpEditFile] = useState(null);
  const [sftpSaving, setSftpSaving] = useState(false);
  
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
  const sftpUploadInputRef = useRef(null);
  const dockerTaskStreamRef = useRef(null);
  const terminalResizeTimers = useRef({});
  const socketRef = useRef(null);
  const pendingMetricUpdatesRef = useRef([]);
  const metricFlushTimerRef = useRef(null);
  const visibleSessionIdsRef = useRef([]);
  const sshSyncEnabledRef = useRef(false);
  const sftpPathByServerRef = useRef({});

  const [historyColWidths, startHistoryResize] = useTableResize([180, 150, 100, 100, 100, 150]);
  const [dockerColWidths, startDockerResize] = useTableResize([180, 220, 100, 180, 120]);
  const [imagesColWidths, startImagesResize] = useTableResize([250, 100, 100, 150, 100]);
  const [networksColWidths, startNetworksResize] = useTableResize([180, 180, 120, 120, 150, 100]);
  const [volumesColWidths, startVolumesResize] = useTableResize([240, 140, 120, 150, 100]);
  const [statsColWidths, startStatsResize] = useTableResize([180, 120, 160, 120, 120, 150]);

  useEffect(() => {
    visibleSessionIdsRef.current = visibleSessionIds;
  }, [visibleSessionIds]);

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
  
  // -------------------- 核心周期初始化 --------------------
  
  useEffect(() => {
    loadServerList();
    loadCredentials();
    connectMetricsStream();
    
    return () => {
      // 清理 WebSocket 与 SSE
      if (socketRef.current) {
        socketRef.current.disconnect();
      }
      if (dockerTaskStreamRef.current) {
        dockerTaskStreamRef.current.close();
      }
      if (metricFlushTimerRef.current) {
        clearTimeout(metricFlushTimerRef.current);
        metricFlushTimerRef.current = null;
      }
      Object.values(terminalResizeTimers.current).forEach(timer => clearTimeout(timer));
      terminalResizeTimers.current = {};
      // 销毁所有终端实例
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
  
  // 同步主 store 中 serverList 状态，便于 dashboard 使用
  useEffect(() => {
    useStore.setState({ serverList });
  }, [serverList]);

  const syncStoreServerList = () => {};

  const mergeTerminalCapabilities = (server, agentOnline) => {
    const hasSshTransport = server.ssh_configured === true || server.terminal_transports?.includes('ssh');
    const terminalTransports = agentOnline ? ['agent'] : [];
    if (hasSshTransport) terminalTransports.push('ssh');

    return {
      agent_online: agentOnline,
      terminal_transports: terminalTransports,
      preferred_terminal_transport: terminalTransports[0] || null,
    };
  };
  
  // 载入主机列表
  const loadServerList = async () => {
    setServerLoading(true);
    try {
      const response = await fetch('/api/server/accounts');
      const data = await response.json();
      if (data.success) {
        setServerList(prev => {
          const prevMap = new Map(prev.map(s => [s.id, s]));
          const updated = data.data.map(server => {
            const existing = prevMap.get(server.id);
            const cachedMetrics = existing?.metricsCache || getCachedServerMetricHistory(server.id);
            return {
              ...server,
              info: server.info || existing?.info || null,
              metricsCache: cachedMetrics || null,
              metricsLoading: existing?.metricsLoading || false,
              gpuChartVisible: existing?.gpuChartVisible || false,
              gpuLoading: existing?.gpuLoading || false,
              netChartVisible: existing?.netChartVisible || false,
              netLoading: existing?.netLoading || false,
              error: existing?.error || null,
              loading: existing?.loading || false
            };
          });
          syncStoreServerList(updated);
          return updated;
        });
      }
    } catch (error) {
      console.error('加载主机列表失败:', error);
      toast.error('加载主机列表失败');
    } finally {
      setServerLoading(false);
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
  
  // Socket.IO 推送实时指标
  const connectMetricsStream = () => {
    try {
      const socket = io('/metrics', {
        reconnection: true,
        reconnectionDelay: 1000,
        reconnectionDelayMax: 5000,
        reconnectionAttempts: Infinity,
        transports: ['websocket', 'polling']
      });
      
      socket.on('connect', () => {
        console.log('✅ Socket.IO 实时流已连接');
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
          setServerList(prev => {
            const updated = prev.map(s => {
              if (s.id === data.serverId) {
                const agentOnline = data.status === 'online';
                const hasResponseTime = Object.prototype.hasOwnProperty.call(data, 'responseTime');
                return {
                  ...s,
                  ...mergeTerminalCapabilities(s, agentOnline),
                  status: data.status,
                  response_time: hasResponseTime ? data.responseTime : s.response_time,
                  error: data.status === 'offline' ? (data.error || 'Agent 离线') : null
                };
              }
              return s;
            });
            syncStoreServerList(updated);
            return updated;
          });
        }
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
      queued.forEach(data => handleSingleMetricUpdate(data));
    }, 80);
  };

  // 处理实时推送的主机指标
  const handleSingleMetricUpdate = (data) => {
    const { serverId, metrics, timestamp } = data;
    const now = timestamp || Date.now();
    mergeServerMetricHistory(
      serverId,
      getCachedServerMetricHistory(serverId) || [],
      [buildMetricHistoryRecord(metrics, null, now)]
    );

    setServerList(prev => {
      const updated = prev.map(server => {
        if (server.id !== serverId) return server;
        
        // 防抖限制刷新间隔 >500ms
        const lastUpdate = server.lastMetricUpdateTime || 0;
        if (lastUpdate > 0 && (now - lastUpdate) < 500) {
          return server;
        }
        
        const info = server.info ? { ...server.info } : {
          cpu: { Load: '-', Usage: '0%', Cores: '-' },
          memory: { Used: '-', Total: '-', Usage: '0%' },
          disk: [{ device: '/', used: '-', total: '-', usage: '0%' }],
          network: { connections: 0, rx_speed: '0 B/s', tx_speed: '0 B/s', rx_total: '0 B', tx_total: '0 B' },
          docker: { installed: false, containers: [] }
        };
        
        // CPU
        const logicalCores = parseInt(metrics.logical_cores) || parseInt(metrics.cores) || parseInt(info.cpu?.LogicalCores) || parseInt(info.cpu?.Cores) || 0;
        const physicalCores = parseInt(metrics.physical_cores) || parseInt(info.cpu?.PhysicalCores) || logicalCores || 0;
        const metricCpu = metrics.cpu && typeof metrics.cpu === 'object' ? metrics.cpu : {};
        const existingCpu = info.cpu && typeof info.cpu === 'object' ? info.cpu : {};
        const resolvedCpuTemp = getCpuTemp({ ...metrics, cpu: { ...existingCpu, ...metricCpu } });
        info.cpu = {
          Load: metrics.load || '-',
          Usage: metrics.cpu_usage || '0%',
          Cores: logicalCores || info.cpu.Cores || '-',
          LogicalCores: logicalCores || info.cpu?.LogicalCores || info.cpu?.Cores || '-',
          PhysicalCores: physicalCores || info.cpu?.PhysicalCores || info.cpu?.Cores || '-',
          Temp: resolvedCpuTemp > 0 ? resolvedCpuTemp : (info.cpu?.Temp || 0),
          Power: metrics.cpu_power || metrics.cpu_power_w || info.cpu?.Power || ''
        };
        
        // Memory
        if (metrics.mem_usage) {
          const memMatch = metrics.mem_usage.match(/(\d+)\/(\d+)MB/);
          if (memMatch) {
            const used = parseInt(memMatch[1]);
            const total = parseInt(memMatch[2]);
            info.memory = {
              Used: used + ' MB',
              Total: total + ' MB',
              Usage: Math.round((used / total) * 100) + '%'
            };
          }
        }
        
        // Disk
        if (metrics.disk_usage) {
          const diskMatch = metrics.disk_usage.match(/(.+?)\/(.+?)\s*\((\d+%?)\)/);
          if (diskMatch) {
            if (!info.disk || !Array.isArray(info.disk)) info.disk = [{}];
            info.disk[0] = {
              device: '/',
              used: diskMatch[1].trim(),
              total: diskMatch[2].trim(),
              usage: diskMatch[3]
            };
          }
        }
        
        // Network
        if (metrics.network) {
          info.network = { ...info.network, ...metrics.network };
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
          info.gpu = {
            Model: pushedGpu.Model || metrics.gpu_model || existingGpu.Model || '',
            Usage: pushedGpu.Usage || metrics.gpu_usage || pushedGpuUsage || existingGpu.Usage || '0%',
            Memory: pushedGpu.Memory || metrics.gpu_mem || existingGpu.Memory || '',
            Power: pushedGpu.Power || metrics.gpu_power || existingGpu.Power || '',
            Temp: pushedGpu.Temp !== undefined ? pushedGpu.Temp : (metrics.gpu_temp !== undefined ? metrics.gpu_temp : (existingGpu.Temp || 0)),
            Percent: pushedGpuPercent !== undefined ? pushedGpuPercent : 0,
          };
        }
        
        // Docker
        if (metrics.docker) {
          info.docker = {
            installed: !!metrics.docker.installed,
            runningCount: metrics.docker.running || 0,
            stoppedCount: metrics.docker.stopped || 0,
            containers: Array.isArray(metrics.docker.containers) ? metrics.docker.containers : []
          };
        }
        
        info.platform = metrics.platform || info.platform;
        info.platformVersion = metrics.platformVersion || info.platformVersion;
        info.uptime = metrics.uptime || info.uptime;
        
        // 增量追加指标趋势缓存
        const cache = mergeServerMetricHistory(
          serverId,
          getCachedServerMetricHistory(serverId) || [],
          server.metricsCache || [],
          [buildMetricHistoryRecord(metrics, info, now)]
        );
        
        return {
          ...server,
          ...mergeTerminalCapabilities(server, true),
          info,
          status: 'online',
          error: null,
          metricsCache: cache,
          lastMetricUpdateTime: now
        };
      });
      syncStoreServerList(updated);
      return updated;
    });
  };
  
  const loadCardMetrics = async (serverId, options = {}) => {
    const { silent = false } = options;
    setServerList(prev => prev.map(s => (
      s.id === serverId
        ? {
            ...s,
            metricsCache: s.metricsCache || getCachedServerMetricHistory(serverId) || null,
            metricsLoading: silent && (s.metricsCache?.length || getCachedServerMetricHistory(serverId)?.length) ? false : true,
            error: silent ? s.error : null,
          }
        : s
    )));

    try {
      const params = new URLSearchParams({
        serverId,
        page: 1,
        pageSize: SERVER_CHART_HISTORY_LIMIT,
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
        sorted,
        getCachedServerMetricHistory(serverId) || []
      );
      setServerList(prev => prev.map(s => (
        s.id === serverId
          ? { ...s, metricsCache: merged, metricsLoading: false, error: null }
          : s
      )));
      return merged;
    } catch (e) {
      setServerList(prev => prev.map(s => (
        s.id === serverId
          ? { ...s, metricsLoading: false, error: silent ? s.error : e.message }
          : s
      )));
      return [];
    }
  };

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
    await Promise.all([
      loadServerInfo(serverId, { force: true }),
      loadCardMetrics(serverId, { silent: true }),
    ]);
    toast.success('主机详情已刷新');
  };

  // 切换折叠卡片并加载详情与历史数据
  const toggleServerExpand = async (serverId) => {
    const server = serverList.find(s => s.id === serverId);
    if (!server) return;
    
    if (server.status !== 'online') {
      toast.warning('主机未在线，无法查看详情');
      return;
    }
    
    if (expandedServers.includes(serverId)) {
      setExpandedServers(prev => prev.filter(id => id !== serverId));
    } else {
      setExpandedServers(prev => [...prev, serverId]);
      setServerCardViews(prev => ({
        ...prev,
        [serverId]: {
          system: prev[serverId]?.system || 'load',
          gpu: prev[serverId]?.gpu || 'detail',
          network: prev[serverId]?.network || 'detail',
        }
      }));
      const cachedMetrics = server.metricsCache || getCachedServerMetricHistory(serverId);
      if (cachedMetrics?.length) {
        loadCardMetrics(serverId, { silent: true });
      }
      await Promise.all([
        server.info ? Promise.resolve(server.info) : loadServerInfo(serverId, { force: false }),
        cachedMetrics ? Promise.resolve(cachedMetrics) : loadCardMetrics(serverId, { silent: !!server.info }),
      ]);
    }
  };
  
  // -------------------- 主机增改删操作 --------------------
  
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
      country: 'auto'
    });
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
      password: '', // 安全考虑，密码置空
      privateKey: '',
      passphrase: '',
      tagsInput: Array.isArray(server.tags) ? server.tags.join(',') : '',
      description: server.description || '',
      country: server.country || 'auto'
    });
    setServerModalMode('edit');
    setServerModalError('');
    setShowServerModal(true);
  };
  
  const applyCredential = (credId) => {
    const cred = serverCredentials.find(c => c.id === credId);
    if (cred) {
      setServerForm(prev => ({
        ...prev,
        username: cred.username,
        authType: cred.auth_type === 'key' ? 'privateKey' : 'password',
        password: cred.password || '',
        privateKey: cred.private_key || '',
        passphrase: cred.passphrase || ''
      }));
      setSelectedCredentialId(credId);
    }
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
        toast.success('🎉 连接测试成功！');
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
    if (!serverForm.name || !serverForm.host || !serverForm.username) {
      setServerModalError('请填写完整必填参数');
      return;
    }
    setServerModalSaving(true);
    setServerModalError('');
    
    try {
      const tags = serverForm.tagsInput ? serverForm.tagsInput.split(',').map(t => t.trim()).filter(Boolean) : [];
      const payload = {
        name: serverForm.name,
        host: serverForm.host,
        port: serverForm.port,
        username: serverForm.username,
        auth_type: serverForm.authType === 'privateKey' ? 'key' : 'password',
        tags,
        description: serverForm.description,
        country: serverForm.country
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
      setServerModalError('Server name is required');
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
        toast.success(data.data?.isNew ? 'Agent host created' : 'Agent install command generated');
        loadServerList();
      } else {
        setServerModalError(data.error || 'Failed to generate Agent install command');
      }
    } catch (e) {
      setServerModalError('Agent quick install request failed: ' + e.message);
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
        if (url.protocol === 'http:' && url.port === '443') {
          url.protocol = 'https:';
          url.port = '';
        }
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

  const getAgentInstallCommand = (osType = agentInstallOS) => {
    if (!agentModalData) return '';
    if (!agentModalData.agentKey) {
      return osType === 'linux'
        ? agentModalData.installCommand || ''
        : agentModalData.winInstallCommand || '';
    }

    const baseUrl = `${getAgentBaseApiUrl()}/api/server/agent/install`;
    if (osType === 'linux') {
      return `curl -fsSL ${baseUrl}/linux/${agentModalData.serverId}/${agentModalData.agentKey} | bash`;
    }
    return `powershell -c "irm ${baseUrl}/win/${agentModalData.serverId}/${agentModalData.agentKey} | iex"`;
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
      const response = await fetch(`/api/server/agent/auto-install/${serverId}`, {
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
    if (!(await dialog.confirm('确定要删除这台主机吗？此操作不可逆！'))) return;
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

  const setCardView = (serverId, key, value) => {
    setServerCardViews(prev => ({
      ...prev,
      [serverId]: {
        system: prev[serverId]?.system || 'load',
        gpu: prev[serverId]?.gpu || 'detail',
        network: prev[serverId]?.network || 'detail',
        [key]: value,
      },
    }));
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

  const getDockerContainerPorts = (container) => {
    const ports = container?.ports ?? container?.Ports ?? container?.portMappings;
    if (typeof ports === 'string') return ports || '-';
    if (Array.isArray(ports)) {
      const formatted = ports.map(port => {
        if (typeof port === 'string') return port;
        const privatePort = port.PrivatePort ?? port.privatePort ?? port.containerPort;
        const publicPort = port.PublicPort ?? port.publicPort ?? port.hostPort;
        const type = port.Type ?? port.type ?? 'tcp';
        if (publicPort && privatePort) return `${publicPort}:${privatePort}/${type}`;
        if (privatePort) return `${privatePort}/${type}`;
        return '';
      }).filter(Boolean);
      return formatted.length > 0 ? formatted.join(', ') : '-';
    }
    return '-';
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
        const response = await fetch(`/api/server/agent/auto-install/${item.serverId}`, {
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
    appendLog('正在获取初始连接状态...\n');

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
        const response = await fetch(`/api/server/agent/auto-install/${server.id}`, {
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
            const response = await fetch(`/api/server/agent/auto-install/${server.id}`, {
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
          country: s.country
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
  
  // -------------------- 凭据库管理 --------------------
  
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
    if (!(await dialog.confirm('确定要删除此凭据吗？'))) return;
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
  
  // -------------------- 采集与定时设置 --------------------
  
  const updateMetricsCollectInterval = async (val) => {
    setMetricsCollectInterval(val);
    try {
      await fetch('/api/server/monitor/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ interval: val })
      });
      toast.success('指标采集时间间隔更新成功');
    } catch (e) {
      console.error(e);
    }
  };
  
  const handleRetentionSliderChange = async (val) => {
    setMonitorConfig({ metrics_retention_days: val });
    try {
      await fetch('/api/server/monitor/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ metrics_retention_days: val })
      });
    } catch (e) {
      console.error(e);
    }
  };
  
  // -------------------- 历史数据记录子标签 --------------------
  
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
  
  // -------------------- Docker 监控与管理 --------------------
  
  useEffect(() => {
    if (serverCurrentTab === 'docker') {
      ensureDockerTaskStream();
      loadDockerResources();
    }
  }, [serverCurrentTab, dockerSubTab, dockerSelectedServer]);
  
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
  
  const submitDockerTask = async (action, payload = {}) => {
    const serverId = payload.serverId || dockerSelectedServer;
    if (!serverId) {
      toast.warning('请先选择一台主机');
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
      // 获取概览数据
      const response = await fetch('/api/server/v2/docker/overview' + (dockerSelectedServer ? `?serverId=${dockerSelectedServer}` : ''));
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

  const renderDockerEmptyState = (message) => (
    <div className="bg-kumo-base border border-kumo-line rounded-lg p-10 text-center text-xs text-kumo-subtle shadow-xs">
      {message}
    </div>
  );
  
  // -------------------- SSH / Agent 嵌入式终端与多分屏 --------------------
  
  // 新连接 SSH 终端会话方法
  const openSSHTerminal = (server) => {
    if (!server) return;
    
    // 检查是否已存在该服务器的 SSH 会话
    const existing = sshSessions.find(s => s.server.id === server.id);
    if (existing) {
      switchToSSHTab(existing.id);
      return;
    }
    
    const type = resolveTerminalProtocol(server);
    if (!type) {
      toast.warning('该主机当前没有可用的终端传输');
      return;
    }

    const sessionId = 'session_' + Date.now();
    
    const newSession = {
      id: sessionId,
      server,
      type,
      connected: false,
      name: server.name
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
    loadSftpDirectory(serverId, lastPath);
  };

  const sendTerminalCommand = (sessionId, command) => {
    const text = String(command || '').trim();
    if (!sessionId || !text) return;

    const payload = `${text}\r`;
    const sendToSession = (targetId) => {
      const target = sshSessionRefs.current[targetId];
      if (target?.ws?.readyState === WebSocket.OPEN) {
        target.ws.send(JSON.stringify({ type: 'input', data: payload }));
        target.terminal?.focus();
      }
    };

    sendToSession(sessionId);
    if (sshSyncEnabledRef.current && visibleSessionIdsRef.current.includes(sessionId)) {
      visibleSessionIdsRef.current.forEach(targetId => {
        if (targetId !== sessionId) sendToSession(targetId);
      });
    }
  };

  const runQuickCommand = (command) => {
    sendTerminalCommand(activeSSHSessionId, command);
    setQuickCommandInput('');
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
  
  // 归还 DOM 元素到仓库
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
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/ssh`);

    ws.onopen = () => {
      ws.send(JSON.stringify({
        type: 'connect',
        sessionId,
        serverId: sessionMeta.server.id,
        protocol: sessionMeta.type,
        lastSeq: sshSessionRefs.current[sessionId]?.lastSeq || 0,
        cols: terminal.cols,
        rows: terminal.rows
      }));

      const hb = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, 20000);

      const session = sshSessionRefs.current[sessionId];
      if (session) session.heartbeatInterval = hb;
    };

    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === 'connected') {
          setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: true } : s));
          if (!msg.resumed) {
            terminal.clear();
          }
          terminal.writeln(`\x1b[1;32m${msg.resumed ? '已恢复终端会话' : '已成功连接到终端'}\x1b[0m`);
          scheduleTerminalFit(sessionId, 100);
        } else if (msg.type === 'history') {
          const session = sshSessionRefs.current[sessionId];
          if (session && msg.toSeq) session.lastSeq = Math.max(session.lastSeq || 0, msg.toSeq);
          terminal.write(msg.data || '');
        } else if (msg.type === 'output') {
          const session = sshSessionRefs.current[sessionId];
          if (session && msg.seq) session.lastSeq = Math.max(session.lastSeq || 0, msg.seq);
          terminal.write(msg.data);
        } else if (msg.type === 'error') {
          terminal.writeln(`\n\x1b[1;31m错误: ${msg.message}\x1b[0m`);
        } else if (msg.type === 'disconnected') {
          setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
          terminal.writeln(`\n\x1b[1;31m连接已从远端断开: ${msg.message || ''}\x1b[0m`);
        } else if (msg.type === 'detached') {
          setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
          terminal.writeln(`\n\x1b[1;33m${msg.message || '终端已在新的窗口恢复'}\x1b[0m`);
        }
      } catch (err) {
        console.error(err);
      }
    };

    ws.onclose = () => {
      const session = sshSessionRefs.current[sessionId];
      if (session?.heartbeatInterval) {
        clearInterval(session.heartbeatInterval);
        session.heartbeatInterval = null;
      }
      setSshSessions(prev => prev.map(s => s.id === sessionId ? { ...s, connected: false } : s));
    };

    ws.onerror = () => {
      terminal.writeln(`\n\x1b[1;31m终端连接发生网络错误\x1b[0m`);
    };

    return ws;
  };

  // 实例化 xterm.js 核心方法
  const initSessionTerminal = (sessionId, sessionMeta) => {
    if (sshSessionRefs.current[sessionId]) return;
    
    // 创建全局唯一的终端包装 div
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
  
  // -------------------- SFTP 文件管理系统 --------------------
  
  const loadSftpDirectory = async (serverId, path = '.') => {
    setSftpLoading(true);
    setSftpError('');
    try {
      const response = await fetch('/api/server/sftp/list', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, path })
      });
      const data = await response.json();
      if (data.success) {
        setSftpFiles(data.data || []);
        setSftpCurrentPath(data.path);
        setSftpServerId(serverId);
        sftpPathByServerRef.current[serverId] = data.path;
        
        // 构建路径导航
        const parts = data.path.split('/').filter(Boolean);
        const crumbs = [{ name: '/', path: '/' }];
        let cur = '';
        parts.forEach(p => {
          cur += '/' + p;
          crumbs.push({ name: p, path: cur });
        });
        setSftpBreadcrumbs(crumbs);
      } else {
        setSftpError(data.error || '加载 SFTP 目录失败');
      }
    } catch (e) {
      setSftpError('请求失败: ' + e.message);
    } finally {
      setSftpLoading(false);
    }
  };
  
  const handleSftpFileClick = (file) => {
    if (file.isDirectory) {
      loadSftpDirectory(sftpServerId, file.path);
    } else {
      openSftpFile(file);
    }
  };
  
  const openSftpFile = async (file) => {
    try {
      const res = await fetch('/api/server/sftp/read', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId: sftpServerId, path: file.path })
      });
      const data = await res.json();
      if (data.success) {
        setSftpEditFile({
          path: file.path,
          name: file.name,
          content: data.data,
          originalContent: data.data
        });
        setShowSftpEditorModal(true);
      }
    } catch (e) {
      toast.error('读取文件失败');
    }
  };
  
  const saveSftpEditedFile = async () => {
    if (!sftpEditFile) return;
    setSftpSaving(true);
    try {
      const res = await fetch('/api/server/sftp/write', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          serverId: sftpServerId,
          path: sftpEditFile.path,
          content: sftpEditFile.content
        })
      });
      const data = await res.json();
      if (data.success) {
        toast.success('文件保存成功');
        setShowSftpEditorModal(false);
        loadSftpDirectory(sftpServerId, sftpCurrentPath);
      }
    } catch (e) {
      toast.error('保存文件异常');
    } finally {
      setSftpSaving(false);
    }
  };
  
  const handleSftpUpload = async (e) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    setSftpUploading(true);
    
    let ok = 0;
    for (let file of files) {
      const fd = new FormData();
      fd.append('serverId', sftpServerId);
      fd.append('path', sftpCurrentPath);
      fd.append('file', file);
      try {
        const res = await fetch('/api/server/sftp/upload', { method: 'POST', body: fd });
        const d = await res.json();
        if (d.success) ok++;
      } catch (err) {
        console.error(err);
      }
    }
    setSftpUploading(false);
    e.target.value = '';
    toast.success(`成功上传 ${ok} 个文件`);
    loadSftpDirectory(sftpServerId, sftpCurrentPath);
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
  
  // 统计数值
  const statsSummary = useMemo(() => {
    const total = serverList.length;
    const online = serverList.filter(s => s.status === 'online').length;
    const offline = total - online;
    return { total, online, offline };
  }, [serverList]);
  
  return (
    <div
      className={
        serverCurrentTab === 'terminal'
          ? 'flex h-[calc(100dvh-80px)] min-h-0 w-full min-w-0 flex-col gap-3 overflow-hidden px-1 sm:h-[calc(100dvh-88px)] lg:h-[calc(100dvh-92px)]'
          : 'flex w-full flex-col gap-6 px-1'
      }
    >
      {/* 顶部标签导航 */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <div className="min-w-0 w-full min-[450px]:w-auto">
          <Tabs
            {...MODULE_TABS_PROPS}
            value={serverCurrentTab}
            onValueChange={setServerCurrentTab}
            tabs={[
              { value: 'list', label: <span className="inline-flex items-center gap-1.5"><Server className="w-4 h-4" /><span className="hidden sm:inline">主机实例管理</span><span className="sm:hidden">主机</span></span> },
              { value: 'history', label: <span className="inline-flex items-center gap-1.5"><History className="w-4 h-4" /><span className="hidden sm:inline">历史趋势</span><span className="sm:hidden">趋势</span></span> },
              { value: 'docker', label: <span className="inline-flex items-center gap-1.5"><Box className="w-4 h-4" />Docker</span> },
              { value: 'management', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4" /><span className="hidden sm:inline">后台管理</span><span className="sm:hidden">管理</span></span> },
              ...(sshSessions.length > 0
                ? [{
                    value: 'terminal',
                    label: (
                      <span className="inline-flex items-center gap-1.5">
                        <TerminalIcon className="w-4 h-4" />
                        <span className="hidden sm:inline">SSH 终端</span>
                        <span className="sm:hidden">SSH</span>
                        <span className="rounded bg-kumo-brand/10 px-1.5 py-0.5 text-[10px] font-bold text-kumo-brand">
                          {sshSessions.length}
                        </span>
                      </span>
                    ),
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
      
      {/* ==================== 1. 主机实例管理 ==================== */}
      {serverCurrentTab === 'list' && (
        <div className="flex flex-col gap-4">
          {/* 控制过滤器栏 */}
          <div className="flex flex-col gap-3 rounded-md border border-kumo-line/90 bg-kumo-base p-3 shadow-sm ring-1 ring-kumo-line/25 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex min-w-0 flex-nowrap items-center gap-2 overflow-x-auto whitespace-nowrap scrollbar-thin sm:gap-3">
              <Tabs
                {...TOOL_TABS_PROPS}
                className="w-fit max-w-full"
                listClassName="w-fit max-w-full"
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
                className="w-fit max-w-full"
                listClassName="w-fit max-w-full"
                value={serverIpDisplayMode}
                onValueChange={setServerIpDisplayMode}
                tabs={[
                  { value: 'normal', label: '明文' },
                  { value: 'masked', label: '打码' },
                  { value: 'hidden', label: '隐藏' },
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
            <div className="flex flex-col items-center justify-center p-12 bg-kumo-base border border-kumo-line rounded-md text-kumo-subtle gap-2">
              <div className="w-6 h-6 border-2 border-kumo-brand border-t-transparent rounded-full animate-spin"></div>
              <p className="text-xs">正在连接并加载主机结构中...</p>
            </div>
          ) : filteredServers.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-16 bg-kumo-base border border-kumo-line rounded-md text-kumo-subtle gap-1.5">
              <span className="text-xl">🔍</span>
              <p className="text-xs">未找到符合当前条件的主机节点</p>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {filteredServers.map(server => {
                const country = getFlagCountry(server);
                const isExpanded = expandedServers.includes(server.id);
                const isDarkMode = theme === 'dark';
                const cardView = serverCardViews[server.id] || { system: 'load', gpu: 'detail', network: 'detail' };
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
                } = getServerMetricDisplay(server.id, server.metricsCache, isExpanded, isDarkMode);
                const hasGpuData = !!server.info?.gpu?.Model || records.some(r => (
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
                const canDrag = !serverSearchText.trim() && serverStatusFilter === 'all' && !isExpanded;
                const txTotal = getByteParts(server.info?.network?.tx_total);
                const rxTotal = getByteParts(server.info?.network?.rx_total);
                const chartLoading = !!server.metricsLoading && records.length === 0;
                const terminalProtocol = resolveTerminalProtocol(server);
                const terminalLabel = terminalProtocol === 'agent' ? 'Agent 终端' : 'SSH 终端';
                
                return (
                  <ContextMenu.Root key={server.id}>
                    <ContextMenu.Trigger
                    draggable={canDrag}
                    onDragStart={(event) => handleServerDragStart(server, event)}
                    onDragOver={handleServerDragOver}
                    onDrop={(event) => handleServerDrop(server.id, event)}
                    onDragEnd={() => setDraggedServerId(null)}
                    className={`bg-kumo-base border rounded-lg transition-all duration-200 ${isExpanded ? 'border-kumo-brand/70 shadow-md ring-1 ring-kumo-brand/20' : 'border-kumo-line/90 shadow-xs hover:border-kumo-interact hover:shadow-sm'} ${draggedServerId === server.id ? 'opacity-50' : ''}`}
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
                                <img
                                  src={`https://flagcdn.com/w20/${country.toLowerCase()}.png`}
                                  className="inline-block mr-1.5 w-4 h-3 rounded-xs object-cover"
                                  alt={country}
                                />
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
                          <div className="order-3 col-span-2 grid w-full grid-cols-4 gap-1.5 text-[10px] font-semibold text-kumo-subtle sm:order-none sm:col-span-1 sm:flex sm:h-9 sm:w-auto sm:items-center sm:gap-2.5">
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
                            title={terminalProtocol ? terminalLabel : '终端不可用'}
                            aria-label={terminalProtocol ? terminalLabel : '终端不可用'}
                            icon={<TerminalIcon className="w-3.5 h-3.5" />}
                            onClick={() => openSSHTerminal(server)}
                            disabled={!canOpenTerminal(server)}
                            className="h-9 w-9 p-0 sm:h-8 sm:w-8"
                          />
                        </div>
                      </div>
                    </div>
                    
                    <AnimatedCollapse open={isExpanded}>
                      <div className="rounded-b-lg border-t border-kumo-line/90 bg-kumo-canvas/45 p-2.5 sm:p-4">
                        {server.loading && !server.info ? (
                          <div className="space-y-2 py-8">
                            <SkeletonLine className="h-4 w-1/3 mx-auto" />
                            <SkeletonLine className="h-4 w-1/2 mx-auto" />
                          </div>
                        ) : server.error ? (
                          <div className="rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs font-semibold text-kumo-danger">
                            {server.error}
                          </div>
                        ) : (
                          <div className="flex flex-col gap-3 sm:gap-4">
                            <div className="grid grid-cols-1 gap-3 md:grid-cols-3 md:gap-4">
                              <div className="flex flex-col gap-2 rounded-lg border border-kumo-line bg-kumo-base p-3 shadow-xs sm:gap-3 sm:p-4">
                                <div className="flex items-center justify-between gap-2 border-b border-kumo-line pb-1.5 sm:pb-2">
                                  <h4 className="text-xs font-bold text-kumo-strong">系统与载荷</h4>
                                  <Tabs
                                    {...TOOL_TABS_PROPS}
                                    value={cardView.system}
                                    onValueChange={(value) => setCardView(server.id, 'system', value)}
                                    tabs={[
                                      { value: 'load', label: '负载' },
                                      { value: 'system', label: '系统' },
                                    ]}
                                  />
                                </div>

                                {cardView.system === 'load' ? (
                                  <div className="flex flex-col gap-1.5 text-[11px] leading-5 sm:gap-2 sm:text-xs">
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">CPU 负载</span>
                                      <span className="text-right font-semibold text-kumo-strong">
                                        {server.info?.cpu?.PhysicalCores && server.info?.cpu?.LogicalCores && server.info.cpu.PhysicalCores !== server.info.cpu.LogicalCores
                                          ? `${server.info.cpu.PhysicalCores}核/${server.info.cpu.LogicalCores}线程`
                                          : `${server.info?.cpu?.PhysicalCores || server.info?.cpu?.Cores || '-'} 核`}
                                        <span className="ml-2">{server.info?.cpu?.Usage || '0%'}</span>
                                      </span>
                                    </div>
                                    {toNumber(server.info?.cpu?.Temp, 0) > 0 && (
                                      <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                        <span className="text-kumo-subtle font-medium">CPU 温度</span>
                                        <span className={`text-right font-semibold ${getTempColorClass(server.info.cpu.Temp)}`}>{Math.round(toNumber(server.info.cpu.Temp))}°C</span>
                                      </div>
                                    )}
                                    {toNumber(server.info?.cpu?.Power, 0) > 0 && (
                                      <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                        <span className="text-kumo-subtle font-medium">CPU 功耗</span>
                                        <span className="text-right font-semibold text-kumo-warning">{toNumber(server.info.cpu.Power, 0).toFixed(1)}W</span>
                                      </div>
                                    )}
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">内存使用</span>
                                      <span className="text-right font-semibold text-kumo-strong">{server.info?.memory?.Used || '-'} / {server.info?.memory?.Total || '-'} ({server.info?.memory?.Usage || '0%'})</span>
                                    </div>
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">系统负载</span>
                                      <span className="text-right font-mono font-semibold text-kumo-strong">{server.info?.cpu?.Load || '-'}</span>
                                    </div>
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">在线时间</span>
                                      <span className="text-right font-semibold text-kumo-strong">{formatUptime(server.info?.uptime || server.info?.system?.Uptime)}</span>
                                    </div>
                                    {(server.info?.disk || []).slice(0, 2).map((disk, idx) => (
                                      <div key={`${server.id}-disk-${idx}`} className="border-t border-kumo-line/70 pt-1.5 sm:pt-2">
                                        <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                          <span className="text-kumo-subtle font-medium">{idx === 0 ? '主存储' : '二级存储'} ({disk.usage || '-'})</span>
                                          <span className="text-right font-semibold text-kumo-strong">{disk.used || '-'} / {disk.total || '-'}</span>
                                        </div>
                                        <div className="mt-1 h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed sm:h-2">
                                          <div className="h-full bg-kumo-warning" style={{ width: disk.usage || '0%' }}></div>
                                        </div>
                                      </div>
                                    ))}
                                  </div>
                                ) : (
                                  <div className="flex flex-col gap-1.5 text-[11px] leading-5 sm:gap-2 sm:text-xs">
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">操作系统</span>
                                      <span className="text-right font-semibold text-kumo-strong">{server.info?.platform || '-'}</span>
                                    </div>
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">系统版本</span>
                                      <span className="max-w-44 truncate text-right font-semibold text-kumo-strong">{server.info?.platformVersion || server.info?.system?.Kernel || '-'}</span>
                                    </div>
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">主机地址</span>
                                      <span className="text-right font-mono font-semibold text-kumo-strong">{getHostAddress(server, serverIpDisplayMode)}</span>
                                    </div>
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">监控模式</span>
                                      <span className="text-right font-semibold text-kumo-strong">{getServerMonitorModeLabel(server)}</span>
                                    </div>
                                    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-2 sm:flex sm:justify-between sm:gap-3">
                                      <span className="text-kumo-subtle font-medium">响应延迟</span>
                                      <span className="text-right font-semibold text-kumo-success">{formatResponseTime(server.response_time)}</span>
                                    </div>
                                  </div>
                                )}
                              </div>
                              
                              <ChartBoundaryBox className="md:col-span-2 flex min-w-0 flex-col gap-2 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base p-3 shadow-xs sm:p-4">
                                {(tooltipBoundary) => (
                                  <>
                                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-kumo-line pb-1.5 sm:pb-2">
                                      <h4 className="text-xs font-bold text-kumo-strong">CPU / 内存趋势</h4>
                                      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                                        <ChartLegend.SmallItem name="CPU" color={cpuColor} value={`${parseInt(server.info?.cpu?.Usage || '0')}%`} />
                                        <ChartLegend.SmallItem name="Memory" color={memColor} value={`${parseInt(server.info?.memory?.Usage || '0')}%`} />
                                        <ChartLegend.SmallItem name="Temp" color={cpuTempColor} value={getLatestMetricValue(records, getCpuTemp, v => `${v.toFixed(1)}°C`)} />
                                      </div>
                                    </div>
                                    <DeferredRender open={isExpanded} fallback={<ChartWarmupSkeleton height={expandedMainChartHeight} />}>
                                      <TimeseriesChart
                                        echarts={echarts}
                                        data={cpuMemSeries}
                                        height={expandedMainChartHeight}
                                        isDarkMode={isDarkMode}
                                        gradient
                                        loading={chartLoading}
                                        tooltipBoundary={tooltipBoundary ?? undefined}
                                        xAxisTickCount={expandedChartXAxisTickCount}
                                        yAxisTickCount={expandedChartYAxisTickCount}
                                        xAxisTickFormat={expandedChartXAxisTickFormat}
                                        yAxisTickFormat={expandedNumberAxisTickFormat}
                                        tooltipValueFormat={formatMetricTooltipValue}
                                        ariaDescription={`${server.name} CPU and memory usage trend`}
                                      />
                                    </DeferredRender>
                                  </>
                                )}
                              </ChartBoundaryBox>
                            </div>

                            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 md:gap-4">
                              <ChartBoundaryBox className="flex min-w-0 flex-col gap-2 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base p-3 shadow-xs sm:gap-3 sm:p-4">
                                {(tooltipBoundary) => (
                                  <>
                                    <div className="flex items-center justify-between gap-2 border-b border-kumo-line pb-1.5 sm:pb-2">
                                      <h4 className="text-xs font-bold text-kumo-strong">GPU</h4>
                                      <Tabs
                                        {...TOOL_TABS_PROPS}
                                        value={cardView.gpu}
                                        onValueChange={(value) => setCardView(server.id, 'gpu', value)}
                                        tabs={[
                                          { value: 'detail', label: '详情' },
                                          { value: 'chart', label: '趋势' },
                                        ]}
                                      />
                                    </div>
                                    {cardView.gpu === 'detail' ? (
                                      hasGpuData ? (
                                        <div className="flex flex-col gap-1.5 text-[11px] leading-5 sm:gap-2 sm:text-xs">
                                          <div className="grid grid-cols-[3rem_minmax(0,1fr)] items-center gap-2 sm:gap-3">
                                            <span className="text-kumo-subtle font-medium">型号</span>
                                            <span className="truncate text-right font-semibold text-kumo-strong" title={server.info?.gpu?.Model}>{server.info?.gpu?.Model || 'GPU'}</span>
                                          </div>
                                          <div className="grid grid-cols-[3rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                            <span className="text-kumo-subtle font-medium">使用率</span>
                                            <span className="text-right font-semibold text-kumo-strong">{server.info?.gpu?.Usage || '0%'}</span>
                                          </div>
                                          {toNumber(server.info?.gpu?.Temp, 0) > 0 && (
                                            <div className="grid grid-cols-[3rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                              <span className="text-kumo-subtle font-medium">温度</span>
                                              <span className={`text-right font-semibold ${getTempColorClass(server.info.gpu.Temp)}`}>{Math.round(toNumber(server.info.gpu.Temp))}°C</span>
                                            </div>
                                          )}
                                          {server.info?.gpu?.Memory && (
                                            <div className="grid grid-cols-[3rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                              <span className="text-kumo-subtle font-medium">显存</span>
                                              <span className="text-right font-semibold text-kumo-strong">{server.info.gpu.Memory} ({Math.round(toNumber(server.info.gpu.Percent, 0))}%)</span>
                                            </div>
                                          )}
                                          {server.info?.gpu?.Power && (
                                            <div className="grid grid-cols-[3rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                              <span className="text-kumo-subtle font-medium">功耗</span>
                                              <span className="text-right font-semibold text-kumo-warning">{server.info.gpu.Power}</span>
                                            </div>
                                          )}
                                        </div>
                                      ) : (
                                        <div className="py-4 text-center text-xs text-kumo-subtle sm:py-6">未检测到 GPU 数据</div>
                                      )
                                    ) : (
                                      <div className="flex flex-col gap-2">
                                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                                          <ChartLegend.SmallItem name="GPU" color={gpuColor} value={getLatestMetricValue(records, r => toNumber(r.gpu_usage, 0), v => `${v.toFixed(1)}%`)} />
                                          <ChartLegend.SmallItem name="VRAM" color={vramColor} value={getLatestMetricValue(records, getGpuMemPercent, v => `${v.toFixed(1)}%`)} />
                                          <ChartLegend.SmallItem name="Power" color={powerColor} value={getLatestMetricValue(records, r => toNumber(r.gpu_power, 0), v => `${v.toFixed(1)}W`)} />
                                          <ChartLegend.SmallItem name="Temp" color={gpuTempColor} value={getLatestMetricValue(records, getGpuTemp, v => `${v.toFixed(1)}°C`)} />
                                        </div>
                                        <DeferredRender open={isExpanded} fallback={<ChartWarmupSkeleton height={expandedSubChartHeight} />}>
                                          <TimeseriesChart
                                            echarts={echarts}
                                            data={gpuSeries}
                                            height={expandedSubChartHeight}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={chartLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            xAxisTickCount={expandedChartXAxisTickCount}
                                            yAxisTickCount={expandedChartYAxisTickCount}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={expandedNumberAxisTickFormat}
                                            tooltipValueFormat={formatMetricTooltipValue}
                                            ariaDescription={`${server.name} GPU usage, VRAM, and power trend`}
                                          />
                                        </DeferredRender>
                                      </div>
                                    )}
                                  </>
                                )}
                              </ChartBoundaryBox>

                              <ChartBoundaryBox className="flex min-w-0 flex-col gap-2 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base p-3 shadow-xs sm:gap-3 sm:p-4">
                                {(tooltipBoundary) => (
                                  <>
                                    <div className="flex items-center justify-between gap-2 border-b border-kumo-line pb-1.5 sm:pb-2">
                                      <h4 className="text-xs font-bold text-kumo-strong">网络</h4>
                                      <Tabs
                                        {...TOOL_TABS_PROPS}
                                        value={cardView.network}
                                        onValueChange={(value) => setCardView(server.id, 'network', value)}
                                        tabs={[
                                          { value: 'detail', label: '详情' },
                                          { value: 'chart', label: '趋势' },
                                        ]}
                                      />
                                    </div>
                                    {cardView.network === 'detail' ? (
                                      <div className="flex flex-col gap-1.5 text-[11px] leading-5 sm:gap-2 sm:text-xs">
                                        <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                          <span className="text-kumo-subtle font-medium">活跃连接数</span>
                                          <span className="text-right font-semibold text-kumo-strong">{server.info?.network?.connections || 0}</span>
                                        </div>
                                        <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                          <span className="text-kumo-subtle font-medium">上传速度</span>
                                          <span className="text-right font-semibold text-kumo-info">{server.info?.network?.tx_speed || '0 B/s'}</span>
                                        </div>
                                        <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                          <span className="text-kumo-subtle font-medium">下载速度</span>
                                          <span className="text-right font-semibold text-kumo-success">{server.info?.network?.rx_speed || '0 B/s'}</span>
                                        </div>
                                        <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-2 sm:flex sm:justify-between sm:gap-3">
                                          <span className="text-kumo-subtle font-medium">累计流量</span>
                                          <span className="grid min-w-0 grid-cols-2 gap-2 text-right font-mono text-[11px] font-semibold tabular-nums sm:w-[168px]">
                                            <span className="truncate text-kumo-info" title={txTotal.text}>&uarr; {txTotal.text}</span>
                                            <span className="truncate text-kumo-success" title={rxTotal.text}>&darr; {rxTotal.text}</span>
                                          </span>
                                        </div>
                                      </div>
                                    ) : (
                                      <div className="flex flex-col gap-2">
                                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                                          <ChartLegend.SmallItem name="Upload" color={txColor} value={getLatestMetricValue(records, r => toNumber(r.net_tx, 0), formatBytesSpeed)} />
                                          <ChartLegend.SmallItem name="Download" color={rxColor} value={getLatestMetricValue(records, r => toNumber(r.net_rx, 0), formatBytesSpeed)} />
                                        </div>
                                        <DeferredRender open={isExpanded} fallback={<ChartWarmupSkeleton height={expandedSubChartHeight} />}>
                                          <TimeseriesChart
                                            echarts={echarts}
                                            data={netSeries}
                                            height={expandedSubChartHeight}
                                            isDarkMode={isDarkMode}
                                            gradient
                                            loading={chartLoading}
                                            tooltipBoundary={tooltipBoundary ?? undefined}
                                            xAxisTickCount={expandedChartXAxisTickCount}
                                            yAxisTickCount={expandedChartYAxisTickCount}
                                            xAxisTickFormat={expandedChartXAxisTickFormat}
                                            yAxisTickFormat={expandedSpeedAxisTickFormat}
                                            tooltipValueFormat={formatBytesSpeed}
                                            ariaDescription={`${server.name} network upload and download speed trend`}
                                          />
                                        </DeferredRender>
                                      </div>
                                    )}
                                  </>
                                )}
                              </ChartBoundaryBox>
                            </div>

                            {server.info?.docker?.installed && (
                              <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base shadow-xs">
                                <Button
                                  type="button"
                                  variant="ghost" size="sm"
                                  className="h-8 w-full justify-between rounded-none border-b border-kumo-line bg-kumo-recessed/25 px-3 text-left"
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

                                <AnimatedCollapse open={dockerExpanded}>
                                  {dockerContainers.length > 0 ? (
                                    <div className="divide-y divide-kumo-line">
                                      {dockerContainers.map(c => {
                                        const state = getDockerContainerState(c);
                                        const stateBadge = getDockerStateBadge(state);
                                        const containerId = getDockerContainerId(c);
                                        const containerName = getDockerContainerName(c);
                                        const containerImage = getDockerContainerImage(c);
                                        return (
                                          <div key={containerId || `${server.id}-${containerName}`} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-3 py-2 text-xs hover:bg-kumo-recessed/20">
                                            <div className="flex min-w-0 items-center gap-2">
                                              <Badge variant={stateBadge.variant} appearance="dot" className="shrink-0">{stateBadge.label}</Badge>
                                              <div className="min-w-0">
                                                <div className="truncate font-semibold text-kumo-strong" title={containerName}>{containerName}</div>
                                                <div className="truncate font-mono text-[10px] text-kumo-subtle" title={containerImage}>{containerImage}</div>
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
                                                icon={<Search className="h-3.5 w-3.5" />}
                                                onClick={(event) => {
                                                  event.stopPropagation();
                                                  submitDockerTask('container.checkUpdates', { serverId: server.id, containerId, containerName, image: containerImage });
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
                        <ContextMenu.Popup className="motion-pop-in z-50 min-w-40 overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5 text-kumo-default shadow-lg outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95">
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
              })}
            </div>
          )}
        </div>
      )}
      
      {/* ==================== 2. 历史趋势 ==================== */}
      {serverCurrentTab === 'history' && (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col md:flex-row gap-4 justify-between bg-kumo-base border border-kumo-line p-4 rounded-lg">
            <div className="flex items-center gap-2">
              <div className="flex items-center gap-1 bg-kumo-recessed/50 p-1 rounded-lg">
                {['1h', '6h', '24h', '7d'].map(range => (
                  <Button size="sm"
                    variant={metricsHistoryTimeRange === range ? 'secondary' : 'ghost'}
                    key={range}
                    onClick={() => setMetricsHistoryTimeRange(range)}
                    className={`px-3 py-1 rounded-md text-[10px] font-bold cursor-pointer transition-colors ${metricsHistoryTimeRange === range ? 'bg-kumo-base text-kumo-strong shadow-xs' : 'text-kumo-subtle hover:text-kumo-strong'}`}
                  >
                    {range}
                  </Button>
                ))}
              </div>
              
              <Button size="sm"
                variant="secondary"
                onClick={triggerManualCollect}
                className="flex items-center gap-1 border border-kumo-line rounded-lg text-xs bg-kumo-base hover:bg-kumo-recessed/30 cursor-pointer font-semibold"
              >
                立即采集
              </Button>
              
              <Button size="sm"
                variant="secondary-destructive"
                onClick={clearMetricsHistory}
                className="flex items-center gap-1 border border-kumo-danger/30 text-kumo-danger rounded-lg text-xs bg-kumo-base hover:bg-kumo-danger/10 cursor-pointer font-semibold"
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
                className="border border-kumo-line rounded-lg px-2.5 py-1 bg-kumo-base text-xs focus:outline-none"
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
          <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden shadow-xs">
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
          <div className="flex flex-col md:flex-row gap-4 justify-between border-b border-kumo-line pb-2.5">
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
                { value: 'stats', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />实时统计</span> },
              ]}
            />
            
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">选择主机</span>
              <Select
                aria-label="选择 Docker 主机" size="sm"
                value={dockerSelectedServer}
                onValueChange={(value) => setDockerSelectedServer(String(value))}
                placeholder="全部 Docker 主机"
                items={[
                  { value: '', label: '全部 Docker 主机' },
                  ...serverList
                    .filter(s => s.status === 'online')
                    .map(s => ({ value: String(s.id), label: s.name || s.id })),
                ]}
              />
            </div>
          </div>
          
          {/* Docker 任务中心 */}
          {dockerTasks.length > 0 && (
            <div className="bg-kumo-recessed border border-kumo-line p-3 rounded-lg text-xs font-mono text-kumo-default flex flex-col gap-1.5 shadow-xs">
              <div className="flex justify-between border-b border-kumo-line pb-1.5 mb-1">
                <span className="inline-flex items-center gap-1.5 font-bold text-kumo-brand"><Activity className="h-3.5 w-3.5" />后台 Docker 任务流水</span>
                <span className="text-[10px] text-kumo-subtle">SSE 实时长连接</span>
              </div>
              <div className="max-h-24 overflow-y-auto flex flex-col gap-1">
                {dockerTasks.map(t => (
                  <div key={t.taskId} className="flex justify-between gap-4">
                    <span className={t.state === 'success' ? 'text-kumo-success' : t.state === 'failed' ? 'text-kumo-danger' : 'text-kumo-warning'}>
                      [{t.state?.toUpperCase()}] {t.action}
                    </span>
                    <span className="text-kumo-subtle">{t.message}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
          
          {/* 内容区域 */}
          {dockerResourceLoading ? (
            <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-4 shadow-xs">
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
                      <div key={server.id} className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden shadow-xs">
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
                                <Table.Head className="p-2 relative">
                                  状态
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(2, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 relative">
                                  端口映射
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(3, e)} />
                                </Table.Head>
                                <Table.Head className="p-2 text-right relative">
                                  操作
                                  <Table.ResizeHandle onMouseDown={(e) => startDockerResize(4, e)} />
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
                                const toggleAction = state === 'running' ? 'container.stop' : 'container.start';
                                return (
                                <Table.Row key={containerId || `${server.id}-${containerName}`} className="border-b border-kumo-line hover:bg-kumo-recessed/10">
                                  <Table.Cell className="p-2 font-bold text-kumo-strong truncate" title={containerName}>{containerName}</Table.Cell>
                                  <Table.Cell className="p-2 truncate" title={containerImage}>{containerImage}</Table.Cell>
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
                                        icon={<Search className="h-3.5 w-3.5" />}
                                        aria-label="检测镜像更新"
                                        onClick={() => submitDockerTask('container.checkUpdates', { serverId: server.id, containerId, containerName, image: containerImage })}
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
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-3.5 shadow-xs">
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
                                  className="rounded bg-kumo-brand text-kumo-inverse text-[10px] font-semibold cursor-pointer"
                                >
                                  Up 启动
                                </Button>
                                <Button size="sm"
                                  variant="secondary"
                                  onClick={() => submitDockerTask('compose.down', composePayload)}
                                  className="rounded border border-kumo-line text-kumo-subtle text-[10px] hover:bg-kumo-recessed/45 cursor-pointer"
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
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-2 shadow-xs">
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
                                className="hover:bg-kumo-danger/10 text-kumo-danger rounded cursor-pointer"
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
              )}

              {/* 4. 网络管理 */}
              {dockerSubTab === 'networks' && (
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-2 shadow-xs">
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
                                  className="hover:bg-kumo-danger/10 text-kumo-danger rounded cursor-pointer disabled:cursor-not-allowed disabled:opacity-40"
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
              )}

              {/* 5. 存储卷管理 */}
              {dockerSubTab === 'volumes' && (
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-2 shadow-xs">
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
                                className="hover:bg-kumo-danger/10 text-kumo-danger rounded cursor-pointer"
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
              )}

              {/* 6. 实时统计 */}
              {dockerSubTab === 'stats' && (
                <div className="bg-kumo-base border border-kumo-line rounded-lg overflow-hidden p-2 shadow-xs">
                  {dockerStats.length === 0 ? (
                    <div className="p-12 text-center text-xs text-kumo-subtle">当前主机中未检索到 Docker 资源统计</div>
                  ) : (
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
                            <Table.Cell className="p-2.5 font-bold text-kumo-strong truncate" title={stat.container_id || stat.name}>{stat.name || stat.container_id || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-kumo-success">{stat.cpu_percent || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-kumo-default">{stat.mem_usage || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-kumo-info">{stat.mem_percent || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 font-mono text-[11px] text-kumo-subtle truncate">{stat.net_io || '-'}</Table.Cell>
                            <Table.Cell className="p-2.5 truncate">{stat.serverName}</Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </ScrollableTable>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}
      
      {/* ==================== 4. 后台管理 ==================== */}
      {serverCurrentTab === 'management' && (
        <div className="flex flex-col gap-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            
            {/* 列表大配置 */}
            <div className="bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-4 shadow-xs">
              <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-2 mb-1">
                ⚙️ 主机自动监控配置
              </h3>
              
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">
                  自动采集间隔 (周期)
                </label>
                <div className="flex flex-wrap gap-2">
                  {[1, 2, 5, 10, 15, 30, 60].map(m => (
                    <Button size="sm"
                      variant={metricsCollectInterval === m ? 'primary' : 'secondary'}
                      key={m}
                      onClick={() => updateMetricsCollectInterval(m)}
                      className={`px-3 py-1.5 border rounded-lg text-xs font-semibold cursor-pointer transition-colors ${metricsCollectInterval === m ? 'bg-kumo-brand text-kumo-inverse border-kumo-brand' : 'bg-kumo-base border-kumo-line text-kumo-subtle hover:text-kumo-strong'}`}
                    >
                      {m}分钟
                    </Button>
                  ))}
                </div>
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-semibold text-kumo-subtle flex justify-between">
                  <span>历史数据保留期限</span>
                  <span className="text-kumo-brand">{monitorConfig.metrics_retention_days} 天</span>
                </label>
                <Input size="sm"
                  aria-label="历史数据保留期限"
                  type="range"
                  min="1"
                  max="180"
                  value={monitorConfig.metrics_retention_days}
                  onChange={e => handleRetentionSliderChange(parseInt(e.target.value))}
                  className="w-full accent-kumo-brand cursor-pointer"
                />
              </div>
            </div>
            
            {/* 批量添加 */}
            <div className="bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-3.5 shadow-xs">
              <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-2">
                📂 批量快速添加主机
              </h3>
              <Textarea
                aria-label="批量快速添加主机"
                value={serverBatchText}
                onChange={e => setServerBatchText(e.target.value)}
                placeholder="例如格式:&#10;前端服务器,192.168.1.10,22,root,密码123&#10;数据库节点,192.168.1.11,22,root,安全密码456"
                className="w-full h-24 p-2.5 border border-kumo-line rounded-lg text-xs font-mono bg-kumo-control focus:outline-none focus:border-kumo-brand"
              />
              {serverBatchError && <div className="text-xs text-kumo-danger font-bold">{serverBatchError}</div>}
              {serverBatchSuccess && <div className="text-xs text-kumo-success font-bold">{serverBatchSuccess}</div>}
              <Button size="sm"
                variant="primary"
                onClick={batchAddServers}
                disabled={serverAddingBatch}
                className="w-full bg-kumo-brand text-kumo-inverse hover:bg-kumo-brand-hover rounded-lg text-xs font-semibold cursor-pointer disabled:opacity-50"
              >
                {serverAddingBatch ? '正在同步提交...' : '确认批量录入'}
              </Button>
            </div>
          </div>
          
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            
            {/* 凭据库 */}
            <div className="md:col-span-1 bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-4 shadow-xs">
              <div className="flex items-center justify-between border-b border-kumo-line pb-2.5 mb-1">
                <h3 className="text-sm font-bold text-kumo-strong">
                  🔑 预设 SSH 凭据库
                </h3>
                <Button size="sm"
                  variant="primary"
                  onClick={() => setShowAddCredentialModal(true)}
                  className="bg-kumo-brand text-kumo-inverse rounded text-[10px] font-bold cursor-pointer"
                >
                  添加凭据
                </Button>
              </div>
              
              <div className="max-h-56 overflow-y-auto flex flex-col gap-2">
                {serverCredentials.length === 0 ? (
                  <div className="py-8 text-center text-xs text-kumo-subtle">
                    暂无预设访问凭据
                  </div>
                ) : (
                  serverCredentials.map(cred => (
                    <div key={cred.id} className={`flex items-center justify-between p-2.5 border rounded-lg bg-kumo-canvas/20 ${cred.is_default ? 'border-kumo-brand/60' : 'border-kumo-line'}`}>
                      <div className="flex flex-col">
                        <span className="text-xs font-bold text-kumo-strong flex items-center gap-1">
                          {cred.is_default && <span className="text-kumo-warning">★</span>}
                          {cred.name}
                        </span>
                        <span className="text-[10px] text-kumo-subtle font-mono">{cred.username}</span>
                      </div>
                      <div className="flex gap-1.5">
                        {!cred.is_default && (
                          <Button
                            shape="square" size="sm"
                            variant="ghost"
                            aria-label="设为默认凭据"
                            onClick={() => setDefaultCredential(cred.id)}
                            className="hover:bg-kumo-recessed rounded text-kumo-subtle cursor-pointer"
                            title="设为默认"
                          >
                            <Star className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <Button
                          shape="square" size="sm"
                          variant="ghost"
                          aria-label="删除凭据"
                          onClick={() => deleteCredential(cred.id)}
                            className="hover:bg-kumo-danger/10 text-kumo-danger rounded cursor-pointer"
                            title="删除"
                          >
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
            
            {/* 备份与导入导出 */}
            <div className="md:col-span-2 bg-kumo-base border border-kumo-line p-5 rounded-lg flex flex-col gap-4 shadow-xs">
              <h3 className="text-sm font-bold text-kumo-strong border-b border-kumo-line pb-2 mb-1">
                📥 备份 / 数据导入导出
              </h3>
              
              <div className="flex flex-col gap-3 text-xs text-kumo-subtle">
                <p>您可以通过导出功能将本地所有注册的主机信息（包含凭据、地址等）打包备份为 JSON 配置文件。在此之后，您可以在其他节点通过导入快速恢复完整的拓扑结构。</p>
                
                <div className="flex gap-3 mt-2">
                  <Button size="sm"
                    variant="secondary"
                    onClick={exportServers}
                    className="flex items-center gap-1 border border-kumo-line rounded-lg bg-kumo-base hover:bg-kumo-recessed/45 font-semibold text-kumo-strong cursor-pointer"
                  >
                    <Upload className="w-3.5 h-3.5" />
                    导出主机备份 (JSON)
                  </Button>
                  
                  <Button size="sm"
                    variant="secondary"
                    onClick={() => {
                      setImportPreview(null);
                      setImportModalError('');
                      setShowImportServerModal(true);
                    }}
                    className="flex items-center gap-1 border border-kumo-line rounded-lg bg-kumo-base hover:bg-kumo-recessed/45 font-semibold text-kumo-strong cursor-pointer"
                  >
                    <Download className="w-3.5 h-3.5" />
                    导入主机配置文件
                  </Button>
                </div>
              </div>
            </div>
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
        const isWindowsTerminal = String(activeInfo.platform || activeServer?.platform || '').toLowerCase().includes('win');
        const quickCommands = isWindowsTerminal
          ? ['dir', 'ipconfig', 'Get-Process | Select-Object -First 10', 'Get-Location']
          : ['pwd', 'ls -la', 'df -h', 'docker ps'];

        return (
          <div className="flex min-h-0 w-full flex-1 flex-col overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
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
                    <button
                      type="button"
                      onClick={() => {
                        if (visibleSessionIds.includes(sess.id)) {
                          activateSSHSession(sess.id);
                        } else {
                          switchToSSHTab(sess.id);
                        }
                      }}
                      className="flex h-full min-w-0 items-center gap-1.5 px-2 text-[11px] font-semibold"
                    >
                      <span className={`h-1.5 w-1.5 rounded-full ${sess.connected ? 'bg-kumo-success' : 'bg-kumo-warning'}`}></span>
                      <span className="max-w-28 truncate">{sess.name}</span>
                      <span className="text-[9px] uppercase text-kumo-subtle">{sess.type}</span>
                    </button>
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
                            <button
                              type="button"
                              onClick={() => activateSSHSession(id)}
                              className="flex min-w-0 items-center gap-1.5 font-semibold text-kumo-strong"
                            >
                              <TerminalIcon className="h-3.5 w-3.5 shrink-0" />
                              <span className="truncate">{slotSession?.name || 'Terminal'}</span>
                            </button>
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

                  {showServerStatusSidebar && (
                    <div className="w-52 shrink-0 border-l border-kumo-line bg-kumo-base p-2.5 text-xs">
                      <div className="mb-2.5 flex items-center justify-between border-b border-kumo-line pb-2">
                        <span className="text-[11px] font-bold text-kumo-strong">资源监控</span>
                        <Button
                          shape="square" size="sm"
                          variant="ghost"
                          icon={<X className="h-3 w-3" />}
                          aria-label="关闭资源监控"
                          title="关闭资源监控"
                          onClick={() => setShowServerStatusSidebar(false)}
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
                </div>

                <div className="grid shrink-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-x-2 gap-y-1.5 border-t border-kumo-line bg-kumo-base px-2.5 py-1.5 text-xs lg:grid-cols-[auto_minmax(0,1fr)_minmax(240px,320px)]">
                  <span className="flex h-6.5 shrink-0 items-center whitespace-nowrap text-[11px] font-semibold text-kumo-subtle">快捷命令</span>
                  <div className="-m-px min-w-0 overflow-x-auto p-px scrollbar-thin">
                    <div className="flex min-w-max items-center gap-1.5 px-px pb-1 pt-px lg:min-w-0">
                      {quickCommands.map(command => (
                        <Button
                          key={command}
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => runQuickCommand(command)}
                          disabled={!activeSSHSessionId}
                          className="shrink-0"
                          title={command}
                        >
                          <span className="block max-w-36 truncate font-mono">{command}</span>
                        </Button>
                      ))}
                    </div>
                  </div>
                  <form
                    className="col-span-2 flex min-w-0 items-center gap-1.5 lg:col-span-1"
                    onSubmit={event => {
                      event.preventDefault();
                      const command = quickCommandInput.trim();
                      if (command) runQuickCommand(command);
                    }}
                  >
                    <Input
                      size="sm"
                      aria-label="自定义快捷命令"
                      value={quickCommandInput}
                      onChange={event => setQuickCommandInput(event.target.value)}
                      placeholder="输入命令"
                      className="min-w-0 flex-1 font-mono"
                    />
                    <Button
                      type="submit"
                      size="sm"
                      variant="primary"
                      icon={<Send className="h-3.5 w-3.5" />}
                      className="shrink-0"
                      disabled={!activeSSHSessionId || !quickCommandInput.trim()}
                    >
                      执行
                    </Button>
                  </form>
                </div>

                {showSftpSidebar && (
                  <div className="h-56 shrink-0 border-t border-kumo-line bg-kumo-base p-3 text-xs">
                    <div className="mb-2 flex items-center justify-between gap-2">
                      <div className="flex min-w-0 items-center gap-2">
                        <FolderOpen className="h-4 w-4 text-kumo-subtle" />
                        <span className="font-bold text-kumo-strong">SFTP</span>
                        <span className="truncate font-mono text-[10px] text-kumo-subtle">{sftpCurrentPath}</span>
                      </div>
                      <div className="flex shrink-0 items-center gap-1.5">
                        <Button
                          type="button"
                          size="sm"
                          variant="secondary"
                          icon={<Upload className="h-3.5 w-3.5" />}
                          onClick={() => sftpUploadInputRef.current?.click()}
                          disabled={!sftpServerId || sftpUploading}
                        >
                          上传
                        </Button>
                        <input
                          ref={sftpUploadInputRef}
                          aria-label="上传 SFTP 文件"
                          type="file"
                          className="hidden"
                          onChange={handleSftpUpload}
                          multiple
                        />
                        <Button
                          size="sm"
                          variant="secondary"
                          icon={<RefreshCw className="h-3.5 w-3.5" />}
                          onClick={() => loadSftpDirectory(sftpServerId, sftpCurrentPath)}
                          loading={sftpLoading || sftpUploading}
                        >
                          刷新
                        </Button>
                        <Button
                          shape="square" size="sm"
                          variant="ghost"
                          icon={<X className="h-3 w-3" />}
                          aria-label="关闭 SFTP"
                          title="关闭 SFTP"
                          onClick={() => setShowSftpSidebar(false)}
                        />
                      </div>
                    </div>
                    <div className="mb-2 flex items-center gap-1.5 overflow-x-auto whitespace-nowrap text-[10px] scrollbar-thin">
                      {sftpBreadcrumbs.map((crumb, idx) => (
                        <React.Fragment key={crumb.path}>
                          <button
                            type="button"
                            onClick={() => loadSftpDirectory(sftpServerId, crumb.path)}
                            className="rounded px-1 py-0.5 font-semibold text-kumo-subtle hover:bg-kumo-recessed hover:text-kumo-strong"
                          >
                            {crumb.name}
                          </button>
                          {idx < sftpBreadcrumbs.length - 1 && <span className="opacity-40">/</span>}
                        </React.Fragment>
                      ))}
                    </div>
                    <div className="grid max-h-36 grid-cols-2 gap-1.5 overflow-y-auto pr-1 scrollbar-thin md:grid-cols-4 xl:grid-cols-6">
                      {sftpLoading ? (
                        <div className="col-span-full py-8 text-center text-[10px] text-kumo-subtle">读取远程目录中...</div>
                      ) : sftpError ? (
                        <div className="col-span-full rounded-md border border-kumo-danger/30 bg-kumo-danger/10 p-2 text-[10px] text-kumo-danger">{sftpError}</div>
                      ) : sftpFiles.length === 0 ? (
                        <div className="col-span-full py-8 text-center text-[10px] text-kumo-subtle">当前目录为空</div>
                      ) : (
                        sftpFiles.map(file => (
                          <button
                            key={file.path}
                            type="button"
                            onClick={() => handleSftpFileClick(file)}
                            className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-kumo-line bg-kumo-recessed/35 px-2 py-1.5 text-left hover:border-kumo-brand/60 hover:bg-kumo-recessed"
                          >
                            <span className="flex min-w-0 items-center gap-1.5">
                              {file.isDirectory ? <Folder className="h-3.5 w-3.5 shrink-0" /> : <FileText className="h-3.5 w-3.5 shrink-0" />}
                              <span className="truncate text-[11px] font-semibold text-kumo-strong" title={file.name}>{file.name}</span>
                            </span>
                            <span className="shrink-0 text-[9px] text-kumo-subtle">{file.isDirectory ? '目录' : formatFileSize(file.size)}</span>
                          </button>
                        ))
                      )}
                    </div>
                  </div>
                )}
              </div>

              <div className="flex w-11 shrink-0 flex-col items-center gap-3 border-l border-kumo-line bg-kumo-base py-3 text-kumo-subtle">
                <Button
                  shape="square" size="sm"
                  variant={showServerStatusSidebar ? 'secondary' : 'ghost'}
                  icon={<Activity className="h-4 w-4" />}
                  aria-label="资源监控"
                  title="资源监控"
                  onClick={() => setShowServerStatusSidebar(prev => !prev)}
                />
                <Button
                  shape="square" size="sm"
                  variant={showSftpSidebar ? 'secondary' : 'ghost'}
                  icon={<FolderOpen className="h-4 w-4" />}
                  aria-label="SFTP 文件浏览"
                  title="SFTP 文件浏览"
                  onClick={() => {
                    setShowSftpSidebar(prev => !prev);
                    if (!showSftpSidebar && activeSSHSessionId) {
                      const serverId = sshSessions.find(s => s.id === activeSSHSessionId)?.server.id;
                      if (serverId) loadSftpDirectory(serverId, '.');
                    }
                  }}
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
        <Dialog size="lg" className="flex max-h-[85vh] flex-col overflow-hidden p-0">
            <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <Dialog.Title className="text-sm font-bold text-kumo-strong">
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
                  />
                )}
              />
            </div>
            
            <div className="p-4 flex-1 overflow-y-auto max-h-[70vh] flex flex-col gap-4 text-xs">
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
              <div className="grid grid-cols-2 gap-4">
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">主机名称 (别名)</label>
                  <Input size="sm"
                    aria-label="主机名称"
                    type="text"
                    value={serverForm.name}
                    onChange={e => setServerForm(prev => ({ ...prev, name: e.target.value }))}
                    placeholder="生产数据库-01"
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">地区 / 归属国家 (Flags)</label>
                  <Select size="sm"
                    aria-label="地区归属国家"
                    value={serverForm.country}
                    onValueChange={(value) => setServerForm(prev => ({ ...prev, country: String(value) }))}
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control focus:outline-none focus:border-kumo-brand"
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
              </div>
              
              <div className="grid grid-cols-3 gap-4">
                <div className="col-span-2 flex flex-col gap-1.5">
                  <label className="font-semibold text-kumo-subtle">连接地址 (IP / Host)</label>
                  <Input size="sm"
                    aria-label="连接地址"
                    type="text"
                    value={serverForm.host}
                    onChange={e => setServerForm(prev => ({ ...prev, host: e.target.value }))}
                    placeholder="12.34.56.78"
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
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
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                  />
                </div>
              </div>
              
              <div className="flex flex-col gap-2">
                <label className="font-semibold text-kumo-subtle">选择凭据预设进行快速填充</label>
                <Select size="sm"
                  aria-label="选择凭据预设"
                  value={selectedCredentialId}
                  onValueChange={(value) => applyCredential(String(value))}
                  placeholder="-- 手动录入 --"
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control focus:outline-none"
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
                <div className="grid grid-cols-2 gap-4">
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">登录用户名</label>
                    <Input size="sm"
                      aria-label="登录用户名"
                      type="text"
                      value={serverForm.username}
                      onChange={e => setServerForm(prev => ({ ...prev, username: e.target.value }))}
                      placeholder="root"
                      className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <label className="font-semibold text-kumo-subtle">身份验证方案</label>
                    <div className="flex gap-2 py-1">
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
                      className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
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
                        className="w-full h-20 p-2 border border-kumo-line rounded-lg text-xs font-mono bg-kumo-control focus:outline-none focus:border-kumo-brand"
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
                        className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
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
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none focus:border-kumo-brand"
                />
              </div>
              
                </>
              ) : (
                <div className="flex flex-col gap-4">
                  <Input
                    label="Host name" size="sm"
                    value={quickDeployName}
                    onChange={(e) => setQuickDeployName(e.target.value)}
                    placeholder="prod-agent-01"
                  />

                  <div className="rounded-lg border border-kumo-line bg-kumo-recessed/35 p-3 text-[11px] leading-relaxed text-kumo-subtle">
                    Agent mode creates or reuses a host record, then returns install commands for the target machine.
                  </div>

                  {quickDeployResult && (
                    <div className="flex flex-col gap-3">
                      <Select
                        label="Install target" size="sm"
                        value={agentInstallOS}
                        onValueChange={setAgentInstallOS}
                        items={[
                          { value: 'linux', label: 'Linux / macOS' },
                          { value: 'windows', label: 'Windows PowerShell' },
                        ]}
                      />
                      <ClipboardText
                        size="sm"
                        text={agentInstallOS === 'linux' ? quickDeployResult.installCommand || '' : quickDeployResult.winInstallCommand || ''}
                        className="w-full"
                        tooltip={{ text: 'Copy command', copiedText: 'Install command copied', side: 'top' }}
                        labels={{ copyAction: 'Copy install command' }}
                      />
                      <div className="grid grid-cols-2 gap-2 text-[11px] text-kumo-subtle">
                        <div className="rounded-md border border-kumo-line bg-kumo-base p-2">
                          <div className="font-semibold text-kumo-strong">Server ID</div>
                          <div className="mt-1 font-mono">{quickDeployResult.serverId}</div>
                        </div>
                        <div className="rounded-md border border-kumo-line bg-kumo-base p-2">
                          <div className="font-semibold text-kumo-strong">API URL</div>
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
            
            <div className="bg-kumo-recessed/25 px-4 py-3 border-t border-kumo-line flex justify-end gap-2.5">
              {serverModalMode === 'add' && serverAddMode === 'agent' ? (
                <>
                  <Button
                    type="button" size="sm"
                    variant="primary"
                    loading={serverModalSaving}
                    onClick={generateQuickInstallCommand}
                  >
                    Generate Agent command
                  </Button>
                </>
              ) : null}
              <Button size="sm"
                variant="secondary"
                onClick={testServerConnection}
                disabled={serverModalSaving}
                className={`px-3.5 py-1.5 border border-kumo-line rounded-lg text-xs font-semibold hover:bg-kumo-recessed cursor-pointer ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                连接测试
              </Button>
              <Button size="sm"
                variant="primary"
                onClick={saveServer}
                disabled={serverModalSaving}
                className={`px-4 py-1.5 bg-kumo-brand text-kumo-inverse hover:bg-kumo-brand-hover rounded-lg text-xs font-bold cursor-pointer ${serverModalMode === 'add' && serverAddMode === 'agent' ? 'hidden' : ''}`}
              >
                {serverModalSaving ? '保存中...' : '确认保存'}
              </Button>
            </div>
        </Dialog>
      </Dialog.Root>
      
      {/* ==================== 模态框: 凭据预设新增 ==================== */}
      <Dialog.Root open={showAddCredentialModal} onOpenChange={setShowAddCredentialModal}>
        <Dialog className="flex max-h-[85vh] flex-col overflow-hidden p-0">
            <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <Dialog.Title className="text-sm font-bold text-kumo-strong">
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
                  />
                )}
              />
            </div>
            
            <div className="p-4 flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">凭据别名</label>
                <Input size="sm"
                  aria-label="凭据别名"
                  type="text"
                  value={credForm.name}
                  onChange={e => setCredForm(prev => ({ ...prev, name: e.target.value }))}
                  placeholder="美国节点通用 root 秘钥"
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
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
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                />
              </div>
              
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">登录凭据模式</label>
                <Select size="sm"
                  aria-label="登录凭据模式"
                  value={credForm.auth_type}
                  onValueChange={(value) => setCredForm(prev => ({ ...prev, auth_type: String(value) }))}
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control"
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
                    className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
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
                      className="w-full h-24 p-2 border border-kumo-line rounded-lg text-xs font-mono bg-kumo-control focus:outline-none"
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
                      className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control text-kumo-strong focus:outline-none"
                    />
                  </div>
                </div>
              )}
            </div>
            
            <div className="bg-kumo-recessed/25 px-4 py-3 border-t border-kumo-line flex justify-end gap-2 text-xs">
              <Button size="sm" variant="secondary" onClick={() => setShowAddCredentialModal(false)} className="border border-kumo-line rounded-lg cursor-pointer">取消</Button>
              <Button size="sm" variant="primary" onClick={addCredential} className="bg-kumo-brand text-kumo-inverse rounded-lg font-bold cursor-pointer">确认保存</Button>
            </div>
        </Dialog>
      </Dialog.Root>
      
      {/* ==================== 模态框: 导入主机备份 ==================== */}
      <Dialog.Root open={showImportServerModal} onOpenChange={setShowImportServerModal}>
        <Dialog className="flex max-h-[85vh] flex-col overflow-hidden p-0">
            <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
              <Dialog.Title className="text-sm font-bold text-kumo-strong">
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
                  />
                )}
              />
            </div>
            
            <div className="p-4 flex flex-col gap-4 text-xs">
              <div className="flex flex-col gap-1.5">
                <label className="font-semibold text-kumo-subtle font-medium">选择备份 JSON 文件</label>
                <Input size="sm"
                  aria-label="选择备份 JSON 文件"
                  type="file"
                  onChange={e => {
                    const f = e.target.files[0];
                    if (f) processImportFile(f);
                  }}
                  className="px-3 py-2 border border-kumo-line rounded-lg bg-kumo-control"
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
            
            <div className="bg-kumo-recessed/25 px-4 py-3 border-t border-kumo-line flex justify-end gap-2 text-xs">
              <Button size="sm" variant="secondary" onClick={() => setShowImportServerModal(false)} className="border border-kumo-line rounded-lg cursor-pointer">取消</Button>
              <Button size="sm"
                variant="primary"
                onClick={confirmImportServers}
                disabled={importModalSaving || !importPreview}
                className="bg-kumo-brand text-kumo-inverse rounded-lg font-bold cursor-pointer disabled:opacity-50"
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
        <Dialog size="lg" className="flex max-h-[88vh] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="text-sm font-bold text-kumo-strong">
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
                        className="w-full"
                        listClassName="w-full"
                        value={agentInstallProtocol}
                        onValueChange={setAgentInstallProtocol}
                        tabs={[
                          { value: 'https', label: 'HTTPS' },
                          { value: 'http', label: 'HTTP' },
                        ]}
                      />
                      <Tabs
                        {...TOOL_TABS_PROPS}
                        className="w-full"
                        listClassName="w-full"
                        value={agentInstallHostType}
                        onValueChange={setAgentInstallHostType}
                        tabs={[
                          { value: 'domain', label: '域名' },
                          { value: 'ip', label: 'IP' },
                        ]}
                      />
                      <Tabs
                        {...TOOL_TABS_PROPS}
                        className="w-full"
                        listClassName="w-full"
                        value={agentInstallOS}
                        onValueChange={setAgentInstallOS}
                        tabs={[
                          { value: 'linux', label: 'Linux' },
                          { value: 'windows', label: 'Windows' },
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
                    text={agentInstallOS === 'linux'
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
            <div className="flex flex-wrap items-center justify-end gap-3">
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
        <Dialog size="xl" className="flex max-h-[88vh] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="text-sm font-bold text-kumo-strong">
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
                  <div key={server.id} className="rounded-md border border-kumo-line bg-kumo-base p-2">
                    <Checkbox
                      label={
                        <span className="inline-flex min-w-0 items-center gap-1.5">
                          {getFlagCountry(server) && (
                            <img
                              src={`https://flagcdn.com/w20/${getFlagCountry(server).toLowerCase()}.png`}
                              className="h-3 w-4 rounded-xs object-cover"
                              alt={getFlagCountry(server)}
                            />
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
            <div className="flex justify-end gap-2">
              <Button
                type="button" size="sm"
                variant="secondary"
                disabled={agentInstallLoading}
                onClick={() => setShowBatchAgentModal(false)}
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
        <Dialog size="lg" className="flex max-h-[88vh] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between bg-kumo-recessed/35 px-4 py-3 border-b border-kumo-line">
            <Dialog.Title className="text-sm font-bold text-kumo-strong">
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
                />
              )}
            />
          </div>

          <div className="flex-1 overflow-y-auto p-4 text-xs text-kumo-default">
            <div className="flex flex-col gap-4">
              <div className="grid grid-cols-2 gap-2">
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
                <div className="rounded-md border border-kumo-line bg-kumo-base p-3 text-kumo-subtle">
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
            <div className="flex justify-end gap-2">
              <Button
                type="button" size="sm"
                variant="secondary"
                onClick={() => setShowUpgradeModal(false)}
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
              >
                开始升级
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 模态框: SFTP 文件编辑器 ==================== */}
      <Dialog.Root
        open={showSftpEditorModal && Boolean(sftpEditFile)}
        onOpenChange={(open) => {
          if (!open) setShowSftpEditorModal(false);
        }}
      >
        {sftpEditFile ? (
          <Dialog size="xl" className="flex h-[70vh] flex-col overflow-hidden p-0 text-xs text-kumo-default">
            <div className="flex items-center justify-between bg-kumo-recessed px-4 py-3 border-b border-kumo-line">
              <Dialog.Title className="font-bold">在线编辑: {sftpEditFile.name}</Dialog.Title>
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
                  />
                )}
              />
            </div>
            
            <div className="p-4 flex-1 flex flex-col gap-2 bg-kumo-canvas">
              <div className="text-[10px] text-kumo-subtle font-mono truncate">{sftpEditFile.path}</div>
              <Textarea
                aria-label="SFTP 文件内容"
                value={sftpEditFile.content}
                onChange={e => setSftpEditFile(prev => ({ ...prev, content: e.target.value }))}
                className="flex-1 w-full p-2.5 bg-kumo-control border border-kumo-line rounded font-mono text-xs focus:outline-none focus:border-kumo-brand text-kumo-strong resize-none"
                spellCheck={false}
              />
            </div>
            
            <div className="bg-kumo-recessed px-4 py-3 border-t border-kumo-line flex justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setShowSftpEditorModal(false)} className="border border-kumo-line rounded cursor-pointer hover:bg-kumo-fill">取消</Button>
              <Button size="sm"
                variant="primary"
                onClick={saveSftpEditedFile}
                disabled={sftpSaving}
                className="bg-kumo-brand text-kumo-inverse rounded font-bold cursor-pointer disabled:opacity-50"
              >
                {sftpSaving ? '正在写入保存...' : '保存文件'}
              </Button>
            </div>
          </Dialog>
        ) : null}
      </Dialog.Root>
    </div>
  );
}

export default ServerPage;
