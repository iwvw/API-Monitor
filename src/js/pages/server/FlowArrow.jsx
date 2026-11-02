import { COMPACT_INLINE_SUBBOX_CLASS } from './constants.js';

export function FlowArrow({ children, muted = false }) {
  return (
    <span className={`inline-flex h-5 w-5 items-center justify-center rounded-[4px] bg-kumo-recessed/70 text-[14px] font-semibold leading-none ${muted ? 'text-kumo-subtle' : 'text-kumo-default'} ${COMPACT_INLINE_SUBBOX_CLASS}`}>
      {children}
    </span>
  );
}
