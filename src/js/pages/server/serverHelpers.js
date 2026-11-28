import { ChartPalette } from '@cloudflare/kumo';
import { formatUptime, maskAddress, parseSpeed } from '../../modules/utils.js';
import {
  SERVER_CHART_HISTORY_LIMIT,
  SERVER_CHART_HISTORY_WINDOW_MS,
  SERVER_REALTIME_SAMPLE_INTERVAL_MS,
  getGpuMemPercent,
  normalizeChartMetricRecords,
  normalizeMetricRecords,
  toTimestamp,
} from '../../modules/serverChartMetrics.js';
import { areRealtimeValuesEqual } from '../../modules/serverRealtime.js';
import { isAgentServer } from '../../modules/serverTerminal.js';
import {
  clampPercent,
  formatBytesValue,
  getCpuTemp,
  getGpuTemp,
  getMetricSeries,
  parseMemoryUsagePercent,
  parseSpeedToBytes,
  toNumber,
} from './utils.js';

export const SERVER_METRIC_FLUSH_DELAY_MS = 350;
export const SERVER_METRIC_MIN_RENDER_INTERVAL_MS = SERVER_REALTIME_SAMPLE_INTERVAL_MS;
export const SERVER_CARD_METRICS_TTL_MS = 60 * 1000;
export const SERVER_NETWORK_QUALITY_TTL_MS = 2 * 60 * 1000;

export const getKumoToken = (tokenName, fallback) => {
  if (typeof window === 'undefined') return fallback;
  const value = window
    .getComputedStyle(document.documentElement)
    .getPropertyValue(tokenName)
    .trim();
  return value || fallback;
};

export const getKumoTerminalTheme = () => ({
  background: getKumoToken('--app-terminal-bg', getKumoToken('--color-kumo-neutral-1000', '#050505')),
  foreground: getKumoToken('--app-terminal-fg', getKumoToken('--color-kumo-neutral-50', '#f8f8f8')),
  cursor: getKumoToken('--color-brand', 'Highlight'),
});

export const DOCKER_STATS_HISTORY_LIMIT = 48;
export const DOCKER_STATS_MOCK_POINTS = 24;
export const SERVER_CHART_COALESCE_WINDOW_MS = 500;
export const SERVER_NETWORK_QUALITY_MAX_POINTS = 240;
export const SERVER_EXPAND_INTERACTION_GUARD_MS = 420;
export const EMPTY_METRIC_RECORDS = Object.freeze([]);
export const serverMetricsHistoryCache = new Map();
export const serverMetricDisplayCache = new Map();

export const areServerValuesEqual = areRealtimeValuesEqual;

export const parseGpuMemoryValue = (metrics = {}, index) => {
  if (index === 0 && metrics.gpu_mem_used !== undefined) return metrics.gpu_mem_used;
  if (index === 1 && metrics.gpu_mem_total !== undefined) return metrics.gpu_mem_total;

  const raw = metrics.gpu_mem;
  if (typeof raw !== 'string' || !raw.includes('/')) return 0;
  const parts = raw.replace(/MB/gi, '').split('/');
  return toNumber(parts[index], 0);
};

export const getCachedServerMetricHistory = serverId => {
  const cached = serverMetricsHistoryCache.get(String(serverId));
  return cached && cached.length > 0 ? cached : null;
};

export const mergeServerMetricHistory = (serverId, ...recordGroups) => {
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
      const ts = toTimestamp(
        record._ts || record.recorded_at || record.timestamp || record.time,
        NaN
      );
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

export const buildMetricHistoryRecord = (metrics = {}, info = {}, timestamp = Date.now()) => {
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
    gpu_usage:
      metrics.gpu_usage !== undefined
        ? toNumber(metrics.gpu_usage, 0)
        : typeof metrics.gpu === 'number'
          ? metrics.gpu
          : info?.gpu
            ? toNumber(info.gpu.Usage, 0)
            : null,
    gpu_mem_percent: getGpuMemPercent({
      gpu_mem_percent:
        metrics.gpu_mem_percent !== undefined ? metrics.gpu_mem_percent : info?.gpu?.Percent,
      gpu_mem_used: gpuMemUsed,
      gpu_mem_total: gpuMemTotal,
    }),
    gpu_mem_used: gpuMemUsed,
    gpu_mem_total: gpuMemTotal,
    gpu_power:
      metrics.gpu_power !== undefined
        ? toNumber(metrics.gpu_power, 0)
        : info?.gpu
          ? toNumber(info.gpu.Power, 0)
          : 0,
    gpu_temp: getGpuTemp({ ...metrics, gpu: { ...infoGpu, ...metricGpu } }),
    net_rx: parseSpeedToBytes(metrics.network?.rx_speed),
    net_tx: parseSpeedToBytes(metrics.network?.tx_speed),
    _ts: ts,
  };
};

