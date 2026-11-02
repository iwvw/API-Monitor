import React from 'react';
import { COMPACT_INLINE_BOX_CLASS } from './constants.js';
import { toNumber, clampPercent } from './utils.js';

function DenseUsageMeterComponent({ label, value, detail, indicatorClassName = '!bg-none !bg-brand', muted = false }) {
  const percent = clampPercent(toNumber(value, 0));
  const resolvedIndicatorClassName = muted ? '!bg-none !bg-kumo-subtle/55' : indicatorClassName;
  return (
    <div className={`relative h-8 min-w-[96px] w-full rounded-md bg-kumo-recessed/45 ${COMPACT_INLINE_BOX_CLASS}`}>
      <div className="absolute left-1.5 right-1.5 top-[3px] grid h-4 grid-cols-[minmax(0,1fr)_auto] items-center gap-1 text-[10px] leading-4 text-kumo-default">
        <span className="min-w-0 truncate text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-[11px] font-semibold leading-4 ${muted ? 'text-kumo-subtle' : 'text-kumo-default'}`}>{detail || `${Math.round(percent)}%`}</span>
      </div>
      <div className="absolute bottom-1 left-1.5 right-1.5 h-1.5 overflow-hidden rounded-full bg-kumo-fill">
        <div
          className={`absolute inset-y-0 left-0 rounded-full transition-[width] duration-300 ease-out ${resolvedIndicatorClassName}`}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

export const DenseUsageMeter = React.memo(DenseUsageMeterComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.detail === next.detail
  && prev.indicatorClassName === next.indicatorClassName
  && prev.muted === next.muted
));
