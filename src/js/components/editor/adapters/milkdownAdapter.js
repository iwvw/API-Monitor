/**
 * Milkdown 编辑器统一适配层
 *
 * 封装 Milkdown core/headless 编辑器的完整生命周期，
 * 上层工作区（DocumentWorkspace / EmbeddedMarkdownEditor）
 * 不直接依赖具体富文本库实例。
 */

import { Editor, editorViewCtx, rootCtx, defaultValueCtx, EditorStatus, editorViewOptionsCtx } from '@milkdown/kit/core';
import { commonmark } from '@milkdown/kit/preset/commonmark';
import { gfm } from '@milkdown/kit/preset/gfm';
import { history } from '@milkdown/kit/plugin/history';
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener';
import { clipboard } from '@milkdown/kit/plugin/clipboard';
import { indent, indentConfig } from '@milkdown/kit/plugin/indent';
import { trailing } from '@milkdown/kit/plugin/trailing';
import { getMarkdown, replaceAll } from '@milkdown/kit/utils';

/**
 * 创建 headless Milkdown 编辑器实例。
 * 不加载任何 Crepe 主题 CSS，由宿主页 Kumo 组件掌控全部 UI。
 *
 * @param {object} opts
 * @param {HTMLElement} opts.root - 编辑器挂载容器
 * @param {string} [opts.defaultValue=''] - 初始 Markdown 内容
 * @param {object} [opts.editorViewOptions] - 额外 ProseMirror EditorView 选项
 * @returns {object} 适配器实例
 */
export function createMilkdownAdapter({ root, defaultValue = '', editorViewOptions = {} }) {
  let editor = null;
  let onChangeListener = null;
  let onSelectionChangeListener = null;
  let focusListener = null;
  let blurListener = null;
  let focusHandler = null;
  let blurHandler = null;
  let editable = true;

  editor = Editor.make()
    .config((ctx) => {
      ctx.set(rootCtx, root);
      ctx.set(defaultValueCtx, defaultValue);
      ctx.set(editorViewOptionsCtx, {
        editable: () => editable,
        ...editorViewOptions,
      });
      ctx.update(indentConfig.key, (value) => ({
        ...value,
        size: 4,
      }));
    })
    .use(commonmark)
    .use(gfm)
    .use(history)
    .use(indent)
    .use(trailing)
    .use(clipboard)
    .use(listener);

  const adapter = {
    /** 创建并挂载编辑器 */
    create() {
      return editor.create();
    },

    /** 销毁编辑器 */
    destroy() {
      onChangeListener = null;
      onSelectionChangeListener = null;
      focusListener = null;
      blurListener = null;
      if (focusHandler) { root.removeEventListener('focusin', focusHandler); focusHandler = null; }
      if (blurHandler) { root.removeEventListener('focusout', blurHandler); blurHandler = null; }
      return editor.destroy();
    },

    /** 获取当前 Markdown 内容 */
    getMarkdown() {
      return editor.action(getMarkdown());
    },

    /** 替换全部 Markdown 内容 */
    setMarkdown(markdown) {
	  if (editor.status === EditorStatus.Created) {
		editor.action(replaceAll(String(markdown ?? ''), true));
	  }
    },

    /** 设置只读模式 */
    setReadonly(readonly) {
      editable = !readonly;
      if (editor.status === EditorStatus.Created) {
        editor.action((ctx) => {
          const view = ctx.get(editorViewCtx);
          view.setProps({ editable: () => !readonly });
        });
      }
    },

    /** 聚焦编辑器 */
    focus() {
      if (editor.status === EditorStatus.Created) {
        editor.action((ctx) => {
          const view = ctx.get(editorViewCtx);
          view.focus();
        });
      }
    },

    /** 注册 Markdown 内容变更回调 */
    onChange(fn) {
      onChangeListener = fn;
      if (editor.status !== EditorStatus.Created) {
        editor.config((ctx) => {
          const mgr = ctx.get(listenerCtx);
          mgr.markdownUpdated((_ctx, markdown) => {
            onChangeListener?.(markdown);
          });
        });
      } else {
        editor.action((ctx) => {
          const mgr = ctx.get(listenerCtx);
          mgr.markdownUpdated((_ctx, markdown) => {
            onChangeListener?.(markdown);
          });
        });
      }
    },

    /** 注册选区变更回调 */
    onSelectionChange(fn) {
      onSelectionChangeListener = fn;
	  if (editor.status === EditorStatus.Created) {
		editor.action((ctx) => {
		  const view = ctx.get(editorViewCtx);
		  const previous = view.props.handleDOMEvents || {};
		  view.setProps({
			handleDOMEvents: {
			  ...previous,
			  selectionchange: (currentView) => {
				onSelectionChangeListener?.(currentView.state.selection);
				return false;
			  },
			},
		  });
		});
	  }
    },

    /** 注册聚焦回调 */
    onFocus(fn) {
      focusListener = fn;
	  if (!focusHandler) { focusHandler = () => focusListener?.(); root.addEventListener('focusin', focusHandler); }
    },

    /** 注册失焦回调 */
    onBlur(fn) {
      blurListener = fn;
	  if (!blurHandler) { blurHandler = () => blurListener?.(); root.addEventListener('focusout', blurHandler); }
    },

    /** 获取底层 Milkdown Editor（仅限内部使用） */
    getEditor() {
      return editor;
    },

    /** 当前是否只读 */
    get readonly() {
      return !editable;
    },
  };

  return adapter;
}
