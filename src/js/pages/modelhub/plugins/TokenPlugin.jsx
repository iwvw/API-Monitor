import { useEffect, useRef, useState } from 'react';
import { Button, Switch, Loader, Dialog, Input, Badge, Table, Toolbar, Select, Popover } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState, AppTable } from '../../../components/ui/AppPrimitives.jsx';
import { Rocket, Users, Layers, TrendingUp, RefreshCw, Plus, Trash, Edit, Upload, Download } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { dialog } from '../../../modules/dialog.js';
import { getAuthHeaders, formatCompact } from '../utils.js';

// TokenPlugin 是「粘贴凭证型」插件的通用面板。
//
// Cline 的账号接入方式相同（粘贴上游 token，没有本地回调式网页登录），
// 面板结构一致，因此抽成一个参数化组件，避免重复代码各自漂移。
//
// config 字段：
//   - api            后端前缀（如 '/api/cline'）
//   - brandIcon      品牌图标组件
//   - title          卡片标题
//   - credentialFields  额外凭证字段 [{ key, label, placeholder, required }]
//   - hasRegion      是否有国内/国际区域
//   - hasCheckin     是否有签到
//   - strategyOptions  选号策略选项
//   - addHint        添加弹窗的说明文案
export function TokenPlugin({
  config,
  api,
  brandIcon: BrandIcon,
  title,
  credentialFields = [],
  hasRegion = false,
  hasCheckin = false,
  strategyOptions,
  addHint = '',
  headerActions = null,
}) {
  const fileInputRef = useRef(null);
  const settingsSeqRef = useRef(0);
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [accounts, setAccounts] = useState([]);
  const [models, setModels] = useState([]);
  const [modelsReady, setModelsReady] = useState(true);
  const [freeOnly, setFreeOnly] = useState(false);
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
  const [editRegion, setEditRegion] = useState('cn');
  const [addOpen, setAddOpen] = useState(false);
  const [addName, setAddName] = useState('');
  const [addRegion, setAddRegion] = useState('cn');
  const [addBusy, setAddBusy] = useState(false);
  const [addValues, setAddValues] = useState({});

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

  const fmtLeft = seconds => {
    const left = Number(seconds) || 0;
    if (left <= 0) return '';
    const h = Math.floor(left / 3600);
    const m = Math.floor((left % 3600) / 60);
    return h > 0 ? `${h}h${m}m` : `${Math.max(1, m)}m`;
  };

  const fmtTokens = v => formatCompact(Number(v) || 0);
  const fmtRate = r => {
    const n = Number(r);
    if (!Number.isFinite(n) || n <= 0) return '—';
    return `${(n * 100).toFixed(1)}%`;
  };

  const ACCOUNT_COLUMNS = [
    { id: 'enabled', role: 'control' },
    { id: 'account', role: 'primary', grow: 1 },
    ...(hasRegion ? [{ id: 'region', role: 'meta', align: 'center' }] : []),
    { id: 'calls', role: 'count', align: 'center' },
    { id: 'token', role: 'status' },
    { id: 'actions', role: 'actions-md' },
  ];

  const USAGE_ACCOUNT_COLUMNS = [
    { id: 'account', role: 'primary', grow: 1 },
    { id: 'calls', role: 'count', align: 'center' },
    { id: 'prompt', role: 'count', align: 'center' },
    { id: 'completion', role: 'count', align: 'center' },
    { id: 'cached', role: 'count', align: 'center' },
  ];

  const USAGE_MODEL_COLUMNS = [...USAGE_ACCOUNT_COLUMNS];

  const MODEL_COLUMNS = [
    { id: 'enabled', role: 'control' },
    { id: 'model', role: 'primary', grow: 1 },
    ...(config?.modelMeta ? [{ id: 'meta', role: 'meta', align: 'center' }] : []),
  ];

  const load = async () => {
    try {
      const res = await fetch(`${api}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
      setAccounts(data.settings?.accounts || []);
      setFreeOnly(!!data.settings?.freeOnly);
    } catch (e) {
      toast.error(`插件设置加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  const loadStatus = async () => {
    try {
      const res = await fetch(`${api}/status`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setStatus(data);
    } catch {
      setStatus(null);
    }
  };

  const loadAccounts = async () => {
    try {
      const res = await fetch(`${api}/accounts`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setAccounts(data.accounts || []);
    } catch {
      /* 保留上一次列表 */
    }
  };

  const loadModels = async () => {
    try {
      const res = await fetch(`${api}/models`, { headers: getAuthHeaders() });
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
      const res = await fetch(`${api}/usage?days=${usageDays}`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setUsage(data);
    } catch {
      /* 保留上一次数据 */
    }
  };

  const loadLink = async () => {
    try {
      const res = await fetch(`${api}/link`, { headers: getAuthHeaders() });
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
      const res = await fetch(`${api}/settings`, {
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
      const res = await fetch(`${api}/accounts/${encodeURIComponent(account.id)}/toggle`, {
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

  const toggleCheckin = async (account, enabled) => {
    setBusyAccount(account.id);
    try {
      const res = await fetch(`${api}/accounts/${encodeURIComponent(account.id)}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ checkinDisabled: !enabled }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      await loadAccounts();
    } catch (e) {
      toast.error(e.message);
    } finally {
      setBusyAccount('');
    }
  };

  const deleteAccount = async account => {
    if (!(await dialog.deleteResource({ message: `确定删除账号 ${account.id} 吗?该账号凭据将从本机移除。` }))) return;
    try {
      const res = await fetch(`${api}/accounts/${encodeURIComponent(account.id)}`, {
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
      const res = await fetch(`${api}/accounts/${encodeURIComponent(account.id)}/test`, {
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

  const checkinAccount = async account => {
    setCheckinBusy(true);
    try {
      const res = await fetch(`${api}/checkin`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '签到失败');
      toast.success(data.message || '签到完成');
      await loadAccounts();
    } catch (e) {
      toast.error(`签到失败：${e.message}`);
    } finally {
      setCheckinBusy(false);
    }
  };

  const checkinAll = async () => {
    setCheckinBusy(true);
    try {
      const res = await fetch(`${api}/checkin`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: '{}',
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '签到失败');
      const ok = (data.results || []).filter(r => r.success).length;
      toast.success(`签到完成：${ok}/${(data.results || []).length} 成功`);
      await loadAccounts();
    } catch (e) {
      toast.error(`签到失败：${e.message}`);
    } finally {
      setCheckinBusy(false);
    }
  };

  const submitAdd = async () => {
    const body = { nickname: addName.trim() };
    if (hasRegion) body.region = addRegion;
    for (const field of credentialFields) {
      const v = String(addValues[field.key] || '').trim();
      if (field.required && !v) {
        toast.error(`请填写${field.label}`);
        return;
      }
      body[field.key] = v;
    }
    setAddBusy(true);
    try {
      const res = await fetch(`${api}/accounts`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '添加失败');
      toast.success('账号已添加');
      setAddOpen(false);
      setAddName('');
      setAddValues({});
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
      const body = { nickname: editName.trim() };
      if (hasRegion) body.region = editRegion;
      const res = await fetch(`${api}/accounts/${encodeURIComponent(editAccount.id)}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(body),
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

  const hasFreePool = models.some(m => m.pool === 'free');
  const nonFreeIds = models.filter(m => m.pool !== 'free').map(m => m.id);
  const visibleModels = freeOnly && hasFreePool ? models.filter(m => m.pool === 'free') : models;

  // 免费模式（入库偏好 settings.freeOnly）：列表只显示免费池，非免费模型批量停用；
  // 关闭只恢复显示（DisabledModels 由专用启停接口管理，PUT 会被服务端强制保留 current）。
  const toggleFreeMode = async enabled => {
    if (!settings) {
      toast.error('插件设置尚未加载');
      setFreeOnly(false);
      return;
    }
    setFreeOnly(enabled);
    try {
      if (enabled && nonFreeIds.length > 0) {
        const res = await fetch(`${api}/models/toggle-batch`, {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ enabled: false, models: nonFreeIds }),
        });
        const data = await res.json();
        if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
        applyModels(models.map(m => (nonFreeIds.includes(m.id) ? { ...m, enabled: false } : m)));
      }
    } catch (e) {
      toast.error(e.message);
      setFreeOnly(false);
      return;
    }
    const ok = await save(
      { ...settings, freeOnly: enabled },
      enabled ? '免费模式已启用' : '已退出免费模式'
    );
    if (!ok) setFreeOnly(false);
  };

  const toggleModel = async (model, enabled) => {
    try {
      const res = await fetch(`${api}/models/toggle/${encodeURIComponent(model.id)}`, {
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
    const targetIds = new Set(visibleModels.map(m => m.id));
    try {
      const res = await fetch(`${api}/models/toggle-batch`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled, models: [...targetIds] }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '操作失败');
      applyModels(models.map(m => (targetIds.has(m.id) ? { ...m, enabled } : m)));
    } catch (e) {
      toast.error(e.message);
    }
  };

  const linkPlugin = async action => {
    setLinkBusy(true);
    try {
      const res = await fetch(`${api}/link`, {
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

  const exportAccounts = () => window.open(`${api}/accounts/export`, '_blank');

  const importFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(String(reader.result || '{}'));
        const payload = Array.isArray(parsed) ? { accounts: parsed } : parsed;
        const res = await fetch(`${api}/accounts/import`, {
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

  const allEnabled = visibleModels.length > 0 && visibleModels.every(m => m.enabled);
  const regionItems = [
    { value: 'cn', label: '国内版' },
    { value: 'intl', label: '国际版' },
  ];

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
        <SectionCard title={title} icon={<BrandIcon className="h-4 w-4 text-brand" />} bodyPadding="none">
          <FieldRow title={<span title="总开关：同时控制中继与模型网关接入。打开后启用中继并注册为网关端点；关闭后中继拒服并移除端点">启用中继</span>}>
            <Switch
              checked={!!settings?.enabled}
              disabled={saving || linkBusy}
              onCheckedChange={toggleEnabled}
            />
          </FieldRow>
          <FieldRow title={<span title="给本插件对外暴露的所有模型名统一加前缀，便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加">模型前缀</span>}>
            <Input
              size="sm"
              className="w-40"
              placeholder={`${config.id}-`}
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          </FieldRow>
          <FieldRow title={<span title="多账号时的选号策略。两种都自动跳过已停用、token 过期与失败冷却中的账号">选号策略</span>}>
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-44"
              value={settings?.accountStrategy || 'first'}
              onValueChange={v => update({ accountStrategy: v })}
              items={strategyOptions}
              disabled={saving || accounts.length < 2}
            />
          </FieldRow>
          {hasCheckin ? (
            <FieldRow title={<span title="站点时区每天自动签到（按天幂等）。账号级签到开关独立于转发开关">每日自动签到</span>}>
              <Switch checked={settings?.autoCheckin !== false} onCheckedChange={v => update({ autoCheckin: v })} />
            </FieldRow>
          ) : null}
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
            </div>
          </FieldRow>
        </SectionCard>

        <SectionCard
          title="账号"
          icon={<Users className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <div className="flex items-center gap-1.5">
              {typeof headerActions === 'function' ? headerActions(refreshAll) : headerActions}
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
              {hasCheckin ? (
                <Button size="sm" variant="outline" disabled={checkinBusy} onClick={checkinAll}>
                  全部签到
                </Button>
              ) : null}
              <Button size="sm" variant="primary" onClick={() => setAddOpen(true)}>
                <Plus className="h-3.5 w-3.5" /> 手动添加
              </Button>
            </div>
          }
        >
          {accounts.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId={`${config.id}-accounts`} columns={ACCOUNT_COLUMNS} className="w-full min-w-[48rem] text-xs">
                <Table.Header variant="compact">
                  <Table.Row className="h-8">
                    <Table.Head className="!px-2 !py-1.5 text-center">启用</Table.Head>
                    <Table.Head className="!px-2.5 !py-1.5">账号</Table.Head>
                    {hasRegion ? <Table.Head className="!px-2 !py-1.5 text-center">区域</Table.Head> : null}
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
                              {a.nickname || a.email || a.id}
                            </div>
                            <div className="truncate font-mono text-kumo-subtle" title={a.id}>
                              {a.id}
                            </div>
                          </div>
                        </Table.Cell>
                        {hasRegion ? (
                          <Table.Cell className="!px-2 !py-1.5 text-center">
                            <Badge variant={a.region === 'intl' ? 'purple' : 'blue'} className="text-xs">
                              {a.region === 'intl' ? '国际' : '国内'}
                            </Badge>
                          </Table.Cell>
                        ) : null}
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
                            {a.lastCheckinAt ? (
                              <span className="font-mono text-[10px] text-kumo-subtle" title="最近一次签到时刻">
                                {String(a.lastCheckinAt).slice(0, 10)}
                              </span>
                            ) : null}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center">
                          <div className="flex items-center justify-center gap-1">
                            {hasCheckin ? (
                              <Button
                                size="sm"
                                shape="square"
                                variant="outline"
                                aria-label={`签到 ${a.id}`}
                                title={a.checkinDisabled ? '该账号已停用签到，点击启用并签到' : '立即签到'}
                                disabled={busyAccount === a.id || checkinBusy}
                                onClick={() => {
                                  if (a.checkinDisabled) void toggleCheckin(a, true);
                                  else void checkinAccount(a);
                                }}
                              >
                                <TrendingUp className="h-3 w-3" />
                              </Button>
                            ) : null}
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
                                setEditRegion(a.region === 'intl' ? 'intl' : 'cn');
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
              <EmptyState title="暂无账号" description="点击「手动添加」，粘贴上游凭证即可加入账号列表。" />
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
                <AppTable tableId={`${config.id}-usage-accounts`} columns={USAGE_ACCOUNT_COLUMNS} className="w-full min-w-[40rem] text-xs">
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
                <AppTable tableId={`${config.id}-usage-models`} columns={USAGE_MODEL_COLUMNS} className="w-full min-w-[40rem] text-xs">
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
              <EmptyState title="暂无用量" description={`近 ${usageDays} 天没有经本插件转发的调用记录。`} />
            </div>
          )}
        </SectionCard>

        <SectionCard
          title="模型"
          icon={<Layers className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={hasFreePool ? (
            <label className="flex items-center gap-1.5 text-xs text-kumo-subtle">
              <Switch
                size="sm"
                checked={freeOnly}
                onCheckedChange={toggleFreeMode}
                aria-label="启用免费"
                title="只启用免费（free）额度池：非免费模型批量停用，列表只显示免费模型"
              />
              启用免费
            </label>
          ) : null}
        >
          {models.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId={`${config.id}-models`} columns={MODEL_COLUMNS} className="w-full min-w-[26rem] text-xs">
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
                    {config?.modelMeta ? (
                      <Table.Head className="!px-2 !py-1.5 text-center">{config.modelMeta.label}</Table.Head>
                    ) : null}
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleModels.map(m => (
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
                      {config?.modelMeta ? (
                        <Table.Cell className="!px-2 !py-1.5 text-center text-kumo-subtle">
                          {config.modelMeta.render ? config.modelMeta.render(m) : (m[config.modelMeta.key] ?? '—')}
                        </Table.Cell>
                      ) : null}
                    </Table.Row>
                  ))}
                  {freeOnly && hasFreePool && visibleModels.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={config?.modelMeta ? 3 : 2} className="text-center py-6 text-sm text-kumo-subtle">
                        无免费模型
                      </Table.Cell>
                    </Table.Row>
                  ) : null}
                </Table.Body>
              </AppTable>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState
                title={modelsReady ? '暂无模型' : '模型目录不可用'}
                description={modelsReady ? '插件未返回可服务的模型。' : '模型目录拉取失败（需要至少一个可用账号）。'}
              />
            </div>
          )}
        </SectionCard>
      </div>

      <Dialog.Root open={addOpen} onOpenChange={setAddOpen}>
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">添加{title}账号</Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">{addHint}</Dialog.Description>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="flex flex-col gap-3">
              <Input size="sm" label="备注名（可选）" type="text" className="w-full" value={addName} onChange={e => setAddName(e.target.value)} />
              {hasRegion ? (
                <Select
                  alignItemWithTrigger
                  size="sm"
                  label="区域"
                  className="w-full"
                  value={addRegion}
                  onValueChange={setAddRegion}
                  items={regionItems}
                />
              ) : null}
              {credentialFields.map(field => (
                <Input
                  key={field.key}
                  size="sm"
                  label={field.label}
                  type="password"
                  autoComplete="off"
                  className="w-full font-mono text-[0.8em]"
                  value={addValues[field.key] || ''}
                  onChange={e => setAddValues(prev => ({ ...prev, [field.key]: e.target.value }))}
                  placeholder={field.placeholder || ''}
                />
              ))}
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
            <div className="flex flex-col gap-3">
              <Input
                size="sm"
                label="备注名"
                type="text"
                className="w-full"
                value={editName}
                onChange={e => setEditName(e.target.value)}
              />
              {hasRegion ? (
                <Select
                  alignItemWithTrigger
                  size="sm"
                  label="区域"
                  className="w-full"
                  value={editRegion}
                  onValueChange={setEditRegion}
                  items={regionItems}
                />
              ) : null}
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
