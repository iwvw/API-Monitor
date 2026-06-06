import React, { useState, useEffect, useCallback, useMemo } from 'react';
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

function AliyunPage() {
  const { theme } = useStore();
  const [activeTab, setActiveTab] = useState('dns'); // 'dns' | 'ecs' | 'swas' | 'accounts'
  
  // Accounts state
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [loadingAccounts, setLoadingAccounts] = useState(false);

  // Column resizing
  const [dnsColWidths, startDnsResize] = useTableResize([180, 100, 100, 250, 100]);
  const [accountsColWidths, startAccountsResize] = useTableResize([180, 220, 120, 200, 100]);

  // Data state
  const [domains, setDomains] = useState([]);
  const [instances, setInstances] = useState([]); // ECS
  const [swasInstances, setSwasInstances] = useState([]); // SWAS
  const [loadingData, setLoadingData] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  // Modal states
  const [showAddAccountModal, setShowAddAccountModal] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState({
    name: '',
    accessKeyId: '',
    accessKeySecret: '',
    regionId: 'cn-hangzhou',
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
      const response = await fetch('/api/aliyun/accounts', {
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
      console.error('[Aliyun] 加载账号失败:', e);
      toast.error('加载阿里云账号失败');
    } finally {
      setLoadingAccounts(false);
    }
  }, [getAuthHeaders, selectedAccountId]);

  const handleAddAccount = async () => {
    if (!accountForm.name || !accountForm.accessKeyId || (!editingAccount && !accountForm.accessKeySecret)) {
      toast.warning('请填写必填字段');
      return;
    }

    setSubmittingAccount(true);
    try {
      const isEdit = !!editingAccount;
      const url = isEdit ? `/api/aliyun/accounts/${editingAccount.id}` : '/api/aliyun/accounts';
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
        setAccountForm({ name: '', accessKeyId: '', accessKeySecret: '', regionId: 'cn-hangzhou', description: '' });
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
    if (!(await dialog.confirm('确定要删除此阿里云账号吗？'))) return;
    try {
      const response = await fetch(`/api/aliyun/accounts/${id}`, {
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
      accessKeyId: acc.accessKeyId || '',
      accessKeySecret: '', // 不回显 Secret
      regionId: acc.regionId || 'cn-hangzhou',
      description: acc.description || ''
    });
    setShowAddAccountModal(true);
  };

  // ==================== 2. Data Loading ====================
  const loadDnsData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/aliyun/accounts/${accountId}/domains`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      setDomains(result.Domains?.Domain || []);
    } catch (e) {
      toast.error('加载域名列表失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadEcsData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/aliyun/accounts/${accountId}/instances`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      setInstances(result.instances || []);
    } catch (e) {
      toast.error('加载 ECS 实例失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadSwasData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/aliyun/accounts/${accountId}/swas`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      setSwasInstances(result.instances || []);
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
    else if (activeTab === 'ecs') loadEcsData(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'swas') loadSwasData(selectedAccountId).then(() => setRefreshing(false));
    else setRefreshing(false);
  }, [activeTab, selectedAccountId, loadDnsData, loadEcsData, loadSwasData]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (selectedAccountId && activeTab !== 'accounts') {
      if (activeTab === 'dns') loadDnsData(selectedAccountId);
      else if (activeTab === 'ecs') loadEcsData(selectedAccountId);
      else if (activeTab === 'swas') loadSwasData(selectedAccountId);
    }
  }, [activeTab, selectedAccountId, loadDnsData, loadEcsData, loadSwasData]);

  // ==================== 3. Instance Actions ====================
  const handleInstanceAction = async (type, instance, action) => {
    const isSwas = type === 'swas';
    const endpoint = isSwas ? 'swas' : 'instances';
    const actionText = action === 'start' ? '启动' : action === 'stop' ? '停止' : '重启';
    
    if (!(await dialog.confirm(`确认${actionText}实例 ${instance.InstanceName || instance.InstanceId} 吗？`))) return;

    try {
      const response = await fetch(`/api/aliyun/accounts/${selectedAccountId}/${endpoint}/${instance.InstanceId}/${action}`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ regionId: instance.RegionId }),
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
    const s = String(status).toLowerCase();
    if (s === 'running' || s === 'active') return 'text-kumo-success bg-kumo-success/10 border-kumo-success/20';
    if (s === 'stopped') return 'text-kumo-danger bg-kumo-danger/10 border-kumo-danger/20';
    if (s.includes('starting') || s.includes('stopping')) return 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20';
    return 'text-kumo-subtle bg-kumo-recessed border-kumo-line';
  };

  const getStatusText = (status) => {
    const map = {
      running: '运行中',
      stopped: '已停止',
      starting: '启动中',
      stopping: '停止中',
      active: '正常',
    };
    return map[String(status).toLowerCase()] || status;
  };

  return (
    <div className="flex flex-col gap-6 w-full px-1">
      {/* 顶部统计卡片 */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <div className="min-w-0 w-full md:w-auto">
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeTab}
            onValueChange={setActiveTab}
            tabs={[
              { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-4 h-4" />DNS 解析</span> },
              { value: 'ecs', label: <span className="inline-flex items-center gap-1.5"><Server className="w-4 h-4" />ECS 实例</span> },
              { value: 'swas', label: <span className="inline-flex items-center gap-1.5"><Cloud className="w-4 h-4" />轻量服务器</span> },
              { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4" />账号管理</span> },
            ]}
          />
        </div>

        {activeTab !== 'accounts' && (
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <span className="text-xs text-kumo-subtle font-medium">账号</span>
              <Select
                aria-label="阿里云账号" size="sm"
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
                    <Table.Head className="p-4">备注</Table.Head>
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
                        备注
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
                          <Table.Cell className="p-4 font-bold text-kumo-strong">{dom.DomainName}</Table.Cell>
                          <Table.Cell className="p-4 text-kumo-default tabular-nums">{dom.RecordCount || '-'}</Table.Cell>
                          <Table.Cell className="p-4">
                            <span className="px-2 py-0.5 rounded border bg-kumo-success/10 border-kumo-success/20 text-kumo-success text-[10px] font-bold">
                              正常
                            </span>
                          </Table.Cell>
                          <Table.Cell className="p-4 text-kumo-subtle truncate max-w-xs">{dom.Remark || '-'}</Table.Cell>
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

            {/* 2. ECS Tab */}
            {activeTab === 'ecs' && (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {instances.length === 0 ? (
                  <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">暂无 ECS 实例</div>
                ) : (
                  instances.map((inst) => (
                    <div key={inst.InstanceId} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                      <div className="flex justify-between items-start">
                        <div className="flex flex-col min-w-0">
                          <span className="text-xs font-bold text-kumo-strong truncate">{inst.InstanceName || inst.InstanceId}</span>
                          <span className="text-[10px] text-kumo-subtle font-mono">{inst.InstanceId}</span>
                        </div>
                        <span className={`px-1.5 py-0.5 rounded border text-[10px] font-bold ${getStatusBadge(inst.Status)}`}>
                          {getStatusText(inst.Status)}
                        </span>
                      </div>
                      
                      <div className="grid grid-cols-2 gap-y-2 text-[10px] border-y border-kumo-line/50 py-2.5 my-1">
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">可用区</span>
                          <span className="text-kumo-strong font-bold">{inst.RegionName}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">IP 地址</span>
                          <span className="text-kumo-strong font-bold font-mono">
                            {inst.PublicIpAddress?.IpAddress?.[0] || inst.VpcAttributes?.PrivateIpAddress?.IpAddress?.[0] || '-'}
                          </span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">规格</span>
                          <span className="text-kumo-strong font-bold">{inst.InstanceTypeFriendly}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">操作系统</span>
                          <span className="text-kumo-strong font-bold truncate">{inst.OSName || '-'}</span>
                        </div>
                      </div>
                      
                      <div className="flex items-center justify-between gap-2 mt-auto pt-1">
                        <div className="flex gap-1.5">
                          <Button
                            onClick={() => handleInstanceAction('ecs', inst, 'start')}
                            disabled={inst.Status === 'Running'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="启动 ECS 实例"
                            className="text-kumo-success hover:bg-kumo-success/10"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('ecs', inst, 'stop')}
                            disabled={inst.Status === 'Stopped'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="停止 ECS 实例"
                            className="text-kumo-danger hover:bg-kumo-danger/10"
                          >
                            <Square className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('ecs', inst, 'reboot')}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="重启 ECS 实例"
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

            {/* 3. SWAS Tab */}
            {activeTab === 'swas' && (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {swasInstances.length === 0 ? (
                  <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">暂无轻量应用服务器</div>
                ) : (
                  swasInstances.map((inst) => (
                    <div key={inst.InstanceId} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                      <div className="flex justify-between items-start">
                        <div className="flex flex-col min-w-0">
                          <span className="text-xs font-bold text-kumo-strong truncate">{inst.InstanceName || inst.InstanceId}</span>
                          <span className="text-[10px] text-kumo-subtle font-mono">{inst.InstanceId}</span>
                        </div>
                        <span className={`px-1.5 py-0.5 rounded border text-[10px] font-bold ${getStatusBadge(inst.Status)}`}>
                          {getStatusText(inst.Status)}
                        </span>
                      </div>
                      
                      <div className="grid grid-cols-2 gap-y-2 text-[10px] border-y border-kumo-line/50 py-2.5 my-1">
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">可用区</span>
                          <span className="text-kumo-strong font-bold">{inst.RegionName}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">公网 IP</span>
                          <span className="text-kumo-strong font-bold font-mono">{inst.PublicIpAddress || '-'}</span>
                        </div>
                        <div className="flex flex-col">
                          <span className="text-kumo-subtle font-medium">套餐规格</span>
                          <span className="text-kumo-strong font-bold">{inst.InstanceTypeFriendly}</span>
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
                            onClick={() => handleInstanceAction('swas', inst, 'start')}
                            disabled={inst.Status === 'Running'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="启动轻量服务器"
                            className="text-kumo-success hover:bg-kumo-success/10"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('swas', inst, 'stop')}
                            disabled={inst.Status === 'Stopped'}
                            variant="secondary" size="sm"
                            shape="square"
                            aria-label="停止轻量服务器"
                            className="text-kumo-danger hover:bg-kumo-danger/10"
                          >
                            <Square className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            onClick={() => handleInstanceAction('swas', inst, 'reboot')}
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
                  <h3 className="text-sm font-bold text-kumo-strong">阿里云账号列表</h3>
                  <Button size="sm"
                    onClick={() => {
                      setEditingAccount(null);
                      setAccountForm({ name: '', accessKeyId: '', accessKeySecret: '', regionId: 'cn-hangzhou', description: '' });
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
                          AccessKey ID
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
                            <Table.Cell className="p-4 font-mono text-kumo-default">{acc.accessKeyId}</Table.Cell>
                            <Table.Cell className="p-4 text-kumo-default">{acc.regionId}</Table.Cell>
                            <Table.Cell className="p-4 text-kumo-subtle truncate max-w-xs">{acc.description || '-'}</Table.Cell>
                            <Table.Cell className="p-4 text-center">
                              <div className="flex justify-center gap-2">
                                <Button
                                  onClick={() => openEditModal(acc)}
                                  variant="secondary" size="sm"
                                  shape="square"
                                  aria-label="编辑阿里云账号"
                                  className="text-kumo-subtle hover:text-kumo-brand"
                                >
                                  <Settings className="w-3.5 h-3.5" />
                                </Button>
                                <Button
                                  onClick={() => deleteAccount(acc.id)}
                                  variant="secondary-destructive" size="sm"
                                  shape="square"
                                  aria-label="删除阿里云账号"
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
            {editingAccount ? '编辑阿里云账号' : '添加阿里云账号'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入您的阿里云 RAM 账号凭据 (建议仅开启读取及必要的控制权限)。
          </Dialog.Description>
          
          <div className="space-y-4">
            <Input
              label="备注名称"
              type="text" size="sm"
              value={accountForm.name}
              onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })}
              placeholder="我的阿里云生产环境"
              className="w-full"
            />
            
            <div className="grid grid-cols-2 gap-4">
              <Input
                label="AccessKey ID"
                type="text" size="sm"
                value={accountForm.accessKeyId}
                onChange={(e) => setAccountForm({ ...accountForm, accessKeyId: e.target.value })}
                placeholder="LTAI..."
                className="w-full font-mono"
              />
              <Input
                label="默认地域 ID"
                type="text" size="sm"
                value={accountForm.regionId}
                onChange={(e) => setAccountForm({ ...accountForm, regionId: e.target.value })}
                placeholder="cn-hangzhou"
                className="w-full font-mono"
              />
            </div>

            <Input
              label="AccessKey Secret"
              type="password" size="sm"
              value={accountForm.accessKeySecret}
              onChange={(e) => setAccountForm({ ...accountForm, accessKeySecret: e.target.value })}
              placeholder={editingAccount ? '(不修改请留空)' : '请输入 Secret'}
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

export default AliyunPage;
