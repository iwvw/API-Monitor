import React, { useEffect, useRef, useState } from 'react';
import { Button, Loader, Textarea } from '@cloudflare/kumo';
import { ChevronDown, Sparkle, Terminal, Copy, Check, X, Edit } from '../Icons.jsx';
import ToolCallCard, { toolLabel, toolPathLabel, ToolSteps } from './ToolCallCard.jsx';
import ApprovalCard from './ApprovalCard.jsx';
import { isStreaming } from '../../modules/adminAiMessages.js';
import { typewriterFrame } from '../../modules/typewriter.js';

/* ---------- 行内渲染（粗体/斜体/删除线/行内代码/链接）——TextBlock 与 TableBlock 共用 ---------- */
function renderInline(text) {
  if (!text) return null;
  // 斜体只匹配紧贴内容的单星号（*文字*），避免把 `* *` 或列表残留星号当斜体
  const parts = text.split(/(\*\*[^*]+\*\*|\*[^*\s][^*\n]*\*|~~[^~]+~~|`[^`]+`|\[[^\]]+\]\([^)]+\))/g);
  return parts.map((part, i) => {
    if (part.startsWith('**') && part.endsWith('**')) {
      return <strong key={i}>{renderInline(part.slice(2, -2))}</strong>;
    }
    if (part.startsWith('~~') && part.endsWith('~~')) {
      return <del key={i} className="text-kumo-subtle">{part.slice(2, -2)}</del>;
    }
    if (part.startsWith('*') && part.endsWith('*')) {
      return <em key={i}>{part.slice(1, -1)}</em>;
    }
    if (part.startsWith('`') && part.endsWith('`')) {
      return <code key={i} className="rounded bg-kumo-recessed px-1.5 py-0.5 font-mono text-[12px]">{part.slice(1, -1)}</code>;
    }
    const linkMatch = part.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
    if (linkMatch) {
      // 仅放行安全协议，javascript:/data: 等链接原样显示不渲染为可点击链接
      const href = /^(https?:|mailto:|tel:)/i.test(linkMatch[2]) ? linkMatch[2] : '';
      return <a key={i} href={href || undefined} target="_blank" rel="noopener noreferrer" className="text-kumo-brand underline">{linkMatch[1]}</a>;
    }
    return part;
  });
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
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={handleCopy}
          className="flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[10px]"
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

/* ---------- 简单表格渲染（| a | b | 语法，自动跳过 --- 分隔行；容忍缺首/尾竖线） ---------- */
function TableBlock({ rows }) {
  if (!rows || rows.length === 0) return null;
  const cells = rows
    .map((r) => {
      const parts = r.split('|');
      // 去掉首尾空 cell：兼容 `| a | b` 缺尾竖线、`a | b |` 缺首竖线的输出
      if (parts.length > 0 && parts[0].trim() === '') parts.shift();
      if (parts.length > 0 && parts[parts.length - 1].trim() === '') parts.pop();
      return parts.map((s) => s.trim());
    })
    .filter((row) => !(row.length > 0 && row.every((c) => /^[-:]+$/.test(c)))); // 跳过 | --- | --- | 分隔行
  if (cells.length === 0) return null;
  const headerCells = cells[0] || [];
  const bodyRows = cells.slice(1);
  return (
    <div className="my-2 overflow-x-auto rounded-lg border border-kumo-line">
      <table className="w-full text-xs">
        <thead>
          <tr className="bg-kumo-control">
            {headerCells.map((c, i) => (
              <th key={i} className="border-r border-kumo-line px-3 py-1.5 text-left font-medium text-kumo-default last:border-r-0">{renderInline(c)}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {bodyRows.map((row, ri) => (
            <tr key={ri} className="border-t border-kumo-line">
              {row.map((c, ci) => (
                <td key={ci} className="border-r border-kumo-line px-3 py-1.5 text-kumo-default last:border-r-0">{renderInline(c)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/* ---------- 打字机输出：流式文本逐字揭示（内嵌 caret，保留 markdown 渲染） ---------- */
const TW_CURSOR_STEP = 2; // 每 tick 揭示字符数
const TW_CURSOR_MS = 20; // tick 间隔（约 100 字符/秒）
function TypewriterText({ text, streaming }) {
  const [visible, setVisible] = useState(() => (streaming ? 0 : text.length));
  const visibleRef = useRef(visible);
  const textRef = useRef(text);

  useEffect(() => {
    const frame = typewriterFrame(textRef.current, text);
    if (frame.reset) {
      textRef.current = text;
      visibleRef.current = 0;
      setVisible(0);
    } else {
      textRef.current = text;
    }
  }, [text]);

  useEffect(() => {
    if (!streaming) {
      visibleRef.current = text.length;
      setVisible(text.length);
      return undefined;
    }
    const timer = window.setInterval(() => {
      const target = textRef.current.length;
      if (visibleRef.current >= target) return; // 等新 delta 到达
      visibleRef.current = Math.min(target, visibleRef.current + TW_CURSOR_STEP);
      setVisible(visibleRef.current);
      if (visibleRef.current >= target) window.clearInterval(timer);
    }, TW_CURSOR_MS);
    return () => window.clearInterval(timer);
  }, [streaming, text]);

  const done = visible >= text.length;
  return (
    <div>
      <RenderLines text={text.slice(0, visible)} />
      {streaming && !done && <span className="askai-caret" aria-hidden />}
    </div>
  );
}

/* ---------- 富文本渲染（标题/列表/代码块/表格/行内样式；折叠由外层卡片 askai-collapse 控制） ---------- */
function TextBlock({ text, streaming }) {
  if (!text) return null;
  return (
    <div>
      {streaming ? <TypewriterText text={text} streaming /> : <RenderLines text={text} />}
    </div>
  );
}

/* 行级渲染（供 TextBlock 折叠共用） */
function RenderLines({ text }) {
  const lines = text.split('\n');
  const elements = [];
  let codeLines = null;
  let tableLines = null;
  let quoteLines = null;

  const flushTable = () => {
    if (tableLines) {
      elements.push(<TableBlock key={elements.length} rows={tableLines} />);
      tableLines = null;
    }
  };
  const flushCode = () => {
    if (codeLines) {
      elements.push(<CodeBlock key={elements.length} code={codeLines.join('\n')} />);
      codeLines = null;
    }
  };
  const flushQuote = () => {
    if (quoteLines) {
      elements.push(
        <blockquote key={elements.length} className="my-1 border-l-2 border-kumo-line pl-2.5 text-kumo-subtle">
          {quoteLines.map((l, i) => <p key={i} className="whitespace-pre-wrap break-words">{renderInline(l)}</p>)}
        </blockquote>
      );
      quoteLines = null;
    }
  };

  for (const line of lines) {
    if (line.trimStart().startsWith('```')) {
      if (codeLines) {
        flushCode();
      } else {
        codeLines = [];
      }
      continue;
    }
    if (codeLines) {
      codeLines.push(line);
      continue;
    }
    const trimmed = line.trim();
    if (/^\|/.test(trimmed) || (/\|/.test(trimmed) && /\|\s*$/.test(trimmed))) {
      flushCode();
      flushQuote();
      if (!tableLines) tableLines = [];
      tableLines.push(trimmed);
      continue;
    }
    if (trimmed.startsWith('>')) {
      flushCode();
      flushTable();
      if (!quoteLines) quoteLines = [];
      quoteLines.push(trimmed.replace(/^>\s?/, ''));
      continue;
    }
    if (/^([-*_])\s*\1\s*\1\s*$/.test(trimmed)) {
      flushCode();
      flushTable();
      flushQuote();
      elements.push(<hr key={elements.length} className="my-2 border-kumo-line" />);
      continue;
    }
    flushQuote();
    flushTable();
    flushCode();
    if (trimmed === '') continue;

    const heading = trimmed.match(/^(#{1,6})\s+(.*)$/);
    if (heading) {
      const level = heading[1].length;
      const cls = level <= 1 ? 'text-base font-bold' : level === 2 ? 'text-sm font-bold' : level === 3 ? 'text-sm font-semibold' : 'text-[13px] font-semibold text-kumo-default';
      elements.push(<h4 key={elements.length} className={`${cls} text-kumo-strong`}>{renderInline(heading[2])}</h4>);
      continue;
    }
    const indent = line.match(/^\s*/)[0].length;
    const nestStyle = indent > 0 ? { paddingLeft: `${Math.min(indent, 6) * 10}px` } : undefined;
    const taskMatch = trimmed.match(/^[-*]\s+\[(x| )\]\s+(.*)$/i);
    if (taskMatch) {
      elements.push(
        <div key={elements.length} className="flex items-start gap-1.5" style={nestStyle}>
          <span className={`mt-0.5 flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border ${taskMatch[1].toLowerCase() === 'x' ? 'border-kumo-brand bg-kumo-brand/15 text-kumo-brand' : 'border-kumo-line text-transparent'}`}>
            {taskMatch[1].toLowerCase() === 'x' ? <Check className="h-2.5 w-2.5" /> : ''}
          </span>
          <span className={`min-w-0 ${taskMatch[1].toLowerCase() === 'x' ? 'text-kumo-subtle line-through' : ''}`}>{renderInline(taskMatch[2])}</span>
        </div>
      );
      continue;
    }
    const listMatch = trimmed.match(/^([-*]|\d+[.、)）])\s+(.*)$/);
    if (listMatch) {
      const marker = listMatch[1];
      const bullet = /^[-*]$/.test(marker) ? '•' : marker;
      elements.push(
        <div key={elements.length} className="flex items-start gap-1.5" style={nestStyle}>
          <span className="shrink-0 text-kumo-subtle">{bullet}</span>
          <span className="min-w-0">{renderInline(listMatch[2])}</span>
        </div>
      );
      continue;
    }
    elements.push(<p key={elements.length} className="whitespace-pre-wrap break-words">{renderInline(line)}</p>);
  }
  flushQuote();
  flushTable();
  flushCode();
  return <div className="space-y-1">{elements}</div>;
}

