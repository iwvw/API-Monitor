// 懒加载的 Markdown 渲染块：renderMarkdown 会连带打包 marked/DOMPurify/katex（~250KB），
// 只有真正要看渲染结果时才动态加载，避免 API 文档页首屏背上这份体积。
import React, { useMemo } from 'react';
import { renderMarkdown } from '../../modules/markdown.js';

export default function MarkdownBlock({ markdown, className = '' }) {
  const html = useMemo(() => renderMarkdown(markdown), [markdown]);
  return (
    <div
      className={`app-markdown-rendered ${className}`.trim()}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
