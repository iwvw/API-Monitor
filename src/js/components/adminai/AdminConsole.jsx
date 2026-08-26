import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { createPortal } from 'react-dom';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Select } from '@cloudflare/kumo/components/select';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Empty, Loader, Tabs } from '@cloudflare/kumo';
import { SectionCard, FieldRow, cx } from '../ui/AppPrimitives.jsx';
import { toast } from '../../modules/toast.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { MessageSquare, Plus, Play, Send, Settings, Trash, X, Bot, ShieldCheck, Sliders, Database, Brain, Search, Edit, ChevronDown, WechatBrand, TelegramBrand, WeComBrand } from '../Icons.jsx';

/* ==================== 通用小组件 ==================== */

function ErrorBanner({ message }) {
  if (!message) return null;
  return (
    <div className="rounded-lg bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">{message}</div>
  );
}

/* 多选模型：portal 悬浮面板勾选（脱离 transform/overflow 父级，fixed 视口定位可靠）；
 * 收起态只显示已选数量（不显示具体模型名） */
function MultiModelSelect({ options, value, onChange }) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState(null);
  const boxRef = useRef(null);
  const panelRef = useRef(null);
  // 只统计仍存在于可用候选列表中的模型：端点上已删/停用的模型不在 options 中，
  // 即便值里残留旧 id 也不计入已选数量，保持一致显示。
  const optionValues = useMemo(() => new Set(options.map((o) => o.value)), [options]);
  const selected = useMemo(
    () =>
      new Set(
        (value || '')
          .split(',')
          .map((s) => s.trim())
          .filter((s) => s && optionValues.has(s))
      ),
    [value, optionValues]
  );
  const close = () => setOpen(false);
  useEffect(() => {
    if (!open) return undefined;
    const onDown = (e) => {
      const inBox = boxRef.current && boxRef.current.contains(e.target);
      const inPanel = panelRef.current && panelRef.current.contains(e.target);
      if (!inBox && !inPanel) close();
    };
    window.addEventListener('mousedown', onDown);
    window.addEventListener('resize', close);
    return () => {
      window.removeEventListener('mousedown', onDown);
      window.removeEventListener('resize', close);
    };
  }, [open]);
  const toggleOpen = () => {
    if (open) {
      close();
      return;
    }
    const r = boxRef.current?.getBoundingClientRect();
    if (r) {
      const w = 340;
      setPos({
        left: Math.max(8, Math.min(r.left, window.innerWidth - w - 8)),
        top: r.bottom + 6,
      });
    }
    setOpen(true);
  };
  const toggle = (v) => {
    const next = new Set(selected);
    if (next.has(v)) next.delete(v);
    else next.add(v);
    onChange([...next].join(','));
  };
  return (
    <div ref={boxRef} className="relative w-full">
      <Button
        type="button"
        size="sm"
        variant="secondary"
        onClick={toggleOpen}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 !px-3 !py-1.5 !text-[11px]"
      >
        <span className="flex min-w-0 items-center gap-1.5 text-kumo-default">
          <Plus className="h-3 w-3 shrink-0 text-kumo-subtle" />
          摘要模型
        </span>
        <Badge variant={selected.size > 0 ? 'primary' : 'outline'} className="shrink-0 !py-0 !text-[10px]">
          {selected.size > 0 ? `已选 ${selected.size} 个` : '未选择'}
        </Badge>
      </Button>
      {open && pos && createPortal(
        <div
          ref={panelRef}
          className="w-[340px] max-h-56 overflow-y-auto rounded-xl bg-kumo-base p-1.5 shadow-xl ring-1 ring-kumo-line"
          style={{ position: 'fixed', left: pos.left, top: pos.top, zIndex: 9999 }}
        >
          {options.length === 0 ? (
            <p className="px-2.5 py-2 text-xs text-kumo-subtle">模型网关无可用模型</p>
          ) : (
            options.map((opt) => (
              <label
                key={opt.value}
                className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-kumo-tint"
              >
                <Checkbox
                  checked={selected.has(opt.value)}
                  onCheckedChange={() => toggle(opt.value)}
                  aria-label={opt.label}
                />
                <span className="min-w-0 truncate text-xs text-kumo-default">{opt.label}</span>
              </label>
            ))
          )}
        </div>,
        document.body
      )}
    </div>
  );
}

