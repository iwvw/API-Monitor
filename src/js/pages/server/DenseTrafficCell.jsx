import { COMPACT_INLINE_BOX_CLASS } from './constants.js';
import { formatDenseFlowValue } from './utils.js';
import { FlowUnitBadge } from './FlowUnitBadge.jsx';
import { FlowArrow } from './FlowArrow.jsx';

export function DenseTrafficCell({ left, leftUnit, right, rightUnit, leftTitle, rightTitle, muted = false }) {
  const leftValue = formatDenseFlowValue(left);
  const rightValue = formatDenseFlowValue(right);
  return (
    <div className={`flex h-8 w-full min-w-[208px] shrink-0 items-center justify-center gap-1 overflow-hidden rounded-md bg-kumo-recessed/35 px-2 text-[14px] leading-none tabular-nums ${muted ? 'text-kumo-subtle' : 'text-kumo-strong'} ${COMPACT_INLINE_BOX_CLASS}`}>
      <span className="min-w-0 flex-1 truncate text-right" title={leftTitle || `${left}${leftUnit}`}>{leftValue}</span>
      <FlowUnitBadge unit={leftUnit} muted={muted} />
      <FlowArrow muted={muted}>&darr;</FlowArrow>
      <span aria-hidden="true" className={`-my-px mx-0.5 w-px self-stretch shrink-0 ${muted ? 'bg-kumo-line/70' : 'bg-kumo-interact/80'}`}></span>
      <FlowArrow muted={muted}>&uarr;</FlowArrow>
      <FlowUnitBadge unit={rightUnit} muted={muted} />
      <span className="min-w-0 flex-1 truncate text-left" title={rightTitle || `${right}${rightUnit}`}>{rightValue}</span>
    </div>
  );
}
