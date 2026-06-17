import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Collapsible } from '@cloudflare/kumo';

const cx = (...classes) => classes.filter(Boolean).join(' ');
const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

const panelMotionClassName = [
  'overflow-hidden',
  'transition-[height,opacity]',
  'duration-160',
  'ease-out',
  'motion-reduce:transition-none',
  'data-[open]:animate-none',
  'data-[closed]:animate-none',
].join(' ');

function prefersReducedMotion() {
  return (
    typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia(REDUCED_MOTION_QUERY).matches
  );
}

function scheduleFrame(callback) {
  if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
    return window.requestAnimationFrame(callback);
  }

  return setTimeout(callback, 0);
}

function cancelFrame(frame) {
  if (typeof window !== 'undefined' && typeof window.cancelAnimationFrame === 'function') {
    window.cancelAnimationFrame(frame);
    return;
  }

  clearTimeout(frame);
}

export function AnimatedCollapse({
  open,
  children,
  className = '',
  panelClassName = '',
  keepMounted = false,
}) {
  const panelRef = useRef(null);
  const frameRef = useRef(null);
  const latestChildrenRef = useRef(children);
  const [rendered, setRendered] = useState(() => keepMounted || open);
  const [displayChildren, setDisplayChildren] = useState(children);
  const [height, setHeight] = useState(open ? 'auto' : '0px');
  const [opacity, setOpacity] = useState(open ? 1 : 0);

  latestChildrenRef.current = children;

  useEffect(() => {
    if (open) {
      setRendered(true);
      setDisplayChildren(children);
      return;
    }
    if (keepMounted) setRendered(true);
  }, [children, keepMounted, open]);

  useLayoutEffect(() => {
    if (!rendered) return undefined;

    const panel = panelRef.current;
    if (!panel) return undefined;

    if (frameRef.current !== null) {
      cancelFrame(frameRef.current);
      frameRef.current = null;
    }

    if (prefersReducedMotion()) {
      setHeight(open ? 'auto' : '0px');
      setOpacity(open ? 1 : 0);
      if (!open) {
        setDisplayChildren(latestChildrenRef.current);
        if (!keepMounted) setRendered(false);
      }
      return undefined;
    }

    const nextHeight = `${panel.scrollHeight}px`;
    if (open) {
      setHeight('0px');
      setOpacity(0);
      frameRef.current = scheduleFrame(() => {
        frameRef.current = null;
        setHeight(nextHeight);
        setOpacity(1);
      });
    } else {
      const currentHeight = panel.getBoundingClientRect().height || panel.scrollHeight;
      setHeight(`${currentHeight}px`);
      setOpacity(1);
      frameRef.current = scheduleFrame(() => {
        frameRef.current = null;
        setHeight('0px');
        setOpacity(0);
      });
    }

    return () => {
      if (frameRef.current !== null) {
        cancelFrame(frameRef.current);
        frameRef.current = null;
      }
    };
  }, [keepMounted, open, rendered]);

  const handleTransitionEnd = (event) => {
    if (event.target !== panelRef.current || event.propertyName !== 'height') return;

    if (open) {
      setHeight('auto');
      return;
    }

    setDisplayChildren(latestChildrenRef.current);
    if (!keepMounted) {
      setRendered(false);
    }
  };

  return (
    <Collapsible.Root open={open} className={className}>
      {rendered && (
        <Collapsible.Panel
          ref={panelRef}
          keepMounted
          aria-hidden={!open}
          className={cx(panelMotionClassName, panelClassName, prefersReducedMotion() ? 'opacity-100' : '')}
          style={{ height, opacity }}
          onTransitionEnd={handleTransitionEnd}
        >
          {displayChildren}
        </Collapsible.Panel>
      )}
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

    const reducedMotion = window.matchMedia?.(REDUCED_MOTION_QUERY)?.matches;
    const wait = reducedMotion ? 0 : delay;

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
