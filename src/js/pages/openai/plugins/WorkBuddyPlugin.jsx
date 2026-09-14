import { Fragment, useEffect, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { Button, Switch, Loader, Dialog, LayerCard, Input, Badge, Table, Toolbar, Select } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState } from '../../../components/ui/AppPrimitives.jsx';
import { CodeBuddyBrand, Rocket, Users, Layers, TrendingUp, RefreshCw, Plus, Trash, Edit, Upload, Download } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { getAuthHeaders, formatCompact } from '../utils.js';

const API = '/api/workbuddy';

// 登录轮询间隔：与参考实现一致，2 秒一次，由前端驱动节奏。
const POLL_INTERVAL_MS = 2000;

// fmtLeft 把剩余秒数格式化成「x 小时 y 分 / y 分」。
const fmtLeft = seconds => {
  const left = Number(seconds) || 0;
  if (left <= 0) return '';
  const h = Math.floor(left / 3600);
  const m = Math.floor((left % 3600) / 60);
  return h > 0 ? `${h}h${m}m` : `${Math.max(1, m)}m`;
};

// fmtUntil 把「限流恢复时刻」（Unix 秒）格式化成本地时间，用于悬停提示。
const fmtUntil = unix => {
  const sec = Number(unix) || 0;
  if (sec <= 0) return '—';
  const d = new Date(sec * 1000);
  const p = n => String(n).padStart(2, '0');
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
};

// tokenStateMeta 把后端 token 状态映射为徽标外观与文案。
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

const fmtContext = value => {
  const n = Number(value) || 0;
  if (n <= 0) return '—';
  return n >= 1000 ? `${Math.round(n / 1000)}K` : String(n);
};

// 用量与扣费的展示格式：词元用万/亿压缩，扣费按量级保留 2~4 位小数
// （credit 常是 0.03 这种小数，固定 2 位会全变成 0.03/0.00 看不出差异）。
const fmtTokens = v => formatCompact(Number(v) || 0);
const fmtCredit = v => {
  const n = Number(v) || 0;
  if (!(n > 0)) return '0';
  if (n >= 1000) return n.toFixed(0);
  if (n >= 1) return n.toFixed(2);
  return n.toFixed(4);
};
const fmtRate = r => {
  const n = Number(r);
  if (!Number.isFinite(n) || n <= 0) return '—';
  return `${(n * 100).toFixed(1)}%`;
};

// fmtBalance 格式化余额数值（积分）：整数不带小数，带小数保留两位。
const fmtBalance = v => {
  const n = Number(v);
  if (!Number.isFinite(n)) return '—';
  return Number.isInteger(n) ? String(n) : n.toFixed(2);
};

// fmtCredits 格式化上游积分倍率（来自 /v3/config 的 credits，形如 "x0.79 credits"）。
// 解析失败时退回原始串，再退回「—」，绝不显示成 0（会被误读为免费）。
const fmtCredits = m => {
  if (m?.creditsParsed) return `x${Number(m.creditsMultiplier).toFixed(2)}`;
  return m?.creditsLabel || '—';
};

