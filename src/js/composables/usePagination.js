/**
 * usePagination Composable
 * 管理分页状态的可复用逻辑
 */
import { ref, computed, watch } from 'vue';

/**
 * 创建分页状态管理
 * @param {Object} options - 配置选项
 * @param {number} options.initialPage - 初始页码
 * @param {number} options.pageSize - 每页数量
 * @param {Function} options.fetchData - 获取数据的函数
 * @returns {Object} 分页状态和方法
 */
export function usePagination(options = {}) {
  const { initialPage = 1, pageSize = 20, fetchData } = options;

  const page = ref(initialPage);
  const size = ref(pageSize);
  const total = ref(0);
  const isLoading = ref(false);
  const error = ref('');
  const items = ref([]);

  // 总页数
  const totalPages = computed(() => {
    return Math.ceil(total.value / size.value) || 1;
  });

  // 是否有上一页
  const hasPrev = computed(() => page.value > 1);

  // 是否有下一页
  const hasNext = computed(() => page.value < totalPages.value);

  // 当前范围描述
  const rangeText = computed(() => {
    const start = (page.value - 1) * size.value + 1;
    const end = Math.min(page.value * size.value, total.value);
    return `${start}-${end} / ${total.value}`;
  });

  // 页码列表（用于渲染分页按钮）
  const pageNumbers = computed(() => {
    const pages = [];
    const maxVisible = 5;
    const half = Math.floor(maxVisible / 2);

    let start = Math.max(1, page.value - half);
    let end = Math.min(totalPages.value, start + maxVisible - 1);

    if (end - start + 1 < maxVisible) {
      start = Math.max(1, end - maxVisible + 1);
    }

    for (let i = start; i <= end; i++) {
      pages.push(i);
    }

    return pages;
  });

  /**
   * 加载数据
   */
  async function load() {
    if (!fetchData || isLoading.value) return;

    isLoading.value = true;
    error.value = '';

    try {
      const result = await fetchData({
        page: page.value,
        pageSize: size.value,
      });

      if (result) {
        items.value = result.items || result.data || [];
        total.value = result.total || 0;
      }
    } catch (err) {
      error.value = err.message || '加载失败';
      items.value = [];
    } finally {
      isLoading.value = false;
    }
  }

  /**
   * 跳转到指定页
   * @param {number} newPage - 目标页码
   */
  function goToPage(newPage) {
    if (newPage < 1 || newPage > totalPages.value) return;
    page.value = newPage;
    load();
  }

  /**
   * 上一页
   */
  function prevPage() {
    if (hasPrev.value) {
      goToPage(page.value - 1);
    }
  }

  /**
   * 下一页
   */
  function nextPage() {
    if (hasNext.value) {
      goToPage(page.value + 1);
    }
  }

  /**
   * 首页
   */
  function firstPage() {
    goToPage(1);
  }

  /**
   * 末页
   */
  function lastPage() {
    goToPage(totalPages.value);
  }

  /**
   * 刷新当前页
   */
  function refresh() {
    load();
  }

  /**
   * 重置到首页并刷新
   */
  function reset() {
    page.value = 1;
    load();
  }

  /**
   * 更新每页数量
   * @param {number} newSize - 新的每页数量
   */
  function setPageSize(newSize) {
    size.value = newSize;
    page.value = 1;
    load();
  }

  return {
    // 状态
    page,
    pageSize: size,
    total,
    totalPages,
    isLoading,
    error,
    items,

    // 计算属性
    hasPrev,
    hasNext,
    rangeText,
    pageNumbers,

    // 方法
    load,
    goToPage,
    prevPage,
    nextPage,
    firstPage,
    lastPage,
    refresh,
    reset,
    setPageSize,
  };
}

export default usePagination;
