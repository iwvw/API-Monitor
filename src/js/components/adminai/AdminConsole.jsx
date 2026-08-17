import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Select } from '@cloudflare/kumo/components/select';
import { Empty, Loader, Tabs } from '@cloudflare/kumo';
import { SectionCard, FieldRow } from '../ui/AppPrimitives.jsx';
import { MessageSquare, Plus, Play, Send, Settings, Trash, X, Bot, ShieldCheck, Sliders, Database, Brain, Search, Edit, ArrowLeft } from '../Icons.jsx';

/* ==================== 通用小组件 ==================== */

function ErrorBanner({ message }) {
  if (!message) return null;
  return (
    <div className="rounded-lg bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">{message}</div>
  );
}

/* ==================== 设置页（多键表单） ==================== */

const SETTING_FIELDS = [
  { key: 'admin_ai_enabled', kind: 'switch', group: 'basic', label: '管理 AI 总开关', description: '关闭后侧栏与 Telegram 不再受理对话' },
  { key: 'admin_ai_default_model', kind: 'select', group: 'basic', label: '默认推理模型', description: '模型来源：模型网关' },
  { key: 'admin_ai_briefing_model', kind: 'select', group: 'basic', label: '站点简报模型', description: '每日站点简报专用模型，留空回退默认模型' },
  { key: 'admin_ai_write_enabled', kind: 'switch', group: 'security', label: '写操作全局开关', description: '写操作需人工审批' },
  { key: 'admin_ai_auto_approve', kind: 'switch', group: 'security', label: '完全批准模式', description: '（危险：AI 可自主执行任何写操作）' },
  { key: 'admin_ai_tool_call_limit', kind: 'number', group: 'runtime', label: '工具调用上限', description: '单轮最多调用次数' },
  { key: 'admin_ai_timeout_seconds', kind: 'number', group: 'runtime', label: '执行超时（秒）' },
  { key: 'admin_ai_context_window', kind: 'number', group: 'runtime', label: '上下文窗口（token）' },
  { key: 'admin_ai_memories_enabled', kind: 'switch', group: 'runtime', label: '长期记忆总开关', description: '记忆检索/记录工具' },
  { key: 'admin_ai_memories_bootstrap_chars', kind: 'number', group: 'runtime', label: '记忆注入上限（字符）', description: '长期记忆字符上限' },
  { key: 'admin_ai_memories_auto_capture', kind: 'switch', group: 'runtime', label: '自动记忆提炼', description: '空闲后自动提炼' },
  { key: 'admin_ai_memories_idle_minutes', kind: 'number', group: 'runtime', label: '提炼空闲分钟数', description: '会话闲置多少分钟后触发自动提炼' },
  { key: 'admin_ai_audit_retention_days', kind: 'number', group: 'retention', label: '审计保留天数', description: '审计记录保留天数' },
];

const SETTING_SECTIONS = [
  { key: 'basic', title: '基础设置', description: '总开关与模型选择', icon: <Bot className="h-4 w-4 text-kumo-brand" /> },
  { key: 'security', title: '安全与审批', description: '写操作与审批策略', icon: <ShieldCheck className="h-4 w-4 text-kumo-brand" /> },
  { key: 'runtime', title: '运行参数', description: '工具调用上限与超时', icon: <Sliders className="h-4 w-4 text-kumo-brand" /> },
  { key: 'retention', title: '数据保留', description: '审计记录保留时长', icon: <Database className="h-4 w-4 text-kumo-brand" /> },
];

// 站点简报模板清单（与后端 briefingTemplatePrompts 保持一致）。
const BRIEFING_TEMPLATE_OPTIONS = [
  { value: 'standard', label: '标准简报', description: '标题 + 关键指标小节（系统资源 / API 调用 / 可用性），突出异常与风险，全文 ≤ 400 字' },
  { value: 'brief', label: '简洁版', description: '一句话结论 + 关键指标与异常项目符号，全文 ≤ 150 字' },
  { value: 'detailed', label: '详细版', description: '摘要 / 分节指标 / 风险建议，全文 ≤ 800 字' },
  { value: 'alert_only', label: '仅异常', description: '只报告异常与风险（按严重度排序）；一切正常时仅输出一句"一切正常"' },
  { value: 'custom', label: '自定义', description: '自行编写简报格式要求，内容将直接作为格式指令注入' },
];

