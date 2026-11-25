import { useSyncExternalStore } from 'react';

// 全局浮层容器注册表。
//
// 背景:Kumo 的浮层(LayerDialog / Popover / DropdownMenu / Tooltip / Toast / ContextMenu)
// 默认 portal 到 `document.body`,且自身 z-index 很低(弹层多为 z-50、Toast 视口 z-1)。
// 页面全屏层会 portal 到 body 并带很高的 z-index(终端 z-[9999]、Ask AI z-[1150]),
// 于是这些默认落到 body 的浮层会被全屏层盖住(编辑弹窗、全局确认弹窗、Toast 都会消失)。
//
// 解决办法:全屏层挂载时把自己作为「当前最上层浮层容器」压栈,应用顶层用
// KumoPortalProvider 把这个容器下发给所有 Kumo 浮层,让它们落到全屏层内部,
// 与全屏层处于同一层叠上下文。多个全屏层叠加时取栈顶(最后打开的那个)。
const stack = [];
let snapshot = null;
const listeners = new Set();

const emit = () => {
  snapshot = stack.length > 0 ? stack[stack.length - 1] : null;
  listeners.forEach(listener => listener());
};

/**
 * 注册一个浮层容器(通常是全屏层的根节点)。
 * @param {HTMLElement | null} node
 * @returns {() => void} 注销函数(组件卸载/全屏退出时调用)
 */
export function pushOverlayPortal(node) {
  if (!node) return () => {};
  const existing = stack.indexOf(node);
  if (existing >= 0) stack.splice(existing, 1);
  stack.push(node);
  emit();
  return () => {
    const index = stack.indexOf(node);
    if (index < 0) return;
    stack.splice(index, 1);
    emit();
  };
}

const subscribe = (listener) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

const getSnapshot = () => snapshot;

/** 当前最上层浮层容器;无全屏层时为 null。 */
export function useOverlayPortalContainer() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
