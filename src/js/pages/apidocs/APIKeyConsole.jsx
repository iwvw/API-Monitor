import React from 'react';
import { CalendarDotsIcon } from '@phosphor-icons/react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { DatePicker, Label, Popover } from '@cloudflare/kumo';
import { AppCard, EmptyState, SectionCard, StatusBadge, cx } from '../../components/ui/AppPrimitives.jsx';
import {
  Activity,
  Copy,
  Edit,
  Eye,
  EyeOff,
  Key,
  Plus,
  RefreshCw,
  Shield,
  Trash,
  X,
} from '../../components/Icons.jsx';
import { StatCard } from './components.jsx';
import {
  API_KEY_EXPIRY_HOURS,
  API_KEY_EXPIRY_MINUTES,
  API_KEY_EXPIRY_PRESETS,
  API_KEY_KINDS,
  API_SCOPE_LABELS,
} from './constants.js';
import {
  apiKeyStatus,
  formatKeyTime,
  matchExpiryPreset,
  parseLocalDateTime,
  toLocalDateTimeInput,
} from './utils.js';

export default function APIKeyConsole({
  overview,
  loading,
  error,
  form,
  setForm,
  editingId,
  submitting,
  issuedSecret,
  onDismissSecret,
  onSave,
  onEdit,
  onCancelEdit,
  onToggle,
  onRotate,
  onRevoke,
  onRefresh,
  onCopy,
}) {
  if (loading && !overview) {
    return (
      <AppCard padding="lg">
        <SkeletonLine className="h-5 w-36" />
        <SkeletonLine className="mt-4 h-80 w-full" />
      </AppCard>
    );
  }

  if (error && !overview) {
    return (
      <EmptyState
        icon={Key}
        title="密钥管理暂不可用"
        description={error}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            重试
          </Button>
        }
      />
    );
  }

  const keys = overview?.keys || [];
  const summary = overview?.summary || {};
  const selectedKind = API_KEY_KINDS.find(item => item.value === form.kind) || API_KEY_KINDS[0];
  const selectedExpiryPreset = matchExpiryPreset(form.expiresAt);

  const toggleScope = scope => {
    setForm(current => ({
      ...current,
      scopes: current.scopes.includes(scope)
        ? current.scopes.filter(item => item !== scope)
        : [...current.scopes, scope],
    }));
  };

  const updateExpiryDate = date => {
    if (!date) return;
    setForm(current => {
      const existing = parseLocalDateTime(current.expiresAt);
      const next = new Date(date);
      next.setHours(existing?.getHours() ?? 23, existing?.getMinutes() ?? 59, 0, 0);
      return { ...current, expiresAt: toLocalDateTimeInput(next) };
    });
  };

  const updateExpiryTime = (part, value) => {
    setForm(current => {
      const next = parseLocalDateTime(current.expiresAt);
      if (!next) return current;
      if (part === 'hour') next.setHours(Number(value));
      if (part === 'minute') next.setMinutes(Number(value));
      return { ...current, expiresAt: toLocalDateTimeInput(next) };
    });
  };

  const applyExpiryPreset = preset => {
    setForm(current => {
      if (preset.days === 0) {
        return { ...current, expiresAt: '' };
      }
      const base = parseLocalDateTime(current.expiresAt);
      const next = new Date();
      next.setDate(next.getDate() + preset.days);
      next.setSeconds(0, 0);
      next.setHours(base?.getHours() ?? 23, base?.getMinutes() ?? 59, 0, 0);
      return { ...current, expiresAt: toLocalDateTimeInput(next) };
    });
  };

  return (
    <div className="grid h-full min-h-0 min-w-0 gap-4 cq-xl:grid-cols-[minmax(320px,0.72fr)_minmax(0,1.28fr)]">
      <div className="min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1 pt-px">
        <div className="grid grid-cols-2 gap-3">
          <StatCard icon={Key} label="密钥总数" value={summary.total || 0} />
          <StatCard icon={Shield} label="使用中" value={summary.active || 0} tone="success" />
          <StatCard icon={Activity} label="已过期" value={summary.expired || 0} tone="warning" />
          <StatCard
            icon={X}
            label="停用 / 撤销"
            value={summary.revoked || 0}
            tone="info"
          />
        </div>

        {issuedSecret && (
          <SectionCard
            title="新密钥仅显示一次"
            icon={<Key className="h-4 w-4 text-kumo-warning" />}
            action={
              <Button size="sm" variant="ghost" onClick={onDismissSecret} aria-label="关闭">
                <X className="h-3.5 w-3.5" />
              </Button>
            }
          >
            <div className="space-y-2">
              <div className="break-all rounded-md border border-kumo-warning/30 bg-kumo-warning/8 px-3 py-2 font-mono text-xs font-semibold text-kumo-strong">
                {issuedSecret}
              </div>
              <Button
                size="sm"
                variant="primary"
                onClick={() => onCopy(issuedSecret, 'API Key 已复制')}
                className="gap-1.5"
              >
                <Copy className="h-3.5 w-3.5" />
                <span>复制密钥</span>
              </Button>
            </div>
          </SectionCard>
        )}

        <SectionCard
          title={editingId ? '编辑密钥' : '生成密钥'}
          icon={<Plus className="h-4 w-4 text-brand" />}
          bodyClassName="space-y-3"
        >
          <Input
            size="sm"
            value={form.name}
            onChange={event => setForm(current => ({ ...current, name: event.target.value }))}
            placeholder="密钥名称，例如 Chrome 插件"
            aria-label="密钥名称"
            className="text-xs"
          />
          <Select alignItemWithTrigger
            size="sm"
            aria-label="密钥类型"
            value={form.kind}
            disabled={Boolean(editingId)}
            onValueChange={kind => setForm(current => ({ ...current, kind, scopes: [] }))}
            items={API_KEY_KINDS.map(({ value, label }) => ({ value, label }))}
            className="text-xs"
          />
          <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs text-kumo-subtle">
            <div className="flex items-center justify-between gap-2">
              <span>{selectedKind.scope}</span>
              <span className="font-mono text-kumo-strong">{selectedKind.prefix}</span>
            </div>
          </div>
          {form.kind === 'api' && (
            <div className="grid gap-2 cq-sm:grid-cols-2">
              {[
                { value: 'api:read', label: '读取后台 API' },
                { value: 'api:write', label: '修改后台 API' },
              ].map(scope => (
                <label
                  key={scope.value}
                  className="flex cursor-pointer items-center gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs text-kumo-strong"
                >
                  <Checkbox
                    checked={form.scopes.includes(scope.value)}
                    onCheckedChange={() => toggleScope(scope.value)}
                    aria-label={scope.label}
                  />
                  <span>{scope.label}</span>
                </label>
              ))}
            </div>
          )}
          <div className="space-y-2">
            <Label>
              过期时间
              <span className="font-normal text-kumo-subtle">（可选）</span>
            </Label>
            <div className="flex flex-wrap gap-2">
              {API_KEY_EXPIRY_PRESETS.map(preset => {
                const active = selectedExpiryPreset === preset.value;
                return (
                  <Button
                    key={preset.value}
                    size="sm"
                    variant={active ? 'primary' : 'secondary'}
                    onClick={() => applyExpiryPreset(preset)}
                    className={cx(
                      'min-w-[4.5rem]',
                      active && 'shadow-[0_0_0_1px_rgba(255,255,255,0.12)]'
                    )}
                  >
                    {preset.label}
                  </Button>
                );
              })}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Popover>
                <Popover.Trigger
                  render={
                    <Button
                      size="sm"
                      variant="outline"
                      icon={CalendarDotsIcon}
                      className="min-w-[12.5rem] justify-start font-normal cq-sm:min-w-[13.5rem]"
                    />
                  }
                >
                  <span className="truncate">
                    {form.expiresAt ? formatKeyTime(form.expiresAt) : '长期有效'}
                  </span>
                </Popover.Trigger>
                <Popover.Content className="p-3">
                  <DatePicker
                    size="sm"
                    mode="single"
                    selected={parseLocalDateTime(form.expiresAt)}
                    onChange={updateExpiryDate}
                  />
                  {form.expiresAt && (
                    <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
                      <Button
                        size="xs"
                        variant="ghost"
                        onClick={() => setForm(current => ({ ...current, expiresAt: '' }))}
                      >
                        清除
                      </Button>
                    </div>
                  )}
                </Popover.Content>
              </Popover>
              <div className="flex items-center gap-1.5">
                <Select alignItemWithTrigger
                  size="sm"
                  aria-label="过期小时"
                  disabled={!form.expiresAt}
                  value={form.expiresAt.slice(11, 13)}
                  onValueChange={value => updateExpiryTime('hour', value)}
                  items={API_KEY_EXPIRY_HOURS}
                />
                <span className="text-center text-sm text-kumo-subtle">:</span>
                <Select alignItemWithTrigger
                  size="sm"
                  aria-label="过期分钟"
                  disabled={!form.expiresAt}
                  value={form.expiresAt.slice(14, 16)}
                  onValueChange={value => updateExpiryTime('minute', value)}
                  items={API_KEY_EXPIRY_MINUTES}
                />
              </div>
            </div>
            <p className="text-[11px] text-kumo-subtle">留空表示长期有效，建议使用 90 天并定期轮换。</p>
          </div>
          {editingId && (
            <Select alignItemWithTrigger
              size="sm"
              aria-label="启用状态"
              value={form.enabled ? 'true' : 'false'}
              onValueChange={value =>
                setForm(current => ({ ...current, enabled: value === 'true' }))
              }
              items={[
                { value: 'true', label: '启用' },
                { value: 'false', label: '停用' },
              ]}
              className="text-xs"
            />
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="primary"
              disabled={submitting}
              onClick={onSave}
              className="gap-1.5"
            >
              {editingId ? <Edit className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
              <span>{editingId ? '保存修改' : '生成密钥'}</span>
            </Button>
            {editingId && (
              <Button size="sm" variant="secondary" onClick={onCancelEdit} className="gap-1.5">
                <X className="h-3.5 w-3.5" />
                <span>取消</span>
              </Button>
            )}
          </div>
        </SectionCard>
      </div>

      <SectionCard
        title="密钥与使用监控"
        icon={<Activity className="h-4 w-4 text-brand" />}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh} loading={loading} aria-label="刷新密钥监控">
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
        }
        className="min-h-0"
        bodyClassName="h-full min-h-0 overflow-y-auto"
      >
        {keys.length === 0 ? (
          <div className="flex h-full min-h-48 items-center justify-center text-xs text-kumo-subtle">
            尚未生成密钥
          </div>
        ) : (
          <div className="space-y-2">
            {keys.map(key => {
              const status = apiKeyStatus(key);
              const kind = API_KEY_KINDS.find(item => item.value === key.kind);
              return (
                <div
                  key={key.id}
                  className={cx(
                    'rounded-md border bg-kumo-recessed/20 p-3',
                    editingId === key.id ? 'border-brand/70' : 'border-kumo-line/80'
                  )}
                >
                  <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="truncate text-xs font-semibold text-kumo-strong">{key.name}</span>
                        <StatusBadge tone={status.tone}>{status.label}</StatusBadge>
                        <span className="rounded border border-kumo-line px-1.5 py-0.5 text-[10px] text-kumo-subtle">
                          {kind?.label || key.kind}
                        </span>
                      </div>
                      <div className="mt-2 break-all font-mono text-[11px] font-semibold text-kumo-strong">
                        {key.maskedKey}
                      </div>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {(key.scopes || []).map(scope => (
                          <span
                            key={scope}
                            className="rounded border border-brand/20 bg-brand/7 px-1.5 py-0.5 text-[10px] text-brand"
                          >
                            {API_SCOPE_LABELS[scope] || scope}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center gap-1">
                      <Button size="sm" shape="square" variant="secondary" onClick={() => onEdit(key)} aria-label="编辑密钥" title="编辑">
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      {!key.revokedAt && (
                        <Button
                          size="sm"
                          shape="square"
                          variant="secondary"
                          onClick={() => onToggle(key)}
                          aria-label={key.enabled ? '停用密钥' : '启用密钥'}
                          title={key.enabled ? '停用' : '启用'}
                        >
                          {key.enabled ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                        </Button>
                      )}
                      <Button size="sm" shape="square" variant="secondary" onClick={() => onRotate(key)} aria-label="轮换密钥" title="轮换">
                        <RefreshCw className="h-3.5 w-3.5" />
                      </Button>
                      {!key.revokedAt && (
                        <Button size="sm" shape="square" variant="secondary-destructive" onClick={() => onRevoke(key)} aria-label="撤销密钥" title="撤销">
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </div>
                  <div className="mt-3 grid gap-2 border-t border-kumo-line/70 pt-3 text-[11px] text-kumo-subtle cq-sm:grid-cols-2 cq-2xl:grid-cols-4">
                    <div><span className="block">请求次数</span><strong className="font-mono text-kumo-strong">{Number(key.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}</strong></div>
                    <div><span className="block">过期时间</span><strong className="font-normal text-kumo-strong">{formatKeyTime(key.expiresAt)}</strong></div>
                    <div><span className="block">最后使用</span><strong className="font-normal text-kumo-strong">{formatKeyTime(key.lastUsedAt)}</strong></div>
                    <div className="min-w-0"><span className="block">最后 IP</span><strong className="block truncate font-mono font-normal text-kumo-strong" title={key.lastIpAddress || ''}>{key.lastIpAddress || '-'}</strong></div>
                  </div>
                  {key.lastUserAgent && (
                    <div className="mt-2 truncate text-[10px] text-kumo-subtle" title={key.lastUserAgent}>
                      {key.lastUserAgent}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>
    </div>
  );
}