/* 设置表单状态（吸底保存栏与设置卡共用，确保保存动作始终可达） */
function useSettingsForm() {
  const [values, setValues] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);
  const [modelOptions, setModelOptions] = useState([]);

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/admin-ai/settings');
        const data = await res.json();
        const body = data.data || data;
        setValues(body.settings || {});
      } catch {
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  // 模型下拉选项：模型网关端点（enabled）的模型合并；值为纯模型名（/v1 按模型名路由）
  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/openai/endpoints');
        const data = await res.json();
        const eps = Array.isArray(data) ? data : (data.data || []);
        const seen = new Set();
        const options = [];
        for (const ep of eps) {
          if (!ep.enabled || !Array.isArray(ep.models)) continue;
          for (const m of ep.models) {
            if (seen.has(m)) continue;
            seen.add(m);
            options.push({ value: m, label: `${ep.name || ep.id} / ${m}` });
          }
        }
        options.sort((a, b) => a.label.localeCompare(b.label));
        setModelOptions(options);
      } catch {
        setModelOptions([]);
      }
    })();
  }, []);

  useEffect(() => {
    if (!savedAt) return undefined;
    const timer = window.setTimeout(() => setSavedAt(0), 2500);
    return () => window.clearTimeout(timer);
  }, [savedAt]);

  const setField = (key, value) => setValues((prev) => ({ ...prev, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values || {}),
      });
      const data = await res.json();
      if ((data.data || data).ok) setSavedAt(Date.now());
    } catch {
    } finally {
      setSaving(false);
    }
  };

  return { values, loading, saving, savedAt, modelOptions, setField, save };
}

function SettingsCard({ form }) {
  const { values, loading, modelOptions, setField } = form;

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  const renderField = (field) => {
    const value = (values && values[field.key]) || (field.kind === 'switch' ? 'false' : '');
    if (field.kind === 'switch') {
      return (
        <FieldRow key={field.key} title={field.label} description={field.description}>
          <Switch
            checked={value === 'true'}
            onCheckedChange={(checked) => setField(field.key, checked ? 'true' : 'false')}
          />
        </FieldRow>
      );
    }
    let control;
    if (field.kind === 'select') {
      control = (
        <Select
          placeholder={modelOptions.length ? '选择模型' : '模型网关无可用模型'}
          value={value || undefined}
          onValueChange={(v) => setField(field.key, String(v))}
          items={modelOptions}
          size="sm"
          className="w-full"
        />
      );
    } else {
      control = (
        <Input
          size="sm"
          className={field.kind === 'number' ? 'w-24' : 'w-full'}
          type={field.kind === 'number' ? 'number' : 'text'}
          placeholder={field.placeholder}
          aria-label={field.label}
          value={value}
          onChange={(e) => setField(field.key, e.target.value)}
        />
      );
    }
    return (
      <FieldRow key={field.key} title={field.label} description={field.description}>
        {control}
      </FieldRow>
    );
  };

  return (
    <div className="space-y-4">
      {SETTING_SECTIONS.map((section) => (
        <SectionCard key={section.key} icon={section.icon} title={section.title} description={section.description} bodyPadding="none">
          {SETTING_FIELDS.filter((field) => field.group === section.key).map(renderField)}
        </SectionCard>
      ))}
    </div>
  );
}

/* ==================== 频道页（Telegram 频道 + 白名单） ==================== */

const EMPTY_FORM = {
  id: '',
  name: '',
  notificationChannelId: '',
};

