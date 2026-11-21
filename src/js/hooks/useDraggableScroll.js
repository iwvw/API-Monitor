import { useCallback, useEffect, useRef, useState } from 'react';

const DRAG_THRESHOLD = 5;
const DRAG_SCROLL_IGNORE_SELECTOR = [
  'button',
  'a',
  'input',
  'textarea',
  'select',
  '[role="button"]',
  '[contenteditable="true"]',
  '[data-drag-scroll-ignore="true"]',
].join(',');

const releasePointerCapture = (element, pointerId) => {
  if (!element?.hasPointerCapture?.(pointerId)) return;
  try {
    element.releasePointerCapture(pointerId);
  } catch {
    // Ignore browsers that reject redundant release calls.
  }
};

export function useDraggableScroll(viewportRef, { disabled = false } = {}) {
  const dragStateRef = useRef(null);
  const clearSuppressTimerRef = useRef(null);
  const suppressClickRef = useRef(false);
  const [isDragging, setIsDragging] = useState(false);

  const clearSuppressionSoon = useCallback(() => {
    if (clearSuppressTimerRef.current) {
      window.clearTimeout(clearSuppressTimerRef.current);
    }
    clearSuppressTimerRef.current = window.setTimeout(() => {
      suppressClickRef.current = false;
      clearSuppressTimerRef.current = null;
    }, 0);
  }, []);

  const stopDragging = useCallback((pointerId = null) => {
    const element = viewportRef.current;
    if (pointerId !== null) releasePointerCapture(element, pointerId);
    dragStateRef.current = null;
    setIsDragging(false);
    document.body.style.removeProperty('user-select');
  }, [viewportRef]);

  useEffect(() => {
    if (!disabled) return undefined;
    stopDragging();
    return undefined;
  }, [disabled, stopDragging]);

  useEffect(() => () => {
    stopDragging();
    if (clearSuppressTimerRef.current) {
      window.clearTimeout(clearSuppressTimerRef.current);
    }
  }, [stopDragging]);

  const onPointerDownCapture = useCallback((event) => {
    if (disabled) return;
    if (event.button !== 0 || event.isPrimary === false) return;

    const element = viewportRef.current;
    if (!element) return;

    const target = event.target;
    if (target && typeof target.closest === 'function' && target.closest(DRAG_SCROLL_IGNORE_SELECTOR)) return;

    const canScrollX = element.scrollWidth - element.clientWidth > 1;
    const canScrollY = element.scrollHeight - element.clientHeight > 1;
    if (!canScrollX && !canScrollY) return;

    if (clearSuppressTimerRef.current) {
      window.clearTimeout(clearSuppressTimerRef.current);
      clearSuppressTimerRef.current = null;
    }
    suppressClickRef.current = false;
    dragStateRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      scrollLeft: element.scrollLeft,
      scrollTop: element.scrollTop,
      canScrollX,
      canScrollY,
      moved: false,
    };

    try {
      element.setPointerCapture?.(event.pointerId);
    } catch {
    }
  }, [disabled, viewportRef]);

  const onPointerMoveCapture = useCallback((event) => {
    const state = dragStateRef.current;
    const element = viewportRef.current;
    if (!state || !element || event.pointerId !== state.pointerId) return;

    const deltaX = event.clientX - state.startX;
    const deltaY = event.clientY - state.startY;
    if (!state.moved && Math.abs(deltaX) < DRAG_THRESHOLD && Math.abs(deltaY) < DRAG_THRESHOLD) {
      return;
    }

    if (!state.moved) {
      state.moved = true;
      suppressClickRef.current = true;
      setIsDragging(true);
      document.body.style.setProperty('user-select', 'none');
    }

    if (state.canScrollX) element.scrollLeft = state.scrollLeft - deltaX;
    if (state.canScrollY) element.scrollTop = state.scrollTop - deltaY;
    event.preventDefault();
  }, [viewportRef]);

  const finishPointer = useCallback((event) => {
    const state = dragStateRef.current;
    if (!state || event.pointerId !== state.pointerId) return;
    const moved = state.moved;
    stopDragging(event.pointerId);
    if (moved) clearSuppressionSoon();
  }, [clearSuppressionSoon, stopDragging]);

  const onPointerUpCapture = useCallback((event) => {
    finishPointer(event);
  }, [finishPointer]);

  const onPointerCancelCapture = useCallback((event) => {
    finishPointer(event);
  }, [finishPointer]);

  const onLostPointerCapture = useCallback((event) => {
    const state = dragStateRef.current;
    if (!state || event.pointerId !== state.pointerId) return;
    const moved = state.moved;
    stopDragging();
    if (moved) clearSuppressionSoon();
  }, [clearSuppressionSoon, stopDragging]);

  const onClickCapture = useCallback((event) => {
    if (!suppressClickRef.current) return;
    suppressClickRef.current = false;
    event.preventDefault();
    event.stopPropagation();
  }, []);

  return {
    isDragging,
    dragHandlers: {
      onPointerDownCapture,
      onPointerMoveCapture,
      onPointerUpCapture,
      onPointerCancelCapture,
      onLostPointerCapture,
      onClickCapture,
    },
  };
}

export default useDraggableScroll;
