import { useEffect, useRef, useState } from 'react';
import { Button, Switch, Loader, Dialog, LayerCard, Input, Badge, Table, Toolbar, Select, Popover } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState, AppTable } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, Users, Layers, TrendingUp, RefreshCw, Plus, Trash, Edit, Clock, Upload, Download } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders, formatCompact } from '../utils.js';
import { formatDateTime } from '../../../modules/utils.js';

const API = '/api/lobsterai';

const POLL_INTERVAL_MS = 2000;

const fmtLeft = seconds => {
  const left = Number(seconds) || 0;
  if (left <= 0) return '';
  const h = Math.floor(left / 3600);
  const m = Math.floor((left % 3600) / 60);
  return h > 0 ? `${h}h${m}m` : `${Math.max(1, m)}m`;
};

// fmtCredits 积分为小数（如 832.35），按量级保留 2~4 位，避免固定两位都显示成 0.00。
const fmtCredits = value => {
  const n = Number(value);
  if (!Number.isFinite(n)) return '—';
  if (n >= 100) return n.toFixed(1);
  if (n >= 1) return n.toFixed(2);
  return n.toFixed(4);
};

// fmtExpiry 格式化批次到期时刻。后端已把上游不带时区的时间归一化为
// RFC3339（UTC），这里直接交给 formatDateTime 按站点时区展示。
const fmtExpiry = value => {
  if (!value) return '';
  return formatDateTime(value);
};

const tokenStateMeta = state => {
  switch (state) {
    case 'valid':
      return { variant: 'success', label: '正常' };
    case 'expiring':
      return { variant: 'warning', label: '即将过期' };
    case 'expired':
      return { variant: 'danger', label: '已过期' };
    default:
      return { variant: 'neutral', label: '未知' };
  }
};

const fmtTokens = v => formatCompact(Number(v) || 0);
const fmtRate = r => {
  const n = Number(r);
  if (!Number.isFinite(n) || n <= 0) return '—';
  return `${(n * 100).toFixed(1)}%`;
};

const LOBSTER_ACCOUNT_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'account', role: 'primary', grow: 1 },
  { id: 'credits', role: 'meta', align: 'center' },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'token', role: 'status' },
  { id: 'actions', role: 'actions-lg' },
];

const LOBSTER_USAGE_ACCOUNT_COLUMNS = [
  { id: 'account', role: 'primary', grow: 1 },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'prompt', role: 'count', align: 'center' },
  { id: 'completion', role: 'count', align: 'center' },
  { id: 'cached', role: 'count', align: 'center' },
];

const LOBSTER_USAGE_MODEL_COLUMNS = [
  { id: 'model', role: 'primary', grow: 1 },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'prompt', role: 'count', align: 'center' },
  { id: 'completion', role: 'count', align: 'center' },
  { id: 'cached', role: 'count', align: 'center' },
];

const LOBSTER_MODEL_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'model', role: 'primary', grow: 1 },
  { id: 'source', role: 'type' },
  { id: 'multiplier', role: 'meta', align: 'center' },
];

// LobsterAIBrand 是 LobsterAI（网易有道龙虾）的简洁内联品牌图标（爪形，无外部依赖）。
export function LobsterAIBrand({ className }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
      <path d="M7 4.2c1 0 1.8.9 1.8 2v3a3.2 3.2 0 0 1 6.4 0v-3c0-1.1.8-2 1.8-2s1.8.9 1.8 2v6.3a7.8 7.8 0 0 1-15.6 0V6.2c0-1.1.8-2 1.8-2Z" />
      <path d="M12 20.6c-.5 0-.9-.3-1.1-.7l-1.5-2.4a.9.9 0 0 1 1.5-1l1.1 1.7 1.1-1.7a.9.9 0 0 1 1.5 1l-1.5 2.4c-.2.4-.6.7-1.1.7Z" opacity="0.7" />
    </svg>
  );
}

