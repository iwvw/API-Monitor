import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import * as echarts from 'echarts/core';
import { BarChart, LineChart } from 'echarts/charts';
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
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Chart, Tabs } from '@cloudflare/kumo';
import { createSiteFontEcharts } from '../chartFont.js';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  AppTable,
  ChartCard,
  ChartWarmupSkeleton,
  DataTableFrame,
  EmptyState,
  InsetPanel,
  KeyValueGrid,
  PageStack,
  ResponsiveSearchInput,
  SectionCard,
  StatusBadge,
  stickyTabsBaseClass,
} from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import { kumoHex, formatCompact } from './openai/utils.js';
import useStore from '../store.js';
import {
  Cloud,
  Copy,
  Cpu,
  Download,
  FolderOpen,
  Globe,
  HardDrive,
  Key,
  Layers,
  PieChart,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Server,
  Settings,
  Shield,
  Square,
  Trash,
  Upload,
} from '../components/Icons.jsx';

echarts.use([
  BarChart,
  LineChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);
const siteFontEcharts = createSiteFontEcharts(echarts);

const emptyAccountForm = {
  name: '',
  serviceAccountJson: '',
  defaultProjectId: '',
  description: '',
};

const emptyCreateForm = {
  name: '',
  zone: '',
  machineType: '',
  image: '',
  bootDiskSizeGb: '',
  network: '',
  subnetwork: '',
};

const tabs = [
  { value: 'instances', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />实例</span> },
  { value: 'disks', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="h-3.5 w-3.5" />磁盘</span> },
  { value: 'network', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />网络</span> },
  { value: 'storage', label: <span className="inline-flex items-center gap-1.5"><Layers className="h-3.5 w-3.5" />存储</span> },
  { value: 'billing', label: <span className="inline-flex items-center gap-1.5"><PieChart className="h-3.5 w-3.5" />费用</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />账号管理</span> },
];

const stateOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'RUNNING', label: '运行中' },
  { value: 'TERMINATED', label: '已停止' },
  { value: 'STOPPING', label: '停止中' },
  { value: 'STARTING', label: '启动中' },
  { value: 'PROVISIONING', label: '创建中' },
  { value: 'STAGING', label: '部署中' },
];

const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier' },
  { id: 'zone', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'machineType', role: 'meta', width: 120 },
  { id: 'createdAt', role: 'datetime' },
];
const DISK_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'zone', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'type', role: 'meta', width: 120 },
  { id: 'sizeGb', role: 'number', width: 88 },
  { id: 'status', role: 'status' },
];
const ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'clientEmail', role: 'identifier' },
  { id: 'defaultProjectId', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];
const FIREWALL_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'direction', role: 'meta', width: 90 },
  { id: 'action', role: 'status' },
  { id: 'priority', role: 'number', width: 80 },
  { id: 'network', role: 'meta', grow: 1, minWidth: 140 },
];
const ADDRESS_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'address', role: 'identifier' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'status', role: 'status' },
];
const BUCKET_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'location', role: 'meta', width: 140 },
  { id: 'storageClass', role: 'meta', width: 120 },
  { id: 'timeCreated', role: 'datetime' },
];

const CACHE_TTL_MS = {
  accounts: 30_000,
  projects: 5 * 60_000,
  instances: 45_000,
  disks: 60_000,
  firewalls: 5 * 60_000,
  addresses: 5 * 60_000,
  buckets: 60_000,
  objects: 30_000,
  billing: 10 * 60_000,
};

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

const unwrap = (result) => result?.data ?? result ?? {};

function getGcpStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['RUNNING', 'READY', 'SUCCESS', 'VALID', 'IN_USE', 'ACTIVE', 'DONE', 'RESERVED'].includes(normalized)) return 'success';
  if (['PROVISIONING', 'STAGING', 'STARTING', 'STOPPING', 'CREATING', 'PENDING', 'RESTORING'].includes(normalized)) return 'info';
  if (['TERMINATED', 'STOPPED', 'SUSPENDED', 'UNKNOWN', 'UNVERIFIED'].includes(normalized)) return 'neutral';
  if (['FAILED', 'FAILURE', 'ERROR', 'INVALID', 'DEGRADED'].includes(normalized)) return 'danger';
  return 'neutral';
}

function getVerifyStatusLabel(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (normalized === 'SUCCESS') return '已验证';
  if (normalized === 'FAILED') return '失败';
  return '未验证';
}

function formatGb(value) {
  const num = Number(value);
  if (!Number.isFinite(num)) return '-';
  return `${num} GB`;
}

function formatMemoryGb(memoryMb) {
  const num = Number(memoryMb);
  if (!Number.isFinite(num) || num <= 0) return '-';
  return `${(num / 1024).toFixed(1)} GB`;
}

function formatCount(value) {
  const num = Number(value) || 0;
  if (!Number.isFinite(num)) return '0';
  if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
  if (num >= 1000) return `${(num / 1000).toFixed(1)}k`;
  return String(num);
}

function formatDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatModelUsageAxis(timestamp) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

