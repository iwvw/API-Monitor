import { useEffect, useRef, useState } from 'react';
import { createLayoutThrottle } from '../modules/layoutThrottle.js';

// 给「布局尺寸」用的节流值:输入连续变化(容器宽度随侧栏宽度动画变化)时,
// 输出按 LAYOUT_THROTTLE_MS 的节奏跟进,并在变化结束后落到最终值。
// 用它的地方会跟着尺寸重排,所以不能每帧都拿到新值。
export const useThrottledLayoutValue = (value, windowMs) => {
  const [applied, setApplied] = useState(value);
  const throttleRef = useRef(null);
  if (!throttleRef.current) {
    throttleRef.current = createLayoutThrottle(next => setApplied(next), windowMs);
  }

  useEffect(() => {
    throttleRef.current?.schedule(value);
  }, [value]);

  useEffect(() => () => throttleRef.current?.cancel(), []);

  return applied;
};

export default useThrottledLayoutValue;
