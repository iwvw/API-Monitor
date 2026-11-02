// 紧凑仪表行：label + 细进度条 + 值（完整显示不截断，hover 显示详情）。hideValue 时只留 label + 进度条。
import React from 'react';

export default function CompactMeter({ label, usage, used, total, tone = 'brand', detail, title, hideValue = false }) {
  const pct = Math.min(100, Math.max(0, Number(usage) || 0));
  const barColor = {
    brand: 'bg-brand',
    info: 'bg-kumo-info',
    success: 'bg-kumo-success',
    warning: 'bg-kumo-warning',
  }[tone] || 'bg-brand';
  return (
    <div className="animate-tile-fade-up flex min-w-0 items-center gap-2 text-[11px] text-kumo-subtle" title={title}>
      <span className="w-8 shrink-0 truncate">{label}</span>
      <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className={`h-full rounded-full ${barColor}`} style={{ width: `${pct}%` }} />
      </div>
      {!hideValue && (
        <span className="w-36 shrink-0 truncate text-left tabular-nums text-kumo-default/80">{detail || `${pct.toFixed(0)}%`}</span>
      )}
    </div>
  );
}
