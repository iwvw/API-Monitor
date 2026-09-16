import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Switch, Loader, Dialog, LayerCard, Input, Badge, Table, Meter, Select, Toolbar } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, PostHogBrand, Settings as SettingsIcon, Plus, RefreshCw, Trash, ExternalLink, ShieldCheck, TrendingUp, Upload, Download } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders } from '../utils.js';

const API = '/api/posthogcode';

// 回调地址由后端固定为 PostHog OAuth 应用注册过的值（默认 http://localhost/callback）。
// 不能按面板所在源推导：注册值与部署域名无关，用 window.location.origin 会得到
// "Mismatching redirect URI"。这里只用于提示占位，实际值取后端下发的 redirectUri。
const DEFAULT_CALLBACK_URI = 'http://localhost/callback';

const fmtLeft = seconds => {
  const s = Number(seconds) || 0;
  if (s <= 0) return '';
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  if (d > 0) return `${d}天${h}小时`;
  const m = Math.floor((s % 3600) / 60);
  return h > 0 ? `${h}小时${m}分` : `${Math.max(1, m)}分`;
};

// PostHogCodePlugin：模型网关「插件中心」卡片——PostHog LLM Gateway 转 OpenAI 兼容 API。
// 鉴权走 PostHog OAuth 2.0 PKCE（llm_gateway:read 是 privileged scope，个人 API Key 拿不到），
// 插件内嵌授权码流程与 refresh token 自动轮换。
export function PostHogCodePlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [linkState, setLinkState] = useState(null);
  const [models, setModels] = useState([]);
  const [usage, setUsage] = useState(null);
  const [usageRows, setUsageRows] = useState([]);
  const [usageTotal, setUsageTotal] = useState(null);
  const [usdPerCredit, setUsdPerCredit] = useState(0.01);
  const [usageLoading, setUsageLoading] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [linkBusy, setLinkBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [testingId, setTestingId] = useState('');
  const [prefixDraft, setPrefixDraft] = useState(null);
  const [authOpen, setAuthOpen] = useState(false);
  const [authUrl, setAuthUrl] = useState('');
  const [authState, setAuthState] = useState('');
  // authRedirectUri 是后端下发的权威回调地址（OAuth 应用注册值），
  // 仅用于占位提示，交换 token 时由后端从会话取用，前端不参与。
  const [authRedirectUri, setAuthRedirectUri] = useState(DEFAULT_CALLBACK_URI);
  const [authRegion, setAuthRegion] = useState('us');
  const [authStarting, setAuthStarting] = useState(false);
  const [callbackUrl, setCallbackUrl] = useState('');
  const [exchanging, setExchanging] = useState(false);
  const fileInputRef = useRef(null);

  const load = async () => {
    try {
      const res = await fetch(`${API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
      setAccounts(data.settings?.accounts || []);
    } catch (e) {
      toast.error(`插件设置加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  const loadStatus = async () => {
    try {
      const res = await fetch(`${API}/status`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setStatus(data);
    } catch {
      setStatus(null);
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

  const loadModels = async () => {
    try {
      const res = await fetch(`${API}/models`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setModels(data.models || []);
    } catch {
      /* 无可用账号时静默 */
    }
  };

  const loadUsage = async () => {
    setUsageLoading(true);
    try {
      const res = await fetch(`${API}/usage`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) {
        setUsageRows(data.accounts || []);
        setUsageTotal(data.total || null);
        setUsdPerCredit(data.usdPerCredit ?? 0.01);
        // 单账号时后端保留顶层字段，沿用最简展示。
        setUsage(data.usage || null);
      } else if (data?.error) {
        toast.error(`用量查询失败：${data.error}`);
      }
    } catch (e) {
      toast.error(`用量查询失败：${e.message}`);
    } finally {
      setUsageLoading(false);
    }
  };

  const refreshAll = async () => {
    setRefreshing(true);
    try {
      await load();
      await loadStatus();
      await loadModels();
      await loadUsage();
    } finally {
      setRefreshing(false);
    }
  };

  const exportAccounts = () => window.open(`${API}/accounts/export`, '_blank');

  const importFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(String(reader.result || '{}'));
        const payload = Array.isArray(parsed) ? { accounts: parsed } : parsed;
        const res = await fetch(`${API}/accounts/import`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify(payload),
        });
        const data = await res.json();
        if (!res.ok || !data?.success) throw new Error(data?.error || '导入失败');
        toast.success(`已导入 ${data.added ?? 0} 个账号`);
        await Promise.all([load(), loadStatus()]);
      } catch (e) {
        toast.error(`导入失败：${e.message}`);
      }
    };
    reader.readAsText(file);
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    loadStatus();
    loadLink();
  }, [settings?.enabled]);

  useEffect(() => {
    if ((status?.availableCount ?? 0) > 0) {
      loadModels();
      loadUsage();
    }
  }, [status?.availableCount]);

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
      setAccounts(data.settings?.accounts || []);
      toast.success(msg);
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    } finally {
      setSaving(false);
    }
  };

  const update = patch => {
    const next = { ...(settings || {}), ...patch };
    setSettings(next);
    void save(next);
  };

  const commitModelPrefix = () => {
    const v = String(prefixDraft ?? '').trim();
    setPrefixDraft(null);
    if (v !== (settings?.modelPrefix || '')) update({ modelPrefix: v });
  };

  // 发起授权：向后端要授权链接与 state，然后打开新窗口。
  const startOAuth = async () => {
    setAuthStarting(true);
    setCallbackUrl('');
    try {
      const res = await fetch(`${API}/oauth/auth-url`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ region: authRegion }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '生成授权链接失败');
      setAuthUrl(data.url);
      setAuthState(data.state);
      setAuthRedirectUri(data.redirectUri || DEFAULT_CALLBACK_URI);
      setAuthOpen(true);
      window.open(data.url, '_blank', 'noopener');
    } catch (e) {
      toast.error(e.message);
    } finally {
      setAuthStarting(false);
    }
  };

  // 完成授权：把跳转后的完整回调地址粘回来。
  const finishOAuth = async () => {
    if (!callbackUrl.trim()) {
      toast.error('请粘贴回调地址');
      return;
    }
    setExchanging(true);
    try {
      const res = await fetch(`${API}/oauth/exchange`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ callbackUrl: callbackUrl.trim(), state: authState }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '授权失败');
      toast.success(`已授权：${data?.account?.email || data?.account?.id || ''}`);
      setAuthOpen(false);
      setCallbackUrl('');
      await load();
      await loadStatus();
      await loadModels();
      await loadUsage();
    } catch (e) {
      toast.error(`授权失败：${e.message}`);
    } finally {
      setExchanging(false);
    }
  };

  const testAccount = async id => {
    setTestingId(id);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(id)}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!data?.success) throw new Error(data?.error || '测试失败');
      toast.success('token 刷新成功');
    } catch (e) {
      toast.error(`刷新失败：${e.message}`);
    } finally {
      setTestingId('');
      await load();
    }
  };

  const deleteAccount = async id => {
    if (!confirmPress(`posthogcode-account-delete:${id}`, `删除账号 ${id}`)) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '删除失败');
      toast.success('账号已删除');
      await load();
      await loadStatus();
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  const toggleAccount = async (id, disabled) => {
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(id)}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ disabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      toast.success(disabled ? '账号已停用' : '账号已启用');
      await load();
      await loadStatus();
    } catch (e) {
      toast.error(e.message);
    }
  };

  const toggleModel = async (id, enabled) => {
    try {
      const res = await fetch(`${API}/models/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ id, enabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      await loadModels();
    } catch (e) {
      toast.error(e.message);
    }
  };

  const toggleAllModels = async enabled => {
    try {
      const res = await fetch(`${API}/models/toggle-batch`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled, ids: models.map(m => m.id) }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      await loadModels();
    } catch (e) {
      toast.error(e.message);
    }
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

  const enabledModelCount = useMemo(() => models.filter(m => m.enabled).length, [models]);
  const allEnabled = models.length > 0 && enabledModelCount === models.length;

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 cq-sm:gap-4">
      <div className="flex items-center justify-between gap-2">
        {linkState?.linked && linkState?.baseUrl ? (
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
            <Rocket className="h-3.5 w-3.5 text-brand" />
            <span className="text-kumo-strong">已接入网关端点</span>
            <span className="truncate font-mono text-kumo-subtle" title="本插件在网关端点列表中的 base_url">
              {linkState.baseUrl}
            </span>
            <span className="text-kumo-subtle">· {linkState.models?.length || 0} 个模型</span>
          </div>
        ) : (
          <span />
        )}
        <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <SectionCard title="PostHog Code" icon={<PostHogBrand className="h-4 w-4 text-brand" />} bodyPadding="none">
          <FieldRow title={<span title="总开关：同时控制中继与模型网关接入。打开后启用中继并注册为网关端点；关闭后中继拒服并移除端点">启用中继</span>}>
            <Switch
              checked={!!settings?.enabled}
              disabled={linkBusy}
              onCheckedChange={v => {
                update({ enabled: v });
                if (v) linkPlugin('link');
                else linkPlugin('unlink');
              }}
            />
          </FieldRow>
          <FieldRow title={<span title="PostHog Cloud 区域：us（gateway.us.posthog.com）或 eu（gateway.eu.posthog.com）。切换后需重新授权">区域</span>}>
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-36"
              value={settings?.region || 'us'}
              onValueChange={v => update({ region: v })}
              items={[
                { value: 'us', label: 'us · 美东' },
                { value: 'eu', label: 'eu · 欧区' },
              ]}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="PostHog 免费层模型（无需绑定付款方式）：GLM-5.2、DeepSeek V4 Flash、Kimi K3。开启后只暴露这些模型，高级模型（Claude Fable/Opus、GPT-6 等）需要 PostHog 付费计划">仅免费层模型</span>}>
            <Switch checked={!!settings?.freeTierOnly} onCheckedChange={v => update({ freeTierOnly: v })} />
          </FieldRow>
          <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀（如 phc-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
            <Input
              size="sm"
              className="w-40"
              placeholder="phc-"
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="多账号时的选号策略。固定首个：始终用列表第一个可用账号，其余作主备，行为最可预期。轮询：依次轮流，请求均匀分摊。按剩余额度：优先用剩余额度最多的账号，避免某个账号先撞上限。后两种都自动跳过已停用、token 过期与失败冷却中的账号">选号策略</span>}>
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-44"
              value={settings?.accountStrategy || 'first'}
              onValueChange={v => update({ accountStrategy: v })}
              items={[
                { value: 'first', label: '固定首个可用' },
                { value: 'round-robin', label: '轮询' },
                { value: 'least-used', label: '按剩余额度' },
              ]}
              disabled={saving || (accounts.length < 2)}
            />
          </FieldRow>
          <FieldRow title={<span title="探测上游可达性：用当前账号拉一次模型列表">连通性测试</span>}>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                disabled={!status?.availableCount}
                onClick={async () => {
                  try {
                    const res = await fetch(`${API}/test`, { method: 'POST', headers: getAuthHeaders(), body: '{}' });
                    const data = await res.json();
                    if (!data?.success) throw new Error(data?.error || '测试失败');
                    toast.success(`上游可达，共 ${data.modelCount} 个模型`);
                    await loadModels();
                  } catch (e) {
                    toast.error(`测试失败：${e.message}`);
                  }
                }}
              >
                测试
              </Button>
              {status ? (
                <span className="text-xs text-kumo-subtle">
                  账号 {status.availableCount}/{status.accountCount} 可用 · 模型 {status.modelCount}
                </span>
              ) : null}
            </div>
          </FieldRow>
        </SectionCard>

        <SectionCard
          title="授权账号"
          icon={<ShieldCheck className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <div className="flex items-center gap-1.5">
              <Toolbar size="sm" aria-label="账号导出导入" className="shrink-0">
                <Toolbar.Button onClick={exportAccounts} icon={<Upload className="h-3.5 w-3.5" />}>
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
              <Button size="sm" variant="outline" disabled={refreshing} onClick={refreshAll} title="重新拉取账号、模型与用量">
                <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
                <span className="hidden cq-sm:inline">刷新</span>
              </Button>
              <Button size="sm" variant="primary" disabled={authStarting} onClick={startOAuth}>
                <Plus className="h-3.5 w-3.5" /> 添加账号
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <LayerCard className="min-w-0 overflow-hidden p-0 shadow-none">
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <Table layout="fixed" className="w-full min-w-[42rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!w-12 !px-2 !py-1.5 text-center">启用</Table.Head>
                    <Table.Head className="!w-56 !px-2.5 !py-1.5">账号</Table.Head>
                    <Table.Head className="!w-20 !px-2 !py-1.5 text-center">调用</Table.Head>
                    <Table.Head className="!w-32 !px-2 !py-1.5 text-center">状态</Table.Head>
                    <Table.Head className="!w-40 !px-2 !py-1.5 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accounts.map(a => (
                    <Table.Row key={a.id} className="h-10">
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex justify-center">
                          <Switch
                            size="sm"
                            checked={!a.disabled}
                            onCheckedChange={v => toggleAccount(a.id, !v)}
                            aria-label={`${a.disabled ? '启用' : '停用'} ${a.email || a.id}`}
                          />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <div className="truncate text-sm font-medium text-kumo-strong" title={a.id}>
                              {a.email || a.id}
                            </div>
                            <span className="shrink-0 rounded bg-kumo-recessed px-1 font-mono text-[0.85em] text-kumo-subtle">
                              {String(a.region || 'us').toUpperCase()}
                            </span>
                          </div>
                          <div className="truncate font-mono text-kumo-subtle" title={a.id}>
                            {a.id !== a.email ? a.id : ''}
                          </div>
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-xs text-kumo-strong">{a.callCount ?? 0}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {a.disabled ? (
                          <Badge variant="danger" className="text-xs">停用</Badge>
                        ) : !a.scopeReady ? (
                          <Badge variant="warning" className="text-xs">需重新授权</Badge>
                        ) : !a.available ? (
                          <Badge variant="warning" className="text-xs">
                            {a.tokenState === 'expired' ? '已过期' : '不可用'}
                          </Badge>
                        ) : (
                          <Badge variant="success" className="text-xs">
                            {a.expiresInSeconds > 0 ? `有效 ${fmtLeft(a.expiresInSeconds)}` : '有效'}
                          </Badge>
                        )}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex items-center justify-center gap-1">
                          <Button size="sm" variant="secondary" disabled={testingId === a.id} onClick={() => testAccount(a.id)} title="强制刷新 token 验证凭据">
                            <RefreshCw className={`h-3 w-3 ${testingId === a.id ? 'animate-spin' : ''}`} /> 刷新
                          </Button>
                          <Button
                            size="sm"
                            shape="square"
                            variant={isArmed(`posthogcode-account-delete:${a.id}`) ? 'destructive' : 'secondary-destructive'}
                            aria-label={isArmed(`posthogcode-account-delete:${a.id}`) ? `再次确认删除 ${a.id}` : `删除 ${a.id}`}
                            title={isArmed(`posthogcode-account-delete:${a.id}`) ? '再次点击确认删除' : `删除 ${a.id}`}
                            onClick={() => deleteAccount(a.id)}
                          >
                            <Trash className="h-3 w-3" />
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                 </Table.Body>
              </Table>
              </div>
            </LayerCard>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无账号"
                description="点击「添加账号」完成 PostHog OAuth 授权（需要 PostHog 账号，scope 为 llm_gateway:read）。"
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="用量"
          icon={<TrendingUp className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <Button size="sm" variant="outline" disabled={!status?.availableCount || usageLoading} onClick={loadUsage}>
              <RefreshCw className={`h-3.5 w-3.5 ${usageLoading ? 'animate-spin' : ''}`} />
              <span className="hidden cq-sm:inline">刷新</span>
            </Button>
          }
        >
          {usageRows.length ? (
            <LayerCard className="min-w-0 overflow-hidden p-0 shadow-none">
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <Table layout="fixed" className="w-full min-w-[46rem] text-xs">
                  <Table.Header sticky variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!w-56 !px-2.5 !py-1.5">账号</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5">剩余额度</Table.Head>
                      <Table.Head className="!w-20 !px-2 !py-1.5 text-right">已用</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center" title="日窗口用量占比，超过会被网关限流">日窗口</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center" title="月窗口用量占比，超过会被网关限流">月窗口</Table.Head>
                      <Table.Head className="!w-28 !px-2 !py-1.5 text-center" title="各账号额度互相独立，周期结束日按各自计费周期">计费周期</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {usageRows.map(row => {
                      const rowLimit = row.limitCredits ?? 0;
                      const rowUsed = row.usedCredits ?? 0;
                      const rowRemain = row.limitCredits != null ? Math.max(0, rowLimit - rowUsed) : null;
                      const pct = rowLimit > 0 ? Math.min(100, (rowRemain / rowLimit) * 100) : null;
                      const tone = pct == null ? '' : pct <= 10 ? 'bg-kumo-danger' : pct <= 30 ? 'bg-kumo-warning' : 'bg-kumo-success';
                      const burst = row.usage ? Number(row.usage.burst?.used_percent ?? 0) : null;
                      const sustained = row.usage ? Number(row.usage.sustained?.used_percent ?? 0) : null;
                      const windowTone = p => (p == null ? '' : p >= 90 ? 'text-kumo-danger' : p >= 70 ? 'text-kumo-warning' : 'text-kumo-subtle');
                      const windowTip = row.usage
                        ? `日窗口 ${Number(row.usage.burst?.used_percent ?? 0).toFixed(1)}%${
                            row.usage.burst?.resets_in_seconds ? `（${fmtLeft(row.usage.burst.resets_in_seconds)}后重置）` : ''
                          } · 月窗口 ${Number(row.usage.sustained?.used_percent ?? 0).toFixed(1)}%${
                            row.usage.sustained?.resets_in_seconds ? `（${fmtLeft(row.usage.sustained.resets_in_seconds)}后重置）` : ''
                          }`
                        : '';
                      return (
                        <Table.Row key={row.id} className="h-12">
                          <Table.Cell className="!px-2.5 !py-1.5">
                            <div className="flex items-center gap-1.5">
                              <div className="truncate text-kumo-strong" title={row.id}>
                                {row.email || row.id}
                              </div>
                              {row.limited ? (
                                <Badge variant="danger" className="shrink-0 text-xs">耗尽</Badge>
                              ) : null}
                            </div>
                            {row.error ? (
                              <div className="truncate text-kumo-warning" title={row.error}>
                                {row.scopeReady ? row.error : '缺少 project:read，需重新授权'}
                              </div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell className="!px-2.5 !py-1.5">
                            {pct != null ? (
                              <Meter
                                label={`$${(rowRemain * usdPerCredit).toFixed(2)} / $${(rowLimit * usdPerCredit).toFixed(2)}`}
                                value={pct}
                                max={100}
                                customValue={`${pct.toFixed(1)}%`}
                                trackClassName="!h-1 bg-kumo-recessed"
                                indicatorClassName={tone}
                              />
                            ) : (
                              <span className="text-kumo-subtle">未取到额度</span>
                            )}
                          </Table.Cell>
                          <Table.Cell className="!px-2 !py-1.5 text-right font-mono text-kumo-subtle">
                            {row.usedCredits != null ? `$${(rowUsed * usdPerCredit).toFixed(2)}` : '—'}
                          </Table.Cell>
                          <Table.Cell className={`!px-2 !py-1.5 text-center font-mono ${windowTone(burst)}`} title={windowTip}>
                            {burst != null ? `${burst.toFixed(1)}%` : '—'}
                          </Table.Cell>
                          <Table.Cell className={`!px-2 !py-1.5 text-center font-mono ${windowTone(sustained)}`} title={windowTip}>
                            {sustained != null ? `${sustained.toFixed(1)}%` : '—'}
                          </Table.Cell>
                          <Table.Cell className="!px-2 !py-1.5 text-center">
                            <span className="font-mono text-kumo-subtle">
                              {row.usage?.billing_period_end
                                ? String(row.usage.billing_period_end).slice(0, 10)
                                : '—'}
                            </span>
                          </Table.Cell>
                        </Table.Row>
                      );
                    })}
                  </Table.Body>
                </Table>
              </div>
            </LayerCard>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无用量数据"
                description={status?.availableCount ? '点击「刷新」拉取上游额度与限额状态。' : '先完成账号授权，再查询用量。'}
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="模型"
          icon={<SettingsIcon className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <span className="text-xs text-kumo-subtle">{enabledModelCount}/{models.length} 已启用</span>
          }
        >
          {models.length ? (
            <LayerCard className="min-w-0 overflow-hidden p-0 shadow-none">
              <div className="min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin">
                <Table layout="fixed" className="w-full min-w-[44rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!w-12 !px-2 !py-1.5 text-center">
                      <div className="flex justify-center">
                        <Switch size="sm" checked={allEnabled} onCheckedChange={toggleAllModels} aria-label="全选模型" />
                      </div>
                    </Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                    <Table.Head className="!w-24 !px-2 !py-1.5 text-center">来源</Table.Head>
                    <Table.Head className="!w-24 !px-2 !py-1.5 text-center">上下文</Table.Head>
                    <Table.Head className="!w-24 !px-2 !py-1.5 text-center">倍率</Table.Head>
                    <Table.Head className="!w-24 !px-2 !py-1.5 text-center">计划</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {models.map(m => (
                    <Table.Row key={m.id} className="h-9">
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex justify-center">
                          <Switch size="sm" checked={!!m.enabled} onCheckedChange={v => toggleModel(m.id, v)} aria-label={`${m.enabled ? '停用' : '启用'} ${m.id}`} />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="truncate font-mono text-kumo-strong" title={m.rawId || m.id}>
                          {m.id}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="text-kumo-subtle">{m.ownedBy || '—'}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-kumo-subtle">
                          {m.contextWindow ? `${Math.round(m.contextWindow / 1000)}K` : '—'}
                        </span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span
                          className="font-mono text-kumo-strong"
                          title={
                            m.price
                              ? `输入 $${m.price.inputPerMtok}/M · 输出 $${m.price.outputPerMtok}/M（基准 Claude Sonnet 5）`
                              : '上游未提供计价'
                          }
                        >
                          {m.price ? `${m.price.approximate ? '≈' : ''}${m.price.multiplier.toFixed(2)}×` : '—'}
                        </span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {m.freeTier ? (
                          <Badge variant="success" className="text-xs">免费</Badge>
                        ) : m.allowed ? (
                          <Badge variant="info" className="text-xs">可用</Badge>
                        ) : (
                          <Badge variant="warning" className="text-xs">受限</Badge>
                        )}
                      </Table.Cell>
                    </Table.Row>
                  ))}
                 </Table.Body>
              </Table>
              </div>
            </LayerCard>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无模型"
                description={status?.availableCount ? '点击「连通性测试」拉取上游模型列表。' : '先完成账号授权，再拉取上游模型列表。'}
              />
            </div>
          )}
        </SectionCard>
      </div>

      <Dialog.Root open={authOpen} onOpenChange={setAuthOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(46rem,calc(100vw-2rem))] !max-w-[min(46rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">PostHog 授权</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              在浏览器完成授权后，把跳转到的完整地址粘贴到下方。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="space-y-4">
              <div className="rounded border border-kumo-line p-3 text-xs leading-relaxed text-kumo-subtle">
                <div className="mb-1 text-kumo-strong">步骤</div>
                <div>1. 点击下方按钮打开授权页（若被拦截可手动复制链接）</div>
                <div>2. 登录并同意授权</div>
                <div>3. 浏览器会跳转到回调地址（可能显示无法访问，属正常）</div>
                <div>4. 复制地址栏完整 URL，粘贴到下面</div>
              </div>
              <div className="flex items-center gap-2">
                <Input size="sm" className="w-full font-mono text-xs" readOnly value={authUrl} aria-label="授权链接" />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void navigator.clipboard?.writeText(authUrl);
                    toast.success('授权链接已复制');
                  }}
                >
                  复制
                </Button>
                <Button size="sm" variant="outline" onClick={() => window.open(authUrl, '_blank', 'noopener')}>
                  <ExternalLink className="h-3.5 w-3.5" /> 打开
                </Button>
              </div>
              <Input
                size="sm"
                label="回调地址"
                className="w-full font-mono text-xs"
                placeholder={`${authRedirectUri || DEFAULT_CALLBACK_URI}?code=...&state=...`}
                value={callbackUrl}
                onChange={e => setCallbackUrl(e.target.value)}
              />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" disabled={exchanging} onClick={finishOAuth}>
              {exchanging ? '验证中...' : '完成授权'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}
