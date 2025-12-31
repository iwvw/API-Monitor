/**
 * useAsync Composable
 * 管理异步操作状态的可复用逻辑
 */
import { ref, computed } from 'vue';

/**
 * 创建异步操作状态管理
 * @param {Function} asyncFn - 异步函数
 * @param {Object} options - 配置选项
 * @param {boolean} options.immediate - 是否立即执行
 * @param {*} options.initialData - 初始数据
 * @param {Function} options.onSuccess - 成功回调
 * @param {Function} options.onError - 错误回调
 * @returns {Object} 异步状态和方法
 */
export function useAsync(asyncFn, options = {}) {
  const { immediate = false, initialData = null, onSuccess, onError } = options;

  const data = ref(initialData);
  const isLoading = ref(false);
  const error = ref(null);
  const isReady = ref(false);

  // 是否成功
  const isSuccess = computed(() => isReady.value && !error.value);

  // 是否失败
  const isError = computed(() => !!error.value);

  /**
   * 执行异步操作
   * @param {...any} args - 传递给异步函数的参数
   * @returns {Promise<*>} 执行结果
   */
  async function execute(...args) {
    if (isLoading.value) return;

    isLoading.value = true;
    error.value = null;

    try {
      const result = await asyncFn(...args);
      data.value = result;
      isReady.value = true;

      if (onSuccess) {
        onSuccess(result);
      }

      return result;
    } catch (err) {
      error.value = err;

      if (onError) {
        onError(err);
      }

      throw err;
    } finally {
      isLoading.value = false;
    }
  }

  /**
   * 重置状态
   */
  function reset() {
    data.value = initialData;
    isLoading.value = false;
    error.value = null;
    isReady.value = false;
  }

  // 立即执行
  if (immediate) {
    execute();
  }

  return {
    data,
    isLoading,
    error,
    isReady,
    isSuccess,
    isError,
    execute,
    reset,
  };
}

/**
 * 创建带防抖的异步操作
 * @param {Function} asyncFn - 异步函数
 * @param {number} delay - 防抖延迟 (ms)
 * @param {Object} options - useAsync 选项
 * @returns {Object} 异步状态和方法
 */
export function useDebouncedAsync(asyncFn, delay = 300, options = {}) {
  let timeoutId = null;
  const asyncState = useAsync(asyncFn, options);

  function debouncedExecute(...args) {
    if (timeoutId) {
      clearTimeout(timeoutId);
    }

    return new Promise((resolve, reject) => {
      timeoutId = setTimeout(async () => {
        try {
          const result = await asyncState.execute(...args);
          resolve(result);
        } catch (err) {
          reject(err);
        }
      }, delay);
    });
  }

  function cancel() {
    if (timeoutId) {
      clearTimeout(timeoutId);
      timeoutId = null;
    }
  }

  return {
    ...asyncState,
    execute: debouncedExecute,
    cancel,
  };
}

/**
 * 创建带节流的异步操作
 * @param {Function} asyncFn - 异步函数
 * @param {number} interval - 节流间隔 (ms)
 * @param {Object} options - useAsync 选项
 * @returns {Object} 异步状态和方法
 */
export function useThrottledAsync(asyncFn, interval = 300, options = {}) {
  let lastExecution = 0;
  const asyncState = useAsync(asyncFn, options);

  async function throttledExecute(...args) {
    const now = Date.now();
    if (now - lastExecution < interval) {
      return asyncState.data.value;
    }

    lastExecution = now;
    return asyncState.execute(...args);
  }

  return {
    ...asyncState,
    execute: throttledExecute,
  };
}

export default useAsync;
