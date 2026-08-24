import React, { useRef, useCallback, useEffect } from 'react';
import { createMilkdownAdapter } from './adapters/milkdownAdapter.js';
import MarkdownVisualCanvas from './MarkdownVisualCanvas.jsx';
import MarkdownSourcePane from './MarkdownSourcePane.jsx';
import DocumentPreviewPane from './DocumentPreviewPane.jsx';
import DocumentToolbar from './DocumentToolbar.jsx';
import DocumentStatusBar from './DocumentStatusBar.jsx';
import DocumentOutline from './DocumentOutline.jsx';
import DocumentSidebar from './DocumentSidebar.jsx';
import { useDocumentEditorState } from './useDocumentEditorState.js';

/**
 * 沉浸式文档工作区 — 全高三栏或双栏布局。
 *
 * 适用场景：提示词库、文档页、说明页等需要长时间停留的主工作区。
 * 支持 write / split / source 三种编辑模式。
 *
 * Props:
 * - initialMarkdown: 初始 Markdown 内容
 * - title: 文档标题
 * - onTitleChange: 标题变更回调
 * - onSave: 保存回调 (markdown) => Promise<void>
 * - readOnly: 是否只读
 * - showOutline: 是否显示大纲
 * - showStatusBar: 是否显示状态栏
 * - rightPanel: 右侧面板内容 { title, content }
 * - leftPanel: 左侧面板内容 { content }
 * - extraToolbarActions: 额外工具栏操作
 */
export default function DocumentWorkspace({
  initialMarkdown = '',
  title = '',
  onTitleChange,
  onSave,
  readOnly = false,
  showOutline = false,
  showStatusBar = true,
  rightPanel,
  leftPanel,
  extraToolbarActions,
  placeholder = '开始输入 Markdown 内容…',
  autosaveDelay = 0,
  className = '',
}) {
  const state = useDocumentEditorState(initialMarkdown, {
    defaultMode: 'write',
    showOutline,
    showStatusBar,
  });

  const adapterRef = useRef(null);

  // Sync external markdown changes（有未保存编辑时不覆盖，避免外部刷新清空输入）
  useEffect(() => {
    if (initialMarkdown !== state.markdownRef.current) {
      if (state.dirty) return;
      if (adapterRef.current) {
        adapterRef.current.setMarkdown(initialMarkdown);
      }
      state.resetMarkdown(initialMarkdown);
    }
  }, [initialMarkdown, state, state.dirty]);  

  const handleCreateAdapter = useCallback(({ root }) => {
    const adapter = createMilkdownAdapter({
      root,
      defaultValue: state.markdownRef.current,
    });
    adapter.onChange(markdown => {
      state.setMarkdown(markdown);
    });
    return adapter;
  }, []);  

  const handleSave = useCallback(async () => {
    if (!onSave || state.saveState === 'saving') return;
    state.markSaving();
    try {
      await onSave(state.markdownRef.current);
      state.markSaved();
    } catch {
      state.markSaveError();
    }
  }, [onSave, state]);

  useEffect(() => {
    if (!autosaveDelay || !onSave || !state.dirty || state.saveState === 'saving') return;
    const timer = window.setTimeout(() => handleSave(), autosaveDelay);
    return () => window.clearTimeout(timer);
  }, [autosaveDelay, handleSave, onSave, state.dirty, state.saveState]);

  const handleCopyMarkdown = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(state.markdownRef.current);
    } catch {
      // Fallback silently
    }
  }, [state]);

  const hasLeftPanel = Boolean(leftPanel);
  const hasRightPanel = Boolean(rightPanel);
  const rightPanelOpen = state.showOutline || hasRightPanel;

  const renderEditor = () => {
    if (state.mode === 'source') {
      return (
        <MarkdownSourcePane
          value={state.markdown}
          onChange={state.setMarkdown}
          readOnly={readOnly}
          placeholder={placeholder}
        />
      );
    }

    if (state.mode === 'split') {
      return (
        <div className="flex min-h-0 flex-1 gap-0 divide-x divide-kumo-line">
          <div className="flex min-h-0 flex-1 flex-col">
            <MarkdownVisualCanvas
              adapterRef={adapterRef}
              onCreateAdapter={handleCreateAdapter}
              readOnly={readOnly}
              placeholder={placeholder}
            />
          </div>
          <div className="flex min-h-0 flex-1 flex-col">
            <DocumentPreviewPane markdown={state.markdown} />
          </div>
        </div>
      );
    }

    if (state.showPreview) {
      return <DocumentPreviewPane markdown={state.markdown} />;
    }

    // write mode
    return (
      <MarkdownVisualCanvas
        adapterRef={adapterRef}
        onCreateAdapter={handleCreateAdapter}
        readOnly={readOnly}
        placeholder={placeholder}
      />
    );
  };

  return (
    <div className={`flex h-full min-h-0 w-full min-w-0 flex-1 flex-col ${className}`.trim()}>
      {/* Toolbar */}
      <DocumentToolbar
        title={title}
        onTitleChange={onTitleChange}
        readOnly={readOnly}
        mode={state.mode}
        onModeChange={state.setMode}
        dirty={state.dirty}
        saveState={state.saveState}
        onSave={onSave ? handleSave : undefined}
        showOutline={showOutline}
        onToggleOutline={state.toggleOutline}
        showPreview={state.showPreview}
        onTogglePreview={state.togglePreview}
        onCopyMarkdown={handleCopyMarkdown}
        extraActions={extraToolbarActions}
        className="shrink-0"
      />

      {/* Main Content Area */}
      <div className="flex min-h-0 flex-1">
        {/* Left Panel */}
        {hasLeftPanel && (
          <div
            className={`flex w-56 shrink-0 flex-col border-r border-kumo-line bg-kumo-base ${leftPanel.className || ''}`.trim()}
          >
            {leftPanel.content}
          </div>
        )}

        {/* Center Editor */}
        <div className="flex min-h-0 flex-1 flex-col">{renderEditor()}</div>

        {/* Right Panel (Outline + Custom) */}
        {rightPanelOpen && (
          <DocumentSidebar
            open={rightPanelOpen}
            onToggle={hasRightPanel ? undefined : state.toggleOutline}
            title={hasRightPanel ? rightPanel.title : '大纲'}
          >
            {state.showOutline && (!hasRightPanel || state.outline.length > 0) && (
              <DocumentOutline
                outline={state.outline}
                className={hasRightPanel ? 'border-b border-kumo-line' : ''}
                onHeadingClick={item => {
                  // Scroll to heading in editor
                  const index = Number(String(item.id).replace('heading-', ''));
                  const el = document.querySelectorAll(
                    '.app-markdown-visual-editor h1, .app-markdown-visual-editor h2, .app-markdown-visual-editor h3, .app-markdown-visual-editor h4, .app-markdown-visual-editor h5, .app-markdown-visual-editor h6'
                  )[index];
                  el?.scrollIntoView({ behavior: 'smooth', block: 'start' });
                }}
              />
            )}
            {hasRightPanel && rightPanel.content}
          </DocumentSidebar>
        )}
      </div>

      {/* StatusBar */}
      {showStatusBar && (
        <DocumentStatusBar
          wordCount={state.wordCount}
          charCount={state.charCount}
          outlineCount={state.outline.length}
          lastSavedAt={state.lastSavedAt}
          className="shrink-0"
        />
      )}
    </div>
  );
}
