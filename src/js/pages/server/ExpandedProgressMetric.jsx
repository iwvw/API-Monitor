import React from 'react';
import { clampPercent, toNumber } from './utils.js';

function ExpandedProgressMetricComponent({
  label,
  value,
  detail,
  caption,
  indicatorClassName = '!bg-none !bg-brand',
  valueClassName = 'text-kumo-strong',
  muted = false,
}) {
  const percent = clampPercent(toNumber(value, 0));
  const displayValue = detail || `${Math.round(percent)}%`;
  const resolvedIndicatorClassName = muted ? '!bg-none !bg-kumo-subtle/55' : indicatorClassName;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2.5 py-2">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <span className="text-[11px] font-medium text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-sm font-semibold tabular-nums ${muted ? 'text-kumo-subtle' : valueClassName}`} title={String(displayValue)}>{displayValue}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-base">
        <div
          className={`h-full rounded-full transition-[width] duration-300 ease-out ${resolvedIndicatorClassName}`}
          style={{ width: `${percent}%` }}
        ></div>
      </div>
      {caption && (
        <div className="min-w-0 break-words text-[10px] font-medium leading-4 text-kumo-subtle" title={String(caption)}>
          {caption}
        </div>
      )}
    </div>
  );
}

export const ExpandedProgressMetric = React.memo(ExpandedProgressMetricComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.detail === next.detail
  && prev.caption === next.caption
  && prev.indicatorClassName === next.indicatorClassName
  && prev.valueClassName === next.valueClassName
  && prev.muted === next.muted
));
