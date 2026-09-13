import React, { useMemo } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Tooltip } from '@cloudflare/kumo/components/tooltip';
import JsonHighlight from '../../components/ui/JsonHighlight.jsx';
import { renderMarkdown } from '../../modules/markdown.js';
import { tryFormatJson } from './utils.js';

export function IconButton({ label, icon, onClick, variant = 'secondary', disabled = false }) {
  return (
    <Tooltip
      content={label}
      render={(
        <Button size="sm" variant={variant} shape="square" aria-label={label} onClick={onClick} disabled={disabled}>
          {icon}
        </Button>
      )}
    />
  );
}

/* AI 输出 markdown 渲染（与编辑器预览同管线：marked + DOMPurify + katex） */
export function MarkdownOutput({ text }) {
  const html = useMemo(() => renderMarkdown(text), [text]);
  if (!text) return null;
  return (
    <div
      className="app-markdown-preview prose prose-sm max-w-none break-words text-xs"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

// renderLogOutput 渲染日志正文：JSON 用 Shiki 语法高亮（跟随亮暗主题），
// AI 任务且非 JSON 时渲染 Markdown。
export function renderLogOutput(output, isAiTask) {
  if (!output) return <span className="text-xs text-kumo-subtle">（无输出）</span>;
  const json = tryFormatJson(output);
  if (json != null) {
    return <JsonHighlight code={json} className="rounded-md border border-kumo-line" minHeight="12rem" />;
  }
  if (isAiTask) {
    return <MarkdownOutput text={output} />;
  }
  return <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-kumo-default">{output}</pre>;
}
