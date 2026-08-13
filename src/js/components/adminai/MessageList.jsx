import React, { useEffect, useRef, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { ChevronDown, ChevronRight, Sparkle, Copy, Check, ThumbsUp, ThumbsDown } from '../Icons.jsx';
import ToolCallCard from './ToolCallCard.jsx';
import ApprovalCard from './ApprovalCard.jsx';

/* ---------- 纯文本渲染（粗体/行内代码/链接） ---------- */
function TextBlock({ text }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g);
  return (
    <span className="whitespace-pre-wrap break-words">
      {parts.map((part, i) => {
        if (part.startsWith('**') && part.endsWith('**')) {
          return <strong key={i}>{part.slice(2, -2)}</strong>;
        }
        if (part.startsWith('`') && part.endsWith('`')) {
          return <code key={i} className="rounded bg-kumo-recessed px-1.5 py-0.5 font-mono text-[12px]">{part.slice(1, -1)}</code>;
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

/* ---------- 代码块 ---------- */
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
    <div className="group relative my-2 overflow-hidden rounded-lg border border-kumo-line bg-kumo-recessed">
      <div className="flex items-center justify-between border-b border-kumo-line px-3 py-1.5">
        <span className="text-[10px] text-kumo-subtle">{language || 'code'}</span>
        <button
          type="button"
          onClick={handleCopy}
          className="flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[10px] text-kumo-subtle transition-colors hover:text-kumo-default"
          aria-label="复制代码"
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
          {copied ? '已复制' : '复制'}
        </button>
      </div>
      <pre className="overflow-x-auto p-3 text-[11px] leading-relaxed"><code>{code}</code></pre>
    </div>
  );
}

/* ---------- 思维链折叠区（reasoning，默认展开） ---------- */
function ReasoningBlock({ text }) {
  const [open, setOpen] = useState(true);
  if (!text) return null;
  return (
    <div className="mb-2 flex flex-col gap-0.5">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-max items-center gap-1 rounded-sm px-1.5 py-0.5 text-[12px] text-neutral-400 transition-colors hover:text-neutral-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400"
      >
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        {open ? '隐藏推理' : '查看推理'}
      </button>
      {open && <p className="whitespace-pre-wrap break-words text-sm text-kumo-default/70">{text}</p>}
    </div>
  );
}

/* ---------- 思考过程折叠区（thinking 工具步骤，默认折叠） ---------- */
function ThinkingBlock({ thinking }) {
  const [open, setOpen] = useState(false);
  if (!thinking || thinking.length === 0) return null;
  return (
    <div className="mb-2 flex flex-col gap-0.5">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex w-max cursor-pointer items-center gap-1 text-[12px] text-neutral-400 transition-colors hover:text-neutral-500"
      >
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        {open ? '隐藏思考过程' : '查看思考过程'}
      </button>
      {open && (
        <div className="mt-1 flex flex-col gap-0.5">
          {thinking.map((step, i) => (
            <div key={i} className="flex items-center gap-1.5 text-xs">
              <span className="text-kumo-subtle">→</span>
              <ToolCallCard toolCall={step} inline />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------- 助手消息底部反馈操作 ---------- */
function FeedbackRow({ onCopy }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = async () => {
    await onCopy();
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  return (
    <div className="mt-1 flex items-center gap-1 text-kumo-subtle">
      <button
        type="button"
        onClick={handleCopy}
        className="flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-kumo-tint hover:text-kumo-default"
        aria-label="复制回答"
      >
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
      <button
        type="button"
        className="flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-kumo-tint hover:text-kumo-default"
        aria-label="点赞"
      >
        <ThumbsUp className="h-3.5 w-3.5" />
      </button>
      <button
        type="button"
        className="flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-kumo-tint hover:text-kumo-default"
        aria-label="点踩"
      >
        <ThumbsDown className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

/* ---------- 消息定义块分发 ---------- */
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
        <div className="my-2 rounded-xl border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs text-kumo-danger">
          <div className="flex items-center justify-between gap-2">
            <span className="break-all">{block.message || '发生错误'}</span>
            {block.retryable && onRetry && (
              <Button size="xs" variant="secondary" onClick={onRetry}>重试</Button>
            )}
          </div>
        </div>
      );
    default:
      return null;
  }
}

/* ---------- 消息列表 ---------- */
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

  if (!messages || messages.length === 0) return null;

  return (
    <div ref={listRef} onScroll={handleScroll} className="h-full overflow-y-auto overscroll-contain scrollbar-thin">
      <div className="flex w-full flex-col gap-4">
        {messages.map((msg, idx) => (
          <article
            key={msg.id || idx}
            className={`flex w-full flex-col gap-2 ${msg.role === 'user' ? 'items-end' : 'items-start'}`}
          >
            {msg.role === 'user' ? (
              <div className="w-fit max-w-prose rounded-xl bg-kumo-fill px-4 py-3 text-sm !leading-relaxed">
                <TextBlock text={msg.content} />
              </div>
            ) : (
              <div className="flex w-full flex-col gap-1">
                <ReasoningBlock text={msg.reasoning} />
                <ThinkingBlock thinking={msg.thinking} />
                <div className="w-full">
                  <div className="mb-1 flex items-center gap-1.5 text-xs text-kumo-subtle">
                    <Sparkle className="h-3.5 w-3.5 text-kumo-brand" />
                    <span>代理</span>
                  </div>
                  <div className="w-full max-w-full rounded-xl bg-kumo-base px-4 py-3 text-sm !leading-relaxed ring-1 ring-kumo-line">
                    <div className="space-y-2">
                      {msg.blocks && msg.blocks.map((block, bi) => (
                        <MessageBlock
                          key={bi}
                          block={block}
                          onResolveApproval={onResolveApproval}
                          onRetry={onRetry}
                        />
                      ))}
                      {!msg.blocks && msg.content && <TextBlock text={msg.content} />}
                    </div>
                  </div>
                  <FeedbackRow onCopy={() => navigator.clipboard.writeText(msg.content || msg.blocks?.map((b) => b.text || '').join('\n') || '')} />
                </div>
              </div>
            )}
          </article>
        ))}
      </div>
    </div>
  );
}