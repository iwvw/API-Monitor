import React, { useCallback, useEffect, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { SectionCard, EmptyState, AppTable } from '../../components/ui/AppPrimitives.jsx';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { Mail, Plus, Trash, RefreshCw, Key, Inbox, GitBranch } from '../../components/Icons.jsx';
import { toast } from '../../modules/toast.js';
import { formatDate } from './utils.jsx';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { ContextMenu } from '@cloudflare/kumo/primitives/context-menu';

// emailcode 收件箱接口不在 /api/cloudflare 前缀下，直接走原生 fetch。
const emailcodeApi = async (path, options = {}) => {
  const res = await fetch(`/api/emailcode${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok || data.success === false) {
    throw new Error(data.error || `请求失败：${res.status}`);
  }
  return data.data ?? data;
};

// 转发策略：收件与转发本可独立，三种组合各有取舍。
// 与后端 emailcode.maxRetentionDays 保持一致。
const MAX_RETENTION_DAYS = 90;

const FORWARD_STRATEGIES = [
  { value: 'inbox_and_forward', label: '收件 + 续转（推荐）' },
  { value: 'inbox_only', label: '只收件，不外发' },
  { value: 'forward', label: '只转发，不收件' },
];

const EMAIL_ZONE_COLUMNS = [
  { id: 'name', role: 'primary', grow: 1 },
  { id: 'enabled', role: 'status' },
];

const EMAIL_ADDRESS_COLUMNS = [
  { id: 'address', role: 'identifier', grow: 1 },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-sm' },
];

// 邮件 Tab：管理 Cloudflare Email Routing。
// 左侧域名列表选择要管理的 zone；右侧展示该域名的开关、收件箱 Worker、目的地地址与路由规则。
// settings / rules / inbox 是 zone 级接口，addresses 是 account 级。
function EmailRoutingPanel({ selectedAccountId, cfApi }) {
  const { isArmed, confirmPress } = useConfirmPress();
  const [zones, setZones] = useState([]);
  const [zoneId, setZoneId] = useState('');
  const [loading, setLoading] = useState(false);
  const [addresses, setAddresses] = useState([]);
  const [rules, setRules] = useState([]);
  const [inboxState, setInboxState] = useState(null);
  const [messages, setMessages] = useState([]);
  const [retentionDays, setRetentionDays] = useState(7);
  const [savedRetentionDays, setSavedRetentionDays] = useState(7);
  const [retentionSaving, setRetentionSaving] = useState(false);
  const [busy, setBusy] = useState(false);
  const [addrLoading, setAddrLoading] = useState(false);
  const [newAddress, setNewAddress] = useState('');
  const [detail, setDetail] = useState(null);
  const [strategy, setStrategy] = useState('inbox_and_forward');

  const base = `/accounts/${selectedAccountId}/email/routing`;
  const zoneQuery = zoneId ? `?zoneId=${encodeURIComponent(zoneId)}` : '';

  const loadZones = useCallback(async () => {
    if (!selectedAccountId) return;
    try {
      const data = await cfApi(`${base}/zones`);
      const list = data?.zones || [];
      setZones(list);
      const prefer = list.find(z => z.name === 'dmuk.org') || list[0];
      setZoneId(prev => prev || prefer?.id || '');
    } catch (e) {
      toast.error(`加载域名列表失败：${e.message}`);
    }
  }, [selectedAccountId, base, cfApi]);

  const load = useCallback(async () => {
    if (!selectedAccountId || !zoneId) return;
    setLoading(true);
    try {
      const [s, r, inbox] = await Promise.all([
        cfApi(`${base}${zoneQuery}`),
        cfApi(`${base}/rules${zoneQuery}`),
        cfApi(`${base}/inbox${zoneQuery}`),
      ]);
      setRules(r?.rules || []);
      setInboxState(inbox || null);
      if (inbox?.strategy) setStrategy(inbox.strategy);
      if (s?.enabled !== undefined) {
        setZones(prev => prev.map(z => (z.id === zoneId ? { ...z, enabled: !!s.enabled, status: s.status || z.status } : z)));
      }
    } catch (e) {
      toast.error(`加载邮件路由失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  }, [selectedAccountId, base, zoneId, zoneQuery, cfApi]);

  // 目的地地址是账号级资源，与所选域名无关，单独加载。
  const loadAddresses = useCallback(async () => {
    if (!selectedAccountId) return;
    setAddrLoading(true);
    try {
      const a = await cfApi(`${base}/addresses`);
      setAddresses(a?.addresses || []);
    } catch (e) {
      toast.error(`加载目的地地址失败：${e.message}`);
    } finally {
      setAddrLoading(false);
    }
  }, [selectedAccountId, base, cfApi]);

  // 默认包含已消费:被 API 消费者(如长轮询)认领过的邮件仍保留在视图中,
  // 若默认隐藏,用户会误以为邮件没收到。
  const loadMessages = useCallback(async () => {
    try {
      const data = await emailcodeApi('/messages?limit=30&includeConsumed=1');
      setMessages(data?.messages || []);
    } catch {
      setMessages([]);
    }
  }, []);

  // 保留天数由后端 emailcode_settings 持久化(全域名共用,默认 7 天)。
  const loadRetention = useCallback(async () => {
    try {
      const data = await emailcodeApi('/settings');
      const days = Number(data?.retentionDays) || 7;
      setRetentionDays(days);
      setSavedRetentionDays(days);
    } catch {
      // 读取失败时保留默认 7 天,不打断收件箱展示。
    }
  }, []);

  const saveRetention = async () => {
    const days = Number(retentionDays);
    if (!Number.isInteger(days) || days < 1 || days > MAX_RETENTION_DAYS) {
      setRetentionDays(savedRetentionDays);
      toast.warning(`保留天数需在 1-${MAX_RETENTION_DAYS} 之间`);
      return;
    }
    if (days === savedRetentionDays) return;
    setRetentionSaving(true);
    try {
      const data = await emailcodeApi('/settings', { method: 'PUT', body: JSON.stringify({ retentionDays: days }) });
      const saved = Number(data?.retentionDays) || days;
      setRetentionDays(saved);
      setSavedRetentionDays(saved);
      toast.success(`保留期已设为 ${saved} 天,超期邮件已清理`);
      await loadMessages();
    } catch (e) {
      setRetentionDays(savedRetentionDays);
      toast.error(`保存保留期失败:${e.message}`);
    } finally {
      setRetentionSaving(false);
    }
  };

  const openDetail = async (msg) => {
    try {
      const full = await emailcodeApi(`/messages/${msg.id}`);
      setDetail(full);
    } catch (e) {
      toast.error(`加载邮件详情失败：${e.message}`);
    }
  };

  useEffect(() => { loadZones(); loadAddresses(); }, [loadZones, loadAddresses]);
  useEffect(() => { load(); }, [load]);
  // 收件箱是全域名汇总,与当前所选域名是否部署 Worker 无关,直接加载。
  useEffect(() => { loadMessages(); loadRetention(); }, [loadMessages, loadRetention]);

  if (!selectedAccountId) {
    return (
      <EmptyState
        card={false}
        icon={Mail}
        title="未选择账号"
        description="请先在顶部选择 Cloudflare 账号。"
      />
    );
  }

  const addAddress = async () => {
    const email = newAddress.trim();
    if (!email) { toast.warning('请输入邮箱地址'); return; }
    if (addresses.some(a => a.email === email)) { toast.warning('该地址已在列表中'); return; }
    setBusy(true);
    try {
      const res = await cfApi(`${base}/addresses`, { method: 'POST', body: JSON.stringify({ email }) });
      toast.success('地址已添加，请到 Cloudflare 后台验证邮箱');
      if (res?.address) setAddresses(prev => [...prev, res.address]);
      setNewAddress('');
    } catch (e) {
      toast.error(`添加地址失败：${e.message}`);
    } finally { setBusy(false); }
  };

  const deleteAddress = async (addr) => {
    if (!confirmPress(`email-addr:${addr.id}`, `删除地址「${addr.email}」`)) return;
    setBusy(true);
    try {
      await cfApi(`${base}/addresses/${addr.id}`, { method: 'DELETE' });
      setAddresses(prev => prev.filter(a => a.id !== addr.id));
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    } finally { setBusy(false); }
  };

  const deployInbox = async () => {
    if (!zoneId) { toast.warning('请先选择域名'); return; }
    setBusy(true);
    try {
      await cfApi(`${base}/inbox`, {
        method: 'POST',
        body: JSON.stringify({ zoneId, strategy }),
      });
      toast.success(strategy === 'forward' ? '已切换为只转发' : '收件箱 Worker 已部署');
      await load();
    } catch (e) {
      toast.error(`操作失败：${e.message}`);
    } finally { setBusy(false); }
  };

  const removeInbox = async () => {
    if (inboxState?.workerName && !confirmPress(`email-inbox:${zoneId}`, `卸载收件箱 Worker「${inboxState.workerName}」并恢复原邮件路由`)) return;
    setBusy(true);
    try {
      await cfApi(`${base}/inbox${zoneQuery}`, { method: 'DELETE' });
      toast.success('收件箱 Worker 已卸载');
      setMessages([]);
      await load();
    } catch (e) {
      toast.error(`卸载失败：${e.message}`);
    } finally { setBusy(false); }
  };

  const deleteRule = async (rule) => {
    if (!confirmPress(`email-rule:${rule.id}`, `删除规则「${rule.name || rule.matcher}」`)) return;
    setBusy(true);
    try {
      await cfApi(`${base}/rules/${rule.id}${zoneQuery}`, { method: 'DELETE' });
      setRules(prev => prev.filter(r => r.id !== rule.id));
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    } finally { setBusy(false); }
  };

  const deleteMessage = async (msg) => {
    if (!confirmPress(`email-msg:${msg.id}`, `删除邮件「${msg.subject || msg.mailbox}」`)) return;
    try {
      await emailcodeApi(`/messages/${msg.id}`, { method: 'DELETE' });
      setMessages(prev => prev.filter(m => m.id !== msg.id));
      toast.success('邮件已删除');
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  // 清理:默认只清已取用的,避免误删未读邮件。
  const clearInbox = async () => {
    if (!confirmPress('email-clear', '清理已取用的邮件')) return;
    setBusy(true);
    try {
      const res = await emailcodeApi('/clear?onlyConsumed=1', { method: 'POST' });
      toast.success(`已清理 ${res?.deleted ?? 0} 封已取用邮件`);
      await loadMessages();
    } catch (e) {
      toast.error(`清理失败：${e.message}`);
    } finally { setBusy(false); }
  };

  return (
    <div className="grid min-w-0 items-start gap-3 grid-cols-1 cq-lg:grid-cols-[minmax(0,30rem)_minmax(0,1fr)]">
      {/* 左列:域名 / 目的地地址 / 邮件路由 / 路由规则,随页面自由滚动 */}
      <div className="flex min-w-0 flex-col gap-2 cq-lg:self-start">
        <div className="dns-table-frame flex max-w-full">
          <div className="dns-table-scroll scrollbar-thin">
            <AppTable tableId="email-zones" columns={EMAIL_ZONE_COLUMNS} className="w-full min-w-[15rem] text-xs">
              <Table.Header sticky variant="compact">
                <Table.Row className="h-8">
                  <Table.Head className="!px-2.5 !py-1.5 text-left">域名</Table.Head>
                  <Table.Head className="!px-2 !py-1.5 text-center">转发</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {zones.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={2} className="py-10 text-center text-kumo-subtle">该账号下暂无域名。</Table.Cell>
                  </Table.Row>
                ) : zones.map(z => (
                  <Table.Row
                    key={z.id}
                    variant={z.id === zoneId ? 'selected' : 'default'}
                    className="h-9 cursor-pointer"
                    title={z.name}
                    onClick={() => setZoneId(z.id)}
                  >
                    <Table.Cell className="!px-2.5 !py-1.5 text-left">
                      <div className="flex min-w-0">
                        <span className="truncate font-semibold text-kumo-strong">{z.name}</span>
                      </div>
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      {z.enabled
                        ? <Badge variant="success" className="text-[10px] leading-4">已启用</Badge>
                        : <Badge variant="secondary" className="text-[10px] leading-4">未启用</Badge>}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </div>
        </div>

        {/* 目的地地址是 Cloudflare 账号级资源，不属于某个域名，所有域名共用。 */}
        <SectionCard
          title="目的地地址"
          icon={<Mail className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={<span className="text-xs text-kumo-subtle"></span>}
        >
          <div className="flex min-w-0 items-center gap-2 border-b border-kumo-line px-3 py-2">
            <Input
              size="sm"
              className="min-w-0 flex-1"
              aria-label="添加转发目标邮箱"
              placeholder="me@gmail.com"
              value={newAddress}
              onChange={e => setNewAddress(e.target.value)}
              disabled={busy}
              onKeyDown={e => { if (e.key === 'Enter' && !e.nativeEvent.isComposing) addAddress(); }}
            />
            <Button size="sm" variant="primary" disabled={busy || !newAddress.trim()} onClick={addAddress} aria-label="添加转发目标邮箱">
              <Plus className="h-3.5 w-3.5" /> 添加
            </Button>
          </div>
          {addrLoading || addresses.length ? (
          <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
            <AppTable tableId="email-addresses" columns={EMAIL_ADDRESS_COLUMNS} className="w-full min-w-[17rem] text-xs">
              <Table.Header variant="compact">
                <Table.Row className="h-8">
                  <Table.Head className="!px-2.5 !py-1.5">地址</Table.Head>
                  <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                  <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {addrLoading ? (
                  Array.from({ length: 2 }).map((_, i) => <Table.Row key={i} className="h-9"><Table.Cell colSpan={3} className="!px-2.5 !py-1.5"><SkeletonLine className="h-3.5 w-full" /></Table.Cell></Table.Row>)
                ) : addresses.map(a => (
                  <Table.Row key={a.id} className="h-10">
                    <Table.Cell className="!px-2.5 !py-1.5"><span className="truncate font-mono text-kumo-strong" title={a.email}>{a.email}</span></Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      {a.verified
                        ? <Badge variant="success" className="text-xs">已验证</Badge>
                        : <Badge variant="warning" className="text-xs">待验证</Badge>}
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5">
                      <div className="mx-auto flex items-center justify-center">
                        <Button size="sm" shape="square" variant={isArmed(`email-addr:${a.id}`) ? 'destructive' : 'secondary-destructive'} disabled={busy} onClick={() => deleteAddress(a)} aria-label={`删除 ${a.email}`} title="删除"><Trash className="h-3 w-3" /></Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </div>
        ) : (
          <div className="p-4">
            <EmptyState card={false} title="暂无地址" description="先添加一个转发目标邮箱，再到 Cloudflare 后台完成验证。" />
          </div>
        )}
        </SectionCard>
        <SectionCard
          title="邮件路由"
          icon={<Mail className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <Button size="sm" variant="outline" disabled={loading} onClick={() => { loadZones(); load(); }} title="重新拉取域名、规则与收件箱状态">
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} /> 刷新
            </Button>
          }
        >
          <div className="flex min-w-0 flex-wrap items-center gap-2 px-4 py-3 text-xs text-kumo-subtle">
            {inboxState?.deployed ? (
              <Badge variant="success" className="ml-auto text-xs">已部署</Badge>
            ) : (
              <Badge variant="secondary" className="ml-auto text-xs">未部署</Badge>
            )}
            <Select
              size="sm"
              className="w-48"
              aria-label="转发策略"
              value={strategy}
              onValueChange={v => setStrategy(String(v))}
              items={FORWARD_STRATEGIES}
            />
            {inboxState?.deployed && inboxState.strategy !== 'forward' ? (
              <Button size="sm" variant={isArmed(`email-inbox:${zoneId}`) ? 'destructive' : 'secondary-destructive'} disabled={busy} onClick={removeInbox} title="卸载 Worker 并恢复原邮件路由" icon={<Trash className="h-4 w-4" />}>
                卸载
              </Button>
            ) : null}
            <Button size="sm" variant="primary" disabled={busy || !zoneId} onClick={deployInbox} title="按所选策略应用收件箱 / 转发配置" icon={<Key className="h-4 w-4" />}>
              {inboxState?.deployed && inboxState.strategy === 'forward' ? '应用' : '部署并应用'}
            </Button>
          </div>
        </SectionCard>

        <SectionCard title="路由规则" icon={<GitBranch className="h-4 w-4 text-brand" />} bodyPadding="none">
          {loading || rules.length ? (
            <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
              <AppTable tableId="email-rules" className="w-full min-w-[26rem] text-xs">
                <colgroup>
                  <col />
                  <col />
                  <col style={{ width: 72 }} />
                  <col style={{ width: 56 }} />
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2.5 !py-1.5">匹配条件</Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">动作</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">启用</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {loading ? (
                    Array.from({ length: 2 }).map((_, i) => <Table.Row key={i} className="h-9"><Table.Cell colSpan={4} className="!px-2.5 !py-1.5"><SkeletonLine className="h-3.5 w-full" /></Table.Cell></Table.Row>)
                  ) : rules.map(ru => (
                    <Table.Row key={ru.id} className="h-10">
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="max-w-[18rem] truncate text-kumo-strong" title={ru.matcher}>{ru.matcher}</div>
                        {ru.matcherValue ? <div className="max-w-[18rem] truncate font-mono text-kumo-subtle" title={ru.matcherValue}>{ru.matcherValue}</div> : null}
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5"><span className="block max-w-[14rem] truncate text-kumo-subtle" title={ru.actions?.join('；')}>{ru.actions?.join('；')}</span></Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {ru.enabled ? <Badge variant="success" className="text-xs">开启</Badge> : <Badge variant="secondary" className="text-xs">关闭</Badge>}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5">
                        <div className="mx-auto flex items-center justify-center">
                          <Button size="sm" shape="square" variant={isArmed(`email-rule:${ru.id}`) ? 'destructive' : 'secondary-destructive'} disabled={busy} onClick={() => deleteRule(ru)} aria-label={`删除规则 ${ru.matcher}`} title="删除"><Trash className="h-3 w-3" /></Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </AppTable>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState card={false} title="暂无规则" description="部署收件箱 Worker 会自动把 catch-all 指向它，此处可查看规则。" />
            </div>
          )}
        </SectionCard>

      </div>

      {/* 右列:仅收件箱(全域名汇总),吸顶全高、列表内部滚动 */}
      <section className="flex min-w-0 flex-col gap-2 cq-lg:sticky cq-lg:top-[132px] cq-lg:h-[calc(100dvh-144px)]">
        <SectionCard
          title="收件箱"
          icon={<Inbox className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          className="min-h-0 cq-lg:flex-1"
          bodyClassName="min-h-0 cq-lg:flex-1"
          actions={
            <div className="flex items-center gap-2">
              <span className="flex items-center gap-1 text-xs text-kumo-subtle" title="邮件保存时间:超期邮件会在保存时与每小时清理时删除">
                保留
                <Input
                  size="sm"
                  type="number"
                  min="1"
                  max={MAX_RETENTION_DAYS}
                  step="1"
                  className="w-16"
                  aria-label="邮件保留天数"
                  value={retentionDays}
                  disabled={retentionSaving}
                  onChange={e => setRetentionDays(Number(e.target.value))}
                  onBlur={saveRetention}
                  onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); e.currentTarget.blur(); } }}
                />
                天
              </span>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => loadMessages()} title="刷新收件箱" icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
              <Button
                size="sm"
                variant={isArmed('email-clear') ? 'destructive' : 'secondary-destructive'}
                disabled={busy}
                onClick={clearInbox}
                title="清理已取用的邮件（未取用的会保留）"
                icon={<Trash className="h-3.5 w-3.5" />}
              >
                清理
              </Button>
            </div>
          }
        >
          {messages.length ? (
            <div className="min-h-0 min-w-0 flex-1 overflow-auto overscroll-contain scrollbar-thin">
              <AppTable tableId="email-messages" className="w-full min-w-[32rem] text-xs">
                <colgroup>
                  <col />
                  <col />
                  <col style={{ width: 160 }} />
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2.5 !py-1.5">收件人</Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">主题 / 发件人</Table.Head>
                    <Table.Head className="!px-2 !py-1.5">收件时间</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {messages.map(m => (
                    <ContextMenu.Root key={m.id}>
                      <ContextMenu.Trigger
                        render={
                          <Table.Row
                            className="h-10 cursor-pointer"
                            tabIndex={0}
                            onClick={() => openDetail(m)}
                            onKeyDown={event => {
                              if (event.key === 'Enter' || event.key === ' ') {
                                event.preventDefault();
                                openDetail(m);
                              }
                            }}
                          />
                        }
                      >
                        <Table.Cell className="!px-2.5 !py-1.5"><span className="block max-w-[18rem] truncate font-mono text-sm text-kumo-strong" title={m.mailbox}>{m.mailbox}</span></Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <div className="max-w-[32rem] truncate text-kumo-strong" title={m.subject}>{m.subject || '—'}</div>
                          <div className="max-w-[32rem] truncate font-mono text-kumo-subtle" title={m.sender}>{m.sender || ''}</div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap text-kumo-subtle">{formatDate(m.receivedAt) || '—'}</Table.Cell>
                      </ContextMenu.Trigger>
                      <ContextMenu.Portal>
                        <ContextMenu.Positioner sideOffset={6}>
                          <ContextMenu.Popup className="z-50 min-w-40 overflow-hidden rounded-lg border border-kumo-line bg-kumo-control p-1.5 text-kumo-default outline-none">
                            <ContextMenu.Item
                              className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none data-highlighted:bg-kumo-overlay"
                              onClick={event => { event.stopPropagation(); openDetail(m); }}
                            >
                              查看正文
                            </ContextMenu.Item>
                            <ContextMenu.Item
                              className="relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-hidden select-none data-highlighted:bg-kumo-overlay"
                              onClick={event => { event.stopPropagation(); deleteMessage(m); }}
                            >
                              删除
                            </ContextMenu.Item>
                          </ContextMenu.Popup>
                        </ContextMenu.Positioner>
                      </ContextMenu.Portal>
                    </ContextMenu.Root>
                  ))}
                </Table.Body>
              </AppTable>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState card={false} title="收件箱为空" description="各域名收到的邮件都汇总在这里(与左侧选中的域名无关),点击行查看正文。" />
            </div>
          )}
        </SectionCard>
      </section>

      <LayerDialog.Root open={!!detail} onOpenChange={open => { if (!open) setDetail(null); }}>
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>{detail?.subject || '邮件详情'}</LayerDialog.Title>
          <LayerDialog.Description>
            <span className="font-mono">{detail?.sender || ''}</span>
            {detail?.receivedAt ? <span className="ml-2">{formatDate(detail.receivedAt)}</span> : null}
          </LayerDialog.Description>
          <LayerDialog.Body>
            {/* w-0 min-w-full:让内容按弹窗宽度铺满,而不是被正文内容撑开/压缩
                (与「部署 Agent」弹窗同款修复,见 ServerModals.jsx)。 */}
            <div className="flex w-0 min-w-full flex-col gap-3 text-xs text-kumo-default">
              <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs text-kumo-subtle">
                <span className="truncate font-mono">收件人 {detail?.mailbox || '—'}</span>
              </div>
              <div className="min-w-0 whitespace-pre-wrap break-words rounded border border-kumo-line p-3 font-mono text-xs leading-relaxed text-kumo-strong">
                {detail?.textBody || '（无正文）'}
              </div>
            </div>
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </div>
  );
}

export default EmailRoutingPanel;