/* ==================== 设置页（多键表单） ==================== */

const SETTING_FIELDS = [
  { key: 'admin_ai_enabled', kind: 'switch', group: 'basic', label: '管理 AI 总开关'},
  { key: 'admin_ai_default_model', kind: 'select', group: 'basic', label: '推理模型'},
  { key: 'admin_ai_summary_model', kind: 'multi_select', group: 'basic', label: '摘要模型' },
  { key: 'admin_ai_briefing_model', kind: 'select', group: 'basic', label: '简报模型'},
  { key: 'admin_ai_write_enabled', kind: 'switch', group: 'security', label: '写操作全局开关'},
  { key: 'admin_ai_auto_approve', kind: 'switch', group: 'security', label: '完全批准模式' },
  { key: 'admin_ai_tool_call_limit', kind: 'number', group: 'runtime', label: '工具调用上限'},
  { key: 'admin_ai_timeout_seconds', kind: 'number', group: 'runtime', label: '执行超时（秒）' },
  { key: 'admin_ai_context_window', kind: 'number', group: 'runtime', label: '上下文窗口（token）' },
  { key: 'admin_ai_memories_enabled', kind: 'switch', group: 'runtime', label: '长期记忆总开关'},
  { key: 'admin_ai_memories_model', kind: 'select', group: 'runtime', label: '记忆提炼模型', description: '自动记忆提炼专用模型，选后即刻生效；留空则回退会话/默认模型' },
  { key: 'admin_ai_memories_bootstrap_chars', kind: 'number', group: 'runtime', label: '记忆注入上限（字符）' },
  { key: 'admin_ai_memories_auto_capture', kind: 'switch', group: 'runtime', label: '自动记忆提炼' },
  { key: 'admin_ai_memories_idle_minutes', kind: 'number', group: 'runtime', label: '提炼空闲分钟数' },
  { key: 'admin_ai_audit_retention_days', kind: 'number', group: 'retention', label: '审计保留天数' },
  { key: 'admin_ai_max_concurrent_runs', kind: 'number', group: 'runtime', label: '全局并发执行上限' },
];

const SETTING_SECTIONS = [
  { key: 'basic', title: '基础设置', description: '总开关与模型选择', icon: <Bot className="h-4 w-4 text-brand" /> },
  { key: 'security', title: '安全与审批', description: '写操作与审批策略', icon: <ShieldCheck className="h-4 w-4 text-brand" /> },
  { key: 'runtime', title: '运行参数', description: '工具调用上限与超时', icon: <Sliders className="h-4 w-4 text-brand" /> },
  { key: 'retention', title: '数据保留', description: '审计记录保留时长', icon: <Database className="h-4 w-4 text-brand" /> },
];

