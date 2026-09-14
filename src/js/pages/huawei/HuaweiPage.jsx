import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { Tabs } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import {
  EmptyState,
  PageStack,
  SectionCard,
  stickyTabsBaseClass,
} from '../../components/ui/AppPrimitives.jsx';
import useStore from '../../store.js';
import SSHTerminalDialog from './SSHTerminalDialog.jsx';
import {
  Edit,
  Key,
  Play,
  RefreshCw,
  RotateCw,
  Square,
  Terminal,
  Trash,
} from '../../components/Icons.jsx';
import ComputePanel from './ComputePanel.jsx';
import FlexusPanel from './FlexusPanel.jsx';
import DnsPanel from './DnsPanel.jsx';
import NetworkPanel from './NetworkPanel.jsx';
import StoragePanel from './StoragePanel.jsx';
import BillingPanel from './BillingPanel.jsx';
import AccountsPanel from './AccountsPanel.jsx';
import AccountDialog from './AccountDialog.jsx';
import ImportAccountsDialog from './ImportAccountsDialog.jsx';
import FlexusDetailDialog from './FlexusDetailDialog.jsx';
import FlexusOpsDialog from './FlexusOpsDialog.jsx';
import SshCredentialDialog from './SshCredentialDialog.jsx';
import BucketDialog from './BucketDialog.jsx';
import { tabs } from './tabs.jsx';
import { emptyAccountForm } from './constants.js';
import { currentBillCycle, unwrap } from './utils.js';

