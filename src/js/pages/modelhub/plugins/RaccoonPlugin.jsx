import { useEffect, useRef, useState } from 'react';
import { Button, Switch, Loader, Dialog, Input, Badge, Table, Toolbar, Select, Popover } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState, AppTable } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, Users, Layers, TrendingUp, RefreshCw, Plus, Trash, Edit, Upload, Download, ExternalLink, Clock, RaccoonBrand } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { dialog } from '../../../modules/dialog.js';
import { getAuthHeaders, formatCompact } from '../utils.js';

const API = '/api/raccoon';

const fmtLeft = seconds => {
  const left = Number(seconds) || 0;
  if (left <= 0) return '';
  const h = Math.floor(left / 3600);
  const m = Math.floor((left % 3600) / 60);
  return h > 0 ? `${h}h${m}m` : `${Math.max(1, m)}m`;
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

const fmtPoints = value => {
  const n = Number(value);
  if (!Number.isFinite(n)) return '—';
  return n.toLocaleString();
};

const RACCOON_ACCOUNT_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'account', role: 'primary', grow: 1 },
  { id: 'points', role: 'meta', align: 'center' },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'token', role: 'status' },
  { id: 'actions', role: 'actions-md' },
];

const RACCOON_USAGE_ACCOUNT_COLUMNS = [
  { id: 'account', role: 'primary', grow: 1 },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'prompt', role: 'count', align: 'center' },
  { id: 'completion', role: 'count', align: 'center' },
  { id: 'cached', role: 'count', align: 'center' },
];

const RACCOON_USAGE_MODEL_COLUMNS = [
  { id: 'model', role: 'primary', grow: 1 },
  { id: 'calls', role: 'count', align: 'center' },
  { id: 'prompt', role: 'count', align: 'center' },
  { id: 'completion', role: 'count', align: 'center' },
  { id: 'cached', role: 'count', align: 'center' },
];

const RACCOON_MODEL_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'model', role: 'primary', grow: 1 },
  { id: 'multiplier', role: 'meta', align: 'center' },
];

// fmtMultiplier 把倍率渲染成紧凑文本：整数不带小数点，其余最多两位并裁零。
const fmtMultiplier = value => {
  const n = Number(value);
  if (!Number.isFinite(n)) return '—';
  if (Number.isInteger(n)) return String(n);
  return String(Math.round(n * 100) / 100);
};

