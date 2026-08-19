import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { DropdownMenu, Tabs, Toolbar } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { AppTable, DataTableFrame, EmptyState, InsetPanel, KeyValueGrid, PageStack, ResponsiveSearchInput, SectionCard, StatusBadge, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import {
  Cloud,
  Copy,
  Cpu,
  Download,
  Edit,
  Globe,
  HardDrive,
  Info,
  Key,
  Layers,
  MoreVertical,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Server,
  Settings,
  Shield,
  Square,
  Terminal,
  Trash,
  Upload,
} from '../components/Icons.jsx';

const emptyAccountForm = {
  name: '',
  tenancyOcid: '',
  userOcid: '',
  fingerprint: '',
  region: 'ap-tokyo-1',
  privateKeyPem: '',
  passphrase: '',
  defaultCompartmentId: '',
  description: '',
};

const emptyResizeForm = {
  shape: '',
  ocpuCount: '',
  memoryGb: '',
  baselineOcpuUtilization: '',
  avoidDowntime: false,
};

const tabs = [
  { value: 'instances', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />实例</span> },
  { value: 'network', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />网络</span> },
  { value: 'storage', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="h-3.5 w-3.5" />卷</span> },
  { value: 'console', label: <span className="inline-flex items-center gap-1.5"><Terminal className="h-3.5 w-3.5" />控制台</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />账号管理</span> },
];

const stateOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'RUNNING', label: '运行中' },
  { value: 'STOPPED', label: '已停止' },
  { value: 'PROVISIONING', label: '创建中' },
  { value: 'TERMINATED', label: '已终止' },
];

const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier' },
  { id: 'shape', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'ocpu', role: 'number', width: 88 },
  { id: 'memory', role: 'number', width: 88 },
  { id: 'createdAt', role: 'datetime' },
];
const ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'compartment', role: 'identifier' },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];
const CACHE_TTL_MS = {
  accounts: 30_000,
  compartments: 5 * 60_000,
  instances: 45_000,
  detail: 45_000,
  shapes: 5 * 60_000,
};

const ADVANCED_INSTANCE_ACTIONS = [
  { value: 'SOFTSTOP', label: '软停止' },
  { value: 'RESET', label: '强制重启' },
  { value: 'SOFTRESET', label: '软重启' },
  { value: 'REBOOTMIGRATE', label: '迁移重启' },
];

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

const unwrap = (result) => result?.data ?? result ?? {};

const parseOciConfig = (text) => text
  .split(/\r?\n/)
  .reduce((values, rawLine) => {
    const line = rawLine.trim();
    if (!line || line.startsWith('[') || line.startsWith('#') || line.startsWith(';')) return values;
    const separator = line.indexOf('=');
    if (separator < 0) return values;
    const key = line.slice(0, separator).trim().toLowerCase();
    const value = line.slice(separator + 1).replace(/\s+#.*$/, '').trim();
    if (key) values[key] = value;
    return values;
  }, {});

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

function parseImportedAccounts(text) {
  const parsed = JSON.parse(text);
  if (Array.isArray(parsed)) return parsed;
  if (Array.isArray(parsed?.accounts)) return parsed.accounts;
  throw new Error('导入内容必须是账号数组或包含 accounts 的对象');
}

function getOciStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['RUNNING', 'AVAILABLE', 'ACTIVE', 'SUCCESS', 'VALID'].includes(normalized)) return 'success';
  if (['PROVISIONING', 'STARTING', 'STOPPING', 'UPDATING', 'PENDING', 'CREATING'].includes(normalized)) return 'info';
  if (['STOPPED', 'INACTIVE', 'TERMINATED', 'UNKNOWN', 'UNVERIFIED'].includes(normalized)) return 'neutral';
  if (['FAILED', 'FAILURE', 'ERROR', 'INVALID'].includes(normalized)) return 'danger';
  return 'neutral';
}

function getVerifyStatusLabel(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['SUCCESS', 'VALID'].includes(normalized)) return '已验证';
  if (['FAILED', 'FAILURE', 'ERROR', 'INVALID'].includes(normalized)) return '失败';
  if (['UNKNOWN', 'UNVERIFIED', ''].includes(normalized)) return '未验证';
  return status || '未验证';
}

function parseNumberInput(value) {
  const text = String(value || '').trim();
  if (!text) return null;
  const numeric = Number(text);
  return Number.isFinite(numeric) ? numeric : Number.NaN;
}

function clampResizeValue(value, min, max, fallback) {
  if (!Number.isFinite(value)) return fallback;
  if (Number.isFinite(min) && value < min) return min;
  if (Number.isFinite(max) && value > max) return max;
  return value;
}

function formatShapeSummary(shape) {
  if (!shape) return '-';
  if (shape.isFlexible) {
    const ocpu = shape.ocpuOptions?.min && shape.ocpuOptions?.max
      ? `${formatInstanceMetric(shape.ocpuOptions.min)}-${formatInstanceMetric(shape.ocpuOptions.max)} OCPU`
      : `${formatInstanceMetric(shape.ocpuCount)} OCPU`;
    const memory = shape.memoryOptions?.min && shape.memoryOptions?.max
      ? `${formatInstanceMetric(shape.memoryOptions.min)}-${formatInstanceMetric(shape.memoryOptions.max)} GB`
      : `${formatInstanceMetric(shape.memoryGb)} GB`;
    return `${ocpu} / ${memory}`;
  }
  return `${formatInstanceMetric(shape.ocpuCount)} OCPU / ${formatInstanceMetric(shape.memoryGb)} GB`;
}

function formatBaselineLabel(value) {
  return {
    BASELINE_1_8: '1/8 OCPU 基线',
    BASELINE_1_2: '1/2 OCPU 基线',
    BASELINE_1_1: '1 OCPU 基线',
  }[value] || value;
}