// LobsterAIPlugin：模型网关「插件中心」卡片——网易有道 LobsterAI 反代。
// 通过手机号/网页登录账号后，插件把 LobsterAI 上游包装成 OpenAI 兼容端点，
// 可一键接入网关端点列表，由网关统一路由/计费/日志；并支持每日自动签到。
export function LobsterAIPlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
  const fileInputRef = useRef(null);
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [models, setModels] = useState([]);
  const [modelsReady, setModelsReady] = useState(true);
  const [linkState, setLinkState] = useState(null);
  const [usage, setUsage] = useState(null);
  const [usageDays, setUsageDays] = useState(7);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [prefixDraft, setPrefixDraft] = useState(null);
  const [busyAccount, setBusyAccount] = useState('');
  const [linkBusy, setLinkBusy] = useState(false);
  const [checkinBusy, setCheckinBusy] = useState(false);
  const [editAccount, setEditAccount] = useState(null);
  const [editName, setEditName] = useState('');
  const [creditsAccount, setCreditsAccount] = useState(null);
  // creditsSeqRef 用于丢弃快速切换账号时后到的过期响应。
  const creditsSeqRef = useRef(0);
  const [creditsData, setCreditsData] = useState(null);
  const [creditsBusy, setCreditsBusy] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [login, setLogin] = useState({ phase: 'idle', state: '', url: '', error: '' });
  const [callbackDraft, setCallbackDraft] = useState('');
  const [callbackBusy, setCallbackBusy] = useState(false);

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

  const loadAccounts = async () => {
    try {
      const res = await fetch(`${API}/accounts`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setAccounts(data.accounts || []);
    } catch {
      /* 保留上一次列表 */
    }
  };

  const loadModels = async () => {
    try {
      const res = await fetch(`${API}/models`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) {
        setModels(data.models || []);
        setModelsReady(!!data.upstreamReady);
      }
    } catch {
      /* 保留上一次列表 */
    }
  };

  const loadUsage = async () => {
    try {
      const res = await fetch(`${API}/usage?days=${usageDays}`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setUsage(data);
    } catch {
      /* 保留上一次数据 */
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
    loadStatus();
    loadModels();
    loadLink();
  }, []);

  useEffect(() => {
    loadUsage();
  }, [usageDays]);

  const refreshAll = async () => {
    setRefreshing(true);
    try {
      await Promise.all([loadStatus(), loadAccounts(), loadModels(), loadLink(), loadUsage()]);
    } finally {
      setRefreshing(false);
    }
  };

  useEffect(() => {
    if (!loginOpen || !login.state || login.phase === 'success') return undefined;
    const timer = setInterval(async () => {
      try {
        const res = await fetch(`${API}/login/poll`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ state: login.state }),
        });
        const data = await res.json();
        if (!res.ok || !data?.success) throw new Error(data?.error || '轮询失败');
        if (data.status === 'success') {
          setLogin(prev => ({ ...prev, phase: 'success', error: '' }));
          toast.success('LobsterAI 登录成功');
          await Promise.all([loadAccounts(), loadStatus(), loadModels()]);
          setLoginOpen(false);
        }
      } catch (e) {
        setLogin(prev => (prev.phase === 'success' ? prev : { ...prev, phase: 'error', error: e.message }));
      }
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [loginOpen, login.state, login.phase]);

  const startLogin = async () => {
    setLogin({ phase: 'loading', state: '', url: '', error: '' });
    try {
      const res = await fetch(`${API}/login/start`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '发起登录失败');
      setLogin({ phase: 'waiting', state: data.state, url: data.url, error: '' });
      if (data.url) window.open(data.url, '_blank', 'noopener,noreferrer');
    } catch (e) {
      setLogin({ phase: 'error', state: '', url: '', error: e.message });
    }
  };

  const openLogin = () => {
    setLoginOpen(true);
    startLogin();
  };

  const completeLogin = async () => {
    const raw = callbackDraft.trim();
    if (!raw) {
      toast.error('请先粘贴回调地址或授权码');
      return;
    }
    setCallbackBusy(true);
    try {
      const res = await fetch(`${API}/login/callback`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ state: login.state, url: raw }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '完成登录失败');
      setLogin(prev => ({ ...prev, phase: 'success', error: '' }));
      setCallbackDraft('');
      toast.success('LobsterAI 登录成功');
      await Promise.all([loadAccounts(), loadStatus(), loadModels()]);
      setLoginOpen(false);
    } catch (e) {
      setLogin(prev => ({ ...prev, phase: 'error', error: e.message }));
    } finally {
      setCallbackBusy(false);
    }
  };

  const handleLoginOpenChange = next => {
    setLoginOpen(next);
    if (!next) {
      setCallbackDraft('');
      setLogin({ phase: 'idle', state: '', url: '', error: '' });
    }
  };

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

  const commitModelPrefix = () => {
    const v = String(prefixDraft ?? '').trim();
    setPrefixDraft(null);
    if (v !== (settings?.modelPrefix || '')) update({ modelPrefix: v }, true);
  };

  const toggleAccount = async (account, enabled) => {
    setBusyAccount(account.id);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ disabled: !enabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      toast.success(enabled ? '账号已启用' : '账号已停用');
      await Promise.all([loadAccounts(), loadStatus()]);
    } catch (e) {
      toast.error(e.message);
    } finally {
      setBusyAccount('');
    }
  };

  const deleteAccount = async account => {
    if (!confirmPress(`lobsterai-account-delete:${account.id}`, `删除账号 ${account.id}`)) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '删除失败');
      toast.success('账号已删除');
      await Promise.all([loadAccounts(), loadStatus()]);
    } catch (e) {
      toast.error(`删除失败：${e.message}`);
    }
  };

  const refreshAccount = async account => {
    setBusyAccount(account.id);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/refresh`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '刷新失败');
      toast.success('token 已刷新');
      await loadAccounts();
    } catch (e) {
      toast.error(`刷新失败：${e.message}`);
    } finally {
      setBusyAccount('');
    }
  };

  const checkinAccount = async account => {
    setBusyAccount(account.id);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/checkin`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '签到失败');
      const result = data.result || {};
      if (result.status === 'success') {
        toast.success(result.gained ? `签到成功，积分 +${result.gained}` : '签到成功');
      } else {
        toast.warning(result.message || '本次没有可领取的签到');
      }
      await Promise.all([loadAccounts(), loadModels()]);
    } catch (e) {
      toast.error(`签到失败：${e.message}`);
    } finally {
      setBusyAccount('');
    }
  };

  const checkinAll = async () => {
    setCheckinBusy(true);
    try {
      const res = await fetch(`${API}/checkin`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '签到失败');
      const results = data.results || [];
      const ok = results.filter(r => r.success).length;
      toast.success(`已对 ${ok}/${results.length} 个账号执行签到`);
      await Promise.all([loadAccounts(), loadModels()]);
    } catch (e) {
      toast.error(`签到失败：${e.message}`);
    } finally {
      setCheckinBusy(false);
    }
  };

  const openCredits = async account => {
    setCreditsAccount(account);
    setCreditsData(null);
    setCreditsBusy(true);
    // 快速切换账号时，前一个请求的响应可能后到并覆盖当前展示，
    // 故用请求序号丢弃过期响应。
    const seq = ++creditsSeqRef.current;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/credits`, {
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (seq !== creditsSeqRef.current) return;
      if (!res.ok || !data?.success) throw new Error(data?.error || '查询失败');
      setCreditsData(data);
    } catch (e) {
      if (seq === creditsSeqRef.current) toast.error(`额度查询失败：${e.message}`);
    } finally {
      if (seq === creditsSeqRef.current) setCreditsBusy(false);
    }
  };

  const saveEditAccount = async () => {
    if (!editAccount) return;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(editAccount.id)}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ nickname: editName.trim() }),
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

  const applyModels = next => {
    setModels(next);
    setSettings(s => (s ? { ...s, disabledModels: next.filter(m => !m.enabled).map(m => m.id) } : s));
  };

  const toggleModel = async (model, enabled) => {
    try {
      const res = await fetch(`${API}/models/toggle/${encodeURIComponent(model.id)}`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      applyModels(models.map(m => (m.id === model.id ? { ...m, enabled } : m)));
    } catch (e) {
      toast.error(e.message);
    }
  };

  const toggleAllModels = async enabled => {
    try {
      const res = await fetch(`${API}/models/toggle-batch`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled, models: models.map(m => m.id) }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      applyModels(models.map(m => ({ ...m, enabled })));
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
      await loadModels();
    } catch (e) {
      toast.error(e.message);
    } finally {
      setLinkBusy(false);
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
        await Promise.all([loadAccounts(), loadStatus()]);
      } catch (e) {
        toast.error(`导入失败：${e.message}`);
      }
    };
    reader.readAsText(file);
  };

  const refreshCatalog = async () => {
    try {
      const res = await fetch(`${API}/models?refresh=1`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) {
        setModels(data.models || []);
        setModelsReady(!!data.upstreamReady);
        toast.success('模型目录已刷新');
      }
    } catch {
      toast.error('模型目录刷新失败');
    }
  };

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  const allEnabled = models.length > 0 && models.every(m => m.enabled);

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
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" disabled={refreshing} onClick={refreshAll}>
            <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} /> 刷新
          </Button>
          <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
            {saving ? '保存中...' : '保存设置'}
          </Button>
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <SectionCard title="LobsterAI" icon={<LobsterAIBrand className="h-4 w-4 text-brand" />} bodyPadding="none">
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
          <FieldRow title={<span title="每日 9 点与 21 点自动为全部账号签到（+100 积分/号/天），关闭后仅可手动签到">每日自动签到</span>}>
            <Switch checked={!!settings?.autoCheckin} onCheckedChange={v => update({ autoCheckin: v })} />
          </FieldRow>
          <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀（如 lobster-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
            <Input
              size="sm"
              className="w-40"
              placeholder="lobster-"
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="多账号时的选号策略。固定首个：始终用列表第一个可用账号，其余作主备，行为最可预期。轮询：依次轮流，请求均匀分摊。按剩余额度：优先用剩余积分最多的账号，避免某个账号先耗尽。后两种都自动跳过已停用、token 过期与失败冷却中的账号">选号策略</span>}>
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
              disabled={saving || accounts.length < 2}
            />
          </FieldRow>
          <FieldRow title={<span title="可用账号/账号总数、处于失败冷却的账号数，以及上游模型目录条数">运行状态</span>}>
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <Badge
                variant={status?.availableCount ? 'success' : 'warning'}
                className="text-xs"
                title="可参与转发的账号数（未停用且 token 未失效）"
              >
                可用账号 {status?.availableCount ?? 0}/{status?.accountCount ?? 0}
              </Badge>
              {status?.coolingCount ? (
                <Badge variant="warning" className="text-xs" title="因上游 429/5xx 被临时冷却的账号数">
                  冷却 {status.coolingCount}
                </Badge>
              ) : null}
              <span className="text-xs text-kumo-subtle" title="上游模型目录条数">
                模型 {status?.modelCount ?? 0}
              </span>
              {status && !status.upstreamReady ? (
                <Badge variant="warning" className="text-xs">上游协议层未就绪</Badge>
              ) : null}
            </div>
          </FieldRow>
        </SectionCard>

        <SectionCard
          title="账号"
          icon={<Users className="h-4 w-4 text-brand" />}
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
              <Button size="sm" variant="outline" disabled={checkinBusy} onClick={checkinAll}>
                <Clock className="h-3.5 w-3.5" /> 全部签到
              </Button>
              <Button size="sm" variant="primary" onClick={openLogin}>
                <Plus className="h-3.5 w-3.5" /> 账号登录
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId="lobsterai-accounts" columns={LOBSTER_ACCOUNT_COLUMNS} className="w-full min-w-[54rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2 !py-1.5 text-center">启用</Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">积分</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">调用</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">token</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accounts.map(a => {
                    const meta = tokenStateMeta(a.tokenState);
                    return (
                      <Table.Row key={a.id} className="h-10">
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!a.disabled}
                              disabled={busyAccount === a.id}
                              onCheckedChange={v => toggleAccount(a, v)}
                              aria-label={`${a.disabled ? '启用' : '停用'} ${a.id}`}
                            />
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <div className="min-w-0">
                            <div className="truncate text-sm font-medium text-kumo-strong" title={a.id}>
                              {a.nickname || a.id}
                            </div>
                            <div className="truncate font-mono text-kumo-subtle">{a.id}</div>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex justify-center">
                            <Popover
                              open={creditsAccount?.id === a.id}
                              onOpenChange={open => {
                                if (open) {
                                  openCredits(a);
                                } else if (creditsAccount?.id === a.id) {
                                  setCreditsAccount(null);
                                }
                              }}
                            >
                              <Popover.Trigger
                                nativeButton={false}
                                render={
                                  <Button
                                    type="button"
                                    size="sm"
                                    variant="ghost"
                                    className="font-mono text-xs focus:outline-none focus:ring-0 focus-visible:outline-none focus-visible:ring-0"
                                    title="点击查看各批次积分与到期时间"
                                  >
                                    {fmtCredits(a.credits)}
                                  </Button>
                                }
                              />
                              <Popover.Content
                                side="bottom"
                                align="center"
                                className="w-80 shrink-0 px-3 pb-2 pt-2.5 max-h-[min(70vh,28rem)] overflow-y-auto overscroll-contain scrollbar-thin"
                              >
                                <div className="flex items-center justify-between gap-2">
                                  <span className="min-w-0 truncate text-xs font-semibold leading-normal text-kumo-strong">
                                    {a.nickname || a.id}
                                  </span>
                                  {creditsAccount?.id === a.id && !creditsBusy ? (
                                    <span className="shrink-0 font-mono text-xs text-kumo-subtle">
                                      合计 {fmtCredits(creditsData?.credits)}
                                    </span>
                                  ) : null}
                                </div>

                                <div className="mt-1.5">
                                  {creditsBusy ? (
                                    <div className="flex h-16 w-full items-center justify-center">
                                      <Loader size="sm" />
                                    </div>
                                  ) : creditsData?.items?.length ? (
                                    <div className="flex flex-col divide-y divide-kumo-line">
                                      {creditsData.items.map((it, idx) => (
                                        <div
                                          key={`${it.type}-${it.expiresAt}-${idx}`}
                                          className="flex items-center justify-between gap-3 py-1.5"
                                        >
                                          <div className="min-w-0">
                                            <div className="truncate text-xs text-kumo-strong" title={it.label || it.type}>
                                              {it.label || it.type || '—'}
                                            </div>
                                            <div className="truncate font-mono text-[0.85em] text-kumo-subtle">
                                              {fmtExpiry(it.expiresAt) || '无到期时间'}
                                            </div>
                                          </div>
                                          <span className="shrink-0 font-mono text-xs text-kumo-strong">
                                            {fmtCredits(it.creditsRemaining)}
                                          </span>
                                        </div>
                                      ))}
                                    </div>
                                  ) : (
                                    <div className="py-3 text-center text-xs text-kumo-subtle">
                                      暂无未过期的积分批次
                                    </div>
                                  )}
                                </div>

                                <div className="mt-1.5 border-t border-kumo-line pt-1.5 pb-0.5">
                                  <span className="text-[0.7em] leading-normal text-kumo-subtle">
                                    {a.callCount ?? 0} 次调用 · 积分按批次到期，先到期的先扣
                                  </span>
                                </div>
                              </Popover.Content>
                            </Popover>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <span className="font-mono text-xs text-kumo-strong" title="该账号累计转发次数">{a.callCount ?? 0}</span>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex flex-col items-center gap-1">
                            <Badge variant={meta.variant} className="text-xs" title={a.lastError || undefined}>
                              {meta.label}
                              {a.tokenState === 'expiring' && a.expiresInSeconds ? ` ${fmtLeft(a.expiresInSeconds)}` : ''}
                            </Badge>
                            {a.cooling ? (
                              <Badge variant="warning" className="text-xs" title="上游返回 429/5xx，暂时跳过该账号">冷却中</Badge>
                            ) : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex items-center justify-center gap-1">
                            <Button
                              size="sm"
                              shape="square"
                              variant="outline"
                              aria-label={`签到 ${a.id}`}
                              title="立即签到"
                              disabled={busyAccount === a.id}
                              onClick={() => checkinAccount(a)}
                            >
                              <Clock className="h-3 w-3" />
                            </Button>
                            <Button
                              size="sm"
                              shape="square"
                              variant="outline"
                              aria-label={`刷新 ${a.id}`}
                              title="刷新 token"
                              disabled={busyAccount === a.id}
                              onClick={() => refreshAccount(a)}
                            >
                              <RefreshCw className="h-3 w-3" />
                            </Button>
                            <Button
                              size="sm"
                              shape="square"
                              variant="outline"
                              aria-label={`编辑 ${a.id}`}
                              onClick={() => {
                                setEditName(a.nickname || '');
                                setEditAccount(a);
                              }}
                            >
                              <Edit className="h-3 w-3" />
                            </Button>
                            <Button
                              size="sm"
                              shape="square"
                              variant={isArmed(`lobsterai-account-delete:${a.id}`) ? 'destructive' : 'secondary-destructive'}
                              aria-label={isArmed(`lobsterai-account-delete:${a.id}`) ? `再次确认删除 ${a.id}` : `删除 ${a.id}`}
                              title={isArmed(`lobsterai-account-delete:${a.id}`) ? '再次点击确认删除' : `删除 ${a.id}`}
                              onClick={() => deleteAccount(a)}
                            >
                              <Trash className="h-3 w-3" />
                            </Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    );
                  })}
                </Table.Body>
              </AppTable>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无账号"
                description="点击「账号登录」，在新标签页完成手机号/网页登录后自动加入账号列表。"
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="用量"
          icon={<TrendingUp className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <div className="flex items-center gap-2">
              <Select
                size="sm"
                className="w-28"
                value={String(usageDays)}
                onValueChange={v => setUsageDays(Number(v))}
                items={[
                  { value: '1', label: '今天' },
                  { value: '7', label: '近 7 天' },
                  { value: '30', label: '近 30 天' },
                  { value: '90', label: '近 90 天' },
                ]}
              />
              <Button size="sm" variant="outline" onClick={loadUsage}>
                刷新
              </Button>
            </div>
          }
        >
          {usage?.totals?.requests ? (
            <>
              <div className="grid grid-cols-2 gap-2 p-3 cq-sm:grid-cols-4">
                <div className="rounded border border-kumo-line px-2 py-1.5 text-center">
                  <div className="text-base font-semibold text-kumo-strong">{usage.totals.requests}</div>
                  <div className="text-[10px] text-kumo-subtle">调用次数</div>
                </div>
                <div className="rounded border border-kumo-line px-2 py-1.5 text-center">
                  <div className="text-base font-semibold text-kumo-strong">{fmtTokens(usage.totals.promptTokens)}</div>
                  <div className="text-[10px] text-kumo-subtle">
                    输入词元（缓存 {fmtTokens(usage.totals.cachedTokens)}）
                  </div>
                </div>
                <div className="rounded border border-kumo-line px-2 py-1.5 text-center">
                  <div className="text-base font-semibold text-kumo-strong">{fmtTokens(usage.totals.completionTokens)}</div>
                  <div className="text-[10px] text-kumo-subtle">输出词元</div>
                </div>
                <div className="rounded border border-kumo-line px-2 py-1.5 text-center">
                  <div className="text-base font-semibold text-kumo-strong">{fmtRate(usage.totals.cacheHitRate)}</div>
                  <div className="text-[10px] text-kumo-subtle">缓存命中率</div>
                </div>
              </div>

              <div className="overflow-x-auto border-t border-kumo-line">
                <AppTable tableId="lobsterai-usage-accounts" columns={LOBSTER_USAGE_ACCOUNT_COLUMNS} className="w-full min-w-[40rem] text-xs">
                  <Table.Header variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">调用</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">输入</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">输出</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">缓存命中</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {usage.byAccount.map(row => (
                      <Table.Row key={row.accountId} className="h-9">
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <div className="truncate text-kumo-strong" title={row.accountId}>
                            {row.accountName || row.accountId}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                          {row.requests}
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-subtle">
                          {fmtTokens(row.promptTokens)}
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-subtle">
                          {fmtTokens(row.completionTokens)}
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-subtle">
                          {fmtTokens(row.cachedTokens)}
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </div>

              <div className="overflow-x-auto border-t border-kumo-line">
                <AppTable tableId="lobsterai-usage-models" columns={LOBSTER_USAGE_MODEL_COLUMNS} className="w-full min-w-[40rem] text-xs">
                  <Table.Header variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">调用</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">输入</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">输出</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">缓存命中</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {usage.byModel.map(row => (
                      <Table.Row key={row.model} className="h-9">
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <div className="truncate font-mono text-kumo-strong" title={row.model}>
                            {row.model}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                          {row.requests}
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-subtle">
                          {fmtTokens(row.promptTokens)}
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-subtle">
                          {fmtTokens(row.completionTokens)}
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-subtle">
                          {fmtTokens(row.cachedTokens)}
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </div>
            </>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无用量"
                description={`近 ${usageDays} 天没有经本插件转发的调用记录。`}
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="模型"
          icon={<Layers className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <Button size="sm" variant="outline" onClick={refreshCatalog} title="从上游重新拉取模型目录">
              <RefreshCw className="h-3.5 w-3.5" /> 刷新目录
            </Button>
          }
        >
          {models.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId="lobsterai-models" columns={LOBSTER_MODEL_COLUMNS} className="w-full min-w-[44rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2 !py-1.5 text-center">
                      <div className="flex justify-center">
                        <Switch
                          size="sm"
                          checked={allEnabled}
                          onCheckedChange={toggleAllModels}
                          title={allEnabled ? '全部停用' : '全部启用'}
                          aria-label="全选模型"
                        />
                      </div>
                    </Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">来源</Table.Head>
                    <Table.Head className="!px-2 !py-1.5 text-center">倍率</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {models.map(m => (
                    <Table.Row key={m.id} className="h-9">
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex justify-center">
                          <Switch
                            size="sm"
                            checked={!!m.enabled}
                            onCheckedChange={v => toggleModel(m, v)}
                            title={
                              m.enabled
                                ? '停用：写入网关端点停用名单（网关不再路由该模型），直连中继也会被拒'
                                : '启用：从网关端点停用名单移除，恢复路由'
                            }
                            aria-label={`${m.enabled ? '停用' : '启用'} ${m.id}`}
                          />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="truncate font-mono text-kumo-strong" title={m.id}>
                          {m.id}
                        </div>
                        {m.name && m.name !== m.id ? (
                          <div className="truncate text-kumo-subtle">{m.name}</div>
                        ) : null}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="text-xs text-kumo-subtle">{m.provider || '—'}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-xs text-kumo-subtle" title="上游 costMultiplier，相对倍率，非货币单价">
                          {m.costMultiplier ? `x${Number(m.costMultiplier).toFixed(2)}` : '—'}
                        </span>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </AppTable>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState
                title={modelsReady ? '暂无模型' : '模型目录不可用'}
                description={
                  modelsReady
                    ? '插件未返回可服务的模型，请点右上角刷新重试。'
                    : '模型目录拉取失败，请点右上角刷新重试。'
                }
              />
            </div>
          )}
        </SectionCard>
      </div>

      <Dialog.Root open={loginOpen} onOpenChange={handleLoginOpenChange}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(30rem,calc(100vw-2rem))] !max-w-[min(30rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">登录 LobsterAI</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              在新标签页完成手机号/网页登录。本机运行时凭据自动回填；部署在远程服务器时，授权后浏览器会跳到一个打不开的地址，把地址栏里的完整链接粘贴到下方即可。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col gap-3">
              {login.phase === 'loading' ? (
                <div className="flex h-[96px] w-full items-center justify-center rounded border border-kumo-line">
                  <Loader size="lg" />
                </div>
              ) : (
                <div className="flex min-h-[96px] w-full items-center justify-center rounded border border-kumo-line px-4 text-center text-xs">
                  {login.phase === 'error' ? (
                    <span className="text-kumo-strong">{login.error}</span>
                  ) : login.phase === 'success' ? (
                    <span className="text-kumo-strong">登录成功。</span>
                  ) : login.phase === 'waiting' ? (
                    <span className="text-kumo-subtle">已打开登录页面，正在等待登录完成…</span>
                  ) : (
                    <span className="text-kumo-subtle">暂无登录链接</span>
                  )}
                </div>
              )}
              {login.url ? (
                <a
                  className="max-w-full truncate font-mono text-[0.75em] text-brand underline"
                  href={login.url}
                  target="_blank"
                  rel="noreferrer"
                  title={login.url}
                >
                  {login.url}
                </a>
              ) : null}
              {login.phase !== 'success' ? (
                <Input
                  size="sm"
                  className="w-full"
                  aria-label="粘贴回调地址"
                  placeholder="粘贴登录后地址栏里的完整回调链接（或授权码）"
                  value={callbackDraft}
                  onChange={e => setCallbackDraft(e.target.value)}
                />
              ) : null}
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>关闭</Button>} />
            <Button size="sm" variant="secondary" disabled={login.phase === 'success'} onClick={startLogin}>
              重新发起登录
            </Button>
            <Button
              size="sm"
              variant="primary"
              disabled={callbackBusy || login.phase === 'success' || !callbackDraft.trim()}
              onClick={completeLogin}
            >
              用粘贴的链接完成登录
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!editAccount} onOpenChange={open => !open && setEditAccount(null)}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">编辑账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              {editAccount ? `修改 ${editAccount.id} 的备注名，仅影响本机展示。` : ''}
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <Input
              size="sm"
              label="备注名"
              type="text"
              className="w-full"
              value={editName}
              onChange={e => setEditName(e.target.value)}
            />
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
