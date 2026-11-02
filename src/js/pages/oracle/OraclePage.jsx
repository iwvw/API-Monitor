import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Tabs } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { PageStack, TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, HardDrive, Layers, RefreshCw } from '../../components/Icons.jsx';
import { CACHE_TTL_MS, emptyAccountForm, emptyResizeForm } from './constants.js';
import { tabs } from './tabs.jsx';
import {
  clampResizeValue,
  downloadJson,
  formatBaselineLabel,
  formatInstanceMetric,
  getAuthHeaders,
  parseImportedAccounts,
  parseNumberInput,
  parseOciConfig,
  unwrap,
} from './utils.js';
import InstanceListPanel from './InstanceListPanel.jsx';
import InstanceDetailPanel from './InstanceDetailPanel.jsx';
import ResourceList from './ResourceList.jsx';
import ConsolePanel from './ConsolePanel.jsx';
import CostPanel from './CostPanel.jsx';
import AccountsPanel from './AccountsPanel.jsx';
import ResizeDialog from './ResizeDialog.jsx';
import AccountDialog from './AccountDialog.jsx';
import AccountImportDialog from './AccountImportDialog.jsx';

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
  const [costOverview, setCostOverview] = useState(null);
  const [loadingCost, setLoadingCost] = useState(false);
  const privateKeyFileRef = useRef(null);
  const accountImportFileRef = useRef(null);
  const cacheRef = useRef({
    accounts: null,
    compartments: new Map(),
    instances: new Map(),
    details: new Map(),
    shapes: new Map(),
    cost: new Map(),
  });
  const inflightRef = useRef({
    accounts: null,
    compartments: new Map(),
    instances: new Map(),
    details: new Map(),
    shapes: new Map(),
    cost: new Map(),
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

  const loadCost = useCallback(async ({ force = false, silent = false, accountId = selectedAccountId } = {}) => {
    if (!accountId) return null;
    const cacheKey = String(accountId);
    const cached = force ? null : getCachedValue(cacheRef.current.cost.get(cacheKey), CACHE_TTL_MS.cost);
    if (cached) {
      setCostOverview(cached);
      return cached;
    }
    if (!force && inflightRef.current.cost.get(cacheKey)) return inflightRef.current.cost.get(cacheKey);
    if (!silent) setLoadingCost(true);
    try {
      const request = (async () => {
        const response = await fetch(`/api/oracle/accounts/${accountId}/cost`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (!response.ok || result.success === false) throw new Error(result.error || '加载成本数据失败');
        const data = unwrap(result);
        cacheRef.current.cost.set(cacheKey, { value: data, at: Date.now() });
        setCostOverview(data);
        return data;
      })();
      inflightRef.current.cost.set(cacheKey, request);
      const data = await request;
      inflightRef.current.cost.delete(cacheKey);
      return data;
    } catch (error) {
      toast.error(error.message || '加载成本数据失败');
      inflightRef.current.cost.delete(cacheKey);
      return null;
    } finally {
      if (!silent) setLoadingCost(false);
    }
  }, [getCachedValue, selectedAccountId]);

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
    if (selectedAccountId && activeTab === 'cost') loadCost();
  }, [selectedAccountId, activeTab, loadCost]);

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
      toast.warning(editingAccount
        ? '请填写账号名称、Region 和 Fingerprint' : '请填写 OCI 账号信息和私钥');
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
          <InstanceListPanel
            filteredInstances={filteredInstances}
            loadingInstances={loadingInstances}
            query={query}
            onQueryChange={setQuery}
            stateFilter={stateFilter}
            onStateFilterChange={setStateFilter}
            selectedInstanceId={selectedInstanceId}
            onSelectInstance={setSelectedInstanceId}
          />

          <InstanceDetailPanel
            loadingDetail={loadingDetail}
            selectedInstance={selectedInstance}
            instanceDetail={instanceDetail}
            onRunAction={runAction}
            onOpenResizeDialog={openResizeDialog}
            onTerminate={terminateInstance}
          />
        </div>
      )}

      {activeTab === 'network' && <ResourceList title="VNIC 附加" icon={<Cloud className="h-4 w-4" />} loading={loadingDetail} items={instanceDetail?.vnicSummary || []} columns={['displayName', 'state', 'privateIp', 'publicIp', 'subnetId']} onCopy={copyText} />}
      {activeTab === 'storage' && <ResourceList title="卷附加" icon={<HardDrive className="h-4 w-4" />} loading={loadingDetail} items={[...(instanceDetail?.bootVolumeSummary || []), ...(instanceDetail?.blockVolumeSummary || [])]} columns={['volumeType', 'state', 'device', 'volumeId', 'attachmentId']} onCopy={copyText} />}
      {activeTab === 'console' && (
        <ConsolePanel
          selectedInstance={selectedInstance}
          consolePublicKey={consolePublicKey}
          onConsolePublicKeyChange={setConsolePublicKey}
          onCreateConsoleConnection={createConsoleConnection}
          loadingDetail={loadingDetail}
          instanceDetail={instanceDetail}
          onCopy={copyText}
          isArmed={isArmed}
          deletingConsoleId={deletingConsoleId}
          onDeleteConsoleConnection={deleteConsoleConnection}
        />
      )}

      {activeTab === 'cost' && (
        <CostPanel
          costOverview={costOverview}
          loadingCost={loadingCost}
          onRefresh={() => loadCost({ force: true })}
        />
      )}

      {activeTab === 'accounts' && (
        <AccountsPanel
          accounts={accounts}
          loadingAccounts={loadingAccounts}
          accountImportFileRef={accountImportFileRef}
          onLoadAccountImportFile={loadAccountImportFile}
          onExportAccounts={exportAccounts}
          onOpenAccountImportDialog={openAccountImportDialog}
          onOpenAccountDialog={openAccountDialog}
          onVerifyAccount={verifyAccount}
          onDeleteAccount={deleteAccount}
          isArmed={isArmed}
        />
      )}

      <ResizeDialog
        open={resizeDialogOpen}
        onOpenChange={setResizeDialogOpen}
        selectedInstance={selectedInstance}
        resizeForm={resizeForm}
        setResizeForm={setResizeForm}
        resizeShapeOptions={resizeShapeOptions}
        loadingResizeShapes={loadingResizeShapes}
        selectedResizeShape={selectedResizeShape}
        applyResizeShape={applyResizeShape}
        saveResize={saveResize}
        submittingResize={submittingResize}
        formatInstanceMetric={formatInstanceMetric}
        formatBaselineLabel={formatBaselineLabel}
      />

      <AccountDialog
        open={accountDialogOpen}
        onOpenChange={setAccountDialogOpen}
        editingAccount={editingAccount}
        accountForm={accountForm}
        setAccountForm={setAccountForm}
        accountConfigText={accountConfigText}
        updateAccountConfigText={updateAccountConfigText}
        privateKeyFileRef={privateKeyFileRef}
        uploadPrivateKey={uploadPrivateKey}
        saveAccount={saveAccount}
        submittingAccount={submittingAccount}
      />

      <AccountImportDialog
        open={accountImportDialogOpen}
        onOpenChange={setAccountImportDialogOpen}
        accountImportFileRef={accountImportFileRef}
        accountImportText={accountImportText}
        setAccountImportText={setAccountImportText}
        accountImportFileName={accountImportFileName}
        accountImportOverwrite={accountImportOverwrite}
        setAccountImportOverwrite={setAccountImportOverwrite}
        submitImportAccounts={submitImportAccounts}
        importingAccounts={importingAccounts}
      />

    </PageStack>
  );
}

export default OraclePage;
