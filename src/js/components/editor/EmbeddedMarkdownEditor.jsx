import React, { useRef, useCallback, useEffect, useState } from 'react';
import { Tabs } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { createMilkdownAdapter } from './adapters/milkdownAdapter.js';
import MarkdownSourcePane from './MarkdownSourcePane.jsx';
import MarkdownVisualCanvas from './MarkdownVisualCanvas.jsx';
import { Eye, CodeFile } from '../Icons.jsx';
import { iconButtonIconClass } from '../ui/AppPrimitives.jsx';

const EMBEDDED_MODES = [
  {
    value: 'visual',
    label: (
      <span className="inline-flex items-center gap-1">
        <Eye className="h-3 w-3" />
      </span>
    ),
  },
  {
    value: 'source',
    label: (
      <span className="inline-flex items-center gap-1">
        <CodeFile className="h-3 w-3" />
      </span>
    ),
  },
];

/**
 * 轻量级嵌入式 Markdown 编辑器。
 *
 * 适用场景：表单字段、弹窗备注、设置页说明、小卡片内容编辑。
 * API 与旧 MarkdownEditor 兼容：
 * - value / onChange
 * - readOnly / placeholder / label / defaultMode
 */
export default function EmbeddedMarkdownEditor({
  value = '',
  onChange,
  label = 'Markdown 编辑器',
  readOnly = false,
  className = '',
  minHeight = '18rem',
  placeholder = '输入 Markdown 内容',
  defaultMode = 'visual',
  showHeader = true,
}) {
  const [mode, setMode] = useState(defaultMode === 'source' ? 'source' : 'visual');
  const adapterRef = useRef(null);
  const onChangeRef = useRef(onChange);
  const lastValueRef = useRef(value);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  // 受控值回写：外部 value 变化时同步到编辑器实例（本地编辑后父级回环的值
  // 与 lastValueRef 相等，不会反复回写）。
  useEffect(() => {
    if (value === lastValueRef.current) return;
    lastValueRef.current = value;
    if (adapterRef.current) {
      adapterRef.current.setMarkdown(String(value ?? ''));
    }
  }, [value]);

  const handleCreateAdapter = useCallback(({ root }) => {
    const adapter = createMilkdownAdapter({
      root,
      defaultValue: String(value ?? ''),
    });
    adapter.onChange((markdown) => {
      lastValueRef.current = markdown;
      onChangeRef.current?.(markdown);
    });
    return adapter;
  }, []);  

  return (
    <div
      className={`app-embedded-markdown-editor flex flex-col overflow-hidden rounded-lg border border-kumo-line ${className}`.trim()}
      style={{ minHeight }}
    >
      {showHeader && (
        <div className="flex shrink-0 items-center justify-between border-b border-kumo-line bg-kumo-recessed/50 px-3 py-1.5">
          <span className="truncate text-xs font-semibold text-kumo-strong">{label}</span>
          <Tabs
            {...TOOL_TABS_PROPS}
            value={mode}
            onValueChange={setMode}
            tabs={EMBEDDED_MODES}
          />
        </div>
      )}
      <div className="flex min-h-0 flex-1 flex-col">
        {mode === 'source' ? (
          <MarkdownSourcePane
            value={value}
            onChange={onChange}
            readOnly={readOnly}
            placeholder={placeholder}
            label={`${label} 源码`}
          />
        ) : (
          <MarkdownVisualCanvas
            adapterRef={adapterRef}
            onCreateAdapter={handleCreateAdapter}
            readOnly={readOnly}
            placeholder={placeholder}
          />
        )}
      </div>
    </div>
  );
}
