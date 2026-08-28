import { useEffect, useMemo, useState } from 'react';
import { Button, Switch, Select, Loader } from '@cloudflare/kumo';
import { AppCard, EmptyState, PageStack } from '../../components/ui/AppPrimitives.jsx';
import { toast } from '../../modules/toast.js';
import { getAuthHeaders } from './utils.js';

const BETA_API = '/api/openaibeta';

// BetaPluginPanel：模型网关「Beta 插件」自包含面板。
// 管理内嵌 Gemini 免费中继的启用开关、出口代理来源、模型启用与健康测试。
// 所有请求只走 /api/openaibeta 前缀，删除本组件即移除全部前端关联。
export function BetaPluginPanel() {
  const [settings, setSettings] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState(null);
  const [endpoints, setEndpoints] = useState([]);
  const [modelToggleLoading, setModelToggleLoading] = useState(false);

  const load = async () => {
    try {
      const res = await fetch(`${BETA_API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
    } catch (e) {
      toast.error(`Beta 插件设置加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  // 出口代理来源：复用模型网关端点代理池（下拉列出已配置代理池的端点）。
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch('/api/openai/endpoints', { headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok) throw new Error(data?.error || '加载端点失败');
        const list = Array.isArray(data) ? data : data?.endpoints || data?.data || [];
        const withPool = list.filter(ep => Array.isArray(ep?.proxyPool) && ep.proxyPool.length > 0);
        if (!cancelled) setEndpoints(withPool);
      } catch {
        if (!cancelled) setEndpoints([]);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  const update = (patch, silent = false) => {
    const next = { ...(settings || {}), ...patch };
    setSettings(next);
    if (!silent) void save(next);
  };

  const save = async next => {
    if (!next) return;
    setSaving(true);
    try {
      const res = await fetch(`${BETA_API}/settings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(next),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      setSettings(data.settings);
      toast.success('设置已保存');
    } catch (e) {
      toast.error(`保存失败：${e.message}`);
    } finally {
      setSaving(false);
    }
  };

  const toggleModel = async (id, enabled) => {
    if (!settings) return;
    setModelToggleLoading(true);
    const nextModels = (settings.models || []).map(m => (m.id === id ? { ...m, enabled } : m));
    const next = { ...settings, models: nextModels };
    setSettings(next);
    try {
      const res = await fetch(`${BETA_API}/models`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ models: nextModels, aliasMap: settings.aliasMap || {} }),
      });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '保存失败');
      toast.success(`${enabled ? '启用' : '停用'} ${id}`);
    } catch (e) {
      toast.error(`模型开关失败：${e.message}`);
      setSettings(prev => ({ ...prev, models: settings.models }));
    } finally {
      setModelToggleLoading(false);
    }
  };

  const runTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await fetch(`${BETA_API}/test`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({}),
      });
      const data = await res.json();
      setTestResult({ ok: res.ok && data?.success, ...data });
    } catch (e) {
      setTestResult({ ok: false, error: e.message });
    } finally {
      setTesting(false);
    }
  };

  const enabledModels = useMemo(() => (settings?.models || []).filter(m => m.enabled).length, [settings]);

  if (loading) {
    return (
      <PageStack viewport>
        <div className="flex h-full items-center justify-center">
          <Loader size="lg" />
        </div>
      </PageStack>
    );
  }

  return (
    <PageStack viewport>
      <div className="flex min-h-0 flex-1 flex-col gap-4">
        <AppCard title="Beta 插件" description="内嵌免费 Gemini 中继（OpenAI 兼容）。属于测试性子插件，可随时移除。">
          <div className="flex flex-wrap items-center gap-4">
            <label className="flex items-center gap-2 text-sm text-kumo-strong">
              <Switch checked={!!settings?.enabled} onCheckedChange={v => update({ enabled: v })} />
              启用中继
            </label>
            <span className="text-xs text-kumo-subtle">
              已启用模型 {enabledModels} / {(settings?.models || []).length}
            </span>
            <div className="ml-auto">
              <Button size="sm" variant="secondary" disabled={saving} onClick={() => save(settings)}>
                {saving ? '保存中...' : '保存设置'}
              </Button>
            </div>
          </div>
        </AppCard>

        <AppCard title="出口代理" description="复用模型网关已有端点的代理池作为出网通道（直连 Google 不可达时必配）。">
          <Select
            size="sm"
            className="w-full max-w-md"
            value={settings?.proxyEndpointId || ''}
            onValueChange={v => update({ proxyEndpointId: v || '' })}
            placeholder="直连（不使用代理）"
          >
            <Select.Option value="">直连（不使用代理）</Select.Option>
            {endpoints.map(ep => (
              <Select.Option key={ep.id} value={ep.id}>
                {ep.name}（{ep.proxyPool.length} 个代理）
              </Select.Option>
            ))}
          </Select>
          {endpoints.length === 0 && (
            <p className="mt-1 text-xs text-kumo-subtle">
              暂无带代理池的网关端点。可先在「API 端点」页为某端点配置出口代理池，再回来选择。
            </p>
          )}
        </AppCard>

        <AppCard title="模型" description="勾选要对外暴露的 Gemini 模型（对应 OpenAI 接口的 model 字段）。">
          {settings?.models?.length ? (
            <div className="grid grid-cols-1 gap-1.5 cq-md:grid-cols-2">
              {settings.models.map(m => (
                <label
                  key={m.id}
                  className="flex cursor-pointer items-center justify-between gap-2 rounded-md border border-kumo-line px-3 py-2 text-sm text-kumo-strong"
                >
                  <span className="truncate font-mono">{m.id}</span>
                  <Switch
                    checked={!!m.enabled}
                    disabled={modelToggleLoading}
                    onCheckedChange={v => toggleModel(m.id, v)}
                  />
                </label>
              ))}
            </div>
          ) : (
            <EmptyState title="暂无模型" description="设置中缺少模型注册表。" />
          )}
        </AppCard>

        <AppCard title="健康测试" description="发送一条最小请求验证 Google 匿名端点可达性与代理链路。">
          <div className="flex items-center gap-3">
            <Button size="sm" variant="primary" disabled={testing || !settings?.enabled} onClick={runTest}>
              {testing ? '测试中...' : '开始测试'}
            </Button>
            {!settings?.enabled && <span className="text-xs text-kumo-subtle">需先启用中继</span>}
          </div>
          {testResult && (
            <div
              className={`mt-3 rounded-md border px-3 py-2 text-sm ${
                testResult.ok ? 'border-kumo-success/40 text-kumo-strong' : 'border-kumo-danger/40 text-kumo-strong'
              }`}
            >
              {testResult.ok ? (
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-kumo-success">✓ 测试通过</span>
                    <span className="text-xs text-kumo-subtle">
                      {testResult.model} · {testResult.latencyMs}ms
                      {testResult.proxy ? ` · 代理 ${testResult.proxy}` : ' · 直连'}
                    </span>
                  </div>
                  {testResult.text && <p className="whitespace-pre-wrap break-all text-xs text-kumo-subtle">{testResult.text}</p>}
                </div>
              ) : (
                <p className="whitespace-pre-wrap break-all text-xs text-kumo-subtle">
                  {testResult.error || '测试失败'}
                </p>
              )}
            </div>
          )}
        </AppCard>
      </div>
    </PageStack>
  );
}
