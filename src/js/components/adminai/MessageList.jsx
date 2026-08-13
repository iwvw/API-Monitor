import React, { useEffect, useRef } from 'react';
import { Banner } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Copy, Check } from '../Icons.jsx';
import ToolCallCard from './ToolCallCard.jsx';
import ApprovalCard from './ApprovalCard.jsx';

function TextBlock({ text }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g);
  return (
    <span>
      {parts.map((part, i) => {
        if (part.startsWith('**') && part.endsWith('**')) {
          return <strong key={i}>{part.slice(2, -2)}</strong>;
        }
        if (part.startsWith('`') && part.endsWith('`')) {
          return <code key={i} className="rounded bg-kumo-recessed px-1 font-mono text-[11px]">{part.slice(1, -1)}</code>;
        }
        const linkMatch = part.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
        if (linkMatch) {
          return <a key={i} href={linkMatch[2]} target="_blank" rel="noopener noreferrer" className="text-kumo-brand underline">{linkMatch[1]}</a>;
        }
        return part;
      })}
    </span>
  );
}

function CodeBlock({ code, language }) {
  const [copied, setCopied] = React.useState(false);
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
    }
  };
  return (
    <div className="group relative my-2 rounded-lg border border-kumo-line bg-kumo-recessed">
      <div className="flex items-center justify-between border-b border-kumo-line px-3 py-1.5">
        <span className="text-[10px] text-kumo-subtle">{language || 'code'}</span>
        <Button
          size="xs"
          variant="ghost"
          onClick={handleCopy}
          className="gap-1 text-[10px]"
          aria-label="复制代码"
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          {copied ? '已复制' : '复制'}
        </Button>
      </div>
      <pre className="overflow-x-auto p-3 text-[11px] leading-relaxed"><code>{code}</code></pre>
    </div>
  );
}

function DoneBlock({ usage, duration }) {
  const [collapsed, setCollapsed] = React.useState(true);
  return (
    <div className="mt-2">
      <Button
        size="xs"
        variant="ghost"
        onClick={() => setCollapsed(!collapsed)}
        className="text-[10px]"
      >
        {collapsed ? '展开用量详情' : '收起用量详情'}
      </Button>
      {!collapsed && (
        <div className="mt-1 rounded border border-kumo-line bg-kumo-control p-2 text-[10px] text-kumo-subtle">
          {usage && <div>用量：{JSON.stringify(usage)}</div>}
          {duration && <div>耗时：{duration}ms</div>}
        </div>
      )}
    </div>
  );
}

function MessageBlock({ block, onResolveApproval, onRetry }) {
  switch (block.type) {
    case 'text':
      return <TextBlock text={block.text} />;
    case 'code_block':
      return <CodeBlock code={block.code} language={block.language} />;
    case 'tool_call':
      return <ToolCallCard toolCall={block} />;
    case 'approval':
      return <ApprovalCard approval={block} onResolve={onResolveApproval} />;
    case 'error':
      return (
        <Banner variant="warning" className="my-2">
          <div className="flex items-center justify-between gap-2">
            <span>{block.message || '发生错误'}</span>
            {block.retryable && onRetry && (
              <Button size="xs" variant="secondary" onClick={onRetry}>重试</Button>
            )}
          </div>
        </Banner>
      );
    case 'done':
      return <DoneBlock usage={block.usage} duration={block.duration} />;
    default:
      return null;
  }
}

export default function MessageList({ messages, onResolveApproval, onRetry }) {
  const listRef = useRef(null);
  const userScrolledUp = useRef(false);

  useEffect(() => {
    const el = listRef.current;
    if (!el || userScrolledUp.current) return;
    el.scrollTop = el.scrollHeight;
  }, [messages]);

  const handleScroll = () => {
    const el = listRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    userScrolledUp.current = !atBottom;
  };

  if (!messages || messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center text-xs text-kumo-subtle">
        开始对话吧
      </div>
    );
  }

  return (
    <div ref={listRef} onScroll={handleScroll} className="flex-1 overflow-y-auto px-4 py-3 scrollbar-thin">
      <div className="flex flex-col gap-4">
        {messages.map((msg, idx) => (
          <div key={msg.id || idx}>
            {msg.role === 'user' ? (
              <div className="flex justify-end">
                <div className="max-w-[85%] rounded-xl bg-kumo-brand px-3 py-2 text-sm text-white">
                  {msg.content}
                </div>
              </div>
            ) : (
              <div>
                <div className="mb-2 text-xs font-semibold text-kumo-subtle">管理 AI</div>
                <div className="space-y-2">
                  {msg.blocks && msg.blocks.map((block, bi) => (
                    <MessageBlock
                      key={bi}
                      block={block}
                      onResolveApproval={onResolveApproval}
                      onRetry={onRetry}
                    />
                  ))}
                  {!msg.blocks && msg.content && (
                    <TextBlock text={msg.content} />
                  )}
                </div>
              </div>
            )}
            {idx < messages.length - 1 && (
              <hr className="border-kumo-line" />
            )}
          </div>
        ))}
      </div>
    </div>
  );
}