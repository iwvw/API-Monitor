import React, { useState, useEffect, useCallback } from 'react';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Tabs } from '@cloudflare/kumo/components/tabs';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import useTableResize from '../composables/useTableResize.js';
import {
  Globe,
  Settings,
  Plus,
  Trash,
  RefreshCw,
  Server,
  Cloud,
  Layers,
  Terminal,
  Database,
  Lock,
  Box
} from '../components/Icons.jsx';

const CLOUDFLARE_TABS = [
  { value: 'zones', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-3.5 h-3.5" />Websites</span> },
  { value: 'workers', label: <span className="inline-flex items-center gap-1.5"><Terminal className="w-3.5 h-3.5" />Workers</span> },
  { value: 'pages', label: <span className="inline-flex items-center gap-1.5"><Layers className="w-3.5 h-3.5" />Pages</span> },
  { value: 'r2', label: <span className="inline-flex items-center gap-1.5"><Database className="w-3.5 h-3.5" />R2</span> },
  { value: 'tunnels', label: <span className="inline-flex items-center gap-1.5"><Lock className="w-3.5 h-3.5" />Zero Trust</span> },
  { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-3.5 h-3.5" />Accounts</span> },
];

function DnsPage() {
  const [activeTab, setActiveTab] = useState('zones'); // 'zones' | 'workers' | 'pages' | 'r2' | 'tunnels' | 'accounts'
  
  // Accounts state
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  
  // Data state
  const [zones, setZones] = useState([]);
  const [workers, setWorkers] = useState([]);
  const [pages, setPages] = useState([]);
  const [r2Buckets, setR2Buckets] = useState([]);
  const [tunnels, setTunnels] = useState([]);
  const [loadingData, setLoadingData] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const [zoneColWidths, startZoneResize] = useTableResize([260, 120, 120, 140]);
  const [tunnelColWidths, startTunnelResize] = useTableResize([260, 120, 140, 140]);
  const [accountColWidths, startAccountResize] = useTableResize([220, 260, 120, 140]);

  // Modal states
  const [showAddAccountModal, setShowAddAccountModal] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState({
    name: '',
    email: '',
    apiToken: '',
    skipVerify: false
  });
  const [submittingAccount, setSubmittingAccount] = useState(false);

  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  // ==================== Account Management ====================
  const loadAccounts = useCallback(async () => {
    try {
      const response = await fetch('/api/cloudflare/accounts', { headers: getAuthHeaders() });
      const data = await response.json();
      if (Array.isArray(data)) {
        setAccounts(data);
        if (data.length > 0 && !selectedAccountId) {
          setSelectedAccountId(String(data[0].id));
        }
      }
    } catch (e) {
      toast.error('加载 Cloudflare 账号失败');
    }
  }, [getAuthHeaders, selectedAccountId]);

  const handleAddAccount = async () => {
    if (!accountForm.name || !accountForm.apiToken) {
      toast.warning('名称和 API Token/Key 必填');
      return;
    }
    setSubmittingAccount(true);
    try {
      const isEdit = !!editingAccount;
      const url = isEdit ? `/api/cloudflare/accounts/${editingAccount.id}` : '/api/cloudflare/accounts';
      const method = isEdit ? 'PUT' : 'POST';

      const response = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(accountForm),
      });
      const result = await response.json();
      if (result.success || result.account) {
        toast.success(isEdit ? '账号已更新' : '账号已添加');
        setShowAddAccountModal(false);
        setEditingAccount(null);
        setAccountForm({ name: '', email: '', apiToken: '', skipVerify: false });
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
    if (!window.confirm('确定要删除此 Cloudflare 账号吗？')) return;
    try {
      const response = await fetch(`/api/cloudflare/accounts/${id}`, {
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
      email: acc.email || '',
      apiToken: '', // 不回显 Token
      skipVerify: false
    });
    setShowAddAccountModal(true);
  };

  // ==================== Data Loading ====================
  const loadZones = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/cloudflare/accounts/${accountId}/zones`, { headers: getAuthHeaders() });
      const result = await response.json();
      setZones(result.zones || []);
    } catch (e) {
      toast.error('加载域名列表失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadWorkers = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/cloudflare/accounts/${accountId}/workers`, { headers: getAuthHeaders() });
      const result = await response.json();
      setWorkers(result.workers || []);
    } catch (e) {
      toast.error('加载 Workers 失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadPages = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/cloudflare/accounts/${accountId}/pages`, { headers: getAuthHeaders() });
      const result = await response.json();
      setPages(result.projects || []);
    } catch (e) {
      toast.error('加载 Pages 失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadR2 = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/cloudflare/accounts/${accountId}/r2/buckets`, { headers: getAuthHeaders() });
      const result = await response.json();
      setR2Buckets(result.buckets || []);
    } catch (e) {
      toast.error('加载 R2 失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadTunnels = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/cloudflare/accounts/${accountId}/tunnels`, { headers: getAuthHeaders() });
      const result = await response.json();
      setTunnels(result.tunnels || []);
    } catch (e) {
      toast.error('加载 Tunnels 失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const refreshData = useCallback(() => {
    if (!selectedAccountId) return;
    setRefreshing(true);
    if (activeTab === 'zones') loadZones(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'workers') loadWorkers(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'pages') loadPages(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'r2') loadR2(selectedAccountId).then(() => setRefreshing(false));
    else if (activeTab === 'tunnels') loadTunnels(selectedAccountId).then(() => setRefreshing(false));
    else setRefreshing(false);
  }, [activeTab, selectedAccountId, loadZones, loadWorkers, loadPages, loadR2, loadTunnels]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (selectedAccountId && activeTab !== 'accounts') {
      if (activeTab === 'zones') loadZones(selectedAccountId);
      else if (activeTab === 'workers') loadWorkers(selectedAccountId);
      else if (activeTab === 'pages') loadPages(selectedAccountId);
      else if (activeTab === 'r2') loadR2(selectedAccountId);
      else if (activeTab === 'tunnels') loadTunnels(selectedAccountId);
    }
  }, [activeTab, selectedAccountId, loadZones, loadWorkers, loadPages, loadR2, loadTunnels]);

  return (
    <div className="flex flex-col gap-6 w-full max-w-7xl mx-auto px-1">
      {/* 顶部：提供商卡片 */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={CLOUDFLARE_TABS}
          size="sm"
          className="max-w-full"
          listClassName="overflow-x-auto scrollbar-thin"
        />

        {activeTab !== 'accounts' && (
          <div className="flex items-center gap-3">
            <Select
              aria-label="选择 Cloudflare 账号"
              size="sm"
              className="w-44"
              placeholder="选择账号"
              value={selectedAccountId || null}
              onValueChange={(value) => setSelectedAccountId(value ? String(value) : '')}
            >
              {accounts.map((acc) => (
                <Select.Option key={acc.id} value={String(acc.id)}>
                  {acc.name}
                </Select.Option>
              ))}
            </Select>
            <Button
              onClick={refreshData}
              disabled={refreshing || !selectedAccountId}
              shape="square"
              size="sm"
              aria-label="刷新 Cloudflare 数据"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />
            </Button>
          </div>
        )}
      </div>

      {/* Content Area */}
      <div className="min-h-[400px]">
        {/* 1. Zones Tab */}
        {activeTab === 'zones' && (
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            {loadingData ? (
              <Table layout="fixed">
                <colgroup>
                  {zoneColWidths.map((width, index) => (
                    <col key={index} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                    <Table.Head className="p-4">Domain</Table.Head>
                    <Table.Head className="p-4">Status</Table.Head>
                    <Table.Head className="p-4">Type</Table.Head>
                    <Table.Head className="p-4 text-center">Action</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {[...Array(5)].map((_, idx) => (
                    <Table.Row key={idx} className="border-b border-kumo-line">
                      <Table.Cell className="p-4"><SkeletonLine className="w-48 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-16 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-12 h-4" /></Table.Cell>
                      <Table.Cell className="p-4 text-center"><SkeletonLine className="w-20 h-4 mx-auto" /></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            ) : (
              <Table layout="fixed">
                <colgroup>
                  {zoneColWidths.map((width, index) => (
                    <col key={index} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                    <Table.Head className="relative p-4 pr-6">
                      Domain
                      <Table.ResizeHandle onMouseDown={(e) => startZoneResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="relative p-4 pr-6">
                      Status
                      <Table.ResizeHandle onMouseDown={(e) => startZoneResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="relative p-4 pr-6">
                      Type
                      <Table.ResizeHandle onMouseDown={(e) => startZoneResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="p-4 text-center">Action</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {zones.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={4} className="p-12 text-center text-kumo-subtle">No domains found</Table.Cell>
                    </Table.Row>
                  ) : (
                    zones.map((zone) => (
                      <Table.Row key={zone.id} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 transition-colors">
                        <Table.Cell className="p-4 font-bold text-kumo-strong">{zone.name}</Table.Cell>
                        <Table.Cell className="p-4">
                          <span className={`px-2 py-0.5 rounded border text-[10px] font-bold ${zone.status === 'active' ? 'bg-kumo-success/10 border-kumo-success/20 text-kumo-success' : 'bg-kumo-warning/10 border-kumo-warning/20 text-kumo-warning'}`}>
                            {zone.status === 'active' ? 'Active' : 'Pending'}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="p-4 text-kumo-subtle">{zone.type}</Table.Cell>
                        <Table.Cell className="p-4 text-center">
                          <Button className="text-[10px] h-7 px-2 border border-kumo-line bg-kumo-recessed/50 hover:bg-kumo-brand/10 hover:text-kumo-brand">
                            Manage DNS
                          </Button>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            )}
          </div>
        )}

        {/* 2. Workers Tab */}
        {activeTab === 'workers' && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {loadingData ? (
              [...Array(6)].map((_, idx) => (
                <div key={idx} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm flex flex-col gap-3">
                  <SkeletonLine className="w-1/2 h-4" />
                  <SkeletonLine className="w-1/3 h-3" />
                  <SkeletonLine className="w-16 h-7 rounded mt-auto" />
                </div>
              ))
            ) : workers.length === 0 ? (
              <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">No workers found</div>
            ) : (
              workers.map((worker) => (
                <div key={worker.id} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                  <div className="flex justify-between items-start">
                    <div className="flex flex-col min-w-0">
                      <span className="text-xs font-bold text-kumo-strong truncate">{worker.name}</span>
                      <span className="text-[10px] text-kumo-subtle font-mono">{new Date(worker.modifiedOn).toLocaleDateString()}</span>
                    </div>
                  </div>
                  <div className="flex items-center justify-between mt-auto pt-1">
                    <Button className="h-7 text-[10px] px-2.5 border border-kumo-line bg-kumo-recessed hover:bg-kumo-base font-bold">
                      Edit Code
                    </Button>
                  </div>
                </div>
              ))
            )}
          </div>
        )}

        {/* 3. Pages Tab */}
        {activeTab === 'pages' && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {loadingData ? (
              [...Array(6)].map((_, idx) => (
                <div key={idx} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm flex flex-col gap-3">
                  <SkeletonLine className="w-2/3 h-4" />
                  <SkeletonLine className="w-1/2 h-3" />
                  <div className="border-t border-kumo-line/50 pt-2 space-y-1">
                    <SkeletonLine className="w-full h-3" />
                    <SkeletonLine className="w-1/3 h-3" />
                  </div>
                </div>
              ))
            ) : pages.length === 0 ? (
              <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">No pages found</div>
            ) : (
              pages.map((page) => (
                <div key={page.name} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                  <div className="flex justify-between items-start">
                    <div className="flex flex-col min-w-0">
                      <span className="text-xs font-bold text-kumo-strong truncate">{page.name}</span>
                      <span className="text-[10px] text-kumo-brand hover:underline cursor-pointer truncate">{page.subdomain}</span>
                    </div>
                  </div>
                  <div className="flex flex-col gap-1 text-[10px] text-kumo-subtle border-t border-kumo-line/50 pt-2">
                    <span>Production: {page.productionBranch || '-'}</span>
                    <span>Status: {page.latestDeployment?.status || '-'}</span>
                  </div>
                </div>
              ))
            )}
          </div>
        )}

        {/* 4. R2 Tab */}
        {activeTab === 'r2' && (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {loadingData ? (
              [...Array(6)].map((_, idx) => (
                <div key={idx} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm flex items-center gap-3">
                  <SkeletonLine className="w-5 h-5 rounded-full" />
                  <div className="flex-1 space-y-2">
                    <SkeletonLine className="w-1/2 h-4" />
                    <SkeletonLine className="w-1/3 h-3" />
                  </div>
                </div>
              ))
            ) : r2Buckets.length === 0 ? (
              <div className="col-span-full p-20 text-center text-kumo-subtle bg-kumo-base border border-kumo-line rounded-lg">No R2 buckets found</div>
            ) : (
              r2Buckets.map((bucket) => (
                <div key={bucket.name} className="bg-kumo-base border border-kumo-line rounded-lg p-4 shadow-sm hover:border-kumo-brand transition-all flex flex-col gap-3">
                  <div className="flex items-center gap-2">
                    <Box className="w-5 h-5 text-kumo-brand" />
                    <span className="text-xs font-bold text-kumo-strong truncate">{bucket.name}</span>
                  </div>
                  <span className="text-[10px] text-kumo-subtle">Created: {new Date(bucket.creation_date).toLocaleDateString()}</span>
                </div>
              ))
            )}
          </div>
        )}

        {/* 5. Tunnels Tab */}
        {activeTab === 'tunnels' && (
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            {loadingData ? (
              <Table layout="fixed">
                <colgroup>
                  {tunnelColWidths.map((width, index) => (
                    <col key={index} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                    <Table.Head className="p-4">Name</Table.Head>
                    <Table.Head className="p-4">Status</Table.Head>
                    <Table.Head className="p-4">Connections</Table.Head>
                    <Table.Head className="p-4 text-center">Action</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {[...Array(4)].map((_, idx) => (
                    <Table.Row key={idx} className="border-b border-kumo-line">
                      <Table.Cell className="p-4"><SkeletonLine className="w-36 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-16 h-4" /></Table.Cell>
                      <Table.Cell className="p-4"><SkeletonLine className="w-8 h-4" /></Table.Cell>
                      <Table.Cell className="p-4 text-center"><SkeletonLine className="w-16 h-4 mx-auto" /></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            ) : (
              <Table layout="fixed">
                <colgroup>
                  {tunnelColWidths.map((width, index) => (
                    <col key={index} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                    <Table.Head className="relative p-4 pr-6">
                      Name
                      <Table.ResizeHandle onMouseDown={(e) => startTunnelResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="relative p-4 pr-6">
                      Status
                      <Table.ResizeHandle onMouseDown={(e) => startTunnelResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="relative p-4 pr-6">
                      Connections
                      <Table.ResizeHandle onMouseDown={(e) => startTunnelResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="p-4 text-center">Action</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {tunnels.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={4} className="p-12 text-center text-kumo-subtle">No tunnels found</Table.Cell>
                    </Table.Row>
                  ) : (
                    tunnels.map((tunnel) => (
                      <Table.Row key={tunnel.id} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10 transition-colors">
                        <Table.Cell className="p-4 font-bold text-kumo-strong">{tunnel.name}</Table.Cell>
                        <Table.Cell className="p-4">
                          <span className={`px-2 py-0.5 rounded border text-[10px] font-bold ${tunnel.status === 'healthy' ? 'bg-kumo-success/10 border-kumo-success/20 text-kumo-success' : 'bg-kumo-danger/10 border-kumo-danger/20 text-kumo-danger'}`}>
                            {tunnel.status}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="p-4 text-kumo-default">{tunnel.connections?.length || 0}</Table.Cell>
                        <Table.Cell className="p-4 text-center">
                          <Button className="text-[10px] h-7 px-2 border border-kumo-line bg-kumo-recessed/50 hover:bg-kumo-brand/10 hover:text-kumo-brand">
                            Configure
                          </Button>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            )}
          </div>
        )}

        {/* 6. Accounts Tab */}
        {activeTab === 'accounts' && (
          <div className="space-y-4">
            <div className="flex justify-between items-center">
              <h3 className="text-sm font-bold text-kumo-strong">Cloudflare 账号</h3>
              <Button
                onClick={() => {
                  setEditingAccount(null);
                  setAccountForm({ name: '', email: '', apiToken: '', skipVerify: false });
                  setShowAddAccountModal(true);
                }}
                className="h-8 text-xs flex items-center gap-1.5"
              >
                <Plus className="w-3.5 h-3.5" />
                <span>添加账号</span>
              </Button>
            </div>

            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
              <Table layout="fixed">
                <colgroup>
                  {accountColWidths.map((width, index) => (
                    <col key={index} style={{ width }} />
                  ))}
                </colgroup>
                <Table.Header>
                  <Table.Row className="bg-kumo-recessed/40 border-b border-kumo-line font-bold text-kumo-subtle">
                    <Table.Head className="relative p-4 pr-6">
                      备注名称
                      <Table.ResizeHandle onMouseDown={(e) => startAccountResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="relative p-4 pr-6">
                      Email
                      <Table.ResizeHandle onMouseDown={(e) => startAccountResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="relative p-4 pr-6">
                      状态
                      <Table.ResizeHandle onMouseDown={(e) => startAccountResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="p-4 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accounts.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={4} className="p-12 text-center text-kumo-subtle">尚未配置任何账号</Table.Cell>
                    </Table.Row>
                  ) : (
                    accounts.map((acc) => (
                      <Table.Row key={acc.id} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10">
                        <Table.Cell className="p-4 font-bold text-kumo-strong">{acc.name}</Table.Cell>
                        <Table.Cell className="p-4 text-kumo-default">{acc.email || '-'}</Table.Cell>
                        <Table.Cell className="p-4">
                          <span className="px-2 py-0.5 rounded border bg-kumo-success/10 border-kumo-success/20 text-kumo-success text-[10px] font-bold">已连接</span>
                        </Table.Cell>
                        <Table.Cell className="p-4 text-center">
                          <div className="flex justify-center gap-2">
                            <Button
                              onClick={() => openEditModal(acc)}
                              shape="square"
                              size="sm"
                              variant="secondary"
                              aria-label={`编辑 ${acc.name}`}
                            >
                              <Settings className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              onClick={() => deleteAccount(acc.id)}
                              shape="square"
                              size="sm"
                              variant="secondary-destructive"
                              aria-label={`删除 ${acc.name}`}
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
      </div>

      {/* Add/Edit Account Modal */}
      <Dialog.Root open={showAddAccountModal} onOpenChange={setShowAddAccountModal}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-xl">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingAccount ? '编辑 Cloudflare 账号' : '添加 Cloudflare 账号'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入您的 Cloudflare API Token 或 Global API Key。推荐使用受限制的 API Token。
          </Dialog.Description>
          
          <div className="space-y-4">
            <Input
              label="备注名称"
              size="sm"
              value={accountForm.name}
              onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })}
              placeholder="我的 Cloudflare"
            />

            <Input
              label="Email（如果使用 Global Key 则必填）"
              size="sm"
              type="email"
              value={accountForm.email}
              onChange={(e) => setAccountForm({ ...accountForm, email: e.target.value })}
              placeholder="example@cloudflare.com"
            />

            <Input
              label="API Token / Global API Key"
              size="sm"
              type="password"
              value={accountForm.apiToken}
              onChange={(e) => setAccountForm({ ...accountForm, apiToken: e.target.value })}
              placeholder={editingAccount ? '(不修改请留空)' : '请输入 Token 或 Key'}
              className="font-mono"
            />

            <Checkbox
              checked={accountForm.skipVerify}
              onCheckedChange={(checked) => setAccountForm({ ...accountForm, skipVerify: checked })}
              label="跳过 API 验证 (仅用于离线添加)"
            />

            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close render={(props) => (
                <Button {...props} variant="secondary" size="sm">取消</Button>
              )} />
              <Button onClick={handleAddAccount} loading={submittingAccount} size="sm">
                {submittingAccount ? '验证中...' : '保存账号'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default DnsPage;
