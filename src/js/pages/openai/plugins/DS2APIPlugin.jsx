import { useEffect, useRef, useState } from 'react';
import { Button, Switch, Loader, Dialog, LayerCard, Input, Badge, Table, Textarea, Toolbar, Select } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, Settings as SettingsIcon, Plus, Upload, Download, RefreshCw, Trash, Edit } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders } from '../utils.js';

const API = '/api/ds2api';

const fmtUntil = (v) => {
  if (!v || v <= 0) return '';
  const left = Math.max(0, v - Math.floor(Date.now() / 1000));
  if (left <= 0) return '';
  const h = Math.floor(left / 3600);
  const m = Math.floor((left % 3600) / 60);
  return h > 0 ? `${h}h${m}m` : `${Math.max(1, m)}m`;
};

const cooldownLabel = (a) => {
  const parts = [];
  if (a.mutedUntil > 0) parts.push(`上游禁言 ${fmtUntil(a.mutedUntil)}`);
  if (a.cooldownUntil > 0) parts.push(`风控 ${fmtUntil(a.cooldownUntil)}`);
  if (a.nodeCooldownUntil > 0) parts.push(`换号 ${fmtUntil(a.nodeCooldownUntil)}`);
  return parts.length ? `（${parts.join(' · ')}）` : '';
};

