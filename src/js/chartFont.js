const patchedEchartsModules = new WeakMap();
const liveChartInstances = new Set();
let fontWatcherStarted = false;

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
  const originalDispose = chart.dispose.bind(chart);
  chart.dispose = () => {
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