function formatSize(value) {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return '-';
  if (num >= 1024 * 1024 * 1024) return `${(num / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (num >= 1024 * 1024) return `${(num / 1024 / 1024).toFixed(1)} MB`;
  if (num >= 1024) return `${(num / 1024).toFixed(1)} KB`;
  return `${num} B`;
}

function GcpPage() {
  const theme = useStore((state) => state.theme);
  const isDarkMode = theme === 'dark';
  const [activeTab, setActiveTab] = useState('instances');
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [projects, setProjects] = useState([]);
  const [selectedProjectId, setSelectedProjectId] = useState('');
  const [instances, setInstances] = useState([]);
  const [disks, setDisks] = useState([]);
  const [firewalls, setFirewalls] = useState([]);
  const [addresses, setAddresses] = useState([]);
  const [buckets, setBuckets] = useState([]);
  const [objects, setObjects] = useState([]);
  const [selectedBucket, setSelectedBucket] = useState('');
  const [billingAccounts, setBillingAccounts] = useState([]);
  const [budgets, setBudgets] = useState([]);
  const [billingInfo, setBillingInfo] = useState(null);
  const [modelUsage, setModelUsage] = useState(null);
  const [loadingModelUsage, setLoadingModelUsage] = useState(false);
  const [modelUsageDays, setModelUsageDays] = useState(30);
  const [query, setQuery] = useState('');
  const [stateFilter, setStateFilter] = useState('all');
  const [loadingAccounts, setLoadingAccounts] = useState(false);
  const [loadingProjectScope, setLoadingProjectScope] = useState(false);
  const [loadingBuckets, setLoadingBuckets] = useState(false);
  const [loadingObjects, setLoadingObjects] = useState(false);
  const [loadingBilling, setLoadingBilling] = useState(false);
  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState(emptyAccountForm);
  const [submittingAccount, setSubmittingAccount] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [createForm, setCreateForm] = useState(emptyCreateForm);
  const [zones, setZones] = useState([]);
  const [machineTypes, setMachineTypes] = useState([]);
  const [subnetworks, setSubnetworks] = useState([]);
  const [images, setImages] = useState([]);
  const [loadingCreateOptions, setLoadingCreateOptions] = useState(false);
  const [submittingCreate, setSubmittingCreate] = useState(false);
  const [resizeDialogOpen, setResizeDialogOpen] = useState(false);
  const [resizeTarget, setResizeTarget] = useState(null);
  const [resizeSize, setResizeSize] = useState('');
  const [bucketDialogOpen, setBucketDialogOpen] = useState(false);
  const [bucketForm, setBucketForm] = useState({ name: '', location: '', storageClass: 'STANDARD' });
  const [submittingBucket, setSubmittingBucket] = useState(false);
  const [uploadingObject, setUploadingObject] = useState(false);
  const objectFileInputRef = useRef(null);
  const accountFileInputRef = useRef(null);

  const cacheRef = useRef({
    accounts: null,
    projects: new Map(),
    instances: new Map(),
    disks: new Map(),
    firewalls: new Map(),
    addresses: new Map(),
    buckets: new Map(),
    objects: new Map(),
    billing: new Map(),
    modelUsage: new Map(),
  });
  const scopeRef = useRef(`${selectedAccountId}/${selectedProjectId}`);
  useEffect(() => {
    scopeRef.current = `${selectedAccountId}/${selectedProjectId}`;
  }, [selectedAccountId, selectedProjectId]);

  const selectedAccount = accounts.find((account) => String(account.id) === String(selectedAccountId));
  const scopeReady = Boolean(selectedAccountId && selectedProjectId);

  const filteredInstances = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return instances.filter((instance) => {
      const matchesState = stateFilter === 'all' || instance.state === stateFilter;
      const haystack = [instance.name, instance.id, instance.state, instance.zone, instance.publicIp, instance.privateIp, instance.machineType]
        .join(' ').toLowerCase();
      return matchesState && (!keyword || haystack.includes(keyword));
    });
  }, [instances, query, stateFilter]);

  const modelUsageChartData = useMemo(
    () => (modelUsage?.daily ?? []).map((point) => ({
      label: formatModelUsageAxis(new Date(`${point.date}T00:00:00Z`).getTime()),
      value: Number(point.count) || 0,
    })),
    [modelUsage]
  );

  const modelUsageChartOptions = useMemo(() => {
    if (modelUsageChartData.length === 0) return null;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    const barColor = kumoHex('--color-brand');
    return {
      grid: { left: 8, right: 12, top: 10, bottom: 0, containLabel: true },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        appendTo: 'body',
        backgroundColor: kumoHex('--color-kumo-base'),
        textStyle: { color: axisColor, fontSize: 11 },
        valueFormatter: (value) => formatCompact(Number(value), 0),
      },
      xAxis: {
        type: 'category',
        data: modelUsageChartData.map((point) => point.label),
        boundaryGap: true,
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
        axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: gridColor } },
        axisLabel: { color: axisColor, fontSize: 10, formatter: (value) => formatCompact(Number(value), 0) },
      },
      series: [
        {
          type: 'bar',
          data: modelUsageChartData.map((point) => point.value),
          barMaxWidth: 26,
          itemStyle: { color: barColor, borderRadius: [2, 2, 0, 0] },
        },
      ],
    };
  }, [modelUsageChartData, isDarkMode]);

  const getCachedValue = useCallback((entry, ttl) => {
    if (!entry) return null;
    if (Date.now() - entry.at > ttl) return null;
    return entry.value;
  }, []);

  const apiFetch = useCallback(async (path, options = {}) => {
    const response = await fetch(path, { ...options, headers: getAuthHeaders() });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) throw new Error(result.error || '请求失败');
    return result;
  }, []);

  const loadAccounts = useCallback(async ({ force = false, silent = false } = {}) => {
    const cached = force ? null : getCachedValue(cacheRef.current.accounts, CACHE_TTL_MS.accounts);
    if (cached) {
      setAccounts(cached);
      if (!selectedAccountId && cached.length > 0) setSelectedAccountId(String(cached[0].id));
      return cached;
    }
    if (!silent) setLoadingAccounts(true);
    const request = (async () => {
      try {
        const result = await apiFetch('/api/gcp/accounts');
        const list = Array.isArray(unwrap(result)) ? unwrap(result) : [];
        cacheRef.current.accounts = { value: list, at: Date.now() };
        setAccounts(list);
        if (!selectedAccountId && list.length > 0) setSelectedAccountId(String(list[0].id));
        return list;
      } catch (error) {
        toast.error(error.message || '加载 GCP 账号失败');
        return [];
      } finally {
        if (!silent) setLoadingAccounts(false);
      }
    })();
    return request;
  }, [apiFetch, getCachedValue, selectedAccountId]);

  const scopeCacheKey = useCallback((key) => `${selectedAccountId}/${selectedProjectId}`, [selectedAccountId, selectedProjectId]);

  const loadProjects = useCallback(async ({ force = false, silent = false } = {}) => {
    if (!selectedAccountId) return [];
    const startedAccount = String(selectedAccountId);
    const cacheKey = startedAccount;
    const cached = force ? null : getCachedValue(cacheRef.current.projects.get(cacheKey), CACHE_TTL_MS.projects);
    if (cached) {
      if (scopeRef.current.split('/')[0] !== startedAccount) return cached;
      setProjects(cached);
      setSelectedProjectId((current) => {
        if (current && cached.some((p) => p.projectId === current)) return current;
        return selectedAccount?.defaultProjectId || cached[0]?.projectId || '';
      });
      return cached;
    }
    try {
      const result = await apiFetch(`/api/gcp/accounts/${startedAccount}/projects`);
      const items = unwrap(result).projects || [];
      if (scopeRef.current.split('/')[0] !== startedAccount) return items;
      cacheRef.current.projects.set(cacheKey, { value: items, at: Date.now() });
      setProjects(items);
      setSelectedProjectId((current) => {
        if (current && items.some((p) => p.projectId === current)) return current;
        return selectedAccount?.defaultProjectId || items[0]?.projectId || '';
      });
      return items;
    } catch (error) {
      toast.error(error.message || '加载 GCP 项目失败');
      return [];
    }
  }, [apiFetch, getCachedValue, selectedAccount, selectedAccountId]);

  const loadInstances = useCallback(async ({ force = false, silent = false } = {}) => {
    if (!scopeReady) return [];
    const startedScope = scopeCacheKey();
    const cacheKey = startedScope;
    const cached = force ? null : getCachedValue(cacheRef.current.instances.get(cacheKey), CACHE_TTL_MS.instances);
    if (cached) { if (scopeRef.current !== startedScope) return cached; setInstances(cached); return cached; }
    if (!silent) setLoadingProjectScope(true);
    try {
      const result = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/instances`);
      const items = unwrap(result).instances || [];
      if (scopeRef.current !== startedScope) return items;
      cacheRef.current.instances.set(cacheKey, { value: items, at: Date.now() });
      setInstances(items);
      return items;
    } catch (error) {
      toast.error(error.message || '加载 GCP 实例失败');
      return [];
    } finally {
      if (!silent) setLoadingProjectScope(false);
    }
  }, [apiFetch, getCachedValue, scopeCacheKey, scopeReady, selectedAccountId, selectedProjectId]);

  const loadDisks = useCallback(async ({ force = false, silent = false } = {}) => {
    if (!scopeReady) return [];
    const startedScope = scopeCacheKey();
    const cacheKey = startedScope;
    const cached = force ? null : getCachedValue(cacheRef.current.disks.get(cacheKey), CACHE_TTL_MS.disks);
    if (cached) { if (scopeRef.current !== startedScope) return cached; setDisks(cached); return cached; }
    if (!silent) setLoadingProjectScope(true);
    try {
      const result = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/disks`);
      const items = unwrap(result).disks || [];
      if (scopeRef.current !== startedScope) return items;
      cacheRef.current.disks.set(cacheKey, { value: items, at: Date.now() });
      setDisks(items);
      return items;
    } catch (error) {
      toast.error(error.message || '加载 GCP 磁盘失败');
      return [];
    } finally {
      if (!silent) setLoadingProjectScope(false);
    }
  }, [apiFetch, getCachedValue, scopeCacheKey, scopeReady, selectedAccountId, selectedProjectId]);

  const loadNetwork = useCallback(async ({ force = false } = {}) => {
    if (!scopeReady) return;
    const startedScope = scopeCacheKey();
    const cacheKey = startedScope;
    if (!force) {
      const fwCached = getCachedValue(cacheRef.current.firewalls.get(cacheKey), CACHE_TTL_MS.firewalls);
      const addrCached = getCachedValue(cacheRef.current.addresses.get(cacheKey), CACHE_TTL_MS.addresses);
      if (fwCached && addrCached) { if (scopeRef.current !== startedScope) return; setFirewalls(fwCached); setAddresses(addrCached); return; }
    }
    setLoadingProjectScope(true);
    try {
      const [fwResult, addrResult] = await Promise.all([
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/firewalls`),
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/addresses`),
      ]);
      const fw = unwrap(fwResult).firewalls || [];
      const addr = unwrap(addrResult).addresses || [];
      if (scopeRef.current !== startedScope) return;
      cacheRef.current.firewalls.set(cacheKey, { value: fw, at: Date.now() });
      cacheRef.current.addresses.set(cacheKey, { value: addr, at: Date.now() });
      setFirewalls(fw);
      setAddresses(addr);
    } catch (error) {
      toast.error(error.message || '加载 GCP 网络失败');
    } finally {
      setLoadingProjectScope(false);
    }
  }, [apiFetch, getCachedValue, scopeCacheKey, scopeReady, selectedAccountId, selectedProjectId]);

  const loadBuckets = useCallback(async ({ force = false, silent = false } = {}) => {
    if (!selectedAccountId || !selectedProjectId) return [];
    const startedScope = scopeCacheKey();
    const cacheKey = startedScope;
    const cached = force ? null : getCachedValue(cacheRef.current.buckets.get(cacheKey), CACHE_TTL_MS.buckets);
    if (cached) { if (scopeRef.current !== startedScope) return cached; setBuckets(cached); return cached; }
    if (!silent) setLoadingBuckets(true);
    try {
      const result = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets`);
      const items = unwrap(result).buckets || [];
      if (scopeRef.current !== startedScope) return items;
      cacheRef.current.buckets.set(cacheKey, { value: items, at: Date.now() });
      setBuckets(items);
      return items;
    } catch (error) {
      toast.error(error.message || '加载 GCP 存储桶失败');
      return [];
    } finally {
      if (!silent) setLoadingBuckets(false);
    }
  }, [apiFetch, getCachedValue, scopeCacheKey, selectedAccountId, selectedProjectId]);

  const loadObjects = useCallback(async ({ force = false, silent = false } = {}) => {
    if (!selectedAccountId || !selectedBucket) return [];
    const startedScope = `${selectedAccountId}/${selectedBucket}`;
    const cacheKey = startedScope;
    const cached = force ? null : getCachedValue(cacheRef.current.objects.get(cacheKey), CACHE_TTL_MS.objects);
    if (cached) { if (scopeRef.current.split('/')[0] !== String(selectedAccountId)) return cached; setObjects(cached); return cached; }
    if (!silent) setLoadingObjects(true);
    try {
      const result = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/buckets/${selectedBucket}/objects`);
      const items = unwrap(result).objects || [];
      if (scopeRef.current.split('/')[0] !== String(selectedAccountId)) return items;
      cacheRef.current.objects.set(cacheKey, { value: items, at: Date.now() });
      setObjects(items);
      return items;
    } catch (error) {
      toast.error(error.message || '加载对象列表失败');
      return [];
    } finally {
      if (!silent) setLoadingObjects(false);
    }
  }, [apiFetch, getCachedValue, selectedAccountId, selectedBucket]);

  const loadBilling = useCallback(async ({ force = false } = {}) => {
    if (!selectedAccountId) return;
    const startedScope = `${selectedAccountId}/${selectedProjectId || 'none'}`;
    const cacheKey = startedScope;
    const cached = force ? null : getCachedValue(cacheRef.current.billing.get(cacheKey), CACHE_TTL_MS.billing);
    if (cached) {
      if (scopeRef.current !== startedScope) return;
      setBillingAccounts(cached.accounts || []);
      setBudgets(cached.budgets || []);
      setBillingInfo(cached.billingInfo || null);
      return;
    }
    setLoadingBilling(true);
    try {
      const accountResult = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/billing-accounts`);
      const accountsList = unwrap(accountResult).billingAccounts || [];
      let budgetsList = [];
      if (accountsList.length > 0) {
        const billingId = accountsList[0].name;
        try {
          const budgetResult = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/billing-accounts/${encodeURIComponent(billingId)}/budgets`);
          budgetsList = unwrap(budgetResult).budgets || [];
        } catch {
          budgetsList = [];
        }
      }
      let billingInfoItem = null;
      if (selectedProjectId) {
        try {
          const infoResult = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${encodeURIComponent(selectedProjectId)}/billing-info`);
          billingInfoItem = unwrap(infoResult).billingInfo || null;
        } catch {
          billingInfoItem = null;
        }
      }
      cacheRef.current.billing.set(cacheKey, {
        value: { accounts: accountsList, budgets: budgetsList, billingInfo: billingInfoItem },
        at: Date.now(),
      });
      if (scopeRef.current !== startedScope) return;
      setBillingAccounts(accountsList);
      setBudgets(budgetsList);
      setBillingInfo(billingInfoItem);
    } catch (error) {
      toast.error(error.message || '加载 GCP 计费信息失败');
    } finally {
      setLoadingBilling(false);
    }
  }, [apiFetch, getCachedValue, selectedAccountId, selectedProjectId]);

  const loadModelUsage = useCallback(async ({ force = false, silent = false } = {}) => {
    if (!selectedAccountId || !selectedProjectId) return null;
    const startedScope = scopeCacheKey();
    const cacheKey = `${selectedAccountId}/${selectedProjectId}/${modelUsageDays}`;
    const CACHE_MODEL_USAGE_MS = 5 * 60_000;
    const cached = force ? null : getCachedValue(cacheRef.current.modelUsage.get(cacheKey), CACHE_MODEL_USAGE_MS);
    if (cached) {
      if (scopeRef.current !== startedScope) return cached;
      setModelUsage(cached);
      return cached;
    }
    if (!silent) setLoadingModelUsage(true);
    try {
      const result = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${encodeURIComponent(selectedProjectId)}/model-usage?days=${modelUsageDays}`);
      const item = unwrap(result).modelUsage || null;
      if (scopeRef.current !== startedScope) return item;
      cacheRef.current.modelUsage.set(cacheKey, { value: item, at: Date.now() });
      setModelUsage(item);
      return item;
    } catch (error) {
      toast.error(error.message || '加载模型用量失败');
      return null;
    } finally {
      if (!silent) setLoadingModelUsage(false);
    }
  }, [apiFetch, getCachedValue, selectedAccountId, selectedProjectId, modelUsageDays]);

  useEffect(() => {
    loadAccounts({ silent: true });
  }, [loadAccounts]);

  useEffect(() => {
    if (!selectedAccountId) return;
    loadProjects({ silent: true });
  }, [selectedAccountId, loadProjects]);

  useEffect(() => {
    if (!scopeReady) return;
    if (activeTab === 'instances') loadInstances({ silent: true });
    if (activeTab === 'disks') loadDisks({ silent: true });
    if (activeTab === 'network') loadNetwork({ silent: true });
    if (activeTab === 'storage') loadBuckets({ silent: true });
    if (activeTab === 'billing') loadBilling({ silent: true });
    setModelUsage(null);
    if (activeTab === 'billing') loadModelUsage({ silent: true });
  }, [activeTab, scopeReady, selectedProjectId, modelUsageDays, loadInstances, loadDisks, loadNetwork, loadBuckets, loadBilling, loadModelUsage]);

  const setDefaultProject = useCallback(async (projectId) => {
    if (!selectedAccountId) return;
    try {
      await apiFetch(`/api/gcp/accounts/${selectedAccountId}/default-project`, {
        method: 'PUT',
        body: JSON.stringify({ defaultProjectId: projectId }),
      });
      setAccounts((current) => current.map((account) => (
        String(account.id) === String(selectedAccountId) ? { ...account, defaultProjectId: projectId } : account
      )));
      toast.success('已设置默认项目');
      cacheRef.current.projects.delete(String(selectedAccountId));
      loadProjects({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '设置默认项目失败');
    }
  }, [apiFetch, loadProjects, selectedAccountId]);

  const openCreateAccount = () => {
    setEditingAccount(null);
    setAccountForm(emptyAccountForm);
    setAccountDialogOpen(true);
  };

  const openEditAccount = (account) => {
    setEditingAccount(account);
    setAccountForm({
      name: account.name || '',
      serviceAccountJson: '',
      defaultProjectId: account.defaultProjectId || '',
      description: account.description || '',
    });
    setAccountDialogOpen(true);
  };

  const importAccountJsonFile = (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      try {
        const text = String(reader.result || '');
        JSON.parse(text);
        setAccountForm((current) => ({ ...current, serviceAccountJson: text.trim() }));
        if (!accountForm.name.trim() && /"client_email"\s*:\s*"([^"]+)"/.test(text)) {
          const email = text.match(/"client_email"\s*:\s*"([^"]+)"/)?.[1] || '';
          setAccountForm((current) => ({ ...current, name: current.name || (email.split('@')[0] || 'GCP 账号') }));
        }
        toast.success('已读取服务账号文件');
      } catch {
        toast.error('文件不是有效的 JSON');
      }
    };
    reader.onerror = () => toast.error('读取文件失败');
    reader.readAsText(file);
    event.target.value = '';
  };

  const submitAccount = async () => {
    if (!accountForm.name.trim()) { toast.error('请填写账号名称'); return; }
    if (!editingAccount && !accountForm.serviceAccountJson.trim()) { toast.error('请粘贴 Service Account JSON'); return; }
    setSubmittingAccount(true);
    try {
      const payload = {
        name: accountForm.name.trim(),
        defaultProjectId: accountForm.defaultProjectId.trim() || undefined,
        description: accountForm.description.trim() || undefined,
      };
      if (accountForm.serviceAccountJson.trim()) payload.serviceAccountJson = accountForm.serviceAccountJson.trim();
      await apiFetch(`/api/gcp/accounts${editingAccount ? `/${editingAccount.id}` : ''}`, {
        method: editingAccount ? 'PUT' : 'POST',
        body: JSON.stringify(payload),
      });
      toast.success(editingAccount ? '账号已更新' : '账号已新增');
      setAccountDialogOpen(false);
      cacheRef.current.accounts = null;
      loadAccounts({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '保存账号失败');
    } finally {
      setSubmittingAccount(false);
    }
  };

  const verifyAccount = async (account) => {
    try {
      await apiFetch(`/api/gcp/accounts/${account.id}/verify`, { method: 'POST', body: '{}' });
      toast.success('账号验证成功');
      cacheRef.current.accounts = null;
      loadAccounts({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '账号验证失败');
    }
  };

  const deleteAccount = async (account) => {
    const ok = await dialog.deleteResource({
      title: '删除 GCP 账号',
      message: `确定删除账号「${account.name}」吗？相关凭证将永久移除。`,
      confirmLabel: '删除账号',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/gcp/accounts/${account.id}`, { method: 'DELETE' });
      toast.success('账号已删除');
      if (String(selectedAccountId) === String(account.id)) setSelectedAccountId('');
      cacheRef.current.accounts = null;
      loadAccounts({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '删除账号失败');
    }
  };

  const runInstanceAction = async (instance, action) => {
    let confirmed = true;
    if (action === 'delete') {
      confirmed = await dialog.deleteResource({
        title: '删除实例',
        message: `确定删除实例「${instance.name}」吗？此操作仅删除实例本身，磁盘是否随删由各盘 autoDelete 决定。`,
        confirmLabel: '删除实例',
      });
    }
    if (!confirmed) return;
    try {
      const result = await apiFetch(
        `/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/instances/${encodeURIComponent(instance.name)}/actions?zone=${encodeURIComponent(instance.zone)}`,
        { method: 'POST', body: JSON.stringify({ action }) }
      );
      const op = unwrap(result);
      const opName = op.name || op.operation?.name;
      toast.success(opName ? `指令已下发（Operation: ${opName}）` : '指令已下发');
      cacheRef.current.instances.delete(scopeCacheKey());
      setTimeout(() => loadInstances({ force: true, silent: true }), 1500);
    } catch (error) {
      toast.error(error.message || `实例操作 ${action} 失败`);
    }
  };

  const openCreateInstance = async () => {
    setCreateForm(emptyCreateForm);
    setCreateDialogOpen(true);
    setLoadingCreateOptions(true);
    try {
      const [zonesResult, machineResult, imageResult, subResult] = await Promise.all([
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/zones`),
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/machine-types`),
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/images`),
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/subnetworks`),
      ]);
      setZones(unwrap(zonesResult).zones || []);
      setMachineTypes(unwrap(machineResult).machineTypes || []);
      setImages(unwrap(imageResult).images || []);
      setSubnetworks(unwrap(subResult).subnetworks || []);
    } catch (error) {
      toast.error(error.message || '加载创建选项失败');
    } finally {
      setLoadingCreateOptions(false);
    }
  };

  const submitCreate = async () => {
    if (!createForm.name.trim() || !createForm.zone || !createForm.machineType) {
      toast.error('请填写名称、可用区和机型');
      return;
    }
    setSubmittingCreate(true);
    try {
      const payload = {
        name: createForm.name.trim(),
        zone: createForm.zone,
        machineType: createForm.machineType,
        image: createForm.image || undefined,
        bootDiskSizeGb: createForm.bootDiskSizeGb ? Number(createForm.bootDiskSizeGb) : undefined,
        network: createForm.network || undefined,
        subnetwork: createForm.subnetwork || undefined,
      };
      await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/instances`, {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      toast.success('创建实例指令已下发');
      setCreateDialogOpen(false);
      cacheRef.current.instances.delete(scopeCacheKey());
      setTimeout(() => loadInstances({ force: true, silent: true }), 1500);
    } catch (error) {
      toast.error(error.message || '创建实例失败');
    } finally {
      setSubmittingCreate(false);
    }
  };

  const openResizeDisk = (disk) => {
    setResizeTarget(disk);
    setResizeSize(String(disk.sizeGb || ''));
    setResizeDialogOpen(true);
  };

  const submitResize = async () => {
    const size = Number(resizeSize);
    if (!Number.isFinite(size) || size <= 0) { toast.error('请输入有效的磁盘大小'); return; }
    try {
      await apiFetch(
        `/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/disks/${encodeURIComponent(resizeTarget.name)}/resize?zone=${encodeURIComponent(resizeTarget.zone)}`,
        { method: 'POST', body: JSON.stringify({ sizeGb: size }) }
      );
      toast.success('磁盘扩容指令已下发');
      setResizeDialogOpen(false);
      cacheRef.current.disks.delete(scopeCacheKey());
      setTimeout(() => loadDisks({ force: true, silent: true }), 1500);
    } catch (error) {
      toast.error(error.message || '磁盘扩容失败');
    }
  };

  const snapshotDisk = async (disk) => {
    const ok = await dialog.deleteResource({
      title: '创建磁盘快照',
      message: `确定为磁盘「${disk.name}」创建快照吗？`,
      confirmLabel: '创建快照',
    });
    if (!ok) return;
    try {
      await apiFetch(
        `/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/disks/${encodeURIComponent(disk.name)}/snapshot?zone=${encodeURIComponent(disk.zone)}`,
        { method: 'POST', body: '{}' }
      );
      toast.success('快照指令已下发');
    } catch (error) {
      toast.error(error.message || '创建快照失败');
    }
  };

  const deleteDisk = async (disk) => {
    const ok = await dialog.deleteResource({
      title: '删除磁盘',
      message: `确定删除磁盘「${disk.name}」吗？此操作不可恢复。`,
      confirmLabel: '删除磁盘',
    });
    if (!ok) return;
    try {
      await apiFetch(
        `/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/disks/${encodeURIComponent(disk.name)}?zone=${encodeURIComponent(disk.zone)}`,
        { method: 'DELETE' }
      );
      toast.success('磁盘删除指令已下发');
      cacheRef.current.disks.delete(scopeCacheKey());
      setTimeout(() => loadDisks({ force: true, silent: true }), 1500);
    } catch (error) {
      toast.error(error.message || '删除磁盘失败');
    }
  };

  const submitBucket = async () => {
    if (!bucketForm.name.trim()) { toast.error('请填写存储桶名称'); return; }
    setSubmittingBucket(true);
    try {
      await apiFetch(`/api/gcp/accounts/${selectedAccountId}/buckets?projectId=${encodeURIComponent(selectedProjectId)}`, {
        method: 'POST',
        body: JSON.stringify({
          name: bucketForm.name.trim(),
          location: bucketForm.location.trim() || undefined,
          storageClass: bucketForm.storageClass,
          versioning: false,
        }),
      });
      toast.success('存储桶已创建');
      setBucketDialogOpen(false);
      cacheRef.current.buckets.delete(scopeCacheKey());
      loadBuckets({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '创建存储桶失败');
    } finally {
      setSubmittingBucket(false);
    }
  };

  const deleteBucket = async (bucket) => {
    const ok = await dialog.deleteResource({
      title: '删除存储桶',
      message: `确定删除存储桶「${bucket.name}」吗？仅支持删除空桶。`,
      confirmLabel: '删除存储桶',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/gcp/accounts/${selectedAccountId}/buckets/${encodeURIComponent(bucket.name)}`, { method: 'DELETE' });
      toast.success('存储桶已删除');
      if (selectedBucket === bucket.name) { setSelectedBucket(''); setObjects([]); }
      cacheRef.current.buckets.delete(scopeCacheKey());
      loadBuckets({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '删除存储桶失败');
    }
  };

  const deleteObject = async (object) => {
    const ok = await dialog.deleteResource({
      title: '删除对象',
      message: `确定删除对象「${object.name}」吗？`,
      confirmLabel: '删除对象',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/gcp/accounts/${selectedAccountId}/buckets/${encodeURIComponent(selectedBucket)}/objects/${encodeURIComponent(object.name)}`, { method: 'DELETE' });
      toast.success('对象已删除');
      cacheRef.current.objects.delete(`${selectedAccountId}/${selectedBucket}`);
      loadObjects({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '删除对象失败');
    }
  };

  const copyText = (text) => {
    if (!text) return;
    navigator.clipboard?.writeText(text).then(
      () => toast.success('已复制'),
      () => toast.error('复制失败')
    );
  };

  const uploadObject = async (file) => {
    if (!file || !selectedBucket) return;
    setUploadingObject(true);
    try {
      const params = new URLSearchParams({ name: file.name || 'uploaded' });
      const response = await fetch(
        `/api/gcp/accounts/${selectedAccountId}/buckets/${encodeURIComponent(selectedBucket)}/objects?${params.toString()}`,
        {
          method: 'POST',
          headers: { 'Content-Type': file.type || 'application/octet-stream' },
          body: file,
        }
      );
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) throw new Error(result.error || '上传对象失败');
      toast.success('对象已上传');
      cacheRef.current.objects.delete(`${selectedAccountId}/${selectedBucket}`);
      loadObjects({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '上传对象失败');
    } finally {
      setUploadingObject(false);
      if (objectFileInputRef.current) objectFileInputRef.current.value = '';
    }
  };

  const downloadObject = async (object) => {
    try {
      const response = await fetch(
        `/api/gcp/accounts/${selectedAccountId}/buckets/${encodeURIComponent(selectedBucket)}/objects/${encodeURIComponent(object.name)}/download`
      );
      if (!response.ok) {
        const result = await response.json().catch(() => ({}));
        throw new Error(result.error || '下载对象失败');
      }
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = object.name.split('/').pop() || object.name;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) {
      toast.error(error.message || '下载对象失败');
    }
  };

  const renderInstanceActions = (instance) => {
    const running = instance.state === 'RUNNING';
    const terminated = instance.state === 'TERMINATED';
    return (
      <div className="flex items-center justify-end gap-1">
        {!running && !terminated && <span className="px-1 text-xs text-kumo-subtle">处理中</span>}
        {!running && (
          <Button type="button" size="sm" variant="secondary" title="启动" onClick={() => runInstanceAction(instance, 'start')}>
            <Play className="h-4 w-4" />
          </Button>
        )}
        {running && (
          <>
            <Button type="button" size="sm" variant="secondary" title="停止" onClick={() => runInstanceAction(instance, 'stop')}>
              <Square className="h-4 w-4" />
            </Button>
            <Button type="button" size="sm" variant="secondary" title="重启" onClick={() => runInstanceAction(instance, 'reset')}>
              <RotateCw className="h-4 w-4" />
            </Button>
          </>
        )}
        {!terminated && (
          <Button type="button" size="sm" variant="danger" title="删除" onClick={() => runInstanceAction(instance, 'delete')}>
            <Trash className="h-4 w-4" />
          </Button>
        )}
      </div>
    );
  };

  const renderDiskActions = (disk) => (
    <div className="flex items-center justify-end gap-1">
      <Button type="button" size="sm" variant="secondary" title="扩容" onClick={() => openResizeDisk(disk)}>
        <Settings className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" variant="secondary" title="快照" onClick={() => snapshotDisk(disk)}>
        <Download className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" variant="danger" title="删除" onClick={() => deleteDisk(disk)}>
        <Trash className="h-4 w-4" />
      </Button>
    </div>
  );

  const renderAccountActions = (account) => (
    <div className="flex items-center justify-end gap-1">
      <Button type="button" size="sm" variant="secondary" title="验证" onClick={() => verifyAccount(account)}>
        <Shield className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" variant="secondary" title="编辑" onClick={() => openEditAccount(account)}>
        <Settings className="h-4 w-4" />
      </Button>
      <Button type="button" size="sm" variant="danger" title="删除" onClick={() => deleteAccount(account)}>
        <Trash className="h-4 w-4" />
      </Button>
    </div>
  );

  return (
    <PageStack>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs {...MODULE_TABS_PROPS} value={activeTab} onValueChange={setActiveTab} tabs={tabs} />
        {activeTab !== 'accounts' && (
          <div className="flex items-center gap-2">
            <Select size="sm" aria-label="GCP 账号" value={selectedAccountId} onValueChange={(value) => { setSelectedAccountId(value); setSelectedProjectId(''); setSelectedBucket(''); setObjects([]); }} items={[
              ...accounts.map((account) => ({ value: String(account.id), label: account.name })),
            ]} placeholder="选择账号" />
            <Select size="sm" aria-label="GCP 项目" value={selectedProjectId} onValueChange={(value) => setSelectedProjectId(value)} items={[
              ...projects.map((project) => ({ value: project.projectId, label: project.name || project.projectId })),
            ]} placeholder="选择项目" />
            <Button type="button" size="sm" variant="secondary" onClick={() => { if (activeTab === 'instances') loadInstances({ force: true }); else if (activeTab === 'disks') loadDisks({ force: true }); else if (activeTab === 'network') loadNetwork({ force: true }); else if (activeTab === 'storage') loadBuckets({ force: true }); else if (activeTab === 'billing') loadBilling({ force: true }); }} title="刷新" aria-label="刷新">
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
        )}
      </div>

      {activeTab === 'instances' && (
        <div className="flex flex-col gap-3">
          <SectionCard
            title="实例"
            icon={<Server className="h-4 w-4" />}
            actions={scopeReady && (
              <>
                <ResponsiveSearchInput value={query} onChange={setQuery} placeholder="搜索名称 / IP / 机型" className="w-56" />
                <Select size="sm" aria-label="状态筛选" value={stateFilter} onValueChange={setStateFilter} className="w-32" items={stateOptions} />
                <Button type="button" size="sm" variant="primary" onClick={openCreateInstance}>
                  <Plus className="h-4 w-4" />创建实例
                </Button>
              </>
            )}
            bodyPadding="none"
            bodyClassName="flex min-h-0 flex-1 flex-col"
          >
            {loadingProjectScope ? (
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
                <AppTable tableId="gcp-instances-loading" columns={INSTANCE_TABLE_COLUMNS}>
                  {[0, 1, 2].map((row) => (
                    <Table.Row key={row}>
                      {INSTANCE_TABLE_COLUMNS.map((col) => (
                        <Table.Cell key={col.id}><SkeletonLine className="h-4" /></Table.Cell>
                      ))}
                    </Table.Row>
                  ))}
                </AppTable>
              </DataTableFrame>
            ) : !scopeReady ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号与项目" description="选择 GCP 账号和项目后查看实例列表" className="min-h-64" />
            ) : filteredInstances.length === 0 ? (
              <EmptyState card={false} icon={Server} title="暂无实例" description="该项目下没有实例" className="min-h-64" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
                <AppTable tableId="gcp-instances" columns={INSTANCE_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>公网 IP</Table.Head>
                      <Table.Head>可用区</Table.Head>
                      <Table.Head>机型</Table.Head>
                      <Table.Head>创建时间</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {filteredInstances.map((instance) => (
                      <Table.Row key={instance.id || instance.name}>
                        <Table.Cell>
                          <div className="truncate text-sm font-semibold text-kumo-strong" title={instance.name || '-'}>{instance.name || '-'}</div>
                        </Table.Cell>
                        <Table.Cell><StatusBadge tone={getGcpStatusTone(instance.state)}>{instance.state || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell>
                          <span className="inline-flex items-center gap-1">
                            <span className="font-mono text-xs">{instance.publicIp || instance.privateIp || '-'}</span>
                            {instance.publicIp && (
                              <Button type="button" size="sm" variant="ghost" className="h-5 w-5 p-0" onClick={() => copyText(instance.publicIp)} aria-label="复制 IP">
                                <Copy className="h-3 w-3" />
                              </Button>
                            )}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{instance.zone || '-'}</Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{instance.machineType || '-'}</Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatDate(instance.creationTimestamp)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>

          {scopeReady && instances.length > 0 && (
            <SectionCard title="实例动作" icon={<Cpu className="h-4 w-4" />} bodyPadding="none">
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-instances-actions" columns={[{ id: 'name', role: 'primary' }, { id: 'state', role: 'status' }, { id: 'actions', role: 'actions-lg', width: 220 }]}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {instances.map((instance) => (
                      <Table.Row key={instance.id || instance.name}>
                        <Table.Cell><div className="truncate text-sm font-semibold text-kumo-strong">{instance.name}</div></Table.Cell>
                        <Table.Cell><StatusBadge tone={getGcpStatusTone(instance.state)}>{instance.state || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell>{renderInstanceActions(instance)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            </SectionCard>
          )}
        </div>
      )}

      {activeTab === 'disks' && (
        <SectionCard
          title="磁盘"
          icon={<HardDrive className="h-4 w-4" />}
          bodyPadding="none"
          bodyClassName="flex min-h-0 flex-1 flex-col"
        >
          {!scopeReady ? (
            <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-64" />
          ) : loadingProjectScope ? (
            <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
              <AppTable tableId="gcp-disks-loading" columns={DISK_TABLE_COLUMNS}>
                {[0, 1, 2].map((row) => (
                  <Table.Row key={row}>
                    {DISK_TABLE_COLUMNS.map((col) => (
                      <Table.Cell key={col.id}><SkeletonLine className="h-4" /></Table.Cell>
                    ))}
                  </Table.Row>
                ))}
              </AppTable>
            </DataTableFrame>
          ) : disks.length === 0 ? (
            <EmptyState card={false} icon={HardDrive} title="暂无磁盘" className="min-h-64" />
          ) : (
            <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
              <AppTable tableId="gcp-disks" columns={DISK_TABLE_COLUMNS}>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>名称</Table.Head>
                    <Table.Head>可用区</Table.Head>
                    <Table.Head>类型</Table.Head>
                    <Table.Head>大小</Table.Head>
                    <Table.Head>状态</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {disks.map((disk) => (
                    <Table.Row key={disk.id || disk.name}>
                      <Table.Cell>
                        <div className="flex items-center gap-2">
                          <div className="truncate text-sm font-semibold text-kumo-strong">{disk.name}</div>
                          <div className="flex items-center gap-1">{renderDiskActions(disk)}</div>
                        </div>
                      </Table.Cell>
                      <Table.Cell className="text-sm text-kumo-strong">{disk.zone || '-'}</Table.Cell>
                      <Table.Cell className="text-sm text-kumo-strong">{disk.type || '-'}</Table.Cell>
                      <Table.Cell>{formatGb(disk.sizeGb)}</Table.Cell>
                      <Table.Cell><StatusBadge tone={getGcpStatusTone(disk.status)}>{disk.status || '-'}</StatusBadge></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </AppTable>
            </DataTableFrame>
          )}
        </SectionCard>
      )}

      {activeTab === 'network' && (
        <div className="flex flex-col gap-3">
          <SectionCard title="防火墙规则" icon={<Shield className="h-4 w-4" />} bodyPadding="none">
            {!scopeReady ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
            ) : firewalls.length === 0 ? (
              <EmptyState card={false} icon={Shield} title="暂无防火墙规则" className="min-h-48" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-firewalls" columns={FIREWALL_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>方向</Table.Head>
                      <Table.Head>动作</Table.Head>
                      <Table.Head>优先级</Table.Head>
                      <Table.Head>网络</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {firewalls.map((rule) => (
                      <Table.Row key={rule.name}>
                        <Table.Cell><div className="truncate text-sm font-semibold text-kumo-strong">{rule.name}</div></Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{rule.direction || '-'}</Table.Cell>
                        <Table.Cell><StatusBadge tone={rule.action === 'ALLOW' ? 'success' : 'danger'}>{rule.action || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell>{rule.priority}</Table.Cell>
                        <Table.Cell className="truncate text-sm text-kumo-strong" title={rule.network}>{rule.network || '-'}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
          <SectionCard title="静态 IP" icon={<Globe className="h-4 w-4" />} bodyPadding="none">
            {!scopeReady ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
            ) : addresses.length === 0 ? (
              <EmptyState card={false} icon={Globe} title="暂无静态 IP" className="min-h-48" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-addresses" columns={ADDRESS_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>地址</Table.Head>
                      <Table.Head>区域</Table.Head>
                      <Table.Head>状态</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {addresses.map((address) => (
                      <Table.Row key={address.id || address.name}>
                        <Table.Cell><div className="truncate text-sm font-semibold text-kumo-strong">{address.name}</div></Table.Cell>
                        <Table.Cell className="font-mono text-xs">{address.address || '-'}</Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{address.region || '-'}</Table.Cell>
                        <Table.Cell><StatusBadge tone={getGcpStatusTone(address.status)}>{address.status || '-'}</StatusBadge></Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
        </div>
      )}

      {activeTab === 'storage' && (
        <div className="flex flex-col gap-3">
          <SectionCard
            title="存储桶"
            icon={<Layers className="h-4 w-4" />}
            actions={scopeReady && (
              <Button type="button" size="sm" variant="primary" onClick={() => { setBucketForm({ name: '', location: '', storageClass: 'STANDARD' }); setBucketDialogOpen(true); }}>
                <Plus className="h-4 w-4" />创建桶
              </Button>
            )}
            bodyPadding="none"
          >
            {!scopeReady ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
            ) : loadingBuckets ? (
              <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
            ) : buckets.length === 0 ? (
              <EmptyState card={false} icon={Layers} title="暂无存储桶" className="min-h-48" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-buckets" columns={BUCKET_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>位置</Table.Head>
                      <Table.Head>存储类别</Table.Head>
                      <Table.Head>创建时间</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {buckets.map((bucket) => (
                      <Table.Row
                        key={bucket.name}
                        variant={selectedBucket === bucket.name ? 'selected' : 'default'}
                        className="cursor-pointer"
                        onClick={() => { setSelectedBucket(bucket.name); setObjects([]); loadObjects({ force: true, silent: true }); }}
                      >
                        <Table.Cell>
                          <div className="flex items-center justify-between gap-2">
                            <div className="truncate text-sm font-semibold text-kumo-strong">{bucket.name}</div>
                            <Button type="button" size="sm" variant="danger" className="h-6 w-6 p-0" onClick={(event) => { event.stopPropagation(); deleteBucket(bucket); }} aria-label="删除桶">
                              <Trash className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{bucket.location || '-'}</Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{bucket.storageClass || '-'}</Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatDate(bucket.timeCreated)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>

          {selectedBucket && (
            <SectionCard
              title={`对象 · ${selectedBucket}`}
              icon={<FolderOpen className="h-4 w-4" />}
              bodyPadding="none"
              actions={(
                <>
                  <input
                    ref={objectFileInputRef}
                    type="file"
                    className="hidden"
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      if (file) uploadObject(file);
                    }}
                  />
                  <Button type="button" size="sm" variant="primary" loading={uploadingObject} onClick={() => objectFileInputRef.current?.click()}>
                    <Upload className="h-4 w-4" />上传对象
                  </Button>
                </>
              )}
            >
              {loadingObjects ? (
                <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
              ) : objects.length === 0 ? (
                <EmptyState card={false} icon={FolderOpen} title="桶内暂无对象" className="min-h-40" />
              ) : (
                <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                  <AppTable tableId="gcp-objects" columns={[{ id: 'name', role: 'primary' }, { id: 'size', role: 'number', width: 120 }, { id: 'type', role: 'meta', grow: 1, minWidth: 160 }, { id: 'updated', role: 'datetime' }, { id: 'actions', role: 'actions-sm', width: 90 }]}>
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>名称</Table.Head>
                        <Table.Head>大小</Table.Head>
                        <Table.Head>类型</Table.Head>
                        <Table.Head>更新时间</Table.Head>
                        <Table.Head>操作</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {objects.map((object) => (
                        <Table.Row key={object.name}>
                          <Table.Cell><div className="truncate text-sm font-semibold text-kumo-strong">{object.name}</div></Table.Cell>
                          <Table.Cell>{formatSize(object.size)}</Table.Cell>
                          <Table.Cell className="truncate text-sm text-kumo-strong">{object.contentType || '-'}</Table.Cell>
                          <Table.Cell className="text-xs text-kumo-subtle">{formatDate(object.updated || object.timeCreated)}</Table.Cell>
                          <Table.Cell>
                            <div className="flex items-center justify-end gap-1">
                              <Button type="button" size="sm" variant="ghost" className="h-6 w-6 p-0" title="下载" onClick={() => downloadObject(object)} aria-label="下载对象">
                                <Download className="h-3.5 w-3.5" />
                              </Button>
                              <Button type="button" size="sm" variant="ghost" className="h-6 w-6 p-0" title="复制名称" onClick={() => copyText(object.name)} aria-label="复制名称">
                                <Copy className="h-3.5 w-3.5" />
                              </Button>
                              <Button type="button" size="sm" variant="danger" className="h-6 w-6 p-0" title="删除" onClick={() => deleteObject(object)} aria-label="删除对象">
                                <Trash className="h-3.5 w-3.5" />
                              </Button>
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </SectionCard>
          )}
        </div>
      )}

      {activeTab === 'billing' && (
        <div className="flex flex-col gap-3">
          <SectionCard title="计费账号" icon={<PieChart className="h-4 w-4" />} bodyPadding="none">
            {!selectedAccountId ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号" className="min-h-48" />
            ) : loadingBilling ? (
              <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
            ) : billingAccounts.length === 0 ? (
              <EmptyState card={false} icon={PieChart} title="没有可访问的计费账号" description="SA 可能需要 billing 相关权限" className="min-h-48" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-billing-accounts" columns={[{ id: 'name', role: 'primary' }, { id: 'displayName', role: 'meta', grow: 1, minWidth: 200 }, { id: 'open', role: 'status' }]}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>计费账号 ID</Table.Head>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {billingAccounts.map((account) => (
                      <Table.Row key={account.name}>
                        <Table.Cell className="font-mono text-xs">{account.name}</Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{account.displayName || '-'}</Table.Cell>
                        <Table.Cell><StatusBadge tone={account.open ? 'success' : 'neutral'}>{account.open ? '启用' : '停用'}</StatusBadge></Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
          <SectionCard title="项目计费信息" icon={<PieChart className="h-4 w-4" />} bodyPadding="md">
            {!selectedAccountId || !selectedProjectId ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-32" />
            ) : loadingBilling ? (
              <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
            ) : !billingInfo ? (
              <EmptyState card={false} icon={PieChart} title="暂无项目计费信息" description="SA 需要 billing 相关权限，或该项目未关联结算账号" className="min-h-32" />
            ) : (
              <KeyValueGrid
                columns={2}
                items={[
                  {
                    label: '结算账号',
                    value: <span className="font-mono text-xs">{billingInfo.billingAccountName || '-'}</span>,
                  },
                  {
                    label: '计费状态',
                    value: <StatusBadge tone={billingInfo.billingEnabled ? 'success' : 'neutral'}>{billingInfo.billingEnabled ? '已启用' : '未启用'}</StatusBadge>,
                  },
                ]}
              />
            )}
          </SectionCard>
          <SectionCard title="预算" icon={<PieChart className="h-4 w-4" />} bodyPadding="none">
            {budgets.length === 0 ? (
              <EmptyState card={false} icon={PieChart} title="暂无预算" className="min-h-40" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="gcp-budgets" columns={[{ id: 'displayName', role: 'primary' }, { id: 'amount', role: 'meta', width: 120 }, { id: 'thresholds', role: 'meta', grow: 1, minWidth: 200 }]}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>预算名称</Table.Head>
                      <Table.Head>金额</Table.Head>
                      <Table.Head>阈值</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {budgets.map((budget) => (
                      <Table.Row key={budget.name}>
                        <Table.Cell><div className="truncate text-sm font-semibold text-kumo-strong">{budget.displayName || budget.name}</div></Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{budget.amount ? `${budget.amount} ${budget.currencyCode || ''}`.trim() : '-'}</Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">
                          {(budget.thresholdRules || []).map((rule) => `${rule.thresholdPercent}%`).join(' / ') || '-'}
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
          <SectionCard
            title="模型用量"
            icon={<Cpu className="h-4 w-4" />}
            actions={scopeReady && (
              <Select size="sm" aria-label="用量时间范围" value={String(modelUsageDays)} onValueChange={(value) => setModelUsageDays(Number(value) || 30)} className="w-32" items={[
                { value: '7', label: '近 7 天' },
                { value: '30', label: '近 30 天' },
                { value: '90', label: '近 90 天' },
              ]} />
            )}
            bodyPadding="none"
          >
            {!scopeReady ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
            ) : loadingModelUsage ? (
              <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
            ) : !modelUsage || (modelUsage.total ?? 0) === 0 ? (
              <EmptyState card={false} icon={Cpu} title="暂无模型调用量" description="SA 需要 roles/monitoring.viewer 权限；若无调用则此处为空" className="min-h-48" />
            ) : (
              <div className="flex flex-col gap-3 p-3">
                <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3">
                  <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
                    <span className="text-[10px] text-kumo-subtle select-none">总调用量</span>
                    <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{formatCount(modelUsage.total)}</span>
                  </div>
                  <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
                    <span className="text-[10px] text-kumo-subtle select-none">日均</span>
                    <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{modelUsage.daily?.length ? formatCount(Math.round(modelUsage.total / Math.max(modelUsage.daily.length, 1))) : '-'}</span>
                  </div>
                  <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
                    <span className="text-[10px] text-kumo-subtle select-none">模型数</span>
                    <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{modelUsage.byModel?.length ?? 0}</span>
                  </div>
                </div>
                <div className="min-h-0 w-full rounded-md border border-kumo-interact/90 bg-kumo-base" style={{ height: 168 }}>
                  {modelUsageChartData.length === 0 ? (
                    <div className="flex h-full items-center justify-center text-sm text-kumo-subtle">暂无数据</div>
                  ) : (
                    <Chart echarts={siteFontEcharts} isDarkMode={isDarkMode} options={modelUsageChartOptions ?? {}} height={168} />
                  )}
                </div>
                {(modelUsage.byModel ?? []).length > 0 && (
                  <DataTableFrame variant="card" density="dense" className="overflow-auto">
                    <AppTable tableId="gcp-model-usage" columns={[{ id: 'model', role: 'primary' }, { id: 'count', role: 'number', width: 140 }]}>
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head>模型</Table.Head>
                          <Table.Head>调用量</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {(modelUsage.byModel ?? []).map((group) => (
                          <Table.Row key={group.model}>
                            <Table.Cell><div className="truncate font-mono text-xs text-kumo-strong">{group.model}</div></Table.Cell>
                            <Table.Cell className="tabular-nums">{formatCount(group.count)}</Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </AppTable>
                  </DataTableFrame>
                )}
              </div>
            )}
          </SectionCard>
        </div>
      )}

      {activeTab === 'accounts' && (
        <SectionCard
          title="GCP 账号"
          icon={<Key className="h-4 w-4" />}
          actions={(
            <Button type="button" size="sm" variant="primary" onClick={openCreateAccount}>
              <Plus className="h-4 w-4" />新增账号
            </Button>
          )}
          bodyPadding="none"
        >
          {loadingAccounts ? (
            <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
              <AppTable tableId="gcp-accounts-loading" columns={ACCOUNT_TABLE_COLUMNS}>
                {[0, 1, 2].map((row) => (
                  <Table.Row key={row}>
                    {ACCOUNT_TABLE_COLUMNS.map((col) => (
                      <Table.Cell key={col.id}><SkeletonLine className="h-4" /></Table.Cell>
                    ))}
                  </Table.Row>
                ))}
              </AppTable>
            </DataTableFrame>
          ) : accounts.length === 0 ? (
            <EmptyState card={false} icon={Key} title="暂无 GCP 账号" description="新增账号并粘贴 Service Account JSON 开始使用" className="min-h-64" />
          ) : (
            <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
              <AppTable tableId="gcp-accounts" columns={ACCOUNT_TABLE_COLUMNS}>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>名称</Table.Head>
                    <Table.Head>Service Account</Table.Head>
                    <Table.Head>默认项目</Table.Head>
                    <Table.Head>验证状态</Table.Head>
                    <Table.Head>操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accounts.map((account) => (
                    <Table.Row key={account.id}>
                      <Table.Cell><div className="truncate text-sm font-semibold text-kumo-strong">{account.name}</div></Table.Cell>
                      <Table.Cell className="truncate font-mono text-xs" title={account.clientEmail}>{account.clientEmail || '-'}</Table.Cell>
                      <Table.Cell className="text-sm text-kumo-strong">{account.defaultProjectId || '-'}</Table.Cell>
                      <Table.Cell>
                        <StatusBadge tone={getGcpStatusTone(account.lastVerifyStatus)}>{getVerifyStatusLabel(account.lastVerifyStatus)}</StatusBadge>
                      </Table.Cell>
                      <Table.Cell>{renderAccountActions(account)}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </AppTable>
            </DataTableFrame>
          )}
        </SectionCard>
      )}

      <Dialog.Root open={accountDialogOpen} onOpenChange={setAccountDialogOpen}>
        <Dialog className="@container !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{editingAccount ? '编辑 GCP 账号' : '新增 GCP 账号'}</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">使用 Service Account JSON 接入 GCP，支持粘贴或导入文件。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">账号名称 *</span>
            <Input size="md" value={accountForm.name} onChange={(event) => setAccountForm({ ...accountForm, name: event.target.value })} placeholder="例如：生产环境" />
          </label>
          {!editingAccount && (
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">Service Account JSON *</span>
              <input
                ref={accountFileInputRef}
                type="file"
                accept=".json,application/json"
                className="hidden"
                onChange={importAccountJsonFile}
              />
              <div className="flex justify-end">
                <Button type="button" size="sm" variant="secondary" onClick={() => accountFileInputRef.current?.click()}>
                  <Upload className="h-4 w-4" />导入 JSON 文件
                </Button>
              </div>
              <CodeEditor
                label="Service Account JSON"
                fileName="service-account.json"
                language="json"
                minHeight="6rem"
                maxHeight="16rem"
                value={accountForm.serviceAccountJson}
                onChange={(serviceAccountJson) => setAccountForm({ ...accountForm, serviceAccountJson })}
                placeholder="粘贴完整的 Service Account JSON 密钥文件内容，或点击上方按钮导入文件"
              />
            </label>
          )}
          {editingAccount && (
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">Service Account JSON（留空不修改）</span>
              <CodeEditor
                label="Service Account JSON"
                fileName="service-account.json"
                language="json"
                minHeight="6rem"
                maxHeight="16rem"
                value={accountForm.serviceAccountJson}
                onChange={(serviceAccountJson) => setAccountForm({ ...accountForm, serviceAccountJson })}
                placeholder="不修改则留空"
              />
            </label>
          )}
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">默认项目</span>
            <Input size="md" value={accountForm.defaultProjectId} onChange={(event) => setAccountForm({ ...accountForm, defaultProjectId: event.target.value })} placeholder="留空则取 JSON 内 project_id" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">备注</span>
            <Input size="md" value={accountForm.description} onChange={(event) => setAccountForm({ ...accountForm, description: event.target.value })} />
          </label>
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" size="sm" variant="secondary" onClick={() => setAccountDialogOpen(false)}>取消</Button>
            <Button type="button" size="sm" variant="primary" loading={submittingAccount} onClick={submitAccount}>
              {editingAccount ? '保存' : '新增'}
            </Button>
          </div>
        </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <Dialog className="@container !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">创建实例</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">GCP 实例创建为异步操作，发票将按所选区与机型计费。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">名称 *</span>
            <Input size="md" value={createForm.name} onChange={(event) => setCreateForm({ ...createForm, name: event.target.value })} placeholder="实例名称" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">可用区 *</span>
            <Select size="base" value={createForm.zone} onValueChange={(value) => setCreateForm({ ...createForm, zone: value })} items={[
                { value: '', label: '选择可用区' },
                ...zones.map((zone) => ({ value: zone.name, label: zone.name })),
              ]} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">机型 *</span>
            <Select size="base" value={createForm.machineType} onValueChange={(value) => setCreateForm({ ...createForm, machineType: value })} items={[
              { value: '', label: '选择机型' },
              ...machineTypes.map((mt) => ({ value: mt.name, label: `${mt.name}（${mt.guestCpus} vCPU / ${formatMemoryGb(mt.memoryMb)}）` })),
            ]} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">镜像</span>
            <Select size="base" value={createForm.image} onValueChange={(value) => setCreateForm({ ...createForm, image: value })} items={[
              { value: '', label: '使用默认镜像' },
              ...images.map((image) => ({ value: image.name, label: image.name })),
            ]} />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">启动盘大小（GB）</span>
              <Input size="md" type="number" value={createForm.bootDiskSizeGb} onChange={(event) => setCreateForm({ ...createForm, bootDiskSizeGb: event.target.value })} />
            </label>
          </div>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">子网</span>
            <Select size="base" value={createForm.subnetwork} onValueChange={(value) => setCreateForm({ ...createForm, subnetwork: value })} items={[
              { value: '', label: '使用默认网络' },
              ...subnetworks.map((sub) => ({ value: sub.name, label: `${sub.name}（${sub.ipCidrRange}）` })),
            ]} />
          </label>
          {loadingCreateOptions && <div className="text-xs text-kumo-subtle">正在加载选项…</div>}
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" size="sm" variant="secondary" onClick={() => setCreateDialogOpen(false)}>取消</Button>
            <Button type="button" size="sm" variant="primary" loading={submittingCreate} onClick={submitCreate}>创建</Button>
          </div>
        </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={resizeDialogOpen} onOpenChange={setResizeDialogOpen}>
        <Dialog className="@container !w-[min(36rem,calc(100vw-2rem))] !max-w-[min(36rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">磁盘扩容</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">磁盘容量只增不减，扩容为异步操作。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <p className="text-sm text-kumo-subtle">磁盘「{resizeTarget?.name}」当前大小为 {formatGb(resizeTarget?.sizeGb)}。</p>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">目标大小（GB）</span>
            <Input size="md" type="number" value={resizeSize} onChange={(event) => setResizeSize(event.target.value)} />
          </label>
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" size="sm" variant="secondary" onClick={() => setResizeDialogOpen(false)}>取消</Button>
            <Button type="button" size="sm" variant="primary" onClick={submitResize}>扩容</Button>
          </div>
        </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={bucketDialogOpen} onOpenChange={setBucketDialogOpen}>
        <Dialog className="@container !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">创建存储桶</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">存储桶名称全局唯一，创建后不可重名。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">名称 *</span>
            <Input size="md" value={bucketForm.name} onChange={(event) => setBucketForm({ ...bucketForm, name: event.target.value })} placeholder="全局唯一存储桶名称" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">位置</span>
            <Input size="md" value={bucketForm.location} onChange={(event) => setBucketForm({ ...bucketForm, location: event.target.value })} placeholder="例如 us-central1" />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-sm text-kumo-subtle">存储类别</span>
            <Select size="base" value={bucketForm.storageClass} onValueChange={(value) => setBucketForm({ ...bucketForm, storageClass: value })} items={[
              { value: 'STANDARD', label: '标准（STANDARD）' },
              { value: 'NEARLINE', label: '近线（NEARLINE）' },
              { value: 'COLDLINE', label: '冷线（COLDLINE）' },
              { value: 'ARCHIVE', label: '归档（ARCHIVE）' },
            ]} />
          </label>
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" size="sm" variant="secondary" onClick={() => setBucketDialogOpen(false)}>取消</Button>
            <Button type="button" size="sm" variant="primary" loading={submittingBucket} onClick={submitBucket}>创建</Button>
          </div>
        </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default GcpPage;