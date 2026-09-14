import React from 'react';

function CompactMetricBarComponent({ label, value, valueClassName, barClassName, color, width = '0%' }) {
  let resolvedWidth = typeof width === 'number' ? `${width}%` : String(width);
  if (!resolvedWidth.endsWith('%') && /^\d+(\.\d+)?$/.test(resolvedWidth)) {
    resolvedWidth = `${resolvedWidth}%`;
  }

  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2 py-1 cq-sm:w-14 cq-sm:border-0 cq-sm:bg-transparent cq-sm:px-0 cq-sm:py-0">
      <div className="flex min-w-0 items-center justify-between gap-1">
        <span className="truncate">{label}</span>
        <span className={`shrink-0 font-semibold ${color ? '' : valueClassName}`} style={color ? { color } : undefined}>{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className={`h-full transition-[width] duration-300 ease-out ${color ? '' : barClassName}`} style={{ width: resolvedWidth, backgroundColor: color || undefined }}></div>
      </div>
    </div>
  );
}

export const CompactMetricBar = React.memo(CompactMetricBarComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.valueClassName === next.valueClassName
  && prev.barClassName === next.barClassName
  && prev.color === next.color
  && prev.width === next.width
));
