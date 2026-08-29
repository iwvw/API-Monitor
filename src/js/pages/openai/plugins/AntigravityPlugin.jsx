import { useEffect, useRef, useState } from 'react';
import { Button, Switch, Select, Loader, Input, Dialog, Table, Badge, Toolbar } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, Plus, Upload, Download, Trash, RefreshCw, Edit } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders } from '../utils.js';

const API = '/api/antigravity';

// 配额标签中文化：上游返回的英文标签映射为中文，未命中时原样返回。
const QUOTA_LABEL_ZH = {
  // creditType
  'GOOGLE_ONE_AI': 'AI 积分',
  'AI Credits': 'AI 积分',
  'AI credits': 'AI 积分',
  credits: '积分',
  // 档位
  'Paid Tier': '付费档位',
  'Free Tier': '免费档位',
  Pro: '专业版',
  Ultra: '至尊版',
  // 窗口/限额短语
  'Weekly Limit Remaining': '本周剩余额度',
  'Daily Limit Remaining': '今日剩余额度',
  'Monthly Limit Remaining': '本月剩余额度',
  'Hourly Limit Remaining': '本小时剩余额度',
  'Five Hour Limit Remaining': '五小时剩余额度',
  'Weekly': '每周',
  'Daily': '每日',
  'Monthly': '每月',
  'Hourly': '每小时',
  'Requests': '请求数',
  requests: '请求数',
  'Tokens': 'Token',
  tokens: 'Token',
  'Search': '搜索',
  search: '搜索',
  // 模型档位
  'Standard': '标准',
  'Extended': '扩展',
  'Flash': 'Flash',
  'Pro High': '专业高',
  'Pro Low': '专业低',
  'Flash High': 'Flash 高',
  'Flash Medium': 'Flash 中',
  'Flash Low': 'Flash 低',
};
const zhLabel = (value) => (value ? (QUOTA_LABEL_ZH[value] || value) : value);

