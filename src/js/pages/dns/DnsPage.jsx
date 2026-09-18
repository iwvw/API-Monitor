import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChartPalette, Tabs } from '@cloudflare/kumo';
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
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import useStore from '../../store.js';
import useTableResize from '../../composables/useTableResize.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { PageStack, SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, RefreshCw } from '../../components/Icons.jsx';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { CLOUDFLARE_TABS } from './tabs.jsx';
import {
  EMPTY_ACCOUNT_FORM,
  EMPTY_ANALYTICS,
  EMPTY_RECORD_FORM,
  EMPTY_TEMPLATE_FORM,
  EMPTY_TUNNEL_CONFIG,
  EMPTY_WORKER_FORM,
  EMPTY_ZONE_FORM,
  RECORD_TYPE_OPTIONS,
} from './constants.js';
import {
  downloadJson,
  formatBytes,
  formatNumber,
  formatNumberAxis,
  formatPercent,
  objectFileName,
  parseAnalyticsTimestamp,
  parseJsonInput,
  r2PreviewKind,
  recordShortName,
  toDisplayNumber,
} from './utils.jsx';
import ZoneListPanel from './ZoneListPanel.jsx';
import DnsRecordsPanel from './DnsRecordsPanel.jsx';
import WorkersPanel from './WorkersPanel.jsx';
import PagesPanel from './PagesPanel.jsx';
import R2Panel from './R2Panel.jsx';
import TunnelsPanel from './TunnelsPanel.jsx';
import TemplatesPanel from './TemplatesPanel.jsx';
import EmailRoutingPanel from './EmailRoutingPanel.jsx';
import AccountsPanel from './AccountsPanel.jsx';
import AccountDialog from './AccountDialog.jsx';
import ZoneDialog from './ZoneDialog.jsx';
import RecordDialog from './RecordDialog.jsx';
import TemplateDialog from './TemplateDialog.jsx';
import WorkerDialog from './WorkerDialog.jsx';
import WorkerRoutesDialog from './WorkerRoutesDialog.jsx';
import WorkerDomainsDialog from './WorkerDomainsDialog.jsx';
import WorkerAnalyticsDialog from './WorkerAnalyticsDialog.jsx';
import PagesDeploymentsDialog from './PagesDeploymentsDialog.jsx';
import PagesDomainsDialog from './PagesDomainsDialog.jsx';
import R2BucketDialog from './R2BucketDialog.jsx';
import R2FolderDialog from './R2FolderDialog.jsx';
import R2PreviewDialog from './R2PreviewDialog.jsx';
import ImportDialog from './ImportDialog.jsx';
import TunnelCreateDialog from './TunnelCreateDialog.jsx';
import TunnelTokenDialog from './TunnelTokenDialog.jsx';
import TunnelConfigDialog from './TunnelConfigDialog.jsx';
import TunnelConnectionsDialog from './TunnelConnectionsDialog.jsx';

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

function DnsPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const theme = useStore(s => s.theme);
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
                <ZoneListPanel
                  loading={loading}
                  zones={zones}
                  selectedZoneId={selectedZoneId}
                  selectedZone={selectedZone}
                  zoneColWidths={zoneColWidths}
                  isArmed={isArmed}
                  onOpenZoneModal={openZoneModal}
                  onPurgeZoneCache={purgeZoneCache}
                  onDeleteZone={deleteZone}
                  onSelectZone={selectZone}
                  onCopyText={copyText}
                />
              </section>

              <section className="flex min-w-0 flex-col gap-2">
                <DnsRecordsPanel
                  loading={loading}
                  records={records}
                  selectedZone={selectedZone}
                  selectedZoneId={selectedZoneId}
                  recordFilter={recordFilter}
                  setRecordFilter={setRecordFilter}
                  recordTypes={recordTypes}
                  recordColWidths={recordColWidths}
                  startRecordResize={startRecordResize}
                  renderResizeHead={renderResizeHead}
                  selectedRecordIds={selectedRecordIds}
                  setSelectedRecordIds={setSelectedRecordIds}
                  toggleRecordSelection={toggleRecordSelection}
                  isArmed={isArmed}
                  analyticsRange={analyticsRange}
                  loadAnalytics={loadAnalytics}
                  analyticsPoints={analyticsPoints}
                  showAnalyticsCharts={showAnalyticsCharts}
                  setShowAnalyticsCharts={setShowAnalyticsCharts}
                  showAnalyticsPanel={showAnalyticsPanel}
                  analyticsChartCards={analyticsChartCards}
                  analyticsSummary={analyticsSummary}
                  sslInfo={sslInfo}
                  updateSslMode={updateSslMode}
                  echarts={echarts}
                  isDarkMode={isDarkMode}
                  loadRecords={loadRecords}
                  openRecordModal={openRecordModal}
                  deleteRecord={deleteRecord}
                  exportRecords={exportRecords}
                  openImportModal={openImportModal}
                  batchDeleteRecords={batchDeleteRecords}
                />
              </section>
            </div>
          )}

          {activeTab === 'workers' && (
            <WorkersPanel
              loading={loading}
              workers={workers}
              workerSubdomain={workerSubdomain}
              workerColWidths={workerColWidths}
              startWorkerResize={startWorkerResize}
              isArmed={isArmed}
              openWorkerModal={openWorkerModal}
              openWorkerRoutesModal={openWorkerRoutesModal}
              openWorkerDomainsModal={openWorkerDomainsModal}
              openWorkerAnalyticsModal={openWorkerAnalyticsModal}
              toggleWorkerSubdomain={toggleWorkerSubdomain}
              deleteWorker={deleteWorker}
            />
          )}

          {activeTab === 'pages' && (
            <PagesPanel
              loading={loading}
              pages={pages}
              pageColWidths={pageColWidths}
              startPageResize={startPageResize}
              isArmed={isArmed}
              openPagesDeploymentsModal={openPagesDeploymentsModal}
              openPagesDomainsModal={openPagesDomainsModal}
              deletePagesProject={deletePagesProject}
            />
          )}

          {activeTab === 'r2' && (
            <R2Panel
              loading={loading}
              selectedAccountId={selectedAccountId}
              r2Buckets={r2Buckets}
              r2BucketSearch={r2BucketSearch}
              setR2BucketSearch={setR2BucketSearch}
              filteredR2Buckets={filteredR2Buckets}
              r2SelectedBucket={r2SelectedBucket}
              selectR2Bucket={selectR2Bucket}
              r2Metrics={r2Metrics}
              r2MetricsTotals={r2MetricsTotals}
              deleteR2Bucket={deleteR2Bucket}
              r2ColWidths={r2ColWidths}
              startR2Resize={startR2Resize}
              r2Prefixes={r2Prefixes}
              r2Objects={r2Objects}
              r2ObjectTotalBytes={r2ObjectTotalBytes}
              r2ObjectSearch={r2ObjectSearch}
              setR2ObjectSearch={setR2ObjectSearch}
              r2Rows={r2Rows}
              filteredR2Rows={filteredR2Rows}
              r2VisibleKeys={r2VisibleKeys}
              selectedR2Objects={selectedR2Objects}
              setSelectedR2Objects={setSelectedR2Objects}
              isArmed={isArmed}
              r2CurrentPrefix={r2CurrentPrefix}
              r2PathSegments={r2PathSegments}
              setR2BucketForm={setR2BucketForm}
              setR2FolderForm={setR2FolderForm}
              setModal={setModal}
              r2UploadInputRef={r2UploadInputRef}
              r2ExpandedPrefixes={r2ExpandedPrefixes}
              handleR2UploadFiles={handleR2UploadFiles}
              toggleR2FolderExpanded={toggleR2FolderExpanded}
              previewR2Object={previewR2Object}
              retryR2Dir={retryR2Dir}
              downloadR2Object={downloadR2Object}
              downloadR2Folder={downloadR2Folder}
              toggleR2Selection={toggleR2Selection}
              loadR2Objects={loadR2Objects}
              clearR2BucketCache={clearR2BucketCache}
              batchDeleteR2Objects={batchDeleteR2Objects}
              deleteR2Object={deleteR2Object}
            />
          )}

          {activeTab === 'tunnels' && (
            <TunnelsPanel
              loading={loading}
              tunnels={tunnels}
              tunnelColWidths={tunnelColWidths}
              startTunnelResize={startTunnelResize}
              isArmed={isArmed}
              setTunnelForm={setTunnelForm}
              setModal={setModal}
              openTunnelTokenModal={openTunnelTokenModal}
              openTunnelConfigModal={openTunnelConfigModal}
              openTunnelConnectionsModal={openTunnelConnectionsModal}
              renameTunnel={renameTunnel}
              deleteTunnel={deleteTunnel}
            />
          )}

          {activeTab === 'templates' && (
            <TemplatesPanel
              templates={templates}
              templateColWidths={templateColWidths}
              startTemplateResize={startTemplateResize}
              isArmed={isArmed}
              openImportModal={openImportModal}
              openTemplateModal={openTemplateModal}
              applyTemplate={applyTemplate}
              deleteTemplate={deleteTemplate}
            />
          )}

          {activeTab === 'email' && (
            <EmailRoutingPanel selectedAccountId={selectedAccountId} cfApi={cfApi} />
          )}

          {activeTab === 'accounts' && (
            <AccountsPanel
              accounts={accounts}
              accountTokens={accountTokens}
              accountColWidths={accountColWidths}
              startAccountResize={startAccountResize}
              isArmed={isArmed}
              openImportModal={openImportModal}
              openAccountModal={openAccountModal}
              exportAccounts={exportAccounts}
              toggleAccountToken={toggleAccountToken}
              verifyAccount={verifyAccount}
              deleteAccount={deleteAccount}
            />
          )}

          </>
        )}
      </div>

      <AccountDialog
        open={modal.type === 'account'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        modal={modal}
        accountForm={accountForm}
        setAccountForm={setAccountForm}
        loading={loading}
        onSaveAccount={saveAccount}
      />

      <ZoneDialog
        open={modal.type === 'zone'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        zoneForm={zoneForm}
        setZoneForm={setZoneForm}
        loading={loading}
        onSaveZone={saveZone}
      />

      <RecordDialog
        open={modal.type === 'record'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        modal={modal}
        recordForm={recordForm}
        setRecordForm={setRecordForm}
        recordTypes={recordTypes}
        loading={loading}
        onSaveRecord={saveRecord}
      />

      <TemplateDialog
        open={modal.type === 'template'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        modal={modal}
        templateForm={templateForm}
        setTemplateForm={setTemplateForm}
        recordTypes={recordTypes}
        loading={loading}
        onSaveTemplate={saveTemplate}
      />

      <WorkerDialog
        open={modal.type === 'worker'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        modal={modal}
        workerForm={workerForm}
        setWorkerForm={setWorkerForm}
        loading={loading}
        onSaveWorker={saveWorker}
      />

      <WorkerRoutesDialog
        open={modal.type === 'workerRoutes'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        workerRouteState={workerRouteState}
        setWorkerRouteState={setWorkerRouteState}
        loading={loading}
        isArmed={isArmed}
        onSaveWorkerRoute={saveWorkerRoute}
        onDeleteWorkerRoute={deleteWorkerRoute}
      />

      <WorkerDomainsDialog
        open={modal.type === 'workerDomains'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        workerDomainState={workerDomainState}
        setWorkerDomainState={setWorkerDomainState}
        isArmed={isArmed}
        onAddWorkerDomain={addWorkerDomain}
        onDeleteWorkerDomain={deleteWorkerDomain}
      />

      <WorkerAnalyticsDialog
        open={modal.type === 'workerAnalytics'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        workerAnalyticsState={workerAnalyticsState}
        loading={loading}
      />

      <PagesDeploymentsDialog
        open={modal.type === 'pagesDeployments'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        pagesDeployState={pagesDeployState}
        isArmed={isArmed}
        onDeletePagesDeployment={deletePagesDeployment}
      />

      <PagesDomainsDialog
        open={modal.type === 'pagesDomains'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        pagesDomainState={pagesDomainState}
        setPagesDomainState={setPagesDomainState}
        isArmed={isArmed}
        onAddPagesDomain={addPagesDomain}
        onDeletePagesDomain={deletePagesDomain}
      />

      <R2BucketDialog
        open={modal.type === 'r2Bucket'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        r2BucketForm={r2BucketForm}
        setR2BucketForm={setR2BucketForm}
        loading={loading}
        onCreateR2Bucket={createR2Bucket}
      />

      <R2FolderDialog
        open={modal.type === 'r2Folder'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        r2FolderForm={r2FolderForm}
        setR2FolderForm={setR2FolderForm}
        r2CurrentPrefix={r2CurrentPrefix}
        loading={loading}
        onCreateR2Folder={createR2Folder}
      />

      <R2PreviewDialog
        open={modal.type === 'r2Preview'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        modal={modal}
      />

      <ImportDialog
        open={modal.type === 'import'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        importState={importState}
        setImportState={setImportState}
        onSubmitImport={submitImport}
      />

      <TunnelCreateDialog
        open={modal.type === 'tunnelCreate'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        tunnelForm={tunnelForm}
        setTunnelForm={setTunnelForm}
        loading={loading}
        onCreateTunnel={createTunnel}
      />

      <TunnelTokenDialog
        open={modal.type === 'tunnelToken'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        tunnelTokenState={tunnelTokenState}
        loading={loading}
      />

      <TunnelConfigDialog
        open={modal.type === 'tunnelConfig'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        tunnelConfigState={tunnelConfigState}
        setTunnelConfigState={setTunnelConfigState}
        loading={loading}
        onSaveTunnelConfig={saveTunnelConfig}
      />

      <TunnelConnectionsDialog
        open={modal.type === 'tunnelConnections'}
        onOpenChange={(open) => { if (!open) closeModal(); }}
        tunnelConnectionState={tunnelConnectionState}
        onCleanupTunnelConnections={cleanupTunnelConnections}
      />
    </PageStack>
  );
}

export default DnsPage;