/* ---------- 推理 part（timeline 行：胶囊 + 流式单行滚动 / 完成后折叠展开） ---------- */

// 推理摘要按句子边界截断：优先句末标点（。！？；），其次句中停顿（，、：），
// 避免在任意字符处断句导致语义被劈开；两行以内展示，完整内容点击展开。
function summarizeByPunctuation(text, maxLen = 96) {
  if (!text) return '';
  const t = text.trim();
  if (t.length <= maxLen) return t;
  const head = t.slice(0, maxLen);
  let idx = Math.max(
    head.lastIndexOf('。'),
    head.lastIndexOf('！'),
    head.lastIndexOf('？'),
    head.lastIndexOf('；'),
  );
  if (idx > maxLen * 0.4) return `${head.slice(0, idx + 1)}…`;
  idx = Math.max(head.lastIndexOf('，'), head.lastIndexOf('、'), head.lastIndexOf(','), head.lastIndexOf('：'));
  if (idx > maxLen * 0.4) return `${head.slice(0, idx + 1)}…`;
  return `${head}…`;
}

function ReasoningPart({ part, streaming }) {
  const [open, setOpen] = useState(false);
  const [tickerOn, setTickerOn] = useState(true);
  const scrollRef = useRef(null);
  useEffect(() => {
    if (tickerOn && scrollRef.current) {
      scrollRef.current.scrollLeft = scrollRef.current.scrollWidth;
    }
  }, [part.text, tickerOn]);
  if (!part.text && !streaming) return null;
  const isCN = (s) => /[\u4e00-\u9fa5]/.test(s || '');
  const displaySummary = isCN(part.summary) ? summarizeByPunctuation(part.summary) : '';
  return (
    <div className="flex flex-col gap-1">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => (streaming ? setTickerOn(!tickerOn) : setOpen(!open))}
        className="group flex w-max items-center gap-1 rounded-full bg-kumo-tint/70 py-0.5 pl-1.5 pr-2 text-[11px] text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-default"
      >
        <Sparkle className="h-3 w-3 text-kumo-brand" />
        推理
        {streaming ? (
          <span className="ml-0.5 flex items-center gap-0.5 text-kumo-brand">
            <span className="askai-typing-dot" />
            <span className="askai-typing-dot" />
            <span className="askai-typing-dot" />
          </span>
        ) : (
          <span className="text-[10px] transition-colors group-hover:text-kumo-default">
            {open ? '收起' : '详情'}
          </span>
        )}
      </Button>
      {streaming && tickerOn && (
        <div
          ref={scrollRef}
          className="min-w-0 max-w-full overflow-x-auto whitespace-nowrap text-xs leading-5 text-kumo-subtle/70"
          title="推理进行中（可横向拖动查看）"
        >
          {part.text}
        </div>
      )}
      {!streaming && open && (
        <div className="askai-collapse" data-open={open}>
          <div className="askai-reason-fade max-h-[220px] overflow-y-auto overscroll-contain border-l-2 border-kumo-line pl-3 pr-1">
            <p className="whitespace-pre-wrap break-words text-xs !leading-relaxed text-kumo-subtle/90">{part.text}</p>
          </div>
        </div>
      )}
      {!streaming && !open && displaySummary && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => setOpen(true)}
          className="askai-reason-fade flex w-full max-w-full cursor-pointer items-center gap-1.5 px-0.5 text-left"
          title="查看完整推理"
        >
          <Sparkle className="h-3 w-3 shrink-0 text-kumo-subtle/60" />
          <span className="line-clamp-2 min-w-0 text-xs text-kumo-subtle/80">{displaySummary}</span>
        </Button>
      )}
    </div>
  );
}

