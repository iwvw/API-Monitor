import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  KeyValueGrid,
  PageStack,
  SectionCard,
  StatusBadge,
  stickyTabsBaseClass,
} from '../components/ui/AppPrimitives.jsx';
import useStore from '../store.js';
import SSHTerminalDialog from './huawei/SSHTerminalDialog.jsx';
import {
  ArrowLeft,
  Cloud,
  Download,
  Edit,
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
  Square,
  Terminal,
  Trash,
  Upload,
} from '../components/Icons.jsx';

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

const unwrap = (result) => result?.data ?? result ?? {};

const currentBillCycle = () => {
  const d = new Date(Date.now() + 8 * 3600 * 1000);
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`;
};

const formatBytes = (bytes) => {
  if (!bytes && bytes !== 0) return '-';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = -1;
  do { value /= 1024; unit += 1; } while (value >= 1024 && unit < units.length - 1);
  return `${value.toFixed(1)} ${units[unit]}`;
};

const formatMoney = (value) => {
  const num = Number(value) || 0;
  return num.toFixed(2);
};

const tabs = [
  { value: 'compute', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />计算实例</span> },
  { value: 'flexus', label: <span className="inline-flex items-center gap-1.5"><Cloud className="h-3.5 w-3.5" />Flexus L</span> },
  { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />域名解析</span> },
  { value: 'network', label: <span className="inline-flex items-center gap-1.5"><Layers className="h-3.5 w-3.5" />网络</span> },
  { value: 'storage', label: <span className="inline-flex items-center gap-1.5"><HardDrive className="h-3.5 w-3.5" />存储</span> },
  { value: 'billing', label: <span className="inline-flex items-center gap-1.5"><PieChart className="h-3.5 w-3.5" />费用</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />账号管理</span> },
];

const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 200, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier', width: 180, minWidth: 150 },
  { id: 'flavorName', role: 'meta', width: 160 },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'createdAt', role: 'datetime' },
  { id: 'actions', role: 'actions-lg', width: 150 },
];

const FLEXUS_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 180, minWidth: 150 },
  { id: 'serverStatus', role: 'status' },
  { id: 'specDescription', role: 'meta', grow: 1, minWidth: 200 },
  { id: 'expireAt', role: 'datetime' },
  { id: 'traffic', role: 'meta', width: 130 },
  { id: 'actions', role: 'actions-lg', width: 150 },
];

const DNS_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 220, minWidth: 180 },
  { id: 'type', role: 'type' },
  { id: 'recordNum', role: 'count' },
  { id: 'status', role: 'status' },
  { id: 'createdAt', role: 'datetime', grow: 1 },
  { id: 'actions', role: 'actions-md', width: 96 },
];

const EIP_TABLE_COLUMNS = [
  { id: 'publicIp', role: 'primary', width: 200, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'bandwidth', role: 'number' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
];

const ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'site', role: 'meta', width: 100 },
  { id: 'accessKeyId', role: 'identifier', width: 180, minWidth: 150 },
  { id: 'defaultRegion', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 180, maxWidth: 220 },
];

const BUCKET_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 220, minWidth: 180 },
  { id: 'region', role: 'meta', width: 160 },
  { id: 'createdAt', role: 'datetime' },
  { id: 'actions', role: 'actions-md', width: 96 },
];

const OBJECT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', grow: 1, minWidth: 200 },
  { id: 'size', role: 'number' },
  { id: 'lastModified', role: 'datetime' },
  { id: 'actions', role: 'actions-md', width: 96 },
];

const BILLING_SUM_COLUMNS = [
  { id: 'serviceTypeName', role: 'primary', grow: 1, minWidth: 160 },
  { id: 'resourceTypeName', role: 'meta', width: 160 },
  { id: 'consumeAmount', role: 'number' },
];

const FREE_RESOURCE_COLUMNS = [
  { id: 'typeName', role: 'primary', grow: 1, minWidth: 160 },
  { id: 'traffic', role: 'meta', width: 140 },
  { id: 'endTime', role: 'datetime' },
];

const emptyAccountForm = {
  name: '',
  site: 'cn',
  accessKeyId: '',
  secretAccessKey: '',
  defaultRegion: '',
  defaultProjectId: '',
  description: '',
  sshUser: '',
  sshPort: 22,
  sshPrivateKey: '',
  sshPassword: '',
};

function getStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['ACTIVE', 'SUCCESS', 'IN_USE', 'RUNNING', 'NORMAL', 'BIND', 'ENABLE'].includes(normalized)) return 'success';
  if (['SHUTOFF', 'STOPPED', 'FREE', 'DISABLE', 'ERROR', 'REBOOT'].includes(normalized)) return 'danger';
  if (['REBOOT', 'BUILD', 'STOPPING', 'PENDING'].includes(normalized)) return 'info';
  return 'neutral';
}

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
    const response = await fetch(path, { ...options, headers: { ...getAuthHeaders(), ...(options.headers || {}) } });
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

  const verifyAccount = async (account) => {
    try {
      const result = await apiFetch(`/api/huawei/accounts/${account.id}/verify`, { method: 'POST' });
      toast.success(result.message || '账号验证成功');
      loadAccounts({ force: true });
    } catch (error) {
      toast.error(error.message || '验证账号失败');
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
          <SectionCard title="ECS 实例" bodyPadding="none">
            {loadingScope ? (
              <SkeletonLines />
            ) : !scopeReady ? (
              <EmptyState card={false} icon={Server} title="请选择账号与项目" description="选择华为云账号和区域项目后展示实例列表" className="min-h-64" />
            ) : instances.length === 0 ? (
              <EmptyState card={false} icon={Server} title="暂无实例" description="该项目下没有 ECS 实例" className="min-h-64" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" >
                <AppTable tableId="huawei-instances" columns={INSTANCE_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>公网 IP</Table.Head>
                      <Table.Head>规格</Table.Head>
                      <Table.Head>区域</Table.Head>
                      <Table.Head>创建时间</Table.Head>
                      <Table.Head>操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {instances.map((instance) => (
                      <Table.Row key={instance.id}>
                        <Table.Cell>
                          <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={instance.name}>{instance.name || '-'}</Button>
                        </Table.Cell>
                        <Table.Cell><StatusBadge tone={getStatusTone(instance.status)}>{instance.status || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell><span className="truncate font-mono text-xs" title={instance.publicIp || instance.privateIp}>{instance.publicIp || instance.privateIp || '-'}</span></Table.Cell>
                        <Table.Cell>
                          <span className="truncate text-sm text-kumo-strong" title={instance.flavorName}>{instance.flavorName || '-'}</span>
                          {instance.vcpus > 0 && <span className="ml-1.5 shrink-0 whitespace-nowrap text-xs text-kumo-subtle">{instance.vcpus} vCPU</span>}
                        </Table.Cell>
                        <Table.Cell className="truncate text-sm text-kumo-strong" title={instance.region}>{instance.region || '-'}</Table.Cell>
                        <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{instance.createdAt || '-'}</Table.Cell>
                        <Table.Cell>{renderInstanceActions(instance)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
        )}

        {activeTab === 'flexus' && (
          <SectionCard title="Flexus L 实例" bodyPadding="none">
            {loadingScope ? (
              <SkeletonLines />
            ) : !selectedAccountId ? (
              <EmptyState card={false} icon={Cloud} title="请选择账号" description="选择华为云账号后展示 Flexus L 实例" className="min-h-64" />
            ) : flexusInstances.length === 0 ? (
              <EmptyState card={false} icon={Cloud} title="暂无 Flexus L 实例" description="该账号下没有 Flexus L 实例" className="min-h-64" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" >
                <AppTable tableId="huawei-flexus" columns={FLEXUS_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>规格</Table.Head>
                      <Table.Head>到期时间</Table.Head>
                      <Table.Head>流量</Table.Head>
                      <Table.Head className="app-table-action">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {flexusInstances.map((instance) => (
                      <Table.Row key={instance.id}>
                        <Table.Cell>
                          <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={`${instance.name} · 点击查看详情`} onClick={() => openFlexusDetail(instance)}>{instance.name || '-'}</Button>
                        </Table.Cell>
                        <Table.Cell><StatusBadge tone={getStatusTone(instance.serverStatus)}>{instance.serverStatus || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell>
                          <div className="min-w-0">
                            <div className="truncate text-sm text-kumo-strong" title={instance.specDescription || instance.specCode}>{instance.specDescription || instance.specCode || '-'}</div>
                            {instance.regionId && <div className="truncate text-[11px] text-kumo-subtle">{instance.regionId}</div>}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{formatExpire(instance.expireAt)}</Table.Cell>
                        <Table.Cell className="whitespace-nowrap text-xs">{formatTraffic(instance)}</Table.Cell>
                        <Table.Cell>{renderFlexusActions(instance)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
        )}

        {activeTab === 'dns' && (
          <SectionCard title="云解析 Zone" bodyPadding="none">
            {loadingScope ? (
              <SkeletonLines />
            ) : !scopeReady ? (
              <EmptyState card={false} icon={Globe} title="请选择账号与项目" description="选择华为云账号和区域项目后展示域名解析" className="min-h-64" />
            ) : zones.length === 0 ? (
              <EmptyState card={false} icon={Globe} title="暂无 Zone" description="该项目下没有 DNS zone" className="min-h-64" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" >
                <AppTable tableId="huawei-dns" columns={DNS_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>域名</Table.Head>
                      <Table.Head>类型</Table.Head>
                      <Table.Head>记录数</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>创建时间</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {zones.map((zone) => (
                      <Table.Row key={zone.id}>
                        <Table.Cell>
                          <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={zone.name}>{zone.name || '-'}</Button>
                        </Table.Cell>
                        <Table.Cell>{zone.type === 'public' ? '公网' : zone.type === 'private' ? '内网' : zone.type || '-'}</Table.Cell>
                        <Table.Cell>{zone.recordNum ?? '-'}</Table.Cell>
                        <Table.Cell><StatusBadge tone={getStatusTone(zone.status)}>{zone.status || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{zone.createdAt || '-'}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
        )}

        {activeTab === 'network' && (
          <SectionCard title="弹性公网 IP" bodyPadding="none">
            {loadingScope ? (
              <SkeletonLines />
            ) : !scopeReady ? (
              <EmptyState card={false} icon={Layers} title="请选择账号与项目" description="选择华为云账号和区域项目后展示网络资源" className="min-h-64" />
            ) : eips.length === 0 ? (
              <EmptyState card={false} icon={Layers} title="暂无弹性公网 IP" description="该项目下没有公网 IP" className="min-h-64" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" >
                <AppTable tableId="huawei-eips" columns={EIP_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>公网 IP</Table.Head>
                      <Table.Head>状态</Table.Head>
                      <Table.Head>带宽</Table.Head>
                      <Table.Head>区域</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {eips.map((eip) => (
                      <Table.Row key={eip.id}>
                        <Table.Cell>
                          <span className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={eip.publicIp}>{eip.publicIp || '-'}</span>
                        </Table.Cell>
                        <Table.Cell><StatusBadge tone={getStatusTone(eip.status)}>{eip.status || '-'}</StatusBadge></Table.Cell>
                        <Table.Cell>{eip.bandwidth > 0 ? `${eip.bandwidth} Mbps` : '-'}</Table.Cell>
                        <Table.Cell className="truncate text-sm text-kumo-strong">{eip.region || '-'}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            )}
          </SectionCard>
        )}

        {activeTab === 'storage' && (
          selectedBucket ? (
            <SectionCard
              title={`桶：${selectedBucket.name}`}
              bodyPadding="none"
              actions={(
                <>
                  <Button type="button" size="sm" variant="secondary" onClick={backToBuckets}><ArrowLeft className="h-4 w-4" />返回桶列表</Button>
                  <Button type="button" size="sm" onClick={() => objectFileRef.current?.click()} disabled={uploadingObject}><Upload className="h-4 w-4" />{uploadingObject ? '上传中…' : '上传'}</Button>
                </>
              )}
            >
              <input ref={objectFileRef} type="file" className="hidden" onChange={handleUploadFile} />
              {loadingScope ? (
                <SkeletonLines />
              ) : objects.length === 0 ? (
                <EmptyState card={false} icon={HardDrive} title="桶为空" description="点击「上传」添加对象，或返回桶列表" className="min-h-64" />
              ) : (
                <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                  <AppTable tableId="huawei-objects" columns={OBJECT_TABLE_COLUMNS}>
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>对象</Table.Head>
                        <Table.Head>大小</Table.Head>
                        <Table.Head>最后修改</Table.Head>
                        <Table.Head className="app-table-action">操作</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {objects.map((object) => (
                        <Table.Row key={object.name}>
                          <Table.Cell><span className="block max-w-full truncate font-mono text-xs" title={object.name}>{object.name}</span></Table.Cell>
                          <Table.Cell>{formatBytes(object.size)}</Table.Cell>
                          <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{object.lastModified || '-'}</Table.Cell>
                          <Table.Cell>
                            <div className="flex items-center justify-end gap-1">
                              <Button type="button" size="sm" shape="square" variant="destructive" title="删除" aria-label="删除" onClick={() => deleteObjectAction(object)}><Trash className="h-4 w-4" /></Button>
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </SectionCard>
          ) : (
            <SectionCard
              title="OBS 桶"
              bodyPadding="none"
              actions={(
                <Button type="button" size="sm" variant="primary" onClick={() => setBucketDialogOpen(true)}><Plus className="h-4 w-4" />新建桶</Button>
              )}
            >
              {loadingScope ? (
                <SkeletonLines />
              ) : !selectedAccountId ? (
                <EmptyState card={false} icon={HardDrive} title="请选择账号" description="选择华为云账号后展示对象存储" className="min-h-64" />
              ) : buckets.length === 0 ? (
                <EmptyState card={false} icon={HardDrive} title="暂无桶" description="点击「新建桶」创建对象存储桶" className="min-h-64" />
              ) : (
                <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                  <AppTable tableId="huawei-buckets" columns={BUCKET_TABLE_COLUMNS}>
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>桶名称</Table.Head>
                        <Table.Head>区域</Table.Head>
                        <Table.Head>创建时间</Table.Head>
                        <Table.Head className="app-table-action">操作</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {buckets.map((bucket) => (
                        <Table.Row key={bucket.name}>
                          <Table.Cell>
                            <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={`${bucket.name} · 点击进入`} onClick={() => openBucket(bucket)}>{bucket.name}</Button>
                          </Table.Cell>
                          <Table.Cell className="truncate text-sm text-kumo-strong">{bucket.region || '-'}</Table.Cell>
                          <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{bucket.createdAt || '-'}</Table.Cell>
                          <Table.Cell>
                            <div className="flex items-center justify-end gap-1">
                              <Button type="button" size="sm" shape="square" variant="destructive" title="删除" aria-label="删除" onClick={() => deleteBucketAction(bucket)}><Trash className="h-4 w-4" /></Button>
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </SectionCard>
          )
        )}

        {activeTab === 'billing' && (
          <div className="columns-1 gap-3 cq-md:columns-2 [&>*]:mb-3 [&>*]:break-inside-avoid">
            <SectionCard title="费用概览">
              {loadingScope ? (
                <SkeletonLines />
              ) : !billingData ? (
                <EmptyState card={false} icon={PieChart} title="暂无费用数据" description="切换到有数据的账单月份查看费用概览" className="min-h-48" />
              ) : (
                <KeyValueGrid
                  items={[
                    { label: '账期', value: billingData.cycle || '-' },
                    { label: '账户余额', value: `${formatMoney((billingData.balances || []).reduce((sum, b) => sum + (b.amount || 0), 0))} 元` },
                    { label: '当月总消费', value: `${formatMoney(billingData.totalConsume)} 元` },
                    { label: '欠费', value: `${formatMoney(billingData.debtAmount)} 元` },
                  ]}
                />
              )}
            </SectionCard>

            <SectionCard title="按服务消费" bodyPadding="none">
              {loadingScope ? (
                <SkeletonLines />
              ) : !billingData?.monthlySums?.length ? (
                <EmptyState card={false} icon={PieChart} title="该账期暂无消费明细" description="未产生费用或数据未出账" className="min-h-48" />
              ) : (
                <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                  <AppTable tableId="huawei-billing-sum" columns={BILLING_SUM_COLUMNS}>
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>服务</Table.Head>
                        <Table.Head>资源类型</Table.Head>
                        <Table.Head>消费金额</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {billingData.monthlySums.map((sum, index) => (
                        <Table.Row key={`${sum.serviceTypeName}-${index}`}>
                          <Table.Cell><span className="block max-w-full truncate font-medium text-kumo-strong">{sum.serviceTypeName || '-'}</span></Table.Cell>
                          <Table.Cell className="truncate text-sm text-kumo-strong">{sum.resourceTypeName || '-'}</Table.Cell>
                          <Table.Cell className="whitespace-nowrap">{formatMoney(sum.consumeAmount)} 元</Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </SectionCard>

            <SectionCard title="Flexus 流量包" bodyPadding="none">
              {loadingScope ? (
                <SkeletonLines />
              ) : freeResources.length === 0 ? (
                <EmptyState card={false} icon={PieChart} title="暂无流量包" description="账号下没有可查询的流量包资源" className="min-h-48" />
              ) : (
                <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                  <AppTable tableId="huawei-free-resources" columns={FREE_RESOURCE_COLUMNS}>
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>资源包</Table.Head>
                        <Table.Head>剩余 / 总量</Table.Head>
                        <Table.Head>当期周期</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {freeResources.map((fr) => (
                        <Table.Row key={fr.freeResourceId}>
                          <Table.Cell><span className="block max-w-full truncate font-medium text-kumo-strong">{fr.typeName || '-'}</span></Table.Cell>
                          <Table.Cell className="whitespace-nowrap">{Math.round(fr.amount)} / {Math.round(fr.originalAmount)} GB</Table.Cell>
                          <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{formatExpire(fr.endTime)}</Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </SectionCard>
          </div>
        )}

        {activeTab === 'accounts' && (
          <SectionCard
            title="华为云账号"
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
                <AppTable tableId="huawei-accounts-loading" columns={ACCOUNT_TABLE_COLUMNS}>
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
              <EmptyState card={false} icon={Key} title="暂无华为云账号" description="新增账号并填写 AK/SK 开始使用" className="min-h-64" />
            ) : (
              <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
                <AppTable tableId="huawei-accounts" columns={ACCOUNT_TABLE_COLUMNS}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>名称</Table.Head>
                      <Table.Head>站点</Table.Head>
                      <Table.Head>AK</Table.Head>
                      <Table.Head>默认区域</Table.Head>
                      <Table.Head>验证状态</Table.Head>
                      <Table.Head className="app-table-action">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {accounts.map((account) => (
                      <Table.Row key={account.id}>
                        <Table.Cell>
                          <span className="block max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={account.name}>{account.name}</span>
                        </Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{account.site === 'intl' ? '国际站' : '国内站'}</Table.Cell>
                        <Table.Cell className="truncate font-mono text-xs" title={account.accessKeyId}>{account.accessKeyId || '-'}</Table.Cell>
                        <Table.Cell className="text-sm text-kumo-strong">{account.defaultRegion || '-'}</Table.Cell>
                        <Table.Cell>
                          <StatusBadge tone={account.lastVerifyStatus === 'success' ? 'success' : account.lastVerifyStatus === 'failed' ? 'danger' : 'neutral'}>
                            {account.lastVerifyStatus === 'success' ? '已验证' : account.lastVerifyStatus === 'failed' ? '验证失败' : '未验证'}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="inline-flex items-center gap-2">
                            <Button type="button" size="sm" variant="secondary" onClick={() => verifyAccount(account)}>验证</Button>
                            <Button type="button" size="sm" variant="secondary" onClick={() => openEditAccount(account)}>编辑</Button>
                            <Button type="button" size="sm" variant="destructive" onClick={() => deleteAccount(account)}><Trash className="h-4 w-4" /></Button>
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

      <Dialog.Root open={Boolean(flexusDetail)} onOpenChange={(open) => { if (!open) setFlexusDetail(null); }}>
        <Dialog className="@container !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{flexusDetail?.name || 'Flexus L 详情'}</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">套餐组合与运行信息，全部来自华为云 API，无需登录官网。</Dialog.Description>
          {flexusDetail && (
            <div className="flex flex-col gap-3">
              <KeyValueGrid
                items={[
                  {
                    label: '运行状态',
                    value: <StatusBadge tone={getStatusTone(flexusDetail.serverStatus)}>{flexusDetail.serverStatus || '-'}</StatusBadge>,
                  },
                  { label: '区域 / 项目', value: `${flexusDetail.regionId || '-'} / ${flexusDetail.projectId || '-'}` },
                  {
                    label: '规格',
                    value: (
                      <div className="min-w-0">
                        <div className="truncate" title={flexusDetail.specDescription}>{flexusDetail.specDescription || '-'}</div>
                        {flexusDetail.specCode && <div className="truncate font-mono text-[11px] text-kumo-subtle">{flexusDetail.specCode}</div>}
                        {flexusDetail.vcpus > 0 && <div className="text-xs text-kumo-subtle">{flexusDetail.vcpus} vCPU · {flexusDetail.memoryMb} MB</div>}
                      </div>
                    ),
                  },
                  { label: '云主机', value: flexusDetail.cloudServerName || flexusDetail.cloudServerId || '-' },
                  { label: '公网 IP', value: flexusDetail.publicIp || '-' },
                  { label: '私网 IP', value: flexusDetail.privateIp || '-' },
                  { label: '镜像', value: flexusDetail.imageName || '-' },
                  { label: '计费', value: flexusDetail.chargeMode === 'prePaid' ? '包年包月' : flexusDetail.chargeMode || '-' },
                  { label: '订单号', value: <code className="text-xs">{flexusDetail.orderId || '-'}</code> },
                  { label: '创建时间', value: flexusDetail.createdAt || '-' },
                  { label: '到期时间', value: <span className="font-medium">{formatExpire(flexusDetail.expireAt)}</span> },
                  {
                    label: '流量包',
                    value: flexusDetail.trafficOriginal
                      ? `${flexusDetail.trafficTypeName || '流量'}：${Math.round(flexusDetail.trafficAmount)} / ${Math.round(flexusDetail.trafficOriginal)} GB（当期至 ${formatExpire(flexusDetail.trafficExpireAt)}）`
                      : '-',
                  },
                ]}
              />
              {flexusDetail.composedResources?.length > 0 && (
                <div>
                  <div className="mb-1 text-xs font-medium text-kumo-subtle">套餐组成</div>
                  <div className="flex flex-wrap gap-1.5">
                    {flexusDetail.composedResources.map((res) => (
                      <span key={res.id || res.name} className="inline-flex items-center gap-1 rounded-md border border-kumo-line bg-kumo-base px-2 py-0.5 text-xs">
                        <code className="max-w-[16rem] truncate" title={res.id}>{res.name || res.id || res.typeName}</code>
                      </span>
                    ))}
                  </div>
                </div>
              )}
              {flexusDetail.publicIp && (
                <div className="mt-1 flex items-center gap-2">
                  <Button type="button" size="sm" onClick={() => setSshTarget({ accountId: selectedAccountId, instance: { name: flexusDetail.name, publicIp: flexusDetail.publicIp } })}>
                    <Terminal className="mr-1 h-4 w-4" />SSH 终端
                  </Button>
                </div>
              )}
            </div>
          )}
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={flexusOps.open} onOpenChange={(open) => setFlexusOps((cur) => ({ ...cur, open }))}>
        <Dialog className="@container !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{flexusOps.type === 'rename' ? '修改实例名称' : '重置实例密码'}</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">Flexus L 实例「{flexusOps.instance?.name || '-'}」</Dialog.Description>
          <div className="flex flex-col gap-3">
            {flexusOps.type === 'rename' ? (
              <Input label="新名称" value={flexusOps.value} onChange={(e) => setFlexusOps((cur) => ({ ...cur, value: e.target.value }))} placeholder="请输入新名称" />
            ) : (
              <Input label="新密码" type="password" value={flexusOps.value} onChange={(e) => setFlexusOps((cur) => ({ ...cur, value: e.target.value }))} placeholder="请输入新密码" />
            )}
            <div className="mt-2 flex items-center justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setFlexusOps((cur) => ({ ...cur, open: false }))}>取消</Button>
              <Button size="sm" onClick={submitFlexusOps}>确定</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={bucketDialogOpen} onOpenChange={setBucketDialogOpen}>
        <Dialog className="@container !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">新建 OBS 桶</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">桶名称全网唯一，创建后不可重名；将创建于当前所选区域。</Dialog.Description>
          <div className="flex flex-col gap-3">
            <Input label="桶名称" value={bucketForm.name} onChange={(e) => setBucketForm({ name: e.target.value })} placeholder="小写字母/数字/中划线" />
            <div className="mt-2 flex items-center justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setBucketDialogOpen(false)}>取消</Button>
              <Button size="sm" onClick={createBucket} disabled={savingBucket}>{savingBucket ? '创建中…' : '创建'}</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={accountDialogOpen} onOpenChange={setAccountDialogOpen}>
        <Dialog className="@container !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{editingAccount ? '编辑华为云账号' : '新增华为云账号'}</Dialog.Title>
          <Dialog.Description className="mb-4 text-xs text-kumo-subtle">使用 AK/SK 接入华为云，SK 将加密存储，列表不回显。</Dialog.Description>
          <div className="flex flex-col gap-3">
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="账号名称" value={accountForm.name} onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })} placeholder="如：生产环境" />
              <Select
                alignItemWithTrigger
                aria-label="站点"
                value={accountForm.site}
                onValueChange={(v) => setAccountForm({ ...accountForm, site: v })}
                items={[
                  { value: 'cn', label: '国内站（myhuaweicloud.cn）' },
                  { value: 'intl', label: '国际站（myhuaweicloud.com）' },
                ]}
              />
            </div>
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="Access Key ID" value={accountForm.accessKeyId} onChange={(e) => setAccountForm({ ...accountForm, accessKeyId: e.target.value })} placeholder={editingAccount ? `当前：${editingAccount.accessKeyId}（留空不更换）` : 'AK 明文保存，列表脱敏显示'} />
              <Input label="Secret Access Key" type="password" value={accountForm.secretAccessKey} onChange={(e) => setAccountForm({ ...accountForm, secretAccessKey: e.target.value })} placeholder={editingAccount ? '留空表示不更换' : 'SK 加密存储'} />
            </div>
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="默认区域" value={accountForm.defaultRegion} onChange={(e) => setAccountForm({ ...accountForm, defaultRegion: e.target.value })} placeholder="如 cn-north-4，可留空" />
              <Input label="默认项目 ID" value={accountForm.defaultProjectId} onChange={(e) => setAccountForm({ ...accountForm, defaultProjectId: e.target.value })} placeholder="验证后自动发现，可留空" />
            </div>
            <Input label="备注" value={accountForm.description} onChange={(e) => setAccountForm({ ...accountForm, description: e.target.value })} placeholder="可选" />
            <div className="border-t border-kumo-line pt-3">
              <div className="mb-2 text-xs font-medium text-kumo-subtle">SSH 凭据（用于面板内终端直连实例）</div>
              <div className="grid gap-3 cq-md:grid-cols-2">
                <Input label="SSH 用户" value={accountForm.sshUser} onChange={(e) => setAccountForm({ ...accountForm, sshUser: e.target.value })} placeholder="默认 root" />
                <Input label="SSH 端口" type="number" value={accountForm.sshPort} onChange={(e) => setAccountForm({ ...accountForm, sshPort: Number(e.target.value) })} placeholder="默认 22" />
              </div>
              <div className="mt-3 grid gap-3 cq-md:grid-cols-2">
                <Textarea label="SSH 私钥（可选）" value={accountForm.sshPrivateKey} onChange={(e) => setAccountForm({ ...accountForm, sshPrivateKey: e.target.value })} placeholder="粘贴 PEM 私钥，编辑时留空不更换" className="min-h-[7rem]" />
                <Input label="SSH 密码（可选）" type="password" value={accountForm.sshPassword} onChange={(e) => setAccountForm({ ...accountForm, sshPassword: e.target.value })} placeholder="与私钥二选一，编辑时留空不更换" />
              </div>
            </div>
            <div className="mt-2 flex items-center justify-end gap-2">
              <Button size="sm" variant="secondary" onClick={() => setAccountDialogOpen(false)}>取消</Button>
              <Button size="sm" onClick={saveAccount} disabled={savingAccount}>{savingAccount ? '保存中…' : '保存'}</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

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

function SkeletonLines() {
  return (
    <DataTableFrame variant="embedded" density="dense">
      <Table>
        <Table.Body>
          {[0, 1, 2].map((i) => (
            <Table.Row key={i}>
              <Table.Cell colSpan={7}><SkeletonLine className="h-5 w-full" /></Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </DataTableFrame>
  );
}
