import { useEffect, useRef, useState } from 'react';

/**
 * 基于 setInterval 的轮询 hook：页面隐藏（document.hidden）时暂停，
 * 页面重新可见时立即执行一次回调并恢复轮询。
 *
 * @param {Function} callback 轮询回调
 * @param {number} intervalMs 轮询间隔（毫秒）
 * @param {Array} deps 额外依赖，变化时重建轮询
 */
export function useVisiblePolling(callback, intervalMs, deps = []) {
  const callbackRef = useRef(callback);

  useEffect(() => {
    callbackRef.current = callback;
  }, [callback]);

  useEffect(() => {
    let timer = null;

    const stop = () => {
      if (timer !== null) {
        window.clearInterval(timer);
        timer = null;
      }
    };

    const invoke = () => {
      const result = callbackRef.current?.();
      if (result && typeof result.then === 'function') result.catch(() => {});
    };

    const start = () => {
      if (timer !== null) return;
      timer = window.setInterval(invoke, intervalMs);
    };

    const handleVisibilityChange = () => {
      if (document.hidden) {
        stop();
      } else {
        invoke();
        start();
      }
    };

    if (!document.hidden) start();
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      stop();
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [intervalMs, ...deps]);
}

/**
 * 返回当前时间戳的 hook：按 intervalMs 更新，页面隐藏时暂停 tick，
 * 页面重新可见时立即刷新为最新时间并恢复。
 *
 * @param {number} [intervalMs=1000] tick 间隔（毫秒）
 * @returns {number} 当前时间戳（Date.now()）
 */
export function useNowTick(intervalMs = 1000) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    let timer = null;

    const stop = () => {
      if (timer !== null) {
        window.clearInterval(timer);
        timer = null;
      }
    };

    const start = () => {
      if (timer !== null) return;
      timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    };

    const handleVisibilityChange = () => {
      if (document.hidden) {
        stop();
      } else {
        setNow(Date.now());
        start();
      }
    };

    if (!document.hidden) start();
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      stop();
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [intervalMs]);

  return now;
}
