import { useEffect, useMemo, useState } from 'react';
import { Button, Switch, Select, Loader, Textarea, Input } from '@cloudflare/kumo';
import { SectionCard, FieldRow, EmptyState } from '../../../components/ui/AppPrimitives.jsx';
import { ArrowLeft, Activity, Bot, Globe, Plug, Rocket, Settings as SettingsIcon } from '../../../components/Icons.jsx';
import { toast } from '../../../modules/toast.js';
import { getAuthHeaders } from '../utils.js';

const BETA_API = '/api/openaibeta';

// Vertex2APIPlugin：模型网关「插件中心」第一张卡片——Vertex to API。
// 内嵌免费 Gemini 中继（OpenAI 兼容）；可复用网关端点代理池/手动代理出网，
// 并可一键接入模型网关端点列表（link），此时 socket 走 loopback 到本模块中继。
export function Vertex2APIPlugin({ onBack }) {
  const [settings, setSettings] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState(null);
  const [endpoints, setEndpoints] = useState([]);
  const [modelToggleLoading, setModelToggleLoading] = useState(false);
  const [linkState, setLinkState] = useState(null);
  const [linkBusy, setLinkBusy] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [manualProxyText, setManualProxyText] = useState('');

  const load = async () => {
    try {
      const res = await fetch(`${BETA_API}/settings`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data?.success) throw new Error(data?.error || '加载失败');
      setSettings(data.settings);
      setManualProxyText((data.settings?.manualProxies || []).join('\n'));
    } catch (e) {
      toast.error(`插件设置加载失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const loadLink = async () => {
    try {
      const res = await fetch(`${BETA_API}/link`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (res.ok) setLinkState(data);
    } catch {
      setLinkState(null);
    }
  };

  useEffect(() => {
    loadLink();
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

        setEndpoints(list.filter(ep => Array.isArray(ep?.proxyPool) && ep.proxyPool.length > 0));
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

  const saveManualProxies = () => {
    update({ manualProxies: manualProxyText.split('\n').map(s => s.trim()).filter(Boolean) });
    toast.success('代理列表已保存');
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

  const linkPlugin = async action => {
    setLinkBusy(true);
    try {
      const res = await fetch(`${BETA_API}/link`, {
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

  const enabledModels = useMemo(() => (settings?.models || []).filter(m => m.enabled).length, [settings]);

  if (loading) {
    return (
      <div className="flex h-full min-w-0 items-center justify-center">
        <Loader size="lg" />
      </div>
    );
  }

  const numberField = (key, label, description, min, max) => (
    <FieldRow title={label} description={description}>
      <Input
        size="sm"
        type="number"
        className="w-24 text-right font-mono"
        value={settings?.[key] ?? ''}
        min={min}
        max={max}
        onChange={e => {
          const v = parseInt(e.target.value, 10);
          update({ [key]: Number.isNaN(v) ? 0 : v });
        }}
      />
    </FieldRow>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 cq-sm:gap-4">
      <div className="flex items-center gap-2">
        <Button size="sm" variant="secondary" onClick={onBack}>
          <ArrowLeft className="h-3.5 w-3.5" /> 返回插件中心
        </Button>
      </div>

      <SectionCard
        title="Vertex to API"
        icon={<Rocket className="h-4 w-4 text-brand" />}
        description="内嵌免费 Gemini 中继（OpenAI 兼容接口）。测试性子插件，可随时移除。"
        bodyPadding="none"
        actions={
          <Button size="sm" variant="primary" disabled={saving} onClick={() => save(settings)}>
            {saving ? '保存中...' : '保存设置'}
          </Button>
        }
      >
        <FieldRow title="启用中继" description="关闭后 /api/openaibeta/v1 中继与网关端点接入都会拒绝服务。">
          <Switch checked={!!settings?.enabled} onCheckedChange={v => update({ enabled: v })} />
        </FieldRow>
        <FieldRow title="对外暴露的模型" description={`已启用 ${enabledModels} / ${(settings?.models || []).length} 个模型，即 OpenAI 接口 model 字段的取值。`}>
          <span className="text-xs text-kumo-subtle">{enabledModels} 个已启用</span>
        </FieldRow>
      </SectionCard>

      <SectionCard
        title="接入模型网关端点列表"
        icon={<Plug className="h-4 w-4 text-brand" />}
        description="把本插件注册为模型网关的一个端点（出现在「API 端点」页）。接入后外部客户端通过网关 /v1 路由到本中继，自动复用网关的密钥、路由、日志与代理池。"
        bodyPadding="none"
      >
        <FieldRow
          title={linkState?.linked ? '已接入' : '未接入'}
          description={linkState?.linked ? '端点地址：' + (linkState?.baseUrl || '') : '接入后端点地址：' + (linkState?.baseUrl || '计算中...')}
        >
          {linkState?.linked ? (
            <Button size="sm" variant="danger" disabled={linkBusy} onClick={() => linkPlugin('unlink')}>
              {linkBusy ? '操作中...' : '从端点列表移除'}
            </Button>
          ) : (
            <Button size="sm" variant="primary" disabled={linkBusy || !settings?.enabled} onClick={() => linkPlugin('link')}>
              {linkBusy ? '接入中...' : '接入网关端点列表'}
            </Button>
          )}
        </FieldRow>
        {!settings?.enabled && (
          <div className="border-t border-kumo-line px-4 py-2.5">
            <span className="text-xs text-kumo-warning">提示：需先启用中继才能接入网关端点。</span>
          </div>
        )}
      </SectionCard>

      <SectionCard
        title="出口代理"
        icon={<Globe className="h-4 w-4 text-brand" />}
        description="出网到 Google 时使用的代理。可复用模型网关已有端点的代理池，或手动填写代理列表（每行一个，支持 http/https/socks5）。"
        bodyPadding="none"
      >
        <FieldRow title="复用网关端点代理池" description="选择后按请求轮询该端点代理池，并尊重其冷却/禁用状态。">
          <Select
            size="sm"
            className="w-full max-w-md"
            value={settings?.proxyEndpointId || ''}
            onValueChange={v => update({ proxyEndpointId: v || '' })}
            placeholder="不使用网关代理池"
          >
            <Select.Option value="">不使用网关代理池</Select.Option>
            {endpoints.map(ep => (
              <Select.Option key={ep.id} value={ep.id}>
                {ep.name}（{ep.proxyPool.length} 个代理）
              </Select.Option>
            ))}
          </Select>
        </FieldRow>
        <FieldRow title="手动代理列表" description="优先级高于网关代理池；留空表示只用上方代理池或直连。">
          <div className="flex min-w-0 max-w-md flex-col gap-2">
            <Textarea
              rows={3}
              className="min-h-16 w-full font-mono text-xs"
              value={manualProxyText}
              onChange={e => setManualProxyText(e.target.value)}
              placeholder={'每行一个，如：\nsocks5://127.0.0.1:1080\nhttp://user:pass@host:port'}
            />
            <Button size="sm" variant="secondary" disabled={saving} onClick={saveManualProxies}>
              保存代理列表
            </Button>
          </div>
        </FieldRow>
      </SectionCard>

      <SectionCard
        title="模型"
        icon={<Bot className="h-4 w-4 text-brand" />}
        description="勾选要对外暴露的 Gemini 模型（对应 OpenAI 接口的 model 字段）。"
        bodyPadding="none"
      >
        <div className="grid gap-1.5 p-4 cq-lg:grid-cols-2">
          {settings?.models?.length ? (
            settings.models.map(m => (
              <label
                key={m.id}
                className="flex cursor-pointer items-center justify-between gap-2 rounded-md border border-kumo-line bg-kumo-base px-3 py-2 text-sm text-kumo-strong"
              >
                <span className="truncate font-mono">{m.id}</span>
                <Switch
                  checked={!!m.enabled}
                  disabled={modelToggleLoading}
                  onCheckedChange={v => toggleModel(m.id, v)}
                />
              </label>
            ))
          ) : (
            <EmptyState title="暂无模型" description="设置中缺少模型注册表。" />
          )}
        </div>
      </SectionCard>

      <SectionCard
        title="高级"
        icon={<SettingsIcon className="h-4 w-4 text-brand" />}
        description="中继运行参数。"
        bodyPadding="none"
        action={
          <Button size="sm" variant="ghost" onClick={() => setAdvancedOpen(o => !o)}>
            {advancedOpen ? '收起高级参数' : '展开高级参数'}
          </Button>
        }
      >
        {advancedOpen && (
          <div>
            {numberField('requestTimeout', '请求超时（秒）', '上游请求总超时。', 1, 1800)}
            {numberField('maxRetries', '最大重试次数', '429/5xx 时重试上限。', 0, 10)}
            {numberField('streamIdleTimeoutSeconds', '流式空闲超时（秒）', '流式包间空闲超时，防呆连接假死。', 1, 300)}
            {numberField('maxN', '最大候选 n', '非流式多候选上限。', 1, 8)}
            <FieldRow title="丢弃 max_tokens" description="转发时移除 max_tokens（部分模型不支持）。">
              <Switch checked={!!settings?.dropMaxTokens} onCheckedChange={v => update({ dropMaxTokens: v })} />
            </FieldRow>
            <FieldRow title="聚合流式" description="先把整段响应聚合为一次输出（对应上游 aggregate_stream）。">
              <Switch checked={!!settings?.aggregateStream} onCheckedChange={v => update({ aggregateStream: v })} />
            </FieldRow>
            <FieldRow title="模型轮次护栏" description="对连续轮次的模型自动补正（3.5+ 系列）。">
              <Switch checked={!!settings?.modelTurnGuardEnabled} onCheckedChange={v => update({ modelTurnGuardEnabled: v })} />
            </FieldRow>
            <FieldRow title="调试日志" description="打开引擎调试日志，便于排障。">
              <Switch checked={!!settings?.debugMode} onCheckedChange={v => update({ debugMode: v })} />
            </FieldRow>
          </div>
        )}
      </SectionCard>

      <SectionCard
        title="健康测试"
        icon={<Activity className="h-4 w-4 text-brand" />}
        description="发送一条最小请求验证 Google 匿名端点可达性与代理链路。"
        bodyPadding="none"
      >
        <div className="flex items-center gap-3 px-4 py-3">
          <Button size="sm" variant="primary" disabled={testing || !settings?.enabled} onClick={runTest}>
            {testing ? '测试中...' : '开始测试'}
          </Button>
          {!settings?.enabled && <span className="text-xs text-kumo-subtle">需先启用中继</span>}
        </div>
        {testResult && (
          <div
            className={`mx-4 mb-3 rounded-md border px-3 py-2 text-sm ${
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
      </SectionCard>
    </div>
  );
}
