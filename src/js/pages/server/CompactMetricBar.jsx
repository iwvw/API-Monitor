import React from 'react';

function CompactMetricBarComponent({ label, value, valueClassName, barClassName, color, width = '0%', boxed = false, muted = false }) {
  let resolvedWidth = typeof width === 'number' ? `${width}%` : String(width);
  if (!resolvedWidth.endsWith('%') && /^\d+(\.\d+)?$/.test(resolvedWidth)) {
    resolvedWidth = `${resolvedWidth}%`;
  }

  const boxClassName = boxed
    ? 'flex min-w-0 flex-col justify-center gap-0.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-2 py-1'
    : 'flex min-w-0 flex-col justify-center gap-0.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/25 px-1.5 py-0.5 cq-card-wide:w-auto cq-card-wide:min-w-14 cq-card-wide:border-0 cq-card-wide:bg-transparent cq-card-wide:px-0 cq-card-wide:py-0';

  const resolvedValueClassName = muted ? 'text-kumo-subtle' : valueClassName;
  const resolvedBarClassName = muted ? 'bg-kumo-subtle/55' : barClassName;
  const useCustomColor = !!color && !muted;

  return (
    <div className={boxClassName}>
      <div className="flex min-w-0 items-center justify-between gap-1">
        <span className="truncate font-normal">{label}</span>
        <span className={`shrink-0 font-semibold ${useCustomColor ? '' : resolvedValueClassName}`} style={useCustomColor ? { color } : undefined}>{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div
          className={`h-full transition-[width] duration-300 ease-out ${useCustomColor ? '' : resolvedBarClassName}`}
          style={{ width: resolvedWidth, backgroundColor: useCustomColor ? color : undefined }}
        ></div>
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
  && prev.boxed === next.boxed
  && prev.muted === next.muted
));
