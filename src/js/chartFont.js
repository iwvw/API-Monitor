import { createLayoutThrottle } from './modules/layoutThrottle.js';

const patchedEchartsModules = new WeakMap();
const liveChartInstances = new Set();
let fontWatcherStarted = false;

// 待重排的图表:同一个实例只保留最后一次请求。
// ResizeObserver 会在每一帧回调里驱动图表 resize,而侧栏展开/收起这类宽度动画
// 会连续改变几十帧;几十个实例各自重排一次就足够吃掉整帧预算,动画必然掉帧。
// 这里把 resize 收敛成一个节流任务,动画期间约 10 次/秒,结束后补一次最终尺寸。
const pendingChartResizes = new Map();

const flushChartResizes = () => {
  if (pendingChartResizes.size === 0) return;
  const jobs = [...pendingChartResizes.values()];
  pendingChartResizes.clear();
  for (const job of jobs) {
    try {
      job();
    } catch {
      // 图表可能在排队期间被销毁
    }
  }
};

const chartResizeThrottle = createLayoutThrottle(flushChartResizes);

export const getSiteChartFontFamily = () => {
  if (typeof document === 'undefined' || !document.body) return '';
  return window.getComputedStyle(document.body).fontFamily || '';
};

const getSiteChartFontWeight = () => {
  if (typeof document === 'undefined' || !document.body) return '';
  return window.getComputedStyle(document.body).fontWeight || '';
};

const mergeChartFont = (option) => {
  if (!option || typeof option !== 'object' || Array.isArray(option)) return option;
  const base = {};
  const fontFamily = getSiteChartFontFamily();
  const fontWeight = getSiteChartFontWeight();
  if (fontFamily) base.fontFamily = fontFamily;
  if (fontWeight) base.fontWeight = fontWeight;
  if (!base.fontFamily && !base.fontWeight) return option;
  const textStyle =
    option.textStyle && typeof option.textStyle === 'object' && !Array.isArray(option.textStyle)
      ? option.textStyle
      : {};
  return { ...option, textStyle: { ...base, ...textStyle } };
};

const patchChartInstance = (chart) => {
  const originalSetOption = chart.setOption.bind(chart);
  chart.setOption = (option, ...rest) => originalSetOption(mergeChartFont(option), ...rest);
  // resize 走节流队列(见上方注释),不直接同步重排。
  const originalResize = chart.resize.bind(chart);
  chart.resize = (...args) => {
    pendingChartResizes.set(chart, () => originalResize(...args));
    chartResizeThrottle.schedule();
  };
  const originalDispose = chart.dispose.bind(chart);
  chart.dispose = () => {
    pendingChartResizes.delete(chart);
    liveChartInstances.delete(chart);
    originalDispose();
  };
  liveChartInstances.add(chart);
  return chart;
};

export const createSiteFontEcharts = (baseEcharts) => {
  if (!baseEcharts || typeof baseEcharts.init !== 'function') return baseEcharts;
  const cached = patchedEchartsModules.get(baseEcharts);
  if (cached) return cached;
  const patched = {
    ...baseEcharts,
    init: (...args) => patchChartInstance(baseEcharts.init(...args)),
  };
  patchedEchartsModules.set(baseEcharts, patched);
  return patched;
};

export const refreshSiteChartFonts = () => {
  for (const chart of liveChartInstances) {
    try {
      chart.setOption(mergeChartFont({}));
    } catch {
      // 图表可能在刷新间隙被销毁
    }
  }
};

// 跨图表 tooltip 互斥：多个 echarts 实例各自维护 tooltip，切到另一个图表时
// 前一个实例的 tooltip 不会自动消失（echarts 只保证实例内部互斥）。移动端点击
// 后尤其明显，会出现两个 tooltip 并存。进入某个图表时主动隐藏其余实例的 tooltip。
export const hideOtherChartTips = (activeChart) => {
  for (const chart of liveChartInstances) {
    if (chart === activeChart) continue;
    try {
      chart.dispatchAction({ type: 'hideTip' });
    } catch {
      // 实例可能已销毁
    }
  }
};

export const ensureSiteChartFont = () => {
  if (fontWatcherStarted || typeof window === 'undefined') return;
  fontWatcherStarted = true;
  if (typeof MutationObserver !== 'undefined') {
    const observer = new MutationObserver(() => refreshSiteChartFonts());
    if (document.body) {
      observer.observe(document.body, { attributes: true, attributeFilter: ['style'] });
    }
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['style'] });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
  }
  if (document.fonts?.ready) {
    document.fonts.ready.then(() => refreshSiteChartFonts()).catch(() => {});
  }
  document.fonts?.addEventListener?.('loadingdone', () => refreshSiteChartFonts());
};
