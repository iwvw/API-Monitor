import { SERVER_FAST_CHART_ANIMATION_OPTIONS, SERVER_LIST_VIEW_STORAGE_KEY, SERVER_COMPACT_COLUMNS_STORAGE_KEY, HOST_COMPACT_COLUMN_IDS, HOST_COMPACT_COLUMNS, HOST_COMPACT_DEFAULT_VISIBLE_COLUMNS, LOCATION_COUNTRY_CODE_MAP } from './constants.js';
import { normalizeChartMetricRecords, normalizeMetricRecords } from '../../modules/serverChartMetrics.js';
import { areRealtimeValuesEqual } from '../../modules/serverRealtime.js';

const areServerValuesEqual = areRealtimeValuesEqual;

export const createEmptyServerStatusPageForm = () => ({
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

export const normalizeServerStatusSlug = (value, fallback = 'servers') => {
  const text = String(value || fallback).trim().toLowerCase();
  const slug = text.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return slug || fallback;
};

export const normalizeServerStatusDomain = (value) => (
  String(value || '')
    .trim()
    .replace(/^https?:\/\//i, '')
    .split('/')[0]
    .replace(/\/+$/g, '')
    .toLowerCase()
);

export const patchFastTimeseriesAnimation = (option, animationOptions = SERVER_FAST_CHART_ANIMATION_OPTIONS) => {
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

export const createFastTimeseriesEcharts = (baseEcharts, animationOptions) => (
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

export const getInitialServerListViewMode = () => {
  if (typeof window === 'undefined') return 'cards';
  const saved = window.localStorage.getItem(SERVER_LIST_VIEW_STORAGE_KEY);
  if (saved === 'compact' || saved === 'cards') return saved;
  return window.matchMedia?.('(min-width: 768px)').matches ? 'compact' : 'cards';
};

export const getInitialCompactVisibleColumns = () => {
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

export const getOSIconClass = (platform) => {
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

export const toNumber = (value, fallback = 0) => {
  if (value === null || value === undefined || value === '') return fallback;
  const parsed = typeof value === 'number' ? value : parseFloat(String(value).replace(/[^\d.-]/g, ''));
  return Number.isFinite(parsed) ? parsed : fallback;
};

export const clampPercent = (value) => Math.max(0, Math.min(100, value));

export const formatDenseFlowValue = (value) => {
  const numericValue = Number.parseFloat(String(value ?? '0').replace(/,/g, ''));
  return Number.isFinite(numericValue) ? numericValue.toFixed(1) : '0.0';
};

export const getSystemOverviewChipClassName = (kind = 'default') => {
  switch (kind) {
    case 'wide':
      return 'cq-sm:col-span-2 cq-xl:col-span-3';
    case 'medium':
      return 'cq-sm:col-span-2';
    default:
      return '';
  }
};

export const getExpandedInfoGridClassName = (dense = false) => (
  `grid min-w-0 grid-cols-1 ${dense ? 'gap-1.5' : 'gap-2'} cq-lg:grid-cols-2`
);

export const getExpandedTrendGridClassName = (compact = false, dense = false) => (
  `grid min-w-0 grid-cols-1 ${compact || dense ? 'gap-1.5' : 'gap-2'} cq-lg:grid-cols-2`
);

export const getExpandedCardSpanClassName = (index, total) => (
  index === total - 1 && total % 2 === 1 ? 'h-full cq-lg:col-span-2' : 'h-full'
);

export const inferCountryCodeFromLocation = (value) => {
  const text = String(value || '').trim();
  if (/^[a-z]{2}$/i.test(text)) return text;
  const normalized = text.toLowerCase();
  return Object.entries(LOCATION_COUNTRY_CODE_MAP).find(([name]) => normalized.includes(name))?.[1] || '';
};

export const cleanCountryDisplayCode = (value) => {
  const text = String(value || '').trim();
  if (!text || text.toLowerCase() === 'auto') return '';
  return text;
};

export const firstLocationText = (...values) => {
  for (const value of values) {
    const text = String(value || '').trim();
    if (text && text.toLowerCase() !== 'auto') return text;
  }
  return '';
};

export const firstLocationNumber = (...values) => {
  for (const value of values) {
    if (value === null || value === undefined || value === '') continue;
    const number = Number(value);
    if (Number.isFinite(number)) return number;
  }
  return undefined;
};

export const getFlagCountry = (server) => {
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

export const normalizeLocationDisplayText = (value) => {
  const text = String(value || '').trim();
  if (!text) return '';
  return /^[a-z]{2,3}$/i.test(text) ? text.toUpperCase() : text;
};

export const getServerLocationText = (server) => {
  return normalizeLocationDisplayText(
    cleanCountryDisplayCode(server.countryCode) ||
    cleanCountryDisplayCode(server.country_code) ||
    cleanCountryDisplayCode(server.info?.countryCode) ||
    cleanCountryDisplayCode(server.info?.country_code) ||
    getFlagCountry(server),
  );
};

export const getServerLocationTitle = (server) => (
  server.location ||
  server.resolved_country ||
  server.info?.location ||
  server.region ||
  server.info?.region ||
  getServerLocationText(server)
);

export const isPageVisible = () => (
  typeof document === 'undefined' || document.visibilityState === 'visible'
);

export const getMetricsSocketUrl = () => {
  const explicitUrl = import.meta.env?.VITE_METRICS_SOCKET_URL;
  if (explicitUrl) return explicitUrl;

  return '/metrics';
};

export const areServerSnapshotsEqual = (a, b) => {
  if (a === b) return true;
  if (!a || !b) return false;

  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  for (const key of keys) {
    if (!areServerValuesEqual(a[key], b[key])) return false;
  }
  return true;
};

export const formatMetricTooltipValue = (value) => {
  const number = toNumber(value, NaN);
  return Number.isFinite(number) ? number.toFixed(1) : '-';
};

export const getMetricSeries = (records, specs, options = {}) => {
  const seriesRecords = options.normalized ? records : normalizeChartMetricRecords(records);
  return specs.map(spec => ({
    name: spec.name,
    color: spec.color,
    data: seriesRecords.map(record => [record._ts, record._gap ? null : spec.value(record)]),
  }));
};

export const getLatestMetricValue = (records, valueGetter, formatter = value => String(value)) => {
  const normalized = normalizeMetricRecords(records);
  if (normalized.length === 0) return '-';
  const latest = normalized[normalized.length - 1];
  return formatter(valueGetter(latest));
};

export const formatChartTime = (timestamp) => {
  const d = new Date(timestamp);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`;
};

export const formatCompactChartTime = (timestamp) => {
  const d = new Date(timestamp);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getMinutes()).padStart(2, '0')}:${String(d.getSeconds()).padStart(2, '0')}`;
};

export const formatPercentAxis = (value) => {
  const number = toNumber(value, 0);
  return `${Number.isInteger(number) ? number : number.toFixed(1)}%`;
};

export const formatCompactPercentAxis = (value) => `${Math.round(toNumber(value, 0))}%`;

export const formatNumberAxis = (value) => {
  const number = toNumber(value, 0);
  return Number.isInteger(number) ? String(number) : number.toFixed(1);
};

export const formatCompactNumberAxis = (value) => {
  const number = toNumber(value, 0);
  const abs = Math.abs(number);
  if (abs >= 1000) return `${(number / 1000).toFixed(abs >= 10000 ? 0 : 1)}k`;
  return `${Math.round(number)}`;
};

export const formatBytesSpeed = (bytes) => {
  const value = toNumber(bytes, 0);
  if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB/s`;
  if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB/s`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB/s`;
  return `${Math.round(value)} B/s`;
};

export const formatBytesValue = (bytes) => {
  const value = Math.max(0, toNumber(bytes, 0));
  if (value >= 1024 * 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024 * 1024)).toFixed(2)} TB`;
  if (value >= 1024 * 1024 * 1024) return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  if (value >= 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${Math.round(value)} B`;
};

export const formatCompactBytesSpeed = (bytes) => {
  const value = toNumber(bytes, 0);
  const abs = Math.abs(value);
  const sign = value < 0 ? '-' : '';
  if (abs >= 1024 * 1024 * 1024) return `${sign}${(abs / (1024 * 1024 * 1024)).toFixed(abs >= 10 * 1024 * 1024 * 1024 ? 0 : 1)}G/s`;
  if (abs >= 1024 * 1024) return `${sign}${(abs / (1024 * 1024)).toFixed(abs >= 10 * 1024 * 1024 ? 0 : 1)}M/s`;
  if (abs >= 1024) return `${sign}${(abs / 1024).toFixed(abs >= 10 * 1024 ? 0 : 1)}K/s`;
  return `${Math.round(value)}B/s`;
};

export const firstPositiveNumber = (values = []) => {
  for (const value of values) {
    const parsed = toNumber(value, NaN);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  return 0;
};

export const parseTemperatureValue = (value) => {
  if (value === null || value === undefined || value === '') return null;
  const parsed = typeof value === 'number'
    ? value
    : parseFloat(String(value).replace(/[^\d.-]/g, ''));
  if (!Number.isFinite(parsed) || parsed <= 0 || parsed > 130) return null;
  return parsed;
};

export const collectTemperatureReadings = (input, parentName = '') => {
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

export const getCpuTemperatureRank = (name) => {
  const normalized = String(name || '').toLowerCase();
  if (/gpu|nvidia|radeon|nvme|ssd|hdd|disk|drive|battery|fan|ambient/.test(normalized)) return 0;
  if (/package|tctl|tdie|x86_pkg|cpu package/.test(normalized)) return 5;
  if (/\bcpu\b|cpu_thermal/.test(normalized)) return 4;
  if (/core\s*\d+|coretemp|k10temp/.test(normalized)) return 3;
  if (/thermal/.test(normalized)) return 1;
  return 0;
};

export const getGpuTemp = (record = {}) => firstPositiveNumber([
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

export const getCpuTemp = (record = {}) => {
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

export const parseSpeedToBytes = (speedStr) => {
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

export const parseMemoryUsagePercent = (metrics = {}, info = {}) => {
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

export const formatLatencyValue = (value) => {
  const ms = toNumber(value, NaN);
  if (!Number.isFinite(ms)) return '-';
  if (ms >= 1000) return `${(ms / 1000).toFixed(ms >= 10000 ? 0 : 1)}s`;
  return `${Math.round(ms)}ms`;
};

export const formatLatencyAxis = (value) => {
  const ms = toNumber(value, NaN);
  if (!Number.isFinite(ms)) return '';
  if (ms >= 1000) return `${(ms / 1000).toFixed(ms >= 10000 ? 0 : 1)}s`;
  return `${Math.round(ms)}`;
};

export const formatNetworkQualityChartTime = (timestamp) => {
  const d = new Date(timestamp);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
};

export const getNetworkQualityTone = (summary = {}) => {
  const lossRate = toNumber(summary.lossRate, 0);
  const jitterMs = toNumber(summary.jitterMs, 0);
  const avgLatency = toNumber(summary.avgLatency, 0);
  if (!summary.latest || summary.latest.success === false || lossRate >= 5 || avgLatency >= 600) return 'danger';
  if (lossRate >= 1 || jitterMs >= 120 || avgLatency >= 250) return 'warning';
  return 'success';
};

export const getNetworkQualityToneClass = (tone) => (
  tone === 'danger'
    ? 'text-kumo-danger'
    : tone === 'warning'
      ? 'text-kumo-warning'
      : 'text-kumo-success'
);
