/**
 * Vue CDN Shim
 * 在 CDN 模式下，Vue 通过全局变量加载，此模块提供兼容的导出
 */

// 如果 Vue 已经通过 CDN 加载到全局，直接导出全局 Vue 的属性
// 这允许代码继续使用 import { createApp } from 'vue' 语法

const Vue = window.Vue;

if (!Vue) {
  console.error(
    '[Vue Shim] Vue is not loaded! Make sure the Vue CDN script is loaded before this module.'
  );
}

// 导出 Vue 3 常用 API
export const createApp = Vue?.createApp;
export const ref = Vue?.ref;
export const reactive = Vue?.reactive;
export const computed = Vue?.computed;
export const watch = Vue?.watch;
export const watchEffect = Vue?.watchEffect;
export const onMounted = Vue?.onMounted;
export const onUnmounted = Vue?.onUnmounted;
export const nextTick = Vue?.nextTick;
export const toRefs = Vue?.toRefs;
export const toRef = Vue?.toRef;
export const isRef = Vue?.isRef;
export const unref = Vue?.unref;
export const shallowRef = Vue?.shallowRef;
export const triggerRef = Vue?.triggerRef;
export const customRef = Vue?.customRef;
export const isProxy = Vue?.isProxy;
export const isReactive = Vue?.isReactive;
export const isReadonly = Vue?.isReadonly;
export const markRaw = Vue?.markRaw;
export const toRaw = Vue?.toRaw;
export const provide = Vue?.provide;
export const inject = Vue?.inject;
export const h = Vue?.h;
export const defineComponent = Vue?.defineComponent;
export const defineAsyncComponent = Vue?.defineAsyncComponent;

// 默认导出整个 Vue 对象
export default Vue;
