import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { cx } from './AppPrimitives.jsx';

export function MobileCardList({ className = '', children }) {
  return <div className={cx('flex min-w-0 flex-col gap-2 cq-md:hidden', className)}>{children}</div>;
}

export function MobileCard({ selected = false, clickable = false, className = '', children, ...props }) {
  return (
    <div
      {...props}
      className={cx(
        'min-w-0 rounded-lg border bg-kumo-base px-3 py-2.5 shadow-none',
        selected ? 'border-brand/60' : 'border-kumo-line',
        clickable && 'cursor-pointer active:bg-kumo-recessed/40',
        className
      )}
    >
      {children}
    </div>
  );
}

export function MobileCardFields({ className = '', children }) {
  return (
    <div className={cx('mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-[11px] leading-normal', className)}>
      {children}
    </div>
  );
}

export function MobileCardField({ label, className = '', children }) {
  return (
    <div className={cx('flex min-w-0 items-center justify-between gap-1.5', className)}>
      <span className="shrink-0 text-kumo-subtle">{label}</span>
      <span className="min-w-0 truncate text-right text-kumo-strong">{children}</span>
    </div>
  );
}

export function MobileCardActions({ className = '', children }) {
  return (
    <div className={cx('mt-2 flex items-center justify-end gap-1.5 border-t border-kumo-line pt-2', className)}>
      {children}
    </div>
  );
}

export function MobileCardSkeleton({ rows = 3 }) {
  return (
    <MobileCard>
      <SkeletonLine className="h-4 w-32" />
      {Array.from({ length: rows }).map((_, index) => (
        <SkeletonLine key={index} className="mt-2 h-3.5 w-full" />
      ))}
    </MobileCard>
  );
}

export function MobileCardEmpty({ children }) {
  return (
    <div className="rounded-lg border border-kumo-line bg-kumo-base px-3 py-8 text-center text-sm text-kumo-subtle">
      {children}
    </div>
  );
}