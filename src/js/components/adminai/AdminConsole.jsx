import React, { useCallback, useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Select } from '@cloudflare/kumo/components/select';
import { Empty, Loader, Tabs } from '@cloudflare/kumo';
import { MessageSquare, Plus, Play, Send, Settings, Trash, X, Bot, ShieldCheck, Sliders, Database, Users } from '../Icons.jsx';

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
  { key: 'admin_ai_auto_approve', kind: 'switch', group: 'security', label: '完全批准模式', description: '开启后所有写操作免审批直接执行（危险：AI 可自主执行任何写操作）' },
  { key: 'admin_ai_tool_call_limit', kind: 'number', group: 'runtime', label: '工具调用上限', description: '单轮最多调用次数' },
  { key: 'admin_ai_timeout_seconds', kind: 'number', group: 'runtime', label: '执行超时（秒）' },
  { key: 'admin_ai_context_window', kind: 'number', group: 'runtime', label: '上下文窗口（token）' },
  { key: 'admin_ai_audit_retention_days', kind: 'number', group: 'retention', label: '审计保留天数', description: '审计记录保留天数' },
];

const SETTING_SECTIONS = [
  { key: 'basic', title: '基础设置', description: '总开关与模型选择', icon: <Bot className="h-4 w-4" /> },
  { key: 'security', title: '安全与审批', description: '写操作与审批策略', icon: <ShieldCheck className="h-4 w-4" /> },
  { key: 'runtime', title: '运行参数', description: '工具调用上限与超时', icon: <Sliders className="h-4 w-4" /> },
  { key: 'retention', title: '数据保留', description: '审计记录保留时长', icon: <Database className="h-4 w-4" /> },
];

function SectionCard({ icon, title, description, children }) {
  return (
    <div className="overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none">
      <div className="flex items-center gap-2.5 border-b border-kumo-line px-4 py-3">
        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-brand">
          {icon}
        </span>
        <div className="min-w-0">
          <div className="text-sm font-semibold text-kumo-strong">{title}</div>
          {description && <div className="truncate text-xs text-kumo-subtle">{description}</div>}
        </div>
      </div>
      <div className="px-4">{children}</div>
    </div>
  );
}

