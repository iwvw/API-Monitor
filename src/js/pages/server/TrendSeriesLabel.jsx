export function TrendSeriesLabel({ name, color }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-[11px] font-medium leading-none text-kumo-subtle">
      <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: color }}></span>
      <span className="truncate">{name}</span>
    </span>
  );
}