// WorkBuddyPlugin：模型网关「插件中心」卡片——腾讯 CodeBuddy / WorkBuddy 转 OpenAI 兼容 API。
// 添加国内版/国际版账号后，插件把上游模型目录与对话转发包装成 OpenAI 兼容端点，
// 可一键接入网关端点列表，由网关统一路由/计费/日志。
export function WorkBuddyPlugin() {
  const { isArmed, confirmPress } = useConfirmPress();
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
  // 余额：accountId → Balance（含 error 字段），点按钮按需拉取。
  const [balances, setBalances] = useState({});
  const [balanceLoading, setBalanceLoading] = useState({});
  const [linkBusy, setLinkBusy] = useState(false);
  const [editAccount, setEditAccount] = useState(null);
  const [editName, setEditName] = useState('');
  const [loginOpen, setLoginOpen] = useState(false);
  const [loginRegion, setLoginRegion] = useState('cn');
  const [login, setLogin] = useState({ phase: 'idle', state: '', url: '', image: '', error: '' });
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

  // 用量与扣费：按站点时区近 N 天聚合（后端已 flush 未落盘增量，含刚发生的调用）。
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

  // 进入页面自动查询各账号余额（只查尚未缓存/未在查询中的账号，避免重复打上游）。
  // 依赖账号 id 列表：登录/删除账号后自动补齐，无需手动点「查询」。
  const accountIdKey = accounts.map(a => a.id).join(',');
  useEffect(() => {
    if (!accounts.length) return;
    accounts.forEach(a => {
      if (balances[a.id] || balanceLoading[a.id]) return;
      loadBalance(a);
    });
  }, [accountIdKey]);

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

  // 登录轮询：只在对话框打开且已拿到 state 时运行。
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
          toast.success('CodeBuddy 登录成功');
          await Promise.all([loadAccounts(), loadStatus(), loadModels()]);
          setLoginOpen(false);
        }
      } catch (e) {
        setLogin(prev => (prev.phase === 'success' ? prev : { ...prev, phase: 'error', error: e.message }));
      }
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [loginOpen, login.state, login.phase]);

  // 登录方式：国内版用 CodeBuddy 客户端扫码；国际版是网页登录（OneID/SSO），
  // 需要用户在浏览器里打开授权链接完成登录，故不生成二维码。
  const regionNeedsQR = region => (region || 'cn') !== 'intl';

  const startLogin = async (region = loginRegion) => {
    const target = region || 'cn';
    setLogin({ phase: 'loading', state: '', url: '', image: '', error: '' });
    try {
      const res = await fetch(`${API}/login/start`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ region: target }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '发起登录失败');
      const image = regionNeedsQR(target) ? await QRCode.toDataURL(data.url, { width: 220, margin: 1 }) : '';
      setLogin({ phase: 'waiting', state: data.state, url: data.url, image, error: '' });
    } catch (e) {
      setLogin({ phase: 'error', state: '', url: '', image: '', error: e.message });
    }
  };

  const openLogin = () => {
    setLoginOpen(true);
    startLogin();
  };

  // 切换登录区域：若当前处于等待授权，立即用新区域重新发起一次登录。
  const changeLoginRegion = value => {
    setLoginRegion(value);
    if (loginOpen && login.phase === 'waiting') {
      startLogin(value);
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

  // 模型前缀：输入过程用本地草稿，失焦才提交（避免每次击键 PUT）。
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
      await loadAccounts();
    } catch (e) {
      toast.error(e.message);
    } finally {
      setBusyAccount('');
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

  // 余额：按账号 id 缓存，点「余额」按钮时按需拉取（不自动全量拉，避免频繁打上游）。
  const loadBalance = async account => {
    setBalanceLoading(prev => ({ ...prev, [account.id]: true }));
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/balance`, {
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '查询失败');
      setBalances(prev => ({ ...prev, [account.id]: data.balance }));
    } catch (e) {
      setBalances(prev => ({ ...prev, [account.id]: { error: e.message } }));
    } finally {
      setBalanceLoading(prev => ({ ...prev, [account.id]: false }));
    }
  };

  const deleteAccount = async account => {
    if (!confirmPress(`workbuddy-account-delete:${account.id}`, `删除账号 ${account.id}`)) return;
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

  // applyModels 是模型启停的唯一落点：同时更新表格与 settings.disabledModels。
  // 若只更新表格，「保存设置」的整对象 PUT 会带上切换前的旧名单，
  // 把刚做的启停覆盖掉（现象：开关存不住、刷新后回到之前的状态）。
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

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  const allEnabled = models.length > 0 && models.every(m => m.enabled);

  // 账号按区域分组（国内在上、国际在下），表格内上下分开显示。
  const accountGroups = [
    { region: 'cn', label: '国内版 · codebuddy.cn', items: accounts.filter(a => (a.region || 'cn') !== 'intl') },
    { region: 'intl', label: '国际版 · workbuddy.ai', items: accounts.filter(a => a.region === 'intl') },
  ].filter(g => g.items.length > 0);

  // 模型区域标签：国际独有 / 国内独有 / 共用。国内=蓝、国际=紫，与账号表保持一致。
  // 模型区域标签：按**型号名**判定（后端已按 displayName 对齐两区域）。
  // 国际独有 / 国内独有 / 共用；国内=蓝、国际=紫，与账号表保持一致。
  // 若该 id 自身不是两版都有（如 Kimi-K3 的国内档 kimi-k3-1），tooltip 里补充说明。
  const modelRegionMeta = m => {
    const idOnly = Array.isArray(m.idRegions) && m.idRegions.length === 1 ? m.idRegions[0] : '';
    const idHint = idOnly
      ? idOnly === 'intl'
        ? '（此档位仅国际版提供）'
        : '（此档位仅国内版提供）'
      : '';
    if (m.internationalOnly) {
      return { label: '国际', variant: 'purple', title: '仅国际版提供（workbuddy.ai），转发只会在国际版账号上选号' };
    }
    if (Array.isArray(m.regions) && m.regions.length === 1 && m.regions[0] === 'cn') {
      return { label: '国内', variant: 'blue', title: '仅国内版提供（codebuddy.cn），转发只会在国内版账号上选号' };
    }
    return {
      label: '共用',
      variant: 'outline',
      title: `该型号国内版与国际版都提供，在所有账号间轮询选号${idHint}`,
    };
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 cq-sm:gap-4">
      <div className="flex items-center justify-end gap-2">
        <Button size="sm" variant="outline" disabled={refreshing} onClick={refreshAll}>
          <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} /> 刷新
        </Button>
        <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <SectionCard title="WorkBuddy" icon={<CodeBuddyBrand className="h-4 w-4 text-brand" />} bodyPadding="none">
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
          <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀（如 wb-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
            <Input
              size="sm"
              className="w-40"
              placeholder="wb-"
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="可用账号/账号总数，以及上游模型目录条数">运行状态</span>}>
            <div className="flex min-w-0 items-center gap-2">
              <Badge
                variant={status?.availableCount ? 'success' : 'warning'}
                className="!text-[0.8em]"
                title="可参与转发的账号数（未停用且 token 未失效）"
              >
                可用账号 {status?.availableCount ?? 0}/{status?.accountCount ?? 0}
              </Badge>
              <span
                className="text-xs text-kumo-subtle"
                title="按区域分列的账号数（国内版 codebuddy.cn / 国际版 workbuddy.ai）"
              >
                国内 {status?.regionCounts?.cn?.available ?? 0}/{status?.regionCounts?.cn?.total ?? 0} · 国际{' '}
                {status?.regionCounts?.intl?.available ?? 0}/{status?.regionCounts?.intl?.total ?? 0}
              </span>
              <span className="text-xs text-kumo-subtle" title="上游 /v3/config 返回的模型条数（国内+国际合并去重）">
                模型 {status?.modelCount ?? 0}
              </span>
              {status?.upstreamUsage ? (
                <Badge
                  variant={status.upstreamUsage.cacheReported ? 'success' : 'neutral'}
                  className="!text-[0.8em]"
                  title={
                    '上游最近一次 usage 的字段：\n' +
                    ((status.upstreamUsage.keys || []).join(', ') || '(空)') +
                    '\n\n原始样例：\n' +
                    (status.upstreamUsage.sample || '(空)') +
                    '\n\n时间：' +
                    (status.upstreamUsage.at || '-') +
                    '\n\n网关的缓存统计只认 usage.prompt_tokens_details.cached_tokens。'
                  }
                >
                  {status.upstreamUsage.cacheReported ? '上游已上报缓存命中' : '上游未上报缓存命中'}
                </Badge>
              ) : null}
              {status && !status.upstreamReady ? (
                <Badge variant="warning" className="!text-[0.8em]">上游协议层未就绪</Badge>
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
              <Button size="sm" variant="primary" onClick={openLogin}>
                <Plus className="h-3.5 w-3.5" /> 添加账号
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <div className="overflow-x-auto">
              <Table layout="fixed" className="w-full min-w-[42rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!w-12 !px-2 !py-1.5 text-center">启用</Table.Head>
                    <Table.Head className="!w-16 !px-2 !py-1.5 text-center">区域</Table.Head>
                    <Table.Head className="!w-56 !px-2.5 !py-1.5">账号</Table.Head>
                    <Table.Head className="!w-20 !px-2 !py-1.5 text-center">调用</Table.Head>
                    <Table.Head className="!w-28 !px-2 !py-1.5 text-center">余额</Table.Head>
                    <Table.Head className="!w-32 !px-2 !py-1.5 text-center">token</Table.Head>
                    <Table.Head className="!w-36 !px-2 !py-1.5 text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accountGroups.map(group => (
                    <Fragment key={group.region}>
                      <Table.Row className="h-7 bg-kumo-recessed/40">
                        <Table.Cell colSpan={7} className="!px-2.5 !py-1 text-[0.75em] font-medium text-kumo-subtle">
                          {group.label}（{group.items.length}）
                        </Table.Cell>
                      </Table.Row>
                      {group.items.map(a => {
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
                            <Table.Cell className="!px-2 !py-1.5 text-center">
                              <Badge
                                variant={a.region === 'intl' ? 'purple' : 'blue'}
                                className="!text-[0.72em]"
                                title={a.region === 'intl' ? '国际版账号（workbuddy.ai）' : '国内版账号（codebuddy.cn）'}
                              >
                                {a.region === 'intl' ? '国际' : '国内'}
                              </Badge>
                            </Table.Cell>
                            <Table.Cell className="!px-2.5 !py-1.5">
                              <div className="min-w-0">
                                <div className="truncate text-sm font-medium text-kumo-strong" title={a.id}>
                                  {a.nickname || a.uid || a.id}
                                </div>
                                <div className="truncate font-mono text-[0.8em] text-kumo-subtle">
                                  {[a.uid, a.enterpriseId].filter(Boolean).join(' · ') || a.id}
                                </div>
                              </div>
                            </Table.Cell>
                            <Table.Cell className="!px-2 !py-1.5 text-center">
                              <span className="font-mono text-xs text-kumo-strong" title="该账号累计转发次数">{a.callCount ?? 0}</span>
                            </Table.Cell>
                            <Table.Cell className="!px-2 !py-1.5 text-center">
                              {(() => {
                                const bal = balances[a.id];
                                if (balanceLoading[a.id]) {
                                  return <Loader size="sm" />;
                                }
                                if (bal?.error) {
                                  return (
                                    <span className="text-xs text-kumo-danger" title={bal.error}>
                                      查询失败
                                    </span>
                                  );
                                }
                                if (bal) {
                                  return (
                                    <span
                                      className="font-mono text-xs text-kumo-strong"
                                      title={
                                        `剩余 ${fmtBalance(bal.remain)} / 共 ${fmtBalance(bal.size)}` +
                                        (bal.updatedAt ? `\n更新于 ${bal.updatedAt}` : '') +
                                        ((bal.packages || [])
                                          .map(p => `\n· ${p.name || '计费包'}: ${fmtBalance(p.remain)}/${fmtBalance(p.size)}`)
                                          .join('') || '')
                                      }
                                    >
                                      {fmtBalance(bal.remain)}
                                      <span className="text-kumo-subtle">/{fmtBalance(bal.size)}</span>
                                    </span>
                                  );
                                }
                                return (
                                  <Button
                                    size="sm"
                                    variant="ghost"
                                    disabled={busyAccount === a.id}
                                    onClick={() => loadBalance(a)}
                                    title="查询该账号余额（按需拉取，不打上游轮询）"
                                  >
                                    查询
                                  </Button>
                                );
                              })()}
                            </Table.Cell>
                            <Table.Cell className="!px-2 !py-1.5 text-center">
                              <div className="flex flex-col items-center gap-1">
                                <Badge variant={meta.variant} className="!text-[0.8em]" title={a.lastError || undefined}>
                                  {meta.label}
                                  {a.tokenState === 'expiring' && a.expiresInSeconds ? ` ${fmtLeft(a.expiresInSeconds)}` : ''}
                                </Badge>
                                {/* 模型级限流：只影响列出的模型，账号本身仍可用（其它模型照常转发），
                                    悬浮可看到每个模型的恢复时刻。 */}
                                {(a.limitedModels?.length ?? 0) > 0 && (
                                  <Badge
                                    variant="warning"
                                    className="!text-[0.75em]"
                                    title={a.limitedModels
                                      .map(m => `${m.model} 限流至 ${fmtUntil(m.until)}`)
                                      .join('\n')}
                                  >
                                    限流 {a.limitedModels.length} 模型
                                  </Badge>
                                )}
                              </div>
                            </Table.Cell>
                            <Table.Cell className="!px-2 !py-1.5 text-center">
                              <div className="flex items-center justify-center gap-1">
                                <Button
                                  size="sm"
                                  variant="secondary"
                                  disabled={busyAccount === a.id}
                                  title="立即刷新 access token"
                                  onClick={() => refreshAccount(a)}
                                >
                                  <RefreshCw className={`h-3 w-3 ${busyAccount === a.id ? 'animate-spin' : ''}`} />
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
                                  variant={isArmed(`workbuddy-account-delete:${a.id}`) ? 'destructive' : 'secondary-destructive'}
                                  aria-label={isArmed(`workbuddy-account-delete:${a.id}`) ? `再次确认删除 ${a.id}` : `删除 ${a.id}`}
                                  title={isArmed(`workbuddy-account-delete:${a.id}`) ? '再次点击确认删除' : `删除 ${a.id}`}
                                  onClick={() => deleteAccount(a)}
                                >
                                  <Trash className="h-3 w-3" />
                                </Button>
                              </div>
                            </Table.Cell>
                          </Table.Row>
                        );
                      })}
                    </Fragment>
                  ))}
                </Table.Body>
              </Table>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无账号"
                description="点击「添加账号」，按账号区域完成授权后即可加入账号池。"
              />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="模型"
          icon={<Layers className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={!models.length}
                title="启用全部模型：同时写回网关端点的停用名单"
                onClick={() => toggleAllModels(true)}
              >
                全部启用
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!models.length}
                title="停用全部模型：网关会停止把请求路由到本插件，直连中继也会被拒"
                onClick={() => toggleAllModels(false)}
              >
                全部停用
              </Button>
            </div>
          }
        >
          {models.length ? (
            <div className="overflow-x-auto">
              <Table layout="fixed" className="w-full min-w-[44rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!w-12 !px-2 !py-1.5 text-center">
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
                    <Table.Head className="!w-16 !px-2 !py-1.5 text-center">区域</Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                    <Table.Head className="!w-20 !px-2 !py-1.5 text-center">
                      <span title="上游积分倍率（/v3/config 的 credits），CodeBuddy 计费体系内的相对倍数，不含货币单价">倍率</span>
                    </Table.Head>
                    <Table.Head className="!w-20 !px-2 !py-1.5 text-center">上下文</Table.Head>
                    <Table.Head className="!w-24 !px-2 !py-1.5 text-center">输出上限</Table.Head>
                    <Table.Head className="!w-20 !px-2 !py-1.5 text-center">图像</Table.Head>
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
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <Badge
                          variant={modelRegionMeta(m).variant}
                          className="!text-[0.72em]"
                          title={modelRegionMeta(m).title}
                        >
                          {modelRegionMeta(m).label}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="flex min-w-0 items-center gap-1.5">
                          <span className="truncate font-mono text-[0.8em] text-kumo-strong" title={m.id}>
                            {m.id}
                          </span>
                          {/* 模型级限流：并排在模型名之后，不单独占行。allLimited = 当前所有
                              可用账号都在该模型上限流（此刻真的打不通，中继直接回 429 且不打上游）。 */}
                          {m.limit && (
                            <Badge
                              variant={m.limit.allLimited ? 'danger' : 'warning'}
                              className="!text-[0.7em] shrink-0"
                              title={
                                (m.limit.allLimited
                                  ? `当前所有可用账号（${m.limit.limited} 个）都在该模型上被上游限流，此刻完全打不通`
                                  : `${m.limit.limited} 个账号在该模型上被限流，另有 ${m.limit.usable} 个账号可用（转发会自动选到可用账号）`) +
                                (m.limit.nextRecoveryAt ? `；最早 ${fmtUntil(m.limit.nextRecoveryAt)} 恢复` : '')
                              }
                            >
                              {m.limit.allLimited ? '限流中' : `限流 ${m.limit.limited}/${m.limit.limited + m.limit.usable}`}
                              {m.limit.nextRecoveryAt ? ` · ${fmtUntil(m.limit.nextRecoveryAt)}` : ''}
                            </Badge>
                          )}
                        </div>
                        <div className="truncate text-[0.75em] text-kumo-subtle">
                          {[m.displayName && m.displayName !== m.id ? m.displayName : '', m.vendor ? `vendor ${m.vendor}` : '']
                            .filter(Boolean)
                            .join(' · ') || ' '}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span
                          className="font-mono text-xs text-kumo-strong"
                          title={m.creditsLabel ? `上游原始值：${m.creditsLabel}` : '上游未提供倍率'}
                        >
                          {fmtCredits(m)}
                        </span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-xs text-kumo-subtle">{fmtContext(m.contextLength)}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <span className="font-mono text-xs text-kumo-subtle">{fmtContext(m.maxOutputTokens)}</span>
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {m.supportsImages ? (
                          <Badge variant="success" className="!text-[0.8em]">支持</Badge>
                        ) : (
                          <span className="text-xs text-kumo-subtle">—</span>
                        )}
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState
                title={modelsReady ? '暂无模型' : '模型目录不可用'}
                description={
                  modelsReady
                    ? '上游 /v3/config 未返回可服务对话的模型（图像类模型会被跳过）。'
                    : '模型目录拉取失败，请检查网络/代理，或点右上角刷新重试。'
                }
              />
            </div>
          )}
        </SectionCard>

        {/* 用量与扣费：选号权重就是站点时区「今天」各账号的扣费，切到「今天」即为当前权重 */}
        <SectionCard
          title="用量与扣费"
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
                  <div className="text-base font-semibold text-kumo-strong">{fmtRate(usage.totals.cacheHitRate)}</div>
                  <div className="text-[10px] text-kumo-subtle">缓存命中率</div>
                </div>
                <div className="rounded border border-kumo-line px-2 py-1.5 text-center">
                  <div className="text-base font-semibold text-kumo-strong">{fmtCredit(usage.totals.credit)}</div>
                  <div className="text-[10px] text-kumo-subtle">扣费（credit）</div>
                </div>
              </div>

              <div className="overflow-x-auto border-t border-kumo-line">
                <Table layout="fixed" className="w-full min-w-[40rem] text-xs">
                  <Table.Header variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                      <Table.Head className="!w-20 !px-2 !py-1.5 text-center">调用</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">输入</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">输出</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">缓存命中</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">扣费</Table.Head>
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
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                          {fmtCredit(row.credit)}
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>

              <div className="overflow-x-auto border-t border-kumo-line">
                <Table layout="fixed" className="w-full min-w-[40rem] text-xs">
                  <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                      <Table.Head className="!w-20 !px-2 !py-1.5 text-center">调用</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">输入</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">输出</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">缓存命中</Table.Head>
                      <Table.Head className="!w-24 !px-2 !py-1.5 text-center">扣费</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {usage.byModel.map(row => (
                      <Table.Row key={row.model} className="h-9">
                        <Table.Cell className="!px-2.5 !py-1.5">
                          <div className="truncate font-mono text-[0.8em] text-kumo-strong" title={row.model}>
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
                        <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                          {fmtCredit(row.credit)}
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>

              {usage.creditReported === false ? (
                <div className="border-t border-kumo-line px-3 py-2 text-[11px] text-kumo-subtle">
                  上游未上报 credit 字段，扣费列显示 0（不代表免费）。
                </div>
              ) : null}
            </>
          ) : (
            <div className="p-4">
              <EmptyState
                title="暂无用量"
                description={`近 ${usageDays} 天没有经本插件转发的调用记录，或上游未返回 usage。`}
              />
            </div>
          )}
        </SectionCard>

        {linkState?.linked && linkState?.baseUrl ? (
          <LayerCard className="min-w-0 p-3 shadow-none">
            <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
              <Rocket className="h-3.5 w-3.5 text-brand" />
              <span className="text-kumo-strong">已接入网关端点</span>
              <span className="font-mono text-kumo-subtle" title="本插件在网关端点列表中的 base_url">
                {linkState.baseUrl}
              </span>
              <span className="text-kumo-subtle">· {linkState.models?.length || 0} 个模型</span>
            </div>
          </LayerCard>
        ) : null}
      </div>

      <Dialog.Root open={loginOpen} onOpenChange={setLoginOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(30rem,calc(100vw-2rem))] !max-w-[min(30rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {loginRegion === 'intl' ? '添加国际版账号' : '添加国内版账号'}
            </Dialog.Title>
            <Dialog.Description className="mb-3 text-sm text-kumo-subtle">
              {loginRegion === 'intl'
                ? '国际版为网页登录：点击授权链接在浏览器完成登录即可。'
                : '用 CodeBuddy 客户端扫描二维码完成授权。'}
              国内版与国际版是两套账号、额度不互通，可分别添加。
            </Dialog.Description>
            <div className="mb-4 flex items-center gap-2">
              <span className="text-xs text-kumo-subtle">账号区域</span>
              <Select
                size="sm"
                value={loginRegion}
                onValueChange={changeLoginRegion}
                disabled={login.phase === 'success'}
                aria-label="登录账号区域"
                items={[
                  { value: 'cn', label: '国内版 · codebuddy.cn' },
                  { value: 'intl', label: '国际版 · workbuddy.ai' },
                ]}
              />
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col items-center gap-3">
              {login.phase === 'loading' ? (
                <div className="flex h-[220px] w-[220px] items-center justify-center">
                  <Loader size="lg" />
                </div>
              ) : login.image ? (
                <img src={login.image} alt="登录二维码" className="h-[220px] w-[220px]" />
              ) : login.url && loginRegion === 'intl' ? (
                // 国际版为网页登录：给出可点击的授权链接与说明，而非二维码。
                <div className="flex w-full flex-col items-center gap-3 rounded border border-kumo-line px-4 py-6 text-center">
                  <span className="text-xs text-kumo-subtle">
                    国际版需在浏览器中完成登录，请点击下方链接授权后返回本页。
                  </span>
                  <a
                    className="max-w-full truncate font-mono text-[0.78em] text-brand underline"
                    href={login.url}
                    target="_blank"
                    rel="noreferrer"
                    title={login.url}
                  >
                    {login.url}
                  </a>
                </div>
              ) : (
                <div className="flex h-[220px] w-[220px] items-center justify-center rounded border border-kumo-line px-4 text-center text-xs text-kumo-subtle">
                  {login.error || '暂无二维码'}
                </div>
              )}
              <div className="text-center text-xs text-kumo-subtle">
                {login.phase === 'waiting' ? (loginRegion === 'intl' ? '等待浏览器授权…' : '等待扫码授权…') : null}
                {login.phase === 'success' ? '登录成功。' : null}
                {login.phase === 'error' ? <span className="text-kumo-strong">{login.error}</span> : null}
              </div>
              {login.url && loginRegion !== 'intl' ? (
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
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>关闭</Button>} />
            <Button size="sm" variant="primary" onClick={() => startLogin()}>
              {loginRegion === 'intl' ? '重新获取链接' : '重新获取二维码'}
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
