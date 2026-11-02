import React from 'react';

function ExpandedInfoChipComponent({ label, value, className = '', valueClassName = 'text-kumo-strong' }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  return (
    <div className={`flex min-h-8.5 min-w-0 items-center justify-between gap-2.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-1 text-[11px] ${className}`}>
      <span className="shrink-0 font-medium text-kumo-subtle">{label}</span>
      <span className={`min-w-0 truncate text-right font-semibold tabular-nums ${valueClassName}`} title={String(displayValue)}>{displayValue}</span>
    </div>
  );
}

export const ExpandedInfoChip = React.memo(ExpandedInfoChipComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.className === next.className
  && prev.valueClassName === next.valueClassName
));