/* ---------- 工具结果 part（timeline 行：摘要灰字，点击复制原文） ---------- */
function ToolResultLine({ part }) {
  const [copied, setCopied] = useState(false);
  if (!part.summary) return null;
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(part.summary);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
    }
  };
  const failed = part.status === 'failed';
  return (
    <div className="askai-tool-result">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={handleCopy}
        title="复制工具结果"
        className={`group flex w-full items-start gap-2 text-left ${failed ? '!text-kumo-danger/90' : '!text-kumo-subtle/75'}`}
      >
        <span className="mt-0.5 shrink-0">
          {failed ? <X className="h-3 w-3" /> : <Check className="h-3 w-3 text-kumo-success/80" />}
        </span>
        <span className="min-w-0 flex-1 line-clamp-1 break-all text-[11px] leading-5">{part.summary}</span>
        {copied ? <Check className="h-3 w-3 shrink-0 text-kumo-brand" /> : <Copy className="h-3 w-3 shrink-0 text-transparent group-hover:text-kumo-subtle" />}
      </Button>
    </div>
  );
}

/* ---------- timeline part 分发（按时间序逐行渲染） ---------- */
function TimelinePart({ part, streaming, onResolveApproval, onRetry, className }) {
  switch (part.type) {
    case 'reasoning':
      return <ReasoningPart part={part} streaming={streaming} />;
    case 'tool_call':
      return (
        <div className="flex items-center gap-1.5">
          <span className="askai-timeline-dot shrink-0" aria-hidden />
          <div className="min-w-0 flex-1">
            <ToolCallCard toolCall={part} inline />
          </div>
        </div>
      );
    case 'tool_result':
      return <ToolResultLine part={part} />;
    case 'text': {
      const content = <TextBlock text={part.text} streaming={streaming} />;
      // 工具步骤/工具结果后的正文加顶部留白（卡片 gap-2 之上再补 mt-2），正文是主内容，需要呼吸空间
      return className ? <div className={className}>{content}</div> : content;
    }
    case 'approval':
      return <ApprovalCard approval={part} onResolve={onResolveApproval} />;
    case 'notice':
      return (
        <div className="flex items-center gap-1.5 text-[11px] text-kumo-subtle/70">
          <Loader size={10} className="shrink-0 animate-spin text-kumo-brand" />
          <span className="min-w-0 break-all">{part.text}</span>
        </div>
      );
    case 'error':
      return (
        <div className="my-2 rounded-xl border border-kumo-danger/30 bg-kumo-danger/10 p-3 text-xs text-kumo-danger">
          <div className="flex items-center justify-between gap-2">
            <span className="break-all">{part.message || '发生错误'}</span>
            {part.retryable && onRetry && (
              <Button size="xs" variant="secondary" onClick={() => onRetry(part.retryPrompt)}>重试</Button>
            )}
          </div>
        </div>
      );
    default:
      return null;
  }
}

