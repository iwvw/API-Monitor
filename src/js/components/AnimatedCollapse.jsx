import { useEffect, useState } from 'react';
import { Collapsible } from '@cloudflare/kumo/components/collapsible';

const cx = (...classes) => classes.filter(Boolean).join(' ');
const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';
const collapsePanelClassName = [
  'overflow-hidden',
  'transition-[height,opacity]',
  'duration-200',
  'ease-out',
  'motion-reduce:transition-none',
  'data-[open]:h-[var(--collapsible-panel-height)]',
  'data-[closed]:h-0',
  'data-[starting-style]:h-0',
  'data-[ending-style]:h-0',
  'data-[open]:opacity-100',
  'data-[closed]:opacity-0',
  'data-[starting-style]:opacity-0',
  'data-[ending-style]:opacity-0',
].join(' ');

export function AnimatedCollapse({
  open,
  onOpenChange,
  children,
  className = '',
  panelClassName = '',
  keepMounted = false,
}) {
  return (
    <Collapsible.Root open={open} onOpenChange={onOpenChange} className={className}>
      <Collapsible.Panel keepMounted={keepMounted} className={cx(collapsePanelClassName, panelClassName)}>
        {children}
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}

export function useDeferredOpen(open, delay = 220) {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    if (!open) {
      setReady(false);
      return undefined;
    }

    if (typeof window === 'undefined') {
      setReady(true);
      return undefined;
    }

    const prefersReducedMotion = window.matchMedia?.(REDUCED_MOTION_QUERY)?.matches;
    const wait = prefersReducedMotion ? 0 : delay;

    if (wait <= 0) {
      setReady(true);
      return undefined;
    }

    const timeout = window.setTimeout(() => {
      setReady(true);
    }, wait);

    return () => {
      window.clearTimeout(timeout);
    };
  }, [delay, open]);

  return ready;
}

export function DeferredRender({
  open,
  delay = 220,
  fallback = null,
  children,
}) {
  const ready = useDeferredOpen(open, delay);

  return ready ? children : fallback;
}

export default AnimatedCollapse;
