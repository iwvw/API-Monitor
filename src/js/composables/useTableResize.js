import { useState, useCallback, useEffect, useRef } from 'react';

export default function useTableResize(initialWidths) {
  const [widths, setWidths] = useState(initialWidths || []);
  const startX = useRef(0);
  const startWidth = useRef(0);
  const activeIndex = useRef(-1);

  const updateWidth = useCallback((clientX) => {
    if (activeIndex.current === -1) return;
    const deltaX = clientX - startX.current;
    const newWidth = Math.max(40, startWidth.current + deltaX);
    setWidths((prev) => {
      const next = [...prev];
      next[activeIndex.current] = newWidth;
      return next;
    });
  }, []);

  const onMouseMove = useCallback((e) => {
    updateWidth(e.clientX);
  }, [updateWidth]);

  const onTouchMove = useCallback((e) => {
    if (e.touches.length === 0) return;
    e.preventDefault();
    updateWidth(e.touches[0].clientX);
  }, [updateWidth]);

  const stopResize = useCallback(() => {
    activeIndex.current = -1;
    document.removeEventListener('mousemove', onMouseMove);
    document.removeEventListener('mouseup', stopResize);
    document.removeEventListener('touchmove', onTouchMove);
    document.removeEventListener('touchend', stopResize);
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
  }, [onMouseMove, onTouchMove]);

  const startResize = useCallback((index, e) => {
    e.preventDefault();
    e.stopPropagation();
    const clientX = e.touches?.[0]?.clientX ?? e.clientX;
    activeIndex.current = index;
    startX.current = clientX;
    startWidth.current = widths[index] || 150;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', stopResize);
    document.addEventListener('touchmove', onTouchMove, { passive: false });
    document.addEventListener('touchend', stopResize);
  }, [widths, onMouseMove, onTouchMove, stopResize]);

  useEffect(() => stopResize, [stopResize]);

  return [widths, startResize];
}
