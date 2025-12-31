/**
 * useResponsive Composable
 * 管理响应式布局状态
 */
import { ref, computed, onMounted, onUnmounted } from 'vue';

// 断点定义
const BREAKPOINTS = {
  xs: 0,
  sm: 576,
  md: 768,
  lg: 992,
  xl: 1200,
  xxl: 1400,
};

/**
 * 响应式布局状态管理
 * @returns {Object} 响应式状态和方法
 */
export function useResponsive() {
  const windowWidth = ref(typeof window !== 'undefined' ? window.innerWidth : 1024);
  const windowHeight = ref(typeof window !== 'undefined' ? window.innerHeight : 768);

  // 是否为移动端
  const isMobile = computed(() => windowWidth.value < BREAKPOINTS.md);

  // 是否为平板
  const isTablet = computed(
    () => windowWidth.value >= BREAKPOINTS.md && windowWidth.value < BREAKPOINTS.lg
  );

  // 是否为桌面端
  const isDesktop = computed(() => windowWidth.value >= BREAKPOINTS.lg);

  // 当前断点
  const currentBreakpoint = computed(() => {
    const width = windowWidth.value;
    if (width >= BREAKPOINTS.xxl) return 'xxl';
    if (width >= BREAKPOINTS.xl) return 'xl';
    if (width >= BREAKPOINTS.lg) return 'lg';
    if (width >= BREAKPOINTS.md) return 'md';
    if (width >= BREAKPOINTS.sm) return 'sm';
    return 'xs';
  });

  // 是否为横屏
  const isLandscape = computed(() => windowWidth.value > windowHeight.value);

  // 是否为竖屏
  const isPortrait = computed(() => windowHeight.value >= windowWidth.value);

  // 更新窗口尺寸
  function updateSize() {
    windowWidth.value = window.innerWidth;
    windowHeight.value = window.innerHeight;
  }

  // 检查是否大于等于指定断点
  function isAtLeast(breakpoint) {
    const bp = BREAKPOINTS[breakpoint] || 0;
    return windowWidth.value >= bp;
  }

  // 检查是否小于指定断点
  function isBelow(breakpoint) {
    const bp = BREAKPOINTS[breakpoint] || 0;
    return windowWidth.value < bp;
  }

  // 防抖处理
  let resizeTimer = null;
  function handleResize() {
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(updateSize, 100);
  }

  onMounted(() => {
    window.addEventListener('resize', handleResize);
    updateSize();
  });

  onUnmounted(() => {
    window.removeEventListener('resize', handleResize);
    if (resizeTimer) clearTimeout(resizeTimer);
  });

  return {
    windowWidth,
    windowHeight,
    isMobile,
    isTablet,
    isDesktop,
    currentBreakpoint,
    isLandscape,
    isPortrait,
    isAtLeast,
    isBelow,
    BREAKPOINTS,
  };
}

export default useResponsive;
