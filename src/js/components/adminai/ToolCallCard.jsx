import React, { useState } from 'react';
import { Loader, Badge } from '@cloudflare/kumo';
import { Copy, Check, ChevronDown, ChevronRight } from '../Icons.jsx';

/* 工具调用卡片 — Cloudflare 「→ Running …」步骤行风格：
 * inline 模式用于 thinking 折叠区；完整模式带参数展开与错误复制 */
export default function ToolCallCard({ toolCall, inline }) {
  const [copied, setCopied] = useState(false);
  const [openArgs, setOpenArgs] = useState(false);

  const { toolName, args, status, error } = toolCall || {};
  const argSummary = args
    ? (typeof args === 'string' ? args : JSON.stringify(args)).slice(0, 200)
    : '';

  const handleCopy = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
    }
  };

  const statusIcon = () => {
    switch (status) {
      case 'running':
        return <Loader size={12} className="text-kumo-brand" />;
      case 'success':
        return <Badge variant="success">成功</Badge>;
      case 'failed':
        return <Badge variant="error">失败</Badge>;
      default:
        return null;
    }
  };

  /* 内联行：→ toolName 状态 */
  if (inline) {
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-kumo-default">
        <span className="truncate font-medium">{toolName || '未知工具'}</span>
        {statusIcon()}
      </span>
    );
  }

  return (
    <div className="rounded-lg bg-kumo-control p-2.5 text-xs">
      <button
        type="button"
        onClick={() => argSummary && setOpenArgs(!openArgs)}
        className="flex w-full items-center gap-1.5 text-left"
      >
        <span className="text-kumo-subtle">→</span>
        <span className="truncate font-medium text-kumo-default">{toolName || '未知工具'}</span>
        <span className="ml-auto flex shrink-0 items-center gap-1">
          {statusIcon()}
          {argSummary && (
            <ChevronDown className={`h-3 w-3 text-kumo-subtle transition-transform ${openArgs ? 'rotate-180' : ''}`} />
          )}
        </span>
      </button>
      {openArgs && argSummary && (
        <div className="mt-2 rounded bg-kumo-base p-2 font-mono text-[11px] break-all text-kumo-subtle">
          {argSummary}
        </div>
      )}
      {status === 'failed' && error && (
        <div className="mt-2 flex items-start gap-2 rounded bg-kumo-danger/10 p-2 text-kumo-danger">
          <span className="flex-1 break-all font-mono text-[11px]">{error}</span>
          <button
            type="button"
            onClick={() => handleCopy(error)}
            className="shrink-0 rounded p-0.5 transition-colors hover:bg-kumo-danger/10"
            aria-label="复制错误消息"
          >
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          </button>
        </div>
      )}
    </div>
  );
}