function ChannelsCard() {
  const [channels, setChannels] = useState([]);
  const [bindings, setBindings] = useState([]);
  const [notificationOptions, setNotificationOptions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState(EMPTY_FORM);
  const [confirmDelete, setConfirmDelete] = useState('');
  // 白名单并入频道编辑：新增成员输入（编辑频道表单内即时生效）。
  const [bindInput, setBindInput] = useState({ userId: '', username: '' });

  const load = useCallback(async () => {
    try {
      const [chRes, bdRes, ntRes] = await Promise.all([
        fetch('/api/admin-ai/channels'),
        fetch('/api/admin-ai/channel-bindings'),
        fetch('/api/notification/channels'),
      ]);
      const chData = await chRes.json();
      const bdData = await bdRes.json();
      const ntData = await ntRes.json();
      setChannels((chData.data || chData).channels || []);
      setBindings((bdData.data || bdData).bindings || []);
      const ntChannels = (ntData.data || ntData) || [];
      setNotificationOptions(
        ntChannels
          .filter((c) => c.type === 'telegram' && c.enabled)
          .map((c) => ({ value: c.id, label: c.name || c.id }))
      );
    } catch {
      setError('频道数据加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const openCreate = () => {
    setForm(EMPTY_FORM);
    setError('');
    setFormOpen(true);
  };

  const openEdit = (channel) => {
    setForm({
      id: channel.id,
      name: channel.name,
      notificationChannelId: channel.notificationChannelId || '',
    });
    setError('');
    setFormOpen(true);
  };

  const setFormField = (field, value) => setForm((prev) => ({ ...prev, [field]: value }));

  const saveChannel = async () => {
    if (!form.name.trim()) {
      setError('请填写频道名称');
      return;
    }
    if (!form.id && !form.notificationChannelId) {
      setError('请选择来源通知渠道（bot token 复用通知中心配置）');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const url = form.id ? `/api/admin-ai/channels/${form.id}` : '/api/admin-ai/channels';
      const payload = form.id
        ? { name: form.name.trim(), notificationChannelId: form.notificationChannelId }
        : { type: 'telegram', name: form.name.trim(), enabled: true, notificationChannelId: form.notificationChannelId };
      const res = await fetch(url, {
        method: form.id ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      const body = data.data || data;
      if (!res.ok || body.ok === false) {
        setError((data.error || {}).message || '保存失败');
        return;
      }
      setFormOpen(false);
      load();
      if (!body.started && body.startError) {
        setError(`频道已创建，但自动启动失败：${body.startError}`);
      }
    } catch {
      setError('保存失败，请重试');
    } finally {
      setSaving(false);
    }
  };

  const toggleEnabled = async (channel) => {
    try {
      await fetch(`/api/admin-ai/channels/${channel.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !channel.enabled }),
      });
      load();
    } catch {
      setError('更新失败');
    }
  };

  const runAction = async (channel, action) => {
    try {
      const res = await fetch(`/api/admin-ai/channels/${channel.id}/${action}`, { method: 'POST' });
      const data = await res.json();
      if (!res.ok || (data.data || data).ok === false) {
        setError(((data.data || data).error) || '操作失败');
      }
      load();
    } catch {
      setError('操作失败');
    }
  };

  const deleteChannel = async (channel) => {
    if (confirmDelete !== channel.id) {
      setConfirmDelete(channel.id);
      return;
    }
    setConfirmDelete('');
    try {
      await fetch(`/api/admin-ai/channels/${channel.id}`, { method: 'DELETE' });
      load();
    } catch {
      setError('删除失败');
    }
  };

  const addBinding = async () => {
    const userId = bindInput.userId.trim();
    if (!form.id || !userId) {
      setError('请填写 Telegram 用户 ID');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/admin-ai/channel-bindings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          channelId: form.id,
          channelUserId: userId,
          username: bindInput.username.trim(),
        }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).ok === false) {
        setError(((data.data || data).error) || '添加失败');
        return;
      }
      setBindInput({ userId: '', username: '' });
      load();
    } catch {
      setError('添加失败');
    } finally {
      setSaving(false);
    }
  };

  const deleteBinding = async (binding) => {
    try {
      await fetch(`/api/admin-ai/channel-bindings/${binding.id}`, { method: 'DELETE' });
      load();
    } catch {
      setError('删除绑定失败');
    }
  };

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  return (
    <div className="space-y-4 pb-4">
      <ErrorBanner message={error} />

      {/* ---- 频道卡片 ---- */}
      <SectionCard
        title="频道"
        icon={<Send className="h-4 w-4 text-kumo-brand" />}
        actions={!formOpen && (
          <Button size="sm" variant="secondary" onClick={openCreate}>
            <Plus className="h-3.5 w-3.5" /> 新建频道
          </Button>
        )}
        bodyPadding="none"
      >
      {channels.length === 0 && !formOpen ? (
        <Empty className="py-10" title="暂无频道" description="点击「新建频道」接入 Telegram Bot" />
      ) : (
        <div className="divide-y divide-kumo-line">
          {channels.map((channel) => (
            <div
              key={channel.id}
              className="flex flex-col gap-3 px-4 py-3.5 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between"
            >
              <div className="flex min-w-0 items-center gap-3">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                  <Send className="h-4 w-4" />
                </span>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-semibold text-kumo-strong">{channel.name}</span>
                    <span className="truncate text-xs text-kumo-subtle">
                      来源：{channel.notificationChannelName || '旧 Token 配置（未选择来源）'}
                    </span>
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-2">
                    <Badge variant="secondary">telegram</Badge>
                    <Badge variant={`${bindings.some((b) => b.channelId === channel.id) ? 'secondary' : 'warning'}`}>
                      {bindings.some((b) => b.channelId === channel.id)
                        ? `白名单 ${bindings.filter((b) => b.channelId === channel.id).length} 人`
                        : '开放'}
                    </Badge>
                    {channel.status === 'running' ? (
                      <Badge variant="success">运行中</Badge>
                    ) : (
                      <Badge variant="secondary">已停止</Badge>
                    )}
                  </div>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Switch checked={channel.enabled} onCheckedChange={() => toggleEnabled(channel)} aria-label="启用频道" />
                {channel.status === 'running' ? (
                  <Button size="sm" variant="secondary" onClick={() => runAction(channel, 'stop')}>
                    停止
                  </Button>
                ) : (
                  <Button size="sm" variant="secondary" onClick={() => runAction(channel, 'start')} disabled={!channel.enabled}>
                    <Play className="h-3 w-3" /> 启动
                  </Button>
                )}
                <Button size="sm" variant="secondary" onClick={() => openEdit(channel)}>
                  编辑
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  className={confirmDelete === channel.id ? '!text-kumo-danger' : ''}
                  onClick={() => deleteChannel(channel)}
                >
                  <Trash className="h-3 w-3" />
                  {confirmDelete === channel.id ? '确认删除' : ''}
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
      </SectionCard>

      {/* ---- 新建/编辑频道表单 ---- */}
      {formOpen && (
        <div className="rounded-xl border border-kumo-line bg-kumo-recessed p-4">
          <div className="mb-3 flex items-center justify-between">
            <span className="text-sm font-semibold text-kumo-strong">
              {form.id ? '编辑频道' : '新建频道'}
            </span>
            <Button size="sm" variant="ghost" onClick={() => { setFormOpen(false); setError(''); }}>
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
          <div className="grid gap-3 cq-sm:grid-cols-2">
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">名称</div>
              <Input size="sm" className="w-full" placeholder="如：Telegram 主机器人" aria-label="频道名称" value={form.name} onChange={(e) => setFormField('name', e.target.value)} />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">来源通知渠道</div>
              <Select
                size="sm"
                className="w-full"
                placeholder={form.notificationChannelId ? undefined : (form.id ? '未选择（沿用旧 Token 配置）' : '选择通知中心的 Telegram 渠道')}
                value={form.notificationChannelId}
                onValueChange={(v) => setFormField('notificationChannelId', String(v))}
                items={notificationOptions.length ? notificationOptions : [{ value: '__none__', label: '暂无可用通知渠道（请先到通知中心配置 Telegram 渠道）' }]}
                disabled={notificationOptions.length === 0}
              />
            </div>
          </div>
          <p className="mt-3 text-xs leading-5 text-kumo-subtle">
            bot token 与推送目标均复用通知中心已配置的 Telegram 渠道（需含 bot_token 与 chat_id），无需在此填写；同一渠道只能被一个 AI 频道引用。
          </p>

          {/* 白名单管理（并入频道编辑设置）：仅编辑已有频道时显示 */}
          {form.id && (
            <div className="mt-4 rounded-lg border border-kumo-line bg-kumo-base p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-xs font-semibold text-kumo-strong">白名单成员</span>
                <span className="text-[11px] text-kumo-subtle">留空 = 任何人可对话</span>
              </div>
              {bindings.filter((b) => b.channelId === form.id).length === 0 ? (
                <p className="text-xs text-kumo-subtle">当前开放，任何人可对话（加入成员后仅列表内用户可对话）</p>
              ) : (
                <div className="space-y-1.5">
                  {bindings.filter((b) => b.channelId === form.id).map((binding) => (
                    <div
                      key={binding.id}
                      className="flex items-center justify-between gap-2 rounded-md border border-kumo-line bg-kumo-base/60 px-2.5 py-1.5"
                    >
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="truncate font-mono text-xs font-medium text-kumo-strong">{binding.channelUserId}</span>
                        {binding.username && <span className="truncate text-xs text-kumo-subtle">{binding.username}</span>}
                      </div>
                      <Button size="sm" variant="secondary" onClick={() => deleteBinding(binding)} aria-label="移除白名单成员">
                        <X className="h-3 w-3" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
              <div className="mt-2.5 flex flex-wrap items-center gap-2">
                <Input
                  size="sm"
                  className="w-40"
                  placeholder="Telegram 用户 ID *"
                  aria-label="Telegram 用户 ID"
                  value={bindInput.userId}
                  onChange={(e) => setBindInput((prev) => ({ ...prev, userId: e.target.value }))}
                />
                <Input
                  size="sm"
                  className="w-36"
                  placeholder="@username（可选）"
                  aria-label="Telegram 用户名"
                  value={bindInput.username}
                  onChange={(e) => setBindInput((prev) => ({ ...prev, username: e.target.value }))}
                />
                <Button size="sm" variant="secondary" onClick={addBinding} disabled={saving || !bindInput.userId.trim()}>
                  <Plus className="h-3 w-3" /> 添加成员
                </Button>
              </div>
            </div>
          )}

          <div className="mt-4 flex justify-end gap-2">
            <Button size="sm" variant="ghost" onClick={() => { setFormOpen(false); setError(''); }}>
              取消
            </Button>
            <Button size="sm" variant="primary" onClick={saveChannel} disabled={saving}>
              {saving ? '保存中...' : form.id ? '保存修改' : '创建频道'}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

/* ==================== 模板页（站点简报格式模板） ==================== */

function TemplatesCard() {
  const [cfg, setCfg] = useState({ type: 'standard', custom: '' });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/admin-ai/settings');
        const data = await res.json();
        const body = data.data || data;
        const raw = (body.settings || {})['admin_ai_briefing_template'];
        if (raw) {
          try {
            setCfg((prev) => ({ ...prev, ...JSON.parse(raw) }));
          } catch {
          }
        }
      } catch {
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  useEffect(() => {
    if (!savedAt) return undefined;
    const timer = window.setTimeout(() => setSavedAt(0), 2500);
    return () => window.clearTimeout(timer);
  }, [savedAt]);

  const option = BRIEFING_TEMPLATE_OPTIONS.find((o) => o.value === cfg.type) || BRIEFING_TEMPLATE_OPTIONS[0];

  const handleSave = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ admin_ai_briefing_template: JSON.stringify(cfg) }),
      });
      const data = await res.json();
      if ((data.data || data).ok) setSavedAt(Date.now());
    } catch {
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  return (
    <div className="space-y-4 pb-4">
      <SectionCard
        icon={<MessageSquare className="h-4 w-4 text-kumo-brand" />}
        title="站点简报模板"
        description="/briefing"
        bodyPadding="none"
      >
        <FieldRow title="模板类型" description={option.description}>
          <Select
            size="sm"
            className="w-full"
            value={cfg.type}
            onValueChange={(v) => setCfg((prev) => ({ ...prev, type: v }))}
            items={BRIEFING_TEMPLATE_OPTIONS.map((o) => ({ value: o.value, label: o.label }))}
          />
        </FieldRow>
        {cfg.type === 'custom' && (
          <div className="border-b border-kumo-line px-4 py-3">
            <Textarea
              rows={3}
              value={cfg.custom}
              onChange={(e) => setCfg((prev) => ({ ...prev, custom: e.target.value }))}
              placeholder="编写简报格式要求，如：使用表格呈现所有指标，先异常后正常；结尾附明日关注事项…"
              className="w-full"
            />
          </div>
        )}
        <div className="flex items-center gap-3 px-4 py-3">
          <Button size="sm" variant="primary" onClick={handleSave} disabled={saving}>
            {saving ? '保存中...' : '保存'}
          </Button>
          {savedAt ? <span className="text-xs text-kumo-success">已保存 ✓</span> : null}
        </div>
      </SectionCard>
    </div>
  );
}

/* ==================== 长期记忆管理 ==================== */

const IMPORTANCE_OPTIONS = Array.from({ length: 10 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }));

function MemoriesCard() {
  const [items, setItems] = useState(null);
  const [loading, setLoading] = useState(true);
  const [q, setQ] = useState('');
  const [adding, setAdding] = useState(false);
  const [newContent, setNewContent] = useState('');
  const [newImportance, setNewImportance] = useState('5');
  const [newTriggers, setNewTriggers] = useState('');
  const [editingId, setEditingId] = useState('');
  const [editContent, setEditContent] = useState('');
  const [editImportance, setEditImportance] = useState('5');
  const [editTriggers, setEditTriggers] = useState('');
  const [confirmDelete, setConfirmDelete] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const memHoverRef = useRef(false);

  const load = useCallback(async (keyword = '') => {
    setLoading(true);
    try {
      const res = await fetch(`/api/admin-ai/memories?q=${encodeURIComponent(keyword)}`);
      const data = await res.json();
      const body = data.data || data;
      setItems(Array.isArray(body.items) ? body.items : []);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load('');
  }, [load]);

  // 搜索防抖：停止输入 250ms 后触发检索，避免每击键一次请求
  useEffect(() => {
    const t = window.setTimeout(() => load(q.trim()), 250);
    return () => window.clearTimeout(t);
  }, [q, load]);

  const handleAdd = async () => {
    const content = newContent.trim();
    if (!content) return;
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/admin-ai/memories', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content, importance: Number(newImportance), triggers: newTriggers.trim() }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).error) {
        setError((data.data || data).error || '保存失败');
        return;
      }
      setNewContent('');
      setNewTriggers('');
      setNewImportance('5');
      setAdding(false);
      await load(q);
    } catch {
      setError('保存失败');
    } finally {
      setSaving(false);
    }
  };

  const startEdit = (item) => {
    setEditingId(item.id);
    setEditContent(item.content);
    setEditImportance(String(item.importance || 5));
    setEditTriggers(item.triggers || '');
  };

  const saveEdit = async (id) => {
    const content = editContent.trim();
    if (!content) return;
    setSaving(true);
    setError('');
    try {
      const res = await fetch(`/api/admin-ai/memories/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content, importance: Number(editImportance), triggers: editTriggers.trim() }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).error) {
        setError((data.data || data).error || '保存失败');
        return;
      }
      setEditingId('');
      await load(q);
    } catch {
      setError('保存失败');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (item) => {
    if (confirmDelete !== item.id) {
      setConfirmDelete(item.id);
      window.setTimeout(() => setConfirmDelete((cur) => (cur === item.id ? '' : cur)), 2500);
      return;
    }
    setConfirmDelete('');
    try {
      await fetch(`/api/admin-ai/memories/${item.id}`, { method: 'DELETE' });
      await load(q);
    } catch {
    }
  };

  const MEM_COLLAPSED_H = 48;

  const handleMemEnter = (e) => {
    memHoverRef.current = true;
    const wrap = e.currentTarget.querySelector('[data-mem-wrap]');
    const inner = e.currentTarget.querySelector('[data-mem-content]');
    if (!wrap || !inner) return;
    inner.classList.remove('line-clamp-2');
    const full = wrap.scrollHeight;
    if (full <= MEM_COLLAPSED_H) {
      inner.classList.add('line-clamp-2');
      return;
    }
    wrap.style.maxHeight = `${full}px`;
  };

  const handleMemLeave = (e) => {
    memHoverRef.current = false;
    const wrap = e.currentTarget.querySelector('[data-mem-wrap]');
    const inner = e.currentTarget.querySelector('[data-mem-content]');
    if (!wrap || !inner) return;
    wrap.style.maxHeight = `${MEM_COLLAPSED_H}px`;
    const onEnd = (ev) => {
      if (ev.propertyName !== 'max-height') return;
      wrap.removeEventListener('transitionend', onEnd);
      if (memHoverRef.current) return;
      inner.classList.add('line-clamp-2');
    };
    wrap.addEventListener('transitionend', onEnd);
  };

  return (
    <div className="space-y-4 pb-4">
      <SectionCard
        icon={<Brain className="h-4 w-4 text-kumo-brand" />}
        title="长期记忆"
        description="在对话中说「记住…」让 AI 写入"
        bodyPadding="none"
      >
        <div className="flex items-center gap-2 border-b border-kumo-line px-4 py-3">
          <div className="relative flex-1">
            <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-kumo-subtle" />
            <Input
              size="sm"
              className="w-full pl-8"
              placeholder="搜索记忆"
              aria-label="搜索记忆"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <Button size="sm" onClick={() => setAdding((v) => !v)}>
            <Plus className="mr-1 h-3.5 w-3.5" />
            新增
          </Button>
        </div>

        {error && (
          <div className="border-b border-kumo-line px-4 py-3">
            <ErrorBanner message={error} />
          </div>
        )}

        {adding && (
          <div className="border-b border-kumo-line px-4 py-3">
            <div className="space-y-2.5 rounded-lg border border-kumo-line bg-kumo-recessed/40 p-3.5">
              <Textarea
                rows={2}
                value={newContent}
                onChange={(e) => setNewContent(e.target.value)}
                placeholder="记忆内容，一句话表述，具体到名称/ID/取值"
                className="w-full"
              />
              <div className="flex flex-wrap items-center gap-2">
                <Select
                  size="sm"
                  className="w-24"
                  value={newImportance}
                  onValueChange={setNewImportance}
                  items={IMPORTANCE_OPTIONS}
                />
                <Input
                  size="sm"
                  className="min-w-40 flex-1"
                  value={newTriggers}
                  aria-label="触发词"
                  onChange={(e) => setNewTriggers(e.target.value)}
                  placeholder="触发词（逗号分隔，选填）"
                />
                <div className="ml-auto flex items-center gap-2">
                  <Button size="sm" variant="primary" onClick={handleAdd} disabled={saving || !newContent.trim()}>
                    {saving ? '保存中...' : '保存'}
                  </Button>
                  <Button size="sm" onClick={() => setAdding(false)}>
                    <X className="mr-1 h-3.5 w-3.5" />
                    取消
                  </Button>
                </div>
              </div>
            </div>
          </div>
        )}

        {loading && items === null ? (
          <div className="flex justify-center py-10">
            <Loader size={20} className="text-kumo-subtle" />
          </div>
        ) : items.length === 0 ? (
          <Empty
            className="py-10"
            icon={<Brain className="h-5 w-5" />}
            title={q ? '没有匹配的记忆' : '还没有长期记忆'}
            description={q ? '换个关键词试试' : '在对话中说「记住…」，或点「新增」记录一条'}
          />
        ) : (
          <div>
            {items.map((item) => (
              <div
                key={item.id}
                className="group border-b border-kumo-line px-4 py-3 last:border-b-0"
                onMouseEnter={editingId === item.id ? undefined : handleMemEnter}
                onMouseLeave={editingId === item.id ? undefined : handleMemLeave}
              >
                {editingId === item.id ? (
                  <div className="space-y-2.5">
                    <Textarea
                      rows={2}
                      value={editContent}
                      onChange={(e) => setEditContent(e.target.value)}
                      className="w-full"
                    />
                    <div className="flex flex-wrap items-center gap-2">
                      <Select
                        size="sm"
                        className="w-24"
                        value={editImportance}
                        onValueChange={setEditImportance}
                        items={IMPORTANCE_OPTIONS}
                      />
                      <Input
                        size="sm"
                        className="min-w-40 flex-1"
                        value={editTriggers}
                        aria-label="触发词"
                        onChange={(e) => setEditTriggers(e.target.value)}
                        placeholder="触发词（逗号分隔，选填）"
                      />
                      <div className="ml-auto flex items-center gap-2">
                        <Button size="sm" variant="primary" onClick={() => saveEdit(item.id)} disabled={saving || !editContent.trim()}>
                          {saving ? '保存中...' : '保存'}
                        </Button>
                        <Button size="sm" onClick={() => setEditingId('')}>
                          <X className="mr-1 h-3.5 w-3.5" />
                          取消
                        </Button>
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center gap-2">
                    <div className="min-w-0 flex-1">
                      <div className="mb-1 flex flex-wrap items-center gap-2 text-xs">
                        <Badge variant={item.importance >= 8 ? 'red' : 'neutral'}>{item.importance}</Badge>
                        <Badge variant="blue">{new Date(item.createdAt).toLocaleDateString()}</Badge>
                        <Badge variant={item.source === 'agent' ? 'teal' : 'orange'}>
                          {item.source === 'agent' ? '自动' : '手动'}
                        </Badge>
                      </div>
                      <div
                        data-mem-wrap
                        className="max-h-12 overflow-hidden transition-[max-height] duration-300 ease-out"
                      >
                        <div data-mem-content className="line-clamp-2 text-sm leading-relaxed text-kumo-strong">
                          {item.content}
                        </div>
                      </div>
                      {item.triggers && (
                        <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
                          <Badge variant="outline" className="gap-1.5">
                            {item.triggers
                              .split(',')
                              .map((t) => t.trim())
                              .filter(Boolean)
                              .map((t, i) => (
                                <React.Fragment key={i}>
                                  {i > 0 && <span className="h-3 w-px bg-kumo-line" aria-hidden />}
                                  <span>{t}</span>
                                </React.Fragment>
                              ))}
                          </Badge>
                        </div>
                      )}
                    </div>
                    <div className="flex shrink-0 flex-col items-center justify-center gap-1">
                      <Button size="sm" variant="secondary" className="!px-2" onClick={() => startEdit(item)} title="编辑">
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        className={`!px-2 ${confirmDelete === item.id ? '!text-kumo-danger' : ''}`}
                        onClick={() => handleDelete(item)}
                      >
                        <Trash className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  );
}

/* ==================== 管理面板（主页面与 Ask AI 侧栏共用） ==================== */

export const TAB_OPTIONS = [
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">设置</span>
      </span>
    ),
  },
  {
    value: 'channels',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Send className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">配置</span>
      </span>
    ),
  },
  {
    value: 'templates',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <MessageSquare className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">模板</span>
      </span>
    ),
  },
  {
    value: 'memories',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Brain className="h-3.5 w-3.5" />
        <span className="hidden @[420px]:inline">记忆</span>
      </span>
    ),
  },
];

export default function AdminConsole({ onBack, hideTabs = false, activeTab: controlledTab, onTabChange }) {
  const [internalTab, setInternalTab] = useState('settings');
  const activeTab = controlledTab ?? internalTab;
  const handleTabChange = (v) => {
    if (onTabChange) onTabChange(v);
    else setInternalTab(v);
  };
  const form = useSettingsForm();

  return (
    <div className="flex min-h-full flex-col">
      {!hideTabs && (
        <div className="sticky top-0 z-10 -mx-4 border-b border-kumo-line bg-[var(--app-main-surface)] px-4 pb-3 pt-4">
          <Tabs
            value={activeTab}
            onValueChange={handleTabChange}
            tabs={TAB_OPTIONS}
          />
        </div>
      )}

      <div className={`space-y-4 pb-4 ${hideTabs ? '' : 'mt-4'}`}>
        {activeTab === 'settings' && <SettingsCard form={form} />}
        {activeTab === 'channels' && <ChannelsCard />}
        {activeTab === 'templates' && <TemplatesCard />}
        {activeTab === 'memories' && <MemoriesCard />}
      </div>

      {/* 吸底栏：mt-auto 让内容不满一屏时仍贴住底边；滚动时 sticky 保持可见 */}
      <div className="sticky bottom-0 z-10 mt-auto -mx-4 flex h-12 shrink-0 items-center justify-end gap-3 border-t border-kumo-line bg-[var(--app-main-surface)] px-4">
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={onBack}
          className="flex h-7 items-center gap-1.5 rounded-md px-1 text-xs"
          aria-label="返回对话"
        >
          <ArrowLeft className="h-3.5 w-3.5" /> 对话
        </Button>
        {activeTab === 'settings' && form.savedAt ? <span className="text-xs text-kumo-success">已保存 ✓</span> : null}
        {activeTab === 'settings' && (
          <Button size="sm" variant="primary" onClick={form.save} disabled={form.saving || !form.values}>
            {form.saving ? '保存中...' : '保存'}
          </Button>
        )}
      </div>
    </div>
  );
}