// DS2APIPlugin：模型网关「插件中心」卡片——DeepSeek 网页版免费池。
// 内嵌 DeepSeek 网页版转发引擎（账号池 + PoW + TLS 指纹），对外提供 OpenAI 兼容端点。
export function DS2APIPlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [linkState, setLinkState] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [addForm, setAddForm] = useState({ name: '', email: '', mobile: '', password: '' });
  const [configOpen, setConfigOpen] = useState(false);
  const [configText, setConfigText] = useState('');
  const [testingId, setTestingId] = useState('');
  const [prefixDraft, setPrefixDraft] = useState(null);
  const [linkBusy, setLinkBusy] = useState(false);
  const [editAccount, setEditAccount] = useState(null);
  const [editForm, setEditForm] = useState({ name: '', email: '', mobile: '', password: '', poolType: '' });
  const fileInputRef = useRef(null);

  const load = async () => {
    try {
      const res = await fetch(`${API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
      setConfigText(data.settings?.configJson || '');
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
      /* 引擎未启用时静默 */
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
  }, [settings?.enabled]);

  useEffect(() => {
    if (status?.engineUp) {
      loadAccounts();
      loadLink();
    }
  }, [status?.engineUp]);

  const save = async (next, msg = '设置已保存') => {
    if (!next) return;
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
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    } finally {
      setSaving(false);
    }
  };

  const update = (patch, silent = false) => {
    const next = { ...(settings || {}), ...patch };
    setSettings(next);
    if (!silent) void save(next);
  };

  const addAccount = async () => {
    try {
      const res = await fetch(`${API}/accounts`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(addForm),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '添加失败');
      toast.success('账号已添加');
      setAddOpen(false);
      setAddForm({ name: '', email: '', mobile: '', password: '' });
      await loadAccounts();
    } catch (e) {
      toast.error(`添加失败：${e.message}`);
    }
  };

  const testAccount = async identifier => {
    setTestingId(identifier);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(identifier)}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!data?.success) throw new Error(data?.error || '测试失败');
      toast.success(`登录成功${data.tokenSet ? '（token 已更新）' : ''}`);
    } catch (e) {
      toast.error(`登录失败：${e.message}`);
    } finally {
      setTestingId('');
    }
  };

  const deleteAccount = async identifier => {
    if (!confirmPress(`ds2api-account-delete:${identifier}`, `删除账号 ${identifier}`)) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(identifier)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '删除失败');
      toast.success('账号已删除');
      await loadAccounts();
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  const openEditAccount = account => {
    setEditForm({
      name: account.name || '',
      email: account.email || '',
      mobile: account.mobile || '',
      password: '',
      poolType: account.poolType || '',
    });
    setEditAccount(account);
  };

  const saveEditAccount = async () => {
    if (!editAccount) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(editAccount.identifier)}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          name: editForm.name.trim(),
          email: editForm.email.trim(),
          mobile: editForm.mobile.trim(),
          password: editForm.password,
          poolType: editForm.poolType.trim(),
        }),
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

  const exportApiFile = () => window.open(`${API}/accounts/export`, '_blank');
  const exportAuthFile = () => exportApiFile();

  // 模型前缀：输入过程用本地草稿，失焦才提交（避免每次击键 PUT）。
  const commitModelPrefix = () => {
    const v = String(prefixDraft ?? '').trim();
    setPrefixDraft(null);
    if (v !== (settings?.modelPrefix || '')) update({ modelPrefix: v }, true);
  };

  const saveConfig = () => {
    try {
      JSON.parse(configText || '{}');
    } catch (e) {
      toast.error(`配置 JSON 格式错误：${e.message}`);
      return;
    }
    update({ configJson: configText }, true);
    setConfigOpen(false);
    toast.success('引擎配置已保存');
  };

  // 自动删除会话：读取/回写 configJson 里的 auto_delete.mode（none/single/all）。
  const parseConfigJson = () => {
    try {
      return JSON.parse(settings?.configJson || '{}');
    } catch {
      return {};
    }
  };

  const getAutoDeleteMode = () => {
    const mode = parseConfigJson()?.auto_delete?.mode;
    return mode === 'single' || mode === 'all' ? mode : 'none';
  };

  const setAutoDeleteMode = mode => {
    const parsed = parseConfigJson();
    const next = JSON.stringify(
      { ...parsed, auto_delete: { ...(parsed.auto_delete || {}), mode } },
      null,
      2,
    );
    setConfigText(next);
    update({ configJson: next }, true);
    toast.success('自动删除会话设置已保存');
  };

  // 专家长提示分段：读取/回写 configJson 里的 expert_prompt_segment.enabled。
  // 受限内存主机（如 256MB 容器）开启分段会产生多次并发上游流的瞬时内存尖峰（实测单请求 +50~70MB），
  // 因此默认关闭；需要把超大专家提示词切段发送时再开启。
  const getExpertPromptSegmentEnabled = () => {
    const val = parseConfigJson()?.expert_prompt_segment?.enabled;
    return val === true;
  };

  const setExpertPromptSegmentEnabled = value => {
    const parsed = parseConfigJson();
    const next = JSON.stringify(
      {
        ...parsed,
        expert_prompt_segment: { ...(parsed.expert_prompt_segment || {}), enabled: !!value },
      },
      null,
      2,
    );
    setConfigText(next);
    update({ configJson: next }, true);
    toast.success(value ? '专家长提示分段已开启' : '专家长提示分段已关闭');
  };

  const importFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(String(reader.result || '{}'));
        const isAccountsOnly = Array.isArray(parsed) || (parsed && Array.isArray(parsed.accounts) && Object.keys(parsed).length <= 1);
        const payload = Array.isArray(parsed) ? { accounts: parsed } : parsed;
        const res = await fetch(`${API}/accounts/import`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify(payload),
        });
        const data = await res.json();
        if (!res.ok || !data?.success) throw new Error(data?.error || '导入失败');
        const n = isAccountsOnly ? (data.added ?? data.imported_accounts ?? 0) : (data.imported_accounts ?? 0);
        toast.success(`已导入 ${n} 个账号${data.imported_keys ? `、${data.imported_keys} 个密钥` : ''}`);
        await loadAccounts();
      } catch (e) {
        toast.error(`导入失败：${e.message}`);
      }
    };
    reader.readAsText(file);
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

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  const engineUp = !!status?.engineUp;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 cq-sm:gap-4">
      <div className="flex items-center justify-end">
        <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <SectionCard title="DS2API" icon={<Rocket className="h-4 w-4 text-brand" />} bodyPadding="none">
            <FieldRow title={<span title="关闭后 /v1/* 与网关端点接入都会拒绝服务">启用中继</span>}>
              <Switch checked={!!settings?.enabled} onCheckedChange={v => update({ enabled: v })} />
            </FieldRow>
            <FieldRow title={<span title="把本插件注册为模型网关端点，外部客户端经网关 /v1/chat/completions 路由到本中继">接入模型网关</span>}>
              <div className="flex items-center gap-2">
                <Switch
                  checked={!!linkState?.linked}
                  disabled={linkBusy || !settings?.enabled}
                  onCheckedChange={checked => linkPlugin(checked ? 'link' : 'unlink')}
                />
              </div>
            </FieldRow>
            <FieldRow title={<span title="账号池与模型别名等引擎设置（账号在下方的账号池管理）；插件为内部 API，无需配置访问密钥">引擎配置</span>}>
              <div className="flex min-w-0 items-center gap-2">
                <Button size="sm" variant="secondary" onClick={() => setConfigOpen(true)}>
                  编辑配置
                </Button>
              </div>
            </FieldRow>
            <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀（如 ds2-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
              <div className="flex min-w-0 items-center gap-2">
                <Input
                  size="sm"
                  className="w-40"
                  placeholder="ds2-"
                  value={prefixDraft ?? settings?.modelPrefix ?? ''}
                  onChange={e => setPrefixDraft(e.target.value)}
                  onBlur={commitModelPrefix}
                  disabled={saving}
                />
              </div>
            </FieldRow>
            <FieldRow title={<span title="每次会话结束后自动删除远端会话：none 不删除；single 仅删当前会话；all 删除该密钥全部会话。写回引擎配置 configJson 的 auto_delete.mode">自动删除会话</span>}>
              <Select
                size="sm"
                className="w-44"
                value={getAutoDeleteMode()}
                onValueChange={setAutoDeleteMode}
                items={[
                  { value: 'none', label: '不删除' },
                  { value: 'single', label: '仅删除当前会话' },
                  { value: 'all', label: '删除全部会话' },
                ]}
                disabled={saving}
              />
            </FieldRow>
            <FieldRow title={<span title="超大专家提示词（超过 expert_prompt_segment.max_chars，默认 160000 字符）是否切成多段逐段发送。受限内存主机开启后每次分段请求会产生多次并发上游流，瞬时内存 +50~70MB，易被 OOM 杀死，故默认关闭。写回引擎配置 configJson 的 expert_prompt_segment.enabled">专家长提示分段</span>}>
              <div className="flex items-center gap-2">
                <Switch
                  checked={getExpertPromptSegmentEnabled()}
                  onCheckedChange={setExpertPromptSegmentEnabled}
                  disabled={saving}
                />
              </div>
            </FieldRow>
          </SectionCard>

          <SectionCard
            title="账号池"
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
                    importFile(e.target.files?.[0]);
                    e.target.value = '';
                  }}
                />
                <Button size="sm" variant="primary" onClick={() => setAddOpen(true)}>
                  <Plus className="h-3.5 w-3.5" /> 添加账号
                </Button>
              </div>
            }
          >
            {engineUp && accounts.length ? (
              <Table layout="fixed" className="w-full text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                    <Table.Head className="!w-24 !px-2 !py-1.5 text-center">状态</Table.Head>
                    <Table.Head className="!w-44 !px-2 !py-1.5 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accounts.map(a => (
                    <Table.Row key={a.identifier} className="h-10">
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium text-kumo-strong" title={a.identifier}>
                            {a.name ? `${a.name}（${a.identifier}）` : a.identifier}
                          </div>
                          <div className="truncate font-mono text-[0.8em] text-kumo-subtle">
                            {[a.email, a.mobile].filter(Boolean).join(' · ') || a.identifier}
                          </div>
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {a.banned ? (
                          <Badge variant="danger" className="!text-[0.8em]">封禁</Badge>
                        ) : a.disabled ? (
                          <Badge variant="danger" className="!text-[0.8em]">禁用</Badge>
                        ) : !a.available ? (
                          <Badge variant="warning" className="!text-[0.8em]">
                            冷却{cooldownLabel(a)}
                          </Badge>
                        ) : (
                          <Badge variant="success" className="!text-[0.8em]">可用</Badge>
                        )}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex items-center justify-center gap-1">
                          <Button size="sm" variant="secondary" disabled={testingId === a.identifier} onClick={() => testAccount(a.identifier)}>
                            <RefreshCw className={`h-3 w-3 ${testingId === a.identifier ? 'animate-spin' : ''}`} /> 登录测试
                          </Button>
                          <Button size="sm" shape="square" variant="outline" aria-label={`编辑 ${a.identifier}`} onClick={() => openEditAccount(a)}>
                            <Edit className="h-3 w-3" />
                          </Button>
                          <Button
                            size="sm"
                            shape="square"
                            variant={isArmed(`ds2api-account-delete:${a.identifier}`) ? 'destructive' : 'secondary-destructive'}
                            aria-label={isArmed(`ds2api-account-delete:${a.identifier}`) ? `再次确认删除 ${a.identifier}` : `删除 ${a.identifier}`}
                            title={isArmed(`ds2api-account-delete:${a.identifier}`) ? '再次点击确认删除' : `删除 ${a.identifier}`}
                            onClick={() => deleteAccount(a.identifier)}
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
                <EmptyState
                  title={engineUp ? '暂无账号' : '引擎未启动'}
                  description={engineUp ? '添加 DeepSeek 网页版账号（邮箱/手机号 + 密码）。' : '启用插件后引擎自动加载，即可管理账号池。'}
                />
              </div>
            )}
          </SectionCard>

          <SectionCard title="账号池健康" icon={<Rocket className="h-4 w-4 text-brand" />} bodyPadding="none">
            {accounts.length ? (
              <div className="space-y-2 p-3">
                <div className="grid grid-cols-4 gap-2 text-center">
                  <div className="rounded border border-kumo-line px-2 py-1.5">
                    <div className="text-base font-semibold text-kumo-strong">{accounts.filter(a => a.available).length}</div>
                    <div className="text-[10px] text-kumo-subtle">可用</div>
                  </div>
                  <div className="rounded border border-kumo-line px-2 py-1.5">
                    <div className="text-base font-semibold text-kumo-strong">{accounts.filter(a => a.banned).length}</div>
                    <div className="text-[10px] text-kumo-subtle">封禁</div>
                  </div>
                  <div className="rounded border border-kumo-line px-2 py-1.5">
                    <div className="text-base font-semibold text-kumo-strong">{accounts.filter(a => !a.available && !a.banned && !a.disabled).length}</div>
                    <div className="text-[10px] text-kumo-subtle">冷却中</div>
                  </div>
                  <div className="rounded border border-kumo-line px-2 py-1.5">
                    <div className="text-base font-semibold text-kumo-strong">{accounts.filter(a => a.disabled && !a.banned).length}</div>
                    <div className="text-[10px] text-kumo-subtle">禁用</div>
                  </div>
                </div>
                {accounts.some(a => !a.available && !a.banned && !a.disabled) && (
                  <div className="pt-1 text-[11px] leading-relaxed text-kumo-subtle">
                    {accounts
                      .filter(a => !a.available && !a.banned && !a.disabled)
                      .map(a => `${a.name || a.identifier}:${cooldownLabel(a)}`)
                      .join('；')}
                  </div>
                )}
              </div>
            ) : (
              <div className="p-4">
                <EmptyState
                  title={engineUp ? '暂无账号' : '引擎未启动'}
                  description={engineUp ? '添加 DeepSeek 网页版账号后展示账号池健康状态。' : '启用插件后引擎自动加载，即可管理账号池。'}
                />
              </div>
            )}
          </SectionCard>
      </div>

      <Dialog.Root open={addOpen} onOpenChange={setAddOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">添加 DeepSeek 账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">邮箱或手机号 + 密码，用于 DeepSeek 网页版登录。</Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <Input size="sm" label="名称（可选）" type="text" value={addForm.name} onChange={e => setAddForm(f => ({ ...f, name: e.target.value }))} className="w-full" />
              <Input size="sm" label="邮箱" type="email" value={addForm.email} onChange={e => setAddForm(f => ({ ...f, email: e.target.value }))} className="w-full" placeholder="user@example.com" />
              <Input size="sm" label="手机号" type="text" value={addForm.mobile} onChange={e => setAddForm(f => ({ ...f, mobile: e.target.value }))} className="w-full" placeholder="13800138000" />
              <Input size="sm" label="密码" type="password" value={addForm.password} onChange={e => setAddForm(f => ({ ...f, password: e.target.value }))} className="w-full" />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" onClick={addAccount}>添加</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={configOpen} onOpenChange={setConfigOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),46rem)] !w-[min(52rem,calc(100vw-2rem))] !max-w-[min(52rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">引擎配置（config.json）</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              账号池与模型别名等引擎设置，格式与 DS2API 一致；插件仅作内部 API 调用，无需配置访问密钥。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <Textarea
              rows={20}
              className="w-full font-mono text-xs"
              value={configText}
              onChange={e => setConfigText(e.target.value)}
              spellCheck={false}
            />
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" onClick={saveConfig}>保存配置</Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!editAccount} onOpenChange={open => !open && setEditAccount(null)}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">编辑账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              {editAccount ? `更新 ${editAccount.identifier} 的登录信息。` : ''}
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <Input size="sm" label="名称（可选）" type="text" value={editForm.name} onChange={e => setEditForm(f => ({ ...f, name: e.target.value }))} className="w-full" />
              <Input size="sm" label="邮箱" type="email" value={editForm.email} onChange={e => setEditForm(f => ({ ...f, email: e.target.value }))} className="w-full" placeholder="user@example.com" />
              <Input size="sm" label="手机号" type="text" value={editForm.mobile} onChange={e => setEditForm(f => ({ ...f, mobile: e.target.value }))} className="w-full" placeholder="13800138000" />
              <Input size="sm" label="密码（留空不改）" type="password" value={editForm.password} onChange={e => setEditForm(f => ({ ...f, password: e.target.value }))} className="w-full" />
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
