import { useEffect, useRef, useState } from 'react';
import { createLayoutThrottle } from '../modules/layoutThrottle.js';

// 容器宽度(节流版):替代 react-grid-layout 的 useContainerWidth。
// 它的 ResizeObserver 每帧 setState,一旦外层宽度在动画里连续变化(侧栏展开/收起),
// 整个网格与所有图块子树都会逐帧重渲染。这里把宽度更新收敛成约 10 次/秒,
// 变化结束后仍会落到最终宽度。
export const useThrottledContainerWidth = (initialWidth = 1280, windowMs) => {
  const containerRef = useRef(null);
  const [width, setWidth] = useState(initialWidth);
  const appliedRef = useRef(initialWidth);
  const throttleRef = useRef(null);
  if (!throttleRef.current) {
    throttleRef.current = createLayoutThrottle((next) => {
      appliedRef.current = next;
      setWidth(next);
    }, windowMs);
  }

  useEffect(() => {
    const node = containerRef.current;
    if (!node || typeof ResizeObserver !== 'function') return undefined;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const next = Math.round(entry.contentRect.width);
      if (next === appliedRef.current) return;
      throttleRef.current?.schedule(next);
    });
    observer.observe(node);
    return () => {
      observer.disconnect();
      throttleRef.current?.cancel();
    };
  }, []);

  return { containerRef, width };
};

export default useThrottledContainerWidth;