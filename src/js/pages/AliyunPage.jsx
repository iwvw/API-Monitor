import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Tabs, Toolbar } from '@cloudflare/kumo';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import useTableResize from '../composables/useTableResize.js';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { handleEditableRowDoubleClick } from '../modules/tableInteractions.js';
import { AppTable, DataTableFrame, EmptyState, StatusBadge, PageStack, SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import { Cloud, Download, Globe, Play, Plus, RefreshCw, RotateCw, Server, Settings, Square, Trash, Upload } from '../components/Icons.jsx';

const emptyAccountForm = {
  name: '',
  accessKeyId: '',
  accessKeySecret: '',
  regionId: 'cn-hangzhou',
  description: '',
};

const ALIYUN_INSTANCE_COLUMNS = [
  { id: 'instance', role: 'primary', minWidth: 200 },
  { id: 'status', role: 'status' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 176 },
  { id: 'address', role: 'identifier', minWidth: 200 },
  { id: 'specification', role: 'meta', grow: 1, minWidth: 176 },
  { id: 'platform', role: 'meta', grow: 1, minWidth: 176 },
  { id: 'actions', role: 'actions-md', width: 144 },
];

const instanceIP = (inst) => inst.PublicIpAddress?.IpAddress?.[0] || inst.VpcAttributes?.PrivateIpAddress?.IpAddress?.[0] || '-';

const statusTone = (status) => {
  const value = String(status || '').toLowerCase();
  if (value === 'running' || value === 'active') return 'success';
  if (value === 'stopped') return 'danger';
  if (value.includes('ing')) return 'warning';
  return 'neutral';
};

const statusText = (status) => ({
  running: '运行中',
  stopped: '已停止',
  starting: '启动中',
  stopping: '停止中',
  active: '正常',
}[String(status || '').toLowerCase()] || status || '-');

function LoadingRows({ columns = 5, rows = 5 }) {
  return Array.from({ length: rows }, (_, row) => (
    <Table.Row key={row}>
      {Array.from({ length: columns }, (_, col) => (
        <Table.Cell key={col}><SkeletonLine className={col === 0 ? 'h-4 w-36' : 'h-4 w-20'} /></Table.Cell>
      ))}
    </Table.Row>
  ));
}

function CloudToolbar({ activeTab, setActiveTab, accounts, selectedAccountId, setSelectedAccountId, refreshing, refreshData }) {
  return (
    <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
      <Tabs
        {...MODULE_TABS_PROPS}
        value={activeTab}
        onValueChange={(value) => setActiveTab(String(value))}
        tabs={[
          { value: 'dns', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />DNS 解析</span> },
          { value: 'ecs', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />ECS 实例</span> },
          { value: 'swas', label: <span className="inline-flex items-center gap-1.5"><Cloud className="h-3.5 w-3.5" />轻量服务器</span> },
          { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-3.5 w-3.5" />账号管理</span> },
        ]}
      />
      {activeTab !== 'accounts' && (
        <TabBarOverflowActions
          items={[
            {
              key: 'account',
              type: 'select',
              label: '账号',
              icon: <Cloud className="h-3.5 w-3.5" />,
              value: selectedAccountId,
              onValueChange: (value) => setSelectedAccountId(String(value)),
              disabled: false,
              options: accounts.map((account) => ({ value: String(account.id), label: account.name })),
            },
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />,
              onClick: refreshData,
              disabled: refreshing || !selectedAccountId,
              loading: refreshing,
            },
          ]}
        />
      )}
    </div>
  );
}

function InstanceActions({ disabled, onAction }) {
  return (
    <div className="flex w-full justify-end gap-1">
      <Button size="sm" shape="square" variant="secondary" disabled={disabled.start} onClick={() => onAction('start')} aria-label="启动" title="启动" icon={<Play className="h-3.5 w-3.5 text-kumo-success" />} />
      <Button size="sm" shape="square" variant="secondary" disabled={disabled.stop} onClick={() => onAction('stop')} aria-label="停止" title="停止" icon={<Square className="h-3.5 w-3.5 text-kumo-danger" />} />
      <Button size="sm" shape="square" variant="secondary" onClick={() => onAction('reboot')} aria-label="重启" title="重启" icon={<RotateCw className="h-3.5 w-3.5 text-brand" />} />
    </div>
  );
}

function AliyunPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [activeTab, setActiveTab] = useState('dns');
  const [accounts, setAccounts] = useState([]);
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [domains, setDomains] = useState([]);
  const [instances, setInstances] = useState([]);
  const [swasInstances, setSwasInstances] = useState([]);
  const [loadingData, setLoadingData] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [showAddAccountModal, setShowAddAccountModal] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState(emptyAccountForm);
  const [submittingAccount, setSubmittingAccount] = useState(false);
  const [dnsColWidths, startDnsResize] = useTableResize([240, 100, 100, 320, 120]);
  const [accountsColWidths, startAccountsResize] = useTableResize([180, 240, 150, 280, 120]);

  const getAuthHeaders = useCallback(() => ({
    'Content-Type': 'application/json',
  }), []);

  const loadAccounts = useCallback(async () => {
    try {
      const response = await fetch('/api/aliyun/accounts', { headers: getAuthHeaders() });
      const data = await response.json();
      if (Array.isArray(data)) {
        setAccounts(data);
        if (data.length > 0 && !selectedAccountId) setSelectedAccountId(String(data[0].id));
      }
    } catch (error) {
      console.error('[Aliyun] 加载账号失败:', error);
      toast.error('加载阿里云账号失败');
    }
  }, [getAuthHeaders, selectedAccountId]);

  const accountImportInputRef = useRef(null);
  const [accountImporting, setAccountImporting] = useState(false);

  const exportAccounts = async () => {
    if (accounts.length === 0) { toast.warning('暂无账号可导出'); return; }
    try {
      const response = await fetch('/api/aliyun/accounts/export', { headers: getAuthHeaders() });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || payload.success !== true) throw new Error(payload.error || '导出账号失败');
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `aliyun-accounts-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
      toast.success(`已导出 ${payload.accounts?.length || 0} 个账号（含 AccessKey Secret，请注意保管）`);
    } catch (error) {
      toast.error(error.message || '导出账号失败');
    }
  };

  const importAccountsFromFile = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setAccountImporting(true);
    try {
      const data = JSON.parse(await file.text());
      const list = Array.isArray(data) ? data : (data.accounts || []);
      if (list.length === 0) throw new Error('文件中没有账号数据');
      if (!(await dialog.confirm(`确认导入 ${list.length} 个账号？已存在相同 AccessKey ID 的账号会自动跳过。`))) return;
      const response = await fetch('/api/aliyun/accounts/import', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ accounts: list, overwrite: false }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || payload.success !== true) throw new Error(payload.error || '导入账号失败');
      await loadAccounts();
      toast.success(`导入完成：新增 ${payload.imported ?? 0} 个，跳过 ${payload.skipped ?? 0} 个`);
    } catch (error) {
      toast.error(error.message || '导入账号失败');
    } finally {
      setAccountImporting(false);
    }
  };

  const loadDnsData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/aliyun/accounts/${accountId}/domains`, { headers: getAuthHeaders() });
      const result = await response.json();
      setDomains(result.Domains?.Domain || []);
    } catch {
      toast.error('加载域名列表失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadEcsData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/aliyun/accounts/${accountId}/instances`, { headers: getAuthHeaders() });
      const result = await response.json();
      setInstances(result.instances || []);
    } catch {
      toast.error('加载 ECS 实例失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const loadSwasData = useCallback(async (accountId) => {
    setLoadingData(true);
    try {
      const response = await fetch(`/api/aliyun/accounts/${accountId}/swas`, { headers: getAuthHeaders() });
      const result = await response.json();
      setSwasInstances(result.instances || []);
    } catch {
      toast.error('加载轻量服务器失败');
    } finally {
      setLoadingData(false);
    }
  }, [getAuthHeaders]);

  const refreshData = useCallback(() => {
    if (!selectedAccountId) return;
    setRefreshing(true);
    const task = activeTab === 'dns'
      ? loadDnsData(selectedAccountId)
      : activeTab === 'ecs'
        ? loadEcsData(selectedAccountId)
        : activeTab === 'swas'
          ? loadSwasData(selectedAccountId)
          : Promise.resolve();
    Promise.resolve(task).finally(() => setRefreshing(false));
  }, [activeTab, loadDnsData, loadEcsData, loadSwasData, selectedAccountId]);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (selectedAccountId && activeTab !== 'accounts') refreshData();
  }, [activeTab, selectedAccountId]);  

  const saveAccount = async () => {
    if (!accountForm.name || !accountForm.accessKeyId || (!editingAccount && !accountForm.accessKeySecret)) {
      toast.warning('请填写必填字段');
      return;
    }
    setSubmittingAccount(true);
    try {
      const isEdit = !!editingAccount;
      const response = await fetch(isEdit ? `/api/aliyun/accounts/${editingAccount.id}` : '/api/aliyun/accounts', {
        method: isEdit ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(accountForm),
      });
      const result = await response.json();
      if (!result.success && !result.id) throw new Error(result.error || '操作失败');
      toast.success(isEdit ? '账号已更新' : '账号已添加');
      setShowAddAccountModal(false);
      setEditingAccount(null);
      setAccountForm(emptyAccountForm);
      loadAccounts();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSubmittingAccount(false);
    }
  };

  const deleteAccount = async (account) => {
    if (!confirmPress(`account:${account.id}`, `删除阿里云账号「${account.name}」`)) return;
    try {
      const response = await fetch(`/api/aliyun/accounts/${account.id}`, { method: 'DELETE', headers: getAuthHeaders() });
      const result = await response.json();
      if (!result.success) throw new Error(result.error || '删除失败');
      toast.success('账号已删除');
      if (selectedAccountId === String(account.id)) setSelectedAccountId('');
      loadAccounts();
    } catch (error) {
      toast.error(error.message || '删除失败');
    }
  };

  const openCreateModal = () => {
    setEditingAccount(null);
    setAccountForm(emptyAccountForm);
    setShowAddAccountModal(true);
  };

  const openEditModal = (account) => {
    setEditingAccount(account);
    setAccountForm({
      name: account.name,
      accessKeyId: account.accessKeyId || '',
      accessKeySecret: '',
      regionId: account.regionId || 'cn-hangzhou',
      description: account.description || '',
    });
    setShowAddAccountModal(true);
  };

  const handleInstanceAction = async (kind, instance, action) => {
    const actionText = action === 'start' ? '启动' : action === 'stop' ? '停止' : '重启';
    if (!(await dialog.confirm(`确认${actionText}实例 ${instance.InstanceName || instance.InstanceId} 吗？`))) return;
    try {
      const endpoint = kind === 'swas' ? 'swas' : 'instances';
      const response = await fetch(`/api/aliyun/accounts/${selectedAccountId}/${endpoint}/${instance.InstanceId}/${action}`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ regionId: instance.RegionId }),
      });
      const result = await response.json();
      if (!result.success) throw new Error(result.error || `${actionText}失败`);
      toast.success(`${actionText}指令已下发`);
      setTimeout(refreshData, 2000);
    } catch (error) {
      toast.error(error.message || `${actionText}请求异常`);
    }
  };

  const renderDns = () => (
    <SectionCard title="DNS 域名" icon={<Globe className="h-4 w-4 text-brand" />} bodyPadding="none">
      <DataTableFrame variant="embedded" density="compact">
        <AppTable layout="fixed" widths={dnsColWidths}>
          <colgroup>{dnsColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="relative pr-6">域名<Table.ResizeHandle onMouseDown={(event) => startDnsResize(0, event)} /></Table.Head>
              <Table.Head className="relative pr-6">记录数<Table.ResizeHandle onMouseDown={(event) => startDnsResize(1, event)} /></Table.Head>
              <Table.Head className="relative pr-6">状态<Table.ResizeHandle onMouseDown={(event) => startDnsResize(2, event)} /></Table.Head>
              <Table.Head className="relative pr-6">备注<Table.ResizeHandle onMouseDown={(event) => startDnsResize(3, event)} /></Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {loadingData ? <LoadingRows columns={5} /> : domains.length === 0 ? (
              <Table.Row><Table.Cell colSpan={5}><EmptyState card={false} title="暂无域名数据" description="添加账号并选择后刷新数据。" /></Table.Cell></Table.Row>
            ) : domains.map((domain) => (
              <Table.Row key={domain.DomainId}>
                <Table.Cell className="font-semibold text-kumo-strong">{domain.DomainName}</Table.Cell>
                <Table.Cell className="tabular-nums">{domain.RecordCount || '-'}</Table.Cell>
                <Table.Cell><StatusBadge tone="success">正常</StatusBadge></Table.Cell>
                <Table.Cell className="truncate text-kumo-subtle">{domain.Remark || '-'}</Table.Cell>
                <Table.Cell><div className="flex w-full justify-end"><Button size="sm" variant="secondary">管理解析</Button></div></Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );

  const renderInstances = (kind) => {
    const items = kind === 'ecs' ? instances : swasInstances;
    const title = kind === 'ecs' ? 'ECS 实例' : '轻量应用服务器';
    return (
      <SectionCard title={title} icon={<Server className="h-4 w-4 text-brand" />} bodyPadding="none">
        <DataTableFrame variant="embedded" density="compact">
          <AppTable tableId={`aliyun-${kind}-instances`} columns={ALIYUN_INSTANCE_COLUMNS}>
            <Table.Header sticky variant="compact">
              <Table.Row>
                <Table.Head>实例</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>地域 / 可用区</Table.Head>
                <Table.Head>公网 / 内网</Table.Head>
                <Table.Head>规格</Table.Head>
                <Table.Head>{kind === 'ecs' ? '操作系统' : '到期时间'}</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {loadingData ? <LoadingRows columns={7} rows={6} /> : items.length === 0 ? (
                <Table.Row><Table.Cell colSpan={7}><EmptyState card={false} title={`暂无${title}`} description="添加账号并选择后刷新数据。" /></Table.Cell></Table.Row>
              ) : items.map((item) => {
                const status = kind === 'ecs' ? item.Status : item.Status;
                return (
                  <Table.Row key={item.InstanceId}>
                    <Table.Cell>
                      <div className="min-w-0">
                        <div className="truncate font-semibold text-kumo-strong">{item.InstanceName || item.InstanceId}</div>
                        <div className="truncate font-mono text-[11px] text-kumo-subtle">{item.InstanceId}</div>
                      </div>
                    </Table.Cell>
                    <Table.Cell><StatusBadge tone={statusTone(status)}>{statusText(status)}</StatusBadge></Table.Cell>
                    <Table.Cell className="truncate">{item.RegionName || '-'}</Table.Cell>
                    <Table.Cell className="font-mono text-[11px]">{kind === 'ecs' ? instanceIP(item) : item.PublicIpAddress || '-'}</Table.Cell>
                    <Table.Cell className="truncate">{item.InstanceTypeFriendly || '-'}</Table.Cell>
                    <Table.Cell className="truncate text-kumo-subtle">{kind === 'ecs' ? item.OSName || '-' : item.ExpiredTime ? new Date(item.ExpiredTime).toLocaleDateString() : '-'}</Table.Cell>
                    <Table.Cell className="text-right">
                      <InstanceActions
                        disabled={{ start: String(status).toLowerCase() === 'running', stop: String(status).toLowerCase() === 'stopped' }}
                        onAction={(action) => handleInstanceAction(kind, item, action)}
                      />
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      </SectionCard>
    );
  };

  const renderAccounts = () => (
    <SectionCard
      title="阿里云账号"
      description="Secret 保存后不回显"
      icon={<Cloud className="h-4 w-4 text-brand" />}
      action={(
        <div className="flex shrink-0 items-center gap-2">
          <Input
            ref={accountImportInputRef}
            type="file"
            accept=".json,application/json"
            aria-label="导入阿里云账号 JSON"
            className="hidden"
            onChange={importAccountsFromFile}
          />
          <Toolbar size="sm" aria-label="导出导入账号" className="shrink-0">
            <Toolbar.Button onClick={exportAccounts} disabled={accounts.length === 0} aria-label="导出账号" title="导出账号（含 AccessKey Secret）" icon={<Upload className="h-3.5 w-3.5" />}>
              <span className="hidden cq-sm:inline">导出</span>
            </Toolbar.Button>
            <Toolbar.Button onClick={() => accountImportInputRef.current?.click()} disabled={accountImporting} aria-label="导入账号" title="导入账号" icon={<Download className="h-3.5 w-3.5" />}>
              <span className="hidden cq-sm:inline">导入</span>
            </Toolbar.Button>
          </Toolbar>
          <Button size="sm" onClick={openCreateModal}><Plus className="h-3.5 w-3.5" />添加账号</Button>
        </div>
      )}
      bodyPadding="none"
    >
      <DataTableFrame variant="embedded" density="compact">
        <AppTable layout="fixed" widths={accountsColWidths}>
          <colgroup>{accountsColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(event) => startAccountsResize(0, event)} /></Table.Head>
              <Table.Head className="relative pr-6">AccessKey ID<Table.ResizeHandle onMouseDown={(event) => startAccountsResize(1, event)} /></Table.Head>
              <Table.Head className="relative pr-6">默认地域<Table.ResizeHandle onMouseDown={(event) => startAccountsResize(2, event)} /></Table.Head>
              <Table.Head className="relative pr-6">描述<Table.ResizeHandle onMouseDown={(event) => startAccountsResize(3, event)} /></Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {accounts.length === 0 ? (
              <Table.Row><Table.Cell colSpan={5}><EmptyState card={false} title="尚未配置账号" description="添加账号后加载资源。" action={<Button size="sm" onClick={openCreateModal}><Plus className="h-3.5 w-3.5" />添加账号</Button>} /></Table.Cell></Table.Row>
            ) : accounts.map((account) => (
              <Table.Row key={account.id} title="双击编辑账号" onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openEditModal(account))}>
                <Table.Cell><span className="font-semibold text-kumo-strong">{account.name}</span></Table.Cell>
                <Table.Cell className="font-mono text-[11px]">{account.accessKeyId}</Table.Cell>
                <Table.Cell>{account.regionId}</Table.Cell>
                <Table.Cell className="truncate text-kumo-subtle">{account.description || '-'}</Table.Cell>
                <Table.Cell className="text-right">
                  <div className="flex w-full justify-end gap-1">
                    <Button size="sm" shape="square" variant="secondary" onClick={() => openEditModal(account)} aria-label="编辑账号" title="编辑账号" icon={<Settings className="h-3.5 w-3.5" />} />
                    <Button size="sm" shape="square" variant={isArmed(`account:${account.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteAccount(account)} aria-label="删除账号" title="删除账号" icon={<Trash className="h-3.5 w-3.5" />} />
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );

  return (
    <PageStack viewport className="min-h-0 flex-1">
      <CloudToolbar
        activeTab={activeTab}
        setActiveTab={setActiveTab}
        accounts={accounts}
        selectedAccountId={selectedAccountId}
        setSelectedAccountId={setSelectedAccountId}
        refreshing={refreshing}
        refreshData={refreshData}
      />

      {activeTab === 'dns' && renderDns()}
      {activeTab === 'ecs' && renderInstances('ecs')}
      {activeTab === 'swas' && renderInstances('swas')}
      {activeTab === 'accounts' && renderAccounts()}

      <Dialog.Root open={showAddAccountModal} onOpenChange={setShowAddAccountModal}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),34rem)] w-[min(calc(100vw-2rem),34rem)] flex-col overflow-hidden p-0">
          <div className="border-b border-kumo-line bg-kumo-recessed/20 px-5 py-4">
            <Dialog.Title className="text-base font-semibold text-kumo-strong">{editingAccount ? '编辑阿里云账号' : '添加阿里云账号'}</Dialog.Title>
            <Dialog.Description className="mt-1 text-xs text-kumo-subtle">建议使用最小权限 RAM 账号。</Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
            <Input size="sm" label="备注名称" value={accountForm.name} onChange={(event) => setAccountForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="生产环境" />
            <div className="grid gap-3 cq-sm:grid-cols-2">
              <Input size="sm" label="AccessKey ID" value={accountForm.accessKeyId} onChange={(event) => setAccountForm((prev) => ({ ...prev, accessKeyId: event.target.value }))} className="font-mono" placeholder="LTAI..." />
              <Input size="sm" label="默认地域 ID" value={accountForm.regionId} onChange={(event) => setAccountForm((prev) => ({ ...prev, regionId: event.target.value }))} className="font-mono" placeholder="cn-hangzhou" />
            </div>
            <Input size="sm" label="AccessKey Secret" value={accountForm.accessKeySecret} onChange={(event) => setAccountForm((prev) => ({ ...prev, accessKeySecret: event.target.value }))} placeholder={editingAccount ? '不修改请留空' : undefined} autoComplete="off" spellCheck={false} className="font-mono" />
            <Textarea size="sm" label="账号描述" value={accountForm.description} onChange={(event) => setAccountForm((prev) => ({ ...prev, description: event.target.value }))} className="min-h-20" />
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-5 py-3">
            <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" onClick={saveAccount} loading={submittingAccount}>保存账号</Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default AliyunPage;
