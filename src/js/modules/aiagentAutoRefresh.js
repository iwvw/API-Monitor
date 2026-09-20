// 实例列表的自动刷新策略。
//
// 从 AiAgentConsole 抽出来单独成模块，让「何时该刷新」这个决策可测：
// 探测会往返主机 Agent（最长 8 秒/实例），无节制的轮询会打满纳管主机，
// 因此策略本身是需要测试锁定的业务规则，而不是随手写在 useEffect 里的细节。

/** 自动刷新间隔：探测要往返主机 Agent，5 秒足以感知变化又不过度打扰。 */
export const AUTO_REFRESH_INTERVAL_MS = 5000;

const STORAGE_KEY = 'aiagent-auto-refresh';

/**
 * 取 localStorage；不可用（无 window、隐私模式）时返回 null。
 *
 * 抽出这个判断让模块在 node 测试环境下也能直接跑，
 * 不必为测试引入 jsdom 或全局桩件。
 */
function storage() {
  try {
    if (typeof window === 'undefined') return null;
    return window.localStorage ?? null;
  } catch {
    return null;
  }
}

/**
 * 读取自动刷新开关的持久化值。
 *
 * 默认开启：进程会被 supervisor 自动拉起、也可能崩溃，用户盯着面板时
 * 希望看到状态变化，而不是手动刷新。存储不可用时回退到默认值。
 */
export function readAutoRefreshPreference() {
  try {
    const raw = storage()?.getItem(STORAGE_KEY);
    if (raw === null || raw === undefined) return true;
    return raw === '1';
  } catch {
    return true;
  }
}

/** 持久化自动刷新开关。存储不可用时静默忽略（不影响功能）。 */
export function writeAutoRefreshPreference(enabled) {
  try {
    storage()?.setItem(STORAGE_KEY, enabled ? '1' : '0');
  } catch {
    // 隐私模式等场景下写入会失败，忽略即可。
  }
}

/**
 * 判断此刻是否应该执行一次自动刷新。
 *
 * 三个条件缺一不可：
 *   1. 用户开启了自动刷新；
 *   2. 页面可见——隐藏时轮询已被 hook 暂停，这里是第二道保险；
 *   3. 没有其它请求在途——避免与手动刷新/生命周期操作叠加，
 *      否则一次探测风暴会让主机 Agent 的任务队列拥塞。
 *
 * @param {{enabled: boolean, loading: boolean}} state
 * @returns {boolean}
 */
export function shouldAutoRefresh(state) {
  if (!state?.enabled) return false;
  if (state.loading) return false;
  if (typeof document !== 'undefined' && document.hidden) return false;
  return true;
}
