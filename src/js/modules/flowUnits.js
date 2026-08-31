export const FLOW_UNIT_BADGE_CLASS = 'inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-[4px] border px-1 font-mono text-[13px] font-semibold leading-none shadow-none';

export const getFlowUnitClassName = (unit) => {
  const normalized = String(unit || 'B').toUpperCase();
  if (normalized === 'K') return 'border-kumo-info/65 bg-kumo-info/25 text-kumo-info';
  if (normalized === 'M') return 'border-kumo-success/65 bg-kumo-success/25 text-kumo-success';
  if (normalized === 'G') return 'border-kumo-warning/65 bg-kumo-warning/25 text-kumo-warning';
  if (normalized === 'T') return 'border-kumo-danger/65 bg-kumo-danger/20 text-kumo-danger';
  if (normalized === 'P') return 'border-kumo-danger/75 bg-kumo-danger/25 text-kumo-danger';
  return 'border-kumo-interact/70 bg-kumo-recessed/70 text-kumo-default';
};
