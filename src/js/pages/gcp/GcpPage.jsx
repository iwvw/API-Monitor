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
import { Select } from '@cloudflare/kumo/components/select';
import { Tabs } from '@cloudflare/kumo';
import { createSiteFontEcharts } from '../../chartFont.js';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { PageStack, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { kumoHex, formatCompact } from '../openai/utils.js';
import useStore from '../../store.js';
import { RefreshCw } from '../../components/Icons.jsx';
import {
  CACHE_TTL_MS,
  MODEL_USAGE_TTL_MS,
  emptyAccountForm,
  emptyBucketForm,
  emptyCreateForm,
  emptyFirewallForm,
} from './constants.js';
import { tabs } from './tabs.jsx';
import {
  formatModelUsageAxis,
  getAuthHeaders,
  unwrap,
} from './utils.jsx';
import InstancesPanel from './InstancesPanel.jsx';
import DisksPanel from './DisksPanel.jsx';
import NetworkPanel from './NetworkPanel.jsx';
import StoragePanel from './StoragePanel.jsx';
import BillingPanel from './BillingPanel.jsx';
import AccountsPanel from './AccountsPanel.jsx';
import DetailDialog from './DetailDialog.jsx';
import AccountDialog from './AccountDialog.jsx';
import CreateInstanceDialog from './CreateInstanceDialog.jsx';
import ResizeDialog from './ResizeDialog.jsx';
import BucketDialog from './BucketDialog.jsx';
import FirewallDialog from './FirewallDialog.jsx';

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
  const machineTypesSeq = useRef(0);
  const [subnetworks, setSubnetworks] = useState([]);
  const [images, setImages] = useState([]);
  const [loadingCreateOptions, setLoadingCreateOptions] = useState(false);
  const [loadingMachineTypes, setLoadingMachineTypes] = useState(false);
  const [submittingCreate, setSubmittingCreate] = useState(false);
  const [resizeDialogOpen, setResizeDialogOpen] = useState(false);
  const [resizeTarget, setResizeTarget] = useState(null);
  const [resizeSize, setResizeSize] = useState('');
  const [bucketDialogOpen, setBucketDialogOpen] = useState(false);
  const [bucketForm, setBucketForm] = useState(emptyBucketForm);
  const [submittingBucket, setSubmittingBucket] = useState(false);
  const [firewallDialogOpen, setFirewallDialogOpen] = useState(false);
  const [editingFirewall, setEditingFirewall] = useState(null);
  const [firewallForm, setFirewallForm] = useState(emptyFirewallForm);
  const [submittingFirewall, setSubmittingFirewall] = useState(false);
  const [uploadingObject, setUploadingObject] = useState(false);
  const [selectedDetail, setSelectedDetail] = useState(null);
  const [selectedObjects, setSelectedObjects] = useState(new Set());
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
    const CACHE_MODEL_USAGE_MS = MODEL_USAGE_TTL_MS;
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
    setMachineTypes([]);
    setCreateDialogOpen(true);
    setLoadingCreateOptions(true);
    try {
      const [zonesResult, imageResult, subResult] = await Promise.all([
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/zones`),
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/images`),
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/subnetworks`),
      ]);
      setZones(unwrap(zonesResult).zones || []);
      setImages(unwrap(imageResult).images || []);
      setSubnetworks(unwrap(subResult).subnetworks || []);
    } catch (error) {
      toast.error(error.message || '加载创建选项失败');
    } finally {
      setLoadingCreateOptions(false);
    }
  };

  const loadMachineTypesForZone = async (zone) => {
    if (!zone) {
      setMachineTypes([]);
      return;
    }
    const seq = ++machineTypesSeq.current;
    setLoadingMachineTypes(true);
    try {
      const machineResult = await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/machine-types?zone=${encodeURIComponent(zone)}`);
      if (seq !== machineTypesSeq.current) return;
      setMachineTypes(unwrap(machineResult).machineTypes || []);
    } catch (error) {
      if (seq !== machineTypesSeq.current) return;
      setMachineTypes([]);
      toast.error(error.message || '加载机型失败');
    } finally {
      if (seq === machineTypesSeq.current) setLoadingMachineTypes(false);
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

  const openFirewallDialog = (fw = null) => {
    setEditingFirewall(fw);
    setFirewallForm(fw ? {
      name: fw.name || '',
      description: '',
      direction: fw.direction || 'INGRESS',
      priority: fw.priority || 1000,
      action: fw.allowed?.length ? 'allow' : 'deny',
      sourceRanges: (fw.sourceRanges || []).join(', '),
      destinationRanges: (fw.destinationRanges || []).join(', '),
      protocol: fw.allowed?.[0]?.ipProtocol || fw.denied?.[0]?.ipProtocol || 'tcp',
      ports: fw.allowed?.[0]?.ports?.join(', ') || fw.denied?.[0]?.ports?.join(', ') || '',
      network: fw.network || '',
      disabled: false,
    } : { ...emptyFirewallForm });
    setFirewallDialogOpen(true);
  };

  const submitFirewall = async () => {
    if (!firewallForm.name.trim()) { toast.error('请填写规则名称'); return; }
    setSubmittingFirewall(true);
    try {
      const rules = [{
        ipProtocol: firewallForm.protocol.trim() || 'tcp',
        ...(firewallForm.ports.trim() ? { ports: firewallForm.ports.split(',').map((p) => p.trim()).filter(Boolean) } : {}),
      }];
      const payload = {
        name: firewallForm.name.trim(),
        ...(firewallForm.description.trim() ? { description: firewallForm.description.trim() } : {}),
        ...(firewallForm.network.trim() ? { network: firewallForm.network.trim() } : {}),
        direction: firewallForm.direction,
        priority: Number(firewallForm.priority) || 1000,
        ...(firewallForm.sourceRanges.trim() ? { sourceRanges: firewallForm.sourceRanges.split(',').map((s) => s.trim()).filter(Boolean) } : {}),
        ...(firewallForm.destinationRanges.trim() ? { destinationRanges: firewallForm.destinationRanges.split(',').map((s) => s.trim()).filter(Boolean) } : {}),
        ...(firewallForm.action === 'allow' ? { allowed: rules } : { denied: rules }),
        disabled: Boolean(firewallForm.disabled),
      };
      const basePath = `/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}`;
      if (editingFirewall) {
        await apiFetch(`${basePath}/firewalls/${encodeURIComponent(editingFirewall.name)}`, { method: 'PATCH', body: JSON.stringify(payload) });
        toast.success('防火墙规则已更新');
      } else {
        await apiFetch(`${basePath}/firewalls`, { method: 'POST', body: JSON.stringify(payload) });
        toast.success('防火墙规则已创建');
      }
      setFirewallDialogOpen(false);
      cacheRef.current.firewalls.delete(scopeCacheKey());
      setTimeout(() => loadNetwork({ force: true }), 1500);
    } catch (error) {
      toast.error(error.message || '保存防火墙规则失败');
    } finally {
      setSubmittingFirewall(false);
    }
  };

  const deleteFirewall = async (fw) => {
    const ok = await dialog.deleteResource({
      title: '删除防火墙规则',
      message: `确定删除防火墙规则「${fw.name}」吗？`,
      confirmLabel: '删除规则',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/gcp/accounts/${selectedAccountId}/projects/${selectedProjectId}/firewalls/${encodeURIComponent(fw.name)}`, { method: 'DELETE' });
      toast.success('防火墙规则已删除');
      cacheRef.current.firewalls.delete(scopeCacheKey());
      setTimeout(() => loadNetwork({ force: true }), 1500);
    } catch (error) {
      toast.error(error.message || '删除防火墙规则失败');
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

  const toggleObjectSelection = (name) => {
    setSelectedObjects((current) => {
      const next = new Set(current);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const toggleAllObjects = () => {
    setSelectedObjects((current) => {
      if (objects.length > 0 && current.size === objects.length) return new Set();
      return new Set(objects.map((object) => object.name));
    });
  };

  const deleteSelectedObjects = async () => {
    if (selectedObjects.size === 0) return;
    const ok = await dialog.deleteResource({
      title: '批量删除对象',
      message: `确定删除选中的 ${selectedObjects.size} 个对象吗？`,
      confirmLabel: '删除选中',
    });
    if (!ok) return;
    try {
      await Promise.all(Array.from(selectedObjects).map((name) =>
        apiFetch(`/api/gcp/accounts/${selectedAccountId}/buckets/${encodeURIComponent(selectedBucket)}/objects/${encodeURIComponent(name)}`, { method: 'DELETE' })
      ));
      toast.success(`已删除 ${selectedObjects.size} 个对象`);
      setSelectedObjects(new Set());
      cacheRef.current.objects.delete(`${selectedAccountId}/${selectedBucket}`);
      loadObjects({ force: true, silent: true });
    } catch (error) {
      toast.error(error.message || '批量删除对象失败');
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

  const closeDetail = () => setSelectedDetail(null);
  const openDetail = (kind, data) => setSelectedDetail({ kind, data });
  const openBucketDialog = () => {
    setBucketForm(emptyBucketForm);
    setBucketDialogOpen(true);
  };
  const selectBucket = (name) => {
    setSelectedBucket(name);
    setObjects([]);
    setSelectedObjects(new Set());
    loadObjects({ force: true, silent: true });
  };

  return (
    <PageStack>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs {...MODULE_TABS_PROPS} value={activeTab} onValueChange={setActiveTab} tabs={tabs} />
        {activeTab !== 'accounts' && (
          <div className="flex items-center gap-2">
            <Select alignItemWithTrigger size="sm" aria-label="GCP 账号" value={selectedAccountId} onValueChange={(value) => { setSelectedAccountId(value); setSelectedProjectId(''); setSelectedBucket(''); setObjects([]); setSelectedObjects(new Set()); }} items={[
              ...accounts.map((account) => ({ value: String(account.id), label: account.name })),
            ]} placeholder="选择账号" />
            <Select alignItemWithTrigger size="sm" aria-label="GCP 项目" value={selectedProjectId} onValueChange={(value) => setSelectedProjectId(value)} items={[
              ...projects.map((project) => ({ value: project.projectId, label: project.name || project.projectId })),
            ]} placeholder="选择项目" />
            <Button type="button" size="sm" variant="secondary" onClick={() => { if (activeTab === 'instances') loadInstances({ force: true }); else if (activeTab === 'disks') loadDisks({ force: true }); else if (activeTab === 'network') loadNetwork({ force: true }); else if (activeTab === 'storage') loadBuckets({ force: true }); else if (activeTab === 'billing') loadBilling({ force: true }); }} title="刷新" aria-label="刷新">
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
        )}
      </div>

      {activeTab === 'instances' && (
        <InstancesPanel
          scopeReady={scopeReady}
          loadingProjectScope={loadingProjectScope}
          filteredInstances={filteredInstances}
          query={query}
          onQueryChange={setQuery}
          stateFilter={stateFilter}
          onStateFilterChange={setStateFilter}
          onCreateInstance={openCreateInstance}
          onOpenDetail={openDetail}
          onCopy={copyText}
          onRunAction={runInstanceAction}
        />
      )}

      {activeTab === 'disks' && (
        <DisksPanel
          scopeReady={scopeReady}
          loadingProjectScope={loadingProjectScope}
          disks={disks}
          onOpenDetail={openDetail}
          onResize={openResizeDisk}
          onSnapshot={snapshotDisk}
          onDelete={deleteDisk}
        />
      )}

      {activeTab === 'network' && (
        <NetworkPanel
          scopeReady={scopeReady}
          loadingProjectScope={loadingProjectScope}
          firewalls={firewalls}
          addresses={addresses}
          onOpenFirewallDialog={openFirewallDialog}
          onOpenDetail={openDetail}
          onEditFirewall={openFirewallDialog}
          onDeleteFirewall={deleteFirewall}
        />
      )}

      {activeTab === 'storage' && (
        <StoragePanel
          scopeReady={scopeReady}
          loadingBuckets={loadingBuckets}
          loadingObjects={loadingObjects}
          buckets={buckets}
          objects={objects}
          selectedBucket={selectedBucket}
          selectedObjects={selectedObjects}
          uploadingObject={uploadingObject}
          objectFileInputRef={objectFileInputRef}
          onOpenBucketDialog={openBucketDialog}
          onSelectBucket={selectBucket}
          onDeleteBucket={deleteBucket}
          onUploadObject={uploadObject}
          onDeleteSelectedObjects={deleteSelectedObjects}
          onToggleAllObjects={toggleAllObjects}
          onToggleObjectSelection={toggleObjectSelection}
          onOpenDetail={openDetail}
          onDownloadObject={downloadObject}
          onDeleteObject={deleteObject}
          onCopy={copyText}
        />
      )}

      {activeTab === 'billing' && (
        <BillingPanel
          scopeReady={scopeReady}
          selectedAccountId={selectedAccountId}
          selectedProjectId={selectedProjectId}
          loadingBilling={loadingBilling}
          billingAccounts={billingAccounts}
          billingInfo={billingInfo}
          budgets={budgets}
          modelUsage={modelUsage}
          loadingModelUsage={loadingModelUsage}
          modelUsageDays={modelUsageDays}
          onModelUsageDaysChange={(value) => setModelUsageDays(Number(value) || 30)}
          modelUsageChartData={modelUsageChartData}
          modelUsageChartOptions={modelUsageChartOptions}
          siteFontEcharts={siteFontEcharts}
          isDarkMode={isDarkMode}
          onOpenDetail={openDetail}
          onCopy={copyText}
        />
      )}

      {activeTab === 'accounts' && (
        <AccountsPanel
          loadingAccounts={loadingAccounts}
          accounts={accounts}
          onOpenCreateAccount={openCreateAccount}
          onOpenDetail={openDetail}
          onVerifyAccount={verifyAccount}
          onEditAccount={openEditAccount}
          onDeleteAccount={deleteAccount}
        />
      )}

      <AccountDialog
        open={accountDialogOpen}
        onOpenChange={setAccountDialogOpen}
        editingAccount={editingAccount}
        accountForm={accountForm}
        setAccountForm={setAccountForm}
        accountFileInputRef={accountFileInputRef}
        onImportAccountJsonFile={importAccountJsonFile}
        onSubmit={submitAccount}
        submittingAccount={submittingAccount}
      />

      <CreateInstanceDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        createForm={createForm}
        setCreateForm={setCreateForm}
        zones={zones}
        machineTypes={machineTypes}
        images={images}
        subnetworks={subnetworks}
        loadingCreateOptions={loadingCreateOptions}
        loadingMachineTypes={loadingMachineTypes}
        submittingCreate={submittingCreate}
        onZoneChange={(value) => { setCreateForm((prev) => ({ ...prev, zone: value, machineType: '' })); loadMachineTypesForZone(value); }}
        onSubmit={submitCreate}
      />

      <ResizeDialog
        open={resizeDialogOpen}
        onOpenChange={setResizeDialogOpen}
        resizeTarget={resizeTarget}
        resizeSize={resizeSize}
        setResizeSize={setResizeSize}
        onSubmit={submitResize}
      />

      <BucketDialog
        open={bucketDialogOpen}
        onOpenChange={setBucketDialogOpen}
        bucketForm={bucketForm}
        setBucketForm={setBucketForm}
        submittingBucket={submittingBucket}
        onSubmit={submitBucket}
      />

      <FirewallDialog
        open={firewallDialogOpen}
        onOpenChange={setFirewallDialogOpen}
        editingFirewall={editingFirewall}
        firewallForm={firewallForm}
        setFirewallForm={setFirewallForm}
        submittingFirewall={submittingFirewall}
        onSubmit={submitFirewall}
      />

      <DetailDialog
        selectedDetail={selectedDetail}
        onClose={closeDetail}
        onCopy={copyText}
      />
    </PageStack>
  );
}

export default GcpPage;
