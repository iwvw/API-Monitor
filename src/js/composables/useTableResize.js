import { useState, useCallback, useRef } from 'react';

export default function useTableResize(initialWidths) {
  const [widths, setWidths] = useState(initialWidths || []);
  const startX = useRef(0);
  const startWidth = useRef(0);
  const activeIndex = useRef(-1);

  const onMouseMove = useCallback((e) => {
    if (activeIndex.current === -1) return;
    const deltaX = e.clientX - startX.current;
    const newWidth = Math.max(40, startWidth.current + deltaX);
    setWidths((prev) => {
      const next = [...prev];
      next[activeIndex.current] = newWidth;
      return next;
    });
  }, []);

  const onMouseUp = useCallback(() => {
    activeIndex.current = -1;
    document.removeEventListener('mousemove', onMouseMove);
    document.removeEventListener('mouseup', onMouseUp);
  }, [onMouseMove]);

  const startResize = useCallback((index, e) => {
    e.preventDefault();
    activeIndex.current = index;
    startX.current = e.clientX;
    startWidth.current = widths[index] || 150;
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  }, [widths, onMouseMove, onMouseUp]);

  return [widths, startResize];
}