/* ---------- 助手消息（timeline：推理/工具/正文按时间序） ---------- */
function AssistantMessage({ msg, streaming, onResolveApproval, onRetry, isCollapsed, toggleCollapse }) {
  const parts = msg.parts || [];
  const pending = streaming && parts.length === 0;
  const hasText = parts.some((p) => p.type === 'text' && p.text);
  const textParts = parts.filter((p) => p.type === 'text');
  const lastTextIdx = textParts.length - 1;
  const lastText = textParts.length > 0 ? textParts[textParts.length - 1].text : '';
  const hasLongText = textParts.some((p) => (p.text || '').length > 800);
  let textSeq = -1;
  // 连续工具 parts（tool_call/tool_result）合并成组，交给 ToolSteps 折叠展示；
  // 其他类型保持逐行 timeline 渲染
  const segments = [];
  let toolGroup = null;
  for (const p of parts) {
    if (p.type === 'tool_call' || p.type === 'tool_result') {
      if (!toolGroup) {
        toolGroup = [];
        segments.push(toolGroup);
      }
      toolGroup.push(p);
    } else {
      toolGroup = null;
      segments.push(p);
    }
  }
  return (
    <div className="flex w-full flex-col gap-1">
      <div className="mb-1 flex items-center gap-1.5 text-xs text-kumo-subtle">
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={toggleCollapse}
          title={isCollapsed ? '展开回复' : '收起回复'}
          className="flex cursor-pointer items-center gap-1 rounded-full bg-kumo-tint/70 py-0.5 pl-1.5 pr-2 text-[11px] font-medium text-kumo-default hover:bg-kumo-tint hover:text-kumo-strong"
        >
          <Terminal className="h-3 w-3 text-kumo-brand" />
          代理
          {!streaming && hasLongText && (
            <ChevronDown
              className={`h-3 w-3 text-kumo-subtle transition-transform duration-200 ${isCollapsed ? '' : 'rotate-180'}`}
            />
          )}
        </Button>
        {streaming && !hasText && !pending && (
          <>
            <span className="flex items-center gap-0.5 text-kumo-brand">
              <span className="askai-typing-dot" />
              <span className="askai-typing-dot" />
              <span className="askai-typing-dot" />
            </span>
            <span className="text-[10px]">正在回复…</span>
          </>
        )}
        {msg.status === 'cancelled' && (
          <span className="rounded-full bg-kumo-tint px-1.5 py-0.5 text-[10px] text-kumo-subtle">已停止</span>
        )}
        {msg.status === 'error' && (
          <span className="rounded-full bg-kumo-danger/10 px-1.5 py-0.5 text-[10px] text-kumo-danger">出错了</span>
        )}
      </div>
      <div className="w-full">
        {!isCollapsed && (
        <div className="askai-collapse" data-open={streaming || !isCollapsed}>
        <div
          className={`w-full max-w-full rounded-xl px-4 py-3 text-sm !leading-relaxed ${
            streaming
              ? 'bg-kumo-base ring-1 ring-kumo-brand/30'
              : 'bg-kumo-base ring-1 ring-kumo-line'
          }`}
        >
          {pending ? (
            <div className="flex flex-col gap-2 py-0.5">
              <div className="askai-skeleton-line w-11/12" />
              <div className="askai-skeleton-line w-2/3" />
            </div>
          ) : parts.length === 0 ? null : (
            <div className="flex flex-col gap-2">
              {segments.map((seg, si) => {
                if (Array.isArray(seg)) {
                  return <ToolSteps key={`tg-${si}`} items={seg} streaming={streaming} />;
                }
                const part = seg;
                const isText = part.type === 'text';
                if (isText) textSeq++;
                // 前一段是工具步骤组（合并数组）或游离工具结果 → 正文加顶部留白（gap-2 之上补 mt-1，总 12px）
                const prevIsTool =
                  (si > 0 && Array.isArray(segments[si - 1])) ||
                  (si > 0 && segments[si - 1]?.type === 'tool_result');
                return (
                  <TimelinePart
                    key={`${si}-${part.type}`}
                    part={part}
                    streaming={streaming && isText && textSeq === lastTextIdx && part.text === lastText}
                    className={prevIsTool && isText ? 'mt-1' : undefined}
                    onResolveApproval={onResolveApproval}
                    onRetry={onRetry}
                  />
                );
              })}
            </div>
          )}
        </div>
        </div>
        )}
      </div>
    </div>
  );
}

