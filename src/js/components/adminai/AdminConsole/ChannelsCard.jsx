import React, { useCallback, useEffect, useRef, useState } from 'react';
import QRCode from 'qrcode';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Select } from '@cloudflare/kumo/components/select';
import { Empty, Loader } from '@cloudflare/kumo';
import { SectionCard, cx } from '../../ui/AppPrimitives.jsx';
import { toast } from '../../../modules/toast.js';
import { useConfirmPress } from '../../../hooks/useConfirmPress.js';
import { MessageSquare, Send, Play, Trash, X, Plus, ChevronDown, WechatBrand, TelegramBrand, WeComBrand } from '../../Icons.jsx';
import ErrorBanner from './ErrorBanner.jsx';

/* ==================== 频道页（Telegram 频道 + 白名单） ==================== */

const EMPTY_FORM = {
  id: '',
  type: 'telegram',
  name: '',
  notificationChannelId: '',
  botTokenSet: false,
  botId: '',
  secret: '',
  notifyOnStart: true,
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
      notifyOnStart: channel.config?.notifyOnStart ?? true,
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
    const tgConfig = isTelegram ? { notifyOnStart: !!form.notifyOnStart } : null;
    setSaving(true);
    setError('');
    try {
      const url = form.id ? `/api/admin-ai/channels/${form.id}` : '/api/admin-ai/channels';
      const payload = form.id
        ? {
            name: form.name.trim(),
            ...(isTelegram ? { notificationChannelId: form.notificationChannelId } : {}),
            ...(weComConfig ? { config: weComConfig } : {}),
            ...(tgConfig ? { config: tgConfig } : {}),
          }
        : {
            type: form.type || 'telegram',
            name: form.name.trim(),
            enabled: isTelegram || isWeCom,
            ...(isTelegram ? { notificationChannelId: form.notificationChannelId } : {}),
            ...(weComConfig ? { config: weComConfig } : {}),
            ...(tgConfig ? { config: tgConfig } : {}),
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
              <Select alignItemWithTrigger
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
                <Select alignItemWithTrigger
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
              <div className="mt-3 flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-base px-3 py-2.5">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-kumo-strong">服务器启动时发送通知</div>
                  <p className="mt-0.5 text-[11px] leading-4 text-kumo-subtle">
                    频道启动（含面板服务器重启）时向白名单成员发送就绪提醒，关闭后不再打扰
                  </p>
                </div>
                <Switch
                  checked={form.notifyOnStart}
                  onCheckedChange={(v) => setFormField('notifyOnStart', !!v)}
                  aria-label="服务器启动时发送通知"
                />
              </div>
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


export default ChannelsCard;