export default function HuaweiPage() {
  const activeTabInit = useStore((state) => state.activeTab?.['huawei']) || 'compute';
  const setActiveTabState = useStore((state) => state.setActiveTab);
  const [activeTab, setActiveTab] = useState(activeTabInit);

  const [accounts, setAccounts] = useState([]);
  const [projects, setProjects] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [selectedProjectId, setSelectedProjectId] = useState('');
  const [loadingAccounts, setLoadingAccounts] = useState(true);
  const [loadingScope, setLoadingScope] = useState(false);

  const [instances, setInstances] = useState([]);
  const [flexusInstances, setFlexusInstances] = useState([]);
  const [zones, setZones] = useState([]);
  const [eips, setEips] = useState([]);

  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState(emptyAccountForm);
  const [savingAccount, setSavingAccount] = useState(false);
  const [flexusDetail, setFlexusDetail] = useState(null);
  const [flexusOps, setFlexusOps] = useState({ open: false, type: 'rename', instance: null, value: '' });
  const [sshTarget, setSshTarget] = useState(null);
  const [sshCredDialogOpen, setSshCredDialogOpen] = useState(false);
  const [sshCredAccount, setSshCredAccount] = useState(null);
  const [sshCredForm, setSshCredForm] = useState({ sshUser: '', sshPort: 22, sshPrivateKey: '', sshPassword: '' });
  const [savingSshCred, setSavingSshCred] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [importingAccounts, setImportingAccounts] = useState(false);
  const [importText, setImportText] = useState('');
  const [importOverwrite, setImportOverwrite] = useState(false);
  const [buckets, setBuckets] = useState([]);
  const [objects, setObjects] = useState([]);
  const [selectedBucket, setSelectedBucket] = useState(null);
  const [bucketPrefix, setBucketPrefix] = useState('');
  const [billingData, setBillingData] = useState(null);
  const [freeResources, setFreeResources] = useState([]);
  const [billingCycle, setBillingCycle] = useState('');
  const [bucketDialogOpen, setBucketDialogOpen] = useState(false);
  const [bucketForm, setBucketForm] = useState({ name: '' });
  const [savingBucket, setSavingBucket] = useState(false);
  const [uploadingObject, setUploadingObject] = useState(false);
  const objectFileRef = useRef(null);

  const cacheRef = useRef({});

  const apiFetch = useCallback(async (path, options = {}) => {
    const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.success === false) throw new Error(result.error || '请求失败');
    return result;
  }, []);

  const getCachedValue = useCallback((entry, ttl) => {
    if (!entry) return null;
    if (Date.now() - entry.at > ttl) return null;
    return entry.value;
  }, []);

  const loadAccounts = useCallback(async ({ force = false, silent = false } = {}) => {
    const cached = force ? null : getCachedValue(cacheRef.current.accounts, 30000);
    if (cached) {
      setAccounts(cached);
      if (!selectedAccountId && cached.length > 0) setSelectedAccountId(String(cached[0].id));
      return cached;
    }
    if (!silent) setLoadingAccounts(true);
    try {
      const result = await apiFetch('/api/huawei/accounts');
      const list = Array.isArray(unwrap(result)) ? unwrap(result) : [];
      cacheRef.current.accounts = { value: list, at: Date.now() };
      setAccounts(list);
      if (!selectedAccountId && list.length > 0) setSelectedAccountId(String(list[0].id));
      return list;
    } catch (error) {
      if (!silent) toast.error(error.message || '加载华为云账号失败');
      return [];
    } finally {
      if (!silent) setLoadingAccounts(false);
    }
  }, [apiFetch, getCachedValue, selectedAccountId]);

  const loadProjects = useCallback(async (accountId, { force = false } = {}) => {
    if (!accountId) return;
    const cached = force ? null : getCachedValue(cacheRef.current[`projects_${accountId}`], 30000);
    if (cached) {
      setProjects(cached);
      if (!selectedProjectId) setSelectedProjectId('all');
      return cached;
    }
    try {
      const result = await apiFetch(`/api/huawei/accounts/${accountId}/projects`);
      const list = Array.isArray(unwrap(result)) ? unwrap(result) : [];
      cacheRef.current[`projects_${accountId}`] = { value: list, at: Date.now() };
      setProjects(list);
      if (!selectedProjectId) setSelectedProjectId('all');
      return list;
    } catch (error) {
      toast.error(error.message || '加载项目列表失败');
      return [];
    }
  }, [apiFetch, getCachedValue, selectedProjectId]);

  const loadScopeData = useCallback(async ({ force = false } = {}) => {
    if (!selectedAccountId || !selectedProjectId) return;
    if (force) setLoadingScope(true);
    const requests = [];
    if (activeTab === 'compute') {
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/instances`)
          .then((r) => setInstances(Array.isArray(unwrap(r)) ? unwrap(r) : []))
          .catch((e) => toast.error(e.message || '加载实例失败')),
      );
    }
    if (activeTab === 'flexus') {
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/flexus-instances`)
          .then((r) => setFlexusInstances(Array.isArray(unwrap(r)) ? unwrap(r) : []))
          .catch((e) => toast.error(e.message || '加载 Flexus L 实例失败')),
      );
    }
    if (activeTab === 'dns') {
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/dns/zones`)
          .then((r) => setZones(Array.isArray(unwrap(r)) ? unwrap(r) : []))
          .catch((e) => toast.error(e.message || '加载 DNS zone 失败')),
      );
    }
    if (activeTab === 'network') {
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/eips`)
          .then((r) => setEips(Array.isArray(unwrap(r)) ? unwrap(r) : []))
          .catch((e) => toast.error(e.message || '加载弹性公网 IP 失败')),
      );
    }
    if (activeTab === 'storage') {
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets`)
          .then((r) => setBuckets(Array.isArray(unwrap(r)) ? unwrap(r) : []))
          .catch((e) => toast.error(e.message || '加载 OBS 桶失败')),
      );
    }
    if (activeTab === 'billing') {
      const cycle = billingCycle || currentBillCycle();
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/billing/overview?cycle=${cycle}`)
          .then((r) => setBillingData(unwrap(r)))
          .catch((e) => toast.error(e.message || '加载费用概览失败')),
      );
      requests.push(
        apiFetch(`/api/huawei/accounts/${selectedAccountId}/billing/free-resources`)
          .then((r) => setFreeResources(Array.isArray(unwrap(r)) ? unwrap(r) : []))
          .catch((e) => toast.error(e.message || '加载资源包用量失败')),
      );
    }
    await Promise.all(requests);
    if (force) setLoadingScope(false);
  }, [selectedAccountId, selectedProjectId, activeTab, apiFetch, billingCycle]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (selectedAccountId) loadProjects(selectedAccountId);
  }, [selectedAccountId, loadProjects]);

  useEffect(() => {
    if (selectedAccountId && selectedProjectId) loadScopeData();
  }, [selectedAccountId, selectedProjectId, activeTab, loadScopeData]);

  const handleTabChange = (value) => {
    setActiveTab(value);
    setActiveTabState?.('huawei', value);
  };

  const handleAccountChange = (value) => {
    setSelectedAccountId(value);
    setSelectedProjectId('all');
    setProjects([]);
    setInstances([]);
    setFlexusInstances([]);
    setZones([]);
    setEips([]);
    setBuckets([]);
    setObjects([]);
    setSelectedBucket(null);
    setBucketPrefix('');
    setBillingData(null);
    setFreeResources([]);
    cacheRef.current[`projects_${value}`] = null;
  };

  const refresh = () => {
    loadAccounts({ force: true });
    if (selectedAccountId) loadProjects(selectedAccountId, { force: true });
    if (selectedAccountId && selectedProjectId) loadScopeData({ force: true });
  };

  const selectedAccount = useMemo(
    () => accounts.find((a) => String(a.id) === String(selectedAccountId)),
    [accounts, selectedAccountId],
  );

  const runInstanceAction = async (projectId, serverIds, action) => {
    if (!selectedAccountId || !projectId) return;
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${projectId}/instances/actions`, {
        method: 'POST',
        body: JSON.stringify({ action, serverIds }),
      });
      toast.success('指令已下发，请稍后刷新确认');
      setTimeout(() => loadScopeData(), 1500);
    } catch (error) {
      toast.error(error.message || '执行实例动作失败');
    }
  };

  const runFlexusAction = async (instanceId, action) => {
    if (!selectedAccountId) return;
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/flexus-instances/${instanceId}/actions`, {
        method: 'POST',
        body: JSON.stringify({ action }),
      });
      toast.success('指令已下发，请稍后刷新确认');
      setTimeout(() => loadScopeData(), 1500);
    } catch (error) {
      toast.error(error.message || '执行 Flexus L 动作失败');
    }
  };

  const openFlexusDetail = (instance) => setFlexusDetail(instance);

  const openFlexusOps = (type, instance) => setFlexusOps({ open: true, type, instance, value: '' });

  const submitFlexusOps = async () => {
    if (!flexusOps.instance || !flexusOps.value.trim()) {
      toast.error(flexusOps.type === 'rename' ? '请填写新名称' : '请填写新密码');
      return;
    }
    try {
      if (flexusOps.type === 'rename') {
        await apiFetch(`/api/huawei/accounts/${selectedAccountId}/flexus-instances/${flexusOps.instance.id}`, {
          method: 'PUT',
          body: JSON.stringify({ name: flexusOps.value.trim() }),
        });
        toast.success('名称已修改');
      } else {
        await apiFetch(`/api/huawei/accounts/${selectedAccountId}/flexus-instances/${flexusOps.instance.id}/reset-password`, {
          method: 'POST',
          body: JSON.stringify({ newPassword: flexusOps.value }),
        });
        toast.success('密码已重置');
      }
      setFlexusOps((cur) => ({ ...cur, open: false }));
      setTimeout(() => loadScopeData(), 1200);
    } catch (error) {
      toast.error(error.message || '操作失败');
    }
  };

  const formatExpire = (value) => {
    if (!value) return '-';
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    return d.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
  };

  const formatTraffic = (instance) => {
    if (!instance.trafficOriginal) return '-';
    const unit = instance.trafficMeasureId === 10 ? 'GB' : '';
    return `${Math.round(instance.trafficAmount)}/${Math.round(instance.trafficOriginal)}${unit}`;
  };

  const loadObjects = useCallback(async (bucket, prefix = '', marker = '') => {
    if (!selectedAccountId) return;
    const params = new URLSearchParams();
    if (prefix) params.set('prefix', prefix);
    if (marker) params.set('marker', marker);
    if (bucket.region) params.set('region', bucket.region);
    try {
      const result = await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets/${encodeURIComponent(bucket.name)}/objects?${params.toString()}`);
      const data = unwrap(result);
      setObjects(Array.isArray(data.objects) ? data.objects : []);
    } catch (error) {
      toast.error(error.message || '加载对象列表失败');
    }
  }, [selectedAccountId, selectedProjectId, apiFetch]);

  const openBucket = async (bucket) => {
    setSelectedBucket(bucket);
    setBucketPrefix('');
    await loadObjects(bucket);
  };

  const backToBuckets = () => {
    setSelectedBucket(null);
    setObjects([]);
    setBucketPrefix('');
  };

  const createBucket = async () => {
    if (!bucketForm.name.trim()) {
      toast.error('请填写桶名称');
      return;
    }
    setSavingBucket(true);
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets`, {
        method: 'POST',
        body: JSON.stringify({ name: bucketForm.name.trim() }),
      });
      toast.success('桶已创建');
      setBucketDialogOpen(false);
      setBucketForm({ name: '' });
      loadScopeData({ force: true });
    } catch (error) {
      toast.error(error.message || '创建桶失败');
    } finally {
      setSavingBucket(false);
    }
  };

  const deleteBucketAction = async (bucket) => {
    const ok = await dialog.deleteResource({
      title: '删除桶',
      message: `确定删除空桶「${bucket.name}」吗？删除前请确认桶内没有对象。`,
      confirmLabel: '删除桶',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets/${encodeURIComponent(bucket.name)}?region=${encodeURIComponent(bucket.region || '')}`, {
        method: 'DELETE',
      });
      toast.success('桶已删除');
      if (selectedBucket?.name === bucket.name) backToBuckets();
      loadScopeData({ force: true });
    } catch (error) {
      toast.error(error.message || '删除桶失败');
    }
  };

  const deleteObjectAction = async (object) => {
    const ok = await dialog.deleteResource({
      title: '删除对象',
      message: `确定删除对象「${object.name}」吗？`,
      confirmLabel: '删除对象',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets/${encodeURIComponent(selectedBucket.name)}/objects/${encodeURIComponent(object.name)}?region=${encodeURIComponent(selectedBucket.region || '')}`, {
        method: 'DELETE',
      });
      toast.success('对象已删除');
      loadObjects(selectedBucket, bucketPrefix);
    } catch (error) {
      toast.error(error.message || '删除对象失败');
    }
  };

  const handleUploadFile = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file || !selectedBucket) return;
    setUploadingObject(true);
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${selectedProjectId}/buckets/${encodeURIComponent(selectedBucket.name)}/objects?name=${encodeURIComponent(file.name)}&region=${encodeURIComponent(selectedBucket.region || '')}`, {
        method: 'POST',
        body: file,
        headers: { 'Content-Type': file.type || 'application/octet-stream' },
      });
      toast.success('上传成功');
      loadObjects(selectedBucket, bucketPrefix);
    } catch (error) {
      toast.error(error.message || '上传失败');
    } finally {
      setUploadingObject(false);
    }
  };

  const deleteInstance = async (server) => {
    const ok = await dialog.deleteResource({
      title: '删除实例',
      message: `确定删除实例「${server.name}」吗？将同时释放公网 IP 与云硬盘。`,
      confirmLabel: '删除实例',
    });
    if (!ok) return;
    const projectId = server.projectId || selectedProjectId;
    if (!selectedAccountId || !projectId) return;
    try {
      await apiFetch(`/api/huawei/accounts/${selectedAccountId}/projects/${projectId}/instances/${server.id}?deletePublicIp=true&deleteVolume=true`, {
        method: 'DELETE',
      });
      toast.success('实例已删除');
      loadScopeData();
    } catch (error) {
      toast.error(error.message || '删除实例失败');
    }
  };

  const openCreateAccount = () => {
    setEditingAccount(null);
    setAccountForm(emptyAccountForm);
    setAccountDialogOpen(true);
  };

  const openEditAccount = (account) => {
    setEditingAccount(account);
    setAccountForm({
      name: account.name,
      site: account.site || 'cn',
      accessKeyId: '',
      secretAccessKey: '',
      defaultRegion: account.defaultRegion || '',
      defaultProjectId: account.defaultProjectId || '',
      description: account.description || '',
      sshUser: account.sshUser || '',
      sshPort: account.sshPort || 22,
      sshPrivateKey: '',
      sshPassword: '',
    });
    setAccountDialogOpen(true);
  };

  const saveAccount = async () => {
    if (!accountForm.name.trim()) {
      toast.error('请填写账号名称');
      return;
    }
    if (!editingAccount && (!accountForm.accessKeyId.trim() || !accountForm.secretAccessKey.trim())) {
      toast.error('请填写 AK 与 SK');
      return;
    }
    setSavingAccount(true);
    try {
      const body = {
        name: accountForm.name.trim(),
        site: accountForm.site,
        accessKeyId: accountForm.accessKeyId.trim(),
        secretAccessKey: accountForm.secretAccessKey.trim(),
        defaultRegion: accountForm.defaultRegion.trim(),
        defaultProjectId: accountForm.defaultProjectId.trim(),
        description: accountForm.description.trim(),
        sshUser: accountForm.sshUser.trim(),
        sshPort: Number(accountForm.sshPort) || 22,
        sshPrivateKey: accountForm.sshPrivateKey,
        sshPassword: accountForm.sshPassword,
      };
      await apiFetch(editingAccount ? `/api/huawei/accounts/${editingAccount.id}` : '/api/huawei/accounts', {
        method: editingAccount ? 'PUT' : 'POST',
        body: JSON.stringify(body),
      });
      toast.success(editingAccount ? '账号已更新' : '账号已添加');
      setAccountDialogOpen(false);
      loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '保存账号失败');
    } finally {
      setSavingAccount(false);
    }
  };

  const openSshDialog = (account) => {
    setSshCredAccount(account);
    setSshCredForm({
      sshUser: account.sshUser || '',
      sshPort: account.sshPort || 22,
      sshPrivateKey: '',
      sshPassword: '',
    });
    setSshCredDialogOpen(true);
  };

  const saveSshCred = async () => {
    if (!sshCredAccount) return;
    setSavingSshCred(true);
    try {
      await apiFetch(`/api/huawei/accounts/${sshCredAccount.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          sshUser: sshCredForm.sshUser.trim(),
          sshPort: Number(sshCredForm.sshPort) || 22,
          sshPrivateKey: sshCredForm.sshPrivateKey,
          sshPassword: sshCredForm.sshPassword,
        }),
      });
      toast.success('SSH 凭据已保存');
      setSshCredDialogOpen(false);
      loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '保存 SSH 凭据失败');
    } finally {
      setSavingSshCred(false);
    }
  };

  const verifyAccount = async (account) => {
    try {
      const result = await apiFetch(`/api/huawei/accounts/${account.id}/verify`, { method: 'POST' });
      toast.success(result.message || '账号验证成功');
      loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '验证账号失败');
    }
  };

  const exportAccountsAction = async () => {
    try {
      const result = await apiFetch('/api/huawei/export/accounts');
      const blob = new Blob([JSON.stringify(result, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `huawei-accounts-${new Date().toISOString().slice(0, 10)}.json`;
      link.click();
      URL.revokeObjectURL(url);
      toast.success('已导出账号清单（含敏感凭据，请妥善保管）');
    } catch (error) {
      toast.error(error.message || '导出失败');
    }
  };

  const submitImport = async () => {
    let parsed;
    try {
      parsed = JSON.parse(importText);
    } catch {
      toast.error('JSON 解析失败');
      return;
    }
    const accounts = Array.isArray(parsed) ? parsed : parsed?.accounts;
    if (!Array.isArray(accounts) || accounts.length === 0) {
      toast.error('未找到 accounts 数组');
      return;
    }
    setImportingAccounts(true);
    try {
      await apiFetch('/api/huawei/import/accounts', {
        method: 'POST',
        body: JSON.stringify({ accounts, overwrite: importOverwrite }),
      });
      toast.success(`已导入 ${accounts.length} 个账号`);
      setImportDialogOpen(false);
      setImportText('');
      setImportOverwrite(false);
      loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '导入失败');
    } finally {
      setImportingAccounts(false);
    }
  };

  const deleteAccount = async (account) => {
    const ok = await dialog.deleteResource({
      title: '删除华为云账号',
      message: `确定删除账号「${account.name}」吗？相关凭证将永久移除。`,
      confirmLabel: '删除账号',
    });
    if (!ok) return;
    try {
      await apiFetch(`/api/huawei/accounts/${account.id}`, { method: 'DELETE' });
      toast.success('账号已删除');
      if (String(account.id) === String(selectedAccountId)) {
        setSelectedAccountId('');
        setSelectedProjectId('');
        setProjects([]);
      }
      loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '删除账号失败');
    }
  };

  const accountOptions = accounts.map((a) => ({ value: String(a.id), label: a.name }));
  const projectOptions = [{ value: 'all', label: '全部区域' }, ...projects.map((p) => ({ value: p.projectId, label: p.name || p.projectId }))];

  const scopeReady = Boolean(selectedAccountId);

  const cycleOptions = useMemo(() => {
    const options = [];
    const now = new Date(Date.now() + 8 * 3600 * 1000);
    for (let i = 0; i < 6; i += 1) {
      const m = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - i, 1));
      const value = `${m.getUTCFullYear()}-${String(m.getUTCMonth() + 1).padStart(2, '0')}`;
      options.push({ value, label: value });
    }
    return options;
  }, []);

  const renderInstanceActions = (instance) => {
    const projectId = instance.projectId || selectedProjectId;
    const sshHost = instance.publicIp || instance.privateIp;
    return (
      <div className="flex items-center justify-end gap-1">
        {sshHost && (
          <Button type="button" size="sm" shape="square" variant="secondary" title="SSH 终端" aria-label="SSH 终端" onClick={() => setSshTarget({ accountId: selectedAccountId, instance: { name: instance.name, publicIp: sshHost } })}><Terminal className="h-4 w-4" /></Button>
        )}
        {instance.status === 'SHUTOFF' && (
          <Button type="button" size="sm" shape="square" variant="secondary" title="启动" aria-label="启动" onClick={() => runInstanceAction(projectId, [instance.id], 'start')}><Play className="h-4 w-4" /></Button>
        )}
        {instance.status === 'ACTIVE' && (
          <>
            <Button type="button" size="sm" shape="square" variant="secondary" title="停止" aria-label="停止" onClick={() => runInstanceAction(projectId, [instance.id], 'stop')}><Square className="h-4 w-4" /></Button>
            <Button type="button" size="sm" shape="square" variant="secondary" title="重启" aria-label="重启" onClick={() => runInstanceAction(projectId, [instance.id], 'reboot')}><RotateCw className="h-4 w-4" /></Button>
          </>
        )}
        <Button type="button" size="sm" shape="square" variant="destructive" title="删除" aria-label="删除" onClick={() => deleteInstance(instance)}><Trash className="h-4 w-4" /></Button>
      </div>
    );
  };

  const renderFlexusActions = (instance) => (
    <div className="flex items-center justify-end gap-1">
      {instance.serverStatus === 'SHUTOFF' && (
        <Button type="button" size="sm" shape="square" variant="secondary" title="启动" aria-label="启动" onClick={() => runFlexusAction(instance.id, 'start')}><Play className="h-4 w-4" /></Button>
      )}
      {instance.serverStatus === 'ACTIVE' && (
        <>
          <Button type="button" size="sm" shape="square" variant="secondary" title="停止" aria-label="停止" onClick={() => runFlexusAction(instance.id, 'stop')}><Square className="h-4 w-4" /></Button>
          <Button type="button" size="sm" shape="square" variant="secondary" title="重启" aria-label="重启" onClick={() => runFlexusAction(instance.id, 'reboot')}><RotateCw className="h-4 w-4" /></Button>
        </>
      )}
      <Button type="button" size="sm" shape="square" variant="secondary" title="重置密码" aria-label="重置密码" onClick={() => openFlexusOps('password', instance)}><Key className="h-4 w-4" /></Button>
      <Button type="button" size="sm" shape="square" variant="secondary" title="改名" aria-label="改名" onClick={() => openFlexusOps('rename', instance)}><Edit className="h-4 w-4" /></Button>
    </div>
  );

  return (
    <PageStack>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs {...MODULE_TABS_PROPS} value={activeTab} onValueChange={handleTabChange} tabs={tabs} />
        {activeTab !== 'accounts' && (
          <div className="flex items-center gap-2">
            {activeTab === 'billing' && (
              <Select alignItemWithTrigger size="sm" aria-label="账单月份" value={billingCycle} onValueChange={setBillingCycle} items={cycleOptions} placeholder="选择月份" />
            )}
            <Select alignItemWithTrigger size="sm" aria-label="华为云账号" value={selectedAccountId} onValueChange={handleAccountChange} items={accountOptions} placeholder="选择账号" />
            <Select alignItemWithTrigger size="sm" aria-label="区域/项目" value={selectedProjectId} onValueChange={setSelectedProjectId} items={projectOptions} placeholder="选择项目" />
            <Button type="button" size="sm" variant="secondary" onClick={refresh} title="刷新">
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
        )}
      </div>

      <div className="flex flex-col gap-3">
        {activeTab === 'compute' && (
          <ComputePanel
            loadingScope={loadingScope}
            scopeReady={scopeReady}
            instances={instances}
            renderInstanceActions={renderInstanceActions}
          />
        )}

        {activeTab === 'flexus' && (
          <FlexusPanel
            loadingScope={loadingScope}
            selectedAccountId={selectedAccountId}
            flexusInstances={flexusInstances}
            formatExpire={formatExpire}
            formatTraffic={formatTraffic}
            openFlexusDetail={openFlexusDetail}
            renderFlexusActions={renderFlexusActions}
          />
        )}

        {activeTab === 'dns' && (
          <DnsPanel loadingScope={loadingScope} scopeReady={scopeReady} zones={zones} />
        )}

        {activeTab === 'network' && (
          <NetworkPanel loadingScope={loadingScope} scopeReady={scopeReady} eips={eips} />
        )}

        {activeTab === 'storage' && (
          <StoragePanel
            loadingScope={loadingScope}
            selectedBucket={selectedBucket}
            objects={objects}
            buckets={buckets}
            selectedAccountId={selectedAccountId}
            uploadingObject={uploadingObject}
            objectFileRef={objectFileRef}
            handleUploadFile={handleUploadFile}
            backToBuckets={backToBuckets}
            openBucket={openBucket}
            deleteObjectAction={deleteObjectAction}
            deleteBucketAction={deleteBucketAction}
            setBucketDialogOpen={setBucketDialogOpen}
          />
        )}

        {activeTab === 'billing' && (
          <BillingPanel
            loadingScope={loadingScope}
            billingData={billingData}
            freeResources={freeResources}
            formatExpire={formatExpire}
          />
        )}

        {activeTab === 'accounts' && (
          <AccountsPanel
            loadingAccounts={loadingAccounts}
            accounts={accounts}
            exportAccountsAction={exportAccountsAction}
            setImportDialogOpen={setImportDialogOpen}
            openCreateAccount={openCreateAccount}
            openSshDialog={openSshDialog}
            verifyAccount={verifyAccount}
            openEditAccount={openEditAccount}
            deleteAccount={deleteAccount}
          />
        )}
      </div>

      <FlexusDetailDialog
        flexusDetail={flexusDetail}
        onOpenChange={(open) => { if (!open) setFlexusDetail(null); }}
        formatExpire={formatExpire}
        selectedAccountId={selectedAccountId}
        setSshTarget={setSshTarget}
      />

      <FlexusOpsDialog flexusOps={flexusOps} setFlexusOps={setFlexusOps} submitFlexusOps={submitFlexusOps} />

      <SshCredentialDialog
        open={sshCredDialogOpen}
        onOpenChange={setSshCredDialogOpen}
        sshCredAccount={sshCredAccount}
        sshCredForm={sshCredForm}
        setSshCredForm={setSshCredForm}
        saveSshCred={saveSshCred}
        savingSshCred={savingSshCred}
      />

      <ImportAccountsDialog
        open={importDialogOpen}
        onOpenChange={setImportDialogOpen}
        importText={importText}
        setImportText={setImportText}
        importOverwrite={importOverwrite}
        setImportOverwrite={setImportOverwrite}
        submitImport={submitImport}
        importingAccounts={importingAccounts}
      />

      <BucketDialog
        open={bucketDialogOpen}
        onOpenChange={setBucketDialogOpen}
        bucketForm={bucketForm}
        setBucketForm={setBucketForm}
        createBucket={createBucket}
        savingBucket={savingBucket}
      />

      <AccountDialog
        open={accountDialogOpen}
        onOpenChange={setAccountDialogOpen}
        editingAccount={editingAccount}
        accountForm={accountForm}
        setAccountForm={setAccountForm}
        saveAccount={saveAccount}
        savingAccount={savingAccount}
      />

      {sshTarget && (
        <SSHTerminalDialog
          accountId={sshTarget.accountId}
          instance={sshTarget.instance}
          onClose={() => setSshTarget(null)}
        />
      )}
    </PageStack>
  );
}
