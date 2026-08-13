import React, { useCallback, useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Select } from '@cloudflare/kumo/components/select';
import { Empty, Loader, SensitiveInput, Tabs } from '@cloudflare/kumo';
import { AppCard } from '../components/ui/AppPrimitives.jsx';
import { Bot, MessageSquare, Plus, Play, Send, Settings, ShieldCheck, Trash, X } from '../components/Icons.jsx';

/* ==================== 通用小组件 ==================== */

function FieldRow({ title, description, children }) {
  return (
    <div className="flex flex-col gap-3 border-b border-kumo-line py-4 last:border-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0">
        <div className="text-sm font-semibold text-kumo-strong">{title}</div>
        {description && <div className="mt-0.5 text-xs text-kumo-subtle">{description}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

function ErrorBanner({ message }) {
  if (!message) return null;
  return (
    <div className="rounded-lg bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">{message}</div>
  );
}

const STATUS_META = {
  success: { variant: 'success', label: '成功' },
  error: { variant: 'error', label: '失败' },
  failed: { variant: 'error', label: '失败' },
  blocked: { variant: 'warning', label: '被拦截' },
  running: { variant: 'primary', label: '运行中' },
};

function StatusBadge({ status }) {
  const meta = STATUS_META[status] || { variant: 'secondary', label: status || '未知' };
  return <Badge variant={meta.variant}>{meta.label}</Badge>;
}

function formatTime(value) {
  if (!value) return '';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString('zh-CN');
}

/* ==================== 设置页（多键表单） ==================== */

const SETTING_FIELDS = [
  { key: 'admin_ai_enabled', kind: 'switch', label: '管理 AI 总开关', description: '关闭后所有来源（Web 侧栏 / Telegram 频道）不再受理新对话' },
  { key: 'admin_ai_default_model', kind: 'text', label: '默认推理模型', description: 'endpointId/modelName，留空使用引擎默认模型', placeholder: 'openai/gpt-4o' },
  { key: 'admin_ai_write_enabled', kind: 'switch', label: '写操作全局开关', description: '允许 AI 执行写操作；写请求仍走人工审批流' },
  { key: 'admin_ai_tool_call_limit', kind: 'number', label: '工具调用上限', description: '单轮执行最多调用工具的次数' },
  { key: 'admin_ai_timeout_seconds', kind: 'number', label: '执行超时（秒）', description: '单轮执行超时秒数' },
  { key: 'admin_ai_context_window', kind: 'number', label: '上下文窗口（token）', description: '上下文 token 上限' },
  { key: 'admin_ai_audit_retention_days', kind: 'number', label: '审计保留天数', description: '执行与工具调用审计记录的保留天数' },
  { key: 'admin_ai_gateway_key', kind: 'secret', label: '网关密钥', description: '外部网关调用管理 AI 接口的鉴权密钥' },
];

function SettingsCard() {
  const [values, setValues] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);

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

  return (
    <div>
      {SETTING_FIELDS.map((field) => {
        const value = (values && values[field.key]) || (field.kind === 'switch' ? 'false' : '');
        let control;
        if (field.kind === 'switch') {
          control = (
            <Switch
              checked={value === 'true'}
              onChange={(checked) => setField(field.key, checked ? 'true' : 'false')}
            />
          );
        } else if (field.kind === 'secret') {
          control = (
            <SensitiveInput
              className="w-64"
              placeholder="留空表示未设置"
              value={value}
              onValueChange={(v) => setField(field.key, v)}
            />
          );
        } else {
          control = (
            <Input
              className={field.kind === 'number' ? 'w-28' : 'w-64'}
              type={field.kind === 'number' ? 'number' : 'text'}
              placeholder={field.placeholder}
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
      })}
      <div className="flex items-center justify-end gap-3 pt-4">
        {savedAt ? <span className="text-xs text-kumo-success">已保存 ✓</span> : null}
        <Button size="sm" variant="primary" onClick={handleSave} disabled={saving || !values}>
          {saving ? '保存中...' : '保存设置'}
        </Button>
      </div>
    </div>
  );
}

/* ==================== 频道页（Telegram 频道 + 用户绑定） ==================== */

const POLICY_OPTIONS = [
  { value: 'allowlist', label: '白名单' },
  { value: 'open', label: '开放' },
];

function PolicyToggle({ value, onChange }) {
  return (
    <div className="inline-flex overflow-hidden rounded-lg border border-kumo-line bg-kumo-control">
      {POLICY_OPTIONS.map((opt) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => onChange(opt.value)}
          className={`px-3 py-1.5 text-xs transition-colors ${
            value === opt.value
              ? 'bg-kumo-fill font-semibold text-kumo-strong'
              : 'text-kumo-subtle hover:text-kumo-default'
          }`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

const EMPTY_FORM = {
  id: '',
  name: '',
  botToken: '',
  dmPolicy: 'allowlist',
  groupPolicy: 'allowlist',
  allowFrom: '',
  textChunkLimit: 4096,
};

function ChannelsCard() {
  const [channels, setChannels] = useState([]);
  const [bindings, setBindings] = useState([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState(EMPTY_FORM);
  const [confirmDelete, setConfirmDelete] = useState('');
  const [bindingOpen, setBindingOpen] = useState(false);
  const [bindingForm, setBindingForm] = useState({ channelId: '', channelUserId: '', username: '', panelUserId: '', role: 'admin' });

  const load = useCallback(async () => {
    try {
      const [chRes, bdRes] = await Promise.all([
        fetch('/api/admin-ai/channels'),
        fetch('/api/admin-ai/channel-bindings'),
      ]);
      const chData = await chRes.json();
      const bdData = await bdRes.json();
      setChannels((chData.data || chData).channels || []);
      setBindings((bdData.data || bdData).bindings || []);
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
    const cfg = channel.config || {};
    setForm({
      id: channel.id,
      name: channel.name,
      botToken: '',
      dmPolicy: cfg.dmPolicy === 'open' ? 'open' : 'allowlist',
      groupPolicy: cfg.groupPolicy === 'open' ? 'open' : 'allowlist',
      allowFrom: Array.isArray(cfg.allowFrom) ? cfg.allowFrom.join(', ') : '',
      textChunkLimit: Number(cfg.textChunkLimit) || 4096,
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
    if (!form.id && !form.botToken.trim()) {
      setError('请填写 botToken');
      return;
    }
    setSaving(true);
    setError('');
    try {
      const config = {
        dmPolicy: form.dmPolicy,
        groupPolicy: form.groupPolicy,
        allowFrom: form.allowFrom.split(',').map((s) => s.trim()).filter(Boolean),
        textChunkLimit: Number(form.textChunkLimit) || 4096,
      };
      if (form.botToken.trim()) config.botToken = form.botToken.trim();
      const url = form.id ? `/api/admin-ai/channels/${form.id}` : '/api/admin-ai/channels';
      const payload = form.id
        ? { name: form.name.trim(), config }
        : { type: 'telegram', name: form.name.trim(), enabled: true, config };
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
          panelUserId: bindingForm.panelUserId.trim(),
          role: bindingForm.role,
        }),
      });
      const data = await res.json();
      if (!res.ok || (data.data || data).ok === false) {
        setError(((data.data || data).error) || '绑定失败');
        return;
      }
      setBindingForm({ channelId: bindingForm.channelId, channelUserId: '', username: '', panelUserId: '', role: 'admin' });
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
    <div className="space-y-5">
      <ErrorBanner message={error} />

      {/* ---- 频道列表 ---- */}
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-semibold text-kumo-strong">频道</div>
          <div className="text-xs text-kumo-subtle">接入渠道（v1 支持 Telegram），写操作默认只读</div>
        </div>
        {!formOpen && (
          <Button size="sm" variant="secondary" onClick={openCreate}>
            <Plus className="h-3.5 w-3.5" /> 新建频道
          </Button>
        )}
      </div>

      {channels.length === 0 && !formOpen ? (
        <Empty title="暂无频道" description="点击「新建频道」接入 Telegram Bot" />
      ) : (
        <div className="space-y-2">
          {channels.map((channel) => (
            <div
              key={channel.id}
              className="flex flex-col gap-3 rounded-xl border border-kumo-line bg-kumo-control px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
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
                    {channel.config
                      ? `${channel.config.dmPolicy === 'open' ? '私聊：开放' : '私聊：白名单'} · ${channel.config.groupPolicy === 'open' ? '群组：开放' : '群组：白名单'}`
                      : '未配置'}
                  </div>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <Switch checked={channel.enabled} onChange={() => toggleEnabled(channel)} aria-label="启用频道" />
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

      {/* ---- 新建/编辑频道表单 ---- */}
      {formOpen && (
        <div className="rounded-xl border border-kumo-line bg-kumo-control p-4">
          <div className="mb-3 flex items-center justify-between">
            <span className="text-sm font-semibold text-kumo-strong">
              {form.id ? '编辑频道' : '新建频道'}
            </span>
            <Button size="sm" variant="ghost" onClick={() => { setFormOpen(false); setError(''); }}>
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">名称</div>
              <Input className="w-full" placeholder="如：Telegram 主机器人" value={form.name} onChange={(e) => setFormField('name', e.target.value)} />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">botToken</div>
              <SensitiveInput
                className="w-full"
                placeholder={form.id ? '留空保持原 Token' : '123456:ABC-DEF...'}
                value={form.botToken}
                onValueChange={(v) => setFormField('botToken', v)}
              />
            </div>
            <div className="flex items-end justify-between gap-3">
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-subtle">私聊策略</div>
                <PolicyToggle value={form.dmPolicy} onChange={(v) => setFormField('dmPolicy', v)} />
              </div>
              <div>
                <div className="mb-1 text-xs font-medium text-kumo-subtle">群组策略</div>
                <PolicyToggle value={form.groupPolicy} onChange={(v) => setFormField('groupPolicy', v)} />
              </div>
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">白名单用户 ID（逗号分隔）</div>
              <Input
                className="w-full"
                placeholder="123456789, 987654321"
                value={form.allowFrom}
                onChange={(e) => setFormField('allowFrom', e.target.value)}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">消息分片上限</div>
              <Input
                className="w-full"
                type="number"
                min={1}
                max={4096}
                value={form.textChunkLimit}
                onChange={(e) => setFormField('textChunkLimit', e.target.value)}
              />
            </div>
          </div>
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

      {/* ---- 用户绑定 ---- */}
      <div className="flex items-center justify-between pt-2">
        <div>
          <div className="text-sm font-semibold text-kumo-strong">用户绑定</div>
          <div className="text-xs text-kumo-subtle">绑定渠道用户后可绕过白名单策略直接授权</div>
        </div>
        {!bindingOpen && (
          <Button size="sm" variant="secondary" onClick={() => setBindingOpen(true)} disabled={channels.length === 0}>
            <Plus className="h-3.5 w-3.5" /> 新增绑定
          </Button>
        )}
      </div>

      {bindingOpen && (
        <div className="rounded-xl border border-kumo-line bg-kumo-control p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">频道</div>
              <Select
                label="频道"
                placeholder="选择频道"
                value={bindingForm.channelId}
                onValueChange={(v) => setBindingForm((prev) => ({ ...prev, channelId: String(v) }))}
                items={channelOptions}
                size="sm"
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">渠道用户 ID *</div>
              <Input
                className="w-full"
                placeholder="Telegram 数字 ID"
                value={bindingForm.channelUserId}
                onChange={(e) => setBindingForm((prev) => ({ ...prev, channelUserId: e.target.value }))}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">用户名</div>
              <Input
                className="w-full"
                placeholder="@username"
                value={bindingForm.username}
                onChange={(e) => setBindingForm((prev) => ({ ...prev, username: e.target.value }))}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">面板用户 ID</div>
              <Input
                className="w-full"
                placeholder="可选"
                value={bindingForm.panelUserId}
                onChange={(e) => setBindingForm((prev) => ({ ...prev, panelUserId: e.target.value }))}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">角色</div>
              <Select
                label="角色"
                value={bindingForm.role}
                onValueChange={(v) => setBindingForm((prev) => ({ ...prev, role: String(v) }))}
                items={[{ value: 'admin', label: 'admin' }, { value: 'user', label: 'user' }]}
                size="sm"
              />
            </div>
            <div className="flex items-end justify-end gap-2">
              <Button size="sm" variant="ghost" onClick={() => setBindingOpen(false)}>
                取消
              </Button>
              <Button size="sm" variant="primary" onClick={saveBinding} disabled={saving}>
                绑定
              </Button>
            </div>
          </div>
        </div>
      )}

      {bindings.length === 0 ? (
        <p className="text-xs text-kumo-subtle">暂无绑定用户</p>
      ) : (
        <div className="space-y-2">
          {bindings.map((binding) => (
            <div
              key={binding.id}
              className="flex items-center justify-between gap-3 rounded-xl border border-kumo-line bg-kumo-control px-4 py-2.5"
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
                    <Badge variant="secondary">{binding.role || 'admin'}</Badge>
                  </div>
                  <div className="mt-0.5 truncate text-xs text-kumo-subtle">
                    {binding.channelName || binding.channelId}
                    {binding.panelUserId ? ` · 面板用户 ${binding.panelUserId}` : ' · 未绑定面板用户'}
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
  );
}

/* ==================== 审计页 ==================== */

const AUDIT_LIMIT = 50;

function AuditCard() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [source, setSource] = useState('');
  const [sourceInput, setSourceInput] = useState('');
  const [offset, setOffset] = useState(0);

  const load = useCallback(async (src, off) => {
    setLoading(true);
    try {
      const q = new URLSearchParams({ limit: String(AUDIT_LIMIT), offset: String(off) });
      if (src) q.set('source', src);
      const res = await fetch(`/api/admin-ai/audit?${q}`);
      const data = await res.json();
      const body = data.data || data;
      setItems(body.items || []);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load(source, offset);
  }, [load, source, offset]);

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Input
          className="w-64"
          placeholder="按来源筛选（如 channel:telegram）"
          value={sourceInput}
          onChange={(e) => setSourceInput(e.target.value)}
        />
        <Button
          size="sm"
          variant="secondary"
          onClick={() => {
            setOffset(0);
            setSource(sourceInput.trim());
          }}
        >
          查询
        </Button>
      </div>

      {loading ? (
        <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>
      ) : items.length === 0 ? (
        <Empty title="暂无审计记录" description="AI 执行与工具调用记录会显示在这里" />
      ) : (
        <div className="space-y-2">
          {items.map((item) => (
            <div key={item.id} className="rounded-xl border border-kumo-line bg-kumo-control p-3">
              <div className="flex items-center justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2">
                  <Badge variant={item.kind === 'execution' ? 'primary' : 'secondary'}>
                    {item.kind === 'execution' ? '执行' : '工具调用'}
                  </Badge>
                  {item.toolName && (
                    <span className="font-mono text-xs font-semibold text-kumo-strong">{item.toolName}</span>
                  )}
                  {item.llmModel && <span className="hidden truncate text-xs text-kumo-subtle sm:inline">{item.llmModel}</span>}
                  {item.promptTokens ? (
                    <span className="hidden text-xs text-kumo-subtle md:inline">
                      ↑{item.promptTokens} ↓{item.completionTokens || 0}
                    </span>
                  ) : null}
                </div>
                <span className="shrink-0 text-xs text-kumo-subtle">{formatTime(item.startedAt)}</span>
              </div>
              {item.inputSummary && (
                <div className="mt-1.5 truncate text-xs text-kumo-subtle" title={item.inputSummary}>
                  {item.inputSummary}
                </div>
              )}
              <div className="mt-1.5 flex items-center gap-2">
                <StatusBadge status={item.status} />
                {item.source && <span className="truncate text-xs text-kumo-subtle">{item.source}</span>}
                {item.error && <span className="min-w-0 truncate text-xs text-kumo-danger" title={item.error}>{item.error}</span>}
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center justify-between pt-1">
        <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - AUDIT_LIMIT))}>
          上一页
        </Button>
        <span className="text-xs text-kumo-subtle">
          第 {offset + 1}–{offset + items.length} 条
        </span>
        <Button size="sm" variant="ghost" disabled={items.length < AUDIT_LIMIT} onClick={() => setOffset(offset + AUDIT_LIMIT)}>
          下一页
        </Button>
      </div>
    </div>
  );
}

/* ==================== 页面 ==================== */

const TAB_OPTIONS = [
  {
    value: 'settings',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Settings className="h-3.5 w-3.5" />
        系统设置
      </span>
    ),
  },
  {
    value: 'channels',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Send className="h-3.5 w-3.5" />
        频道配置
      </span>
    ),
  },
  {
    value: 'audit',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <ShieldCheck className="h-3.5 w-3.5" />
        审计查询
      </span>
    ),
  },
];

export default function AdminAIPage() {
  const [activeTab, setActiveTab] = useState('settings');

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-4 sm:p-6">
      <div className="flex items-center gap-2">
        <Bot className="h-5 w-5 text-kumo-brand" />
        <h1 className="text-lg font-bold text-kumo-strong">管理 AI</h1>
      </div>

      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        tabs={TAB_OPTIONS}
      />

      <AppCard>
        {activeTab === 'settings' && <SettingsCard />}
        {activeTab === 'channels' && <ChannelsCard />}
        {activeTab === 'audit' && <AuditCard />}
      </AppCard>
    </div>
  );
}
