import React from 'react';
import { Badge, Loader } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Copy, Check } from '../Icons.jsx';

export default function ToolCallCard({ toolCall }) {
  const [copied, setCopied] = React.useState(false);

  const { toolName, args, status, error } = toolCall || {};
  const argSummary = args ? JSON.stringify(args).slice(0, 120) : '';

  const handleCopy = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
    }
  };

  const statusBadge = () => {
    switch (status) {
      case 'running':
        return (
          <span className="inline-flex items-center gap-1 text-xs text-kumo-brand">
            <Loader size={12} />
            运行中
          </span>
        );
      case 'success':
        return <Badge variant="success">成功</Badge>;
      case 'failed':
        return <Badge variant="error">失败</Badge>;
      default:
        return <Badge variant="neutral">等待中</Badge>;
    }
  };

  return (
    <div className="rounded-lg border border-kumo-line bg-kumo-control p-3 text-xs">
      <div className="mb-2 flex items-center justify-between">
        <span className="font-semibold text-kumo-strong">{toolName || '未知工具'}</span>
        {statusBadge()}
      </div>
      {argSummary && (
        <div className="mb-1 truncate font-mono text-kumo-subtle">
          {argSummary}
        </div>
      )}
      {status === 'failed' && error && (
        <div className="mt-2 flex items-start gap-2 rounded bg-kumo-danger/10 p-2 text-kumo-danger">
          <span className="flex-1 break-all font-mono text-[11px]">{error}</span>
          <Button
            size="xs"
            variant="ghost"
            shape="square"
            onClick={() => handleCopy(error)}
            aria-label="复制错误消息"
          >
            {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          </Button>
        </div>
      )}
    </div>
  );
}