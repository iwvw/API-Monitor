import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChartPalette, ClipboardText, LayerCard, Popover, Tabs, Toolbar } from '@cloudflare/kumo';
import SiteFontTimeseriesChart from '../components/SiteFontTimeseriesChart.jsx';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
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
import useStore from '../store.js';
import useTableResize from '../composables/useTableResize.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { handleEditableRowDoubleClick } from '../modules/tableInteractions.js';
import { AppCard, PageStack, ResponsiveSearchInput, SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import { AnimatedCollapse } from '../components/AnimatedCollapse.jsx';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import {
  AlertTriangle,
  ArrowLeft,
  Box,
  Check,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Cloud,
  Copy,
  Database,
  Download,
  Edit,
  ExternalLink,
  Eye,
  EyeOff,
  FileText,
  Folder,
  Globe,
  Key,
  Layers,
  Lock,
  Plus,
  RefreshCw,
  Save,
  Search,
  Settings,
  Shield,
  Terminal,
  Trash,
  Upload,
  X,
} from '../components/Icons.jsx';

const RECORD_TYPE_OPTIONS = ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'NS', 'SRV', 'CAA', 'PTR'];
const SSL_MODES = [
  { value: 'off', label: '关闭' },
  { value: 'flexible', label: '灵活' },
  { value: 'full', label: '完全' },
  { value: 'strict', label: '严格' },
];
const SSL_MODE_LABELS = Object.fromEntries(SSL_MODES.map((mode) => [mode.value, mode.label]));
const ZONE_TYPE_LABELS = {
  full: '完全',
  partial: '部分',
};
const RECORD_TYPE_BADGE_VARIANTS = {
  A: 'blue',
  AAAA: 'teal',
  CNAME: 'purple',
  TXT: 'neutral',
  MX: 'orange',
  NS: 'green',
  SRV: 'info',
  CAA: 'warning',
  PTR: 'secondary',
};
const EMPTY_ANALYTICS = {
  requests: 0,
  bandwidth: 0,
  cachedRequests: 0,
  cachedBytes: 0,
  threats: 0,
  pageViews: 0,
  uniques: 0,
  cacheHitRate: 0,
  timeseries: [],
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

const CLOUDFLARE_TABS = [
  { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />DNS</span> },
  { value: 'workers', label: <span className="inline-flex items-center gap-1.5"><Terminal className="h-3.5 w-3.5" />Workers</span> },
  { value: 'pages', label: <span className="inline-flex items-center gap-1.5"><Layers className="h-3.5 w-3.5" />Pages</span> },
  { value: 'r2', label: <span className="inline-flex items-center gap-1.5"><Database className="h-3.5 w-3.5" />R2 存储</span> },
  { value: 'tunnels', label: <span className="inline-flex items-center gap-1.5"><Lock className="h-3.5 w-3.5" />Tunnel</span> },
  { value: 'templates', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />DNS 模板</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />账号</span> },
];

const EMPTY_ACCOUNT_FORM = {
  name: '',
  email: '',
  cfAccountId: '',
  apiToken: '',
  skipVerify: false,
};

const EMPTY_ZONE_FORM = {
  name: '',
  jumpStart: false,
};

const EMPTY_RECORD_FORM = {
  type: 'A',
  name: '@',
  content: '',
  ttl: 1,
  proxied: false,
  priority: 10,
};

const EMPTY_TEMPLATE_FORM = {
  name: '',
  description: '',
  type: 'A',
  recordName: '@',
  content: '',
  ttl: 1,
  proxied: false,
  priority: 10,
};

const EMPTY_WORKER_FORM = {
  name: '',
  script: 'export default {\n  async fetch(request, env, ctx) {\n    return new Response("Hello Cloudflare Workers");\n  },\n};',
};

const EMPTY_TUNNEL_CONFIG = JSON.stringify(
  {
    ingress: [
      { hostname: 'app.example.com', service: 'http://localhost:8080' },
      { service: 'http_status:404' },
    ],
  },
  null,
  2
);

function formatDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatBytes(bytes) {
  if (bytes === null || bytes === undefined || bytes === '') return '-';
  const value = Number(bytes || 0);
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(2)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(2)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(2)} KB`;
  return `${value} B`;
}

function r2PreviewKind(key = '') {
  const extension = String(key).split('?')[0].split('#')[0].split('.').pop()?.toLowerCase() || '';
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'svg'].includes(extension)) return 'image';
  if (['mp4', 'webm', 'mov', 'm4v', 'ogg'].includes(extension)) return 'video';
  if (['mp3', 'wav', 'm4a', 'aac', 'flac', 'oga'].includes(extension)) return 'audio';
  if (extension === 'pdf') return 'frame';
  if ([
    'txt', 'log', 'json', 'xml', 'csv', 'md', 'yaml', 'yml', 'html', 'css', 'js', 'jsx', 'ts', 'tsx',
    'go', 'py', 'sh', 'sql', 'ini', 'env', 'toml',
  ].includes(extension)) return 'frame';
  return 'unknown';
}

function objectFileName(value = '') {
  const parts = String(value).split('/').filter(Boolean);
  return parts[parts.length - 1] || 'object';
}

function formatNumber(value) {
  if (value === null || value === undefined || value === '') return '-';
  const number = Number(value || 0);
  if (number >= 1000000) return `${(number / 1000000).toFixed(1)}M`;
  if (number >= 1000) return `${(number / 1000).toFixed(1)}K`;
  return String(number);
}

function formatNumberAxis(value) {
  const number = Number(value || 0);
  const abs = Math.abs(number);
  if (abs >= 1000000) return `${(number / 1000000).toFixed(abs >= 10000000 ? 0 : 1)}M`;
  if (abs >= 1000) return `${(number / 1000).toFixed(abs >= 10000 ? 0 : 1)}K`;
  return `${Math.round(number)}`;
}

function toDisplayNumber(value, fallback = 0) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function formatPercent(value) {
  const number = toDisplayNumber(value);
  return `${Number.isInteger(number) ? number : number.toFixed(1)}%`;
}

function parseAnalyticsTimestamp(point) {
  const timestamp = new Date(point?.datetime || point?.since || point?.date || '').getTime();
  return Number.isFinite(timestamp) ? timestamp : null;
}

function formatAnalyticsAxisTime(timestamp, range = '24h') {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  if (range === '24h') {
    return `${String(date.getHours()).padStart(2, '0')}:00`;
  }
  return `${String(date.getMonth() + 1).padStart(2, '0')}/${String(date.getDate()).padStart(2, '0')}`;
}

function sslModeLabel(mode) {
  return SSL_MODE_LABELS[mode] || mode || '-';
}

function zoneTypeLabel(type) {
  return ZONE_TYPE_LABELS[type] || type || '-';
}

function zoneNameServers(zone = {}) {
  const list = zone.nameServers || zone.name_servers || [];
  return Array.isArray(list) ? list.filter(Boolean) : [];
}

function recordTypeBadgeVariant(type) {
  return RECORD_TYPE_BADGE_VARIANTS[String(type || '').toUpperCase()] || 'outline';
}

function DnsPanelCard({ className = '', children }) {
  return (
    <div className={`rounded-lg border border-kumo-line bg-kumo-base shadow-none ${className}`}>
      {children}
    </div>
  );
}

function recordShortName(name, zoneName) {
  if (!name || !zoneName) return name || '@';
  if (name === zoneName) return '@';
  const suffix = `.${zoneName}`;
  return name.endsWith(suffix) ? name.slice(0, -suffix.length) : name;
}

function zoneStatusLabel(status) {
  const map = {
    active: '已激活',
    pending: '待验证',
    initializing: '初始化中',
    moved: '已迁移',
    deleted: '已删除',
  };
  return map[status] || status || '未知';
}

function tunnelStatusLabel(status, connections = []) {
  if (connections.length > 0) return '已连接';
  const map = {
    active: '已连接',
    healthy: '健康',
    inactive: '未连接',
    down: '离线',
    degraded: '降级',
  };
  return map[status] || status || '未知';
}

function statusVariant(status) {
  if (['active', 'healthy', 'success', 'finished', 'ok'].includes(status)) return 'success';
  if (['error', 'failed', 'down'].includes(status)) return 'error';
  if (['pending', 'initializing', 'queued', 'building', 'degraded'].includes(status)) return 'warning';
  return 'outline';
}

function downloadJson(filename, payload) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

function parseJsonInput(input, fallbackKey) {
  const parsed = JSON.parse(input);
  if (Array.isArray(parsed)) return parsed;
  if (fallbackKey && Array.isArray(parsed[fallbackKey])) return parsed[fallbackKey];
  return parsed;
}

function DnsPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const { theme } = useStore();
  const isDarkMode = theme === 'dark';
  const [activeTab, setActiveTab] = useState('dns');
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [zones, setZones] = useState([]);
  const [selectedZoneId, setSelectedZoneId] = useState('');
  const [records, setRecords] = useState([]);
  const [recordTypes, setRecordTypes] = useState(RECORD_TYPE_OPTIONS);
  const [templates, setTemplates] = useState([]);
  const [workers, setWorkers] = useState([]);
  const [workerSubdomain, setWorkerSubdomain] = useState('');
  const [pages, setPages] = useState([]);
  const [r2Buckets, setR2Buckets] = useState([]);
  const [r2SelectedBucket, setR2SelectedBucket] = useState(null);
  const [r2Objects, setR2Objects] = useState([]);
  const [r2Prefixes, setR2Prefixes] = useState([]);
  const [r2CurrentPrefix, setR2CurrentPrefix] = useState('');
  const [r2BucketSearch, setR2BucketSearch] = useState('');
  const [r2ObjectSearch, setR2ObjectSearch] = useState('');
  const [r2DirCache, setR2DirCache] = useState({});
  const [r2ExpandedPrefixes, setR2ExpandedPrefixes] = useState([]);
  const [r2DirErrors, setR2DirErrors] = useState({});
  const [r2Metrics, setR2Metrics] = useState(null);
  const r2UploadInputRef = useRef(null);
  const [tunnels, setTunnels] = useState([]);
  const [sslInfo, setSslInfo] = useState(null);
  const [analytics, setAnalytics] = useState(null);
  const [analyticsRange, setAnalyticsRange] = useState('24h');
  const [showAnalyticsCharts, setShowAnalyticsCharts] = useState(false);
  const [selectedRecordIds, setSelectedRecordIds] = useState([]);
  const [selectedR2Objects, setSelectedR2Objects] = useState([]);
  const [accountTokens, setAccountTokens] = useState({});
  const [loading, setLoading] = useState({});
  const [recordFilter, setRecordFilter] = useState({ type: '', name: '' });
  const [modal, setModal] = useState({ type: null, data: null });
  const [accountForm, setAccountForm] = useState(EMPTY_ACCOUNT_FORM);
  const [zoneForm, setZoneForm] = useState(EMPTY_ZONE_FORM);
  const [recordForm, setRecordForm] = useState(EMPTY_RECORD_FORM);
  const [templateForm, setTemplateForm] = useState(EMPTY_TEMPLATE_FORM);
  const [workerForm, setWorkerForm] = useState(EMPTY_WORKER_FORM);
  const [workerRouteState, setWorkerRouteState] = useState({ worker: null, routes: [], form: { id: '', pattern: '', script: '' } });
  const [workerDomainState, setWorkerDomainState] = useState({ worker: null, domains: [], hostname: '', environment: 'production' });
  const [workerAnalyticsState, setWorkerAnalyticsState] = useState({ worker: null, analytics: null });
  const [pagesDeployState, setPagesDeployState] = useState({ project: null, deployments: [] });
  const [pagesDomainState, setPagesDomainState] = useState({ project: null, domains: [], domain: '' });
  const [r2BucketForm, setR2BucketForm] = useState({ name: '', location: 'auto' });
  const [r2FolderForm, setR2FolderForm] = useState({ name: '' });
  const [importState, setImportState] = useState({ kind: '', text: '', overwrite: false });
  const [tunnelForm, setTunnelForm] = useState({ name: '' });
  const [tunnelTokenState, setTunnelTokenState] = useState({ tunnel: null, token: '' });
  const [tunnelConfigState, setTunnelConfigState] = useState({ tunnel: null, text: EMPTY_TUNNEL_CONFIG });
  const [tunnelConnectionState, setTunnelConnectionState] = useState({ tunnel: null, connections: [] });

  const zoneColWidths = ['31%', '20%', '13%', '11%', '25%'];
  const [recordColWidths, startRecordResize] = useTableResize([34, 54, 82, 140, 48, 50, 106, 70]);
  const [workerColWidths, startWorkerResize] = useTableResize([260, 160, 180, 280]);
  const [pageColWidths, startPageResize] = useTableResize([240, 220, 150, 150, 220]);
  const [r2ColWidths, startR2Resize] = useTableResize([44, 420, 128, 180, 128]);
  const [tunnelColWidths, startTunnelResize] = useTableResize([260, 120, 100, 180, 240]);
  const [templateColWidths, startTemplateResize] = useTableResize([220, 90, 260, 120, 180]);
  const [accountColWidths, startAccountResize] = useTableResize([220, 220, 260, 240, 170, 190]);

  const selectedAccount = useMemo(
    () => accounts.find((account) => String(account.id) === String(selectedAccountId)) || null,
    [accounts, selectedAccountId]
  );

  const selectedZone = useMemo(
    () => zones.find((zone) => zone.id === selectedZoneId) || null,
    [zones, selectedZoneId]
  );

  const selectedRecords = useMemo(
    () => records.filter((record) => selectedRecordIds.includes(record.id)),
    [records, selectedRecordIds]
  );

  const analyticsSummary = useMemo(() => ({
    requests: toDisplayNumber(analytics?.requests),
    bandwidth: toDisplayNumber(analytics?.bandwidth),
    uniques: toDisplayNumber(analytics?.uniques),
    cacheHitRate: toDisplayNumber(analytics?.cacheHitRate),
  }), [analytics]);

  const analyticsPoints = useMemo(() => (
    Array.isArray(analytics?.timeseries)
      ? analytics.timeseries
        .filter(Boolean)
        .map((point) => ({ ...point, timestamp: parseAnalyticsTimestamp(point) }))
        .filter((point) => point.timestamp !== null)
      : []
  ), [analytics]);

  const analyticsChartCards = useMemo(() => {
    const requestColor = ChartPalette.categorical(0, isDarkMode);
    const bandwidthColor = ChartPalette.semantic('Success', isDarkMode);
    const cacheColor = ChartPalette.semantic('Attention', isDarkMode);

    return [
      {
        key: 'requests',
        label: '请求趋势',
        value: formatNumber(analyticsSummary.requests),
        data: [{
          name: '请求量',
          color: requestColor,
          data: analyticsPoints.map((point) => [point.timestamp, toDisplayNumber(point.requests)]),
        }],
        yAxisTickFormat: formatNumberAxis,
        tooltipValueFormat: (value) => `${formatNumber(value)} 次`,
      },
      {
        key: 'bandwidth',
        label: '带宽趋势',
        value: formatBytes(analyticsSummary.bandwidth),
        data: [{
          name: '带宽',
          color: bandwidthColor,
          data: analyticsPoints.map((point) => [point.timestamp, toDisplayNumber(point.bandwidth)]),
        }],
        yAxisTickFormat: formatBytes,
        tooltipValueFormat: formatBytes,
      },
      {
        key: 'cacheHitRate',
        label: '缓存命中率',
        value: formatPercent(analyticsSummary.cacheHitRate),
        data: [{
          name: '命中率',
          color: cacheColor,
          data: analyticsPoints.map((point) => [point.timestamp, toDisplayNumber(point.cacheHitRate)]),
        }],
        yAxisTickFormat: formatPercent,
        tooltipValueFormat: formatPercent,
      },
    ];
  }, [analyticsPoints, analyticsSummary.bandwidth, analyticsSummary.cacheHitRate, analyticsSummary.requests, isDarkMode]);
  const showAnalyticsPanel = showAnalyticsCharts && (analyticsPoints.length > 0 || loading.analytics);

  useEffect(() => {
    setShowAnalyticsCharts(false);
  }, [selectedZoneId]);

  const setLoadingKey = useCallback((key, value) => {
    setLoading((prev) => ({ ...prev, [key]: value }));
  }, []);

  const getAuthHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
  }), []);

  const cfApi = useCallback(async (path, options = {}) => {
    const headers = {
      ...getAuthHeaders(),
      ...(options.headers || {}),
    };
    const response = await fetch(`/api/cloudflare${path}`, {
      ...options,
      headers,
    });
    const text = await response.text();
    const data = text ? JSON.parse(text) : {};
    if (!response.ok || data.error) {
      throw new Error(data.error || data.message || `请求失败：${response.status}`);
    }
    return data;
  }, [getAuthHeaders]);

  const closeModal = useCallback(() => {
    setModal({ type: null, data: null });
  }, []);

  const copyText = useCallback(async (text, label = '内容') => {
    const value = String(text || '').trim();
    if (!value || value === '-') {
      toast.warning('没有可复制的内容');
      return;
    }

    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value);
      } else {
        const textarea = document.createElement('textarea');
        textarea.value = value;
        textarea.setAttribute('readonly', '');
        textarea.style.position = 'fixed';
        textarea.style.opacity = '0';
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        document.body.removeChild(textarea);
      }
      toast.success(`${label}已复制`);
    } catch (error) {
      toast.error(`复制失败：${error.message}`);
    }
  }, []);

  const loadAccounts = useCallback(async () => {
    setLoadingKey('accounts', true);
    try {
      const data = await cfApi('/accounts');
      const list = Array.isArray(data) ? data : [];
      setAccounts(list);
      setSelectedAccountId((prev) => {
        if (prev && list.some((account) => String(account.id) === String(prev))) return prev;
        return list[0] ? String(list[0].id) : '';
      });
    } catch (error) {
      toast.error(`加载 Cloudflare 账号失败：${error.message}`);
    } finally {
      setLoadingKey('accounts', false);
    }
  }, [cfApi, setLoadingKey]);

  const loadRecordTypes = useCallback(async () => {
    try {
      const data = await cfApi('/record-types');
      if (Array.isArray(data) && data.length > 0) setRecordTypes(data);
    } catch (error) {
      setRecordTypes(RECORD_TYPE_OPTIONS);
    }
  }, [cfApi]);

  const loadTemplates = useCallback(async () => {
    setLoadingKey('templates', true);
    try {
      const data = await cfApi('/templates');
      setTemplates(Array.isArray(data) ? data : []);
    } catch (error) {
      toast.error(`加载 DNS 模板失败：${error.message}`);
    } finally {
      setLoadingKey('templates', false);
    }
  }, [cfApi, setLoadingKey]);

  const loadZones = useCallback(async (accountId = selectedAccountId) => {
    if (!accountId) {
      setZones([]);
      setSelectedZoneId('');
      return;
    }
    setLoadingKey('zones', true);
    try {
      const data = await cfApi(`/accounts/${accountId}/zones`);
      const list = data.zones || [];
      setZones(list);
      setSelectedZoneId((prev) => (prev && list.some((zone) => zone.id === prev) ? prev : ''));
    } catch (error) {
      toast.error(`加载域名失败：${error.message}`);
    } finally {
      setLoadingKey('zones', false);
    }
  }, [cfApi, selectedAccountId, setLoadingKey]);

  const recordsSeqRef = useRef(0); // 切 zone/筛选竞态防护：过期响应直接丢弃

  const loadRecords = useCallback(async (zoneId = selectedZoneId, options = recordFilter) => {
    if (!selectedAccountId || !zoneId) {
      setRecords([]);
      return;
    }
    const seq = ++recordsSeqRef.current;
    const params = new URLSearchParams();
    if (options.type) params.set('type', options.type);
    if (options.name) params.set('name', options.name);

    setLoadingKey('records', true);
    try {
      const query = params.toString();
      const data = await cfApi(
        `/accounts/${selectedAccountId}/zones/${zoneId}/records${query ? `?${query}` : ''}`
      );
      if (seq !== recordsSeqRef.current) return; // 已有更新的请求，丢弃本次结果
      setRecords(data.records || []);
      setSelectedRecordIds([]);
    } catch (error) {
      if (seq !== recordsSeqRef.current) return;
      toast.error(`加载 DNS 记录失败：${error.message}`);
    } finally {
      if (seq === recordsSeqRef.current) setLoadingKey('records', false);
    }
  }, [cfApi, recordFilter, selectedAccountId, selectedZoneId, setLoadingKey]);

  const loadSsl = useCallback(async (zoneId = selectedZoneId) => {
    if (!selectedAccountId || !zoneId) {
      setSslInfo(null);
      return;
    }
    setLoadingKey('ssl', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/zones/${zoneId}/ssl`);
      setSslInfo(data.ssl || null);
    } catch (error) {
      setSslInfo(null);
    } finally {
      setLoadingKey('ssl', false);
    }
  }, [cfApi, selectedAccountId, selectedZoneId, setLoadingKey]);

  const loadAnalytics = useCallback(async (range = analyticsRange, zoneId = selectedZoneId) => {
    if (!selectedAccountId || !zoneId) {
      setAnalytics(null);
      return;
    }
    setLoadingKey('analytics', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/zones/${zoneId}/analytics?timeRange=${range}`);
      setAnalytics(data.analytics || EMPTY_ANALYTICS);
      setAnalyticsRange(range);
    } catch (error) {
      setAnalytics(EMPTY_ANALYTICS);
    } finally {
      setLoadingKey('analytics', false);
    }
  }, [analyticsRange, cfApi, selectedAccountId, selectedZoneId, setLoadingKey]);

  const selectZone = useCallback((zone) => {
    setSelectedZoneId(zone.id);
    setRecords([]);
    setSelectedRecordIds([]);
    loadRecords(zone.id);
    loadSsl(zone.id);
    loadAnalytics(analyticsRange, zone.id);
  }, [analyticsRange, loadAnalytics, loadRecords, loadSsl]);

  const loadWorkers = useCallback(async () => {
    if (!selectedAccountId) {
      setWorkers([]);
      return;
    }
    setLoadingKey('workers', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/workers`);
      setWorkers(data.workers || []);
      setWorkerSubdomain(data.subdomain || '');
    } catch (error) {
      toast.error(`加载 Workers 失败：${error.message}`);
    } finally {
      setLoadingKey('workers', false);
    }
  }, [cfApi, selectedAccountId, setLoadingKey]);

  const loadPages = useCallback(async () => {
    if (!selectedAccountId) {
      setPages([]);
      return;
    }
    setLoadingKey('pages', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/pages`);
      setPages(data.projects || []);
    } catch (error) {
      toast.error(`加载 Pages 失败：${error.message}`);
    } finally {
      setLoadingKey('pages', false);
    }
  }, [cfApi, selectedAccountId, setLoadingKey]);

  const loadR2Buckets = useCallback(async () => {
    if (!selectedAccountId) {
      setR2Buckets([]);
      setR2Metrics(null);
      return;
    }
    setLoadingKey('r2', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/r2/buckets`);
      setR2Buckets(data.buckets || []);
      const metrics = await cfApi(`/accounts/${selectedAccountId}/r2/metrics`).catch(() => null);
      setR2Metrics(metrics || null);
    } catch (error) {
      toast.error(`加载 R2 存储桶失败：${error.message}`);
    } finally {
      setLoadingKey('r2', false);
    }
  }, [cfApi, selectedAccountId, setLoadingKey]);

  const r2MetricsTotals = useMemo(() => {
    let bytes = 0;
    let objects = 0;
    ['standard', 'infrequentAccess'].forEach((storageClass) => {
      const group = r2Metrics?.[storageClass];
      if (!group) return;
      ['uploaded', 'published'].forEach((state) => {
        const entry = group?.[state];
        if (!entry) return;
        bytes += Number(entry.payloadSize || 0) + Number(entry.metadataSize || 0);
        objects += Number(entry.objects || 0);
      });
    });
    return { bytes, objects };
  }, [r2Metrics]);

  const r2CacheKey = useCallback((bucketName, prefix) => `${selectedAccountId}|${bucketName}|${prefix}`, [selectedAccountId]);

  const fetchR2Dir = useCallback(async (bucketName, prefix, { force = false } = {}) => {
    const key = r2CacheKey(bucketName, prefix);
    const cached = r2DirCache[key];
    if (cached && !force) return cached;
    const params = new URLSearchParams({ delimiter: '/', limit: '1000' });
    if (prefix) params.set('prefix', prefix);
    try {
      const data = await cfApi(
        `/accounts/${selectedAccountId}/r2/buckets/${encodeURIComponent(bucketName)}/objects?${params.toString()}`
      );
      const entry = { objects: data.objects || [], prefixes: data.delimited_prefixes || [] };
      setR2DirCache((prev) => ({ ...prev, [key]: entry }));
      setR2DirErrors((prev) => {
        if (!prev[key]) return prev;
        const next = { ...prev };
        delete next[key];
        return next;
      });
      return entry;
    } catch (error) {
      // 失败态单独记录，UI 显示「加载失败」行并可重试，避免永久 loading 占位。
      setR2DirErrors((prev) => ({ ...prev, [key]: true }));
      throw error;
    }
  }, [cfApi, r2CacheKey, r2DirCache, selectedAccountId]);

  const retryR2Dir = useCallback((prefix) => {
    const bucketName = r2SelectedBucket?.name;
    if (!bucketName || !selectedAccountId) return;
    const key = r2CacheKey(bucketName, prefix);
    setR2DirErrors((prev) => {
      if (!prev[key]) return prev;
      const next = { ...prev };
      delete next[key];
      return next;
    });
    fetchR2Dir(bucketName, prefix, { force: true }).catch((error) => toast.error(`加载目录「${prefix}」失败：${error.message}`));
  }, [fetchR2Dir, r2CacheKey, r2SelectedBucket, selectedAccountId]);

  const clearR2BucketCache = useCallback((bucketName) => {
    setR2DirCache((prev) => Object.fromEntries(
      Object.entries(prev).filter(([key]) => !key.startsWith(`${selectedAccountId}|${bucketName}|`))
    ));
    setR2DirErrors((prev) => Object.fromEntries(
      Object.entries(prev).filter(([key]) => !key.startsWith(`${selectedAccountId}|${bucketName}|`))
    ));
  }, [selectedAccountId]);

  const toggleR2FolderExpanded = useCallback((prefix) => {
    setR2ExpandedPrefixes((prev) => (prev.includes(prefix) ? prev.filter((p) => p !== prefix) : [...prev, prefix]));
  }, []);

  const loadR2Objects = useCallback(async (bucketName = r2SelectedBucket?.name, prefix = r2CurrentPrefix, { force = false } = {}) => {
    if (!selectedAccountId || !bucketName) return;
    const cached = r2DirCache[r2CacheKey(bucketName, prefix)];
    if (cached && !force) {
      setR2Objects(cached.objects);
      setR2Prefixes(cached.prefixes);
      if ((prefix || '') !== r2CurrentPrefix) setR2ObjectSearch('');
      setR2CurrentPrefix(prefix || '');
      setSelectedR2Objects([]);
      return;
    }
    setLoadingKey('r2Objects', true);
    try {
      const entry = await fetchR2Dir(bucketName, prefix, { force });
      setR2Objects(entry.objects);
      setR2Prefixes(entry.prefixes);
      if ((prefix || '') !== r2CurrentPrefix) setR2ObjectSearch('');
      setR2CurrentPrefix(prefix || '');
      setSelectedR2Objects([]);
    } catch (error) {
      toast.error(`加载 R2 对象失败：${error.message}`);
    } finally {
      setLoadingKey('r2Objects', false);
    }
  }, [cfApi, fetchR2Dir, r2CacheKey, r2CurrentPrefix, r2DirCache, r2SelectedBucket, selectedAccountId, setLoadingKey]);

  useEffect(() => {
    const bucketName = r2SelectedBucket?.name;
    if (!bucketName || !selectedAccountId) return;
    r2ExpandedPrefixes.forEach((prefix) => {
      const key = r2CacheKey(bucketName, prefix);
      if (!r2DirCache[key] && !r2DirErrors[key]) {
        fetchR2Dir(bucketName, prefix).catch((error) => toast.error(`加载目录「${prefix}」失败：${error.message}`));
      }
    });
  }, [fetchR2Dir, r2CacheKey, r2DirCache, r2DirErrors, r2ExpandedPrefixes, r2SelectedBucket, selectedAccountId]);

  const loadTunnels = useCallback(async () => {
    if (!selectedAccountId) {
      setTunnels([]);
      return;
    }
    setLoadingKey('tunnels', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/tunnels`);
      setTunnels(data.tunnels || []);
    } catch (error) {
      toast.error(`加载 Tunnel 失败：${error.message}`);
    } finally {
      setLoadingKey('tunnels', false);
    }
  }, [cfApi, selectedAccountId, setLoadingKey]);

  const refreshCurrentTab = useCallback(() => {
    if (activeTab === 'dns') {
      loadZones();
      if (selectedZoneId) {
        loadRecords();
        loadSsl();
        loadAnalytics();
      }
    } else if (activeTab === 'workers') loadWorkers();
    else if (activeTab === 'pages') loadPages();
    else if (activeTab === 'r2') {
      loadR2Buckets();
      if (r2SelectedBucket) loadR2Objects(r2SelectedBucket.name, r2CurrentPrefix);
    } else if (activeTab === 'tunnels') loadTunnels();
    else if (activeTab === 'templates') loadTemplates();
    else if (activeTab === 'accounts') loadAccounts();
  }, [
    activeTab,
    loadAccounts,
    loadAnalytics,
    loadPages,
    loadR2Buckets,
    loadR2Objects,
    loadRecords,
    loadSsl,
    loadTemplates,
    loadTunnels,
    loadWorkers,
    loadZones,
    r2CurrentPrefix,
    r2SelectedBucket,
    selectedZoneId,
  ]);

  useEffect(() => {
    loadAccounts();
    loadRecordTypes();
    loadTemplates();
  }, [loadAccounts, loadRecordTypes, loadTemplates]);

  useEffect(() => {
    if (!selectedAccountId) return;
    if (activeTab === 'dns') loadZones(selectedAccountId);
    if (activeTab === 'workers') loadWorkers();
    if (activeTab === 'pages') loadPages();
    if (activeTab === 'r2') loadR2Buckets();
    if (activeTab === 'tunnels') loadTunnels();
  }, [activeTab, loadPages, loadR2Buckets, loadTunnels, loadWorkers, loadZones, selectedAccountId]);

  const openAccountModal = (account = null) => {
    setModal({ type: 'account', data: account });
    setAccountForm(account
      ? { name: account.name || '', email: account.email || '', cfAccountId: account.cfAccountId || '', apiToken: '', skipVerify: false }
      : EMPTY_ACCOUNT_FORM);
  };

  const saveAccount = async () => {
    if (!accountForm.name.trim() || (!modal.data && !accountForm.apiToken.trim())) {
      toast.warning('请填写账号名称和 API 令牌');
      return;
    }
    setLoadingKey('saveAccount', true);
    try {
      const isEdit = Boolean(modal.data);
      await cfApi(isEdit ? `/accounts/${modal.data.id}` : '/accounts', {
        method: isEdit ? 'PUT' : 'POST',
        body: JSON.stringify(accountForm),
      });
      toast.success(isEdit ? '账号已更新' : '账号已添加');
      closeModal();
      loadAccounts();
    } catch (error) {
      toast.error(`保存账号失败：${error.message}`);
    } finally {
      setLoadingKey('saveAccount', false);
    }
  };

  const deleteAccount = async (account) => {
    if (!confirmPress(`account:${account.id}`, `删除 Cloudflare 账号「${account.name}」`)) return;
    try {
      await cfApi(`/accounts/${account.id}`, { method: 'DELETE' });
      toast.success('账号已删除');
      setSelectedAccountId((prev) => (String(prev) === String(account.id) ? '' : prev));
      loadAccounts();
    } catch (error) {
      toast.error(`删除账号失败：${error.message}`);
    }
  };

  const verifyAccount = async (account) => {
    setLoadingKey(`verify-${account.id}`, true);
    try {
      const result = await cfApi(`/accounts/${account.id}/verify`, { method: 'POST' });
      if (result.valid) toast.success('账号令牌有效');
      else toast.error(`账号令牌无效：${result.error || '未知错误'}`);
    } catch (error) {
      toast.error(`验证失败：${error.message}`);
    } finally {
      setLoadingKey(`verify-${account.id}`, false);
    }
  };

  const toggleAccountToken = async (account) => {
    if (accountTokens[account.id]) {
      setAccountTokens((prev) => ({ ...prev, [account.id]: '' }));
      return;
    }
    try {
      const data = await cfApi(`/accounts/${account.id}/token`);
      setAccountTokens((prev) => ({ ...prev, [account.id]: data.apiToken || '' }));
    } catch (error) {
      toast.error(`获取令牌失败：${error.message}`);
    }
  };

  const exportAccounts = async () => {
    try {
      const data = await cfApi('/export/accounts');
      downloadJson(`cloudflare-accounts-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`, {
        version: '1.0',
        exportTime: new Date().toISOString(),
        accounts: Array.isArray(data) ? data : data.accounts || [],
      });
      toast.success('账号已导出');
    } catch (error) {
      toast.error(`导出账号失败：${error.message}`);
    }
  };

  const openImportModal = (kind) => {
    setImportState({ kind, text: '', overwrite: false });
    setModal({ type: 'import', data: { kind } });
  };

  const submitImport = async () => {
    try {
      if (importState.kind === 'accounts') {
        const accountsToImport = parseJsonInput(importState.text, 'accounts');
        if (!Array.isArray(accountsToImport)) throw new Error('导入内容必须是账号数组或包含 accounts 的对象');
        await cfApi('/import/accounts', {
          method: 'POST',
          body: JSON.stringify({ accounts: accountsToImport, overwrite: importState.overwrite }),
        });
        toast.success(`已导入 ${accountsToImport.length} 个账号`);
        loadAccounts();
      } else if (importState.kind === 'templates') {
        const templatesToImport = parseJsonInput(importState.text, 'templates');
        if (!Array.isArray(templatesToImport)) throw new Error('导入内容必须是模板数组或包含 templates 的对象');
        await cfApi('/import/templates', {
          method: 'POST',
          body: JSON.stringify({ templates: templatesToImport, overwrite: importState.overwrite }),
        });
        toast.success(`已导入 ${templatesToImport.length} 个模板`);
        loadTemplates();
      } else if (importState.kind === 'records') {
        if (!selectedAccountId || !selectedZoneId) throw new Error('请先选择域名');
        const recordsToImport = parseJsonInput(importState.text, 'records');
        if (!Array.isArray(recordsToImport)) throw new Error('导入内容必须是记录数组或包含 records 的对象');
        const result = await cfApi(`/accounts/${selectedAccountId}/zones/${selectedZoneId}/batch`, {
          method: 'POST',
          body: JSON.stringify({ records: recordsToImport }),
        });
        toast.success(`导入完成：成功 ${result.created || 0} 条，失败 ${result.failed || 0} 条`);
        loadRecords();
      }
      closeModal();
    } catch (error) {
      toast.error(`导入失败：${error.message}`);
    }
  };

  const openZoneModal = () => {
    setZoneForm(EMPTY_ZONE_FORM);
    setModal({ type: 'zone', data: null });
  };

  const saveZone = async () => {
    if (!selectedAccountId) {
      toast.warning('请先选择 Cloudflare 账号');
      return;
    }
    if (!zoneForm.name.trim()) {
      toast.warning('请填写域名');
      return;
    }
    setLoadingKey('saveZone', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/zones`, {
        method: 'POST',
        body: JSON.stringify(zoneForm),
      });
      toast.success('域名已添加');
      closeModal();
      loadZones();
    } catch (error) {
      toast.error(`添加域名失败：${error.message}`);
    } finally {
      setLoadingKey('saveZone', false);
    }
  };

  const deleteZone = async (zone = selectedZone) => {
    if (!zone || !selectedAccountId) return;
    if (!confirmPress(`zone:${zone.id}`, `删除域名「${zone.name}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/zones/${zone.id}`, { method: 'DELETE' });
      toast.success('域名已删除');
      setSelectedZoneId('');
      setRecords([]);
      loadZones();
    } catch (error) {
      toast.error(`删除域名失败：${error.message}`);
    }
  };

  const purgeZoneCache = async () => {
    if (!selectedZoneId) {
      toast.warning('请先选择域名');
      return;
    }
    if (!confirmPress(`zone-cache:${selectedZoneId}`, `清除「${selectedZone?.name}」的 CDN 缓存`)) return;
    setLoadingKey('purge', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/zones/${selectedZoneId}/purge`, {
        method: 'POST',
        body: JSON.stringify({ purge_everything: true }),
      });
      toast.success('缓存已清除');
    } catch (error) {
      toast.error(`清除缓存失败：${error.message}`);
    } finally {
      setLoadingKey('purge', false);
    }
  };

  const updateSslMode = async (mode) => {
    if (!selectedZoneId) return;
    setLoadingKey('ssl', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/zones/${selectedZoneId}/ssl`, {
        method: 'PATCH',
        body: JSON.stringify({ mode }),
      });
      setSslInfo((prev) => ({ ...(prev || {}), ...(data.ssl || {}) }));
      toast.success('SSL 模式已更新');
    } catch (error) {
      toast.error(`更新 SSL 模式失败：${error.message}`);
    } finally {
      setLoadingKey('ssl', false);
    }
  };

  const openRecordModal = (record = null) => {
    setRecordForm(record
      ? {
        type: record.type || 'A',
        name: recordShortName(record.name, selectedZone?.name),
        content: record.content || '',
        ttl: record.ttl || 1,
        proxied: Boolean(record.proxied),
        priority: record.priority || 10,
      }
      : EMPTY_RECORD_FORM);
    setModal({ type: 'record', data: record });
  };

  const saveRecord = async () => {
    if (!selectedZoneId) {
      toast.warning('请先选择域名');
      return;
    }
    if (!recordForm.name.trim() || !recordForm.content.trim()) {
      toast.warning('请填写记录名称和内容');
      return;
    }
    const payload = {
      ...recordForm,
      ttl: Number(recordForm.ttl) || 1,
      priority: Number(recordForm.priority) || 10,
    };
    setLoadingKey('saveRecord', true);
    try {
      const isEdit = Boolean(modal.data);
      await cfApi(
        isEdit
          ? `/accounts/${selectedAccountId}/zones/${selectedZoneId}/records/${modal.data.id}`
          : `/accounts/${selectedAccountId}/zones/${selectedZoneId}/records`,
        {
          method: isEdit ? 'PUT' : 'POST',
          body: JSON.stringify(payload),
        }
      );
      toast.success(isEdit ? '记录已更新' : '记录已添加');
      closeModal();
      loadRecords();
    } catch (error) {
      toast.error(`保存记录失败：${error.message}`);
    } finally {
      setLoadingKey('saveRecord', false);
    }
  };

  const deleteRecord = async (record) => {
    if (!confirmPress(`record:${record.id}`, `删除记录「${record.name}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/zones/${selectedZoneId}/records/${record.id}`, {
        method: 'DELETE',
      });
      toast.success('记录已删除');
      loadRecords();
    } catch (error) {
      toast.error(`删除记录失败：${error.message}`);
    }
  };

  const batchDeleteRecords = async () => {
    if (selectedRecordIds.length === 0) return;
    if (!confirmPress('batch-records', `删除选中的 ${selectedRecordIds.length} 条 DNS 记录`)) return;
    setLoadingKey('batchDeleteRecords', true);
    try {
      const results = await Promise.allSettled(selectedRecords.map((record) => cfApi(
        `/accounts/${selectedAccountId}/zones/${selectedZoneId}/records/${record.id}`,
        { method: 'DELETE' }
      )));
      const errors = results.filter(r => r.status === 'rejected');
      if (errors.length > 0) {
        toast.error(`批量删除完成，但 ${errors.length}/${selectedRecords.length} 条失败：${errors[0].reason?.message || '未知错误'}`);
      } else {
        toast.success('选中记录已删除');
      }
      loadRecords();
    } finally {
      setLoadingKey('batchDeleteRecords', false);
    }
  };

  const exportRecords = () => {
    if (!selectedZoneId) {
      toast.warning('请先选择域名');
      return;
    }
    downloadJson(`dns-${selectedZone?.name || selectedZoneId}-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`, {
      version: '1.0',
      exportTime: new Date().toISOString(),
      zoneName: selectedZone?.name,
      zoneId: selectedZoneId,
      records: records.map((record) => ({
        type: record.type,
        name: recordShortName(record.name, selectedZone?.name),
        content: record.content,
        ttl: record.ttl,
        proxied: Boolean(record.proxied),
        priority: record.priority,
      })),
    });
    toast.success('DNS 记录已导出');
  };

  const openTemplateModal = (template = null) => {
    const firstRecord = template?.records?.[0] || template || {};
    setTemplateForm(template
      ? {
        name: template.name || '',
        description: template.description || '',
        type: firstRecord.type || 'A',
        recordName: firstRecord.name || '@',
        content: firstRecord.content || '',
        ttl: firstRecord.ttl || 1,
        proxied: Boolean(firstRecord.proxied),
        priority: firstRecord.priority || 10,
      }
      : EMPTY_TEMPLATE_FORM);
    setModal({ type: 'template', data: template });
  };

  const saveTemplate = async () => {
    if (!templateForm.name.trim() || !templateForm.content.trim()) {
      toast.warning('请填写模板名称和记录内容');
      return;
    }
    const payload = {
      name: templateForm.name,
      description: templateForm.description,
      records: [{
        type: templateForm.type,
        name: templateForm.recordName || '@',
        content: templateForm.content,
        ttl: Number(templateForm.ttl) || 1,
        proxied: Boolean(templateForm.proxied),
        priority: Number(templateForm.priority) || 10,
      }],
    };
    setLoadingKey('saveTemplate', true);
    try {
      const isEdit = Boolean(modal.data);
      await cfApi(isEdit ? `/templates/${modal.data.id}` : '/templates', {
        method: isEdit ? 'PUT' : 'POST',
        body: JSON.stringify(payload),
      });
      toast.success(isEdit ? '模板已更新' : '模板已添加');
      closeModal();
      loadTemplates();
    } catch (error) {
      toast.error(`保存模板失败：${error.message}`);
    } finally {
      setLoadingKey('saveTemplate', false);
    }
  };

  const deleteTemplate = async (template) => {
    if (!confirmPress(`template:${template.id}`, `删除模板「${template.name}」`)) return;
    try {
      await cfApi(`/templates/${template.id}`, { method: 'DELETE' });
      toast.success('模板已删除');
      loadTemplates();
    } catch (error) {
      toast.error(`删除模板失败：${error.message}`);
    }
  };

  const applyTemplate = async (template) => {
    if (!selectedZoneId) {
      toast.warning('请先在“域名与 DNS”中选择域名');
      return;
    }
    const recordName = await dialog.prompt({
      message: '请输入应用到 DNS 的记录名称，留空使用模板中的名称',
      defaultValue: '',
    });
    if (recordName === null) return;
    try {
      const result = await cfApi(`/templates/${template.id}/apply`, {
        method: 'POST',
        body: JSON.stringify({ accountId: selectedAccountId, zoneId: selectedZoneId, recordName: recordName.trim() || undefined }),
      });
      toast.success(`模板已应用：成功 ${result.created || 0} 条，失败 ${result.failed || 0} 条`);
      loadRecords();
    } catch (error) {
      toast.error(`应用模板失败：${error.message}`);
    }
  };

  const openWorkerModal = async (worker = null) => {
    setLoadingKey('workerScript', Boolean(worker));
    setModal({ type: 'worker', data: worker });
    if (!worker) {
      setWorkerForm(EMPTY_WORKER_FORM);
      return;
    }
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(worker.name)}`);
      setWorkerForm({ name: worker.name, script: data.worker?.script || '' });
    } catch (error) {
      toast.error(`加载 Worker 脚本失败：${error.message}`);
      setWorkerForm({ name: worker.name, script: '' });
    } finally {
      setLoadingKey('workerScript', false);
    }
  };

  const saveWorker = async () => {
    if (!workerForm.name.trim() || !workerForm.script.trim()) {
      toast.warning('请填写 Worker 名称和脚本内容');
      return;
    }
    setLoadingKey('saveWorker', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(workerForm.name.trim())}`, {
        method: 'PUT',
        body: JSON.stringify({ script: workerForm.script }),
      });
      toast.success('Worker 已保存');
      closeModal();
      loadWorkers();
    } catch (error) {
      toast.error(`保存 Worker 失败：${error.message}`);
    } finally {
      setLoadingKey('saveWorker', false);
    }
  };

  const deleteWorker = async (worker) => {
    if (!confirmPress(`worker:${worker.id}`, `删除 Worker「${worker.name}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(worker.name)}`, {
        method: 'DELETE',
      });
      toast.success('Worker 已删除');
      loadWorkers();
    } catch (error) {
      toast.error(`删除 Worker 失败：${error.message}`);
    }
  };

  const toggleWorkerSubdomain = async (worker, enabled) => {
    try {
      await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(worker.name)}/toggle`, {
        method: 'POST',
        body: JSON.stringify({ enabled }),
      });
      toast.success(enabled ? 'Worker 子域名访问已启用' : 'Worker 子域名访问已停用');
    } catch (error) {
      toast.error(`切换 Worker 状态失败：${error.message}`);
    }
  };

  const openWorkerRoutesModal = async (worker) => {
    if (!selectedZoneId) {
      toast.warning('请先在“域名与 DNS”中选择用于路由的域名');
      return;
    }
    setWorkerRouteState({ worker, routes: [], form: { id: '', pattern: '', script: worker.name } });
    setModal({ type: 'workerRoutes', data: worker });
    setLoadingKey('workerRoutes', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/zones/${selectedZoneId}/workers/routes`);
      setWorkerRouteState((prev) => ({
        ...prev,
        routes: (data.routes || []).filter((route) => !route.script || route.script === worker.name),
      }));
    } catch (error) {
      toast.error(`加载 Worker 路由失败：${error.message}`);
    } finally {
      setLoadingKey('workerRoutes', false);
    }
  };

  const saveWorkerRoute = async () => {
    const form = workerRouteState.form;
    if (!form.pattern.trim() || !form.script.trim()) {
      toast.warning('请填写路由规则和 Worker 名称');
      return;
    }
    setLoadingKey('saveWorkerRoute', true);
    try {
      await cfApi(
        form.id
          ? `/accounts/${selectedAccountId}/zones/${selectedZoneId}/workers/routes/${form.id}`
          : `/accounts/${selectedAccountId}/zones/${selectedZoneId}/workers/routes`,
        {
          method: form.id ? 'PUT' : 'POST',
          body: JSON.stringify({ pattern: form.pattern, script: form.script }),
        }
      );
      toast.success(form.id ? 'Worker 路由已更新' : 'Worker 路由已添加');
      openWorkerRoutesModal(workerRouteState.worker);
    } catch (error) {
      toast.error(`保存 Worker 路由失败：${error.message}`);
    } finally {
      setLoadingKey('saveWorkerRoute', false);
    }
  };

  const deleteWorkerRoute = async (route) => {
    if (!confirmPress(`worker-route:${route.id}`, `删除路由「${route.pattern}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/zones/${selectedZoneId}/workers/routes/${route.id}`, {
        method: 'DELETE',
      });
      toast.success('Worker 路由已删除');
      openWorkerRoutesModal(workerRouteState.worker);
    } catch (error) {
      toast.error(`删除 Worker 路由失败：${error.message}`);
    }
  };

  const openWorkerDomainsModal = async (worker) => {
    setWorkerDomainState({ worker, domains: [], hostname: '', environment: 'production' });
    setModal({ type: 'workerDomains', data: worker });
    setLoadingKey('workerDomains', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(worker.name)}/domains`);
      setWorkerDomainState((prev) => ({ ...prev, domains: data.domains || [] }));
    } catch (error) {
      toast.error(`加载 Worker 域名失败：${error.message}`);
    } finally {
      setLoadingKey('workerDomains', false);
    }
  };

  const addWorkerDomain = async () => {
    if (!workerDomainState.hostname.trim()) {
      toast.warning('请填写域名');
      return;
    }
    setLoadingKey('saveWorkerDomain', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(workerDomainState.worker.name)}/domains`, {
        method: 'POST',
        body: JSON.stringify({
          hostname: workerDomainState.hostname.trim(),
          environment: workerDomainState.environment || 'production',
        }),
      });
      toast.success('Worker 域名已添加');
      openWorkerDomainsModal(workerDomainState.worker);
    } catch (error) {
      toast.error(`添加 Worker 域名失败：${error.message}`);
    } finally {
      setLoadingKey('saveWorkerDomain', false);
    }
  };

  const deleteWorkerDomain = async (domain) => {
    if (!confirmPress(`worker-domain:${domain.id}`, `删除 Worker 域名「${domain.hostname}」`)) return;
    try {
      await cfApi(
        `/accounts/${selectedAccountId}/workers/${encodeURIComponent(workerDomainState.worker.name)}/domains/${domain.id}`,
        { method: 'DELETE' }
      );
      toast.success('Worker 域名已删除');
      openWorkerDomainsModal(workerDomainState.worker);
    } catch (error) {
      toast.error(`删除 Worker 域名失败：${error.message}`);
    }
  };

  const openWorkerAnalyticsModal = async (worker) => {
    setWorkerAnalyticsState({ worker, analytics: null });
    setModal({ type: 'workerAnalytics', data: worker });
    setLoadingKey('workerAnalytics', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/workers/${encodeURIComponent(worker.name)}/analytics`);
      setWorkerAnalyticsState({ worker, analytics: data.analytics || {} });
    } catch (error) {
      toast.error(`加载 Worker 统计失败：${error.message}`);
    } finally {
      setLoadingKey('workerAnalytics', false);
    }
  };

  const deletePagesProject = async (project) => {
    if (!confirmPress(`pages-project:${project.id}`, `删除 Pages 项目「${project.name}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/pages/${encodeURIComponent(project.name)}`, {
        method: 'DELETE',
      });
      toast.success('Pages 项目已删除');
      loadPages();
    } catch (error) {
      toast.error(`删除 Pages 项目失败：${error.message}`);
    }
  };

  const openPagesDeploymentsModal = async (project) => {
    setPagesDeployState({ project, deployments: [] });
    setModal({ type: 'pagesDeployments', data: project });
    setLoadingKey('pagesDeployments', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/pages/${encodeURIComponent(project.name)}/deployments`);
      setPagesDeployState({ project, deployments: data.deployments || [] });
    } catch (error) {
      toast.error(`加载 Pages 部署失败：${error.message}`);
    } finally {
      setLoadingKey('pagesDeployments', false);
    }
  };

  const deletePagesDeployment = async (deployment) => {
    if (!confirmPress(`pages-deployment:${deployment.id}`, '删除该 Pages 部署')) return;
    try {
      await cfApi(
        `/accounts/${selectedAccountId}/pages/${encodeURIComponent(pagesDeployState.project.name)}/deployments/${deployment.id}`,
        { method: 'DELETE' }
      );
      toast.success('Pages 部署已删除');
      openPagesDeploymentsModal(pagesDeployState.project);
    } catch (error) {
      toast.error(`删除 Pages 部署失败：${error.message}`);
    }
  };

  const openPagesDomainsModal = async (project) => {
    setPagesDomainState({ project, domains: [], domain: '' });
    setModal({ type: 'pagesDomains', data: project });
    setLoadingKey('pagesDomains', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/pages/${encodeURIComponent(project.name)}/domains`);
      setPagesDomainState((prev) => ({ ...prev, domains: data.domains || [] }));
    } catch (error) {
      toast.error(`加载 Pages 域名失败：${error.message}`);
    } finally {
      setLoadingKey('pagesDomains', false);
    }
  };

  const addPagesDomain = async () => {
    if (!pagesDomainState.domain.trim()) {
      toast.warning('请填写域名');
      return;
    }
    setLoadingKey('savePagesDomain', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/pages/${encodeURIComponent(pagesDomainState.project.name)}/domains`, {
        method: 'POST',
        body: JSON.stringify({ domain: pagesDomainState.domain.trim() }),
      });
      toast.success('Pages 域名已添加');
      openPagesDomainsModal(pagesDomainState.project);
    } catch (error) {
      toast.error(`添加 Pages 域名失败：${error.message}`);
    } finally {
      setLoadingKey('savePagesDomain', false);
    }
  };

  const deletePagesDomain = async (domain) => {
    if (!confirmPress(`pages-domain:${domain.id}`, `删除 Pages 域名「${domain.name}」`)) return;
    try {
      await cfApi(
        `/accounts/${selectedAccountId}/pages/${encodeURIComponent(pagesDomainState.project.name)}/domains/${encodeURIComponent(domain.name)}`,
        { method: 'DELETE' }
      );
      toast.success('Pages 域名已删除');
      openPagesDomainsModal(pagesDomainState.project);
    } catch (error) {
      toast.error(`删除 Pages 域名失败：${error.message}`);
    }
  };

  const createR2Bucket = async () => {
    if (!r2BucketForm.name.trim()) {
      toast.warning('请填写存储桶名称');
      return;
    }
    setLoadingKey('saveR2Bucket', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/r2/buckets`, {
        method: 'POST',
        body: JSON.stringify(r2BucketForm),
      });
      toast.success('R2 存储桶已创建');
      closeModal();
      setR2BucketForm({ name: '', location: 'auto' });
      loadR2Buckets();
    } catch (error) {
      toast.error(`创建 R2 存储桶失败：${error.message}`);
    } finally {
      setLoadingKey('saveR2Bucket', false);
    }
  };

  const deleteR2Bucket = async (bucket) => {
    if (!confirmPress(`r2-bucket:${bucket.name}`, `删除 R2 存储桶「${bucket.name}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/r2/buckets/${encodeURIComponent(bucket.name)}`, {
        method: 'DELETE',
      });
      toast.success('R2 存储桶已删除');
      if (r2SelectedBucket?.name === bucket.name) {
        setR2SelectedBucket(null);
        setR2Objects([]);
        setR2Prefixes([]);
      }
      loadR2Buckets();
    } catch (error) {
      toast.error(`删除 R2 存储桶失败：${error.message}`);
    }
  };

  const selectR2Bucket = async (bucket) => {
    setR2SelectedBucket(bucket);
    setR2CurrentPrefix('');
    setR2ExpandedPrefixes([]);
    setR2DirErrors({});
    await loadR2Objects(bucket.name, '');
  };

  const r2ObjectApiPath = (objectKey, suffix = '') => (
    `/accounts/${selectedAccountId}/r2/buckets/${encodeURIComponent(r2SelectedBucket.name)}/objects/${encodeURIComponent(objectKey)}${suffix}`
  );

  const uploadR2Object = async (objectKey, body, contentType = 'application/octet-stream') => {
    if (!selectedAccountId || !r2SelectedBucket?.name) throw new Error('请先选择 R2 存储桶');
    const response = await fetch(`/api/cloudflare${r2ObjectApiPath(objectKey)}`, {
      method: 'PUT',
      headers: {
        'Content-Type': contentType || 'application/octet-stream',
      },
      body,
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || payload.error) {
      throw new Error(payload.error || `上传失败：HTTP ${response.status}`);
    }
    return payload;
  };

  const handleR2UploadFiles = async (event) => {
    const files = Array.from(event.target.files || []);
    event.target.value = '';
    if (files.length === 0) return;
    setLoadingKey('uploadR2', true);
    try {
      const results = await Promise.allSettled(files.map((file) => {
        const relativePath = file.webkitRelativePath || file.name;
        const key = `${r2CurrentPrefix}${relativePath}`.replace(/^\/+/, '');
        return uploadR2Object(key, file, file.type || 'application/octet-stream');
      }));
      const errors = results.filter(r => r.status === 'rejected');
      if (errors.length > 0) {
        toast.error(`R2 上传完成，但 ${errors.length}/${files.length} 个文件失败：${errors[0].reason?.message || '未知错误'}`);
      } else {
        toast.success(`已上传 ${files.length} 个文件`);
      }
      clearR2BucketCache(r2SelectedBucket.name);
      await loadR2Objects(r2SelectedBucket.name, r2CurrentPrefix);
    } finally {
      setLoadingKey('uploadR2', false);
    }
  };

  const createR2Folder = async () => {
    const folderName = r2FolderForm.name.trim().replace(/^\/+|\/+$/g, '');
    if (!folderName) {
      toast.warning('请输入文件夹名称');
      return;
    }
    setLoadingKey('createR2Folder', true);
    try {
      const key = `${r2CurrentPrefix}${folderName}/.keep`;
      await uploadR2Object(key, new Blob([''], { type: 'application/octet-stream' }));
      toast.success('文件夹已创建');
      setR2FolderForm({ name: '' });
      setModal({ type: null, data: null });
      clearR2BucketCache(r2SelectedBucket.name);
      await loadR2Objects(r2SelectedBucket.name, r2CurrentPrefix);
    } catch (error) {
      toast.error(`创建文件夹失败：${error.message}`);
    } finally {
      setLoadingKey('createR2Folder', false);
    }
  };

  const listAllR2Keys = async (bucketName, prefix) => {
    const keys = [];
    let cursor = '';
    do {
      const params = new URLSearchParams({ prefix, limit: '1000' });
      if (cursor) params.set('cursor', cursor);
      const data = await cfApi(
        `/accounts/${selectedAccountId}/r2/buckets/${encodeURIComponent(bucketName)}/objects?${params.toString()}`
      );
      (data.objects || []).forEach((object) => {
        const key = object.key || object.name;
        if (key) keys.push(key);
      });
      cursor = data.cursor || '';
    } while (cursor);
    return keys;
  };

  const deleteR2KeysChunked = async (bucketName, keys, chunkSize = 25) => {
    let totalErrors = 0;
    const firstError = [];
    for (let i = 0; i < keys.length; i += chunkSize) {
      const results = await Promise.allSettled(keys.slice(i, i + chunkSize).map((key) => cfApi(
        `/accounts/${selectedAccountId}/r2/buckets/${encodeURIComponent(bucketName)}/objects/${encodeURIComponent(key)}`,
        { method: 'DELETE' }
      )));
      const rejected = results.filter(r => r.status === 'rejected');
      totalErrors += rejected.length;
      if (rejected.length > 0 && firstError.length === 0) {
        firstError.push(rejected[0].reason?.message || '未知错误');
      }
    }
    if (totalErrors > 0) {
      throw new Error(`${totalErrors}/${keys.length} 个对象删除失败：${firstError[0]}`);
    }
  };

  const deleteR2Object = async (objectKey) => {
    const isFolder = String(objectKey).endsWith('/');
    if (!confirmPress(`r2-object:${objectKey}`, isFolder ? `删除目录「${objectKey}」及其全部内容？` : `删除对象「${objectKey}」？`)) return;
    try {
      if (isFolder) {
        const keys = await listAllR2Keys(r2SelectedBucket.name, objectKey);
        await deleteR2KeysChunked(r2SelectedBucket.name, keys);
      } else {
        await cfApi(
          `/accounts/${selectedAccountId}/r2/buckets/${encodeURIComponent(r2SelectedBucket.name)}/objects/${encodeURIComponent(objectKey)}`,
          { method: 'DELETE' }
        );
      }
      toast.success(isFolder ? `目录「${objectKey}」已删除` : 'R2 对象已删除');
      clearR2BucketCache(r2SelectedBucket.name);
      loadR2Objects();
    } catch (error) {
      toast.error(`删除 R2 ${isFolder ? '目录' : '对象'}失败：${error.message}`);
    }
  };

  const batchDeleteR2Objects = async () => {
    if (selectedR2Objects.length === 0) return;
    if (!confirmPress('batch-r2-objects', `删除选中的 ${selectedR2Objects.length} 个 R2 对象（含目录全部内容）`)) return;
    setLoadingKey('batchDeleteR2', true);
    try {
      const folderKeys = selectedR2Objects.filter((key) => String(key).endsWith('/'));
      const fileKeys = selectedR2Objects.filter((key) => !String(key).endsWith('/'));
      if (fileKeys.length > 0) {
        await deleteR2KeysChunked(r2SelectedBucket.name, fileKeys);
      }
      for (const folderKey of folderKeys) {
        const keys = await listAllR2Keys(r2SelectedBucket.name, folderKey);
        await deleteR2KeysChunked(r2SelectedBucket.name, keys);
      }
      toast.success('选中的 R2 对象已删除');
      clearR2BucketCache(r2SelectedBucket.name);
      loadR2Objects();
    } catch (error) {
      toast.error(`批量删除 R2 对象失败：${error.message}`);
    } finally {
      setLoadingKey('batchDeleteR2', false);
    }
  };

  const downloadR2Object = async (objectKey) => {
    window.open(`/api/cloudflare${r2ObjectApiPath(objectKey, '/download')}`, '_blank', 'noopener,noreferrer');
  };

  const downloadR2Folder = (prefix) => {
    window.open(`/api/cloudflare/accounts/${encodeURIComponent(selectedAccountId)}/r2/buckets/${encodeURIComponent(r2SelectedBucket.name)}/objects/folder-download?prefix=${encodeURIComponent(prefix)}`, '_blank', 'noopener,noreferrer');
  };

  const r2ObjectPreviewUrl = (objectKey) => (
    `/api/cloudflare/accounts/${encodeURIComponent(selectedAccountId)}/r2/buckets/${encodeURIComponent(r2SelectedBucket.name)}/objects/${encodeURIComponent(objectKey)}/preview`
  );

  const previewR2Object = (objectKey) => {
    setModal({
      type: 'r2Preview',
      data: {
        key: objectKey,
        name: objectFileName(objectKey),
        kind: r2PreviewKind(objectKey),
        url: r2ObjectPreviewUrl(objectKey),
      },
    });
  };

  const createTunnel = async () => {
    if (!tunnelForm.name.trim()) {
      toast.warning('请填写 Tunnel 名称');
      return;
    }
    setLoadingKey('saveTunnel', true);
    try {
      await cfApi(`/accounts/${selectedAccountId}/tunnels`, {
        method: 'POST',
        body: JSON.stringify({ name: tunnelForm.name.trim() }),
      });
      toast.success('Tunnel 已创建');
      closeModal();
      setTunnelForm({ name: '' });
      loadTunnels();
    } catch (error) {
      toast.error(`创建 Tunnel 失败：${error.message}`);
    } finally {
      setLoadingKey('saveTunnel', false);
    }
  };

  const renameTunnel = async (tunnel) => {
    const name = await dialog.prompt({
      message: '请输入新的 Tunnel 名称',
      defaultValue: tunnel.name,
    });
    if (!name || name === tunnel.name) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/tunnels/${tunnel.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ name }),
      });
      toast.success('Tunnel 已重命名');
      loadTunnels();
    } catch (error) {
      toast.error(`重命名 Tunnel 失败：${error.message}`);
    }
  };

  const deleteTunnel = async (tunnel) => {
    if (!confirmPress(`tunnel:${tunnel.id}`, `删除 Tunnel「${tunnel.name}」`)) return;
    try {
      await cfApi(`/accounts/${selectedAccountId}/tunnels/${tunnel.id}`, { method: 'DELETE' });
      toast.success('Tunnel 已删除');
      loadTunnels();
    } catch (error) {
      toast.error(`删除 Tunnel 失败：${error.message}`);
    }
  };

  const openTunnelTokenModal = async (tunnel) => {
    setTunnelTokenState({ tunnel, token: '' });
    setModal({ type: 'tunnelToken', data: tunnel });
    setLoadingKey('tunnelToken', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/tunnels/${tunnel.id}/token`);
      setTunnelTokenState({ tunnel, token: data.token || '' });
    } catch (error) {
      toast.error(`获取 Tunnel 令牌失败：${error.message}`);
    } finally {
      setLoadingKey('tunnelToken', false);
    }
  };

  const openTunnelConfigModal = async (tunnel) => {
    setTunnelConfigState({ tunnel, text: EMPTY_TUNNEL_CONFIG });
    setModal({ type: 'tunnelConfig', data: tunnel });
    setLoadingKey('tunnelConfig', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/tunnels/${tunnel.id}/configuration`);
      setTunnelConfigState({ tunnel, text: JSON.stringify(data.config || { ingress: [] }, null, 2) });
    } catch (error) {
      toast.error(`加载 Tunnel 配置失败：${error.message}`);
    } finally {
      setLoadingKey('tunnelConfig', false);
    }
  };

  const saveTunnelConfig = async () => {
    try {
      const config = JSON.parse(tunnelConfigState.text);
      await cfApi(`/accounts/${selectedAccountId}/tunnels/${tunnelConfigState.tunnel.id}/configuration`, {
        method: 'PUT',
        body: JSON.stringify({ config }),
      });
      toast.success('Tunnel 配置已保存');
      closeModal();
    } catch (error) {
      toast.error(`保存 Tunnel 配置失败：${error.message}`);
    }
  };

  const openTunnelConnectionsModal = async (tunnel) => {
    setTunnelConnectionState({ tunnel, connections: [] });
    setModal({ type: 'tunnelConnections', data: tunnel });
    setLoadingKey('tunnelConnections', true);
    try {
      const data = await cfApi(`/accounts/${selectedAccountId}/tunnels/${tunnel.id}/connections`);
      setTunnelConnectionState({ tunnel, connections: data.connections || [] });
    } catch (error) {
      toast.error(`加载 Tunnel 连接失败：${error.message}`);
    } finally {
      setLoadingKey('tunnelConnections', false);
    }
  };

  const cleanupTunnelConnections = async (tunnel, clientId = '') => {
    const message = clientId ? '确定要清理该连接吗？' : `确定要清理 Tunnel“${tunnel.name}”的全部连接吗？`;
    if (!(await dialog.confirm(message))) return;
    try {
      await cfApi(
        `/accounts/${selectedAccountId}/tunnels/${tunnel.id}/connections${clientId ? `?clientId=${encodeURIComponent(clientId)}` : ''}`,
        { method: 'DELETE' }
      );
      toast.success('Tunnel 连接已清理');
      openTunnelConnectionsModal(tunnel);
      loadTunnels();
    } catch (error) {
      toast.error(`清理 Tunnel 连接失败：${error.message}`);
    }
  };

  const toggleRecordSelection = (recordId, checked) => {
    setSelectedRecordIds((prev) => {
      if (checked) return prev.includes(recordId) ? prev : [...prev, recordId];
      return prev.filter((id) => id !== recordId);
    });
  };

  const toggleR2Selection = (objectKey, checked) => {
    setSelectedR2Objects((prev) => {
      if (checked) return prev.includes(objectKey) ? prev : [...prev, objectKey];
      return prev.filter((key) => key !== objectKey);
    });
  };

  const filteredR2Buckets = useMemo(() => {
    const keyword = r2BucketSearch.trim().toLowerCase();
    if (!keyword) return r2Buckets;
    return r2Buckets.filter((bucket) => String(bucket.name || '').toLowerCase().includes(keyword));
  }, [r2BucketSearch, r2Buckets]);

  const r2Rows = useMemo(() => [
    ...r2Prefixes.map((prefix) => ({
      key: prefix,
      name: prefix.slice(r2CurrentPrefix.length).replace(/\/$/, '') || prefix.replace(/\/$/, ''),
      isFolder: true,
    })),
    ...r2Objects
      .filter((object) => !String(object.key || object.name || '').endsWith('/.keep'))
      .map((object) => ({
        ...object,
        key: object.key || object.name,
        name: (object.key || object.name || '').slice(r2CurrentPrefix.length) || object.key || object.name,
        isFolder: false,
      })),
  ], [r2CurrentPrefix, r2Objects, r2Prefixes]);

  const r2TreeRows = useMemo(() => {
    const bucketName = r2SelectedBucket?.name;
    const rows = [];
    const walk = (prefix, depth) => {
      const entry = bucketName ? r2DirCache[r2CacheKey(bucketName, prefix)] : null;
      if (!entry) {
        if (bucketName && r2DirErrors[r2CacheKey(bucketName, prefix)]) {
          rows.push({ key: `r2-error:${prefix}`, prefix, name: '', isFolder: false, isError: true, depth });
          return;
        }
        rows.push({ key: `r2-loading:${prefix}`, prefix, name: '', isFolder: false, isLoading: true, depth });
        return;
      }
      entry.prefixes.forEach((subPrefix) => {
        rows.push({ key: subPrefix, name: subPrefix.slice(prefix.length).replace(/\/$/, ''), isFolder: true, depth });
        if (r2ExpandedPrefixes.includes(subPrefix)) walk(subPrefix, depth + 1);
      });
      entry.objects.forEach((object) => {
        const objectKey = object.key || object.name;
        if (String(objectKey || '').endsWith('/.keep')) return;
        rows.push({ ...object, key: objectKey, name: (objectKey || '').slice(prefix.length) || objectKey, isFolder: false, depth });
      });
    };
    r2Rows.forEach((row) => {
      rows.push({ ...row, depth: 0 });
      if (row.isFolder && r2ExpandedPrefixes.includes(row.key)) walk(row.key, 1);
    });
    return rows;
  }, [r2CacheKey, r2DirCache, r2DirErrors, r2ExpandedPrefixes, r2Rows, r2SelectedBucket]);

  const filteredR2Rows = useMemo(() => {
    const keyword = r2ObjectSearch.trim().toLowerCase();
    if (!keyword) return r2TreeRows;
    return r2TreeRows.filter((row) => String(row.name || row.key || '').toLowerCase().includes(keyword));
  }, [r2ObjectSearch, r2TreeRows]);

  const r2VisibleKeys = useMemo(
    () => filteredR2Rows.filter((row) => !row.isLoading && !row.isError).map((row) => row.key),
    [filteredR2Rows]
  );

  const r2ObjectTotalBytes = useMemo(
    () => r2Objects.reduce((total, object) => total + Number(object.size || 0), 0),
    [r2Objects]
  );

  const r2PathSegments = useMemo(
    () => r2CurrentPrefix.split('/').filter(Boolean),
    [r2CurrentPrefix]
  );
  const isViewportWorkspaceTab = ['dns', 'r2'].includes(activeTab);
  const pageShellClassName = 'dns-workspace min-h-full max-w-full cq-md:h-full cq-md:min-h-0 cq-md:flex-1';
  const contentAreaClassName = 'flex min-w-0 flex-col';
  const renderResizeHead = (label, index, startResize, align = 'left') => {
    const alignClassName = {
      left: 'justify-start text-left',
      center: 'justify-center text-center',
      right: 'justify-end text-right',
    }[align] || 'justify-start text-left';

    return (
      <Table.Head className="!p-0">
        <div className={`flex h-8 w-full items-center px-2.5 ${alignClassName}`}>
          {label}
        </div>
        <Table.ResizeHandle onMouseDown={(e) => startResize(index, e)} onTouchStart={(e) => startResize(index, e)} />
      </Table.Head>
    );
  };

  return (
    <PageStack viewport className="dns-workspace min-h-full max-w-full">
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={CLOUDFLARE_TABS}
        />

        <TabBarOverflowActions
          items={[
            ...(!['accounts', 'templates'].includes(activeTab)
              ? [
                  {
                    key: 'account',
                    type: 'select',
                    label: '账号',
                    icon: <Cloud className="h-3.5 w-3.5" />,
                    value: selectedAccountId || '',
                    onValueChange: (value) => setSelectedAccountId(value ? String(value) : ''),
                    disabled: false,
                    options: [
                      ...accounts.map((account) => ({
                        value: String(account.id),
                        label: account.name,
                      })),
                    ],
                  },
                ]
              : []),
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-4 w-4" />,
              onClick: refreshCurrentTab,
              loading: Object.values(loading).some(Boolean),
            },
          ]}
        />
      </div>

      <div className={contentAreaClassName}>
        {!selectedAccountId && !['accounts', 'templates'].includes(activeTab) ? (
          <SectionCard
            title="Cloudflare 账号"
            icon={<Cloud className="h-4 w-4 text-brand" />}
            bodyPadding="xl"
          >
            <div className="flex flex-col items-center gap-3 text-center text-sm text-kumo-subtle">
              <Cloud className="h-10 w-10 text-kumo-subtle" />
              <div>尚未配置账号。</div>
              <Button size="sm" onClick={() => setActiveTab('accounts')}>
                添加账号
              </Button>
            </div>
          </SectionCard>
        ) : (
          <>
          {activeTab === 'dns' && (
            <div className="dns-split grid min-w-0 gap-3">
              <section className="flex min-w-0 flex-col gap-2 cq-lg:sticky cq-lg:top-[70px] cq-lg:max-h-[calc(100vh-82px)] cq-lg:overflow-y-auto cq-lg:overscroll-contain cq-lg:self-start">
              <div className="flex min-h-8 shrink-0 flex-col gap-2 pl-px cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
                <div className="grid grid-cols-3 gap-2 cq-sm:flex cq-sm:flex-wrap cq-sm:items-center">
                  <Button size="sm" variant="secondary" onClick={openZoneModal} icon={<Plus className="h-4 w-4" />} className="w-full justify-center cq-sm:w-auto">
                    添加域名
                  </Button>
                  {selectedZone && (
                    <>
                      <Button size="sm" variant={isArmed(`zone-cache:${selectedZoneId}`) ? 'destructive' : 'secondary-destructive'} onClick={purgeZoneCache} disabled={loading.purge} icon={<Shield className="h-4 w-4" />} className="w-full justify-center cq-sm:w-auto">
                        清除缓存
                      </Button>
                      <Button size="sm" variant={isArmed(`zone:${selectedZone.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteZone(selectedZone)} icon={<Trash className="h-4 w-4" />} className="w-full justify-center cq-sm:w-auto">
                        删除域名
                      </Button>
                    </>
                  )}
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2 @[430px]:grid-cols-3 cq-md:hidden">
                {loading.zones ? (
                  Array.from({ length: 4 }).map((_, index) => (
                    <LayerCard key={index} className="min-w-0 p-2">
                      <SkeletonLine className="h-4 w-24" />
                      <SkeletonLine className="mt-2 h-5 w-20" />
                    </LayerCard>
                  ))
                ) : zones.length === 0 ? (
                  <LayerCard className="min-w-full p-8 text-center text-xs text-kumo-subtle">暂无域名。</LayerCard>
                ) : zones.map((zone) => (
                  <LayerCard
                    key={zone.id}
                    className={`min-w-0 cursor-pointer p-2 transition-colors ${zone.id === selectedZoneId ? 'border-brand/60 bg-brand/5 ring-1 ring-brand/35' : ''}`}
                    onClick={() => selectZone(zone)}
                  >
                    <div className="min-w-0">
                      <div className="truncate text-sm font-bold text-kumo-strong" title={zone.name}>{zone.name}</div>
                      <div className="mt-2 flex items-center gap-1.5">
                        <Badge variant={statusVariant(zone.status)} className="text-[10px] leading-4">{zoneStatusLabel(zone.status)}</Badge>
                        <Badge variant="secondary" className="text-[10px] leading-4">{zoneTypeLabel(zone.type)}</Badge>
                      </div>
                    </div>
                  </LayerCard>
                ))}
              </div>

              <div className="dns-table-frame hidden max-w-full cq-md:flex">
                <div className="dns-table-scroll scrollbar-thin">
                <Table layout="fixed" className="w-full text-xs">
                  <colgroup>
                    {zoneColWidths.map((width, index) => <col key={index} style={{ width }} />)}
                  </colgroup>
                  <Table.Header sticky variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5 text-left">域名</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5 text-center">状态</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5 text-center">类型</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5 text-center">NS</Table.Head>
                      <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {loading.zones ? (
                      Array.from({ length: 4 }).map((_, index) => (
                        <Table.Row key={index} className="h-9">
                          <Table.Cell className="!px-2.5 !py-1.5 text-left"><SkeletonLine className="h-3.5 w-32" /></Table.Cell>
                          <Table.Cell className="!px-2.5 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-14" /></Table.Cell>
                          <Table.Cell className="!px-2.5 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-10" /></Table.Cell>
                          <Table.Cell className="!px-2.5 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-8" /></Table.Cell>
                          <Table.Cell className="!px-2 !py-1.5 text-center"><SkeletonLine className="mx-auto h-3.5 w-12" /></Table.Cell>
                        </Table.Row>
                      ))
                    ) : zones.length === 0 ? (
                      <Table.Row>
                        <Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">
                          暂无域名。
                        </Table.Cell>
                      </Table.Row>
                    ) : zones.map((zone) => (
                      <Table.Row
                        key={zone.id}
                        variant={zone.id === selectedZoneId ? 'selected' : 'default'}
                        className="h-9 cursor-pointer"
                        title="双击进入管理"
                        onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => selectZone(zone))}
                      >
                        <Table.Cell className="!px-2.5 !py-1.5 text-left">
                          <div className="flex min-w-0">
                            <span className="truncate font-semibold text-kumo-strong" title={zone.name}>{zone.name}</span>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5 text-center">
                          <Badge variant={statusVariant(zone.status)} className="text-[10px] leading-4">{zoneStatusLabel(zone.status)}</Badge>
                        </Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5 text-center">{zoneTypeLabel(zone.type)}</Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5 text-center">
                          <div className="flex w-full justify-center">
                            <Popover>
                              <Popover.Trigger
                                render={(
                                  <Button
                                    size="sm"
                                    shape="square"
                                    variant="secondary"
                                    aria-label={`查看 ${zone.name} 名称服务器`}
                                    title="名称服务器"
                                    onClick={(event) => event.stopPropagation()}
                                    icon={<Eye className="h-3.5 w-3.5" />}
                                  />
                                )}
                              />
                              <Popover.Content side="right" align="center" className="w-80 p-3" onClick={(event) => event.stopPropagation()}>
                                <div className="mb-2 flex items-center justify-between gap-2">
                                  <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                                    名称服务器
                                  </Popover.Title>
                                  <Button
                                    size="sm"
                                    variant="secondary"
                                    disabled={zoneNameServers(zone).length === 0}
                                    onClick={() => copyText(zoneNameServers(zone).join('\n'), '名称服务器')}
                                    icon={<Copy className="h-3.5 w-3.5" />}
                                  >
                                    全部
                                  </Button>
                                </div>
                                <div className="mb-2 truncate text-xs text-kumo-subtle" title={zone.name}>{zone.name}</div>
                                <div className="grid gap-2">
                                  {zoneNameServers(zone).length > 0 ? zoneNameServers(zone).map((nameServer, index) => (
                                    <div key={`${nameServer}-${index}`} className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                                      <code className="min-w-0 flex-1 truncate font-mono text-xs text-kumo-strong" title={nameServer}>
                                        {nameServer}
                                      </code>
                                      <Button
                                        size="sm"
                                        shape="square"
                                        variant="secondary"
                                        aria-label={`复制 ${nameServer}`}
                                        title="复制"
                                        onClick={() => copyText(nameServer, '名称服务器')}
                                        icon={<Copy className="h-3.5 w-3.5" />}
                                      />
                                    </div>
                                  )) : (
                                    <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-3 text-center text-xs text-kumo-subtle">
                                      暂无名称服务器
                                    </div>
                                  )}
                                </div>
                              </Popover.Content>
                            </Popover>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="inline-flex gap-1">
                            <Button size="sm" shape="square" variant="secondary" onClick={(event) => {
                              event.stopPropagation();
                              selectZone(zone);
                            }} aria-label={zone.id === selectedZoneId ? `已选择 ${zone.name}` : `管理 ${zone.name}`} title={zone.id === selectedZoneId ? '已选择' : '管理'}>
                              {zone.id === selectedZoneId ? <Check className="h-3.5 w-3.5" /> : <Search className="h-3.5 w-3.5" />}
                            </Button>
                            <Button size="sm" shape="square" variant={isArmed(`zone:${zone.id}`) ? 'destructive' : 'secondary-destructive'} onClick={(event) => {
                              event.stopPropagation();
                              deleteZone(zone);
                            }} aria-label={`删除 ${zone.name}`} title="删除">
                              <Trash className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
                </div>
              </div>

              </section>

              <section className="flex min-w-0 flex-col gap-2">
              <div className="flex min-h-8 shrink-0 items-center justify-between gap-2 px-1">
                <div className="flex min-w-0 items-center gap-2 text-xs text-kumo-subtle">
                  <Globe className="h-3.5 w-3.5 shrink-0" />
                  <span className="truncate font-medium text-kumo-strong">
                    {selectedZone ? selectedZone.name : 'DNS 记录'}
                  </span>
                </div>
                {selectedZone && (
                  <div className="flex shrink-0 items-center gap-2">
                    <Select size="sm"
                      value={analyticsRange}
                      onValueChange={(value) => loadAnalytics(String(value))}
                      className="w-20 shrink-0"
                      items={[
                        { value: '24h', label: '24 小时' },
                        { value: '7d', label: '7 天' },
                        { value: '30d', label: '30 天' },
                      ]}
                    />
                    {(analyticsPoints.length > 0 || loading.analytics) && (
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => setShowAnalyticsCharts((value) => !value)}
                        icon={showAnalyticsCharts ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                        className="shrink-0 px-2"
                      >
                        {showAnalyticsCharts ? '收起趋势' : '展开趋势'}
                      </Button>
                    )}
                    <div className="shrink-0 text-xs text-kumo-subtle">
                      {records.length} 条记录
                    </div>
                  </div>
                )}
              </div>
              {selectedZone ? (
                <>
                  <div className="dns-summary-grid order-3 grid shrink-0 gap-2 cq-md:order-none">
                    <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
                      <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">SSL 模式</div>
                      <Select size="sm"
                        value={sslInfo?.mode || null}
                        onValueChange={(value) => updateSslMode(String(value))}
                        placeholder="选择模式"
                        loading={loading.ssl}
                        renderValue={(value) => sslModeLabel(value)}
                        className="w-18 shrink-0"
                        items={SSL_MODES}
                      />
                    </DnsPanelCard>
                    <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
                      <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">唯一访问者</div>
                      <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">{loading.analytics ? '加载中' : formatNumber(analyticsSummary.uniques)}</div>
                    </DnsPanelCard>
                    <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
                      <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">请求量</div>
                      <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">{loading.analytics ? '加载中' : formatNumber(analyticsSummary.requests)}</div>
                    </DnsPanelCard>
                    <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
                      <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">带宽</div>
                      <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">{loading.analytics ? '加载中' : formatBytes(analyticsSummary.bandwidth)}</div>
                    </DnsPanelCard>
                    <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
                      <div className="flex min-w-0 items-center gap-2">
                          <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">缓存命中率</div>
                          <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">
                            {loading.analytics ? '加载中' : formatPercent(analyticsSummary.cacheHitRate)}
                          </div>
                        </div>
                    </DnsPanelCard>
                  </div>

                  <AnimatedCollapse
                    open={showAnalyticsPanel}
                    className={showAnalyticsPanel ? 'order-5 shrink-0 cq-md:order-none' : 'contents'}
                  >
                    <div className="dns-chart-grid grid gap-2 pt-0.5">
                    {analyticsChartCards.map((card) => (
                      <DnsPanelCard key={card.key} className="min-w-0 overflow-hidden p-3">
                        <div className="flex items-center justify-between gap-2">
                          <div className="truncate text-xs font-medium text-kumo-strong">{card.label}</div>
                          <div className="shrink-0 text-xs font-semibold text-kumo-subtle">{loading.analytics ? '加载中' : card.value}</div>
                        </div>
                        <div className="mt-2 min-w-0 overflow-hidden" style={{ height: 108 }}>
                          <SiteFontTimeseriesChart
                            echarts={echarts}
                            data={card.data}
                            height={108}
                            isDarkMode={isDarkMode}
                            gradient
                            loading={loading.analytics && analyticsPoints.length === 0}
                            xAxisTickCount={3}
                            yAxisTickCount={2}
                            xAxisTickFormat={(timestamp) => formatAnalyticsAxisTime(timestamp, analyticsRange)}
                            yAxisTickFormat={card.yAxisTickFormat}
                            tooltipValueFormat={card.tooltipValueFormat}
                            tooltipFollowCursor="x"
                            ariaDescription={`Cloudflare ${card.label}`}
                          />
                        </div>
                      </DnsPanelCard>
                    ))}
                    </div>
                  </AnimatedCollapse>

                  <div className="dns-toolbar-frame order-1 flex shrink-0 flex-wrap items-center justify-between gap-2 p-2 cq-md:order-none">
                    <div className="grid w-full grid-cols-[minmax(0,1fr)_minmax(7rem,0.7fr)_auto] gap-2 cq-sm:flex cq-sm:w-auto cq-sm:flex-wrap cq-sm:items-center">
                      <Input size="sm"
                        aria-label="按名称筛选 DNS 记录"
                        value={recordFilter.name}
                        onChange={(event) => setRecordFilter((prev) => ({ ...prev, name: event.target.value }))}
                        placeholder="筛选名称"
                        className="w-full cq-sm:w-48"
                      />
                      <Select size="sm"
                        aria-label="按类型筛选 DNS 记录"
                        value={recordFilter.type || null}
                        onValueChange={(value) => setRecordFilter((prev) => ({ ...prev, type: value ? String(value) : '' }))}
                        placeholder="全部类型"
                        className="w-full cq-sm:w-36"
                        items={recordTypes.map((type) => ({ value: type, label: type }))}
                      />
                      <Button size="sm" variant="secondary" onClick={() => loadRecords(selectedZoneId, recordFilter)}>
                        查询
                      </Button>
                    </div>
                    <div className="flex w-full flex-wrap items-center gap-2 cq-sm:w-auto">
                      <Button size="sm" onClick={() => openRecordModal()} icon={<Plus className="h-4 w-4" />}>
                        添加记录
                      </Button>
                      <Toolbar size="sm" aria-label="导出导入 DNS 记录" className="shrink-0">
                        <Toolbar.Button onClick={exportRecords} aria-label="导出 DNS 记录" icon={<Upload className="h-3.5 w-3.5" />}>
                          <span className="hidden cq-sm:inline">导出</span>
                        </Toolbar.Button>
                        <Toolbar.Button onClick={() => openImportModal('records')} aria-label="导入 DNS 记录" icon={<Download className="h-3.5 w-3.5" />}>
                          <span className="hidden cq-sm:inline">导入</span>
                        </Toolbar.Button>
                      </Toolbar>
                      {selectedRecordIds.length > 0 && (
                        <Button size="sm" variant={isArmed('batch-records') ? 'destructive' : 'secondary-destructive'} onClick={batchDeleteRecords} icon={<Trash className="h-4 w-4" />}>
                          删除 {selectedRecordIds.length}
                        </Button>
                      )}
                    </div>
                  </div>

                  <div className="order-2 grid gap-2 pb-3 cq-md:hidden">
                    {loading.records ? (
                      Array.from({ length: 5 }).map((_, index) => (
                        <LayerCard key={index} className="p-3">
                          <SkeletonLine className="h-4 w-32" />
                          <SkeletonLine className="mt-2 h-3.5 w-full" />
                          <SkeletonLine className="mt-2 h-3.5 w-24" />
                        </LayerCard>
                      ))
                    ) : records.length === 0 ? (
                      <LayerCard className="p-8 text-center text-xs text-kumo-subtle">暂无匹配记录。</LayerCard>
                    ) : records.map((record) => (
                      <LayerCard
                        key={record.id}
                        className={`p-3 ${selectedRecordIds.includes(record.id) ? 'ring-1 ring-brand/35' : ''}`}
                        onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openRecordModal(record))}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0">
                            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                              <Badge variant={recordTypeBadgeVariant(record.type)} className="min-w-10 justify-center text-[10px] leading-4">{record.type}</Badge>
                              <span className="min-w-0 truncate text-sm font-bold text-kumo-strong" title={recordShortName(record.name, selectedZone.name)}>
                                {recordShortName(record.name, selectedZone.name)}
                              </span>
                              <Badge variant={record.proxied ? 'success' : 'outline'} className="text-[10px] leading-4">{record.proxied ? '代理' : '仅 DNS'}</Badge>
                            </div>
                            <div className="mt-2 break-all font-mono text-[11px] text-kumo-default" title={record.content}>{record.content}</div>
                            <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-kumo-subtle">
                              <span>TTL {record.ttl === 1 ? '自动' : record.ttl}</span>
                              <span>{formatDate(record.modifiedOn)}</span>
                            </div>
                          </div>
                          <div className="flex shrink-0 items-center gap-1">
                            <Button size="sm" shape="square" variant="secondary" onClick={() => openRecordModal(record)} aria-label={`编辑 ${record.name}`} title="编辑" icon={<Edit className="h-3.5 w-3.5" />} />
                            <Button size="sm" shape="square" variant={isArmed(`record:${record.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteRecord(record)} aria-label={`删除 ${record.name}`} title="删除" icon={<Trash className="h-3.5 w-3.5" />} />
                          </div>
                        </div>
                      </LayerCard>
                    ))}
                  </div>

                  <div className="dns-table-frame order-2 hidden max-w-full cq-md:flex cq-md:order-none">
                    <div className="dns-table-scroll scrollbar-thin">
                    <Table layout="fixed" className="w-full text-xs" style={{ minWidth: recordColWidths.reduce((sum, width) => sum + width, 0) }}>
                      <colgroup>
                        {recordColWidths.map((width, index) => <col key={index} style={{ width }} />)}
                      </colgroup>
                      <Table.Header sticky variant="compact">
                        <Table.Row className="h-8">
                          <Table.CheckHead
                            checked={records.length > 0 && selectedRecordIds.length === records.length}
                            indeterminate={selectedRecordIds.length > 0 && selectedRecordIds.length < records.length}
                            onCheckedChange={(checked) => setSelectedRecordIds(checked ? records.map((record) => record.id) : [])}
                            aria-label="全选 DNS 记录"
                            className="!px-2 !py-1.5 text-center"
                          />
                          {renderResizeHead('类型', 1, startRecordResize, 'center')}
                          {renderResizeHead('名称', 2, startRecordResize)}
                          {renderResizeHead('内容', 3, startRecordResize)}
                          {renderResizeHead('TTL', 4, startRecordResize, 'center')}
                          {renderResizeHead('代理', 5, startRecordResize, 'center')}
                          {renderResizeHead('更新时间', 6, startRecordResize, 'center')}
                          <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {loading.records ? (
                          Array.from({ length: 5 }).map((_, index) => (
                            <Table.Row key={index} className="h-9">
                              <Table.Cell colSpan={8} className="!px-2.5 !py-1.5"><SkeletonLine className="mx-auto h-3.5 w-full max-w-2xl" /></Table.Cell>
                            </Table.Row>
                          ))
                        ) : records.length === 0 ? (
                          <Table.Row>
                            <Table.Cell colSpan={8} className="py-10 text-center text-kumo-subtle">
                              暂无匹配记录。
                            </Table.Cell>
                          </Table.Row>
                        ) : records.map((record) => (
                          <Table.Row
                            key={record.id}
                            variant={selectedRecordIds.includes(record.id) ? 'selected' : 'default'}
                            className="h-9 cursor-pointer"
                            title="双击编辑记录"
                            onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openRecordModal(record))}
                          >
                            <Table.CheckCell
                              checked={selectedRecordIds.includes(record.id)}
                              onCheckedChange={(checked) => toggleRecordSelection(record.id, Boolean(checked))}
                              aria-label={`选择 ${record.name}`}
                              className="!px-2 !py-1.5 text-center"
                            />
                            <Table.Cell className="!px-2.5 !py-1.5 text-center">
                              <Badge variant={recordTypeBadgeVariant(record.type)} className="min-w-12 justify-center text-[10px] leading-4">
                                {record.type}
                              </Badge>
                            </Table.Cell>
                            <Table.Cell className="!px-2.5 !py-1.5 text-left font-semibold text-kumo-strong">
                              <div className="truncate" title={recordShortName(record.name, selectedZone.name)}>{recordShortName(record.name, selectedZone.name)}</div>
                            </Table.Cell>
                            <Table.Cell className="!px-2.5 !py-1.5 text-left">
                              <div className="truncate font-mono text-[11px]" title={record.content}>{record.content}</div>
                            </Table.Cell>
                            <Table.Cell className="!px-2.5 !py-1.5 text-center">{record.ttl === 1 ? '自动' : record.ttl}</Table.Cell>
                            <Table.Cell className="!px-2.5 !py-1.5 text-center"><Badge variant={record.proxied ? 'success' : 'outline'} className="text-[10px] leading-4">{record.proxied ? '开启' : '关闭'}</Badge></Table.Cell>
                            <Table.Cell className="!px-2.5 !py-1.5 text-center">
                              <div className="truncate" title={formatDate(record.modifiedOn)}>{formatDate(record.modifiedOn)}</div>
                            </Table.Cell>
                            <Table.Cell className="!px-2 !py-1.5 text-center">
                              <div className="inline-flex gap-1">
                                <Button size="sm" shape="square" variant="secondary" onClick={() => openRecordModal(record)} aria-label={`编辑 ${record.name}`} title="编辑" icon={<Edit className="h-3.5 w-3.5" />} />
                                <Button size="sm" shape="square" variant={isArmed(`record:${record.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteRecord(record)} aria-label={`删除 ${record.name}`} title="删除" icon={<Trash className="h-3.5 w-3.5" />} />
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                    </div>
                  </div>
                </>
              ) : (
                <div className="flex min-h-0 flex-1 items-center justify-center rounded-lg border border-kumo-line bg-kumo-base p-8 shadow-none">
                  <div className="flex flex-col items-center gap-3 text-center text-sm text-kumo-subtle">
                    <Globe className="h-10 w-10 text-kumo-subtle" />
                    <div>选择左侧域名后管理记录。</div>
                  </div>
                </div>
              )}
              </section>
            </div>
          )}

          {activeTab === 'workers' && (
            <SectionCard
              title="Workers"
              description={workerSubdomain ? <>默认子域名：<span className="font-mono text-kumo-strong">{workerSubdomain}.workers.dev</span></> : '默认子域名未返回'}
              icon={<Terminal className="h-4 w-4 text-brand" />}
              action={(
                <Button size="sm" onClick={() => openWorkerModal()} icon={<Plus className="h-4 w-4" />}>
                  新建 Worker
                </Button>
              )}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
              <Table layout="fixed">
                <colgroup>{workerColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startWorkerResize(0, e)} onTouchStart={(e) => startWorkerResize(0, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">创建时间<Table.ResizeHandle onMouseDown={(e) => startWorkerResize(1, e)} onTouchStart={(e) => startWorkerResize(1, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">更新时间<Table.ResizeHandle onMouseDown={(e) => startWorkerResize(2, e)} onTouchStart={(e) => startWorkerResize(2, e)} /></Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {loading.workers ? (
                    Array.from({ length: 4 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={4}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
                  ) : workers.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={4} className="py-10 text-center text-kumo-subtle">没有 Workers。</Table.Cell></Table.Row>
                  ) : workers.map((worker) => (
                    <Table.Row
                      key={worker.id || worker.name}
                      className="cursor-pointer"
                      title="双击编辑 Worker 代码"
                      onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openWorkerModal(worker))}
                    >
                      <Table.Cell className="font-medium text-kumo-strong">{worker.name}</Table.Cell>
                      <Table.Cell>{formatDate(worker.createdOn)}</Table.Cell>
                      <Table.Cell>{formatDate(worker.modifiedOn)}</Table.Cell>
                      <Table.Cell className="text-right">
                        <div className="inline-flex flex-wrap justify-end gap-2">
                          <Button size="sm" variant="secondary" onClick={() => openWorkerModal(worker)}>代码</Button>
                          <Button size="sm" variant="secondary" onClick={() => openWorkerRoutesModal(worker)}>路由</Button>
                          <Button size="sm" variant="secondary" onClick={() => openWorkerDomainsModal(worker)}>域名</Button>
                          <Button size="sm" variant="secondary" onClick={() => openWorkerAnalyticsModal(worker)}>统计</Button>
                          <Button size="sm" variant="secondary" onClick={() => toggleWorkerSubdomain(worker, true)}>启用</Button>
                          <Button size="sm" variant={isArmed(`worker:${worker.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorker(worker)} aria-label={`删除 ${worker.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </SectionCard>
          )}

          {activeTab === 'pages' && (
            <SectionCard
              title="Pages 项目"
              icon={<Layers className="h-4 w-4 text-brand" />}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
              <Table layout="fixed">
                <colgroup>{pageColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="relative pr-6">项目<Table.ResizeHandle onMouseDown={(e) => startPageResize(0, e)} onTouchStart={(e) => startPageResize(0, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">访问地址<Table.ResizeHandle onMouseDown={(e) => startPageResize(1, e)} onTouchStart={(e) => startPageResize(1, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">生产分支<Table.ResizeHandle onMouseDown={(e) => startPageResize(2, e)} onTouchStart={(e) => startPageResize(2, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">最新部署<Table.ResizeHandle onMouseDown={(e) => startPageResize(3, e)} onTouchStart={(e) => startPageResize(3, e)} /></Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {loading.pages ? (
                    Array.from({ length: 4 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
                  ) : pages.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">没有 Pages 项目。</Table.Cell></Table.Row>
                  ) : pages.map((project) => (
                    <Table.Row key={project.name}>
                      <Table.Cell className="font-medium text-kumo-strong">{project.name}</Table.Cell>
                      <Table.Cell>
                        {project.subdomain ? (
                          <LinkButton size="sm" variant="secondary" href={`https://${project.subdomain}`} external icon={<ExternalLink className="h-4 w-4" />}>
                            打开
                          </LinkButton>
                        ) : '-'}
                      </Table.Cell>
                      <Table.Cell>{project.productionBranch || '-'}</Table.Cell>
                      <Table.Cell>
                        <Badge variant={statusVariant(project.latestDeployment?.status)}>
                          {project.latestDeployment?.status || '未知'}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell className="text-right">
                        <div className="inline-flex flex-wrap justify-end gap-2">
                          <Button size="sm" variant="secondary" onClick={() => openPagesDeploymentsModal(project)}>部署</Button>
                          <Button size="sm" variant="secondary" onClick={() => openPagesDomainsModal(project)}>域名</Button>
                          <Button size="sm" variant={isArmed(`pages-project:${project.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deletePagesProject(project)} aria-label={`删除 ${project.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </SectionCard>
          )}

          {activeTab === 'r2' && (
            <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 gap-3 cq-lg:grid-cols-[18rem_minmax(0,1fr)]">
              <AppCard padding="sm" className="flex min-h-0 flex-col gap-3 cq-lg:h-full">
                <div className="flex items-center justify-between gap-2">
                  <div className="min-w-0">
                    <div className="text-sm font-semibold text-kumo-strong">存储桶</div>
                    <div className="text-xs text-kumo-subtle">
                      {r2Buckets.length} 个 Bucket
                      {r2Metrics && (r2MetricsTotals.bytes > 0 || r2MetricsTotals.objects > 0) && (
                        <span> · 已用 {formatBytes(r2MetricsTotals.bytes)} · {r2MetricsTotals.objects.toLocaleString('en-US', { useGrouping: false })} 个对象</span>
                      )}
                    </div>
                  </div>
                  <Button
                    size="sm"
                    shape="square"
                    onClick={() => { setR2BucketForm({ name: '', location: 'auto' }); setModal({ type: 'r2Bucket', data: null }); }}
                    disabled={!selectedAccountId}
                    aria-label="创建存储桶"
                    title="创建存储桶"
                    icon={<Plus className="h-4 w-4" />}
                  />
                </div>

                <ResponsiveSearchInput
                  value={r2BucketSearch}
                  onChange={(event) => setR2BucketSearch(event.target.value)}
                  placeholder="搜索存储桶"
                  ariaLabel="搜索 R2 存储桶"
                />

                <div className="flex min-h-0 flex-col gap-2 overflow-y-auto pr-1">
                  {loading.r2 ? Array.from({ length: 5 }).map((_, index) => (
                    <div key={index} className="rounded-md border border-kumo-line p-3">
                      <SkeletonLine className="h-4 w-36" />
                    </div>
                  )) : !selectedAccountId ? (
                    <div className="rounded-md border border-dashed border-kumo-line p-4 text-sm text-kumo-subtle">先选择 Cloudflare 账号。</div>
                  ) : r2Buckets.length === 0 ? (
                    <div className="rounded-md border border-dashed border-kumo-line p-4 text-sm text-kumo-subtle">还没有 R2 存储桶。</div>
                  ) : filteredR2Buckets.length === 0 ? (
                    <div className="rounded-md border border-dashed border-kumo-line p-4 text-sm text-kumo-subtle">没有匹配的存储桶。</div>
                  ) : filteredR2Buckets.map((bucket) => {
                    const isSelected = r2SelectedBucket?.name === bucket.name;
                    return (
                      <div
                        key={bucket.name}
                        className={`group rounded-md border p-2.5 transition ${isSelected ? 'border-brand/70 bg-brand/10 shadow-sm' : 'border-kumo-line bg-kumo-base hover:border-brand/50 hover:bg-kumo-recessed/40'}`}
                      >
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          className="h-auto w-full min-w-0 items-start justify-start gap-2 px-0 py-0 text-left bg-transparent! hover:bg-transparent! active:bg-transparent! data-[active=true]:bg-transparent! data-[selected=true]:bg-transparent! focus-visible:bg-transparent!"
                          onClick={() => selectR2Bucket(bucket)}
                        >
                          <Box className={`mt-0.5 h-4 w-4 shrink-0 ${isSelected ? 'text-brand' : 'text-kumo-subtle'}`} />
                          <span className="min-w-0 flex-1">
                            <span className={`block truncate text-sm font-medium ${isSelected ? 'text-brand' : 'text-kumo-strong'}`}>{bucket.name}</span>
                            <span className="mt-1 block truncate text-xs text-kumo-subtle" title={`创建于 ${formatDate(bucket.creation_date || bucket.created_at)}`}>
                              {formatDate(bucket.creation_date || bucket.created_at)}
                            </span>
                          </span>
                        </Button>
                        <div className="mt-2 flex items-center justify-between gap-2">
                          <Badge variant={bucket.public_url_base ? 'success' : 'outline'} className="text-[10px] leading-4">
                            {bucket.public_url_base ? '公开访问' : '私有'}
                          </Badge>
                          <Button size="sm" shape="square" variant={isArmed(`r2-bucket:${bucket.name}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteR2Bucket(bucket)} aria-label={`删除 ${bucket.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                        </div>
                      </div>
                    );
                  })}
                </div>
              </AppCard>

              <AppCard padding="none" className="flex min-h-0 min-w-0 flex-col overflow-hidden cq-lg:h-full">
                {!r2SelectedBucket ? (
                  <div className="flex min-h-72 flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
                    <Database className="h-8 w-8 text-kumo-subtle" />
                    <div>
                      <div className="font-medium text-kumo-strong">选择一个存储桶</div>
                      <div className="mt-1 text-sm text-kumo-subtle">左侧可搜索并进入 Bucket。</div>
                    </div>
                    <Button size="sm" disabled={!selectedAccountId} onClick={() => { setR2BucketForm({ name: '', location: 'auto' }); setModal({ type: 'r2Bucket', data: null }); }} icon={<Plus className="h-4 w-4" />}>
                      创建存储桶
                    </Button>
                  </div>
                ) : (
                  <div className="flex min-h-0 min-w-0 flex-1 flex-col">
                    <input
                      ref={r2UploadInputRef}
                      type="file"
                      multiple
                      className="hidden"
                      onChange={handleR2UploadFiles}
                    />
                    <div className="flex shrink-0 flex-col gap-3 border-b border-kumo-line p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="min-w-0 flex-1">
                          <div className="flex min-w-0 flex-wrap items-center gap-1 rounded-md border border-kumo-line bg-kumo-recessed/25 px-2 py-1.5 text-sm">
                            <Database className="h-4 w-4 shrink-0 text-brand" />
                            <Button type="button" size="xs" variant="ghost" className="h-auto max-w-48 truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => loadR2Objects(r2SelectedBucket.name, '')}>
                              {r2SelectedBucket.name}
                            </Button>
                            <span className="text-kumo-subtle">/</span>
                            <Button type="button" size="xs" variant="ghost" className="h-auto px-1.5 py-0.5 text-kumo-subtle hover:text-kumo-strong" onClick={() => loadR2Objects(r2SelectedBucket.name, '')}>
                              根目录
                            </Button>
                            {r2PathSegments.map((segment, index) => {
                              const prefix = `${r2PathSegments.slice(0, index + 1).join('/')}/`;
                              return (
                                <React.Fragment key={prefix}>
                                  <span className="text-kumo-subtle">/</span>
                                  <Button type="button" size="xs" variant="ghost" className="h-auto max-w-40 truncate px-1.5 py-0.5 text-kumo-subtle hover:text-kumo-strong" onClick={() => loadR2Objects(r2SelectedBucket.name, prefix)}>
                                    {segment}
                                  </Button>
                                </React.Fragment>
                              );
                            })}
                          </div>
                        </div>

                        <div className="flex flex-wrap items-center gap-2">
                          <Button
                            size="sm"
                            variant="primary"
                            onClick={() => r2UploadInputRef.current?.click()}
                            disabled={loading.uploadR2}
                            icon={<Upload className="h-4 w-4" />}
                          >
                            上传
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => {
                              setR2FolderForm({ name: '' });
                              setModal({ type: 'r2Folder', data: null });
                            }}
                            icon={<Folder className="h-4 w-4" />}
                          >
                            新建文件夹
                          </Button>
                          {r2CurrentPrefix && (
                            <Button
                              size="sm"
                              variant="secondary"
                              onClick={() => {
                                const parent = r2CurrentPrefix.split('/').filter(Boolean).slice(0, -1).join('/');
                                loadR2Objects(r2SelectedBucket.name, parent ? `${parent}/` : '');
                              }}
                              icon={<ArrowLeft className="h-4 w-4" />}
                            >
                              上一级
                            </Button>
                          )}
                          <Button size="sm" shape="square" variant="secondary" onClick={() => { clearR2BucketCache(r2SelectedBucket.name); loadR2Objects(r2SelectedBucket.name, r2CurrentPrefix, { force: true }); }} aria-label="刷新对象" title="刷新" icon={<RefreshCw className="h-4 w-4" />} />
                          {selectedR2Objects.length > 0 && (
                            <Button size="sm" variant={isArmed('batch-r2-objects') ? 'destructive' : 'secondary-destructive'} onClick={batchDeleteR2Objects} disabled={loading.batchDeleteR2} icon={<Trash className="h-4 w-4" />}>
                              删除 {selectedR2Objects.length}
                            </Button>
                          )}
                        </div>
                      </div>

                      <div className="flex flex-col gap-2 cq-lg:flex-row cq-lg:items-center cq-lg:justify-between">
                        <div className="flex flex-wrap items-center gap-2 text-xs text-kumo-subtle">
                          <Badge variant="outline">{r2Prefixes.length} 个文件夹</Badge>
                          <Badge variant="outline">{r2Objects.filter((object) => !String(object.key || object.name || '').endsWith('/.keep')).length} 个对象</Badge>
                          <Badge variant="outline">{formatBytes(r2ObjectTotalBytes)}</Badge>
                          {selectedR2Objects.length > 0 && <Badge variant="info">已选择 {selectedR2Objects.length}</Badge>}
                        </div>
                        <div className="w-full max-w-md">
                          <ResponsiveSearchInput
                            value={r2ObjectSearch}
                            onChange={(event) => setR2ObjectSearch(event.target.value)}
                            placeholder="搜索当前目录"
                            ariaLabel="搜索当前 R2 目录"
                          />
                        </div>
                      </div>
                    </div>

                    <div className="min-h-0 flex-1 overflow-auto">
                        <Table layout="fixed">
                          <colgroup>{r2ColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
                          <Table.Header variant="compact">
                            <Table.Row>
                              <Table.CheckHead
                                checked={r2VisibleKeys.length > 0 && r2VisibleKeys.every((key) => selectedR2Objects.includes(key))}
                                indeterminate={selectedR2Objects.length > 0 && !r2VisibleKeys.every((key) => selectedR2Objects.includes(key))}
                                onCheckedChange={(checked) => setSelectedR2Objects(checked ? r2VisibleKeys : [])}
                                aria-label="全选当前可见 R2 对象"
                              />
                              <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startR2Resize(1, e)} onTouchStart={(e) => startR2Resize(1, e)} /></Table.Head>
                              <Table.Head className="relative pr-6">大小<Table.ResizeHandle onMouseDown={(e) => startR2Resize(2, e)} onTouchStart={(e) => startR2Resize(2, e)} /></Table.Head>
                              <Table.Head className="relative pr-6">修改时间<Table.ResizeHandle onMouseDown={(e) => startR2Resize(3, e)} onTouchStart={(e) => startR2Resize(3, e)} /></Table.Head>
                              <Table.Head className="app-table-action">操作</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {loading.r2Objects ? (
                              Array.from({ length: 7 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
                            ) : r2Rows.length === 0 ? (
                              <Table.Row><Table.Cell colSpan={5} className="py-12 text-center text-kumo-subtle">当前目录为空。</Table.Cell></Table.Row>
                            ) : filteredR2Rows.length === 0 ? (
                              <Table.Row><Table.Cell colSpan={5} className="py-12 text-center text-kumo-subtle">没有匹配的对象。</Table.Cell></Table.Row>
                            ) : filteredR2Rows.map((row) => (
                              <Table.Row
                                key={row.key}
                                className={`cursor-pointer hover:bg-kumo-recessed/35 ${row.isLoading ? 'opacity-70' : ''}`}
                                onClick={() => { if (row.isLoading || row.isError) return; if (row.isFolder) toggleR2FolderExpanded(row.key); else previewR2Object(row.key); }}
                                title={row.isError ? '目录加载失败' : (row.isFolder ? '展开目录' : '预览文件')}
                              >
                                {(row.isLoading || row.isError) ? (
                                  <Table.Cell />
                                ) : (
                                  <Table.CheckCell
                                    checked={selectedR2Objects.includes(row.key)}
                                    onClick={(event) => event.stopPropagation()}
                                    onCheckedChange={(checked) => toggleR2Selection(row.key, Boolean(checked))}
                                    aria-label={`选择 ${row.key}`}
                                  />
                                )}
                                <Table.Cell>
                                  <div className="flex min-w-0 items-center gap-1" style={{ marginLeft: row.depth * 18 }}>
                                    {row.isError ? (
                                      <Button
                                        type="button"
                                        size="xs"
                                        variant="ghost"
                                        className="h-auto min-w-0 justify-start gap-2 px-0 py-0 text-left bg-transparent! hover:bg-transparent! active:bg-transparent! focus-visible:bg-transparent!"
                                        onClick={(event) => { event.stopPropagation(); retryR2Dir(row.prefix); }}
                                        aria-label={`重试加载目录 ${row.prefix}`}
                                        title={`重试加载 ${row.prefix}`}
                                      >
                                        <AlertTriangle className="h-4 w-4 shrink-0 text-kumo-warning" />
                                        <span className="truncate text-kumo-danger">加载失败，重试</span>
                                      </Button>
                                    ) : row.isLoading ? (
                                      <span className="inline-flex items-center gap-2 px-1 text-xs text-kumo-subtle"><SkeletonLine className="h-3.5 w-3.5" />加载中…</span>
                                    ) : (
                                      <>
                                    {row.isFolder && (
                                      <Button
                                        type="button"
                                        size="xs"
                                        shape="square"
                                        variant="ghost"
                                        className="h-6 w-6 shrink-0 bg-transparent! text-kumo-subtle hover:bg-transparent! active:bg-transparent! hover:text-brand! focus-visible:bg-transparent!"
                                        onClick={(event) => { event.stopPropagation(); toggleR2FolderExpanded(row.key); }}
                                        aria-label={r2ExpandedPrefixes.includes(row.key) ? `折叠目录 ${row.name}` : `展开目录 ${row.name}`}
                                        title={r2ExpandedPrefixes.includes(row.key) ? '折叠目录' : '展开目录'}
                                      >
                                        {r2ExpandedPrefixes.includes(row.key) ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                                      </Button>
                                    )}
                                    <Button
                                      type="button"
                                      size="xs"
                                      variant="ghost"
                                      className={`h-auto min-w-0 flex-1 justify-start gap-2 px-0 py-0 text-left bg-transparent! hover:bg-transparent! active:bg-transparent! focus-visible:bg-transparent! ${row.isFolder ? 'font-medium text-kumo-strong hover:text-brand!' : 'text-kumo-strong'}`}
                                      onClick={(event) => {
                                        event.stopPropagation();
                                        if (row.isLoading) return;
                                        if (row.isFolder) toggleR2FolderExpanded(row.key);
                                        else previewR2Object(row.key);
                                      }}
                                      title={row.key}
                                    >
                                      {row.isLoading ? <SkeletonLine className="h-3.5 w-3.5" /> : row.isFolder ? <Folder className="h-4 w-4 shrink-0 text-brand" /> : <FileText className="h-4 w-4 shrink-0 text-kumo-subtle" />}
                                      <span className="truncate">{row.isLoading ? '加载中…' : (row.name || row.key)}</span>
                                    </Button>
                                      </>
                                    )}
                                  </div>
                                </Table.Cell>
                                <Table.Cell>{row.isFolder ? '-' : formatBytes(row.size)}</Table.Cell>
                                <Table.Cell>{row.isFolder ? '-' : formatDate(row.uploaded || row.last_modified)}</Table.Cell>
                                <Table.Cell className="text-right">
                                  {row.isFolder ? (
                                    <div className="inline-flex gap-2" onClick={(event) => event.stopPropagation()}>
                                      <Button size="sm" shape="square" variant="secondary" onClick={() => downloadR2Folder(row.key)} aria-label={`下载目录 ${row.key}`} title="下载目录" icon={<Download className="h-4 w-4" />} />
                                      <Button size="sm" shape="square" variant={isArmed(`r2-object:${row.key}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteR2Object(row.key)} aria-label={`删除目录 ${row.key}`} title="删除目录" icon={<Trash className="h-4 w-4" />} />
                                    </div>
                                  ) : (
                                    <div className="inline-flex gap-2" onClick={(event) => event.stopPropagation()}>
                                      <Button size="sm" shape="square" variant="secondary" onClick={() => previewR2Object(row.key)} aria-label={`预览 ${row.key}`} title="预览" icon={<Eye className="h-4 w-4" />} />
                                      <Button size="sm" shape="square" variant="secondary" onClick={() => downloadR2Object(row.key)} aria-label={`下载 ${row.key}`} title="下载" icon={<Download className="h-4 w-4" />} />
                                      <Button size="sm" shape="square" variant={isArmed(`r2-object:${row.key}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteR2Object(row.key)} aria-label={`删除 ${row.key}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                                    </div>
                                  )}
                                </Table.Cell>
                              </Table.Row>
                            ))}
                          </Table.Body>
                        </Table>
                    </div>
                  </div>
                )}
              </AppCard>
            </div>
          )}

          {activeTab === 'tunnels' && (
            <SectionCard
              title="Tunnel"
              icon={<Lock className="h-4 w-4 text-brand" />}
              action={(
                <Button size="sm" onClick={() => { setTunnelForm({ name: '' }); setModal({ type: 'tunnelCreate', data: null }); }} icon={<Plus className="h-4 w-4" />}>
                  创建 Tunnel
                </Button>
              )}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
              <Table layout="fixed">
                <colgroup>{tunnelColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(0, e)} onTouchStart={(e) => startTunnelResize(0, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">状态<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(1, e)} onTouchStart={(e) => startTunnelResize(1, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">连接<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(2, e)} onTouchStart={(e) => startTunnelResize(2, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">创建时间<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(3, e)} onTouchStart={(e) => startTunnelResize(3, e)} /></Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {loading.tunnels ? (
                    Array.from({ length: 4 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
                  ) : tunnels.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">没有 Tunnel。</Table.Cell></Table.Row>
                  ) : tunnels.map((tunnel) => (
                    <Table.Row key={tunnel.id}>
                      <Table.Cell>
                        <div className="flex flex-col">
                          <span className="font-medium text-kumo-strong">{tunnel.name}</span>
                          <span className="text-xs text-kumo-subtle">{tunnel.id}</span>
                        </div>
                      </Table.Cell>
                      <Table.Cell><Badge variant={statusVariant(tunnel.status)}>{tunnelStatusLabel(tunnel.status, tunnel.connections || [])}</Badge></Table.Cell>
                      <Table.Cell>{tunnel.connections?.length || 0}</Table.Cell>
                      <Table.Cell>{formatDate(tunnel.createdAt)}</Table.Cell>
                      <Table.Cell className="text-right">
                        <div className="inline-flex flex-wrap justify-end gap-2">
                          <Button size="sm" variant="secondary" onClick={() => openTunnelTokenModal(tunnel)}>令牌</Button>
                          <Button size="sm" variant="secondary" onClick={() => openTunnelConfigModal(tunnel)}>配置</Button>
                          <Button size="sm" variant="secondary" onClick={() => openTunnelConnectionsModal(tunnel)}>连接</Button>
                          <Button size="sm" shape="square" variant="secondary" onClick={() => renameTunnel(tunnel)} aria-label={`重命名 ${tunnel.name}`} title="重命名" icon={<Edit className="h-4 w-4" />} />
                          <Button size="sm" shape="square" variant={isArmed(`tunnel:${tunnel.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTunnel(tunnel)} aria-label={`删除 ${tunnel.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </SectionCard>
          )}

          {activeTab === 'templates' && (
            <SectionCard
              title="DNS 模板"
              icon={<FileText className="h-4 w-4 text-brand" />}
              actions={(
                <>
                <Toolbar size="sm" aria-label="导出导入模板" className="shrink-0">
                  <Toolbar.Button onClick={() => openImportModal('templates')} aria-label="导入模板" title="导入模板" icon={<Download className="h-3.5 w-3.5" />}>
                    <span className="hidden cq-sm:inline">导入</span>
                  </Toolbar.Button>
                  <Toolbar.Button onClick={() => downloadJson(`cloudflare-dns-templates-${Date.now()}.json`, { version: '1.0', templates })} aria-label="导出模板" title="导出模板" icon={<Upload className="h-3.5 w-3.5" />}>
                    <span className="hidden cq-sm:inline">导出</span>
                  </Toolbar.Button>
                </Toolbar>
                <Button size="sm" onClick={() => openTemplateModal()} icon={<Plus className="h-4 w-4" />}>添加模板</Button>
                </>
              )}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
              <Table layout="fixed">
                <colgroup>{templateColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(0, e)} onTouchStart={(e) => startTemplateResize(0, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">记录数<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(1, e)} onTouchStart={(e) => startTemplateResize(1, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">描述<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(2, e)} onTouchStart={(e) => startTemplateResize(2, e)} /></Table.Head>
                    <Table.Head className="relative pr-6">更新时间<Table.ResizeHandle onMouseDown={(e) => startTemplateResize(3, e)} onTouchStart={(e) => startTemplateResize(3, e)} /></Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {templates.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">没有 DNS 模板。</Table.Cell></Table.Row>
                  ) : templates.map((template) => (
                    <Table.Row
                      key={template.id}
                      className="cursor-pointer"
                      title="双击编辑模板"
                      onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openTemplateModal(template))}
                    >
                      <Table.Cell className="font-medium text-kumo-strong">{template.name}</Table.Cell>
                      <Table.Cell>{template.records?.length || 0}</Table.Cell>
                      <Table.Cell><div className="truncate">{template.description || '-'}</div></Table.Cell>
                      <Table.Cell>{formatDate(template.updatedAt || template.createdAt)}</Table.Cell>
                      <Table.Cell className="text-right">
                        <div className="inline-flex gap-2">
                          <Button size="sm" variant="secondary" onClick={() => applyTemplate(template)}>应用</Button>
                          <Button size="sm" shape="square" variant="secondary" onClick={() => openTemplateModal(template)} aria-label={`编辑 ${template.name}`} title="编辑" icon={<Edit className="h-4 w-4" />} />
                          <Button size="sm" shape="square" variant={isArmed(`template:${template.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTemplate(template)} aria-label={`删除 ${template.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </SectionCard>
          )}

          {activeTab === 'accounts' && (
            <SectionCard
              title="Cloudflare 账号"
              icon={<Settings className="h-4 w-4 text-brand" />}
              actions={(
                <>
                <Toolbar size="sm" aria-label="导出导入账号" className="shrink-0">
                  <Toolbar.Button onClick={exportAccounts} aria-label="导出账号" title="导出账号" icon={<Upload className="h-3.5 w-3.5" />}>
                    <span className="hidden cq-sm:inline">导出</span>
                  </Toolbar.Button>
                  <Toolbar.Button onClick={() => openImportModal('accounts')} aria-label="导入账号" title="导入账号" icon={<Download className="h-3.5 w-3.5" />}>
                    <span className="hidden cq-sm:inline">导入</span>
                  </Toolbar.Button>
                </Toolbar>
                <Button size="sm" variant="primary" onClick={() => openAccountModal()} icon={<Plus className="h-4 w-4" />}>添加账号</Button>
                </>
              )}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
                <Table layout="fixed">
                  <colgroup>{accountColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head className="relative pr-6">备注名称<Table.ResizeHandle onMouseDown={(e) => startAccountResize(0, e)} onTouchStart={(e) => startAccountResize(0, e)} /></Table.Head>
                      <Table.Head className="relative pr-6">邮箱<Table.ResizeHandle onMouseDown={(e) => startAccountResize(1, e)} onTouchStart={(e) => startAccountResize(1, e)} /></Table.Head>
                      <Table.Head className="relative pr-6">Account ID<Table.ResizeHandle onMouseDown={(e) => startAccountResize(2, e)} onTouchStart={(e) => startAccountResize(2, e)} /></Table.Head>
                      <Table.Head className="relative pr-6">令牌<Table.ResizeHandle onMouseDown={(e) => startAccountResize(3, e)} onTouchStart={(e) => startAccountResize(3, e)} /></Table.Head>
                      <Table.Head className="relative pr-6">最后使用<Table.ResizeHandle onMouseDown={(e) => startAccountResize(4, e)} onTouchStart={(e) => startAccountResize(4, e)} /></Table.Head>
                      <Table.Head className="app-table-action">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {accounts.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={6} className="py-10 text-center text-kumo-subtle">暂无 Cloudflare 账号。</Table.Cell></Table.Row>
                    ) : accounts.map((account) => (
                      <Table.Row
                        key={account.id}
                        className="cursor-pointer"
                        title="双击编辑账号"
                        onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openAccountModal(account))}
                      >
                        <Table.Cell className="font-medium text-kumo-strong">{account.name}</Table.Cell>
                        <Table.Cell>{account.userEmail || account.email || '-'}</Table.Cell>
                        <Table.Cell><code className="block truncate text-xs">{account.cfAccountId || '-'}</code></Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-2">
                            <code className="truncate text-xs">{accountTokens[account.id] || (account.hasToken ? '••••••••••••••••' : '-')}</code>
                            {account.hasToken && (
                              <Button size="sm" shape="square" variant="secondary" onClick={() => toggleAccountToken(account)} aria-label="显示或隐藏令牌">
                                {accountTokens[account.id] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                              </Button>
                            )}
                          </div>
                        </Table.Cell>
                        <Table.Cell>{formatDate(account.lastUsed)}</Table.Cell>
                        <Table.Cell className="text-right">
                          <div className="inline-flex gap-2">
                            <Button size="sm" shape="square" variant="secondary" onClick={() => verifyAccount(account)} aria-label={`验证 ${account.name}`} title="验证" icon={<Shield className="h-4 w-4" />} />
                            <Button size="sm" shape="square" variant="secondary" onClick={() => openAccountModal(account)} aria-label={`编辑 ${account.name}`} title="编辑" icon={<Edit className="h-4 w-4" />} />
                            <Button size="sm" shape="square" variant={isArmed(`account:${account.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteAccount(account)} aria-label={`删除 ${account.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
            </SectionCard>
          )}
          </>
        )}
      </div>

      <Dialog.Root open={Boolean(modal.type)} onOpenChange={(open) => { if (!open) closeModal(); }}>
        {modal.type && (
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),48rem)] !w-[min(760px,calc(100vw-2rem))] !max-w-[min(760px,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4 scrollbar-thin">
          {modal.type === 'account' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">
                {modal.data ? '编辑 Cloudflare 账号' : '添加 Cloudflare 账号'}
              </Dialog.Title>
              <Dialog.Description className="text-sm text-kumo-subtle">
                推荐使用 API Token，邮箱可留空；账户 Token（cfat_）需额外填 Account ID。Origin CA Key（v1.0-）已弃用。
              </Dialog.Description>
              <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
                <Input size="sm" label="备注名称" value={accountForm.name} onChange={(event) => setAccountForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="生产账号" />
                <Input size="sm" label="邮箱" type="email" value={accountForm.email} onChange={(event) => setAccountForm((prev) => ({ ...prev, email: event.target.value }))} placeholder="name@example.com" />
              </div>
              <Input size="sm"
                label="Account ID"
                value={accountForm.cfAccountId}
                onChange={(event) => setAccountForm((prev) => ({ ...prev, cfAccountId: event.target.value }))}
                className="font-mono"
              />
              <Input size="sm"
                label="API Token / 全局 API Key"
                type="text"
                name="cf_credential_value"
                value={accountForm.apiToken}
                onChange={(event) => setAccountForm((prev) => ({ ...prev, apiToken: event.target.value }))}
                placeholder={modal.data ? '不修改请留空' : 'cfat_... 或普通 API Token'}
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-bwignore="true"
                data-form-type="other"
                spellCheck={false}
                className="font-mono"
              />
              <Checkbox
                checked={accountForm.skipVerify}
                onCheckedChange={(checked) => setAccountForm((prev) => ({ ...prev, skipVerify: Boolean(checked) }))}
                label="跳过 API 验证"
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" variant="primary" onClick={saveAccount} disabled={loading.saveAccount} icon={<Save className="h-4 w-4" />}>
                  保存
                </Button>
              </div>
            </div>
          )}

          {modal.type === 'zone' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">添加域名</Dialog.Title>
              <Input size="sm" label="域名" value={zoneForm.name} onChange={(event) => setZoneForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="example.com" />
              <div className="flex items-center justify-between rounded-md border border-kumo-line p-3">
                <div>
                  <div className="text-sm font-medium text-kumo-strong">扫描现有 DNS 记录</div>
                  <div className="text-xs text-kumo-subtle">对应 Cloudflare jump_start 参数。</div>
                </div>
                <Switch checked={zoneForm.jumpStart} onCheckedChange={(checked) => setZoneForm((prev) => ({ ...prev, jumpStart: Boolean(checked) }))} />
              </div>
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={saveZone} disabled={loading.saveZone} icon={<Plus className="h-4 w-4" />}>
                  添加
                </Button>
              </div>
            </div>
          )}

          {modal.type === 'record' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">
                {modal.data ? '编辑 DNS 记录' : '添加 DNS 记录'}
              </Dialog.Title>
              <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-3">
                <Select size="sm"
                  label="类型"
                  value={recordForm.type}
                  onValueChange={(value) => setRecordForm((prev) => ({ ...prev, type: String(value) }))}
                  items={recordTypes.map((type) => ({ value: type, label: type }))}
                />
                <Input size="sm" label="名称" value={recordForm.name} onChange={(event) => setRecordForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="@ 或 www" />
                <Input size="sm" label="TTL" type="number" value={String(recordForm.ttl)} onChange={(event) => setRecordForm((prev) => ({ ...prev, ttl: event.target.value }))} />
              </div>
              <Input size="sm" label="内容" value={recordForm.content} onChange={(event) => setRecordForm((prev) => ({ ...prev, content: event.target.value }))} placeholder="IP、域名或文本内容" />
              <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
                <Input size="sm" label="优先级" type="number" value={String(recordForm.priority)} onChange={(event) => setRecordForm((prev) => ({ ...prev, priority: event.target.value }))} />
                <div className="flex items-center justify-between rounded-md border border-kumo-line p-3">
                  <div>
                    <div className="text-sm font-medium text-kumo-strong">代理流量</div>
                    <div className="text-xs text-kumo-subtle">开启 Cloudflare 橙云代理。</div>
                  </div>
                  <Switch checked={recordForm.proxied} onCheckedChange={(checked) => setRecordForm((prev) => ({ ...prev, proxied: Boolean(checked) }))} />
                </div>
              </div>
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={saveRecord} disabled={loading.saveRecord} icon={<Save className="h-4 w-4" />}>
                  保存
                </Button>
              </div>
            </div>
          )}

          {modal.type === 'template' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">
                {modal.data ? '编辑 DNS 模板' : '添加 DNS 模板'}
              </Dialog.Title>
              <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
                <Input size="sm" label="模板名称" value={templateForm.name} onChange={(event) => setTemplateForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="默认 A 记录" />
                <Input size="sm" label="描述" value={templateForm.description} onChange={(event) => setTemplateForm((prev) => ({ ...prev, description: event.target.value }))} placeholder="可选" />
              </div>
              <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-4">
                <Select size="sm"
                  label="类型"
                  value={templateForm.type}
                  onValueChange={(value) => setTemplateForm((prev) => ({ ...prev, type: String(value) }))}
                  items={recordTypes.map((type) => ({ value: type, label: type }))}
                />
                <Input size="sm" label="记录名称" value={templateForm.recordName} onChange={(event) => setTemplateForm((prev) => ({ ...prev, recordName: event.target.value }))} />
                <Input size="sm" label="TTL" type="number" value={String(templateForm.ttl)} onChange={(event) => setTemplateForm((prev) => ({ ...prev, ttl: event.target.value }))} />
                <Input size="sm" label="优先级" type="number" value={String(templateForm.priority)} onChange={(event) => setTemplateForm((prev) => ({ ...prev, priority: event.target.value }))} />
              </div>
              <Input size="sm" label="内容" value={templateForm.content} onChange={(event) => setTemplateForm((prev) => ({ ...prev, content: event.target.value }))} />
              <Checkbox
                checked={templateForm.proxied}
                onCheckedChange={(checked) => setTemplateForm((prev) => ({ ...prev, proxied: Boolean(checked) }))}
                label="默认开启代理"
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={saveTemplate} disabled={loading.saveTemplate} icon={<Save className="h-4 w-4" />}>
                  保存
                </Button>
              </div>
            </div>
          )}

          {modal.type === 'worker' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">
                {modal.data ? `编辑 Worker：${modal.data.name}` : '新建 Worker'}
              </Dialog.Title>
              {!modal.data && (
                <Input size="sm" label="Worker 名称" value={workerForm.name} onChange={(event) => setWorkerForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="my-worker" />
              )}
              {modal.data && <Input size="sm" label="Worker 名称" value={workerForm.name} readOnly />}
              {loading.workerScript ? (
                <SkeletonLine className="h-64 w-full" />
              ) : (
                <CodeEditor
                  label="脚本内容"
                  language="javascript"
                  value={workerForm.script}
                  onChange={(script) => setWorkerForm((prev) => ({ ...prev, script }))}
                  minHeight="24rem"
                />
              )}
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={saveWorker} disabled={loading.saveWorker} icon={<Save className="h-4 w-4" />}>
                  保存
                </Button>
              </div>
            </div>
          )}

          {modal.type === 'workerRoutes' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Worker 路由：{workerRouteState.worker?.name}</Dialog.Title>
              <div className="grid grid-cols-1 gap-3 cq-md:grid-cols-[1fr_220px_auto]">
                <Input size="sm"
                  label="路由规则"
                  value={workerRouteState.form.pattern}
                  onChange={(event) => setWorkerRouteState((prev) => ({ ...prev, form: { ...prev.form, pattern: event.target.value } }))}
                  placeholder="example.com/*"
                />
                <Input size="sm"
                  label="Worker"
                  value={workerRouteState.form.script}
                  onChange={(event) => setWorkerRouteState((prev) => ({ ...prev, form: { ...prev.form, script: event.target.value } }))}
                />
                <div className="flex items-end">
                  <Button size="sm" onClick={saveWorkerRoute} disabled={loading.saveWorkerRoute}>
                    {workerRouteState.form.id ? '更新路由' : '添加路由'}
                  </Button>
                </div>
              </div>
              <LayerCard className="overflow-x-auto p-0">
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>规则</Table.Head>
                      <Table.Head>Worker</Table.Head>
                      <Table.Head className="app-table-action">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {workerRouteState.routes.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={3} className="py-8 text-center text-kumo-subtle">没有 Worker 路由。</Table.Cell></Table.Row>
                    ) : workerRouteState.routes.map((route) => (
                      <Table.Row key={route.id}>
                        <Table.Cell>{route.pattern}</Table.Cell>
                        <Table.Cell>{route.script || '-'}</Table.Cell>
                        <Table.Cell className="text-right">
                          <div className="inline-flex gap-2">
                            <Button size="sm" shape="square" variant="secondary" onClick={() => setWorkerRouteState((prev) => ({ ...prev, form: { id: route.id, pattern: route.pattern, script: route.script || workerRouteState.worker?.name || '' } }))} aria-label="编辑 Worker 路由" title="编辑" icon={<Edit className="h-4 w-4" />} />
                            <Button size="sm" shape="square" variant={isArmed(`worker-route:${route.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorkerRoute(route)} aria-label="删除 Worker 路由" title="删除" icon={<Trash className="h-4 w-4" />} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </LayerCard>
            </div>
          )}

          {modal.type === 'workerDomains' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Worker 自定义域名：{workerDomainState.worker?.name}</Dialog.Title>
              <div className="grid grid-cols-1 gap-3 cq-md:grid-cols-[1fr_180px_auto]">
                <Input size="sm" label="域名" value={workerDomainState.hostname} onChange={(event) => setWorkerDomainState((prev) => ({ ...prev, hostname: event.target.value }))} placeholder="worker.example.com" />
                <Input size="sm" label="环境" value={workerDomainState.environment} onChange={(event) => setWorkerDomainState((prev) => ({ ...prev, environment: event.target.value }))} />
                <div className="flex items-end"><Button size="sm" onClick={addWorkerDomain}>添加域名</Button></div>
              </div>
              <LayerCard className="overflow-x-auto p-0">
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row><Table.Head>域名</Table.Head><Table.Head>环境</Table.Head><Table.Head>Zone</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {workerDomainState.domains.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={4} className="py-8 text-center text-kumo-subtle">没有自定义域名。</Table.Cell></Table.Row>
                    ) : workerDomainState.domains.map((domain) => (
                      <Table.Row key={domain.id}>
                        <Table.Cell>{domain.hostname}</Table.Cell>
                        <Table.Cell>{domain.environment || '-'}</Table.Cell>
                        <Table.Cell>{domain.zoneName || domain.zoneId || '-'}</Table.Cell>
                        <Table.Cell className="text-right"><Button size="sm" shape="square" variant={isArmed(`worker-domain:${domain.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorkerDomain(domain)} aria-label={`删除 ${domain.hostname}`} title="删除" icon={<Trash className="h-4 w-4" />} /></Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </LayerCard>
            </div>
          )}

          {modal.type === 'workerAnalytics' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Worker 统计：{workerAnalyticsState.worker?.name}</Dialog.Title>
              {loading.workerAnalytics ? (
                <SkeletonLine className="h-64 w-full" />
              ) : (
                <CodeEditor label="统计数据" language="json" value={JSON.stringify(workerAnalyticsState.analytics || {}, null, 2)} readOnly minHeight="20rem" />
              )}
              <div className="flex justify-end"><Button size="sm" variant="secondary" onClick={closeModal}>关闭</Button></div>
            </div>
          )}

          {modal.type === 'pagesDeployments' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Pages 部署：{pagesDeployState.project?.name}</Dialog.Title>
              <LayerCard className="overflow-x-auto p-0">
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row><Table.Head>地址</Table.Head><Table.Head>环境</Table.Head><Table.Head>状态</Table.Head><Table.Head>创建时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {pagesDeployState.deployments.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={5} className="py-8 text-center text-kumo-subtle">没有部署记录。</Table.Cell></Table.Row>
                    ) : pagesDeployState.deployments.map((deployment) => (
                      <Table.Row key={deployment.id}>
                        <Table.Cell><div className="truncate">{deployment.url || '-'}</div></Table.Cell>
                        <Table.Cell>{deployment.environment || '-'}</Table.Cell>
                        <Table.Cell><Badge variant={statusVariant(deployment.status)}>{deployment.status || '未知'}</Badge></Table.Cell>
                        <Table.Cell>{formatDate(deployment.createdOn)}</Table.Cell>
                        <Table.Cell className="text-right">
                          <div className="inline-flex gap-2">
                            {deployment.url && <LinkButton size="sm" shape="square" variant="secondary" href={deployment.url} external aria-label="打开部署地址" title="打开" icon={<ExternalLink className="h-4 w-4" />} />}
                            <Button size="sm" shape="square" variant={isArmed(`pages-deployment:${deployment.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deletePagesDeployment(deployment)} aria-label="删除部署" title="删除" icon={<Trash className="h-4 w-4" />} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </LayerCard>
            </div>
          )}

          {modal.type === 'pagesDomains' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Pages 自定义域名：{pagesDomainState.project?.name}</Dialog.Title>
              <div className="grid grid-cols-1 gap-3 cq-md:grid-cols-[1fr_auto]">
                <Input size="sm" label="域名" value={pagesDomainState.domain} onChange={(event) => setPagesDomainState((prev) => ({ ...prev, domain: event.target.value }))} placeholder="www.example.com" />
                <div className="flex items-end"><Button size="sm" onClick={addPagesDomain}>添加域名</Button></div>
              </div>
              <LayerCard className="overflow-x-auto p-0">
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row><Table.Head>域名</Table.Head><Table.Head>状态</Table.Head><Table.Head>验证状态</Table.Head><Table.Head>创建时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {pagesDomainState.domains.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={5} className="py-8 text-center text-kumo-subtle">没有自定义域名。</Table.Cell></Table.Row>
                    ) : pagesDomainState.domains.map((domain) => (
                      <Table.Row key={domain.id || domain.name}>
                        <Table.Cell>{domain.name}</Table.Cell>
                        <Table.Cell><Badge variant={statusVariant(domain.status)}>{domain.status || '未知'}</Badge></Table.Cell>
                        <Table.Cell>{domain.validationStatus || '-'}</Table.Cell>
                        <Table.Cell>{formatDate(domain.createdOn)}</Table.Cell>
                        <Table.Cell className="text-right"><Button size="sm" shape="square" variant={isArmed(`pages-domain:${domain.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deletePagesDomain(domain)} aria-label={`删除 ${domain.name}`} title="删除" icon={<Trash className="h-4 w-4" />} /></Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </LayerCard>
            </div>
          )}

          {modal.type === 'r2Bucket' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">创建 R2 存储桶</Dialog.Title>
              <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
                <Input size="sm" label="存储桶名称" value={r2BucketForm.name} onChange={(event) => setR2BucketForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="my-bucket" />
                <Input size="sm" label="位置" value={r2BucketForm.location} onChange={(event) => setR2BucketForm((prev) => ({ ...prev, location: event.target.value }))} placeholder="auto" />
              </div>
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={createR2Bucket} disabled={loading.saveR2Bucket}>创建</Button>
              </div>
            </div>
          )}

          {modal.type === 'r2Folder' && (
            <div className="flex flex-col gap-4">
              <div>
                <Dialog.Title className="text-base font-semibold text-kumo-strong">新建文件夹</Dialog.Title>
                <Dialog.Description className="mt-1 text-xs text-kumo-subtle">
                  在当前路径 {r2CurrentPrefix || '/'} 下创建文件夹。
                </Dialog.Description>
              </div>
              <Input
                size="sm"
                label="文件夹名称"
                value={r2FolderForm.name}
                onChange={(event) => setR2FolderForm({ name: event.target.value })}
                placeholder="assets"
                autoFocus
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={createR2Folder} disabled={loading.createR2Folder}>创建</Button>
              </div>
            </div>
          )}

          {modal.type === 'r2Preview' && (
            <div className="flex min-h-0 flex-col gap-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <Dialog.Title className="truncate text-base font-semibold text-kumo-strong">预览：{modal.data?.name}</Dialog.Title>
                  <Dialog.Description className="mt-1 truncate text-xs text-kumo-subtle">{modal.data?.key}</Dialog.Description>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Button size="sm" variant="secondary" onClick={() => window.open(modal.data?.url, '_blank', 'noopener,noreferrer')} icon={<ExternalLink className="h-4 w-4" />}>
                    新窗口打开
                  </Button>
                  <Button size="sm" variant="secondary" onClick={closeModal}>关闭</Button>
                </div>
              </div>

              <div className="min-h-[28rem] overflow-hidden rounded-md border border-kumo-line bg-kumo-recessed/25">
                {modal.data?.kind === 'image' ? (
                  <div className="flex h-[68vh] max-h-[42rem] min-h-[28rem] items-center justify-center overflow-auto p-3">
                    <img src={modal.data.url} alt={modal.data.name} className="max-h-full max-w-full object-contain" />
                  </div>
                ) : modal.data?.kind === 'video' ? (
                  <div className="flex h-[68vh] max-h-[42rem] min-h-[28rem] items-center justify-center p-3">
                    <video src={modal.data.url} controls className="max-h-full max-w-full rounded-md bg-black" />
                  </div>
                ) : modal.data?.kind === 'audio' ? (
                  <div className="flex h-44 items-center justify-center p-6">
                    <audio src={modal.data.url} controls className="w-full max-w-xl" />
                  </div>
                ) : modal.data?.kind === 'frame' ? (
                  <iframe
                    title={`预览 ${modal.data.name}`}
                    src={modal.data.url}
                    className="h-[68vh] max-h-[42rem] min-h-[28rem] w-full bg-kumo-base"
                  />
                ) : (
                  <div className="flex min-h-[28rem] flex-col items-center justify-center gap-3 p-8 text-center">
                    <FileText className="h-8 w-8 text-kumo-subtle" />
                      <div>
                        <div className="font-medium text-kumo-strong">该类型暂不支持内嵌预览</div>
                        <div className="mt-1 text-sm text-kumo-subtle">由浏览器按文件类型处理。</div>
                      </div>
                    <Button size="sm" onClick={() => window.open(modal.data?.url, '_blank', 'noopener,noreferrer')} icon={<ExternalLink className="h-4 w-4" />}>
                      打开对象
                    </Button>
                  </div>
                )}
              </div>
            </div>
          )}

          {modal.type === 'import' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">
                导入{importState.kind === 'accounts' ? '账号' : importState.kind === 'templates' ? '模板' : 'DNS 记录'}
              </Dialog.Title>
              <CodeEditor
                label="JSON 内容"
                language="json"
                value={importState.text}
                onChange={(text) => setImportState((prev) => ({ ...prev, text }))}
                minHeight="20rem"
                placeholder='{"records":[{"type":"A","name":"@","content":"1.1.1.1","ttl":1,"proxied":false}]}'
              />
              {importState.kind !== 'records' && (
                <Checkbox
                  checked={importState.overwrite}
                  onCheckedChange={(checked) => setImportState((prev) => ({ ...prev, overwrite: Boolean(checked) }))}
                  label="覆盖现有数据"
                />
              )}
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={submitImport} icon={<Upload className="h-4 w-4" />}>导入</Button>
              </div>
            </div>
          )}

          {modal.type === 'tunnelCreate' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">创建 Tunnel</Dialog.Title>
              <Input size="sm" label="Tunnel 名称" value={tunnelForm.name} onChange={(event) => setTunnelForm({ name: event.target.value })} placeholder="my-tunnel" />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={createTunnel} disabled={loading.saveTunnel}>创建</Button>
              </div>
            </div>
          )}

          {modal.type === 'tunnelToken' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Tunnel 令牌：{tunnelTokenState.tunnel?.name}</Dialog.Title>
              {loading.tunnelToken ? (
                <SkeletonLine className="h-28 w-full" />
              ) : (
                <>
                  <div className="flex flex-col gap-2">
                    <ClipboardText
                      size="sm"
                      text={tunnelTokenState.token}
                      className="w-full"
                      tooltip={{ text: '复制令牌', copiedText: '令牌已复制', side: 'top' }}
                      labels={{ copyAction: '复制 Tunnel 令牌' }}
                    />
                    <ClipboardText
                      size="sm"
                      text={`cloudflared tunnel run --token ${tunnelTokenState.token}`}
                      className="w-full"
                      tooltip={{ text: '复制命令', copiedText: '运行命令已复制', side: 'top' }}
                      labels={{ copyAction: '复制 cloudflared 运行命令' }}
                    />
                  </div>
                </>
              )}
            </div>
          )}

          {modal.type === 'tunnelConfig' && (
            <div className="flex flex-col gap-4">
              <Dialog.Title className="text-base font-semibold text-kumo-strong">Tunnel 配置：{tunnelConfigState.tunnel?.name}</Dialog.Title>
              {loading.tunnelConfig ? (
                <SkeletonLine className="h-80 w-full" />
              ) : (
                <CodeEditor
                  label="配置 JSON"
                  language="json"
                  value={tunnelConfigState.text}
                  onChange={(text) => setTunnelConfigState((prev) => ({ ...prev, text }))}
                  minHeight="24rem"
                />
              )}
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="secondary" onClick={closeModal}>取消</Button>
                <Button size="sm" onClick={saveTunnelConfig} icon={<Save className="h-4 w-4" />}>保存</Button>
              </div>
            </div>
          )}

          {modal.type === 'tunnelConnections' && (
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Dialog.Title className="text-base font-semibold text-kumo-strong">Tunnel 连接：{tunnelConnectionState.tunnel?.name}</Dialog.Title>
                <Button size="sm" variant="secondary-destructive" onClick={() => cleanupTunnelConnections(tunnelConnectionState.tunnel)}>
                  清理全部连接
                </Button>
              </div>
              <LayerCard className="overflow-x-auto p-0">
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row><Table.Head>客户端</Table.Head><Table.Head>版本</Table.Head><Table.Head>架构</Table.Head><Table.Head>边缘节点</Table.Head><Table.Head>连接时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {tunnelConnectionState.connections.length === 0 ? (
                      <Table.Row><Table.Cell colSpan={6} className="py-8 text-center text-kumo-subtle">没有活动连接。</Table.Cell></Table.Row>
                    ) : tunnelConnectionState.connections.map((connection) => (
                      <Table.Row key={connection.id || connection.clientId}>
                        <Table.Cell><code className="text-xs">{connection.clientId || connection.id || '-'}</code></Table.Cell>
                        <Table.Cell>{connection.clientVersion || '-'}</Table.Cell>
                        <Table.Cell>{connection.arch || '-'}</Table.Cell>
                        <Table.Cell>{connection.coloName || '-'}</Table.Cell>
                        <Table.Cell>{formatDate(connection.connectedAt)}</Table.Cell>
                        <Table.Cell className="text-right">
                          <Button size="sm" variant="secondary-destructive" onClick={() => cleanupTunnelConnections(tunnelConnectionState.tunnel, connection.clientId || connection.id)}>
                            清理
                          </Button>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </LayerCard>
            </div>
          )}
          </div>
        </Dialog>
        )}
      </Dialog.Root>
    </PageStack>
  );
}

export default DnsPage;