// 站点简报模板清单（与后端 briefingTemplatePrompts 保持一致）。
const BRIEFING_TEMPLATE_OPTIONS = [
  { value: 'standard', label: '标准简报', description: '标题 + 关键指标小节（系统资源 / API 调用 / 可用性），突出异常与风险，全文 ≤ 400 字' },
  { value: 'brief', label: '简洁版', description: '一句话结论 + 关键指标与异常项目符号，全文 ≤ 150 字' },
  { value: 'detailed', label: '详细版', description: '摘要 / 分节指标 / 风险建议，全文 ≤ 800 字' },
  { value: 'alert_only', label: '仅异常', description: '只报告异常与风险（按严重度排序）；一切正常时仅输出一句"一切正常"' },
  { value: 'custom', label: '自定义', description: '内容直接作为格式指令注入' },
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

  // 模型下拉选项：只列对外 /v1 暴露的模型（/api/openai/models 与 /v1/models 同源，
  // 已过滤禁用模型并应用映射去前缀；经 /v1 调用自动多端点负载均衡）。
  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/openai/models');
        const data = await res.json();
        const list = Array.isArray(data) ? data : (data.data || []);
        const options = (list || [])
          .filter((m) => m && m.id)
          .map((m) => ({ value: m.id, label: m.id }));
        options.sort((a, b) => a.label.localeCompare(b.label));
        setModelOptions(options);
      } catch {
        setModelOptions([]);
      }
    })();
  }, []);

  // 配置多选模型仅在模型网关可用列表内收敛：端点列表删除模型后，
  // /api/openai/models 不再返回该 id，这里自动剔除对应选中项并静默持久化，
  // 避免已删模型仍被后端 summaryModel 引用；网关无可用模型时不清空配置。
  const prunedRef = useRef(false);
  useEffect(() => {
    if (!values || loading) return undefined;
    if (!modelOptions.length || prunedRef.current) return undefined;
    const available = new Set(modelOptions.map((o) => o.value));
    const next = { ...values };
    let changed = false;
    for (const field of SETTING_FIELDS) {
      if (field.kind !== 'multi_select') continue;
      const raw = String(next[field.key] || '').trim();
      const kept = raw
        .split(',')
        .map((s) => s.trim())
        .filter((m) => m && available.has(m))
        .join(',');
      if (kept !== raw) {
        next[field.key] = kept;
        changed = true;
      }
    }
    if (!changed) return undefined;
    prunedRef.current = true;
    setValues(next);
    // 只写发生变化的键，避免静默覆盖用户已改但未保存的表单值；
    // 后端按 key 逐键 INSERT OR REPLACE，缺失的键保持不变。
    const prunedKeys = SETTING_FIELDS.filter(
      (f) => f.kind === 'multi_select' && next[f.key] !== values[f.key]
    );
    if (prunedKeys.length) {
      const body = Object.fromEntries(prunedKeys.map((f) => [f.key, next[f.key]]));
      fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }).catch(() => {});
    }
    return undefined;
  }, [loading, modelOptions, values]);

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
      if ((data.data || data).ok) {
        setSavedAt(Date.now());
        toast.success('设置已保存');
      } else {
        toast.error('保存失败');
      }
    } catch {
      toast.error('保存失败');
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
      if (field.group === 'security') {
        // 安全与审批：开关渲染为卡片风格（与 AI 接入权限卡片同款：图标+文本，
        // 选中态=开关打开）；颜色统一：选中 border/brand + text/brand，
        // 未选中 border/kumo-line + text/kumo-strong
        const checked = value === 'true';
        return (
          <button
            key={field.key}
            type="button"
            aria-pressed={checked}
            onClick={() => setField(field.key, checked ? 'false' : 'true')}
className={cx(
              'flex flex-col items-center gap-1.5 rounded-lg border px-3 py-3 transition-colors',
              checked
                ? 'border-(--text-color-brand) bg-kumo-tint text-brand'
                : 'border-kumo-line bg-kumo-recessed/25 text-kumo-strong hover:bg-kumo-recessed/50'
            )}
          >
            <ShieldCheck className={cx('h-4 w-4', checked ? 'text-brand' : 'text-kumo-strong')} />
            <span className="text-xs font-medium">{field.label}</span>
          </button>
        );
      }
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
    if (field.kind === 'multi_select') {
      // 多选模型：下拉框内复选框多选，值存逗号串（后端按候选顺序逐个失败回退）
      control = <MultiModelSelect options={modelOptions} value={value} onChange={(v) => setField(field.key, v)} />;
    } else if (field.kind === 'select') {
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
          {section.key === 'security' ? (
            <div className="grid gap-3 p-4 grid-cols-2">
              {SETTING_FIELDS.filter((field) => field.group === section.key).map(renderField)}
            </div>
          ) : (
            SETTING_FIELDS.filter((field) => field.group === section.key).map(renderField)
          )}
        </SectionCard>
      ))}
    </div>
  );
}

/* ==================== 频道页（Telegram 频道 + 白名单） ==================== */