function OraclePage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [activeTab, setActiveTab] = useState('instances');
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [selectedCompartmentId, setSelectedCompartmentId] = useState('');
  const [compartments, setCompartments] = useState([]);
  const [instances, setInstances] = useState([]);
  const [selectedInstanceId, setSelectedInstanceId] = useState('');
  const [instanceDetail, setInstanceDetail] = useState(null);
  const [query, setQuery] = useState('');
  const [stateFilter, setStateFilter] = useState('all');
  const [loadingAccounts, setLoadingAccounts] = useState(false);
  const [loadingInstances, setLoadingInstances] = useState(false);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState(emptyAccountForm);
  const [accountConfigText, setAccountConfigText] = useState('');
  const [submittingAccount, setSubmittingAccount] = useState(false);
  const [accountImportDialogOpen, setAccountImportDialogOpen] = useState(false);
  const [accountImportText, setAccountImportText] = useState('');
  const [accountImportFileName, setAccountImportFileName] = useState('');
  const [accountImportOverwrite, setAccountImportOverwrite] = useState(false);
  const [importingAccounts, setImportingAccounts] = useState(false);
  const [resizeDialogOpen, setResizeDialogOpen] = useState(false);
  const [resizeForm, setResizeForm] = useState(emptyResizeForm);
  const [resizeShapes, setResizeShapes] = useState([]);
  const [loadingResizeShapes, setLoadingResizeShapes] = useState(false);
  const [submittingResize, setSubmittingResize] = useState(false);
  const [deletingConsoleId, setDeletingConsoleId] = useState('');
  const [consolePublicKey, setConsolePublicKey] = useState('');
  const privateKeyFileRef = useRef(null);
  const accountImportFileRef = useRef(null);
  const cacheRef = useRef({
    accounts: null,
    compartments: new Map(),
    instances: new Map(),
    details: new Map(),
    shapes: new Map(),
  });
  const inflightRef = useRef({
    accounts: null,
    compartments: new Map(),
    instances: new Map(),
    details: new Map(),
    shapes: new Map(),
  });
  const previousScopeRef = useRef('');

  const selectedAccount = accounts.find((account) => String(account.id) === String(selectedAccountId));
  const selectedInstance = useMemo(
    () => instances.find((instance) => instance.id === selectedInstanceId) || instanceDetail,
    [instances, instanceDetail, selectedInstanceId]
  );
  const currentShape = useMemo(
    () => resizeShapes.find((shape) => shape.name === selectedInstance?.shape) || null,
    [resizeShapes, selectedInstance]
  );
  const resizeShapeOptions = useMemo(() => {
    if (!selectedInstance) return resizeShapes;
    if (!currentShape?.resizeCompatibleShapes?.length) return resizeShapes;
    const allowed = new Set([selectedInstance.shape, ...currentShape.resizeCompatibleShapes]);
    return resizeShapes.filter((shape) => allowed.has(shape.name));
  }, [currentShape, resizeShapes, selectedInstance]);
  const selectedResizeShape = useMemo(
    () => resizeShapes.find((shape) => shape.name === resizeForm.shape) || null,
    [resizeShapes, resizeForm.shape]
  );

  const filteredInstances = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return instances.filter((instance) => {
      const matchesState = stateFilter === 'all' || instance.state === stateFilter;
      const haystack = [
        instance.name,
        instance.id,
        instance.state,
        instance.shape,
        instance.availabilityDomain,
        instance.primaryPublicIp,
        instance.primaryPrivateIp,
      ].join(' ').toLowerCase();
      return matchesState && (!keyword || haystack.includes(keyword));
    });
  }, [instances, query, stateFilter]);

  const getCachedValue = useCallback((entry, ttl) => {
    if (!entry) return null;
    if (Date.now() - entry.at > ttl) return null;
    return entry.value;
  }, []);

  const loadAccounts = useCallback(async ({ force = false, silent = false } = {}) => {
    const cached = force ? null : getCachedValue(cacheRef.current.accounts, CACHE_TTL_MS.accounts);
    if (cached) {
      setAccounts(cached);
      if (!selectedAccountId && cached.length > 0) {
        setSelectedAccountId(String(cached[0].id));
        setSelectedCompartmentId(cached[0].defaultCompartmentId || '');
      }
      return cached;
    }
    if (!force && inflightRef.current.accounts) return inflightRef.current.accounts;
    if (!silent) setLoadingAccounts(true);
    const request = (async () => {
    try {
      const response = await fetch('/api/oracle/accounts', { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '加载账号失败');
      const list = Array.isArray(unwrap(result)) ? unwrap(result) : [];
      cacheRef.current.accounts = { value: list, at: Date.now() };
      setAccounts(list);
      if (!selectedAccountId && list.length > 0) {
        setSelectedAccountId(String(list[0].id));
        setSelectedCompartmentId(list[0].defaultCompartmentId || '');
      }
      return list;
    } catch (error) {
      toast.error(error.message || '加载 Oracle 账号失败');
      return [];
    } finally {
      inflightRef.current.accounts = null;
      if (!silent) setLoadingAccounts(false);
    }
    })();
    inflightRef.current.accounts = request;
    return request;
  }, [getCachedValue, selectedAccountId]);

  const loadCompartments = useCallback(async ({ force = false, silent = false, accountId = selectedAccountId } = {}) => {
    if (!accountId) return [];
    const cacheKey = String(accountId);
    const cached = force ? null : getCachedValue(cacheRef.current.compartments.get(cacheKey), CACHE_TTL_MS.compartments);
    if (cached) {
      setCompartments(cached);
      setSelectedCompartmentId((current) => {
        if (current && cached.some((item) => String(item.id) === String(current))) return current;
        return selectedAccount?.defaultCompartmentId || cached[0]?.id || '';
      });
      return cached;
    }
    if (!force && inflightRef.current.compartments.get(cacheKey)) return inflightRef.current.compartments.get(cacheKey);
    try {
      const request = (async () => {
        const response = await fetch(`/api/oracle/accounts/${accountId}/compartments`, { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '加载 compartment 失败');
      const items = unwrap(result).items || [];
      cacheRef.current.compartments.set(cacheKey, { value: items, at: Date.now() });
      setCompartments(items);
        setSelectedCompartmentId((current) => {
          if (current && items.some((item) => String(item.id) === String(current))) return current;
          return selectedAccount?.defaultCompartmentId || items[0]?.id || '';
        });
        return items;
      })();
      inflightRef.current.compartments.set(cacheKey, request);
      const items = await request;
      inflightRef.current.compartments.delete(cacheKey);
      return items;
    } catch (error) {
      toast.error(error.message || '加载 compartment 失败');
      inflightRef.current.compartments.delete(cacheKey);
      return [];
    }
  }, [getCachedValue, selectedAccount?.defaultCompartmentId, selectedAccountId]);

  const loadInstances = useCallback(async ({
    force = false,
    silent = false,
    accountId = selectedAccountId,
    compartmentId = selectedCompartmentId,
  } = {}) => {
    if (!accountId) return [];
    const cacheKey = `${accountId}:${compartmentId || 'root'}`;
    const cached = force ? null : getCachedValue(cacheRef.current.instances.get(cacheKey), CACHE_TTL_MS.instances);
    if (cached) {
      setInstances(cached);
      setSelectedInstanceId((current) => {
        if (current && cached.some((instance) => instance.id === current)) return current;
        return cached[0]?.id || '';
      });
      return cached;
    }
    if (!force && inflightRef.current.instances.get(cacheKey)) return inflightRef.current.instances.get(cacheKey);
    if (!silent) setLoadingInstances(true);
    try {
      const request = (async () => {
        const params = new URLSearchParams();
        if (compartmentId) params.set('compartmentId', compartmentId);
        const response = await fetch(`/api/oracle/accounts/${accountId}/instances?${params}`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (!response.ok || result.success === false) throw new Error(result.error || '加载实例失败');
        const list = unwrap(result).instances || [];
        cacheRef.current.instances.set(cacheKey, { value: list, at: Date.now() });
        setInstances(list);
        setSelectedInstanceId((current) => {
          if (current && list.some((instance) => instance.id === current)) return current;
          return list[0]?.id || '';
        });
        return list;
      })();
      inflightRef.current.instances.set(cacheKey, request);
      const list = await request;
      inflightRef.current.instances.delete(cacheKey);
      return list;
    } catch (error) {
      toast.error(error.message || '加载实例失败');
      inflightRef.current.instances.delete(cacheKey);
      return [];
    } finally {
      if (!silent) setLoadingInstances(false);
    }
  }, [getCachedValue, selectedAccountId, selectedCompartmentId]);

  const loadInstanceDetail = useCallback(async ({
    force = false,
    silent = false,
    accountId = selectedAccountId,
    compartmentId = selectedCompartmentId,
    instanceId = selectedInstanceId,
  } = {}) => {
    if (!accountId || !instanceId) return null;
    const cacheKey = `${accountId}:${compartmentId || 'root'}:${instanceId}`;
    const cached = force ? null : getCachedValue(cacheRef.current.details.get(cacheKey), CACHE_TTL_MS.detail);
    if (cached) {
      setInstanceDetail(cached);
      return cached;
    }
    if (!force && inflightRef.current.details.get(cacheKey)) return inflightRef.current.details.get(cacheKey);
    if (!silent) setLoadingDetail(true);
    try {
      const request = (async () => {
        const params = new URLSearchParams();
        if (compartmentId) params.set('compartmentId', compartmentId);
        const response = await fetch(`/api/oracle/accounts/${accountId}/instances/${encodeURIComponent(instanceId)}?${params}`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (!response.ok || result.success === false) throw new Error(result.error || '加载实例详情失败');
        const instance = unwrap(result).instance || null;
        cacheRef.current.details.set(cacheKey, { value: instance, at: Date.now() });
        setInstanceDetail(instance);
        return instance;
      })();
      inflightRef.current.details.set(cacheKey, request);
      const instance = await request;
      inflightRef.current.details.delete(cacheKey);
      return instance;
    } catch (error) {
      toast.error(error.message || '加载实例详情失败');
      inflightRef.current.details.delete(cacheKey);
      return null;
    } finally {
      if (!silent) setLoadingDetail(false);
    }
  }, [getCachedValue, selectedAccountId, selectedCompartmentId, selectedInstanceId]);

  const loadShapes = useCallback(async ({
    force = false,
    accountId = selectedAccountId,
    compartmentId = selectedCompartmentId,
    availabilityDomain = selectedInstance?.availabilityDomain,
    imageId = selectedInstance?.imageId,
  } = {}) => {
    if (!accountId) return [];
    const cacheKey = `${accountId}:${compartmentId || 'root'}:${availabilityDomain || 'ad'}:${imageId || 'image'}`;
    const cached = force ? null : getCachedValue(cacheRef.current.shapes.get(cacheKey), CACHE_TTL_MS.shapes);
    if (cached) {
      setResizeShapes(cached);
      return cached;
    }
    if (!force && inflightRef.current.shapes.get(cacheKey)) return inflightRef.current.shapes.get(cacheKey);
    setLoadingResizeShapes(true);
    try {
      const request = (async () => {
        const params = new URLSearchParams();
        if (compartmentId) params.set('compartmentId', compartmentId);
        if (availabilityDomain) params.set('availabilityDomain', availabilityDomain);
        if (imageId) params.set('imageId', imageId);
        const response = await fetch(`/api/oracle/accounts/${accountId}/shapes?${params}`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (!response.ok || result.success === false) throw new Error(result.error || '加载规格失败');
        const items = unwrap(result).items || [];
        cacheRef.current.shapes.set(cacheKey, { value: items, at: Date.now() });
        setResizeShapes(items);
        return items;
      })();
      inflightRef.current.shapes.set(cacheKey, request);
      const items = await request;
      inflightRef.current.shapes.delete(cacheKey);
      return items;
    } catch (error) {
      toast.error(error.message || '加载规格失败');
      inflightRef.current.shapes.delete(cacheKey);
      return [];
    } finally {
      setLoadingResizeShapes(false);
    }
  }, [getCachedValue, selectedAccountId, selectedCompartmentId, selectedInstance]);

  const invalidateScopeCache = useCallback((accountId = selectedAccountId) => {
    if (!accountId) return;
    const prefix = `${accountId}:`;
    cacheRef.current.compartments.delete(String(accountId));
    Array.from(cacheRef.current.instances.keys()).forEach((key) => {
      if (key.startsWith(prefix)) cacheRef.current.instances.delete(key);
    });
    Array.from(cacheRef.current.details.keys()).forEach((key) => {
      if (key.startsWith(prefix)) cacheRef.current.details.delete(key);
    });
    Array.from(cacheRef.current.shapes.keys()).forEach((key) => {
      if (key.startsWith(prefix)) cacheRef.current.shapes.delete(key);
    });
  }, [selectedAccountId]);

  const refreshCurrentWorkspace = useCallback(async () => {
    await loadInstances({ force: true });
    if (selectedInstanceId) {
      await loadInstanceDetail({ force: true, instanceId: selectedInstanceId });
    }
  }, [loadInstanceDetail, loadInstances, selectedInstanceId]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    previousScopeRef.current = '';
    setSelectedCompartmentId('');
    setCompartments([]);
    setInstances([]);
    setSelectedInstanceId('');
    setInstanceDetail(null);
    if (selectedAccountId) loadCompartments();
  }, [selectedAccountId, loadCompartments]);

  useEffect(() => {
    const scopeKey = `${selectedAccountId}:${selectedCompartmentId || 'root'}`;
    if (scopeKey === previousScopeRef.current) return;
    previousScopeRef.current = scopeKey;
    setSelectedInstanceId('');
    setInstanceDetail(null);
  }, [selectedAccountId, selectedCompartmentId]);

  useEffect(() => {
    if (selectedAccountId && activeTab !== 'accounts') loadInstances();
  }, [selectedAccountId, selectedCompartmentId, activeTab, loadInstances]);

  useEffect(() => {
    if (selectedInstanceId && activeTab !== 'accounts') loadInstanceDetail();
  }, [selectedInstanceId, activeTab, loadInstanceDetail]);

  const openAccountDialog = (account = null) => {
    setEditingAccount(account);
    setAccountConfigText('');
    setAccountForm(account ? {
      name: account.name || '',
      tenancyOcid: '',
      userOcid: '',
      fingerprint: account.fingerprint || '',
      region: account.region || 'ap-tokyo-1',
      privateKeyPem: '',
      passphrase: '',
      defaultCompartmentId: account.defaultCompartmentId || '',
      description: account.description || '',
    } : emptyAccountForm);
    setAccountDialogOpen(true);
  };

  const updateAccountConfigText = (value) => {
    setAccountConfigText(value);
    const parsed = parseOciConfig(value);
    setAccountForm((prev) => ({
      ...prev,
      userOcid: parsed.user || prev.userOcid,
      fingerprint: parsed.fingerprint || prev.fingerprint,
      tenancyOcid: parsed.tenancy || prev.tenancyOcid,
      region: parsed.region || prev.region,
    }));
  };

  const uploadPrivateKey = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const text = await file.text();
      setAccountForm((prev) => ({ ...prev, privateKeyPem: text.trim() }));
      toast.success('私钥已导入');
    } catch (error) {
      toast.error(error.message || '读取私钥文件失败');
    } finally {
      event.target.value = '';
    }
  };

  const openAccountImportDialog = () => {
    setAccountImportText('');
    setAccountImportFileName('');
    setAccountImportOverwrite(false);
    setAccountImportDialogOpen(true);
  };

  const loadAccountImportFile = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.json')) {
      toast.error('仅支持导入 .json 文件');
      event.target.value = '';
      return;
    }
    try {
      const text = await file.text();
      parseImportedAccounts(text);
      setAccountImportText(text);
      setAccountImportFileName(file.name);
      setAccountImportDialogOpen(true);
      toast.success(`已载入 ${file.name}`);
    } catch (error) {
      toast.error(error.message || '读取导入文件失败');
    } finally {
      event.target.value = '';
    }
  };

  const exportAccounts = async () => {
    try {
      const response = await fetch('/api/oracle/export/accounts', { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '导出账号失败');
      const items = Array.isArray(unwrap(result)?.accounts) ? unwrap(result).accounts : [];
      downloadJson(`oracle-accounts-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`, {
        version: '1.0',
        exportTime: new Date().toISOString(),
        accounts: items,
      });
      toast.success('Oracle 账号已导出');
    } catch (error) {
      toast.error(error.message || '导出账号失败');
    }
  };

  const submitImportAccounts = async () => {
    setImportingAccounts(true);
    try {
      const accountsToImport = parseImportedAccounts(accountImportText);
      const response = await fetch('/api/oracle/import/accounts', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accounts: accountsToImport,
          overwrite: accountImportOverwrite,
        }),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '导入账号失败');
      toast.success(`已导入 ${unwrap(result).imported || accountsToImport.length} 个 Oracle 账号`);
      setAccountImportDialogOpen(false);
      setAccountImportText('');
      setAccountImportFileName('');
      setAccountImportOverwrite(false);
      cacheRef.current.accounts = null;
      cacheRef.current.compartments.clear();
      cacheRef.current.instances.clear();
      cacheRef.current.details.clear();
      cacheRef.current.shapes.clear();
      setSelectedAccountId('');
      setSelectedCompartmentId('');
      setSelectedInstanceId('');
      setInstanceDetail(null);
      await loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '导入账号失败');
    } finally {
      setImportingAccounts(false);
    }
  };

  const saveAccount = async () => {
    const missingRequired = !accountForm.name ||
      !accountForm.region ||
      !accountForm.fingerprint ||
      (!editingAccount && (!accountForm.tenancyOcid || !accountForm.userOcid || !accountForm.privateKeyPem));
    if (missingRequired) {
      toast.warning(editingAccount ? '请填写账号名称、Region 和 Fingerprint' : '请填写 OCI 账号信息和私钥');
      return;
    }
    setSubmittingAccount(true);
    try {
      const url = editingAccount ? `/api/oracle/accounts/${editingAccount.id}` : '/api/oracle/accounts';
      const method = editingAccount ? 'PUT' : 'POST';
      const response = await fetch(url, { method, headers: getAuthHeaders(), body: JSON.stringify(accountForm) });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '保存账号失败');
      toast.success(editingAccount ? 'Oracle 账号已更新' : 'Oracle 账号已添加');
      setAccountDialogOpen(false);
      cacheRef.current.accounts = null;
      invalidateScopeCache(editingAccount?.id);
      await loadAccounts();
    } catch (error) {
      toast.error(error.message || '保存账号失败');
    } finally {
      setSubmittingAccount(false);
    }
  };

  const verifyAccount = async (accountId) => {
    try {
      const response = await fetch(`/api/oracle/accounts/${accountId}/verify`, { method: 'POST', headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '验证失败');
      toast.success('账号验证成功');
      cacheRef.current.accounts = null;
      loadAccounts();
    } catch (error) {
      toast.error(error.message || '账号验证失败');
    }
  };

  const deleteAccount = async (accountId) => {
    if (!confirmPress(`account:${accountId}`, '删除 Oracle 账号')) return;
    try {
      const response = await fetch(`/api/oracle/accounts/${accountId}`, { method: 'DELETE', headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '删除失败');
      toast.success('账号已删除');
      cacheRef.current.accounts = null;
      invalidateScopeCache(accountId);
      if (String(selectedAccountId) === String(accountId)) setSelectedAccountId('');
      loadAccounts();
    } catch (error) {
      toast.error(error.message || '删除账号失败');
    }
  };

  const openResizeDialog = async () => {
    if (!selectedInstance) return;
    setResizeForm({
      shape: selectedInstance.shape || '',
      ocpuCount: selectedInstance.ocpuCount ? String(selectedInstance.ocpuCount) : '',
      memoryGb: selectedInstance.memoryGb ? String(selectedInstance.memoryGb) : '',
      baselineOcpuUtilization: '',
      avoidDowntime: false,
    });
    setResizeDialogOpen(true);
    await loadShapes({ force: true });
  };

  const applyResizeShape = (shapeName) => {
    const nextShape = resizeShapes.find((shape) => shape.name === shapeName);
    setResizeForm((current) => {
      if (!nextShape) return { ...current, shape: shapeName };
      if (!nextShape.isFlexible) {
        return { ...current, shape: shapeName, ocpuCount: '', memoryGb: '', baselineOcpuUtilization: '' };
      }
      const currentOcpu = parseNumberInput(current.ocpuCount);
      const currentMemory = parseNumberInput(current.memoryGb);
      const nextOcpu = clampResizeValue(
        currentOcpu,
        nextShape.ocpuOptions?.min,
        nextShape.ocpuOptions?.max,
        nextShape.ocpuCount || nextShape.ocpuOptions?.min || 1
      );
      const nextMemory = clampResizeValue(
        currentMemory,
        nextShape.memoryOptions?.min,
        nextShape.memoryOptions?.max,
        nextShape.memoryGb || nextShape.memoryOptions?.min || 1
      );
      return {
        ...current,
        shape: shapeName,
        ocpuCount: Number.isFinite(nextOcpu) ? String(nextOcpu) : '',
        memoryGb: Number.isFinite(nextMemory) ? String(nextMemory) : '',
        baselineOcpuUtilization: nextShape.baselineOcpuUtilizations?.includes(current.baselineOcpuUtilization)
          ? current.baselineOcpuUtilization
          : '',
      };
    });
  };

  const saveResize = async () => {
    if (!selectedInstance) return;
    const nextShape = resizeForm.shape || selectedInstance.shape || '';
    const selectedShapeConfig = resizeShapes.find((shape) => shape.name === nextShape) || null;
    const payload = {
      shape: nextShape,
      avoidDowntime: !!resizeForm.avoidDowntime,
    };
    if (selectedShapeConfig?.isFlexible) {
      const ocpu = parseNumberInput(resizeForm.ocpuCount);
      const memory = parseNumberInput(resizeForm.memoryGb);
      if (!Number.isFinite(ocpu) || ocpu <= 0 || !Number.isFinite(memory) || memory <= 0) {
        toast.warning('请选择合法的 OCPU 和内存值');
        return;
      }
      payload.ocpuCount = ocpu;
      payload.memoryGb = memory;
      if (resizeForm.baselineOcpuUtilization) {
        payload.baselineOcpuUtilization = resizeForm.baselineOcpuUtilization;
      }
    }
    setSubmittingResize(true);
    try {
      const response = await fetch(`/api/oracle/accounts/${selectedAccountId}/instances/${encodeURIComponent(selectedInstance.id)}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '升降配失败');
      toast.success('实例配置更新请求已提交');
      setResizeDialogOpen(false);
      invalidateScopeCache();
      await refreshCurrentWorkspace();
    } catch (error) {
      toast.error(error.message || '升降配失败');
    } finally {
      setSubmittingResize(false);
    }
  };

  const runAction = async (action) => {
    if (!selectedInstance) return;
    const labels = { START: '启动', STOP: '停止', SOFTSTOP: '软停止', RESET: '强制重启', SOFTRESET: '软重启', REBOOTMIGRATE: '迁移重启' };
    if (!(await dialog.confirm(`确认${labels[action] || action}实例 ${selectedInstance.name || selectedInstance.id} 吗？`))) return;
    try {
      const response = await fetch(`/api/oracle/accounts/${selectedAccountId}/instances/${encodeURIComponent(selectedInstance.id)}/actions`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ action }),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '动作执行失败');
      toast.success(`${labels[action] || action}指令已下发`);
      invalidateScopeCache();
      setTimeout(() => {
        loadInstances({ force: true });
        loadInstanceDetail({ force: true });
      }, 1200);
    } catch (error) {
      toast.error(error.message || '动作执行失败');
    }
  };

  const terminateInstance = async () => {
    if (!selectedInstance) return;
    if (!(await dialog.confirm(`确认终止实例 ${selectedInstance.name || selectedInstance.id} 吗？该操作风险较高，请确认已备份数据。`))) return;
    try {
      const response = await fetch(`/api/oracle/accounts/${selectedAccountId}/instances/${encodeURIComponent(selectedInstance.id)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
        body: JSON.stringify({ preserveBootVolume: true }),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '终止失败');
      toast.success('终止指令已下发');
      invalidateScopeCache();
      setSelectedInstanceId('');
      setTimeout(() => loadInstances({ force: true }), 1200);
    } catch (error) {
      toast.error(error.message || '终止实例失败');
    }
  };

  const createConsoleConnection = async () => {
    if (!selectedInstance || !consolePublicKey.trim()) {
      toast.warning('请先选择实例并填写 SSH 公钥');
      return;
    }
    try {
      const response = await fetch(`/api/oracle/accounts/${selectedAccountId}/instances/${encodeURIComponent(selectedInstance.id)}/console-connections`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ publicKey: consolePublicKey }),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '创建控制台连接失败');
      toast.success('控制台连接已创建');
      invalidateScopeCache();
      setConsolePublicKey('');
      loadInstanceDetail({ force: true });
    } catch (error) {
      toast.error(error.message || '创建控制台连接失败');
    }
  };

  const deleteConsoleConnection = async (connectionId) => {
    if (!selectedInstance || !connectionId) return;
    if (!confirmPress(`console:${connectionId}`, '删除控制台连接')) return;
    setDeletingConsoleId(connectionId);
    try {
      const response = await fetch(`/api/oracle/accounts/${selectedAccountId}/console-connections/${encodeURIComponent(connectionId)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '删除控制台连接失败');
      toast.success('控制台连接已删除');
      invalidateScopeCache();
      await loadInstanceDetail({ force: true });
    } catch (error) {
      toast.error(error.message || '删除控制台连接失败');
    } finally {
      setDeletingConsoleId('');
    }
  };

  const copyText = async (text) => {
    if (!text) return;
    await navigator.clipboard.writeText(text);
    toast.success('已复制');
  };

  return (
    <PageStack viewport>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={tabs}
        />
        <TabBarOverflowActions
          items={[
            {
              key: 'account',
              type: 'select',
              label: '账号',
              icon: <Cloud className="h-3.5 w-3.5" />,
              value: selectedAccountId,
              onValueChange: setSelectedAccountId,
              disabled: loadingAccounts,
              options: accounts.map((account) => ({ value: String(account.id), label: account.name })),
            },
            {
              key: 'compartment',
              type: 'select',
              label: '分区',
              icon: <Layers className="h-3.5 w-3.5" />,
              value: selectedCompartmentId,
              onValueChange: setSelectedCompartmentId,
              disabled: !selectedAccountId || compartments.length === 0,
              options: compartments.map((item) => ({ value: String(item.id), label: item.name || item.id })),
            },
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-4 w-4" />,
              onClick: refreshCurrentWorkspace,
              disabled: !selectedAccountId,
            },
          ]}
        />
      </div>

      {activeTab === 'instances' && (
        <div className="grid min-h-0 gap-4 cq-xl:grid-cols-[minmax(0,1.45fr)_minmax(360px,0.75fr)]">
          <SectionCard
            title="实例列表"
            description={`${filteredInstances.length} 台实例`}
            className="min-h-0"
            bodyPadding="none"
            bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
            icon={<Server className="h-4 w-4 text-brand" />}
            actions={(
              <>
                <ResponsiveSearchInput
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="搜索名称、ID、IP、shape"
                  ariaLabel="搜索 Oracle 实例"
                  className="w-40 cq-sm:w-52"
                />
                <Select
                  aria-label="实例状态筛选"
                  size="sm"
                  className="w-32 cq-sm:w-36"
                  value={stateFilter}
                  onValueChange={setStateFilter}
                  items={stateOptions}
                />
              </>
            )}
          >
            {loadingInstances ? (
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
                <AppTable tableId="oracle-instances-loading" columns={INSTANCE_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>公共 IP</Table.Head>
                      <Table.Head>配置</Table.Head>
                      <Table.Head>OCPU</Table.Head>
                      <Table.Head>内存(GB)</Table.Head>
                      <Table.Head>创建时间</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    <TableSkeletonRows columns={7} rows={6} />
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            ) : filteredInstances.length === 0 ? (
              <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-kumo-subtle">暂无实例</div>
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
                <AppTable tableId="oracle-instances" columns={INSTANCE_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>公共 IP</Table.Head>
                      <Table.Head>配置</Table.Head>
                      <Table.Head>OCPU</Table.Head>
                      <Table.Head>内存(GB)</Table.Head>
                      <Table.Head>创建时间</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {filteredInstances.map((instance) => (
                      <Table.Row
                        key={instance.id}
                        variant={selectedInstanceId === instance.id ? 'selected' : 'default'}
                        className="cursor-pointer"
                        onClick={() => setSelectedInstanceId(instance.id)}
                      >
                        <Table.Cell>
                          <div className="truncate text-sm font-semibold text-kumo-strong" title={instance.name || '-'}>
                            {instance.name || '-'}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <StatusBadge tone={getOciStatusTone(instance.state)}>{instance.state || '-'}</StatusBadge>
                        </Table.Cell>
                        <Table.Cell className="font-mono text-xs">{instance.primaryPublicIp || '-'}</Table.Cell>
                        <Table.Cell>
                          <div className="truncate text-sm text-kumo-strong" title={instance.shape || '-'}>
                            {instance.shape || '-'}
                          </div>
                        </Table.Cell>
                        <Table.Cell>{formatInstanceMetric(instance.ocpuCount)}</Table.Cell>
                        <Table.Cell>{formatInstanceMetric(instance.memoryGb)}</Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">{formatOciDate(instance.timeCreated)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>

          <SectionCard
            title="实例详情"
            icon={<Settings className="h-4 w-4" />}
            className="min-w-0 cq-xl:sticky cq-xl:top-0 cq-xl:self-start"
            bodyPadding="none"
            bodyClassName="flex flex-col"
            actions={selectedInstance && (
              <>
                <Button type="button" size="sm" variant="secondary" onClick={() => runAction('START')}><Play className="h-4 w-4" /></Button>
                <Button type="button" size="sm" variant="secondary" onClick={() => runAction('STOP')}><Square className="h-4 w-4" /></Button>
                <Button type="button" size="sm" variant="secondary" onClick={openResizeDialog}><Cpu className="h-4 w-4" /></Button>
                <DropdownMenu>
                  <DropdownMenu.Trigger
                    render={<Button type="button" size="sm" variant="secondary" icon={<MoreVertical className="h-4 w-4" />} aria-label="更多实例动作" title="更多动作" />}
                  />
                  <DropdownMenu.Content side="bottom" align="end" sideOffset={6} className="min-w-40">
                    <DropdownMenu.Item onClick={() => runAction('SOFTSTOP')}>
                      软停止
                    </DropdownMenu.Item>
                    <DropdownMenu.Separator />
                    {ADVANCED_INSTANCE_ACTIONS.filter((action) => action.value !== 'SOFTSTOP').map((action) => (
                      <DropdownMenu.Item key={action.value} onClick={() => runAction(action.value)}>
                        {action.label}
                      </DropdownMenu.Item>
                    ))}
                  </DropdownMenu.Content>
                </DropdownMenu>
                <Button type="button" size="sm" variant="secondary-destructive" onClick={terminateInstance}><Trash className="h-4 w-4" /></Button>
              </>
            )}
          >
            {loadingDetail ? <DetailGridSkeleton /> : selectedInstance ? <DetailGrid instance={instanceDetail || selectedInstance} /> : <EmptySelection />}
          </SectionCard>
        </div>
      )}

      {activeTab === 'network' && <ResourceList title="VNIC 附加" icon={<Cloud className="h-4 w-4" />} loading={loadingDetail} items={instanceDetail?.vnicSummary || []} columns={['displayName', 'state', 'privateIp', 'publicIp', 'subnetId']} onCopy={copyText} />}
      {activeTab === 'storage' && <ResourceList title="卷附加" icon={<HardDrive className="h-4 w-4" />} loading={loadingDetail} items={[...(instanceDetail?.bootVolumeSummary || []), ...(instanceDetail?.blockVolumeSummary || [])]} columns={['volumeType', 'state', 'device', 'volumeId', 'attachmentId']} onCopy={copyText} />}
      {activeTab === 'console' && (
        <div className="grid min-h-0 flex-1 gap-4 cq-xl:grid-cols-[minmax(300px,0.72fr)_minmax(0,1.28fr)]">
          <SectionCard
            title="创建控制台连接"
            icon={<Terminal className="h-4 w-4 text-brand" />}
            className="min-h-0"
            bodyClassName="flex min-h-0 flex-1 flex-col gap-4"
            action={<Button type="button" size="sm" onClick={createConsoleConnection} disabled={!selectedInstance || !consolePublicKey.trim()}><Plus className="mr-2 h-4 w-4" />创建连接</Button>}
          >
            {selectedInstance ? (
              <>
                <InsetPanel tone="recessed" padding="sm">
                  <KeyValueGrid
                    columns={1}
                    items={[
                      {
                        label: '实例',
                        value: (
                          <span className="block truncate font-semibold" title={selectedInstance.name || selectedInstance.id}>
                            {selectedInstance.name || selectedInstance.id}
                          </span>
                        ),
                      },
                      {
                        label: '状态',
                        value: <StatusBadge tone={getOciStatusTone(selectedInstance.state)}>{selectedInstance.state || '-'}</StatusBadge>,
                      },
                      {
                        label: '公共 IP',
                        value: <span className="font-mono text-xs">{selectedInstance.primaryPublicIp || '-'}</span>,
                      },
                    ]}
                  />
                </InsetPanel>
                <CodeEditor
                  label="控制台连接 SSH 公钥"
                  language="text"
                  value={consolePublicKey}
                  onChange={setConsolePublicKey}
                  placeholder="粘贴 SSH 公钥"
                  minHeight="10rem"
                />
                <div className="text-xs leading-5 text-kumo-subtle">
                  创建后可在右侧查看连接串和指纹。
                </div>
              </>
            ) : (
              <EmptyState
                icon={Terminal}
                title="请先选择实例"
                card={false}
                className="min-h-[18rem]"
              />
            )}
          </SectionCard>

          <SectionCard
            title="控制台连接列表"
            icon={<Settings className="h-4 w-4" />}
            className="min-h-0 flex-1"
            bodyPadding="none"
            bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
          >
            {!selectedInstance ? (
              <EmptyState
                icon={Terminal}
                title="暂无可展示连接"
                description="选中实例后显示现有连接。"
                card={false}
                className="min-h-[20rem]"
              />
            ) : (
              <ResourceList
                embedded
                loading={loadingDetail}
                items={instanceDetail?.consoleSummary || []}
                columns={['state', 'connectionString', 'fingerprint', 'timeCreated']}
                onCopy={copyText}
                renderActions={(item) => (
                  <Button
                    type="button"
                    size="sm"
                    shape="square"
                    variant={isArmed(`console:${item.id}`) ? 'destructive' : 'secondary-destructive'}
                    disabled={deletingConsoleId === item.id}
                    onClick={() => deleteConsoleConnection(item.id)}
                    aria-label="删除控制台连接"
                    title="删除连接"
                    icon={<Trash className="h-3.5 w-3.5" />}
                  />
                )}
              />
            )}
          </SectionCard>
        </div>
      )}

      {activeTab === 'accounts' && (
        <SectionCard
          title="Oracle 账号"
          icon={<Key className="h-4 w-4 text-brand" />}
          description={accounts.length > 0 ? `${accounts.length} 个已配置账号` : '管理 OCI 凭证和默认 Compartment'}
          className="min-h-0 flex-1"
          bodyPadding="none"
          bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
          actions={(
            <>
              <input
                ref={accountImportFileRef}
                type="file"
                accept=".json,application/json"
                className="hidden"
                onChange={loadAccountImportFile}
              />
              <Toolbar size="sm" aria-label="导出导入账号" className="shrink-0">
                <Toolbar.Button type="button" onClick={exportAccounts} aria-label="导出账号" title="导出账号" icon={<Upload className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导出</span>
                </Toolbar.Button>
                <Toolbar.Button type="button" onClick={openAccountImportDialog} aria-label="导入账号" title="导入账号" icon={<Download className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导入</span>
                </Toolbar.Button>
              </Toolbar>
              <Button type="button" size="sm" shape="square" variant="primary" onClick={() => openAccountDialog()} aria-label="添加账号" title="添加账号" icon={<Plus className="h-4 w-4" />} />
            </>
          )}
        >
          <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
            <AppTable tableId="oracle-accounts" columns={ACCOUNT_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>Region</Table.Head>
                  <Table.Head>默认 Compartment</Table.Head>
                  <Table.Head>验证状态</Table.Head>
                  <Table.Head className="app-table-action">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {loadingAccounts && accounts.length === 0 ? (
                  <TableSkeletonRows columns={5} rows={5} />
                ) : accounts.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">尚未配置 Oracle 账号。</Table.Cell>
                  </Table.Row>
                ) : accounts.map((account) => (
                  <Table.Row
                    key={account.id}
                    className="cursor-pointer"
                    title="双击编辑账号"
                    onDoubleClick={() => openAccountDialog(account)}
                  >
                    <Table.Cell>
                      <div className="min-w-0">
                        <div className="truncate text-sm font-semibold text-kumo-strong" title={account.name || '-'}>
                          {account.name}
                        </div>
                        <div className="truncate font-mono text-[11px] text-kumo-subtle" title={account.description || account.tenancyOcid || '-'}>
                          {account.description || account.tenancyOcid}
                        </div>
                      </div>
                    </Table.Cell>
                    <Table.Cell>{account.region}</Table.Cell>
                    <Table.Cell><code className="block truncate text-xs">{account.defaultCompartmentId || '-'}</code></Table.Cell>
                    <Table.Cell>
                      <StatusBadge tone={getOciStatusTone(account.lastVerifyStatus)}>
                        {getVerifyStatusLabel(account.lastVerifyStatus)}
                      </StatusBadge>
                    </Table.Cell>
                    <Table.Cell className="text-right">
                      <div className="inline-flex gap-2">
                        <Button type="button" size="sm" shape="square" variant="secondary" onClick={() => verifyAccount(account.id)} aria-label={`验证 ${account.name}`} title="验证" icon={<Shield className="h-4 w-4" />} />
                        <Button type="button" size="sm" shape="square" variant="secondary" onClick={() => openAccountDialog(account)} aria-label={`编辑 ${account.name}`} title="编辑" icon={<Edit className="h-4 w-4" />} />
                        <Button type="button" size="sm" shape="square" variant={isArmed(`account:${account.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteAccount(account.id)} aria-label={`删除 ${account.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        </SectionCard>
      )}

      <Dialog.Root open={resizeDialogOpen} onOpenChange={setResizeDialogOpen}>
        <Dialog className="@container !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-bold text-kumo-strong">实例升降配</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            调整 shape 或 Flex 规格时，Oracle 可能重启实例，建议在低峰期执行。
          </Dialog.Description>
          {selectedInstance ? (
            <div className="space-y-4">
              <InsetPanel tone="recessed">
                <KeyValueGrid
                  items={[
                    {
                      label: '实例',
                      value: (
                        <div className="truncate font-semibold" title={selectedInstance.name || selectedInstance.id}>
                          {selectedInstance.name || selectedInstance.id}
                        </div>
                      ),
                    },
                    {
                      label: '当前规格',
                      value: <div className="font-mono">{selectedInstance.shape || '-'}</div>,
                    },
                    {
                      label: '当前 OCPU / 内存',
                      value: `${formatInstanceMetric(selectedInstance.ocpuCount)} / ${formatInstanceMetric(selectedInstance.memoryGb)} GB`,
                    },
                    {
                      label: '可用域',
                      value: selectedInstance.availabilityDomain || '-',
                    },
                  ]}
                />
              </InsetPanel>

              <Select
                aria-label="目标规格"
                size="sm"
                className="w-full"
                value={resizeForm.shape}
                onValueChange={applyResizeShape}
                disabled={loadingResizeShapes || resizeShapeOptions.length === 0}
                items={resizeShapeOptions.map((shape) => ({
                  value: shape.name,
                  label: `${shape.name} · ${formatShapeSummary(shape)}`,
                }))}
              />

              {selectedResizeShape ? (
                <InsetPanel tone="surface">
                  <div className="mb-1 text-sm font-semibold text-kumo-strong">{selectedResizeShape.name}</div>
                  <div className="text-xs leading-5 text-kumo-subtle">
                    {selectedResizeShape.processorDescription || 'Oracle 计算实例规格'}
                  </div>
                  <KeyValueGrid
                    className="mt-3"
                    items={[
                      {
                        label: '默认规格',
                        value: formatShapeSummary(selectedResizeShape),
                      },
                      {
                        label: '计费',
                        value: selectedResizeShape.billingType || 'PAID',
                      },
                    ]}
                  />
                  {selectedResizeShape.isFlexible ? (
                    <div className="mt-4 grid gap-3 cq-md:grid-cols-2">
                      <Input
                        size="sm"
                        label={`OCPU${selectedResizeShape.ocpuOptions?.min || selectedResizeShape.ocpuOptions?.max ? ` (${formatInstanceMetric(selectedResizeShape.ocpuOptions?.min)} - ${formatInstanceMetric(selectedResizeShape.ocpuOptions?.max)})` : ''}`}
                        value={resizeForm.ocpuCount}
                        onChange={(event) => setResizeForm((current) => ({ ...current, ocpuCount: event.target.value }))}
                        placeholder={selectedResizeShape.ocpuCount ? String(selectedResizeShape.ocpuCount) : '例如 2'}
                      />
                      <Input
                        size="sm"
                        label={`内存 GB${selectedResizeShape.memoryOptions?.min || selectedResizeShape.memoryOptions?.max ? ` (${formatInstanceMetric(selectedResizeShape.memoryOptions?.min)} - ${formatInstanceMetric(selectedResizeShape.memoryOptions?.max)})` : ''}`}
                        value={resizeForm.memoryGb}
                        onChange={(event) => setResizeForm((current) => ({ ...current, memoryGb: event.target.value }))}
                        placeholder={selectedResizeShape.memoryGb ? String(selectedResizeShape.memoryGb) : '例如 12'}
                      />
                      {selectedResizeShape.baselineOcpuUtilizations?.length ? (
                        <Select
                          aria-label="baseline OCPU"
                          size="sm"
                          className="cq-md:col-span-2"
                          value={resizeForm.baselineOcpuUtilization}
                          onValueChange={(value) => setResizeForm((current) => ({ ...current, baselineOcpuUtilization: value }))}
                          items={[
                            { value: '', label: '使用默认基线' },
                            ...selectedResizeShape.baselineOcpuUtilizations.map((value) => ({
                              value,
                              label: formatBaselineLabel(value),
                            })),
                          ]}
                        />
                      ) : null}
                    </div>
                  ) : (
                    <div className="mt-4 text-xs text-kumo-subtle">
                      固定规格，只需切换 shape，无需填写 OCPU / 内存。
                    </div>
                  )}
                </InsetPanel>
              ) : (
                <InsetPanel tone="dashed" bodyClassName="py-6 text-center text-sm text-kumo-subtle">
                  {loadingResizeShapes ? '正在加载可选规格...' : '当前实例没有可用的规格数据，请刷新后重试。'}
                </InsetPanel>
              )}

              <Switch
                size="sm"
                label="尽量避免停机更新"
                controlFirst={false}
                checked={resizeForm.avoidDowntime}
                onCheckedChange={(checked) => setResizeForm((current) => ({ ...current, avoidDowntime: Boolean(checked) }))}
              />

              <div className="flex justify-end gap-2">
                <Button type="button" variant="secondary" onClick={() => setResizeDialogOpen(false)}>取消</Button>
                <Button type="button" onClick={saveResize} disabled={submittingResize || loadingResizeShapes}>
                  {submittingResize ? '提交中...' : '提交变更'}
                </Button>
              </div>
            </div>
          ) : (
            <div className="py-8 text-center text-sm text-kumo-subtle">请先选择一个实例。</div>
          )}
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={accountDialogOpen} onOpenChange={setAccountDialogOpen}>
        <Dialog className="@container !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-bold text-kumo-strong">
            {editingAccount ? '编辑 Oracle 账号' : '添加 Oracle 账号'}
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            从 OCI 控制台复制 API Key 配置，并粘贴 PEM 私钥全文。
          </Dialog.Description>
          <div className="mb-4 space-y-2">
            <CodeEditor
              label="OCI 配置文件"
              language="ini"
              value={accountConfigText}
              onChange={updateAccountConfigText}
              minHeight="10rem"
              placeholder={`[DEFAULT]
user=ocid1.user...
fingerprint=fa:d1:...
tenancy=ocid1.tenancy...
region=us-sanjose-1
key_file=<path to your private keyfile>`}
            />
            <div className="flex items-start gap-1.5 text-xs leading-5 text-kumo-subtle">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>粘贴后自动填充 User OCID、Fingerprint、Tenancy OCID 和 Region；key_file 不使用，私钥请在下方粘贴或上传。</span>
            </div>
          </div>
          <div className="grid gap-3 cq-md:grid-cols-2">
            <Input
              label="账号名称 *"
              value={accountForm.name}
              onChange={(event) => setAccountForm({ ...accountForm, name: event.target.value })}
              placeholder="如：东京主账号"
            />
            <Input
              label="Region *"
              value={accountForm.region}
              onChange={(event) => setAccountForm({ ...accountForm, region: event.target.value })}
              placeholder="如：ap-tokyo-1"
            />
            <Input
              label={editingAccount ? 'Tenancy OCID' : 'Tenancy OCID *'}
              value={accountForm.tenancyOcid}
              onChange={(event) => setAccountForm({ ...accountForm, tenancyOcid: event.target.value })}
              placeholder={editingAccount ? '留空不修改' : 'ocid1.tenancy...'}
            />
            <Input
              label={editingAccount ? 'User OCID' : 'User OCID *'}
              value={accountForm.userOcid}
              onChange={(event) => setAccountForm({ ...accountForm, userOcid: event.target.value })}
              placeholder={editingAccount ? '留空不修改' : 'ocid1.user...'}
            />
            <Input
              label="Fingerprint *"
              value={accountForm.fingerprint}
              onChange={(event) => setAccountForm({ ...accountForm, fingerprint: event.target.value })}
              placeholder="fingerprint"
            />
            <Input
              label="默认 Compartment"
              value={accountForm.defaultCompartmentId}
              onChange={(event) => setAccountForm({ ...accountForm, defaultCompartmentId: event.target.value })}
              placeholder="可选；留空用根租户"
            />
            <Input
              label="私钥 Passphrase"
              value={accountForm.passphrase}
              onChange={(event) => setAccountForm({ ...accountForm, passphrase: event.target.value })}
              placeholder="可选"
              type="text"
              autoComplete="off"
              data-1p-ignore
              data-lpignore="true"
              data-bwignore="true"
              data-form-type="other"
              spellCheck={false}
            />
            <Input
              label="备注"
              value={accountForm.description}
              onChange={(event) => setAccountForm({ ...accountForm, description: event.target.value })}
              placeholder="可选"
            />
            <div className="cq-md:col-span-2">
              <input
                ref={privateKeyFileRef}
                type="file"
                accept=".pem,.key,.txt"
                className="hidden"
                onChange={uploadPrivateKey}
              />
              <div className="mb-1.5 flex items-center justify-between gap-3">
                <div className="text-sm font-medium text-kumo-strong">
                  {editingAccount ? 'Oracle API 私钥 PEM' : 'Oracle API 私钥 PEM *'}
                </div>
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={() => privateKeyFileRef.current?.click()}
                  icon={<Upload className="h-3.5 w-3.5" />}
                >
                  上传 PEM
                </Button>
              </div>
              <CodeEditor
                label="Oracle API 私钥 PEM"
                language="text"
                value={accountForm.privateKeyPem}
                onChange={(privateKeyPem) => setAccountForm({ ...accountForm, privateKeyPem })}
                minHeight="10rem"
                placeholder={editingAccount ? '留空不修改私钥' : '-----BEGIN PRIVATE KEY-----'}
              />
            </div>
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setAccountDialogOpen(false)}>取消</Button>
            <Button type="button" onClick={saveAccount} disabled={submittingAccount}>{submittingAccount ? '保存中...' : '保存'}</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={accountImportDialogOpen} onOpenChange={setAccountImportDialogOpen}>
        <Dialog className="!w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-bold text-kumo-strong">导入 Oracle 账号</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
            支持导入本页导出的 Oracle 账号 JSON，可选文件或直接粘贴。
          </Dialog.Description>
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-3">
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={() => accountImportFileRef.current?.click()}
                icon={<Download className="h-3.5 w-3.5" />}
              >
                选择 JSON 文件
              </Button>
              <div className="min-w-0 text-xs text-kumo-subtle">
                {accountImportFileName || '未选择文件'}
              </div>
            </div>
            <CodeEditor
              label="导入 Oracle 账号 JSON"
              language="json"
              value={accountImportText}
              onChange={setAccountImportText}
              minHeight="14rem"
              placeholder={`{\n  "version": "1.0",\n  "accounts": []\n}`}
            />
            <Switch
              size="sm"
              label="覆盖当前已有账号"
              controlFirst={false}
              checked={accountImportOverwrite}
              onCheckedChange={(checked) => setAccountImportOverwrite(Boolean(checked))}
            />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setAccountImportDialogOpen(false)}>取消</Button>
              <Button type="button" onClick={submitImportAccounts} disabled={importingAccounts || !accountImportText.trim()}>
                {importingAccounts ? '导入中...' : '导入'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

    </PageStack>
  );
}

function DetailGrid({ instance }) {
  const rows = [
    ['实例名称', instance.name],
    ['状态', instance.state],
    ['规格', instance.shape],
    ['OCPU', formatInstanceMetric(instance.ocpuCount)],
    ['内存', instance.memoryGb ? `${formatInstanceMetric(instance.memoryGb)} GB` : '-'],
    ['可用域', instance.availabilityDomain],
    ['故障域', instance.faultDomain],
    ['公网 IP', instance.primaryPublicIp],
    ['私网 IP', instance.primaryPrivateIp],
    ['镜像 ID', instance.imageId],
    ['创建时间', instance.timeCreated],
  ];
  return (
    <div className="min-h-0 flex-1 overflow-auto scrollbar-thin">
      <div className="divide-y divide-kumo-line/80">
        {rows.map(([label, value]) => (
          <div key={label} className="grid grid-cols-[108px_minmax(0,1fr)] items-center gap-3 px-4 py-2.5">
            <div className="whitespace-nowrap text-sm text-kumo-subtle">{label}</div>
            <div className="min-w-0">
              {label === '状态' ? (
                <StatusBadge tone={getOciStatusTone(value)}>{value || '-'}</StatusBadge>
              ) : (
                <div className="truncate text-sm text-kumo-strong" title={value || '-'}>
                  {value || '-'}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function EmptySelection() {
  return <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-kumo-subtle">请选择一个实例查看详情</div>;
}

function ResourceList({ title, icon, items, columns, onCopy, embedded = false, loading = false, renderActions = null }) {
  const table = loading ? (
    <DataTableFrame
      variant="embedded"
      density="dense"
      className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin [&_td:first-child]:!pr-1.5 [&_td:last-child]:!pl-1.5"
    >
      <AppTable tableId={`oracle-${title}-loading`} columns={resourceColumnSpecs(columns, renderActions)}>
        <Table.Header variant="compact">
          <Table.Row>
            {columns.map((column) => <Table.Head key={column}>{columnLabel(column)}</Table.Head>)}
            {renderActions ? <Table.Head className="app-table-action">操作</Table.Head> : null}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          <TableSkeletonRows columns={columns.length + (renderActions ? 1 : 0)} rows={5} />
        </Table.Body>
      </AppTable>
    </DataTableFrame>
  ) : items.length === 0 ? (
    <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-kumo-subtle">暂无数据</div>
  ) : (
    <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
      <AppTable tableId={`oracle-${title}`} columns={resourceColumnSpecs(columns, renderActions)}>
        <Table.Header variant="compact">
          <Table.Row>
            {columns.map((column) => <Table.Head key={column}>{columnLabel(column)}</Table.Head>)}
            {renderActions ? <Table.Head className="app-table-action">操作</Table.Head> : null}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {items.map((item, index) => (
            <Table.Row key={item.id || item.attachmentId || item.volumeId || index}>
              {columns.map((column) => (
                <Table.Cell key={column}>
                  <div className="flex items-center gap-2">
                    {column === 'state' ? (
                      <StatusBadge tone={getOciStatusTone(item[column])}>{String(item[column] || '-')}</StatusBadge>
                    ) : (
                      <div className="min-w-0 truncate text-sm text-kumo-strong" title={String(item[column] || '-')}>
                        {String(item[column] || '-')}
                      </div>
                    )}
                    {item[column] && ['connectionString', 'volumeId', 'attachmentId', 'subnetId'].includes(column) && (
                      <Button type="button" size="sm" shape="square" variant="ghost" onClick={() => onCopy(item[column])} aria-label={`复制${columnLabel(column)}`} title="复制" icon={<Copy className="h-3.5 w-3.5" />} />
                    )}
                  </div>
                </Table.Cell>
              ))}
              {renderActions ? <Table.Cell className="text-right">{renderActions(item)}</Table.Cell> : null}
            </Table.Row>
          ))}
        </Table.Body>
      </AppTable>
    </DataTableFrame>
  );
  if (embedded) return table;
  return (
    <SectionCard
      title={title}
      icon={icon}
      className="min-h-0 flex-1"
      bodyPadding="none"
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
    >
      {table}
    </SectionCard>
  );
}

function resourceColumnSpecs(columns, renderActions) {
  const flexibleColumn = columns.find((column) => ['displayName', 'connectionString', 'volumeId'].includes(column)) || columns[0];
  const specs = columns.map((column) => {
    if (column === flexibleColumn) {
      if (column === 'connectionString') return { id: column, role: 'content' };
      if (column === 'volumeId') return { id: column, role: 'identifier' };
      return { id: column, role: 'primary' };
    }
    if (column === 'state') return { id: column, role: 'status' };
    if (column === 'volumeType') return { id: column, role: 'type' };
    if (column === 'timeCreated') return { id: column, role: 'datetime' };
    if (['privateIp', 'publicIp', 'fingerprint'].includes(column)) {
      return { id: column, role: 'identifier' };
    }
    if (['subnetId', 'attachmentId'].includes(column)) {
      return { id: column, role: 'identifier', minWidth: 200 };
    }
    return { id: column, role: 'meta', grow: 1, minWidth: 160 };
  });
  if (renderActions) specs.push({ id: 'actions', role: 'actions-md' });
  return specs;
}

function TableSkeletonRows({ columns, rows = 5 }) {
  return Array.from({ length: rows }).map((_, index) => (
    <Table.Row key={index}>
      <Table.Cell colSpan={columns}>
        <SkeletonLine className="h-4 w-full" />
      </Table.Cell>
    </Table.Row>
  ));
}

function DetailGridSkeleton() {
  return (
    <div className="min-h-0 flex-1 overflow-auto scrollbar-thin">
      <div className="divide-y divide-kumo-line/80">
        {Array.from({ length: 8 }).map((_, index) => (
          <div key={index} className="grid grid-cols-[108px_minmax(0,1fr)] items-center gap-3 px-4 py-2.5">
            <SkeletonLine className="h-4 w-16" />
            <SkeletonLine className="h-4 w-full" />
          </div>
        ))}
      </div>
    </div>
  );
}

function columnLabel(column) {
  return {
    displayName: '名称',
    state: '状态',
    privateIp: '私网 IP',
    publicIp: '公网 IP',
    subnetId: '子网',
    volumeType: '类型',
    device: '设备',
    volumeId: '卷 ID',
    attachmentId: '附加 ID',
    connectionString: '连接串',
    fingerprint: '指纹',
    timeCreated: '创建时间',
  }[column] || column;
}

function formatInstanceMetric(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return '-';
  return Number.isInteger(numeric) ? String(numeric) : numeric.toFixed(1).replace(/\.0$/, '');
}

function formatOciDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export default OraclePage;
