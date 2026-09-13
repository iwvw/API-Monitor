import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ClipboardText, Switch } from '@cloudflare/kumo';
import { AppCard, EmptyState, SectionCard, cx } from '../../components/ui/AppPrimitives.jsx';
import { Bot, Copy, Eye, EyeOff, Key, Plug, Shield } from '../../components/Icons.jsx';
import { SnippetBox } from './components.jsx';
import { fixedPanelClass } from './constants.js';

const POLICY_CARDS = [
  { value: 'minimal', title: '只读', Icon: Eye },
  { value: 'standard', title: '标准', Icon: Shield },
  { value: 'full', title: '全部权限', Icon: Key },
];

export default function AIAccessConsole({
  aiAccess,
  loading,
  error,
  keyVisible,
  setKeyVisible,
  onRefresh,
  onRotateKey,
  onToggleWrite,
  onSetPolicy,
  onCopy,
}) {
  if (loading) {
    return (
      <AppCard padding="lg">
        <SkeletonLine className="h-5 w-36" />
        <SkeletonLine className="mt-4 h-80 w-full" />
      </AppCard>
    );
  }

  if (error) {
    return (
      <EmptyState
        icon={Bot}
        title="AI 接入暂不可用"
        description={error}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            重试
          </Button>
        }
      />
    );
  }

  const agentKey = aiAccess?.agentKey || {};
  const endpoints = aiAccess?.endpoints || {};
  const guide = aiAccess?.guide || '';
  const policy = aiAccess?.policy || {};

  return (
    <div className="grid h-full min-h-0 min-w-0 gap-4 cq-xl:grid-cols-[minmax(360px,0.82fr)_minmax(0,1.18fr)]">
      <div
        className={cx(fixedPanelClass, 'min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1 pt-px')}
      >
        <SectionCard title="Agent Key" icon={<Key className="h-4 w-4 text-brand" />}>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <div className="min-w-0 flex-1 truncate rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2 font-mono text-xs font-semibold text-kumo-strong">
              {keyVisible ? agentKey.value : agentKey.masked}
            </div>
            <Button size="sm" variant="secondary" onClick={() => setKeyVisible(!keyVisible)}>
              {keyVisible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => onCopy(agentKey.value, 'Agent Key 已复制')}
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
            <Button size="sm" variant="destructive" onClick={onRotateKey} className="gap-1.5">
              <Key className="h-3.5 w-3.5" />
              <span>轮换</span>
            </Button>
          </div>
        </SectionCard>

        <SectionCard title="接入地址" icon={<Plug className="h-4 w-4 text-brand" />}>
          <div className="space-y-2">
            {Object.entries(endpoints).map(([key, value]) => (
              <div key={key} className="grid min-w-0 gap-1">
                <span className="text-xs font-semibold text-kumo-subtle">{key}</span>
                <ClipboardText
                  size="sm"
                  text={value}
                  className="min-w-0 w-full"
                  tooltip={{ text: '复制地址', copiedText: '地址已复制' }}
                />
              </div>
            ))}
          </div>
        </SectionCard>

        <SectionCard title="调用策略" icon={<Shield className="h-4 w-4 text-brand" />}>
          <div className="grid gap-2 text-xs text-kumo-subtle">
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <span>允许方法</span>
              <span className="font-mono text-kumo-strong">
                {(policy.allowedMethods || []).join(' / ') || '-'}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <span>请求体限制</span>
              <span className="font-mono text-kumo-strong">
                {policy.bodyLimitBytes ? `${Math.round(policy.bodyLimitBytes / 1024)} KB` : '-'}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <div className="flex items-center gap-2">
                <span>允许写入</span>
                <span className="hidden text-[10px] text-kumo-subtle cq-sm:inline">
                  开启后 Agent 才能执行 POST/PUT/PATCH/DELETE，全部写入都会审计
                </span>
              </div>
              <Switch
                checked={policy.writeEnabled === true}
                onCheckedChange={checked => onToggleWrite(Boolean(checked))}
                aria-label="允许 AI Agent 写入操作"
              />
            </div>
            <div className="grid gap-2 cq-md:grid-cols-3">
              {POLICY_CARDS.map(({ value, title, Icon }) => {
                const active = (policy.accessPolicy || 'standard') === value;
                return (
                  <button
                    key={value}
                    type="button"
                    onClick={() => onSetPolicy(value)}
                    aria-pressed={active}
                    aria-label={`切换到 ${title} 权限模式`}
className={cx(
                      'flex flex-col items-center gap-1.5 rounded-lg border px-3 py-3',
                      active
                        ? 'border-(--text-color-brand) bg-kumo-tint text-brand'
                        : 'border-kumo-line bg-kumo-recessed/25 text-kumo-strong hover:bg-kumo-recessed/50'
                    )}
                  >
                    <Icon className={cx('h-4 w-4', active ? 'text-brand' : 'text-kumo-strong')} />
                    <span className="text-xs font-medium">{title}</span>
                  </button>
                );
              })}
            </div>
          </div>
        </SectionCard>
      </div>

      <div
        className={cx(fixedPanelClass, 'min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1 pt-px')}
      >
        <SectionCard
          title="AI 接入指南"
          icon={<Bot className="h-4 w-4 text-brand" />}
          action={
            <Button size="sm" variant="secondary" onClick={onRefresh}>
              刷新
            </Button>
          }
          bodyClassName="grid gap-3"
        >
<div className="grid gap-2 cq-md:grid-cols-3">
            {[
              {
                step: '1',
                title: '复制密钥并注册',
                text: '复制有效 Agent Key，在 AI 客户端按指南注册 manifest / MCP 地址并以 Bearer 鉴权连接。',
              },
              {
                step: '2',
                title: '扫描目录',
                text: '用 list_apis 按模块/分组过滤查看可用接口，不拉全量、省 token。',
              },
              {
                step: '3',
                title: '按契约调用',
                text: '先用 get_route 取接口请求体 schema 与示例，再 call_api 调用，减少试错；写入需开启「允许写入」。',
              },
            ].map(item => (
              <div
                key={item.step}
                className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3"
              >
                <div className="mb-2 flex items-center gap-2">
                  <span className="flex h-5 w-5 items-center justify-center rounded border border-brand/30 bg-brand/10 font-mono text-[10px] font-semibold text-brand">
                    {item.step}
                  </span>
                  <div className="text-xs font-semibold text-kumo-strong">{item.title}</div>
                </div>
                <p className="text-xs leading-relaxed text-kumo-subtle">{item.text}</p>
              </div>
            ))}
          </div>
          <SnippetBox label="复制指南" value={guide} onCopy={onCopy} />
        </SectionCard>
      </div>
    </div>
  );
}
