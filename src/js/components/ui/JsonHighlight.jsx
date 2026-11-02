import React from 'react';
import { CodeHighlighted } from '@cloudflare/kumo/code';

// JSON 语法高亮只读展示（Kumo CodeHighlighted / Shiki 引擎，跟随亮暗主题）。
// 与 CodeEditor（CodeMirror，编辑用）分工：本组件只做展示。
// 保留原有 props 形状，内部改为复用 Kumo 的 ShikiProvider 管线，去掉自维护的
// shiki 实例与主题切换逻辑。
export default function JsonHighlight({ code, className = '', minHeight }) {
  return (
    <div
      className={`shiki-json-block ${className}`.trim()}
      style={minHeight ? { minHeight } : undefined}
    >
      <CodeHighlighted code={code} lang="json" variant="plain" showCopyButton />
    </div>
  );
}
