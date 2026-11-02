import { FLOW_UNIT_BADGE_CLASS, getFlowUnitClassName } from '../../modules/flowUnits.js';

export function FlowUnitBadge({ unit, muted = false }) {
  return (
    <span className={`${FLOW_UNIT_BADGE_CLASS} ${muted ? 'border-kumo-line/70 bg-kumo-recessed text-kumo-subtle' : getFlowUnitClassName(unit)}`}>
      {unit || 'B'}
    </span>
  );
}
