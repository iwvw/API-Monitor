import React, { useCallback, useEffect, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { SectionCard, EmptyState, AppTable } from '../../components/ui/AppPrimitives.jsx';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { Mail, Plus, Trash, RefreshCw, Key, Copy, Inbox, Eye, GitBranch } from '../../components/Icons.jsx';
import { toast } from '../../modules/toast.js';
import { formatDate } from './utils.jsx';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';

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

const EMAIL_RULE_COLUMNS = [
  { id: 'matcher', role: 'primary', minWidth: 224 },
  { id: 'actionDetail', role: 'content', grow: 1 },
  { id: 'enabled', role: 'status' },
  { id: 'actions', role: 'actions-sm' },
];

const EMAIL_MESSAGE_COLUMNS = [
  { id: 'code', role: 'meta' },
  { id: 'mailbox', role: 'identifier' },
  { id: 'subject', role: 'content', grow: 1, verticalAlign: 'middle' },
  { id: 'receivedAt', role: 'datetime' },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg' },
];

// 邮件 Tab：管理 Cloudflare Email Routing。
// 左侧域名列表选择要管理的 zone；右侧展示该域名的开关、收件箱 Worker、目的地地址与路由规则。
// settings / rules / inbox 是 zone 级接口，addresses 是 account 级。
function EmailRoutingPanel({ selectedAccountId, cfApi }) {
  const { isArmed, confirmPress } = useConfirmPress();
  const [zones, setZones] = useState([]);
  const [zoneId, setZoneId] = useState('');
  const [loading, setLoading] = useState(false);
  const [settings, setSettings] = useState(null);
  const [addresses, setAddresses] = useState([]);
  const [rules, setRules] = useState([]);
  const [inboxState, setInboxState] = useState(null);
  const [messages, setMessages] = useState([]);
  const [busy, setBusy] = useState(false);
  const [addrLoading, setAddrLoading] = useState(false);
  const [newAddress, setNewAddress] = useState('');
  const [stats, setStats] = useState(null);
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
      setSettings(s || null);
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

  // 默认包含已消费：验证码被自动消费后才是正常工作的表现，
  // 若默认隐藏，用户会误以为「邮件没发出来」。
  const loadMessages = useCallback(async () => {
    try {
      const data = await emailcodeApi('/messages?limit=30&includeConsumed=1');
      setMessages(data?.messages || []);
    } catch {
      setMessages([]);
    }
  }, []);

  // 提码率是模板变更的早期信号：突降说明某站点改了邮件格式。
  const loadStats = useCallback(async () => {
    try {
      const data = await emailcodeApi('/stats?days=14');
      setStats(data || null);
    } catch {
      setStats(null);
    }
  }, []);

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
  useEffect(() => { if (inboxState?.deployed) { loadMessages(); loadStats(); } }, [inboxState?.deployed, loadMessages, loadStats]);

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

  const currentZone = zones.find(z => z.id === zoneId);
  const zoneName = currentZone?.name || settings?.name || '';

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

  const consumeMessage = async (msg) => {
    try {
      await emailcodeApi(`/messages/${msg.id}/consume`, { method: 'POST' });
      setMessages(prev => prev.filter(m => m.id !== msg.id));
      toast.success('已标记为已取用');
    } catch (e) {
      toast.error(`操作失败：${e.message}`);
    }
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

  // 清理：默认只清已取用的，避免误删还没用的验证码。
  const clearInbox = async () => {
    if (!confirmPress('email-clear', '清理已取用的邮件')) return;
    setBusy(true);
    try {
      const res = await emailcodeApi('/clear?onlyConsumed=1', { method: 'POST' });
      toast.success(`已清理 ${res?.deleted ?? 0} 封已取用邮件`);
      await Promise.all([loadMessages(), loadStats()]);
    } catch (e) {
      toast.error(`清理失败：${e.message}`);
    } finally { setBusy(false); }
  };

  return (
    <div className="grid min-w-0 items-start gap-3 grid-cols-1 cq-lg:grid-cols-3">
      {/* 左侧：域名列表（域名 + 是否启用转发） */}
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
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <Button size="sm" shape="square" variant={isArmed(`email-addr:${a.id}`) ? 'destructive' : 'secondary-destructive'} disabled={busy} onClick={() => deleteAddress(a)} aria-label={`删除 ${a.email}`} title="删除"><Trash className="h-3 w-3" /></Button>
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
      </div>

      {/* 右侧：所选域名的邮件路由详情 */}
      <div className="flex min-w-0 flex-col gap-3 cq-lg:col-span-2">
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
          <div className="flex min-w-0 flex-wrap items-center gap-2 px-4 py-3">
            <span className="min-w-0 truncate font-mono text-sm font-semibold text-kumo-strong" title={zoneName}>{zoneName || '—'}</span>
            {settings?.enabled
              ? <Badge variant="success" className="text-xs">已启用</Badge>
              : <Badge variant="error" className="text-xs">未启用</Badge>}
            <span className="ml-auto text-xs text-kumo-subtle">{addresses.length} 个地址 · {rules.length} 条规则</span>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-2 border-t border-kumo-line px-4 py-3 text-xs text-kumo-subtle">
            <Key className="h-3.5 w-3.5 shrink-0 text-brand" />
            <span className="min-w-0 flex-1" title="收件与转发互相独立，可选三种组合">
              发往 <span className="font-mono text-kumo-strong">{zoneName || '本域名'}</span> 的邮件按所选策略处理。
            </span>
            {inboxState?.deployed ? (
              <Badge variant="success" className="text-xs">已部署</Badge>
            ) : (
              <Badge variant="secondary" className="text-xs">未部署</Badge>
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
              <AppTable tableId="email-rules" columns={EMAIL_RULE_COLUMNS} className="w-full min-w-[38rem] text-xs">
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
                        <div className="text-kumo-strong">{ru.matcher}</div>
                        {ru.matcherValue ? <div className="truncate font-mono text-kumo-subtle" title={ru.matcherValue}>{ru.matcherValue}</div> : null}
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5 text-kumo-subtle">{ru.actions?.join('；')}</Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {ru.enabled ? <Badge variant="success" className="text-xs">开启</Badge> : <Badge variant="secondary" className="text-xs">关闭</Badge>}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <Button size="sm" shape="square" variant={isArmed(`email-rule:${ru.id}`) ? 'destructive' : 'secondary-destructive'} disabled={busy} onClick={() => deleteRule(ru)} aria-label={`删除规则 ${ru.matcher}`} title="删除"><Trash className="h-3 w-3" /></Button>
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

        {inboxState?.deployed && inboxState.strategy !== 'forward' ? (
          <SectionCard
            title="收件箱"
            icon={<Inbox className="h-4 w-4 text-brand" />}
            bodyPadding="none"
            actions={
              <div className="flex items-center gap-2">
                {stats ? (
                  <span className="text-xs text-kumo-subtle" title="近 14 天：收到的邮件数与成功提取验证码的比例">
                    近 14 天收 {stats.received} · 提码 {stats.extracted} · 成功率 {Math.round((stats.rate || 0) * 100)}%
                  </span>
                ) : null}
                <Button size="sm" variant="outline" disabled={busy} onClick={() => { loadMessages(); loadStats(); }} title="刷新收件箱与统计" icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
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
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <AppTable tableId="email-messages" columns={EMAIL_MESSAGE_COLUMNS} className="w-full min-w-[48rem] text-xs">
                  <Table.Header variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5">验证码</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5">收件人</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5">主题 / 发件人</Table.Head>
                      <Table.Head className="!px-2 !py-1.5">收件时间</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {messages.map(m => (
                      <Table.Row key={m.id} className="h-10">
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <span className="font-mono font-semibold tracking-widest text-kumo-strong">{m.code || '—'}</span>
                        </Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5"><span className="truncate font-mono text-kumo-subtle" title={m.mailbox}>{m.mailbox}</span></Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <div className="truncate text-kumo-strong" title={m.subject}>{m.subject || '—'}</div>
                          <div className="truncate font-mono text-kumo-subtle" title={m.sender}>{m.sender || ''}</div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 whitespace-nowrap text-kumo-subtle">{formatDate(m.receivedAt) || '—'}</Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex flex-col items-center gap-0.5">
                            {m.extractStatus === 'ok'
                              ? <Badge variant="success" className="text-xs">已提取</Badge>
                              : m.extractStatus === 'ambiguous'
                                ? <Badge variant="warning" className="text-xs">多候选</Badge>
                                : <Badge variant="secondary" className="text-xs">无验证码</Badge>}
                            {m.consumedAt ? <span className="text-[10px] text-kumo-subtle" title={`取用方 ${m.consumedBy || ''}`}>已被取用</span> : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex items-center justify-center gap-1">
                            <Button size="sm" shape="square" variant="secondary" disabled={busy} onClick={() => openDetail(m)} aria-label="查看正文" title="查看正文"><Eye className="h-3 w-3" /></Button>
                            <Button size="sm" shape="square" variant="secondary" disabled={busy} onClick={() => consumeMessage(m)} aria-label="标记已取用" title="标记已取用"><Copy className="h-3 w-3" /></Button>
                            <Button size="sm" shape="square" variant={isArmed(`email-msg:${m.id}`) ? 'destructive' : 'secondary-destructive'} disabled={busy} onClick={() => deleteMessage(m)} aria-label="删除邮件" title="删除"><Trash className="h-3 w-3" /></Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </div>
            ) : (
              <div className="p-4">
                <EmptyState card={false} title="收件箱为空" description="该域名收到的验证码邮件会显示在这里。" />
              </div>
            )}
          </SectionCard>
        ) : null}
      </div>

      <LayerDialog.Root open={!!detail} onOpenChange={open => { if (!open) setDetail(null); }}>
        <LayerDialog.Content size="lg">
          <LayerDialog.Title>{detail?.subject || '邮件详情'}</LayerDialog.Title>
          <LayerDialog.Description>
            <span className="font-mono">{detail?.sender || ''}</span>
            {detail?.receivedAt ? <span className="ml-2">{formatDate(detail.receivedAt)}</span> : null}
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2 text-xs text-kumo-subtle">
                <span className="font-mono">收件人 {detail?.mailbox || '—'}</span>
                {detail?.code ? <Badge variant="success" className="text-xs">验证码 {detail.code}</Badge> : null}
                {detail?.link ? (
                  <a className="truncate text-brand hover:underline" href={detail.link} target="_blank" rel="noreferrer">{detail.link}</a>
                ) : null}
              </div>
              <div className="whitespace-pre-wrap break-words rounded border border-kumo-line p-3 font-mono text-xs leading-relaxed text-kumo-strong">
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