// AntigravityPlugin：模型网关「插件中心」详情——Claude 订阅转 API。
// Google 账号授权后，对外提供 Anthropic Messages 兼容端点。
export function AntigravityPlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [linkState, setLinkState] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [proxypoolPools, setProxypoolPools] = useState([]);
  const [oauthOpen, setOauthOpen] = useState(false);
  const [oauthAuthUrl, setOauthAuthUrl] = useState('');
  const [oauthCallback, setOauthCallback] = useState('');
  const [oauthName, setOauthName] = useState('');
  const [oauthBusy, setOauthBusy] = useState(false);
  const [linkBusy, setLinkBusy] = useState(false);
  const [editAccount, setEditAccount] = useState(null);
  const [editForm, setEditForm] = useState({ name: '', planType: '' });
  const [quota, setQuota] = useState(null);
  const [quotaLoading, setQuotaLoading] = useState(false);
  const quotaPrevRef = useRef(null); // 上次 remainingFraction 快照，用于检测窗口是否在消耗
  const fileInputRef = useRef(null);

  const load = async () => {
    try {
      const res = await fetch(`${API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
    } catch (e) {
      toast.error(`插件设置加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  const loadAccounts = async () => {
    try {
      const res = await fetch(`${API}/accounts`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setAccounts(data.accounts || []);
    } catch {
      /* 静默 */
    }
  };

  const loadQuota = async () => {
    setQuotaLoading(true);
    try {
      // 多账号：为每个账号单独拉配额；无账号时拉默认（轮询）账号。
      const targets = accounts.length ? accounts.map(a => a.email) : [''];
      const results = await Promise.all(
        targets.map(async email => {
          const q = email ? `?email=${encodeURIComponent(email)}` : '';
          const res = await fetch(`${API}/quota${q}`, { headers: getAuthHeaders() });
          const data = await res.json();
          if (!res.ok || !data?.success) return { email, quota: null, error: data?.error };
          return { email, quota: data.quota || null, error: null };
        })
      );
      setQuota(results);
    } catch {
      setQuota([]);
    } finally {
      setQuotaLoading(false);
    }
  };

  const loadLink = async () => {
    try {
      const res = await fetch(`${API}/link`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setLinkState(data);
    } catch {
      setLinkState(null);
    }
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch(`${API}/status`, { headers: getAuthHeaders() });
        const data = await res.json();
        if (res.ok && !cancelled) setStatus(data);
      } catch {
        if (!cancelled) setStatus(null);
      }
    })();
    return () => { cancelled = true; };
  }, [settings?.enabled, settings?.proxyPoolId]);

  useEffect(() => {
    loadAccounts();
    loadLink();
  }, []);

  useEffect(() => {
    if (status?.authorized) {
      loadQuota();
    }
  }, [status?.authorized]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch('/api/proxypool', { headers: getAuthHeaders() });
        const data = await res.json();
        if (res.ok && data?.success && !cancelled) setProxypoolPools(data.pools || []);
      } catch {
        if (!cancelled) setProxypoolPools([]);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  const save = async (next, msg = '设置已保存') => {
    if (!next) return false;
    setSaving(true);
    try {
      const res = await fetch(`${API}/settings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(next),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      setSettings(data.settings);
      toast.success(msg);
      return true;
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const update = (patch, silent = false) => {
    const next = { ...(settings || {}), ...patch };
    setSettings(next);
    if (!silent) void save(next);
  };

  const startOAuth = async () => {
    setOauthBusy(true);
    try {
      const res = await fetch(`${API}/oauth/auth-url`, { method: 'POST', headers: getAuthHeaders(), body: '{}' });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '生成授权链接失败');
      setOauthAuthUrl(data.authUrl);
      setOauthCallback('');
      setOauthOpen(true);
    } catch (e) {
      toast.error(e.message);
    } finally {
      setOauthBusy(false);
    }
  };

  const finishOAuth = async () => {
    setOauthBusy(true);
    try {
      const res = await fetch(`${API}/oauth/exchange`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ callbackUrl: oauthCallback.trim(), name: oauthName.trim() }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '授权失败');
      toast.success(`授权成功：${data.email}（${data.planType}）`);
      setOauthOpen(false);
      await loadAccounts();
      setStatus(prev => ({ ...prev, authorized: true, accountCount: (prev?.accountCount || 0) + 1 }));
    } catch (e) {
      toast.error(`授权失败：${e.message}`);
    } finally {
      setOauthBusy(false);
    }
  };

  const deleteAccount = async email => {
    if (!confirmPress(`antigravity-account-delete:${email}`, `删除账号 ${email}`)) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(email)}`, { method: 'DELETE', headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '删除失败');
      toast.success('账号已删除');
      await loadAccounts();
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  const toggleAccount = async (email, disabled) => {
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(email)}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ disabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      await loadAccounts();
    } catch (e) {
      toast.error(e.message);
    }
  };

  const openEditAccount = account => {
    setEditForm({ name: account.name || '', planType: account.planType || '' });
    setEditAccount(account);
  };

  const saveEditAccount = async () => {
    if (!editAccount) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(editAccount.email)}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ name: editForm.name.trim(), planType: editForm.planType.trim() }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      toast.success('账号已更新');
      setEditAccount(null);
      await loadAccounts();
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    }
  };

  const importAuthFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(String(reader.result || '{}'));
        const list = Array.isArray(parsed) ? parsed : parsed?.accounts || [];
        if (!list.length) throw new Error('文件中没有账号');
        const res = await fetch(`${API}/accounts`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ accounts: list }),
        });
        const data = await res.json();
        if (!res.ok || !data?.success) throw new Error(data?.error || '导入失败');
        toast.success(`已导入 ${data.added || 0} 个账号`);
        await loadAccounts();
      } catch (e) {
        toast.error(`导入失败：${e.message}`);
      }
    };
    reader.readAsText(file);
  };

  const exportAuthFile = () => {
    window.open(`${API}/accounts/export`, '_blank');
  };

  const linkPlugin = async action => {
    setLinkBusy(true);
    try {
      const res = await fetch(`${API}/link`, {
        method: action === 'link' ? 'POST' : 'DELETE',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || (action === 'link' ? '接入失败' : '断开失败'));
      setLinkState(data);
      toast.success(action === 'link' ? '已接入模型网关端点列表' : '已从端点列表移除');
    } catch (e) {
      toast.error(e.message);
    } finally {
      setLinkBusy(false);
    }
  };

  const field = (label, desc, control) => (
    <FieldRow title={<span title={desc}>{label}</span>}>
      {control}
    </FieldRow>
  );

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  const authorized = !!status?.authorized;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex items-center justify-end">
        <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>

      <div className="flex min-w-0 flex-col gap-3">
        <SectionCard title="Antigravity" icon={<Rocket className="h-4 w-4 text-brand" />} bodyPadding="none">
          {field(
            '启用中继',
            '关闭后 /v1/messages 与网关端点接入都会拒绝服务',
            <Switch checked={!!settings?.enabled} onCheckedChange={v => update({ enabled: v })} />
          )}
          {field(
            '接入模型网关',
            '把本插件注册为模型网关端点，外部客户端经网关 /v1/messages 路由到本中继',
            <div className="flex items-center gap-2">
              <Switch
                checked={!!linkState?.linked}
                disabled={linkBusy || !settings?.enabled}
                onCheckedChange={checked => linkPlugin(checked ? 'link' : 'unlink')}
              />
            </div>
          )}
          {field(
            '代理池',
            '引用「代理池」插件管理的池，按请求轮询出口并共享冷却/429 禁用状态',
            <Select
              size="sm"
              className="w-full max-w-md"
              value={settings?.proxyPoolId || ''}
              onValueChange={v => update({ proxyPoolId: v || '' })}
              placeholder="不使用"
            >
              <Select.Option value="">不使用</Select.Option>
              {proxypoolPools.map(p => (
                <Select.Option key={p.id} value={p.id}>
                  {p.name || p.id}（{p.proxies?.length || 0} 个出口）
                </Select.Option>
              ))}
            </Select>
          )}
          {field(
            '配额刷新检测',
            '后台每 15 分钟检测各账号配额窗口是否在消耗/已刷新，状态变化时按通知规则推送',
            <Switch checked={!!settings?.quotaMonitorEnabled} onCheckedChange={v => update({ quotaMonitorEnabled: v })} />
          )}
        </SectionCard>

        <SectionCard
          title="Google 账号"
          bodyPadding="none"
          actions={
            <div className="flex items-center gap-1.5">
              <Toolbar size="sm" aria-label="账号导出导入" className="shrink-0">
                <Toolbar.Button onClick={exportAuthFile} icon={<Upload className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导出</span>
                </Toolbar.Button>
                <Toolbar.Button onClick={() => fileInputRef.current?.click()} icon={<Download className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导入</span>
                </Toolbar.Button>
              </Toolbar>
              <input
                ref={fileInputRef}
                type="file"
                accept=".json,application/json"
                className="hidden"
                onChange={e => {
                  importAuthFile(e.target.files?.[0]);
                  e.target.value = '';
                }}
              />
              <Button size="sm" variant="primary" onClick={startOAuth} disabled={oauthBusy}>
                <Plus className="h-3.5 w-3.5" /> 添加账号
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <Table layout="fixed" className="w-full text-xs">
              <Table.Header variant="compact">
                <Table.Row className="h-8">
                  <Table.Head className="!w-12 !px-2 !py-1.5 text-center">启用</Table.Head>
                  <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                  <Table.Head className="!w-24 !px-2 !py-1.5 text-center">套餐</Table.Head>
                  <Table.Head className="!w-20 !px-2 !py-1.5 text-center">状态</Table.Head>
                  <Table.Head className="!w-24 !px-2 !py-1.5 text-center">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {accounts.map(a => (
                  <Table.Row key={a.email} className="h-10">
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <div className="flex justify-center">
                        <Switch size="sm" checked={!a.disabled} onCheckedChange={v => toggleAccount(a.email, !v)} aria-label={`${a.disabled ? '启用' : '停用'} ${a.email}`} />
                      </div>
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium text-kumo-strong">
                          {a.name ? `${a.name}（${a.email}）` : a.email}
                        </div>
                        <div className="truncate font-mono text-[0.8em] text-kumo-subtle" title={a.projectId}>
                          {a.projectId}
                        </div>
                      </div>
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <span className="truncate text-sm text-kumo-subtle" title={a.planType}>{a.planType || '-'}</span>
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <Badge variant={a.disabled ? 'neutral' : 'success'} className="!text-[0.8em]">
                        {a.disabled ? '停用' : '可用'}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <div className="flex items-center justify-center gap-1">
                        <Button size="sm" shape="square" variant="outline" aria-label={`编辑 ${a.email}`} onClick={() => openEditAccount(a)}>
                          <Edit className="h-3 w-3" />
                        </Button>
                        <Button
                          size="sm"
                          shape="square"
                          variant={isArmed(`antigravity-account-delete:${a.email}`) ? 'destructive' : 'secondary-destructive'}
                          aria-label={isArmed(`antigravity-account-delete:${a.email}`) ? `再次确认删除 ${a.email}` : `删除 ${a.email}`}
                          title={isArmed(`antigravity-account-delete:${a.email}`) ? '再次点击确认删除' : `删除 ${a.email}`}
                          onClick={() => deleteAccount(a.email)}
                        >
                          <Trash className="h-3 w-3" />
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          ) : (
            <div className="p-4">
              <EmptyState title="暂无账号" description="添加 Google 账号后即可使用 Claude 订阅能力。" />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="配额"
          icon={<RefreshCw className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <Button size="sm" variant="outline" disabled={quotaLoading || !authorized} onClick={loadQuota}>
              <RefreshCw className={`h-3 w-3 ${quotaLoading ? 'animate-spin' : ''}`} /> 刷新
            </Button>
          }
        >
          {Array.isArray(quota) && quota.length ? (
            <div className={`grid gap-3 p-3 ${quota.length > 1 ? 'grid-cols-2' : 'grid-cols-1'}`}>
              {quota.map((item, ai) => {
                const q = item.quota;
                const hasData = q && ((q.credits?.length) || (q.groups?.length));
                return (
                  <div key={ai} className="rounded-lg border border-kumo-line bg-kumo-base px-3 py-2.5">
                    <div className="mb-2 flex items-center justify-between gap-2">
                      <Badge variant="neutral" className="!text-[0.8em]">{item.email || '默认账号'}</Badge>
                      {item.error && <span className="shrink-0 text-xs text-kumo-subtle">{item.error}</span>}
                    </div>
                    {hasData ? (
                      <div className="space-y-2">
                        {q.credits?.filter(c => c.creditType && c.creditAmount).map((c, i) => (
                          <div key={`credit-${i}`} className="flex items-center justify-between rounded border border-kumo-line px-3 py-2 text-sm">
                            <span className="text-kumo-subtle">{zhLabel(c.creditType)}</span>
                            <span className="font-medium text-kumo-strong">{Number(c.creditAmount).toLocaleString()}</span>
                          </div>
                        ))}
                        {q.groups?.map((g, gi) => (
                          <div key={`group-${gi}`}>
                            <div className="mb-1.5 text-sm font-medium text-kumo-strong" title={g.description || undefined}>
                              {zhLabel(g.displayName) || `分组 ${gi + 1}`}
                            </div>
                            <div className="space-y-2">
                              {g.buckets?.map((b, bi) => {
                                const pct = b.remainingFraction == null ? null : Math.max(0, Math.min(100, Math.round(b.remainingFraction * 100)));
                                const barColor = pct == null ? 'bg-kumo-line' : pct >= 50 ? 'bg-kumo-success' : pct >= 20 ? 'bg-kumo-warning' : 'bg-kumo-danger';
                                return (
                                  <div key={`bucket-${gi}-${bi}`}>
                                    <div className="flex items-center justify-between text-sm">
                                      <span className="truncate text-kumo-subtle" title={b.description || undefined}>{zhLabel(b.displayName) || zhLabel(b.window) || '额度'}</span>
                                      <span className="ml-2 shrink-0 font-medium text-kumo-strong">{pct == null ? '—' : `${pct}%`}</span>
                                    </div>
                                    <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-kumo-line">
                                      <div
                                        className={`h-full rounded-full ${barColor}`}
                                        style={{ width: `${Math.max(0, Math.min(100, pct ?? 0))}%` }}
                                      />
                                    </div>
                                    {b.resetTime && (
                                      <div className="mt-0.5 text-xs text-kumo-subtle">
                                        重置：{new Date(b.resetTime).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })}
                                      </div>
                                    )}
                                  </div>
                                );
                              })}
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="text-sm text-kumo-subtle">{item.error || '暂无配额数据'}</div>
                    )}
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="p-4">
              <EmptyState
                title={authorized ? '暂无配额' : '未授权'}
                description={authorized ? '点击刷新从上游拉取配额信息。' : '添加并授权账号后显示配额。'}
                action={authorized ? (
                  <Button size="sm" variant="secondary" disabled={quotaLoading} onClick={loadQuota}>
                    <RefreshCw className={`h-3 w-3 ${quotaLoading ? 'animate-spin' : ''}`} /> 刷新配额
                  </Button>
                ) : undefined}
              />
            </div>
          )}
        </SectionCard>
      </div>

      <Dialog.Root open={oauthOpen} onOpenChange={setOauthOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(46rem,calc(100vw-2rem))] !max-w-[min(46rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">Google 账号授权</Dialog.Title>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <Input size="sm" label="名称（可选）" type="text" value={oauthName} onChange={e => setOauthName(e.target.value)} className="w-full" />
              <Input size="sm" type="text" readOnly className="w-full font-mono text-[0.8em]" value={oauthAuthUrl} />
              <div className="flex items-center gap-2">
                <Button size="sm" variant="secondary" onClick={() => window.open(oauthAuthUrl, '_blank')} disabled={!oauthAuthUrl}>
                  打开授权链接
                </Button>
                <Button size="sm" variant="outline" onClick={() => navigator.clipboard?.writeText(oauthAuthUrl)}>复制链接</Button>
              </div>
              <Input
                size="sm"
                label="回调地址"
                type="text"
                value={oauthCallback}
                onChange={e => setOauthCallback(e.target.value)}
                placeholder="http://localhost:8085/callback?state=...&code=..."
                className="w-full font-mono text-[0.8em]"
              />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" disabled={oauthBusy || !oauthCallback.trim()} onClick={finishOAuth}>
              {oauthBusy ? '授权中...' : '完成授权'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!editAccount} onOpenChange={open => !open && setEditAccount(null)}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">编辑账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              {editAccount ? `更新 ${editAccount.email} 的显示信息。` : ''}
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <Input size="sm" label="名称（可选）" type="text" value={editForm.name} onChange={e => setEditForm(f => ({ ...f, name: e.target.value }))} className="w-full" placeholder="显示名" />
              <Input size="sm" label="套餐（可选）" type="text" value={editForm.planType} onChange={e => setEditForm(f => ({ ...f, planType: e.target.value }))} className="w-full" placeholder="如 pro / ultra" />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" onClick={saveEditAccount}>保存</Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