const EMPTY_FORM = {
  id: '',
  type: 'telegram',
  name: '',
  notificationChannelId: '',
  botTokenSet: false,
  botId: '',
  secret: '',
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
  const { isArmed, confirmPress } = useConfirmPress();
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
      type: channel.type || 'telegram',
      name: channel.name,
      notificationChannelId: channel.notificationChannelId || '',
      botTokenSet: channel.type === 'wechat' ? !!(channel.config?.botToken) : false,
      botId: channel.config?.botId || '',
      secret: '',
    });
    setError('');
    setFormOpen(true);
  };

  const setFormField = (field, value) => setForm((prev) => ({ ...prev, [field]: value }));

  const saveChannel = async () => {
    if (!form.name.trim()) {
      setError('填写频道名称');
      return;
    }
    const isTelegram = form.type === 'telegram';
    const isWeCom = form.type === 'wecom';
    if (!form.id && isTelegram && !form.notificationChannelId) {
      setError('选择来源通知渠道（bot token 复用通知中心配置）');
      return;
    }
    if (isWeCom && !form.botId.trim()) {
      setError('填写企业微信 Bot ID');
      return;
    }
    if (!form.id && isWeCom && !form.secret.trim()) {
      setError('填写企业微信 Secret');
      return;
    }
    const weComConfig = isWeCom
      ? { botId: form.botId.trim(), ...(form.secret.trim() ? { secret: form.secret.trim() } : {}) }
      : null;
    setSaving(true);
    setError('');
    try {
      const url = form.id ? `/api/admin-ai/channels/${form.id}` : '/api/admin-ai/channels';
      const payload = form.id
        ? {
            name: form.name.trim(),
            ...(isTelegram ? { notificationChannelId: form.notificationChannelId } : {}),
            ...(weComConfig ? { config: weComConfig } : {}),
          }
        : {
            type: form.type || 'telegram',
            name: form.name.trim(),
            enabled: isTelegram || isWeCom,
            ...(isTelegram ? { notificationChannelId: form.notificationChannelId } : {}),
            ...(weComConfig ? { config: weComConfig } : {}),
          };
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
    if (!confirmPress(`adminai-channel:${channel.id}`, `删除频道「${channel.name || channel.id}」`)) return;
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
      setError(`填写${form.type === 'wechat' ? '微信' : 'Telegram'} 用户 ID`);
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
    if (!confirmPress(`adminai-binding:${binding.id}`, '移除白名单成员')) return;
    try {
      await fetch(`/api/admin-ai/channel-bindings/${binding.id}`, { method: 'DELETE' });
      load();
    } catch {
      setError('删除绑定失败');
    }
  };

  // ---- 微信扫码登录 ----
  const [qrState, setQrState] = useState({ channelId: null, loading: false, qrcode: '', qrcodeImg: '', imgSrc: '', status: '' });
  const qrPollRef = useRef(null);

  // 根据后端返回生成可显示的二维码图片：
  // 优先用完整链接在本地生成（qrcode 库），兼容 base64 图片 / URL 两种后端返回。
  const resolveQRImg = async (body) => {
    const { qrcodeUrl, qrcodeImg } = body;
    const link = qrcodeUrl || (typeof qrcodeImg === 'string' && qrcodeImg.startsWith('http') ? qrcodeImg : '');
    if (link) {
      try {
        return await QRCode.toDataURL(link, { width: 200, margin: 1 });
      } catch { /* 本地生成失败则回退 base64 原样 */ }
    }
    if (typeof qrcodeImg === 'string' && qrcodeImg.startsWith('data:image')) {
      return qrcodeImg;
    }
    if (typeof qrcodeImg === 'string' && qrcodeImg && !qrcodeImg.startsWith('http')) {
      return `data:image/png;base64,${qrcodeImg}`;
    }
    return '';
  };

  const startQRLogin = async (channelId) => {
    setQrState({ channelId, loading: true, qrcode: '', qrcodeImg: '', imgSrc: '', status: 'requesting' });
    setError('');
    try {
      const res = await fetch(`/api/admin-ai/channels/${channelId}/wechat/qrcode`, { method: 'POST' });
      const data = await res.json();
      const body = data.data || data;
      if (!res.ok || !body.qrcode) {
        setQrState((prev) => ({ ...prev, loading: false, status: 'error' }));
        setError((data.error || {}).message || '获取二维码失败');
        return;
      }
      const imgSrc = await resolveQRImg(body);
      setQrState({ channelId, loading: false, qrcode: body.qrcode, qrcodeImg: body.qrcodeImg, imgSrc, status: 'waiting' });
      pollQRStatus(channelId, body.qrcode);
    } catch {
      setQrState((prev) => ({ ...prev, loading: false, status: 'error' }));
      setError('获取二维码失败');
    }
  };

  const pollQRStatus = async (channelId, qrcode) => {
    if (qrPollRef.current) clearTimeout(qrPollRef.current);
    const poll = async () => {
      try {
        const res = await fetch(`/api/admin-ai/channels/${channelId}/wechat/qrcode/status?qrcode=${encodeURIComponent(qrcode)}`);
        const data = await res.json();
        const body = data.data || data;
        const status = body.status || '';
        setQrState((prev) => ({ ...prev, status }));
        if (status === 'confirmed') {
          setQrState((prev) => ({ ...prev, status: 'confirmed' }));
          setForm((prev) => ({ ...prev, botTokenSet: true }));
          load();
          return;
        }
        if (status === 'expired') return;
        qrPollRef.current = setTimeout(poll, 2000);
      } catch {
        qrPollRef.current = setTimeout(poll, 3000);
      }
    };
    poll();
  };

  const cancelQRLogin = () => {
    if (qrPollRef.current) clearTimeout(qrPollRef.current);
    setQrState({ channelId: null, loading: false, qrcode: '', qrcodeImg: '', imgSrc: '', status: '' });
  };

  useEffect(() => () => { if (qrPollRef.current) clearTimeout(qrPollRef.current); }, []);

  // 微信未授权频道：打开编辑表单即自动拉取二维码（不显示手动按钮）。
  const qrAutoRef = useRef(null);
  useEffect(() => {
    if (!formOpen || !form.id || form.type !== 'wechat') {
      qrAutoRef.current = null;
      cancelQRLogin();
      return;
    }
    if (form.botTokenSet) {
      qrAutoRef.current = null;
      return;
    }
    if (qrAutoRef.current === form.id) return; // 该频道已在拉取中
    qrAutoRef.current = form.id;
    startQRLogin(form.id);
  }, [formOpen, form.id, form.type, form.botTokenSet]);

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  return (
    <div className="space-y-4 pb-4">
      <ErrorBanner message={error} />

      {/* ---- 频道卡片 ---- */}
      <SectionCard
        title="频道"
        icon={<Send className="h-4 w-4 text-brand" />}
        actions={!formOpen && (
          <Button size="sm" variant="secondary" onClick={openCreate}>
            <Plus className="h-3.5 w-3.5" /> 新建频道
          </Button>
        )}
        bodyPadding="none"
      >
      {channels.length === 0 && !formOpen ? (
        <Empty className="py-10" title="暂无频道" description="点击「新建频道」接入 Telegram 或微信" />
      ) : (
        <div className="divide-y divide-kumo-line">
          {channels.map((channel) => (
            <div
              key={channel.id}
              className="flex flex-col gap-3 px-4 py-3.5 cq-sm:flex-row cq-sm:items-center cq-sm:justify-between"
            >
              <div className="flex min-w-0 items-center gap-3">
                <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                  {channel.type === 'wechat' ? (
                    <WechatBrand className="size-6" />
                  ) : channel.type === 'telegram' ? (
                    <TelegramBrand className="size-6" />
                  ) : channel.type === 'wecom' ? (
                    <WeComBrand className="size-6" />
                  ) : (
                    <Send className="h-4 w-4" />
                  )}
                </span>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-semibold text-kumo-strong">{channel.name}</span>
                    {channel.type === 'wechat' ? (
                      <span className="truncate text-xs text-kumo-subtle">
                        {channel.config?.botToken ? '已授权' : '未授权（需扫码）'}
                      </span>
                    ) : channel.type === 'wecom' ? (
                      <span className="truncate text-xs text-kumo-subtle">
                        {channel.config?.botId && channel.config?.secret ? '已配置' : '待配置（需填 Bot ID / Secret）'}
                      </span>
                    ) : (
                      <span className="truncate text-xs text-kumo-subtle">
                        来源：{channel.notificationChannelName || '旧 Token 配置（未选择来源）'}
                      </span>
                    )}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-2">
                    <Badge variant="secondary">{channel.type}</Badge>
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
                  variant={isArmed(`adminai-channel:${channel.id}`) ? 'destructive' : 'secondary'}
                  onClick={() => deleteChannel(channel)}
                >
                  <Trash className="h-3 w-3" />
                  {isArmed(`adminai-channel:${channel.id}`) ? '确认删除' : ''}
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
              <div className="mb-1 text-xs font-medium text-kumo-subtle">频道类型</div>
              <Select
                size="sm"
                className="w-full"
                value={form.type || 'telegram'}
                onValueChange={(v) => { setFormField('type', String(v)); setError(''); }}
                items={[{ value: 'telegram', label: 'Telegram Bot' }, { value: 'wechat', label: '微信（个人号扫码）' }, { value: 'wecom', label: '企业微信（智能机器人）' }]}
              />
            </div>
            <div>
              <div className="mb-1 text-xs font-medium text-kumo-subtle">名称</div>
              <Input size="sm" className="w-full" placeholder={form.type === 'wechat' ? '如：微信机器人' : '如：Telegram 主机器人'} aria-label="频道名称" value={form.name} onChange={(e) => setFormField('name', e.target.value)} />
            </div>
          </div>

          {form.type === 'telegram' ? (
            <>
              <div className="mt-3">
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
              <p className="mt-3 text-xs leading-5 text-kumo-subtle">
                复用通知中心已配置的 Telegram 渠道（需含 bot_token 与 chat_id），无需在此填写；同一渠道只能被一个 AI 频道引用。
              </p>
            </>
          ) : form.type === 'wechat' ? (
            <>
              <p className="mt-3 text-xs leading-5 text-kumo-subtle">
                微信频道通过扫码登录个人微信号获取 Bot 权限，bot_token 加密存储在频道配置中。创建频道后在下方扫码登录。
              </p>
              {form.id && (
                <div className="mt-3 rounded-lg border border-kumo-line bg-kumo-base p-3">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs font-semibold text-kumo-strong">微信账号</span>
                    {form.botTokenSet || (qrState.channelId === form.id && qrState.status === 'confirmed') ? (
                      <Badge variant="success">已授权</Badge>
                    ) : (
                      <Badge variant="outline">未授权</Badge>
                    )}
                  </div>
                  {form.botTokenSet ? (
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <p className="text-xs text-kumo-subtle">已绑定微信账号，机器人将以此账号收发消息。</p>
                      <Button size="sm" variant="secondary" onClick={() => setForm((prev) => ({ ...prev, botTokenSet: false }))}>
                        重新授权
                      </Button>
                    </div>
                  ) : qrState.channelId === form.id && qrState.imgSrc ? (
                    <div className="flex flex-col items-center gap-2">
                      <img src={qrState.imgSrc} alt="微信登录二维码" className="w-48 rounded-lg" />
                      <p className="text-xs text-kumo-subtle">
                        {qrState.status === 'scanned' ? '已扫描，请在手机确认' : '请用微信扫码登录'}
                      </p>
                    </div>
                  ) : qrState.channelId === form.id && qrState.loading ? (
                    <div className="flex flex-col items-center gap-2 py-4">
                      <Loader size={24} className="text-kumo-subtle" />
                      <p className="text-xs text-kumo-subtle">正在获取二维码…</p>
                    </div>
                  ) : (
                    <div className="flex flex-col items-center gap-2 py-2">
                      <p className="text-xs text-kumo-subtle">二维码加载失败</p>
                      <Button size="sm" variant="ghost" onClick={() => { qrAutoRef.current = null; startQRLogin(form.id); }}>
                        重新加载
                      </Button>
                    </div>
                  )}
                </div>
              )}
            </>
          ) : (
            <>
              <p className="mt-3 text-xs leading-5 text-kumo-subtle">
                企业微信「智能机器人」长链接模式：出站 WebSocket 连接，无需公网回调地址。凭据来自企微管理后台 → 工作台 → 智能机器人 → 创建机器人（API 模式·通过长链接配置）。Secret 加密存储；编辑时留空表示保持不变。
              </p>
              <div className="mt-3 grid gap-3 cq-sm:grid-cols-2">
                <div>
                  <div className="mb-1 text-xs font-medium text-kumo-subtle">Bot ID</div>
                  <Input size="sm" className="w-full" placeholder="企微智能机器人 Bot ID" aria-label="企业微信 Bot ID" value={form.botId} onChange={(e) => setFormField('botId', e.target.value)} />
                </div>
                <div>
                  <div className="mb-1 text-xs font-medium text-kumo-subtle">Secret</div>
                  <Input
                    size="sm"
                    type="password"
                    className="w-full"
                    placeholder={form.id ? '已配置，留空保持不变' : '企微智能机器人 Secret'}
                    aria-label="企业微信 Secret"
                    value={form.secret}
                    onChange={(e) => setFormField('secret', e.target.value)}
                  />
                </div>
              </div>
            </>
          )}

          {/* 白名单管理（并入频道编辑设置）：仅 Telegram 显示；微信/企微不设白名单 */}
          {form.id && form.type === 'telegram' && (
            <div className="mt-4 rounded-lg border border-kumo-line bg-kumo-base p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-xs font-semibold text-kumo-strong">白名单成员</span>
                <span className="text-[11px] text-kumo-subtle">留空 = 任何人可对话</span>
              </div>
              {bindings.filter((b) => b.channelId === form.id).length === 0 ? (
                <p className="text-xs text-kumo-subtle">加入成员后仅列表内用户可对话</p>
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
                      <Button size="sm" variant={isArmed(`adminai-binding:${binding.id}`) ? 'destructive' : 'secondary'} onClick={() => deleteBinding(binding)} aria-label="移除白名单成员">
                        {isArmed(`adminai-binding:${binding.id}`) ? <Trash className="h-3 w-3" /> : <X className="h-3 w-3" />}
                      </Button>
                    </div>
                  ))}
                </div>
              )}
              <div className="mt-2.5 flex flex-wrap items-center gap-2">
                <Input
                  size="sm"
                  className="w-40"
                  placeholder={(form.type === 'wechat' ? '微信' : 'Telegram') + ' 用户 ID *'}
                  aria-label={form.type === 'wechat' ? '微信用户 ID' : 'Telegram 用户 ID'}
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
      if ((data.data || data).ok) {
        setSavedAt(Date.now());
        toast.success('模板已保存');
      } else {
        toast.error('保存失败');
      }
    } catch {
      toast.error('保存失败');
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
        icon={<MessageSquare className="h-4 w-4 text-brand" />}
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
  const { isArmed: memIsArmed, confirmPress: memConfirmPress } = useConfirmPress();
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
    if (!memConfirmPress(`adminai-memory:${item.id}`, '删除记忆条目')) return;
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
        icon={<Brain className="h-4 w-4 text-brand" />}
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
                        <Badge variant={item.source === 'auto' ? 'orange' : 'teal'}>
                          {item.source === 'auto' ? '自动提炼' : 'AI 记录'}
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
                        variant={memIsArmed(`adminai-memory:${item.id}`) ? 'destructive' : 'secondary'}
                        className="!px-2"
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

export default function AdminConsole({ hideTabs = false, activeTab: controlledTab, onTabChange }) {
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
        {activeTab === 'settings' && (
          <Button size="sm" variant="primary" onClick={form.save} disabled={form.saving || !form.values}>
            {form.saving ? '保存中...' : '保存'}
          </Button>
        )}
      </div>
    </div>
  );
}