function SettingsCard() {
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

  const handleSave = async () => {
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

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  const renderField = (field) => {
    const value = (values && values[field.key]) || (field.kind === 'switch' ? 'false' : '');
    if (field.kind === 'switch') {
      // 开关类单行显示：标题描述居左、开关居右
      return (
        <div key={field.key} className="flex items-center justify-between gap-3 border-b border-kumo-line py-3.5 last:border-0">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">{field.label}</div>
            {field.description && <div className="mt-0.5 text-xs text-kumo-subtle">{field.description}</div>}
          </div>
          <Switch
            checked={value === 'true'}
            onCheckedChange={(checked) => setField(field.key, checked ? 'true' : 'false')}
          />
        </div>
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
          className={field.kind === 'number' ? 'w-24' : 'w-full'}
          type={field.kind === 'number' ? 'number' : 'text'}
          placeholder={field.placeholder}
          value={value}
          onChange={(e) => setField(field.key, e.target.value)}
        />
      );
    }
    if (field.kind === 'number') {
      // 数字类：名称左、输入右，与开关行同一对齐基线
      return (
        <div key={field.key} className="flex items-center justify-between gap-3 border-b border-kumo-line py-3.5 last:border-0">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-kumo-strong">{field.label}</div>
            {field.description && <div className="mt-0.5 text-xs text-kumo-subtle">{field.description}</div>}
          </div>
          {control}
        </div>
      );
    }
    return (
      <div key={field.key} className="border-b border-kumo-line py-3.5 last:border-0">
        <div className="mb-2">
          <div className="text-sm font-semibold text-kumo-strong">{field.label}</div>
          {field.description && <div className="mt-0.5 text-xs text-kumo-subtle">{field.description}</div>}
        </div>
        {control}
      </div>
    );
  };

  return (
    <div>
      <div className="space-y-4">
        {SETTING_SECTIONS.map((section) => (
          <SectionCard key={section.key} icon={section.icon} title={section.title} description={section.description}>
            {SETTING_FIELDS.filter((field) => field.group === section.key).map(renderField)}
          </SectionCard>
        ))}
      </div>
      <div className="sticky bottom-0 z-10 -mx-4 mt-3 flex h-12 items-center justify-end gap-3 border-t border-kumo-line bg-[var(--app-main-surface)] px-4">
        {savedAt ? <span className="text-xs text-kumo-success">已保存 ✓</span> : null}
        <Button size="sm" variant="primary" onClick={handleSave} disabled={saving || !values}>
          {saving ? '保存中...' : '保存'}
        </Button>
      </div>
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
  const [bindingOpen, setBindingOpen] = useState(false);
  const [bindingForm, setBindingForm] = useState({ channelId: '', channelUserId: '', username: '' });

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

  const saveBinding = async () => {
    if (!bindingForm.channelId || !bindingForm.channelUserId.trim()) {
      setError('请选择频道并填写用户 ID');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const res = await fetch('/api/admin-ai/channel-bindings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          channelId: bindingForm.channelId,
          channelUserId: bindingForm.channelUserId.trim(),
          username: bindingForm.username.trim(),
        }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).ok === false) {
        setError(((data.data || data).error) || '绑定失败');
        return;
      }
      setBindingForm({ channelId: bindingForm.channelId, channelUserId: '', username: '' });
      setBindingOpen(false);
      load();
    } catch {
      setError('绑定失败');
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

  const channelOptions = channels.map((c) => ({ value: c.id, label: c.name || c.id }));

  return (
    <div className="space-y-4 pb-4">
      <ErrorBanner message={error} />

      {/* ---- 频道卡片 ---- */}
      <div className="overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-brand">
              <Send className="h-4 w-4" />
            </span>
            <div className="min-w-0">
              <div className="text-sm font-semibold text-kumo-strong">频道</div>
              <div className="truncate text-xs text-kumo-subtle">从通知中心选择 Telegram 渠道作为 AI 机器人来源</div>
            </div>
          </div>
          {!formOpen && (
            <Button size="sm" variant="secondary" onClick={openCreate}>
              <Plus className="h-3.5 w-3.5" /> 新建频道
            </Button>
          )}
        </div>

        <div className="p-3.5">
      {channels.length === 0 && !formOpen ? (
        <Empty title="暂无频道" description="点击「新建频道」接入 Telegram Bot" />
      ) : (
        <div className="space-y-2">
          {channels.map((channel) => (
            <div
              key={channel.id}
              className="flex flex-col gap-3 rounded-lg border border-kumo-line bg-kumo-base px-4 py-3 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between"
            >
              <div className="flex min-w-0 items-center gap-3">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                  <Send className="h-4 w-4" />
                </span>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-semibold text-kumo-strong">{channel.name}</span>
                    <Badge variant="secondary">telegram</Badge>
                    {channel.status === 'running' ? (
                      <Badge variant="success">运行中</Badge>
                    ) : (
                      <Badge variant="secondary">已停止</Badge>
                    )}
                  </div>
                  <div className="mt-0.5 truncate text-xs text-kumo-subtle">
                    来源：{channel.notificationChannelName || '旧 Token 配置（未选择来源）'}
                  </div>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Switch checked={channel.enabled} onCheckedChange={() => toggleEnabled(channel)} aria-label="启用频道" />
                {channel.status === 'running' ? (
                  <Button size="sm" variant="ghost" onClick={() => runAction(channel, 'stop')}>
                    停止
                  </Button>
                ) : (
                  <Button size="sm" variant="secondary" onClick={() => runAction(channel, 'start')} disabled={!channel.enabled}>
                    <Play className="h-3 w-3" /> 启动
                  </Button>
                )}
                <Button size="sm" variant="ghost" onClick={() => openEdit(channel)}>
                  编辑
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
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
        </div>
      </div>

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
              <Input className="w-full" placeholder="如：Telegram 主机器人" value={form.name} onChange={(e) => setFormField('name', e.target.value)} />
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

      {/* ---- 白名单卡片 ---- */}
      <div className="overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none">
        <div className="flex items-center justify-between gap-3 border-b border-kumo-line px-4 py-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-brand">
              <Users className="h-4 w-4" />
            </span>
            <div className="min-w-0">
              <div className="text-sm font-semibold text-kumo-strong">白名单</div>
              <div className="truncate text-xs text-kumo-subtle">留空 = 任何人可对话；填入后仅列表内用户可对话</div>
            </div>
          </div>
          {!bindingOpen && (
            <Button size="sm" variant="secondary" onClick={() => setBindingOpen(true)} disabled={channels.length === 0}>
              <Plus className="h-3.5 w-3.5" /> 新增
            </Button>
          )}
        </div>
        <div className="space-y-2.5 p-3.5">
      {bindingOpen && (
        <div className="rounded-lg border border-kumo-line bg-kumo-recessed p-4">
          <div className="grid gap-3 cq-sm:grid-cols-3">
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">频道</div>
              <Select
                size="sm"
                className="w-full"
                placeholder="选择频道"
                value={bindingForm.channelId}
                onValueChange={(v) => setBindingForm((prev) => ({ ...prev, channelId: String(v) }))}
                items={channelOptions}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">Telegram 用户 ID *</div>
              <Input
                className="w-full"
                placeholder="数字 ID"
                value={bindingForm.channelUserId}
                onChange={(e) => setBindingForm((prev) => ({ ...prev, channelUserId: e.target.value }))}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">用户名</div>
              <Input
                className="w-full"
                placeholder="@username（可选）"
                value={bindingForm.username}
                onChange={(e) => setBindingForm((prev) => ({ ...prev, username: e.target.value }))}
              />
            </div>
            <div className="flex items-end justify-end gap-2 cq-sm:col-span-3">
              <Button size="sm" variant="ghost" onClick={() => { setBindingOpen(false); setBindingForm({ channelId: bindingForm.channelId, channelUserId: '', username: '' }); }}>
                取消
              </Button>
              <Button size="sm" variant="primary" onClick={saveBinding} disabled={saving}>
                添加
              </Button>
            </div>
          </div>
        </div>
      )}

      {bindings.length === 0 ? (
        <p className="text-xs text-kumo-subtle">
          {bindingOpen ? '' : '白名单为空，当前开放（任何人可对话）。'}
        </p>
      ) : (
        <div className="space-y-2">
          {bindings.map((binding) => (
            <div
              key={binding.id}
              className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-base px-4 py-2.5"
            >
              <div className="flex min-w-0 items-center gap-3">
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                  <MessageSquare className="h-4 w-4" />
                </span>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium text-kumo-strong">
                      {binding.channelUserId}
                    </span>
                    {binding.username && <span className="truncate text-xs text-kumo-subtle">{binding.username}</span>}
                  </div>
                  <div className="mt-0.5 truncate text-xs text-kumo-subtle">
                    {binding.channelName || binding.channelId}
                  </div>
                </div>
              </div>
              <Button size="sm" variant="ghost" onClick={() => deleteBinding(binding)}>
                <Trash className="h-3 w-3" />
              </Button>
            </div>
          ))}
        </div>
      )}
        </div>
      </div>
    </div>
  );
}

/* ==================== 管理面板（主页面与 Ask AI 侧栏共用） ==================== */

const TAB_OPTIONS = [
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
        <span className="hidden @[420px]:inline">频道配置</span>
      </span>
    ),
  },
];

export default function AdminConsole() {
  const [activeTab, setActiveTab] = useState('settings');

  return (
    <div className="space-y-4">
      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        tabs={TAB_OPTIONS}
      />

      {activeTab === 'settings' && <SettingsCard />}
      {activeTab === 'channels' && <ChannelsCard />}
    </div>
  );
}
