import React, { useEffect, useRef, useState } from 'react';
import { Tabs } from '@cloudflare/kumo';
import { CodeFile, Eye } from '../Icons.jsx';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import CodeEditor from './CodeEditor.jsx';
import { createMilkdownAdapter } from '../editor/adapters/milkdownAdapter.js';

const EDITOR_MODES = [
  {
    value: 'visual',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <Eye className="h-3.5 w-3.5" />
        所见即所得
      </span>
    ),
  },
  {
    value: 'source',
    label: (
      <span className="inline-flex items-center gap-1.5">
        <CodeFile className="h-3.5 w-3.5" />
        源码
      </span>
    ),
  },
];

function VisualMarkdownEditor({ value, onChange, readOnly, label, placeholder }) {
  const rootRef = useRef(null);
  const adapterRef = useRef(null);
  const [status, setStatus] = useState('loading');

  useEffect(() => {
    let cancelled = false;
    let adapter = null;
    const root = rootRef.current;
    if (!root) return;

    setStatus('loading');

    adapter = createMilkdownAdapter({
      root,
      defaultValue: String(value ?? ''),
    });

    adapter.onChange((markdown) => {
      onChange?.(markdown);
    });

    adapter.create().then(() => {
      if (cancelled) {
        adapter.destroy();
        return;
      }
      adapterRef.current = adapter;
      setStatus('ready');
    }).catch(() => {
      if (!cancelled) setStatus('error');
    });

    return () => {
      cancelled = true;
      if (adapterRef.current === adapter) adapterRef.current = null;
      if (adapter) adapter.destroy();
    };
  }, []);  

  // Sync external value changes
  useEffect(() => {
    const adapter = adapterRef.current;
    if (adapter && adapter.getMarkdown() !== String(value ?? '')) {
      adapter.setMarkdown(String(value ?? ''));
    }
  }, [value]);

  useEffect(() => {
    adapterRef.current?.setReadonly(readOnly);
  }, [readOnly]);

  return (
    <div className="app-markdown-editor-visual" aria-label={label} aria-busy={status === 'loading'}>
      <div ref={rootRef} className="app-markdown-editor-crepe" />
      {status === 'loading' ? (
        <div className="app-markdown-editor-state">正在加载编辑器</div>
      ) : null}
      {status === 'error' ? (
        <div className="app-markdown-editor-state text-kumo-error">
          编辑器加载失败，切换源码模式
        </div>
      ) : null}
    </div>
  );
}

export default function MarkdownEditor({
  value = '',
  onChange,
  label = 'Markdown 编辑器',
  readOnly = false,
  className = '',
  minHeight = '18rem',
  placeholder = '输入 Markdown 内容',
  defaultMode = 'visual',
}) {
  const [mode, setMode] = useState(defaultMode === 'source' ? 'source' : 'visual');

  return (
    <div className={`app-markdown-editor ${className}`.trim()} style={{ minHeight }}>
      <div className="app-markdown-editor-header">
        <span className="truncate text-xs font-semibold text-kumo-strong">{label}</span>
        <Tabs {...TOOL_TABS_PROPS} value={mode} onValueChange={setMode} tabs={EDITOR_MODES} />
      </div>
      <div className="app-markdown-editor-body">
        {mode === 'source' ? (
          <CodeEditor
            value={value}
            onChange={onChange}
            fileName="document.md"
            language="markdown"
            label={`${label}源码`}
            readOnly={readOnly}
            placeholder={placeholder}
            showHeader={false}
            showLanguage={false}
            lineWrapping
            minHeight="100%"
          />
        ) : (
          <VisualMarkdownEditor
            value={value}
            onChange={onChange}
            readOnly={readOnly}
            label={label}
            placeholder={placeholder}
          />
        )}
      </div>
    </div>
  );
}
