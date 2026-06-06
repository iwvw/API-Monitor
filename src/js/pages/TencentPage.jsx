import React, { useState, useEffect, useCallback } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Tabs } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import useTableResize from '../composables/useTableResize.js';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { handleEditableRowDoubleClick } from '../modules/tableInteractions.js';
import {
  Database,
  Globe,
  Server,
  Cloud,
  Settings,
  Plus,
  Trash,
  RefreshCw,
  Search,
  Play,
  Square,
  RotateCw,
  MoreVertical,
  Activity,
  History,
  ArrowRight,
  Shield,
  ChevronRight,
  ChevronDown
} from '../components/Icons.jsx';

function TencentPage() {
  const { theme } = useStore();
  const [activeTab, setActiveTab] = useState('dns'); // 'dns' | 'cvm' | 'lighthouse' | 'accounts'
  
  // Accounts state
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [loadingAccounts, setLoadingAccounts] = useState(false);

  // Column resizing
  const [dnsColWidths, startDnsResize] = useTableResize([180, 100, 100, 150, 100]);
  const [accountsColWidths, startAccountsResize] = useTableResize([180, 220, 120, 200, 100]);

  // Data state
  const [domains, setDomains] = useState([]);
  const [cvmInstances, setCvmInstances] = useState([]); // CVM
  const [lighthouseInstances, setLighthouseInstances] = useState([]); // Lighthouse
  const [loadingData, setLoadingData] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  // Modal states
  const [showAddAccountModal, setShowAddAccountModal] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState({
    name: '',
    secretId: '',
    secretKey: '',
    regionId: 'ap-guangzhou',
    description: ''
  });
  const [submittingAccount, setSubmittingAccount] = useState(false);

  // Global Auth Header
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  // ==================== 1. Account Management ====================
  const loadAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      const response = await fetch('/api/tencent/accounts', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setAccounts(data);
        if (data.length > 0 && !selectedAccountId) {
          setSelectedAccountId(String(data[0].id));
        }
      }
    } catch (e) {
      console.error('[Tencent] 加载账号失败:', e);
      toast.error('加载腾讯云账号失败');
    } finally {
      setLoadingAccounts(false);
    }
  }, [getAuthHeaders, selectedAccountId]);

  const handleAddAccount = async () => {
    if (!accountForm.name || !accountForm.secretId || (!editingAccount && !accountForm.secretKey)) {
      toast.warning('请填写必填字段');
      return;
    }

    setSubmittingAccount(true);
    try {
      const isEdit = !!editingAccount;
      const url = isEdit ? `/api/tencent/accounts/${editingAccount.id}` : '/api/tencent/accounts';
      const method = isEdit ? 'PUT' : 'POST';

      const response = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(accountForm),
      });
      const result = await response.json();
      if (result.success || result.id) {
        toast.success(isEdit ? '账号已更新' : '账号已添加');
        setShowAddAccountModal(false);
        setEditingAccount(null);
        setAccountForm({ name: '', secretId: '', secretKey: '', regionId: 'ap-guangzhou', description: '' });
        loadAccounts();
      } else {
        toast.error(result.error || '操作失败');
      }
    } catch (e) {
      toast.error('请求失败: ' + e.message);
    } finally {
      setSubmittingAccount(false);
    }
  };

  const deleteAccount = async (id) => {
    if (!(await dialog.confirm('确定要删除此腾讯云账号吗？'))) return;
    try {
      const response = await fetch(`/api/tencent/accounts/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('账号已删除');
        if (selectedAccountId === String(id)) setSelectedAccountId('');
        loadAccounts();
      }
    } catch (e) {
      toast.error('删除失败');
    }
  };

  const openEditModal = (acc) => {
    setEditingAccount(acc);
    setAccountForm({
      name: acc.name,
      secretId: acc.secret_id || '',
      secretKey: '', 
      regionId: acc.region_id || 'ap-guangzhou',
      description: acc.description || ''
    });
    setShowAddAccountModal(true);
  };

  // ==================== 2. Data Loading ====================
  const loadDnsData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/tencent/accounts/${accountId}/domains`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      setDomains(result.Domains || []);
    } catch (e) {
      toast.error('加载域名列表失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadCvmData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/tencent/accounts/${accountId}/cvm`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      setCvmInstances(result || []);
    } catch (e) {
      toast.error('加载 CVM 实例失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadLighthouseData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/tencent/accounts/${accountId}/lighthouse`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      setLighthouseInstances(result || []);
    } catch (e) {
      toast.error('加载轻量服务器失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const refreshData = useCallback(() => {
    if (!selectedAccountId) return;
    setRefreshing(true);
    if (activeTab === 'dns') loadDnsData(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'cvm') loadCvmData(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'lighthouse') loadLighthouseData(selectedAccountId).then(() => setRefreshing(false));
    else setRefreshing(false);
  }, [activeTab, selectedAccountId, loadDnsData, loadCvmData, loadLighthouseData]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (selectedAccountId && activeTab !== 'accounts') {
      if (activeTab === 'dns') loadDnsData(selectedAccountId);
      else if (activeTab === 'cvm') loadCvmData(selectedAccountId);
      else if (activeTab === 'lighthouse') loadLighthouseData(selectedAccountId);
    }
  }, [activeTab, selectedAccountId, loadDnsData, loadCvmData, loadLighthouseData]);

  // ==================== 3. Instance Actions ====================
  const handleInstanceAction = async (type, instance, action) => {
    const isLighthouse = type === 'lighthouse';
    const endpoint = isLighthouse ? 'lighthouse' : 'cvm';
    const actionText = action === 'START' ? '启动' : action === 'STOP' ? '停止' : '重启';
    
    if (!(await dialog.confirm(`确认${actionText}实例 ${instance.InstanceName || instance.InstanceId} 吗？`))) return;

    try {
      const response = await fetch(`/api/tencent/accounts/${selectedAccountId}/${endpoint}/${instance.InstanceId}/control`, {
        method: 'POST',
        headers: {
            ...getAuthHeaders(),
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({ action, region: instance._Region || instance.Region }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`${actionText}指令已下发`);
        setTimeout(refreshData, 2000);
      } else {
        toast.error(`${actionText}失败: ${result.error}`);
      }
    } catch (e) {
      toast.error(`${actionText}请求异常`);
    }
  };

  // ==================== Helpers ====================
  const getStatusBadge = (status) => {
    const s = String(status).toUpperCase();
    if (s === 'RUNNING') return 'text-kumo-success bg-kumo-success/10 border-kumo-success/20';
    if (s === 'STOPPED') return 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20';
    if (s.includes('ING')) return 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20';
    return 'text-kumo-subtle bg-kumo-recessed border-kumo-line';
  };

  const getStatusText = (status) => {
    const map = {
      RUNNING: '运行中',
      STOPPED: '已停止',
      STARTING: '启动中',
      STOPPING: '停止中',
      REBOOTING: '重启中',
    };
    return map[String(status).toUpperCase()] || status;
  };

  return (
    <div className="flex flex-col gap-6 w-full px-1">
      {/* Header Tabs */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <div className="min-w-0 w-full md:w-auto">
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeTab}
            onValueChange={setActiveTab}
            tabs={[
              { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-4 h-4" />DNS 解析</span> },
              { value: 'cvm', label: <span className="inline-flex items-center gap-1.5"><Server className="w-4 h-4" />CVM 实例</span> },
              { value: 'lighthouse', label: <span className="inline-flex items-center gap-1.5"><Cloud className="w-4 h-4" />轻量服务器</span> },
              { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4" />账号管理</span> },
            ]}
          />
        </div>

        {activeTab !== 'accounts' && (
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">账号</span>
              <Select
                aria-label="腾讯云账号" size="sm"
                value={selectedAccountId}
                onValueChange={setSelectedAccountId}
                items={accounts.map((acc) => ({ value: String(acc.id), label: acc.name }))}
              />
            </div>
            <Button
              onClick={refreshData}
              disabled={refreshing || !selectedAccountId}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新"
              title="刷新"
              icon={<RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />}
            />
          </div>
        )}
      </div>

      {/* Content Area */}
      <div className="min-h-[400px]">
        {loadingData ? (
          activeTab === 'dns' ? (
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
              <Table layout="fixed">
                <colgroup>
                  {dnsColWidths.map((w, idx) => (
                    <col key={idx} style={{ width: w }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                    <Table.Head className="p-4">域名</Table.Head>
                    <Table.Head className="p-4">记录数</Table.Head>
                    <Table.Head className="p-4">状态</Table.Head>
                    <Table.Head className="p-4">到期时间</Table.Head>
                    <Table.Head className="p-4 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {[...Array(5)].map((_, idx) => (
                    <Table.Row key={idx} className="border-b border-kumo-line">
                      <Table.Cell className="p-4"><SkeletonLine className="w-32 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-12 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-16 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-40 h-4" /></Table.Cell>
                      <Table.Cell className="p-4 text-center"><SkeletonLine className="w-16 h-4 mx-auto" /></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {[...Array(6)].map((_, idx) => (
                <div key={idx} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm flex flex-col gap-3">
                  <div className="flex justify-between items-start">
                    <div className="flex flex-col gap-1 w-2/3">
                      <SkeletonLine className="w-full h-4" />
                      <SkeletonLine className="w-1/2 h-3" />
                    </div>
                    <SkeletonLine className="w-16 h-5 rounded" />
                  </div>
                  <div className="border-y border-kumo-line/50 py-2.5 my-1 flex flex-col gap-2">
                    <div className="flex justify-between">
                      <SkeletonLine className="w-1/3 h-3" />
                      <SkeletonLine className="w-1/3 h-3" />
                    </div>
                    <div className="flex justify-between">
                      <SkeletonLine className="w-1/3 h-3" />
                      <SkeletonLine className="w-1/3 h-3" />
                    </div>
                  </div>
                  <div className="flex justify-between items-center mt-auto pt-1">
                    <div className="flex gap-1.5">
                      <SkeletonLine className="w-8 h-8 rounded" />
                      <SkeletonLine className="w-8 h-8 rounded" />
                      <SkeletonLine className="w-8 h-8 rounded" />
                    </div>
                    <SkeletonLine className="w-20 h-7 rounded" />
                  </div>
                </div>
              ))}
            </div>
          )
        ) : (
          <>
            {/* 1. DNS Tab */}
            {activeTab === 'dns' && (
              <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
                <Table layout="fixed">
                  <colgroup>
                    {dnsColWidths.map((w, idx) => (
                      <col key={idx} style={{ width: w }} />
                    ))}
                  </colgroup>
                  <Table.Header>
                    <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                      <Table.Head className="relative group pr-6 p-4">
                        域名
                        <Table.ResizeHandle onMouseDown={(e) => startDnsResize(0, e)} />
                      </Table.Head>
                      <Table.Head className="relative group pr-6 p-4">
                        记录数
                        <Table.ResizeHandle onMouseDown={(e) => startDnsResize(1, e)} />
                      </Table.Head>
                      <Table.Head className="relative group pr-6 p-4">
                        状态
                        <Table.ResizeHandle onMouseDown={(e) => startDnsResize(2, e)} />
                      </Table.Head>
                      <Table.Head className="relative group pr-6 p-4">
                        到期时间
                        <Table.ResizeHandle onMouseDown={(e) => startDnsResize(3, e)} />
                      </Table.Head>
                      <Table.Head className="p-4 text-center">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {domains.length === 0 ? (
                      <Table.Row>
                        <Table.Cell colSpan={5} className="p-12 text-center text-kumo-subtle">暂无域名数据</Table.Cell>
                      </Table.Row>
                    ) : (
                      domains.map((dom) => (
                        <Table.Row key={dom.DomainId} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 transition-colors">
                          <Table.Cell className="p-4 font-bold text-kumo-strong">{dom.Name}</Table.Cell>
                          <Table.Cell className="p-4 text-kumo-default tabular-nums">{dom.RecordCount || '-'}</Table.Cell>
                          <Table.Cell className="p-4">
                            <span className={`px-2 py-0.5 rounded border text-[10px] font-bold ${dom.Status === 'ENABLE' ? 'bg-kumo-success/10 border-kumo-success/20 text-kumo-success' : 'bg-kumo-recessed border-kumo-line text-kumo-subtle'}`}>
                              {dom.Status === 'ENABLE' ? '正常' : '已暂停'}
                            </span>
                          </Table.Cell>
                          <Table.Cell className="p-4 text-kumo-subtle">{dom.Expiration || '-'}</Table.Cell>
                          <Table.Cell className="p-4 text-center">
                            <Button size="sm" className="text-[10px] border border-kumo-line bg-kumo-recessed/50 hover:bg-kumo-brand/10 hover:text-kumo-brand">
                              管理解析
                            </Button>
                          </Table.Cell>
                        </Table.Row>
                      ))
                    )}
                  </Table.Body>
                </Table>
              </div>
            )}

            {/* 2. CVM Tab */}
            {activeTab === 'cvm' && (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {cvmInstances.length === 0 ? (
                  <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">暂无 CVM 实例</div>
                ) : (
                  cvmInstances.map((inst) => (
                    <div key={inst.InstanceId} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                      <div className="flex justify-between items-start">
                        <div className="flex flex-col min-w-0">
                          <span className="text-xs font-bold text-kumo-strong truncate">{inst.InstanceName || inst.InstanceId}</span>
                          <span className="text-[10px] text-kumo-subtle font-mono">{inst.InstanceId}</span>
                        </div>
                        <span className={`px-1.5 py-0.5 rounded border text-[10px] font-bold ${getStatusBadge(inst.InstanceState)}`}>
                          {getStatusText(inst.InstanceState)}
                        </span>
                      </div>
                      
                      <div className="grid grid-cols-2 gap-y-2 text-[10px] border-y border-kumo-line/50 py-2.5 my-1">
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">可用区</span>
                          <span className="text-kumo-strong font-bold">{inst.Placement?.Zone}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">IP 地址</span>
                          <span className="text-kumo-strong font-bold font-mono">
                            {inst.PublicIpAddresses?.[0] || inst.PrivateIpAddresses?.[0] || '-'}
                          </span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">规格</span>
                          <span className="text-kumo-strong font-bold">{inst.CPU}核 {inst.Memory}GB</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">操作系统</span>
                          <span className="text-kumo-strong font-bold truncate">{inst.OsName || '-'}</span>
                        </div>
                      </div>
                      
                      <div className="flex items-center justify-between gap-2 mt-auto pt-1">
                        <div className="flex gap-1.5">
                          <Button
                            onClick={() => handleInstanceAction('cvm', inst, 'START')}
                            disabled={inst.InstanceState === 'RUNNING'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="启动 CVM 实例"
                            className="text-kumo-success hover:bg-kumo-success/10"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('cvm', inst, 'STOP')}
                            disabled={inst.InstanceState === 'STOPPED'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="停止 CVM 实例"
                            className="text-kumo-danger hover:bg-kumo-danger/10"
                          >
                            <Square className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('cvm', inst, 'REBOOT')}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="重启 CVM 实例"
                            className="text-kumo-brand hover:bg-kumo-brand/10"
                          >
                            <RotateCw className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                        <Button size="sm" className="text-[10px] border border-kumo-line bg-kumo-recessed hover:bg-kumo-base font-bold">
                          监控详情
                        </Button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}

            {/* 3. Lighthouse Tab */}
            {activeTab === 'lighthouse' && (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {lighthouseInstances.length === 0 ? (
                  <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">暂无轻量应用服务器</div>
                ) : (
                  lighthouseInstances.map((inst) => (
                    <div key={inst.InstanceId} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                      <div className="flex justify-between items-start">
                        <div className="flex flex-col min-w-0">
                          <span className="text-xs font-bold text-kumo-strong truncate">{inst.InstanceName || inst.InstanceId}</span>
                          <span className="text-[10px] text-kumo-subtle font-mono">{inst.InstanceId}</span>
                        </div>
                        <span className={`px-1.5 py-0.5 rounded border text-[10px] font-bold ${getStatusBadge(inst.InstanceState)}`}>
                          {getStatusText(inst.InstanceState)}
                        </span>
                      </div>
                      
                      <div className="grid grid-cols-2 gap-y-2 text-[10px] border-y border-kumo-line/50 py-2.5 my-1">
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">可用区</span>
                          <span className="text-kumo-strong font-bold">{inst.Zone}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">公网 IP</span>
                          <span className="text-kumo-strong font-bold font-mono">{inst.PublicAddresses?.[0] || '-'}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">套餐规格</span>
                          <span className="text-kumo-strong font-bold">{inst.CPU}核 {inst.Memory}GB</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">到期时间</span>
                          <span className={`font-bold ${new Date(inst.ExpiredTime).getTime() < Date.now() + 7*24*3600*1000 ? 'text-kumo-danger' : 'text-kumo-strong'}`}>
                            {new Date(inst.ExpiredTime).toLocaleDateString()}
                          </span>
                        </div>
                      </div>
                      
                      <div className="flex items-center justify-between gap-2 mt-auto pt-1">
                        <div className="flex gap-1.5">
                          <Button
                            onClick={() => handleInstanceAction('lighthouse', inst, 'START')}
                            disabled={inst.InstanceState === 'RUNNING'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="启动轻量服务器"
                            className="text-kumo-success hover:bg-kumo-success/10"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('lighthouse', inst, 'STOP')}
                            disabled={inst.InstanceState === 'STOPPED'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="停止轻量服务器"
                            className="text-kumo-danger hover:bg-kumo-danger/10"
                          >
                            <Square className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('lighthouse', inst, 'REBOOT')}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="重启轻量服务器"
                            className="text-kumo-brand hover:bg-kumo-brand/10"
                          >
                            <RotateCw className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                        <Button size="sm" className="text-[10px] border border-kumo-line bg-kumo-recessed hover:bg-kumo-base font-bold">
                          管理详情
                        </Button>
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}

            {/* 4. Accounts Tab */}
            {activeTab === 'accounts' && (
              <div className="space-y-4">
                <div className="flex justify-between items-center">
                  <h3 className="text-sm font-bold text-kumo-strong">腾讯云账号列表</h3>
                  <Button size="sm"
                    onClick={() => {
                      setEditingAccount(null);
                      setAccountForm({ name: '', secretId: '', secretKey: '', regionId: 'ap-guangzhou', description: '' });
                      setShowAddAccountModal(true);
                    }}
                    className="text-xs flex items-center gap-1.5"
                  >
                    <Plus className="w-3.5 h-3.5" />
                    <span>添加账号</span>
                  </Button>
                </div>

                <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
                  <Table layout="fixed">
                    <colgroup>
                      {accountsColWidths.map((w, idx) => (
                        <col key={idx} style={{ width: w }} />
                      ))}
                    </colgroup>
                    <Table.Header>
                      <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                        <Table.Head className="relative group pr-6 p-4">
                          备注名称
                          <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(0, e)} />
                        </Table.Head>
                        <Table.Head className="relative group pr-6 p-4">
                          Secret ID
                          <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(1, e)} />
                        </Table.Head>
                        <Table.Head className="relative group pr-6 p-4">
                          默认地域
                          <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(2, e)} />
                        </Table.Head>
                        <Table.Head className="relative group pr-6 p-4">
                          描述
                          <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(3, e)} />
                        </Table.Head>
                        <Table.Head className="p-4 text-center">操作</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {accounts.length === 0 ? (
                        <Table.Row>
                          <Table.Cell colSpan={5} className="p-12 text-center text-kumo-subtle">尚未配置任何账号</Table.Cell>
                        </Table.Row>
                      ) : (
                        accounts.map((acc) => (
                          <Table.Row
                            key={acc.id}
                            className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 cursor-pointer"
                            title="双击编辑账号"
                            onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openEditModal(acc))}
                          >
                            <Table.Cell className="p-4 font-bold text-kumo-strong">{acc.name}</Table.Cell>
                            <Table.Cell className="p-4 font-mono text-kumo-default">{acc.secret_id}</Table.Cell>
                            <Table.Cell className="p-4 text-kumo-default">{acc.region_id}</Table.Cell>
                            <Table.Cell className="p-4 text-kumo-subtle truncate max-w-xs">{acc.description || '-'}</Table.Cell>
                            <Table.Cell className="p-4 text-center">
                              <div className="flex justify-center gap-2">
                                <Button
                                  onClick={() => openEditModal(acc)}
                                  variant="secondary" size="sm"
                                  shape="square"
                                  aria-label="编辑腾讯云账号"
                                  className="text-kumo-subtle hover:text-kumo-brand"
                                >
                                  <Settings className="w-3.5 h-3.5" />
                                </Button>
                                <Button
                                  onClick={() => deleteAccount(acc.id)}
                                  variant="secondary-destructive" size="sm"
                                  shape="square"
                                  aria-label="删除腾讯云账号"
                                  className="hover:bg-kumo-danger/10"
                                >
                                  <Trash className="w-3.5 h-3.5" />
                                </Button>
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        ))
                      )}
                    </Table.Body>
                  </Table>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {/* Add/Edit Account Modal */}
      <Dialog.Root open={showAddAccountModal} onOpenChange={setShowAddAccountModal}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-xl">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingAccount ? '编辑腾讯云账号' : '添加腾讯云账号'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入您的腾讯云 API 密钥 (SecretId & SecretKey)。
          </Dialog.Description>
          
          <div className="space-y-4">
            <Input
              label="备注名称"
              type="text" size="sm"
              value={accountForm.name}
              onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })}
              placeholder="我的腾讯云生产环境"
              className="w-full"
            />
            
            <div className="grid grid-cols-2 gap-4">
              <Input
                label="Secret ID"
                type="text" size="sm"
                value={accountForm.secretId}
                onChange={(e) => setAccountForm({ ...accountForm, secretId: e.target.value })}
                placeholder="AKID..."
                className="w-full font-mono"
              />
              <Input
                label="默认地域 ID"
                type="text" size="sm"
                value={accountForm.regionId}
                onChange={(e) => setAccountForm({ ...accountForm, regionId: e.target.value })}
                placeholder="ap-guangzhou"
                className="w-full font-mono"
              />
            </div>

            <Input
              label="Secret Key"
              type="password" size="sm"
              value={accountForm.secretKey}
              onChange={(e) => setAccountForm({ ...accountForm, secretKey: e.target.value })}
              placeholder={editingAccount ? '(不修改请留空)' : '请输入 SecretKey'}
              className="w-full font-mono"
            />

            <Textarea
              label="账号描述"
              size="sm"
              value={accountForm.description}
              onChange={(e) => setAccountForm({ ...accountForm, description: e.target.value })}
              className="w-full min-h-[60px]"
            />

            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close
                render={(props) => (
                  <Button size="sm"
                    {...props}
                    variant="secondary"
                    className="border border-kumo-line bg-kumo-recessed text-xs"
                  >
                    取消
                  </Button>
                )}
              />
              <Button size="sm" onClick={handleAddAccount} disabled={submittingAccount} className="text-xs">
                {submittingAccount ? '提交中...' : '保存账号'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default TencentPage;
