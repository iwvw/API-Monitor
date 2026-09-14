import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { AppCard, StatusBadge, cx } from '../../components/ui/AppPrimitives.jsx';
import { Copy } from '../../components/Icons.jsx';
import { methodClassName } from './utils.js';

export function StatCard({ icon: Icon, label, value, tone = 'brand' }) {
  const toneClass =
    {
      brand: 'bg-kumo-info/6 text-kumo-info',
      success: 'bg-kumo-success/6 text-kumo-success',
      warning: 'bg-kumo-warning/8 text-kumo-warning',
      info: 'bg-brand/7 text-brand',
    }[tone] || 'bg-kumo-info/6 text-kumo-info';

  return (
    <AppCard padding="none" className={cx('min-w-0 p-2 cq-sm:p-3', toneClass)}>
      <div className="flex items-center justify-between gap-2 text-[11px] text-kumo-subtle cq-sm:gap-3 cq-sm:text-xs">
        <span className="truncate">{label}</span>
        <span className="shrink-0">
          <Icon className="h-5 w-5" />
        </span>
      </div>
      <div className="mt-1">
        <div className="truncate font-mono text-sm font-semibold text-kumo-strong">
          {value}
        </div>
      </div>
    </AppCard>
  );
}

export function FilterSelect({ label, value, onValueChange, items }) {
  return (
    <Select alignItemWithTrigger
      size="sm"
      aria-label={label}
      value={value}
      onValueChange={onValueChange}
      items={items}
      className="w-full min-w-0 text-xs text-kumo-strong"
    />
  );
}

export function RouteMethodPills({ methods = [] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {methods.map(method => (
        <span
          key={method}
          className={cx(
            'inline-flex items-center rounded border px-1.5 py-0.5 font-mono text-[10px] font-semibold',
            methodClassName(method)
          )}
        >
          {method}
        </span>
      ))}
    </div>
  );
}

export function ParamTable({ title, items }) {
  if (!items?.length) return null;
  return (
    <div>
      <div className="mb-2 text-xs font-semibold text-kumo-subtle">{title}</div>
      <div className="space-y-2">
        {items.map(item => (
          <div
            key={`${title}:${item.in}:${item.name}`}
            className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs font-semibold text-kumo-strong">{item.name}</span>
              <StatusBadge tone="neutral">{item.in}</StatusBadge>
              <StatusBadge tone={item.required ? 'warning' : 'neutral'}>
                {item.required ? '必填' : '可选'}
              </StatusBadge>
            </div>
            <div className="mt-1 text-xs leading-relaxed text-kumo-subtle">
              {item.description || '-'}
            </div>
            {item.example ? (
              <div className="mt-1 font-mono text-[11px] text-kumo-subtle">
                例如: {item.example}
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}

export function InfoRow({ label, value }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-line/80 bg-kumo-recessed/30 px-3 py-2">
      <div className="text-[11px] font-semibold text-kumo-subtle">{label}</div>
      <div className="mt-1 truncate font-mono text-xs font-semibold text-kumo-strong">
        {value || '-'}
      </div>
    </div>
  );
}

export function SnippetBox({ label, value, onCopy }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-line bg-kumo-recessed/35">
      <div className="flex items-center justify-between gap-2 border-b border-kumo-line px-3 py-2">
        <div className="truncate text-xs font-semibold text-kumo-strong">{label}</div>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => onCopy(value, `${label} 已复制`)}
          className="gap-1.5"
        >
          <Copy className="h-3.5 w-3.5" />
          <span>复制</span>
        </Button>
      </div>
      <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11px] leading-relaxed text-kumo-subtle">
        {value}
      </pre>
    </div>
  );
}