/* ---------- 消息列表 ---------- */
export default function MessageList({ messages, onResolveApproval, onRetry, onEditResend }) {
  const listRef = useRef(null);
  const userScrolledUp = useRef(false);
  const [collapsedIds, setCollapsedIds] = useState({});
  const [editing, setEditing] = useState(null);
  const [editWidth, setEditWidth] = useState(null);
  const [copiedEdit, setCopiedEdit] = useState(false);
  const editRef = useRef(null);

  useEffect(() => {
    if (editing) editRef.current?.focus();
  }, [editing]);

  const resizeEditBox = () => {
    const el = editRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 192)}px`;
  };
  useEffect(() => {
    if (editing) resizeEditBox();
  }, [editing]);

  const saveEdit = () => {
    const text = (editing?.text || '').trim();
    if (!text) return;
    const id = editing.id;
    setEditing(null);
    onEditResend?.(id, text);
  };

  const handleCopyEdit = async () => {
    try {
      await navigator.clipboard.writeText(editing?.text || '');
      setCopiedEdit(true);
      setTimeout(() => setCopiedEdit(false), 2000);
    } catch {
    }
  };

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
    <div ref={listRef} onScroll={handleScroll} className="h-full overflow-y-auto overscroll-contain scrollbar-thin px-1.5 pt-4 pb-4">
      <div className="flex w-full flex-col gap-4">
        {messages.map((msg, idx) => {
          const streaming = isStreaming(msg.status);
          const parts = msg.parts || [];
          const hasLongText = parts.some((p) => p.type === 'text' && (p.text || '').length > 800);
          const msgKey = msg.id || idx;
          const isCollapsed = hasLongText && !!collapsedIds[msgKey];
          const toggleCollapse = () => setCollapsedIds((prev) => ({ ...prev, [msgKey]: !prev[msgKey] }));
          return (
          <article
            key={msgKey}
            className={`flex w-full flex-col gap-2 ${msg.role === 'user' ? 'items-end' : 'items-start'}`}
          >
            {msg.role === 'user' ? (
              editing && editing.id === msg.id ? (
                <div className="flex w-fit max-w-full items-end gap-1.5">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    shape="square"
                    onClick={handleCopyEdit}
                    className="!h-6 !w-6 shrink-0 !rounded-full !p-0 !text-kumo-subtle"
                    aria-label="复制消息"
                    title="复制消息内容"
                  >
                    {copiedEdit ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                  </Button>
                  <div className="w-fit min-w-[10rem] max-w-full" style={editWidth ? { width: editWidth } : undefined}>
                    <div className="w-full rounded-2xl rounded-tr-md bg-gradient-to-b from-kumo-brand to-kumo-brand-hover px-4 py-2.5 shadow-[0_1px_2px_rgba(0,0,0,0.12)]">
                      <Textarea
                        ref={editRef}
                        rows={1}
                        value={editing.text}
                        onChange={(e) => {
                          setEditing((prev) => ({ ...prev, text: e.target.value }));
                          resizeEditBox();
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                            e.preventDefault();
                            saveEdit();
                          }
                        }}
                        className="!ring-0 max-h-48 w-full resize-none rounded-lg border-0 bg-transparent p-0 text-sm !leading-relaxed text-white outline-none placeholder:text-white/50"
                        style={{ maxHeight: 192 }}
                      />
                    </div>
                    <div className="mt-1.5 flex items-center justify-end gap-1.5">
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => { setEditing(null); setEditWidth(null); }}
                        className="!text-kumo-subtle hover:!bg-kumo-tint hover:!text-kumo-default"
                      >
                        取消
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="primary"
                        onClick={saveEdit}
                        disabled={!editing.text.trim()}
                      >
                        发送
                      </Button>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="group relative flex w-fit max-w-full items-end gap-1.5">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    shape="square"
                    onClick={(e) => {
                      const bubble = e.currentTarget.closest('article')?.querySelector('[data-user-bubble]');
                      setEditWidth(bubble ? bubble.getBoundingClientRect().width : null);
                      setEditing({ id: msg.id, text: msg.content || '' });
                    }}
                    className="mb-0.5 !h-6 !w-6 shrink-0 !rounded-full !p-0 !text-kumo-subtle opacity-0 transition-all duration-200 group-hover:opacity-100 hover:!bg-kumo-tint hover:!text-kumo-default"
                    aria-label="编辑重发"
                  >
                    <Edit className="h-3 w-3" />
                  </Button>
                  <div data-user-bubble className="min-w-0 rounded-2xl rounded-tr-md bg-gradient-to-b from-kumo-brand to-kumo-brand-hover px-4 py-2.5 text-sm !leading-relaxed text-white shadow-[0_1px_2px_rgba(0,0,0,0.12)]">
                    <TextBlock text={msg.content} />
                  </div>
                </div>
              )
            ) : (
              <AssistantMessage
                msg={msg}
                streaming={streaming}
                onResolveApproval={onResolveApproval}
                onRetry={onRetry}
                isCollapsed={isCollapsed}
                toggleCollapse={toggleCollapse}
              />
            )}
          </article>
          );
        })}
      </div>
    </div>
  );
}