export const normalizeByteText = (value, fallback = '0 B') => {
  if (value === null || value === undefined) return fallback;
  const raw = String(value).trim();
  if (!raw || raw === '-' || raw.toLowerCase() === 'nan') return fallback;

  const match = raw
    .replace(/,/g, '')
    .replace(/\/s$/i, '')
    .match(/^([0-9]+(?:\.[0-9]+)?)\s*([KMGTPE]?B?)$/i);
  if (!match) return fallback;

  const amount = Number(match[1]);
  if (!Number.isFinite(amount)) return fallback;

  const unit = (match[2] || 'B').toUpperCase();
  const normalizedUnit = unit.length === 1 && unit !== 'B' ? `${unit}B` : unit;
  return `${match[1]} ${normalizedUnit}`;
};

export const getByteParts = value => {
  const text = normalizeByteText(value);
  return {
    ...parseSpeed(text),
    text,
  };
};

export const resolveCycleBytes = (totalBytes, baselineBytes) => {
  const total = toNumber(totalBytes, NaN);
  const baseline = toNumber(baselineBytes, NaN);
  if (!Number.isFinite(total)) return 0;
  if (!Number.isFinite(baseline) || baseline <= 0) return Math.max(0, total);
  // 网卡累计计数器回绕（主机重启后从 0 重新计数）时 total < baseline，
  // 直接相减会得到负数并被钳成 0，让流量永久显示 0。回绕后应以当前累计值
  // 作为新的起点重新累计，而不是显示成「没有流量」。
  // total === 0 视为本轮未采集，不能当成本期用掉全部量，保持 0。
  if (total > 0 && total < baseline) return Math.max(0, total);
  if (total < baseline) return 0;
  return Math.max(0, total - baseline);
};

export const getCycleByteParts = (totalBytes, baselineBytes) => {
  const bytes = resolveCycleBytes(totalBytes, baselineBytes);
  return {
    bytes,
    text: formatBytesValue(bytes),
    ...parseSpeed(formatBytesValue(bytes)),
  };
};

export const resolveNetworkTotalBytes = (network, direction) => {
  const rawBytes = toNumber(network?.[`${direction}_total_bytes`], NaN);
  if (Number.isFinite(rawBytes)) return Math.max(0, rawBytes);
  return Math.max(0, parseSpeedToBytes(network?.[`${direction}_total`]));
};

export const getServerCycleParts = (server = {}) => {
  const network = server?.info?.network || {};
  return {
    rx: getCycleByteParts(
      resolveNetworkTotalBytes(network, 'rx'),
      server?.traffic_cycle_baseline_rx
    ),
    tx: getCycleByteParts(
      resolveNetworkTotalBytes(network, 'tx'),
      server?.traffic_cycle_baseline_tx
    ),
  };
};

export const TRAFFIC_QUOTA_UNITS = {
  GB: 1024 * 1024 * 1024,
  TB: 1024 * 1024 * 1024 * 1024,
  PB: 1024 * 1024 * 1024 * 1024 * 1024,
};

export const TRAFFIC_CYCLE_OPTIONS = [
  { value: 'none', label: '不重置' },
  { value: 'calendar_month', label: '自然月' },
  { value: 'monthly', label: '每月指定日' },
  { value: 'custom', label: '自定义范围' },
];

export const normalizeTrafficCycleDayInput = value => {
  const day = Math.round(toNumber(value, 1));
  return Math.min(28, Math.max(1, day));
};

export const bytesToTrafficQuotaForm = bytes => {
  const value = toNumber(bytes, 0);
  if (value <= 0) return { value: '', unit: 'TB' };
  const unit =
    value >= TRAFFIC_QUOTA_UNITS.PB ? 'PB' : value >= TRAFFIC_QUOTA_UNITS.TB ? 'TB' : 'GB';
  const amount = value / TRAFFIC_QUOTA_UNITS[unit];
  return {
    value: Number.isInteger(amount)
      ? String(amount)
      : amount.toFixed(3).replace(/0+$/, '').replace(/\.$/, ''),
    unit,
  };
};

export const trafficQuotaInputToBytes = (value, unit = 'TB') => {
  const amount = toNumber(value, 0);
  if (amount <= 0) return 0;
  return Math.round(amount * (TRAFFIC_QUOTA_UNITS[unit] || TRAFFIC_QUOTA_UNITS.TB));
};

export const normalizeTrafficAlertPercentInput = value => {
  const percent = toNumber(value, 100);
  return Math.min(100, Math.max(1, percent));
};

