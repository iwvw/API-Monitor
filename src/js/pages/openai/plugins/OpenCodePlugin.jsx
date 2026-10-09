import { useEffect, useState } from 'react';
import { Button, Switch, Loader, Input, Badge, Table, Select } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState, AppTable } from '../../../components/ui/AppPrimitives.jsx';
import { OpenCodeBrand, Terminal, Rocket, RefreshCw, Layers } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { getAuthHeaders } from '../utils.js';

const API = '/api/opencode';

const OPENCODE_MODEL_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'model', role: 'primary', grow: 1 },
  { id: 'capability', role: 'status' },
];

// 会话身份字段（键与 opencode-proxy 的 session.json 保持一致，便于直接粘贴）。
const SESSION_FIELDS = [
  ['userAgent', 'User-Agent'],
  ['xOpencodeClient', 'x-opencode-client'],
  ['xOpencodeOrgId', 'x-opencode-org-id'],
  ['xOpencodeProject', 'x-opencode-project'],
  ['xOpencodeSession', 'x-opencode-session'],
  ['xSessionId', 'x-session-id'],
  ['xSessionAffinity', 'x-session-affinity'],
];

export { OpenCodeBrand };

// OpenCodePlugin：模型网关「插件中心」卡片——OpenCode Zen 免费模型转 OpenAI 兼容 API。
// 配置会话身份后，插件把 free 层模型（mimo / longcat / muse-spark）经
// OpenCode 身份握手转发到 Zen，并接入网关端点统一路由/计费/日志。
export function OpenCodePlugin() {
  const [settings, setSettings] = useState(null);
  const [status, setStatus] = useState(null);
  const [models, setModels] = useState([]);
  const [modelsReady, setModelsReady] = useState(true);
  const [linkState, setLinkState] = useState(null);
  const [proxypoolPools, setProxypoolPools] = useState([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [linkBusy, setLinkBusy] = useState(false);
  const [prefixDraft, setPrefixDraft] = useState(null);
  const [apiKeyDraft, setApiKeyDraft] = useState(null);

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

  const loadStatus = async () => {
    try {
      const res = await fetch(`${API}/status`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setStatus(data);
    } catch {
      setStatus(null);
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

  const loadLink = async () => {
    try {
      const res = await fetch(`${API}/link`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setLinkState(data);
    } catch {
      setLinkState(null);
    }
  };

  const loadProxyPools = async () => {
    try {
      const res = await fetch('/api/proxypool', { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok && data?.success) setProxypoolPools(data.pools || []);
    } catch {
      setProxypoolPools([]);
    }
  };

  useEffect(() => {
    load();
    loadStatus();
    loadModels();
    loadLink();
    loadProxyPools();
  }, []);

  const refreshAll = async () => {
    setRefreshing(true);
    try {
      await Promise.all([load(), loadStatus(), loadModels(), loadLink(), loadProxyPools()]);
    } finally {
      setRefreshing(false);
    }
  };

  const save = async (next, msg = '设置已保存') => {
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
      setApiKeyDraft(null);
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

  // API Key：草稿非空才随设置提交（留空保持不变；显式清空则回到免费层）。
  const saveSettings = () => {
    const next = { ...(settings || {}) };
    if (apiKeyDraft !== null) next.apiKey = apiKeyDraft;
    void save(next);
  };

  const commitModelPrefix = () => {
    const v = String(prefixDraft ?? '').trim();
    setPrefixDraft(null);
    if (v !== (settings?.modelPrefix || '')) update({ modelPrefix: v }, true);
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

  const allEnabled = models.length > 0 && models.every(m => m.enabled);
  const session = settings?.session || {};

  const field = (label, desc, control) => (
    <FieldRow title={<span title={desc}>{label}</span>}>{control}</FieldRow>
  );

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
          <Button size="sm" variant="primary" disabled={saving} onClick={saveSettings}>
            {saving ? '保存中...' : '保存设置'}
          </Button>
        </div>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <SectionCard title="OpenCode Zen" icon={<OpenCodeBrand className="h-4 w-4 text-brand" />} bodyPadding="none">
          {field(
            '启用中继',
            '总开关：同时控制中继与模型网关接入。打开后启用中继并注册为网关端点；关闭后中继拒服并移除端点',
            <Switch
              checked={!!settings?.enabled}
              disabled={linkBusy}
              onCheckedChange={v => {
                update({ enabled: v });
                if (v) linkPlugin('link');
                else linkPlugin('unlink');
              }}
            />
          )}
          {field(
            '模型前缀',
            '给本插件对外暴露的所有模型名统一加前缀（如 oc-），便于在网关端点列表区分来源；请求转发时自动剥掉前缀还原到原模型，留空表示不加',
            <Input
              size="sm"
              className="w-40"
              placeholder="oc-"
              aria-label="模型前缀"
              value={prefixDraft ?? settings?.modelPrefix ?? ''}
              onChange={e => setPrefixDraft(e.target.value)}
              onBlur={commitModelPrefix}
              disabled={saving}
            />
          )}
          {field(
            '代理池',
            '引用「代理池」插件管理的池，按请求轮询出口并共享冷却/429 禁用状态',
            <Select
              alignItemWithTrigger
              size="sm"
              className="w-full max-w-md"
              value={settings?.proxyPoolId || ''}
              onValueChange={v => update({ proxyPoolId: v || '' })}
              placeholder="不使用"
              items={[
                { value: '', label: '不使用' },
                ...proxypoolPools.map(p => ({
                  value: p.id,
                  label: `${p.name || p.id}（${p.proxies?.length || 0} 个出口）`,
                })),
              ]}
            />
          )}
          {field(
            'Zen API Key（可选）',
            '付费 Zen 模型使用的 key；留空则免费模型使用字面 key "public"。免费层按出口 IP 限流，无需任何账号',
            <Input
              size="sm"
              className="w-full max-w-md"
              aria-label="Zen API Key"
              placeholder={settings?.hasApiKey ? '已配置（留空保持不变）' : '留空使用免费层（public）'}
              value={apiKeyDraft ?? ''}
              onChange={e => setApiKeyDraft(e.target.value)}
              disabled={saving}
            />
          )}
          <div className="flex min-w-0 flex-col gap-3 border-b border-kumo-line px-4 py-3 last:border-b-0 cq-tight:flex-row cq-tight:items-center">
            <div className="min-w-0 shrink-0">
              <div className="truncate text-sm font-semibold text-kumo-strong">运行状态</div>
            </div>
            <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-4 gap-y-2 cq-tight:justify-end">
              <div className="flex min-w-0 items-center gap-2">
                <span className="text-xs text-kumo-subtle">会话身份</span>
                <Badge
                  variant={status?.sessionReady ? 'success' : 'warning'}
                  className="text-xs"
                  title={status?.sessionReady ? '已配置，转发时重放给 Zen 上游' : '未配置 xSessionId，中继会拒绝服务'}
                >
                  {status?.sessionReady ? '已配置' : '未配置'}
                </Badge>
              </div>
              <div className="flex min-w-0 items-center gap-2" title="上游模型目录条数">
                <span className="text-xs text-kumo-subtle">模型</span>
                <span className="font-mono text-xs text-kumo-default">{status?.modelCount ?? 0}</span>
              </div>
              {status && !status.upstreamReady ? (
                <Badge variant="warning" className="text-xs">上游协议层未就绪</Badge>
              ) : null}
            </div>
          </div>
        </SectionCard>

        <SectionCard
          title="会话身份"
          icon={<Terminal className="h-4 w-4 text-brand" />}
          bodyPadding="none"
        >
          <div className="flex flex-col gap-1 border-b border-kumo-line px-4 py-3">
            <div className="text-xs leading-relaxed text-kumo-subtle">
              Zen 免费层只对「看起来像 OpenCode 客户端」的请求放行。请填写真实 opencode 会话的身份请求头；身份过期（上游报 FreeTierError）时换一份新的即可。
            </div>
          </div>
          <div className="grid grid-cols-1 gap-3 px-4 py-3 cq-md:grid-cols-2">
            {SESSION_FIELDS.map(([key, label]) => (
              <div key={key} className="flex min-w-0 flex-col gap-1">
                <label className="text-xs text-kumo-subtle">{label}</label>
                <Input
                  size="sm"
                  className="w-full font-mono"
                  aria-label={label}
                  placeholder="留空则不发送该头"
                  value={session[key] ?? ''}
                  onChange={e => update({ session: { ...session, [key]: e.target.value } }, true)}
                  disabled={saving}
                />
              </div>
            ))}
          </div>
        </SectionCard>

        <SectionCard
          title="模型"
          icon={<Layers className="h-4 w-4 text-brand" />}
          bodyPadding="none"
        >
          {models.length ? (
            <div className="overflow-x-auto">
              <AppTable tableId="opencode-models" columns={OPENCODE_MODEL_COLUMNS} className="w-full min-w-[36rem] text-xs">
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
                    <Table.Head className="!px-2 !py-1.5 text-center">能力</Table.Head>
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
                            title={m.enabled ? '停用：写入网关端点停用名单（网关不再路由该模型），直连中继也会被拒' : '启用：从网关端点停用名单移除，恢复路由'}
                            aria-label={`${m.enabled ? '停用' : '启用'} ${m.id}`}
                          />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-2.5 !py-1.5">
                        <div className="truncate font-mono text-kumo-strong" title={m.id}>
                          {m.id}
                        </div>
                        {m.description ? <div className="truncate text-kumo-subtle">{m.description}</div> : null}
                      </Table.Cell>
                      <Table.Cell className="!px-2 !py-1.5 text-center">
                        <div className="flex items-center justify-center gap-1 whitespace-nowrap">
                          {m.supportsReasoning ? <Badge variant="purple" className="text-xs" title="原生推理模型，走 /zen/v1/responses">推理</Badge> : null}
                          {m.supportsToolCall ? <Badge variant="outline" className="text-xs" title="支持函数调用（免费层门控会自动注入 read/shell）">工具</Badge> : null}
                          {m.supportsImages ? <Badge variant="outline" className="text-xs" title="支持图片输入">多模态</Badge> : null}
                        </div>
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
                description={modelsReady ? '插件未返回可服务的模型，请点右上角刷新重试。' : '模型目录拉取失败，请点右上角刷新重试。'}
              />
            </div>
          )}
        </SectionCard>
      </div>
    </div>
  );
}