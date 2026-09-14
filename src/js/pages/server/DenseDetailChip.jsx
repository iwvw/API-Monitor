export function DenseDetailChip({ label, value, className = '', valueClassName = 'text-kumo-strong' }) {
  const displayValue = value === 0 ? 0 : (value || '-');
  return (
    <div className={`flex h-7 min-w-0 items-center overflow-hidden rounded-md border border-kumo-line/60 bg-kumo-recessed/20 px-2.5 text-[11px] shadow-none ${className}`}>
      <span className="shrink-0 font-medium text-kumo-subtle">{label}</span>
      <span aria-hidden="true" className="mx-2 h-3 w-px shrink-0 bg-kumo-line/70"></span>
      <span className={`min-w-0 flex-1 truncate text-right font-semibold tabular-nums ${valueClassName}`} title={String(displayValue)}>{displayValue}</span>
    </div>
  );
}