export const getTrafficQuota = (server = {}) => {
  const network = server.info?.network || {};
  const limit = toNumber(network.traffic_limit_bytes ?? server.traffic_limit_bytes, 0);
  const rawUsed = toNumber(network.traffic_used_bytes, NaN);
  const rxBytes = toNumber(network.rx_total_bytes, NaN);
  const txBytes = toNumber(network.tx_total_bytes, NaN);
  const parsedRx = getByteParts(network.rx_total).bytes;
  const parsedTx = getByteParts(network.tx_total).bytes;
  const used = Number.isFinite(rawUsed)
    ? rawUsed
    : Number.isFinite(rxBytes) && Number.isFinite(txBytes)
      ? rxBytes + txBytes
      : parsedRx + parsedTx;
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

export const EMPTY_SERIES = [];

export const SERVER_CREDENTIAL_COLUMNS = [
  { id: 'name', role: 'primary', minWidth: 120 },
  { id: 'username', role: 'identifier', minWidth: 120 },
  { id: 'actions', role: 'actions-sm' },
];

export const NETWORK_TARGET_COLUMNS = [
  { id: 'name', role: 'primary', minWidth: 160, grow: 1 },
  { id: 'endpoint', role: 'identifier', minWidth: 200 },
  { id: 'enabled', role: 'status' },
  { id: 'actions', role: 'actions-sm' },
];
export const EMPTY_SERVER_METRIC_DISPLAY = {
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

export const getServerMetricDisplay = (serverId, metricsSource, isExpanded, isDarkMode) => {
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
    cpuMemSeries: isExpanded
      ? getMetricSeries(
          chartRecords,
          [
            { name: 'CPU (%)', color: cpuColor, value: r => toNumber(r.cpu_usage, 0) },
            { name: '内存 (%)', color: memColor, value: r => toNumber(r.mem_usage, 0) },
            { name: 'CPU 温度 (°C)', color: cpuTempColor, value: getCpuTemp },
          ],
          { normalized: true }
        )
      : EMPTY_METRIC_RECORDS,
    gpuSeries: isExpanded
      ? getMetricSeries(
          chartRecords,
          [
            { name: 'GPU', color: gpuColor, value: r => toNumber(r.gpu_usage, 0) },
            { name: 'VRAM', color: vramColor, value: getGpuMemPercent },
            { name: '功耗 (W)', color: powerColor, value: r => toNumber(r.gpu_power, 0) },
            { name: 'Temp (°C)', color: gpuTempColor, value: getGpuTemp },
          ],
          { normalized: true }
        )
      : EMPTY_METRIC_RECORDS,
    netSeries: isExpanded
      ? getMetricSeries(
          chartRecords,
          [
            { name: '上传', color: txColor, value: r => toNumber(r.net_tx, 0) },
            { name: '下载', color: rxColor, value: r => toNumber(r.net_rx, 0) },
          ],
          { normalized: true }
        )
      : EMPTY_METRIC_RECORDS,
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

export const getTempColorClass = temp => {
  const value = toNumber(temp, 0);
  if (value >= 80) return 'text-kumo-danger';
  if (value >= 65) return 'text-kumo-warning';
  if (value > 0) return 'text-kumo-success';
  return 'text-kumo-subtle';
};

export const getHostAddress = (server, mode = 'normal') => {
  const host = server?.host && server.host !== '0.0.0.0' ? server.host : server?.info?.ip;
  if (!host) return '-';
  const address = server?.host ? `${host}:${server.port || 22}` : host;
  return mode === 'normal' ? address : maskAddress(address, mode);
};

export const getServerMonitorModeLabel = (server = {}) => {
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

export const isServerOnline = (server = {}) =>
  server.status === 'online' || server.agent_online === true || server.agent_connected === true;

export const getGpuModelText = gpu => {
  if (!gpu) return '';
  if (typeof gpu === 'string') return gpu;
  if (Array.isArray(gpu)) {
    const names = gpu
      .map(item => {
        if (!item) return '';
        if (typeof item === 'string') return item;
        if (typeof item === 'object')
          return item.Model || item.model || item.name || item.Name || '';
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

export const formatResponseTime = value => {
  const ms = toNumber(value, NaN);
  return Number.isFinite(ms) && ms > 0 ? `${Math.round(ms)}ms` : '-';
};

export const isNetworkQualityUnsupportedError = (message = '') => {
  const text = String(message || '');
  return (
    text.includes('不支持的任务类型') ||
    /unsupported\s+task\s+type/i.test(text) ||
    /task\s+type:\s*40/i.test(text)
  );
};

export const buildNetworkQualitySeries = (quality, isDarkMode) => {
  const series = Array.isArray(quality?.series) ? quality.series : [];
  return series.map((target, index) => ({
    name: target.name,
    color:
      index === 0
        ? ChartPalette.categorical(1, isDarkMode)
        : index === 1
          ? ChartPalette.semantic('Success', isDarkMode)
          : index === 2
            ? ChartPalette.semantic('Warning', isDarkMode)
            : ChartPalette.categorical(index + 2, isDarkMode),
    data: (Array.isArray(target.data) ? target.data : target.points || [])
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
          point?.latency_ms === null || point?.latency_ms === undefined
            ? null
            : toNumber(point.latency_ms, null),
        ];
      })
      .filter(point => Number.isFinite(point[0])),
  }));
};

export const getPrimaryLoadValue = load => {
  const match = String(load || '').match(/-?\d+(?:\.\d+)?/);
  return match ? match[0] : '-';
};

// 负载档位:用于给负载标签上色。
// 有 CPU 核数时按「每核负载」判定(32 核机器负载 4 其实很闲),没有核数时
// 退回绝对阈值。返回 'neutral' | 'success' | 'warning' | 'danger'。
export const getLoadTone = (load, cpu) => {
  const value = Number(getPrimaryLoadValue(load));
  if (!Number.isFinite(value) || value < 0) return 'neutral';
  const cores =
    Number(cpu?.LogicalCores) || Number(cpu?.Cores) || Number(cpu?.PhysicalCores) || 0;
  if (cores > 0) {
    const ratio = value / cores;
    if (ratio >= 1) return 'danger';
    if (ratio >= 0.7) return 'warning';
    return 'success';
  }
  if (value >= 2) return 'danger';
  if (value >= 1) return 'warning';
  return 'success';
};

export const formatUptimeDaysOnly = uptime => {
  const formatted = formatUptime(uptime);
  if (!formatted || formatted === '-') return '-';
  const text = String(formatted);
  const match = text.match(/(\d+)\s*天/);
  if (match) return `${match[1]}天`;
  if (/(刚刚|时|分|秒)/.test(text)) return '0天';
  return text;
};

export const formatDateInputValue = value => {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toISOString().slice(0, 10);
};

export const normalizeExpiryInputValue = value => {
  if (!value) return null;
  const date = new Date(`${value}T23:59:59`);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
};

export const normalizeStartInputValue = value => {
  if (!value) return null;
  const date = new Date(`${value}T00:00:00`);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
};

export const formatServerRemainingTime = expiresAt => {
  if (!expiresAt) return '永久';
  const expiry = new Date(expiresAt).getTime();
  if (!Number.isFinite(expiry)) return '永久';
  const diff = expiry - Date.now();
  if (diff <= 0) return '已过期';

  return `${Math.max(1, Math.ceil(diff / 86400000))}天`;
};

export const getServerRemainingToneClass = expiresAt => {
  if (!expiresAt) return 'text-kumo-success';
  const expiry = new Date(expiresAt).getTime();
  if (!Number.isFinite(expiry)) return 'text-kumo-success';
  const diff = expiry - Date.now();
  if (diff <= 0) return 'text-kumo-danger';
  if (diff <= 7 * 86400000) return 'text-kumo-warning';
  return 'text-kumo-success';
};

export const DAY_MS = 86400000;

export const toDateMs = value => {
  if (!value) return NaN;
  const time = new Date(value).getTime();
  return Number.isFinite(time) ? time : NaN;
};

export const getServerLifecycle = (server = {}) => {
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
    : remainingMs > 0
      ? 0
      : 100;
  const remainingPercent = Number.isFinite(totalMs)
    ? clampPercent((remainingMs / totalMs) * 100)
    : remainingMs > 0
      ? 100
      : 0;
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

export const asArray = value => (Array.isArray(value) ? value : []);

export const normalizeDockerOverviewServer = (server = {}) => {
  const serverId = String(server.serverId || server.id || '');
  const resources = server.resources || {};
  const docker = server.docker || {};
  const containers =
    asArray(resources.containers).length > 0
      ? asArray(resources.containers)
      : asArray(docker.containers);
  const images = asArray(resources.images);
  const networks = asArray(resources.networks);
  const volumes = asArray(resources.volumes);
  const stats = asArray(resources.stats);
  const composeProjects = asArray(resources.composeProjects || resources.compose?.projects);
  const hasDockerResources =
    containers.length > 0 ||
    images.length > 0 ||
    networks.length > 0 ||
    volumes.length > 0 ||
    stats.length > 0 ||
    composeProjects.length > 0;

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

export const getComposeProjectName = (project = {}) =>
  project.Name || project.name || project.Project || project.project || '-';

export const normalizeDockerStringList = value => {
  if (Array.isArray(value)) {
    return value.map(item => String(item || '').trim()).filter(Boolean);
  }
  return String(value || '')
    .split(',')
    .map(item => item.trim())
    .filter(Boolean);
};

export const getComposeConfigFileList = (project = {}) =>
  normalizeDockerStringList(
    project.ConfigFiles ||
      project.configFiles ||
      project.config_files ||
      project.config_file ||
      project.configFile ||
      project.Files ||
      project.files ||
      ''
  );

export const getComposeConfigFiles = (project = {}) => getComposeConfigFileList(project).join(', ');

export const getComposePrimaryConfigFile = (project = {}) => getComposeConfigFileList(project)[0] || '';

export const getComposeWorkingDir = (project = {}) =>
  project.WorkingDir ||
  project.workingDir ||
  project.working_dir ||
  project.ProjectDir ||
  project.projectDir ||
  '-';

export const getComposeStatus = (project = {}) => String(project.Status || project.status || '-');

// 项目快照里的配置文件路径在上次扫描后可能已失效（目录被移走/重命名）。
// Agent 回传 configFiles 为空即代表该路径无法再访问。
export const isComposeConfigMissing = (project = {}) => getComposeConfigFileList(project).length === 0;

export const getDockerImageRepository = (image = {}) => {
  const repoTags = Array.isArray(image.RepoTags || image.repoTags)
    ? image.RepoTags || image.repoTags
    : [];
  const firstRepoTag = repoTags.find(Boolean) || '';
  const repoFromTag =
    firstRepoTag && firstRepoTag !== '<none>:<none>'
      ? firstRepoTag.split(':').slice(0, -1).join(':')
      : '';
  return (
    image.repository || image.Repository || image.repo || image.Repo || repoFromTag || '<none>'
  );
};

export const getDockerImageTag = (image = {}) => {
  const repoTags = Array.isArray(image.RepoTags || image.repoTags)
    ? image.RepoTags || image.repoTags
    : [];
  const firstRepoTag = repoTags.find(Boolean) || '';
  if (image.tag || image.Tag) return image.tag || image.Tag;
  if (firstRepoTag && firstRepoTag.includes(':')) return firstRepoTag.split(':').pop();
  return '-';
};

export const getDockerImageId = (image = {}) =>
  image.id || image.Id || image.ID || image.imageId || image.ImageID || '';

export const getDockerImageSize = (image = {}) =>
  image.size || image.Size || image.virtualSize || image.VirtualSize || '-';

export const getDockerNetworkName = (network = {}) => network.name || network.Name || '-';

export const getDockerNetworkId = (network = {}) =>
  network.id || network.Id || network.ID || network.networkId || '';

export const getDockerNetworkDriver = (network = {}) => network.driver || network.Driver || '-';

export const getDockerNetworkScope = (network = {}) => network.scope || network.Scope || '-';

export const getDockerVolumeName = (volume = {}) => volume.name || volume.Name || '-';

export const getDockerVolumeDriver = (volume = {}) => volume.driver || volume.Driver || '-';

export const getDockerVolumeScope = (volume = {}) => volume.scope || volume.Scope || '-';

export const formatComposeStatusLabel = (status = '') => {
  const normalized = String(status || '').trim();
  const lower = normalized.toLowerCase();
  const count = normalized.match(/\((\d+)\)/)?.[1];
  if (lower.includes('running')) return count ? `运行中 ${count} 个服务` : '运行中';
  if (lower.includes('exited') || lower.includes('stopped'))
    return count ? `已停止 ${count} 个服务` : '已停止';
  if (lower.includes('paused')) return count ? `已暂停 ${count} 个服务` : '已暂停';
  return normalized || '-';
};

export const getDockerTaskStateLabel = (state = '') => {
  const normalized = String(state || '').toLowerCase();
  if (normalized === 'success' || normalized === 'succeeded') return '成功';
  if (normalized === 'failed' || normalized === 'error') return '失败';
  if (normalized === 'timeout') return '超时';
  if (normalized === 'cancelled' || normalized === 'canceled') return '已取消';
  if (normalized === 'pending' || normalized === 'queued') return '排队中';
  if (normalized === 'running' || normalized === 'processing') return '执行中';
  return state || '执行中';
};

export const getDockerTaskActionLabel = (action = '') => {
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

export const getDockerOverviewScope = tab => {
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

export const getDockerOverviewResourceCount = (server = {}, tab = '') => {
  const resources = server.resources || {};
  if (tab === 'compose') return asArray(resources.composeProjects).length;
  if (tab === 'images') return asArray(resources.images).length;
  if (tab === 'networks') return asArray(resources.networks).length;
  if (tab === 'volumes') return asArray(resources.volumes).length;
  if (tab === 'stats') return asArray(resources.stats).length;
  return asArray(resources.containers).length;
};

export const isDockerOverviewHostVisible = (server = {}, tab = '') =>
  isServerOnline(server) &&
  (!!server.docker?.installed || getDockerOverviewResourceCount(server, tab) > 0);

export const isDockerMockPreviewEnabled = () =>
  typeof window !== 'undefined' &&
  import.meta.env?.DEV &&
  new URLSearchParams(window.location.search).has('mockDocker');

export const DOCKER_LOG_TAIL_ITEMS = [
  { value: '100', label: '100 行' },
  { value: '200', label: '200 行' },
  { value: '500', label: '500 行' },
  { value: '1000', label: '1000 行' },
];

export const createMockDockerOverview = import.meta.env.DEV
  ? () => {
      const servers = [
        {
          id: 'mock-hk',
          name: '香港',
          status: 'online',
          docker: { installed: true },
          resources: {
            containers: [
              {
                id: 'ca0c6b6aced2',
                name: 'siyuan1',
                image: 'demoshang/siyuan:latest',
                state: 'running',
                status: 'Up 3 days',
                ports: '-',
              },
              {
                id: '76cb1d7413f9',
                name: 'siyuan',
                image: 'demoshang/siyuan:latest',
                state: 'running',
                status: 'Up 9 days',
                ports: '6806:6806/tcp',
              },
              {
                id: 'd47c255e2d48',
                name: 'api-monitor',
                image: 'iwvw/api-monitor:dev',
                state: 'running',
                status: 'Up 2 hours',
                ports: '3000:3000/tcp',
              },
              {
                id: '3b671d99cb57',
                name: 'vertex2api',
                image: 'iwvw/vertex2api:main',
                state: 'running',
                status: 'Up 12 hours',
                ports: '2156:2156/tcp',
              },
              {
                id: '9561e5097b35',
                name: 'mongo',
                image: 'mongo:latest',
                state: 'running',
                status: 'Up 22 days',
                ports: '27017/tcp',
              },
              {
                id: 'b201e1bcdd78',
                name: 'openresty',
                image: '1panel/openresty:1.29.2.5-0-noble',
                state: 'running',
                status: 'Up 22 days',
                ports: '80:80/tcp, 443:443/tcp',
              },
              {
                id: '6679aee4b9d1',
                name: 'halowebui',
                image: 'ghcr.io/ztx888/halowebui:slim',
                state: 'running',
                status: 'Up 8 days',
                ports: '3770:8080/tcp',
              },
              {
                id: 'a2d735c4b8f0',
                name: 'wordai',
                image: 'iwvw/wordai:latest',
                state: 'running',
                status: 'Up 5 days',
                ports: '8380:80/tcp',
              },
              {
                id: 'a8c2ff1900df',
                name: 'uptime-kuma',
                image: 'louislam/uptime-kuma:2',
                state: 'running',
                status: 'Up 17 days',
                ports: '3001:3001/tcp',
              },
              {
                id: 'c782442c1f12',
                name: 'redis',
                image: 'redis:alpine',
                state: 'running',
                status: 'Up 17 days',
                ports: '6379/tcp',
              },
              {
                id: 'ea19ab776a20',
                name: 'mx-server',
                image: 'innei/mx-server:11.0.5',
                state: 'running',
                status: 'Up 4 days',
                ports: '2333:2333/tcp',
              },
              {
                id: '93ab2010df58',
                name: 'shiro',
                image: 'innei/shiroi:latest',
                state: 'paused',
                status: 'Paused',
                ports: '2323:2323/tcp',
              },
              {
                id: 'b7e2f4a91c05',
                name: 'komidash',
                image: 'komidash:local',
                state: 'running',
                status: 'Up 6 hours',
                ports: '8899:80/tcp',
              },
            ],
            images: [
              {
                id: 'sha256:api',
                repository: 'iwvw/api-monitor',
                tag: 'dev',
                size: '218MB',
                created: '2026-07-04',
              },
              {
                id: 'sha256:redis',
                repository: 'redis',
                tag: 'alpine',
                size: '42MB',
                created: '2026-06-29',
              },
            ],
            networks: [
              { id: 'net-front', name: 'frontend', driver: 'bridge', scope: 'local' },
              { id: 'net-db', name: 'database', driver: 'bridge', scope: 'local' },
            ],
            volumes: [
              {
                name: 'mongo_data',
                driver: 'local',
                mountpoint: '/var/lib/docker/volumes/mongo_data/_data',
              },
              {
                name: 'kuma_data',
                driver: 'local',
                mountpoint: '/var/lib/docker/volumes/kuma_data/_data',
              },
            ],
            stats: [
              {
                container_id: 'd47c255e2d48',
                name: 'api-monitor',
                cpu_percent: '1.8%',
                mem_usage: '122MiB / 1.9GiB',
                mem_percent: '6.2%',
                net_io: '12MB / 8MB',
                block_io: '55MB / 4MB',
              },
              {
                container_id: '9561e5097b35',
                name: 'mongo',
                cpu_percent: '0.7%',
                mem_usage: '346MiB / 1.9GiB',
                mem_percent: '17.8%',
                net_io: '4MB / 5MB',
                block_io: '210MB / 40MB',
              },
            ],
            composeProjects: [
              {
                Name: 'edge-stack',
                Status: 'running(4)',
                ConfigFiles: '/opt/1panel/docker/compose/api-monitor/docker-compose.yml',
                WorkingDir: '/opt/1panel/docker/compose/api-monitor',
              },
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
              {
                id: 'f1c1f2a10111',
                name: 'gateway',
                image: 'nginx:1.27-alpine',
                state: 'running',
                status: 'Up 4 days',
                ports: '8080:80/tcp',
              },
              {
                id: 'f1c1f2a10222',
                name: 'worker',
                image: 'iwvw/worker:main',
                state: 'running',
                status: 'Up 2 days',
                ports: '-',
              },
              {
                id: 'f1c1f2a10333',
                name: 'archive-db',
                image: 'postgres:16-alpine',
                state: 'exited',
                status: 'Exited (0) 3 hours ago',
                ports: '5432/tcp',
              },
            ],
            images: [
              {
                id: 'sha256:nginx-sg',
                repository: 'nginx',
                tag: '1.27-alpine',
                size: '74MB',
                created: '2026-07-02',
              },
              {
                id: 'sha256:worker-sg',
                repository: 'iwvw/worker',
                tag: 'main',
                size: '156MB',
                created: '2026-07-03',
              },
            ],
            networks: [{ id: 'net-edge-sg', name: 'edge', driver: 'bridge', scope: 'local' }],
            volumes: [
              {
                name: 'archive_pgdata',
                driver: 'local',
                mountpoint: '/var/lib/docker/volumes/archive_pgdata/_data',
              },
            ],
            stats: [
              {
                container_id: 'f1c1f2a10111',
                name: 'gateway',
                cpu_percent: '0.9%',
                mem_usage: '86MiB / 1.9GiB',
                mem_percent: '4.4%',
                net_io: '6MB / 9MB',
                block_io: '18MB / 7MB',
              },
              {
                container_id: 'f1c1f2a10222',
                name: 'worker',
                cpu_percent: '1.4%',
                mem_usage: '214MiB / 1.9GiB',
                mem_percent: '11.1%',
                net_io: '8MB / 5MB',
                block_io: '72MB / 13MB',
              },
            ],
            composeProjects: [],
          },
        },
      ];

      const withDoubledItems = (items, clone) => {
        const list = asArray(items);
        return [...list, ...list.map((item, index) => clone(item, index))];
      };

      const doubledServers = servers.map(server => ({
        ...server,
        resources: {
          ...server.resources,
          containers: withDoubledItems(server.resources?.containers, container => ({
            ...container,
            id: `${container.id}2`,
            name: `${container.name}-2`,
          })),
          images: withDoubledItems(server.resources?.images, image => ({
            ...image,
            id: `${image.id}-2`,
            tag: `${image.tag}-2`,
          })),
          networks: withDoubledItems(server.resources?.networks, network => ({
            ...network,
            id: `${network.id}-2`,
            name: `${network.name}-2`,
          })),
          volumes: withDoubledItems(server.resources?.volumes, volume => ({
            ...volume,
            name: `${volume.name}-2`,
          })),
          stats: withDoubledItems(server.resources?.stats, stat => ({
            ...stat,
            container_id: `${stat.container_id}2`,
            name: `${stat.name}-2`,
          })),
          composeProjects: withDoubledItems(server.resources?.composeProjects, project => ({
            ...project,
            Name: `${project.Name}-2`,
            ConfigFiles: String(project.ConfigFiles || '').replace(/(\.ya?ml)$/, '-2$1'),
            WorkingDir: `${project.WorkingDir}-2`,
          })),
        },
      }));

      const updateChecks = [
        {
          serverId: 'mock-hk',
          containerId: 'd47c255e2d48',
          containerName: 'api-monitor',
          image: 'iwvw/api-monitor:dev',
          currentDigest: 'sha256:local-api',
          latestDigest: 'sha256:remote-api',
          hasUpdate: true,
        },
        {
          serverId: 'mock-hk',
          containerId: 'b7e2f4a91c05',
          containerName: 'komidash',
          image: 'komidash:local',
          currentDigest: 'sha256:local-komidash',
          latestDigest: '',
          hasUpdate: false,
          skipped: true,
        },
        {
          serverId: 'mock-hk',
          containerId: '9561e5097b35',
          containerName: 'mongo',
          image: 'mongo:latest',
          currentDigest: 'sha256:local-mongo',
          latestDigest: 'sha256:remote-mongo',
          hasUpdate: true,
        },
        {
          serverId: 'mock-hk',
          containerId: 'ca0c6b6aced2',
          containerName: 'siyuan1',
          image: 'demoshang/siyuan:latest',
          currentDigest: 'sha256:siyuan',
          latestDigest: 'sha256:siyuan',
          hasUpdate: false,
        },
        {
          serverId: 'mock-sg',
          containerId: 'f1c1f2a10222',
          containerName: 'worker',
          image: 'iwvw/worker:main',
          currentDigest: 'sha256:local-worker',
          latestDigest: 'sha256:remote-worker',
          hasUpdate: true,
        },
      ];

      const doubledUpdateChecks = updateChecks.flatMap(check => [
        check,
        { ...check, containerId: `${check.containerId}2` },
      ]);

      return {
        servers: doubledServers.map(normalizeDockerOverviewServer),
        updateChecks: doubledUpdateChecks,
      };
    }
  : () => ({ servers: [], updateChecks: [] });

export const hasServerDockerInstalled = (server = {}) => {
  const docker = server.info?.docker || {};
  const containers = asArray(docker.containers);
  return (
    isServerOnline(server) &&
    (!!docker.installed ||
      containers.length > 0 ||
      toNumber(docker.runningCount ?? docker.running, 0) > 0 ||
      toNumber(docker.stoppedCount ?? docker.stopped, 0) > 0)
  );
};

export const getDockerStatName = (stat = {}) =>
  stat.name ||
  stat.Name ||
  stat.container_name ||
  stat.container ||
  stat.container_id ||
  stat.ID ||
  '-';

export const getDockerStatCpuPercent = (stat = {}) =>
  clampPercent(toNumber(stat.cpu_percent ?? stat.CPUPerc, 0));

export const getDockerStatMemPercent = (stat = {}) =>
  clampPercent(toNumber(stat.mem_percent ?? stat.MemPerc, 0));

export const getDockerStatMemUsage = (stat = {}) => stat.mem_usage || stat.MemUsage || '-';

export const getDockerStatNetIo = (stat = {}) => stat.net_io || stat.NetIO || '-';

export const getDockerStatBlockIo = (stat = {}) => stat.block_io || stat.BlockIO || '-';

export const parseDockerByteValue = value => {
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

export const parseDockerIoPair = value => {
  const [first = '', second = ''] = String(value || '').split('/');
  return [parseDockerByteValue(first), parseDockerByteValue(second)];
};

export const getDockerStatKey = (stat = {}) =>
  String(stat.serverId || '') +
  ':' +
  String(stat.container_id || stat.ID || stat.id || getDockerStatName(stat));

export const normalizeDockerStatSnapshot = (stat = {}, index = 0, timestamp = Date.now()) => {
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

export const createDockerStatsSeedHistory = (stats = [], timestamp = Date.now()) => {
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
        blockRead:
          base.blockRead * Math.max(0.2, 0.88 + Math.sin((pointIndex + statIndex) * 0.35) * 0.12),
        blockWrite:
          base.blockWrite * Math.max(0.2, 0.86 + Math.cos((pointIndex + statIndex) * 0.4) * 0.14),
      };
    });
  });
};

export const buildDockerStatSeries = (history = [], metric, isDarkMode) => {
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
  return Array.from(seriesMap.values()).filter(series =>
    series.data.some(([, value]) => toNumber(value, 0) > 0)
  );
};

export const buildDockerPairSeries = (history = [], metrics) =>
  metrics
    .map(metric => ({
      name: metric.name,
      color: metric.color,
      data: history.map(snapshot => {
        const total = snapshot.reduce((sum, item) => sum + toNumber(item[metric.key], 0), 0);
        return [snapshot[0]?.timestamp || Date.now(), total];
      }),
    }))
    .filter(series => series.data.some(([, value]) => value > 0));

export const matchContainerAutoUpdateLabel = (labels, spec) => {
  const normalized = String(spec || '').trim();
  if (!normalized || !labels || typeof labels !== 'object') return false;
  const idx = normalized.indexOf('=');
  const key = (idx >= 0 ? normalized.slice(0, idx) : normalized).trim();
  const value = idx >= 0 ? normalized.slice(idx + 1).trim() : '';
  if (!key) return false;
  const got = labels[key];
  if (got === undefined || got === null) return false;
  return value === '' || String(got) === value;
};

export const getContainerAutoUpdateBadge = result => {
  if (!result) return { variant: 'neutral', label: '自动更新', title: '命中自动更新策略' };
  if (result.status === 'updated')
    return { variant: 'success', label: '已自动更新', title: result.message || '' };
  if (result.status === 'failed')
    return { variant: 'error', label: '自动更新失败', title: result.message || '' };
  if (result.status === 'up-to-date')
    return { variant: 'neutral', label: '自动·已最新', title: result.message || '' };
  return { variant: 'neutral', label: '自动·跳过', title: result.message || '' };
};