// RaccoonPlugin：模型网关「插件中心」卡片——商汤小浣熊反代。
// 用账号的 access_token（+refresh_token）把 xiaohuanxiong.com 包装成 OpenAI 兼容端点，
// 可一键接入网关端点列表；支持多账号选号、token 自动刷新、积分查询与用量统计。
export function RaccoonPlugin() {
  const fileInputRef = useRef(null);
  const settingsSeqRef = useRef(0);
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
  const [onboardingBusy, setOnboardingBusy] = useState(false);
  const [editAccount, setEditAccount] = useState(null);
  const [editName, setEditName] = useState('');
  const [balanceAccount, setBalanceAccount] = useState(null);
  const balanceSeqRef = useRef(0);
  const [balanceData, setBalanceData] = useState(null);
  const [balanceBusy, setBalanceBusy] = useState(false);
  const [points, setPoints] = useState({});
  const [addOpen, setAddOpen] = useState(false);
  const [addName, setAddName] = useState('');
  const [addToken, setAddToken] = useState('');
  const [addRefresh, setAddRefresh] = useState('');
  const [addBusy, setAddBusy] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [loginUrl, setLoginUrl] = useState('');
  const [loginState, setLoginState] = useState('');
  const [loginAuto, setLoginAuto] = useState(false);
  const [loginPhase, setLoginPhase] = useState('idle'); // idle|waiting|success|error
  const [loginError, setLoginError] = useState('');
  const [loginCallback, setLoginCallback] = useState('');
  const [loginBusy, setLoginBusy] = useState(false);
  const [loginStarting, setLoginStarting] = useState(false);

  // loadPointsInner 拉取给定账号列表的可用积分并写入 points 映射。
  // 定义在 load 之前，避免 const 的暂时性死区（TDZ）。
  const loadPointsInner = async list => {
    if (!Array.isArray(list) || !list.length) return;
    const results = await Promise.all(
      list.map(async a => {
        try {
          const res = await fetch(`${API}/accounts/${encodeURIComponent(a.id)}/balance`, { headers: getAuthHeaders() });
          const data = await res.json();
          if (!res.ok || !data?.success) return null;
          const v = data.balance?.available_points;
          return v == null ? null : [a.id, v];
        } catch {
          return null;
        }
      })
    );
    const map = {};
    for (const r of results) if (r) map[r[0]] = r[1];
    if (Object.keys(map).length) setPoints(prev => ({ ...prev, ...map }));
  };

  const load = async () => {
    try {
      const res = await fetch(`${API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
      const list = data.settings?.accounts || [];
      setAccounts(list);
      // 打开页面即自动拉取各账号积分，列表直接显示可用积分。
      void loadPointsInner(list);
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

  // 自动拉取各账号可用积分，填充账号表「积分」列（失败静默，保留上一次值）。
  const loadPoints = list => loadPointsInner(Array.isArray(list) ? list : accounts);

  const loadAccounts = async () => {
    try {
      const res = await fetch(`${API}/accounts`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) {
        const list = data.accounts || [];
        setAccounts(list);
        void loadPoints(list);
      }
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

  const save = async (next, msg = '设置已保存', snapshot) => {
    if (!next) {
      toast.error('插件设置尚未加载,无法保存');
      return false;
    }
    const seq = ++settingsSeqRef.current;
    setSaving(true);
    try {
      const res = await fetch(`${API}/settings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(next),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      if (seq === settingsSeqRef.current) setSettings(data.settings);
      toast.success(msg);
      return true;
    } catch (e) {
      if (seq === settingsSeqRef.current) {
        if (snapshot !== undefined) setSettings(snapshot);
        toast.error(`保存失败：${e.message}`);
      }
      return false;
    } finally {
      if (seq === settingsSeqRef.current) setSaving(false);
    }
  };

  const update = (patch, silent = false) => {
    if (!settings) {
      toast.error('插件设置尚未加载,暂不能修改');
      return;
    }
    const snapshot = settings;
    const next = { ...settings, ...patch };
    setSettings(next);
    if (!silent) void save(next, '设置已保存', snapshot);
  };

  // 启用开关:先落库设置,成功后再接入/移除网关端点。
  const toggleEnabled = async enabled => {
    if (!settings) {
      toast.error('插件设置尚未加载,暂不能修改');
      return;
    }
    const ok = await save({ ...settings, enabled }, '设置已保存', settings);
    if (ok) await linkPlugin(enabled ? 'link' : 'unlink');
  };

  const commitModelPrefix = () => {
    const v = String(prefixDraft ?? '').trim();
    setPrefixDraft(null);
    if (v !== (settings?.modelPrefix || '')) update({ modelPrefix: v });
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
    if (!(await dialog.deleteResource({ message: `确定删除账号 ${account.id} 吗?该账号凭据将从本机移除。` }))) return;
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

  const testAccount = async account => {
    setBusyAccount(account.id);
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '测试失败');
      toast.success(`连通正常：${data.modelCount ?? 0} 个模型`);
      await loadAccounts();
    } catch (e) {
      toast.error(`测试失败：${e.message}`);
    } finally {
      setBusyAccount('');
    }
  };

  const openBalance = async account => {
    setBalanceAccount(account);
    setBalanceData(null);
    setBalanceBusy(true);
    const seq = ++balanceSeqRef.current;
    try {
      const res = await fetch(`${API}/accounts/${encodeURIComponent(account.id)}/balance?refresh=1`, {
        headers: getAuthHeaders(),
      });
      const data = await res.json();
      if (seq !== balanceSeqRef.current) return;
      if (!res.ok || !data?.success) throw new Error(data?.error || '查询失败');
      setBalanceData(data);
      if (data.balance?.available_points != null) {
        setPoints(prev => ({ ...prev, [account.id]: data.balance.available_points }));
      }
    } catch (e) {
      if (seq === balanceSeqRef.current) toast.error(`积分查询失败：${e.message}`);
    } finally {
      if (seq === balanceSeqRef.current) setBalanceBusy(false);
    }
  };

  const openLogin = async () => {
    setLoginOpen(true);
    setLoginCallback('');
    setLoginUrl('');
    setLoginState('');
    setLoginError('');
    setLoginPhase('idle');
    setLoginStarting(true);
    // 只有浏览器与后端同机（localhost/回环）时，127.0.0.1 回调才可能回到后端；
    // 通过公网 IP 远程访问时，127.0.0.1 指向用户自己的电脑，自动回调必然等不到。
    const host = window.location.hostname;
    const localBrowser = host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host === '::1';
    try {
      const res = await fetch(`${API}/login/start`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ local: localBrowser }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '发起登录失败');
      setLoginUrl(data.url || '');
      setLoginState(data.state || '');
      setLoginAuto(!!data.auto);
      setLoginPhase('waiting');
      if (data.url) window.open(data.url, '_blank', 'noopener,noreferrer');
    } catch (e) {
      toast.error(e.message);
      setLoginOpen(false);
    } finally {
      setLoginStarting(false);
    }
  };

  const finishLogin = async () => {
    if (!loginCallback.trim()) return;
    setLoginBusy(true);
    try {
      const res = await fetch(`${API}/login/exchange`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ callbackUrl: loginCallback.trim() }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '登录失败');
      toast.success(data.updated ? '账号已更新' : '登录成功，账号已添加');
      setLoginOpen(false);
      await Promise.all([loadAccounts(), loadStatus(), loadModels()]);
    } catch (e) {
      toast.error(`登录失败：${e.message}`);
    } finally {
      setLoginBusy(false);
    }
  };

  // 自动模式：轮询 /login/wait，浏览器 loopback 回调一到就自动完成。
  useEffect(() => {
    if (!loginOpen || !loginState || loginPhase !== 'waiting' || !loginAuto) return undefined;
    const timer = setInterval(async () => {
      try {
        const res = await fetch(`${API}/login/wait`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ state: loginState }),
        });
        const data = await res.json();
        if (data?.status === 'success') {
          setLoginPhase('success');
          toast.success(data.updated ? '账号已更新' : '登录成功，账号已添加');
          clearInterval(timer);
          await Promise.all([loadAccounts(), loadStatus(), loadModels()]);
          setLoginOpen(false);
        } else if (data?.status === 'error' || data?.status === 'expired') {
          setLoginPhase('error');
          setLoginError(data?.error || '登录已超时，请重新发起');
          clearInterval(timer);
        }
      } catch {
        /* 网络抖动：下一轮继续 */
      }
    }, 2000);
    return () => clearInterval(timer);
  }, [loginOpen, loginState, loginPhase, loginAuto]);

  const submitAdd = async () => {
    if (!addToken.trim()) {
      toast.error('请填写 access_token');
      return;
    }
    setAddBusy(true);
    try {
      const res = await fetch(`${API}/accounts`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          nickname: addName.trim(),
          access_token: addToken.trim(),
          refresh_token: addRefresh.trim(),
        }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '添加失败');
      toast.success('账号已添加');
      setAddOpen(false);
      setAddName('');
      setAddToken('');
      setAddRefresh('');
      await Promise.all([loadAccounts(), loadStatus(), loadModels()]);
    } catch (e) {
      toast.error(`添加失败：${e.message}`);
    } finally {
      setAddBusy(false);
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

  const runCheckin = async account => {
    setCheckinBusy(true);
    try {
      const res = await fetch(`${API}/checkin`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(account ? { accountId: account.id } : {}),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '签到失败');
      if (Array.isArray(data.results)) {
        const ok = data.results.filter(r => r.success).length;
        toast.success(`签到完成：${ok}/${data.results.length} 成功`);
      } else {
        toast.success(data.message || '签到完成');
      }
      await Promise.all([loadAccounts(), loadStatus()]);
    } catch (e) {
      toast.error(`签到失败：${e.message}`);
    } finally {
      setCheckinBusy(false);
    }
  };

  const claimOnboarding = async account => {
    setOnboardingBusy(true);
    try {
      const res = await fetch(`${API}/onboarding`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(account ? { accountId: account.id } : {}),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '领取失败');
      toast.success(data.gained > 0 ? `首登奖励已领取，+${data.gained} 积分` : '首登奖励已结算（无可领额度）');
      await Promise.all([loadAccounts(), loadStatus()]);
    } catch (e) {
      toast.error(`领取失败：${e.message}`);
    } finally {
      setOnboardingBusy(false);
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
        <SectionCard title="小浣熊" icon={<RaccoonBrand className="h-4 w-4 text-brand" />} bodyPadding="none">
          <FieldRow title={<span title="总开关：同时控制中继与模型网关接入。打开后启用中继并注册为网关端点；关闭后中继拒服并移除端点">启用中继</span>}>
            <Switch
              checked={!!settings?.enabled}
              disabled={saving || linkBusy}
              onCheckedChange={toggleEnabled}
            />
          </FieldRow>
          <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀（如 raccoon-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
            <Input
              size="sm"
              className="w-40"
              placeholder="raccoon-"
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="站点时区每天 9:05 与 21:05 自动签到（按天幂等）。上游按 UTC+8 自然日发放每日积分（实测 300 分/天，约 7 天有效），触发方式是打一次 setting_info。签到后会自动探测一次性首登奖励（桌面端 3000 + 手机端 3000，各领一次）。账号级签到开关独立于转发开关">每日自动签到</span>}>
            <Switch checked={settings?.autoCheckin !== false} onCheckedChange={v => update({ autoCheckin: v })} />
          </FieldRow>
          <FieldRow title={<span title="多账号时的选号策略。固定首个：始终用列表第一个可用账号，其余作主备，行为最可预期。轮询：依次轮流，请求均匀分摊。两种都自动跳过已停用、token 过期与失败冷却中的账号">选号策略</span>}>
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-44"
              value={settings?.accountStrategy || 'first'}
              onValueChange={v => update({ accountStrategy: v })}
              items={[
                { value: 'first', label: '固定首个可用' },
                { value: 'round-robin', label: '轮询' },
              ]}
              disabled={saving || accounts.length < 2}
            />
          </FieldRow>
          <FieldRow title={<span title="可用账号/账号总数、处于失败冷却的账号数，以及模型目录条数">运行状态</span>}>
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
              <span className="text-xs text-kumo-subtle" title="模型目录条数">
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
              <Button size="sm" variant="outline" disabled={checkinBusy} onClick={() => runCheckin(null)}>
                全部签到
              </Button>
              <Button size="sm" variant="outline" disabled={onboardingBusy} onClick={() => claimOnboarding(null)}>
                领首登奖励
              </Button>
              <Button size="sm" variant="primary" onClick={openLogin}>
                <ExternalLink className="h-3.5 w-3.5" /> 网页登录
              </Button>
              <Button size="sm" variant="outline" onClick={() => setAddOpen(true)}>
                <Plus className="h-3.5 w-3.5" /> 手动添加
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId="raccoon-accounts" columns={RACCOON_ACCOUNT_COLUMNS} className="w-full min-w-[48rem] text-xs">
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
                            <div className="truncate font-mono text-kumo-subtle" title={a.id}>
                              {a.id}
                            </div>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex justify-center">
                            <Popover
                              open={balanceAccount?.id === a.id}
                              onOpenChange={open => {
                                if (open) {
                                  openBalance(a);
                                } else if (balanceAccount?.id === a.id) {
                                  setBalanceAccount(null);
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
                                    className="px-1 font-mono text-xs text-kumo-strong underline-offset-2 hover:underline focus:outline-none focus:ring-0 focus-visible:outline-none focus-visible:ring-0"
                                    title={points[a.id] == null ? '积分尚未加载，点击立即查询' : '点击查看积分明细'}
                                  >
                                    {points[a.id] == null ? '···' : fmtPoints(points[a.id])}
                                  </Button>
                                }
                              />
                              <Popover.Content side="bottom" align="center" className="w-64 shrink-0 px-3 pb-2 pt-2.5">
                                <div className="truncate text-xs font-semibold leading-normal text-kumo-strong">
                                  {a.nickname || a.id}
                                </div>
                                {balanceBusy ? (
                                  <div className="flex h-16 w-full items-center justify-center">
                                    <Loader size="sm" />
                                  </div>
                                ) : balanceData?.balance ? (
                                  <div className="mt-1.5 space-y-1">
                                    {[
                                      ['可用积分', 'available_points'],
                                      ['每日积分', 'daily_points'],
                                      ['本月积分', 'monthly_points'],
                                      ['奖励积分', 'reward_points'],
                                      ['充值积分', 'topup_points'],
                                    ].map(([label, key]) => (
                                      <div key={key} className="flex items-center justify-between gap-3 py-0.5">
                                        <span className="text-xs text-kumo-subtle">{label}</span>
                                        <span className="font-mono text-xs text-kumo-strong">
                                          {fmtPoints(balanceData.balance[key])}
                                        </span>
                                      </div>
                                    ))}
                                  </div>
                                ) : (
                                  <div className="py-3 text-center text-xs text-kumo-subtle">暂无积分数据</div>
                                )}
                                {/* 一次性首登奖励：两条各 3000，互不共享额度 */}
                                {a.onboarding ? (
                                  <div className="mt-2 border-t border-kumo-line pt-2">
                                    <div className="mb-1 flex items-center justify-between gap-2">
                                      <span className="text-xs font-semibold text-kumo-strong">首登奖励</span>
                                      <span className="font-mono text-[10px] text-kumo-subtle">
                                        {a.onboarding.earned}/{a.onboarding.total}
                                      </span>
                                    </div>
                                    {(a.onboarding.tasks || []).map(task => (
                                      <div key={task.key} className="flex items-center justify-between gap-3 py-0.5">
                                        <span className="truncate text-xs text-kumo-subtle" title={task.title}>
                                          {task.title}
                                        </span>
                                        <Badge
                                          variant={task.error ? 'danger' : task.already ? 'success' : 'neutral'}
                                          className="shrink-0 !text-[0.72em]"
                                          title={task.error || (task.already ? '已结算' : '未结算')}
                                        >
                                          {task.error ? '失败' : task.already ? '已领' : '待领'}
                                        </Badge>
                                      </div>
                                    ))}
                                    {!a.onboarding.settled ? (
                                      <Button
                                        size="sm"
                                        variant="outline"
                                        className="mt-1.5 w-full"
                                        disabled={onboardingBusy}
                                        onClick={() => claimOnboarding(a)}
                                      >
                                        领取
                                      </Button>
                                    ) : null}
                                  </div>
                                ) : null}
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
                              <Badge
                                variant="warning"
                                className="text-xs"
                                title={
                                  a.cooldownSeconds
                                    ? `该账号被上游限流，冷却中，约 ${fmtLeft(a.cooldownSeconds)} 后重试`
                                    : '上游返回 429/5xx，暂时跳过该账号'
                                }
                              >
                                冷却中{a.cooldownSeconds ? ` ${fmtLeft(a.cooldownSeconds)}` : ''}
                              </Badge>
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
                              title="立即签到（领取当日积分）"
                              disabled={busyAccount === a.id || checkinBusy}
                              onClick={() => runCheckin(a)}
                            >
                              <Clock className="h-3 w-3" />
                            </Button>
                            <Button
                              size="sm"
                              shape="square"
                              variant="outline"
                              aria-label={`测试 ${a.id}`}
                              title="测试连通性"
                              disabled={busyAccount === a.id}
                              onClick={() => testAccount(a)}
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
                              variant="secondary-destructive"
                              aria-label={`删除 ${a.id}`}
                              title={`删除 ${a.id}`}
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
                description="点击「添加账号」，粘贴从小浣熊网页端 / 桌面端取得的 access_token 即可加入账号列表。"
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
                <AppTable tableId="raccoon-usage-accounts" columns={RACCOON_USAGE_ACCOUNT_COLUMNS} className="w-full min-w-[40rem] text-xs">
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
                <AppTable tableId="raccoon-usage-models" columns={RACCOON_USAGE_MODEL_COLUMNS} className="w-full min-w-[40rem] text-xs">
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

        <SectionCard title="模型" icon={<Layers className="h-4 w-4 text-brand" />} bodyPadding="none">
          {models.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId="raccoon-models" columns={RACCOON_MODEL_COLUMNS} className="w-full min-w-[26rem] text-xs">
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
                    <Table.Head className="!px-2 !py-1.5 text-center">
                      <span title="上游实时生效倍率（billing_effective_multiplier），0 表示当前免费">实时倍率</span>
                    </Table.Head>
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
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        {m.multiplier == null ? (
                          <span className="text-kumo-subtle">—</span>
                        ) : (
                          <Badge variant={Number(m.multiplier) === 0 ? 'success' : 'neutral'} className="font-mono text-xs">
                            {Number(m.multiplier) === 0 ? '免费' : `×${fmtMultiplier(m.multiplier)}`}
                          </Badge>
                        )}
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
                description={modelsReady ? '插件未返回可服务的模型。' : '模型目录拉取失败。'}
              />
            </div>
          )}
        </SectionCard>
      </div>

      <Dialog.Root open={loginOpen} onOpenChange={setLoginOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">网页登录小浣熊</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              {loginAuto
                ? '已在新的标签页打开小浣熊授权页。完成登录后会自动跳转回本机并加入账号列表，无需操作。'
                : '已在新的标签页打开小浣熊授权页。完成登录后浏览器会跳转到 127.0.0.1 回调地址（远程访问时该页可能打不开，属正常）；请把地址栏整条 URL 复制粘贴到下方。'}
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col gap-3">
              <div className="flex min-h-[72px] w-full items-center justify-center rounded border border-kumo-line px-4 text-center text-xs">
                {loginStarting ? (
                  <Loader size="lg" />
                ) : loginPhase === 'success' ? (
                  <span className="text-kumo-strong">登录成功，账号已加入列表。</span>
                ) : loginPhase === 'error' ? (
                  <span className="text-kumo-strong">{loginError}</span>
                ) : loginAuto ? (
                  <span className="text-kumo-subtle">已打开授权页，正在等待浏览器自动回调…</span>
                ) : (
                  <span className="text-kumo-subtle">请完成授权页登录，然后把跳转后的整条地址栏 URL 粘贴到下方</span>
                )}
              </div>
              {loginUrl ? (
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="secondary" onClick={() => window.open(loginUrl, '_blank', 'noopener,noreferrer')}>
                    重新打开授权页
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => navigator.clipboard?.writeText(loginUrl)}>
                    复制链接
                  </Button>
                </div>
              ) : null}
              <Input
                size="sm"
                label={loginAuto ? '回调地址（自动模式下无需填写，仅作兜底）' : '回调地址'}
                type="text"
                className="w-full font-mono text-[0.8em]"
                value={loginCallback}
                onChange={e => setLoginCallback(e.target.value)}
                placeholder="office-raccoon://auth/callback?code=...&state=..."
              />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" disabled={loginBusy || !loginCallback.trim()} onClick={finishLogin}>
              {loginBusy ? '登录中...' : '完成登录'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={addOpen} onOpenChange={setAddOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">添加小浣熊账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              浏览器登录 xiaohuanxiong.com 后，在开发者工具 Network 里复制任意请求的
              <span className="font-mono"> Authorization: Bearer &lt;access_token&gt;</span>；桌面端 token 存于
              <span className="font-mono"> ~/.box-agent/config/auth.json</span>。
            </Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col gap-3">
              <Input size="sm" label="备注名（可选）" type="text" className="w-full" value={addName} onChange={e => setAddName(e.target.value)} />
              <Input
                size="sm"
                label="access_token（必填）"
                type="text"
                className="w-full font-mono text-[0.8em]"
                value={addToken}
                onChange={e => setAddToken(e.target.value)}
                placeholder="eyJhbGci…"
              />
              <Input
                size="sm"
                label="refresh_token（推荐）"
                type="text"
                className="w-full font-mono text-[0.8em]"
                value={addRefresh}
                onChange={e => setAddRefresh(e.target.value)}
                placeholder="用于 access_token 过期后自动续期"
              />
            </div>
          </div>
          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-kumo-line px-6 py-4">
            <Dialog.Close render={props => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
            <Button size="sm" variant="primary" disabled={addBusy} onClick={submitAdd}>
              {addBusy ? '添加中...' : '添加'}
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
