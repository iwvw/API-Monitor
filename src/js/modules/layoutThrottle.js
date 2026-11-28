// 布局尺寸连续变化(侧栏展开/收起、窗口拖拽)时的节流器:
// 前缘立即执行,此后每个窗口最多执行一次,窗口末尾再补一次尾帧。
// 目的:宽度动画期间每一帧都会推动下游重算(整页网格重渲染 + 所有图表 resize),
// 节流后同一段时间只重算约 10 次,让动画本身跑在主线程空档上。
export const LAYOUT_THROTTLE_MS = 100;

const currentTime = () =>
  typeof performance !== 'undefined' && typeof performance.now === 'function'
    ? performance.now()
    : Date.now();

export const createLayoutThrottle = (run, windowMs = LAYOUT_THROTTLE_MS, clock = currentTime) => {
  let timer = 0;
  let lastRun = -Infinity;
  let pendingArgs = null;

  const flush = () => {
    timer = 0;
    if (pendingArgs === null) return;
    const args = pendingArgs;
    pendingArgs = null;
    lastRun = clock();
    run(...args);
  };

  // 只保留最后一次调用的参数:尺寸类更新关心的是最终值,中间态没有意义。
  const schedule = (...args) => {
    pendingArgs = args;
    if (timer) return;
    const wait = Math.max(0, lastRun + windowMs - clock());
    if (wait === 0) {
      flush();
      return;
    }
    timer = setTimeout(flush, wait);
  };

  const cancel = () => {
    if (timer) clearTimeout(timer);
    timer = 0;
    pendingArgs = null;
  };

  return { schedule, cancel };
};
