import React from 'react';

export const EXPANDED_VALUE_TONES = {
  default: 'text-kumo-strong',
  brand: 'text-brand',
  success: 'text-kumo-success',
  warning: 'text-kumo-warning',
  info: 'text-kumo-info',
  danger: 'text-kumo-danger',
};

function ExpandedStatTileComponent({ label, value, caption, tone = 'default', className = '', captionClassName = '', inline = false }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  if (inline) {
    return (
      <div className={`flex min-w-0 items-center justify-between gap-2.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-2 ${className}`}>
        <span className="shrink-0 text-[11px] font-medium text-kumo-subtle">{label}</span>
        <span className={`min-w-0 truncate text-right text-sm font-semibold tabular-nums ${EXPANDED_VALUE_TONES[tone] || EXPANDED_VALUE_TONES.default}`} title={String(displayValue)}>
          {displayValue}
        </span>
      </div>
    );
  }
  return (
    <div className={`min-w-0 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-2.5 py-2 ${className}`}>
      <div className="text-[10px] font-medium text-kumo-subtle">{label}</div>
      <div className={`mt-1 truncate text-sm font-semibold tabular-nums ${EXPANDED_VALUE_TONES[tone] || EXPANDED_VALUE_TONES.default}`} title={String(displayValue)}>
        {displayValue}
      </div>
      {caption && (
        <div className={`mt-1 truncate text-[10px] font-medium text-kumo-subtle ${captionClassName}`} title={String(caption)}>
          {caption}
        </div>
      )}
    </div>
  );
}

export const ExpandedStatTile = React.memo(ExpandedStatTileComponent, (prev, next) => (
  prev.label === next.label
  && prev.value === next.value
  && prev.caption === next.caption
  && prev.tone === next.tone
  && prev.className === next.className
  && prev.inline === next.inline
  && prev.captionClassName === next.captionClassName
));
