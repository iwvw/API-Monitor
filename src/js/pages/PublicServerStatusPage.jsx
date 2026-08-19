import React, { useCallback, useEffect, useMemo, useState } from 'react';
import io from 'socket.io-client';
import { Meter, Tabs } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AlertTriangle, Globe, Home, LogIn, RefreshCw, Shield } from '../components/Icons.jsx';
import PublicPageIconPicker from '../components/public/PublicPageIconPicker.jsx';
import useStore from '../store.js';
import CountryFlag from '../components/CountryFlag.jsx';
import ServerLocationMap from '../components/server/ServerLocationMap.jsx';
import { useCloudflareSpotlight } from '../hooks/useCloudflareSpotlight.js';
import { FLOW_UNIT_BADGE_CLASS, getFlowUnitClassName } from '../modules/flowUnits.js';
import {
  getPublicPageFaviconHref,
  swapPublicPageFavicon,
  withPublicPageIconId,
} from '../modules/publicPageBranding.js';
import { toast } from '../modules/toast.js';
import { normalizeTrafficLimitMode, resolveTrafficUsedBytes } from '../modules/trafficMetrics.js';
import { TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import * as echarts from 'echarts/core';
import { MapChart, ScatterChart } from 'echarts/charts';
import { TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';

echarts.use([
  MapChart,
  ScatterChart,
  TooltipComponent,
  CanvasRenderer,
]);

const COLUMN_TABS = [
  { value: '3', label: '3列' },
  { value: '4', label: '4列' },
];

const normalizePublicPath = () => {
  const path = window.location.pathname.replace(/\/+$/, '');
  const match = path.match(/^\/(?:servers|s)\/([^/]+)$/);
  return match ? decodeURIComponent(match[1]) : '';
};

const formatDateTime = (value) => {
  if (!value) return '尚未更新';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '尚未更新';
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
};

const formatBytes = (value) => {
  const bytes = Number(value) || 0;
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let next = bytes / 1024;
  let index = 0;
  while (next >= 1024 && index < units.length - 1) {
    next /= 1024;
    index += 1;
  }
  return `${next.toFixed(next >= 10 ? 1 : 2)} ${units[index]}`;
};

const formatCompactBytes = (value) => formatBytes(value).replace(/\s/g, '');
const formatSpeed = (value) => `${formatBytes(value)}/s`;
const formatPercent = (value) => `${Math.round(Math.max(0, Math.min(100, Number(value) || 0)))}%`;
const hasValue = (value) => value !== null && value !== undefined && value !== '' && Number(value) !== 0;

const toNumber = (value, fallback = null) => {
  if (value === null || value === undefined || value === '') return fallback;
  if (typeof value === 'number') return Number.isFinite(value) ? value : fallback;
  const parsed = parseFloat(String(value).replace(/[^\d.-]/g, ''));
  return Number.isFinite(parsed) ? parsed : fallback;
};

const firstNumber = (source, keys, fallback = 0) => {
  for (const key of keys) {
    if (!Object.prototype.hasOwnProperty.call(source || {}, key)) continue;
    const value = toNumber(source[key], null);
    if (value !== null) return value;
  }
  return fallback;
};

const firstText = (source, keys, fallback = '') => {
  for (const key of keys) {
    const value = source?.[key];
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return fallback;
};

const cleanCountryDisplayCode = (value) => {
  const text = String(value || '').trim();
  if (!text || text.toLowerCase() === 'auto') return '';
  return text;
};

const mergeRealtimeMetrics = (server, metrics, timestamp) => {
  const network = metrics?.network && typeof metrics.network === 'object' ? metrics.network : {};
  const docker = metrics?.docker && typeof metrics.docker === 'object' ? metrics.docker : {};
  const gpu = metrics?.gpu && typeof metrics.gpu === 'object' ? metrics.gpu : {};
  const uptimeSeconds = firstNumber(metrics, ['uptime_seconds', 'uptime_raw'], null);
  const rawUptime = metrics?.uptime;
  const uptimeLabel = uptimeSeconds !== null
    ? ''
    : (typeof rawUptime === 'string' && rawUptime.trim() ? rawUptime.trim() : server.uptimeLabel);
  const trafficRxBytes = firstNumber(metrics, ['net_in_transfer', 'net_rx_total', 'rx_total_bytes'], firstNumber(network, ['rx_total_bytes'], server.trafficRxBytes));
  const trafficTxBytes = firstNumber(metrics, ['net_out_transfer', 'net_tx_total', 'tx_total_bytes'], firstNumber(network, ['tx_total_bytes'], server.trafficTxBytes));
  const networkTrafficBytes = firstNumber(network, ['traffic_used_bytes'], null);
  const metricsTrafficBytes = firstNumber(metrics, ['traffic_used_bytes'], null);
  const trafficLimitMode = normalizeTrafficLimitMode(firstText(
    metrics,
    ['traffic_limit_mode'],
    firstText(network, ['traffic_limit_mode'], server.trafficLimitMode || 'total'),
  ));
  const trafficUsedBytes = networkTrafficBytes
    ?? metricsTrafficBytes
    ?? resolveTrafficUsedBytes(trafficRxBytes, trafficTxBytes, trafficLimitMode);

  return {
    ...server,
    status: 'online',
    online: true,
    location: firstText(metrics, ['location', 'resolved_country', 'region'], server.location),
    region: firstText(metrics, ['region'], server.region),
    countryCode: cleanCountryDisplayCode(firstText(metrics, ['country_code', 'country'], server.countryCode)),
    latitude: firstNumber(metrics, ['lat', 'latitude'], server.latitude),
    longitude: firstNumber(metrics, ['lon', 'longitude'], server.longitude),
    platform: firstText(metrics, ['platform', 'os'], server.platform),
    platformVersion: firstText(metrics, ['platform_version'], server.platformVersion),
    agentVersion: firstText(metrics, ['agent_version'], server.agentVersion),
    uptime: uptimeSeconds ?? toNumber(rawUptime, server.uptime),
    uptimeLabel,
    load: firstText(metrics, ['load'], server.load),
    cpu: firstNumber(metrics, ['cpu', 'cpu_usage'], server.cpu),
    cpuTemp: firstNumber(metrics, ['cpu_temp', 'cpuTemp'], server.cpuTemp),
    cpuPower: firstNumber(metrics, ['cpu_power', 'cpuPower'], server.cpuPower),
    memory: firstNumber(metrics, ['mem_percent', 'mem_usage_percent', 'memory', 'memory_usage', 'mem_usage'], server.memory),
    memoryUsedBytes: firstNumber(metrics, ['mem_used_raw', 'memory_used_raw'], server.memoryUsedBytes),
    memoryTotalBytes: firstNumber(metrics, ['mem_total_raw', 'memory_total_raw'], server.memoryTotalBytes),
    disk: firstNumber(metrics, ['disk_percent', 'disk_usage'], server.disk),
    diskUsed: firstText(metrics, ['disk_used'], server.diskUsed),
    diskTotal: firstText(metrics, ['disk_total'], server.diskTotal),
    diskUsedBytes: firstNumber(metrics, ['disk_used_raw'], server.diskUsedBytes),
    diskTotalBytes: firstNumber(metrics, ['disk_total_raw'], server.diskTotalBytes),
    netRx: firstNumber(metrics, ['net_rx', 'net_in_speed', 'network_rx'], server.netRx),
    netTx: firstNumber(metrics, ['net_tx', 'net_out_speed', 'network_tx'], server.netTx),
    connections: firstNumber(metrics, ['connections', 'tcp_conn_count'], firstNumber(network, ['connections'], server.connections)),
    dockerRunning: firstNumber(metrics, ['docker_running'], firstNumber(docker, ['running'], server.dockerRunning)),
    dockerStopped: firstNumber(metrics, ['docker_stopped'], firstNumber(docker, ['stopped'], server.dockerStopped)),
    gpu: firstNumber(metrics, ['gpu_usage', 'gpu'], firstNumber(gpu, ['Usage'], server.gpu)),
    gpuTemp: firstNumber(metrics, ['gpu_temp', 'gpuTemp'], firstNumber(gpu, ['Temp'], server.gpuTemp)),
    gpuPower: firstNumber(metrics, ['gpu_power', 'gpuPower'], firstNumber(gpu, ['Power'], server.gpuPower)),
    gpuMemory: firstNumber(metrics, ['gpu_mem_percent'], firstNumber(gpu, ['Percent'], server.gpuMemory)),
    gpuModel: firstText(metrics, ['gpu_model', 'gpu_name'], firstText(gpu, ['Model', 'Name'], server.gpuModel)),
    trafficUsedBytes,
    trafficRxBytes,
    trafficTxBytes,
    trafficLimitMode,
    updatedAt: timestamp ? new Date(timestamp).toISOString() : new Date().toISOString(),
  };
};

const getMetricsSocketUrl = () => {
  const explicitUrl = import.meta.env?.VITE_METRICS_SOCKET_URL;
  return explicitUrl || '/metrics';
};

const LOCATION_COUNTRY_CODE_MAP = {
  netherlands: 'nl',
  holland: 'nl',
  amsterdam: 'nl',
  japan: 'jp',
  tokyo: 'jp',
  singapore: 'sg',
  'hong kong': 'hk',
  china: 'cn',
  taiwan: 'tw',
  korea: 'kr',
  canada: 'ca',
  australia: 'au',
  germany: 'de',
  frankfurt: 'de',
  france: 'fr',
  paris: 'fr',
  'united states': 'us',
  usa: 'us',
  america: 'us',
  'united kingdom': 'gb',
  london: 'gb',
  荷兰: 'nl',
  日本: 'jp',
  新加坡: 'sg',
  香港: 'hk',
  中国: 'cn',
  台湾: 'tw',
  韩国: 'kr',
  加拿大: 'ca',
  澳大利亚: 'au',
  德国: 'de',
  法国: 'fr',
  美国: 'us',
  英国: 'gb',
};

const inferCountryCodeFromLocation = (value) => {
  const text = String(value || '').trim();
  if (/^[a-z]{2}$/i.test(text)) return text;
  const normalized = text.toLowerCase();
  return Object.entries(LOCATION_COUNTRY_CODE_MAP).find(([name]) => normalized.includes(name))?.[1] || '';
};

const getFlagCountry = (server) => (
  cleanCountryDisplayCode(server.countryCode) ||
  cleanCountryDisplayCode(server.country_code) ||
  inferCountryCodeFromLocation(server.location) ||
  inferCountryCodeFromLocation(server.region) ||
  inferCountryCodeFromLocation(server.resolved_country) ||
  ''
);

const getOSIconClass = (platform) => {
  const baseClass = 'shrink-0 text-base leading-none';
  if (!platform) return `fas fa-server ${baseClass} text-kumo-subtle`;
  const p = String(platform).toLowerCase();
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

const getPlatformText = (server) => [server.platform, server.platformVersion].filter(Boolean).join(' ');

const formatUptime = (server) => {
  if (!server?.online) return '离线';
  const labelDays = String(server.uptimeLabel || '').match(/(\d+)\s*(?:d|天)/i);
  const seconds = Number(server.uptime) || 0;
  const days = labelDays ? Number(labelDays[1]) : Math.floor(seconds / 86400);
  return `${days}天`;
};

const percentTone = (value) => {
  const number = Number(value) || 0;
  if (number >= 90) return 'bg-kumo-danger';
  if (number >= 75) return 'bg-kumo-warning';
  return 'bg-kumo-success';
};

const compactLoad = (value) => {
  const [first] = String(value || '').trim().split(/\s+/);
  return first ? `L ${first}` : '';
};

function MetricBar({ label, value, subValue = '', offline = false }) {
  const percent = Math.max(0, Math.min(100, Number(value) || 0));
  return (
    <div className="flex h-16 min-w-0 flex-col rounded-md border border-kumo-interact/80 bg-kumo-recessed/35 px-2.5 py-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[10px] font-semibold text-kumo-strong sm:text-xs">{label}</span>
        <span className="text-[10px] font-bold tabular-nums text-kumo-strong sm:text-xs">{offline ? '--' : formatPercent(percent)}</span>
      </div>
      <Meter
        label={`${label} 使用率`}
        value={offline ? 0 : percent}
        showValue={false}
        className="public-status-meter-compact mt-1.5"
        trackClassName="h-1.5 bg-kumo-line/70"
        indicatorClassName={offline ? 'bg-kumo-muted' : percentTone(percent)}
      />
      <div className="mt-auto overflow-hidden text-ellipsis whitespace-nowrap pt-1 text-[10px] leading-snug text-kumo-subtle sm:text-[11px]" title={subValue}>
        {offline ? '无数据' : (subValue || '--')}
      </div>
    </div>
  );
}

const compactDiskText = (server) => {
  if (server.diskUsed && server.diskTotal) return `${server.diskUsed} / ${server.diskTotal}`;
  if (hasValue(server.diskUsedBytes) && hasValue(server.diskTotalBytes)) return `${formatCompactBytes(server.diskUsedBytes)} / ${formatCompactBytes(server.diskTotalBytes)}`;
  return '';
};

const compactMemoryText = (server) => {
  if (hasValue(server.memoryUsedBytes) && hasValue(server.memoryTotalBytes)) return `${formatCompactBytes(server.memoryUsedBytes)} / ${formatCompactBytes(server.memoryTotalBytes)}`;
  return '';
};

const trafficRemaining = (server) => {
  const limit = Number(server.trafficLimitBytes) || 0;
  const used = Math.max(0, Number(server.trafficUsedBytes) || 0);
  const unlimited = limit <= 0;
  const remaining = Math.max(0, limit - used);
  const remainingPercent = limit > 0 ? Math.max(0, Math.min(100, (remaining / limit) * 100)) : 0;
  const usedPercent = limit > 0 ? Math.max(0, Math.min(100, (used / limit) * 100)) : 0;
  return { limit, used, remaining, remainingPercent, usedPercent, unlimited };
};

const remainingTone = (percent) => {
  if (percent <= 10) return 'bg-kumo-danger';
  if (percent <= 25) return 'bg-kumo-warning';
  return 'bg-brand';
};

const getByteParts = (value) => {
  const bytes = Math.max(0, Number(value) || 0);
  if (bytes < 1024) return { num: bytes.toFixed(1), unit: 'B', text: formatBytes(bytes) };
  const units = ['K', 'M', 'G', 'T', 'P'];
  let next = bytes / 1024;
  let index = 0;
  while (next >= 1024 && index < units.length - 1) {
    next /= 1024;
    index += 1;
  }
  return { num: next.toFixed(1), unit: units[index], text: formatBytes(bytes) };
};

const formatFlowPart = (part, suffix = '') => `${part.num}${part.unit}${suffix}`;

const FLOW_KIND_CLASS = {
  speed: {
    label: 'text-kumo-success',
    box: 'border-kumo-interact/75 bg-kumo-recessed/35',
  },
  traffic: {
    label: 'text-kumo-info',
    box: 'border-kumo-interact/75 bg-kumo-recessed/35',
  },
};

function FlowUnitBadge({ unit, suffix = '' }) {
  return (
    <span className={`${FLOW_UNIT_BADGE_CLASS} h-[18px] min-w-[18px] px-0.5 text-[11px] sm:h-5 sm:min-w-5 sm:px-1 sm:text-[13px] ${getFlowUnitClassName(unit)}`}>
      {unit || 'B'}{suffix}
    </span>
  );
}

function FlowArrow({ children }) {
  return (
    <span className="inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[4px] border border-kumo-line/70 bg-kumo-recessed/70 text-[11px] font-bold leading-none text-kumo-default sm:h-5 sm:w-5 sm:text-sm">
      {children}
    </span>
  );
}

function FlowPair({ left, right, leftTitle, rightTitle, kind = 'speed', suffix = '' }) {
  const kindClass = FLOW_KIND_CLASS[kind] || FLOW_KIND_CLASS.speed;
  return (
    <div className={`grid h-6 min-w-0 grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] items-center overflow-hidden rounded-md border px-1 text-[11px] font-bold tabular-nums leading-none text-kumo-strong sm:text-xs ${kindClass.box}`}>
      <div className="flex min-w-0 items-center justify-end gap-1 px-0.5" title={leftTitle || formatFlowPart(left, suffix)}>
        <span className="min-w-[2.2rem] truncate text-right text-[11px] sm:min-w-[2.6rem] sm:text-[13px]">{left.num}</span>
        <FlowUnitBadge unit={left.unit} suffix={suffix} />
        <FlowArrow>↓</FlowArrow>
      </div>
      <span aria-hidden="true" className="h-full w-px bg-kumo-line/80" />
      <div className="flex min-w-0 items-center justify-start gap-1 px-0.5" title={rightTitle || formatFlowPart(right, suffix)}>
        <FlowArrow>↑</FlowArrow>
        <FlowUnitBadge unit={right.unit} suffix={suffix} />
        <span className="min-w-[2.2rem] truncate text-left text-[11px] sm:min-w-[2.6rem] sm:text-[13px]">{right.num}</span>
      </div>
    </div>
  );
}

function FlowRow({ label, left, right, leftTitle, rightTitle, kind = 'speed', suffix = '' }) {
  const kindClass = FLOW_KIND_CLASS[kind] || FLOW_KIND_CLASS.speed;
  return (
    <div className="grid h-full min-h-0 min-w-0 grid-cols-[1.75rem_minmax(0,1fr)] items-center gap-1 sm:grid-cols-[2rem_minmax(0,1fr)] sm:gap-1.5">
      <span className={`text-[10px] font-bold leading-none sm:text-xs ${kindClass.label}`}>{label}</span>
      <FlowPair left={left} right={right} leftTitle={leftTitle} rightTitle={rightTitle} kind={kind} suffix={suffix} />
    </div>
  );
}

function NetworkTrafficPanel({ speedLeft, speedRight, totalLeft, totalRight, speedLeftTitle, speedRightTitle, totalLeftTitle, totalRightTitle }) {
  return (
    <div className="h-16 min-w-0 rounded-md border border-kumo-interact/80 bg-kumo-recessed/35 px-2 py-1">
      <div className="grid h-full min-w-0 grid-rows-2 items-center gap-0">
        <FlowRow label="网速" left={speedLeft} right={speedRight} leftTitle={speedLeftTitle} rightTitle={speedRightTitle} kind="speed" />
        <FlowRow label="流量" left={totalLeft} right={totalRight} leftTitle={totalLeftTitle} rightTitle={totalRightTitle} kind="traffic" />
      </div>
    </div>
  );
}

function RemainingMini({ traffic }) {
  const hasLimit = traffic.limit > 0;
  const unlimited = traffic.unlimited || !hasLimit;
  const displayPercent = unlimited ? '∞' : formatPercent(traffic.remainingPercent);
  const displayValue = unlimited ? '无限' : formatCompactBytes(traffic.remaining);
  const progressTone = unlimited ? 'bg-brand' : remainingTone(traffic.remainingPercent);
  return (
    <div className="flex h-16 min-w-0 flex-col rounded-md border border-kumo-interact/80 bg-kumo-recessed/35 px-2 py-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[10px] font-semibold text-kumo-strong sm:text-xs">剩余流量</span>
        <span className="text-[10px] font-bold tabular-nums text-kumo-strong sm:text-xs">{displayPercent}</span>
      </div>
      <Meter
        label="剩余流量"
        value={unlimited ? 100 : traffic.remainingPercent}
        showValue={false}
        className="public-status-meter-compact mt-1.5"
        trackClassName="h-1.5 bg-kumo-line/70"
        indicatorClassName={progressTone}
      />
      <div className="mt-auto truncate pt-1 text-right text-[10px] font-bold leading-snug text-kumo-strong sm:text-[11px]" title={unlimited ? '无限' : `${formatCompactBytes(traffic.remaining)} / ${formatCompactBytes(traffic.limit)}`}>
        {displayValue}
      </div>
    </div>
  );
}

function ServerCard({ server }) {
  const [showPlatform, setShowPlatform] = useState(false);
  const offline = !server.online;
  const traffic = trafficRemaining(server);
  const country = getFlagCountry(server);
  const platformText = getPlatformText(server);
  const memoryText = compactMemoryText(server);
  const diskText = compactDiskText(server);
  const rxSpeed = getByteParts(server.netRx);
  const txSpeed = getByteParts(server.netTx);
  const rxTotal = getByteParts(server.trafficRxBytes);
  const txTotal = getByteParts(server.trafficTxBytes);
  const cpuDetailText = [
    compactLoad(server.load),
    hasValue(server.cpuTemp) ? `${Math.round(server.cpuTemp)}°C` : '',
  ].filter(Boolean).join(' · ');

  return (
    <article className={`public-server-status-card flex h-full flex-col rounded-lg border p-2.5 ${server.online ? 'border-kumo-line bg-kumo-base' : 'border-kumo-danger/25 bg-kumo-danger/5'}`}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <Button
              type="button"
              size="xs"
              variant="ghost"
              shape="square"
              className="shrink-0 text-kumo-subtle hover:text-kumo-strong"
              title={platformText || '点击显示系统版本'}
              aria-label="显示系统版本"
              onClick={() => setShowPlatform((value) => !value)}
            >
              <i className={getOSIconClass(server.platform)} />
            </Button>
            {country && <CountryFlag preferSvg countryCode={country} className="h-3.5 w-5 shrink-0 !rounded-[2px] text-sm" />}
            <h3 className="min-w-0 truncate text-[13px] font-bold leading-tight text-kumo-strong sm:text-sm" title={server.name}>{server.name}</h3>
          </div>
          {showPlatform && platformText && (
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-kumo-subtle sm:text-xs">
              <span className="min-w-0 truncate" title={platformText}>{platformText}</span>
            </div>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2 text-[11px] sm:text-xs">
          {server.online ? (
            <div className="font-bold tabular-nums text-kumo-strong">{formatUptime(server)}</div>
          ) : null}
          <span className={`shrink-0 rounded-md border px-2 py-0.5 text-[10px] font-semibold sm:text-xs ${server.online ? 'border-kumo-success/30 bg-kumo-success/10 text-kumo-success' : 'border-kumo-danger/30 bg-kumo-danger/10 text-kumo-danger'}`}>
            {server.online ? '在线' : '离线'}
          </span>
        </div>
      </div>

      <div className="mt-2 grid grid-cols-3 gap-1.5">
        <MetricBar label="CPU" value={server.cpu} subValue={cpuDetailText} offline={offline} />
        <MetricBar label="内存" value={server.memory} subValue={memoryText} offline={offline} />
        <MetricBar label="硬盘" value={server.disk} subValue={diskText} offline={offline} />
      </div>

      <div className="mt-1.5 grid grid-cols-3 gap-1.5 text-[11px] sm:text-xs">
        <div className="col-span-2 min-w-0">
        <NetworkTrafficPanel
          speedLeft={offline ? { num: '0.0', unit: 'B' } : rxSpeed}
          speedRight={offline ? { num: '0.0', unit: 'B' } : txSpeed}
          totalLeft={rxTotal}
          totalRight={txTotal}
          speedLeftTitle={offline ? '0 B/s' : formatSpeed(server.netRx)}
          speedRightTitle={offline ? '0 B/s' : formatSpeed(server.netTx)}
          totalLeftTitle={rxTotal.text}
          totalRightTitle={txTotal.text}
        />
        </div>
        <RemainingMini traffic={traffic} />
      </div>
    </article>
  );
}

function PublicServerStatusPage({ domainOnly = false, onDomainNotFound }) {
  const isAuthenticated = useStore((state) => state.isAuthenticated);
  const slug = useMemo(() => normalizePublicPath(), []);
  const surfaceRef = useCloudflareSpotlight();
  const [page, setPage] = useState(null);
  const [initialLoading, setInitialLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [fetchedAt, setFetchedAt] = useState('');
  const [wideColumns, setWideColumns] = useState(() => {
    const stored = window.localStorage?.getItem('publicServerStatusColumns');
    return stored === '4' ? 4 : 3;
  });
  const [mapOpen, setMapOpen] = useState(false);
  const load = useCallback(async ({ silent = false } = {}) => {
    if (silent) {
      setRefreshing(true);
    } else {
      setInitialLoading(true);
    }
    setError('');
    try {
      const endpoint = slug && !domainOnly
        ? `/api/server/public/status-pages/${encodeURIComponent(slug)}`
        : `/api/server/public/status-page-by-domain?domain=${encodeURIComponent(window.location.host)}`;
      const response = await fetch(endpoint, { cache: 'no-store' });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        const nextError = new Error(result.error || '主机状态页不存在或未公开');
        nextError.status = response.status;
        throw nextError;
      }
      setPage(result.data || result);
      setFetchedAt(new Date().toISOString());
    } catch (err) {
      if (!slug && domainOnly && err.status === 404 && onDomainNotFound) {
        onDomainNotFound();
        return;
      }
      setError(err.message || '主机状态页加载失败');
      if (!silent) setPage(null);
    } finally {
      setInitialLoading(false);
      setRefreshing(false);
    }
  }, [domainOnly, onDomainNotFound, slug]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!page?.title) return undefined;
    const previousTitle = document.title;
    document.title = page.title;
    return () => {
      document.title = previousTitle;
    };
  }, [page?.title]);

  useEffect(() => swapPublicPageFavicon(getPublicPageFaviconHref('server', page?.config)), [page?.config]);

  const servers = Array.isArray(page?.servers) ? page.servers : [];
  const serverIdKey = useMemo(() => servers.map(item => String(item.id)).sort().join('|'), [servers]);

  useEffect(() => {
    if (!serverIdKey) return undefined;
    const visibleServerIds = new Set(serverIdKey.split('|').filter(Boolean));
    const socket = io(getMetricsSocketUrl(), {
      reconnection: true,
      reconnectionDelay: 1000,
      reconnectionDelayMax: 5000,
      reconnectionAttempts: Infinity,
      transports: ['websocket', 'polling'],
    });

    socket.on('metrics:update', data => {
      if (!data?.serverId || !data.metrics || !visibleServerIds.has(String(data.serverId))) return;
      const updatedAt = new Date().toISOString();
      setPage(prev => {
        if (!prev || !Array.isArray(prev.servers)) return prev;
        return {
          ...prev,
          servers: prev.servers.map(server => (
            String(server.id) === String(data.serverId)
              ? mergeRealtimeMetrics(server, data.metrics, data.timestamp)
              : server
          )),
        };
      });
      setFetchedAt(updatedAt);
      setError('');
    });

    socket.on('server:status', data => {
      if (!data?.serverId || !visibleServerIds.has(String(data.serverId))) return;
      const updatedAt = new Date().toISOString();
      setPage(prev => {
        if (!prev || !Array.isArray(prev.servers)) return prev;
        return {
          ...prev,
          servers: prev.servers.map(server => (
            String(server.id) === String(data.serverId)
              ? {
                ...server,
                status: data.status || server.status,
                online: data.agent_online ?? data.agentOnline ?? data.status === 'online',
                updatedAt: data.lastSeen || updatedAt,
              }
              : server
          )),
        };
      });
      setFetchedAt(updatedAt);
    });

    return () => {
      socket.disconnect();
    };
  }, [serverIdKey]);

  const setColumnPreference = useCallback((columns) => {
    const nextColumns = String(columns) === '4' ? 4 : 3;
    setWideColumns(nextColumns);
    window.localStorage?.setItem('publicServerStatusColumns', String(nextColumns));
  }, []);

  const pageMaxWidthClass = wideColumns === 4 ? 'max-w-[112rem]' : 'max-w-[84rem]';
  const serverGridClass = wideColumns === 4
    ? 'grid gap-3 md:grid-cols-2 2xl:grid-cols-4'
    : 'grid gap-3 md:grid-cols-2 xl:grid-cols-3';
  const visibleServers = servers;

  const updatePageIcon = async (iconId) => {
    if (!page?.id) return;
    const response = await fetch(`/api/server/status-pages/${page.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: page.title || '主机状态',
        slug: page.slug || '',
        domain: page.domain || '',
        description: page.description || '',
        public: page.public !== false,
        cacheSeconds: page.cacheSeconds || 300,
        serverIds: Array.isArray(page.serverIds) ? page.serverIds : [],
        config: withPublicPageIconId(page.config, iconId),
      }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) {
      throw new Error(result.error || '保存主机状态页图标失败');
    }
    setPage((current) => (current ? {
      ...current,
      config: withPublicPageIconId(current.config, iconId),
    } : current));
    toast.success(iconId ? '主机状态页图标已更新' : '已恢复主机状态页默认图标');
  };

  return (
    <div ref={surfaceRef} className="cf-ai-background-surface public-server-status-page relative isolate min-h-screen text-kumo-default">
      <div aria-hidden="true" className="cf-ai-background pointer-events-none absolute inset-0" />
      <main className={`relative z-10 mx-auto flex min-h-screen w-full ${pageMaxWidthClass} flex-col px-4 py-6 sm:px-6 lg:px-8`}>
        <header className="mb-6 flex items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <PublicPageIconPicker
              pageKind="server"
              config={page?.config}
              isAuthenticated={isAuthenticated}
              onChange={updatePageIcon}
              triggerClassName="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-kumo-line bg-kumo-base text-brand"
              iconClassName="h-5 w-5"
            />
            <div className="min-w-0">
              <div className="truncate text-base font-bold text-kumo-strong">{page?.title || '主机状态'}</div>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Tabs
              {...TOOL_TABS_PROPS}
              className="hidden w-fit max-w-full sm:block"
              listClassName="w-fit max-w-full"
              value={String(wideColumns)}
              onValueChange={setColumnPreference}
              tabs={COLUMN_TABS}
            />
            <Button
              size="sm"
              variant="secondary"
              shape="square"
              className={mapOpen ? 'border-brand/50 text-brand' : ''}
              onClick={() => setMapOpen(prev => !prev)}
              icon={<Globe className="h-3.5 w-3.5" />}
              aria-label={mapOpen ? '切回主机卡片' : '切换到主机地图'}
              title={mapOpen ? '切回主机卡片' : '切换到主机地图'}
            />
            <Button
              size="sm"
              variant="secondary"
              shape="square"
              onClick={() => load({ silent: true })}
              loading={refreshing}
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              aria-label="刷新"
              title="刷新"
            />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => { window.location.href = '/'; }}
              icon={isAuthenticated ? <Home className="h-3.5 w-3.5" /> : <LogIn className="h-3.5 w-3.5" />}
              aria-label={isAuthenticated ? '主页' : '登录'}
              title={isAuthenticated ? '跳转到主页' : '跳转到登录页'}
            >
              {isAuthenticated ? '主页' : '登录'}
            </Button>
          </div>
        </header>

        {!initialLoading && error && !page && (
          <div className="flex flex-1 flex-col items-center justify-center rounded-lg border border-kumo-line bg-kumo-base p-10 text-center">
            <AlertTriangle className="mb-3 h-9 w-9 text-kumo-warning" />
            <h1 className="text-lg font-bold text-kumo-strong">无法显示主机状态页</h1>
            <p className="mt-2 max-w-md text-sm leading-relaxed text-kumo-subtle">{error}</p>
          </div>
        )}

        {!initialLoading && page && (
          <div className="flex flex-col gap-4">
            {mapOpen ? (
              <div className="w-full overflow-hidden rounded-lg border border-kumo-line bg-kumo-base max-h-[calc(100vh-140px)] flex items-center justify-center">
                {visibleServers.length === 0 ? (
                  <div className="w-full" style={{ aspectRatio: wideColumns === 4 ? '2.3 / 1' : '16 / 9' }}>
                    <SkeletonLine className="h-full w-full rounded-none" />
                  </div>
                ) : (
                  <div className="w-full max-h-[calc(100vh-140px)]">
                    <ServerLocationMap
                      echarts={echarts}
                      servers={visibleServers}
                      resolveStatus={(server) => (server?.online ? 'online' : 'offline')}
                      aspectRatio={wideColumns === 4 ? '2.3 / 1' : '16 / 9'}
                    />
                  </div>
                )}
              </div>
            ) : (
              <section>
                {servers.length === 0 ? (
                  <div className="rounded-lg border border-kumo-line bg-kumo-base p-8 text-center text-sm text-kumo-subtle">这个状态页还没有绑定主机。</div>
                ) : (
                  <div className={serverGridClass}>
                    {visibleServers.map((server) => <ServerCard key={server.id} server={server} />)}
                    {visibleServers.length === 0 && <div className="col-span-full rounded-lg border border-kumo-line bg-kumo-base p-8 text-center text-sm text-kumo-subtle">暂无匹配主机。</div>}
                  </div>
                )}
              </section>
            )}

            <footer className="flex flex-col gap-2 py-4 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
              <span className="inline-flex items-center gap-1"><Shield className="h-3.5 w-3.5" />由 API Monitor 提供</span>
              <span>数据时间：{formatDateTime(fetchedAt || page.updatedAt || page.createdAt)}</span>
            </footer>
          </div>
        )}
      </main>
    </div>
  );
}

export default PublicServerStatusPage;
