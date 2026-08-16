import React, { useEffect, useRef, useState } from 'react';
import { Button, Loader, Textarea } from '@cloudflare/kumo';
import { ChevronDown, Sparkle, Terminal, Copy, Check, X, Edit } from '../Icons.jsx';
import ToolCallCard, { toolLabel, toolPathLabel } from './ToolCallCard.jsx';
import ApprovalCard from './ApprovalCard.jsx';
import { isStreaming } from '../../modules/adminAiMessages.js';

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
      return <a key={i} href={linkMatch[2]} target="_blank" rel="noopener noreferrer" className="text-kumo-brand underline">{linkMatch[1]}</a>;
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
// 固定步长匀速 reveal：后端 delta 可能是一大块（上游 chunk 粒度粗），
// 若按剩余比例放大步长会「先蹦两字、剩余瞬间刷完」；固定小步长让大块也平滑打字。
const TW_CURSOR_STEP = 2; // 每 tick 揭示字符数
const TW_CURSOR_MS = 20; // tick 间隔（约 100 字符/秒）
function TypewriterText({ text, streaming }) {
  const [visible, setVisible] = useState(() => (streaming ? 0 : text.length));
  const visibleRef = useRef(visible);
  const textRef = useRef(text);

  // 文本被整体替换（同一组件实例承载新消息）时从头开始
  useEffect(() => {
    if (!text.startsWith(textRef.current)) {
      textRef.current = text;
      visibleRef.current = 0;
      setVisible(0);
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
    // 表格行：标准 `| a | b |`，也容忍缺首/尾竖线的 `a | b |` / `| a | b` 输出
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
    // 缩进层级：`  - 子项` 这类嵌套列表按前导空格缩进展示
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
      // 无序列表（* / -）渲染为标准项目符号 •，有序列表保留数字
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

/* ---------- 思维链折叠区（reasoning，CF 风格胶囊标签） ----------
 * 收起时显示摘要：优先 AI 生成的标题式摘要（≤10 字），未生成时回退截断；展开显示完整推理。 */
function ReasoningBlock({ text, summary, streaming }) {
  const [open, setOpen] = useState(false);
  if (!text && !streaming) return null;
  // 收起摘要只显示 AI 生成的中文标题摘要；无摘要/非中文时不显示摘要文本
  const isCN = (s) => /[\u4e00-\u9fa5]/.test(s || '');
  const displaySummary = isCN(summary) ? summary : '';
  return (
    <div className="mb-1.5 flex flex-col gap-1">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => setOpen(!open)}
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
            {open ? '隐藏' : '摘要'}
          </span>
        )}
      </Button>
            <div className="askai-collapse" data-open={open}>
        <div className="askai-reason-fade max-h-[220px] overflow-y-auto overscroll-contain border-l-2 border-kumo-line pl-3 pr-1">
          <p className="whitespace-pre-wrap break-words text-xs !leading-relaxed text-kumo-subtle/90">{text}</p>
        </div>
      </div>
      {!open && displaySummary && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => setOpen(true)}
          className="askai-reason-fade flex w-max max-w-full cursor-pointer items-center gap-1.5 text-left"
          title="查看完整推理"
        >
          <span className="truncate text-xs text-kumo-subtle/80">{displaySummary}</span>
        </Button>
      )}
    </div>
  );
}

/* ---------- 思考过程折叠区（thinking 工具步骤，默认展开展示工具状态与作用） ---------- */
function ThinkingBlock({ thinking, streaming }) {
  const [open, setOpen] = useState(true);
  const [expandedGroups, setExpandedGroups] = useState({});
  // 工具步骤视图：语义描述（默认，不展示具体路径）⇄ 实际 API 路径
  const [showPath, setShowPath] = useState(false);
  if (!thinking || thinking.length === 0) return null;

  // 相同展示文本的连续步骤折叠为一组，组标题显示 ×N；分组键随视图模式切换
  const stepKey = (step) => (showPath
    ? toolPathLabel(step.toolName, step.args)
    : (step.desc || toolLabel(step.toolName) || '未知工具'));
  const groups = [];
  for (const step of thinking) {
    const key = stepKey(step);
    const last = groups[groups.length - 1];
    if (last && last.key === key) {
      last.steps.push(step);
    } else {
      groups.push({ key, steps: [step] });
    }
  }
  const toggleGroup = (gi) => setExpandedGroups((prev) => ({ ...prev, [gi]: !prev[gi] }));

  return (
    <div className="mb-2 flex flex-col gap-1">
      {/* 标题行：箭头放进 16px 槽居中（中心 8px），与下面树的竖线/圆点同一垂直线 */}
      <div className="flex items-center gap-1.5">
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => setOpen(!open)}
          className="flex w-max cursor-pointer items-center gap-1 text-[11px] text-kumo-subtle hover:text-kumo-default"
        >
          <span className="flex w-4 shrink-0 justify-center">
            <ChevronDown className={`h-3 w-3 transition-transform duration-200 ${open ? 'rotate-0' : '-rotate-90'}`} />
          </span>
          工具步骤
        </Button>
        <span
          className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-kumo-tint px-1 text-[10px] font-medium leading-none text-kumo-subtle transition-colors hover:text-kumo-default"
          title={`本轮共调用 ${thinking.length} 次工具`}
        >
          {thinking.length}
        </span>
        <div className="flex items-center gap-0.5 rounded-full bg-kumo-tint/70 p-0.5" role="group" aria-label="工具步骤显示模式">
          <Button
            type="button"
            size="xs"
            variant="ghost"
            onClick={() => setShowPath(false)}
            title="显示语义化工具步骤描述"
            className={`!h-auto !rounded-full !px-2 !py-0.5 text-[10px] leading-none ${!showPath ? 'bg-kumo-base text-kumo-default' : 'text-kumo-subtle hover:text-kumo-default'}`}
          >
            语义
          </Button>
          <Button
            type="button"
            size="xs"
            variant="ghost"
            onClick={() => setShowPath(true)}
            title="显示实际调用的 API 路径"
            className={`!h-auto !rounded-full !px-2 !py-0.5 text-[10px] leading-none ${showPath ? 'bg-kumo-base text-kumo-default' : 'text-kumo-subtle hover:text-kumo-default'}`}
          >
            路径
          </Button>
        </div>
      </div>
      <div className="askai-collapse" data-open={open}>
        {/* 工具步骤列表：一条主竖线在 x=8px，标题箭头/各行内容起点对齐；层级靠二级文字 ml-4 缩进。
            统一 text-xs 覆盖外层 text-sm */}
        <div className="askai-reason-fade relative mt-1 text-xs">
          <span className="absolute left-2 top-0 bottom-0 w-px bg-kumo-line" aria-hidden />
          <div className="flex flex-col gap-1.5">
            {groups.map((group, gi) => {
              const multi = group.steps.length > 1;
              const isOpen = !!expandedGroups[gi];
              const finalStatus = group.steps[group.steps.length - 1].status;
              return (
                <div key={gi} className="flex flex-col gap-1.5">
                  {/* 一级行：内容起点与竖线同一 x（标题槽占位保持对齐） */}
                  <div className="askai-tool-stagger flex items-center" style={{ animationDelay: `${Math.min(gi * 90, 1200)}ms` }}>
                    {multi ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => toggleGroup(gi)}
                        title={isOpen ? '收起' : '展开'}
                        className="flex w-full cursor-pointer items-center gap-1.5 rounded-md py-0.5 pl-4 pr-1 text-xs text-kumo-default hover:bg-kumo-tint/60"
                      >
                        <span className="truncate">{group.key}</span>
                        <span className="shrink-0 rounded-full bg-kumo-tint px-1.5 text-[10px] text-kumo-subtle">×{group.steps.length}</span>
                        {finalStatus === 'success' && <Check className="h-3 w-3 shrink-0 text-kumo-success" />}
                        {finalStatus === 'failed' && <X className="h-3 w-3 shrink-0 text-kumo-danger" />}
                        {finalStatus === 'running' && <Loader size={10} className="shrink-0 animate-spin text-kumo-brand" />}
                      </Button>
                    ) : (
                      <div className="pl-4">
                        <ToolCallCard toolCall={group.steps[0]} inline showPath={showPath} />
                      </div>
                    )}
                  </div>
                  {multi && isOpen && (
                    /* 二级：文字 ml-4 缩进区分层级 */
                    <div className="flex flex-col gap-1.5 pb-0.5">
                      {group.steps.map((step, i) => (
                        <div
                          key={i}
                          className="askai-tool-stagger flex items-center"
                          style={{ animationDelay: `${Math.min((gi + i) * 90, 1200)}ms` }}
                        >
                          <div className="ml-8 min-w-0 flex-1">
                            <ToolCallCard toolCall={step} inline showPath={showPath} />
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

/* ---------- 消息定义块分发 ---------- */
function MessageBlock({ block, streaming, onResolveApproval, onRetry }) {
  switch (block.type) {
    case 'text':
      return <TextBlock text={block.text} streaming={streaming} />;
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
              <Button size="xs" variant="secondary" onClick={() => onRetry(block.retryPrompt)}>重试</Button>
            )}
          </div>
        </div>
      );
    default:
      return null;
  }
}

/* ---------- 消息列表 ---------- */
export default function MessageList({ messages, onResolveApproval, onRetry, onEditResend }) {
  const listRef = useRef(null);
  const userScrolledUp = useRef(false);
  // 超长回复折叠状态（按消息 id 独立记忆），由「代理」标签点击切换
  const [collapsedIds, setCollapsedIds] = useState({});
  // 用户消息编辑态：{ id, text }
  const [editing, setEditing] = useState(null);
  const [copiedEdit, setCopiedEdit] = useState(false);
  const editRef = useRef(null);

  useEffect(() => {
    if (editing) editRef.current?.focus();
  }, [editing]);

  /* 编辑输入框按内容自适应宽高（上限对齐气泡 max-w-prose），
     w-fit 容器下 w-full 会塌缩成 textarea 固有宽度导致宽度异常 */
  const resizeEditBox = () => {
    const el = editRef.current;
    if (!el) return;
    el.style.width = '0px';
    el.style.width = `${Math.min(el.scrollWidth, 560)}px`;
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
          const pending = streaming && !msg.reasoning && !msg.thinking?.length && !msg.blocks?.length && !msg.content;
          const hasText = (msg.blocks || []).some((b) => b.type === 'text' && b.text);
          // 正文卡只在有实际内容（文本/审批/错误/工具块）或等待首个事件时渲染，
          // 推理/工具执行阶段不显示空白卡片
          const hasCardContent = pending || !!msg.content || (msg.blocks && msg.blocks.length > 0);
          // 打字机只作用于最后一个文本块（前面的文本块已定型）
          let lastTextIdx = -1;
          (msg.blocks || []).forEach((b, i) => { if (b.type === 'text') lastTextIdx = i; });
          const msgKey = msg.id || idx;
          const hasLongText = (msg.blocks || []).some((b) => b.type === 'text' && (b.text || '').length > 800);
          // 折叠只对超长文本生效：收起后整个正文卡不渲染（不留空白卡片）
          const isCollapsed = hasLongText && !!collapsedIds[msgKey];
          const toggleCollapse = () => setCollapsedIds((prev) => ({ ...prev, [msgKey]: !prev[msgKey] }));
          return (
          <article
            key={msgKey}
            className={`flex w-full flex-col gap-2 ${msg.role === 'user' ? 'items-end' : 'items-start'}`}
          >
            {msg.role === 'user' ? (
              editing && editing.id === msg.id ? (
                <div className="relative w-fit max-w-prose">
                  <div className="flex flex-col gap-2 rounded-2xl rounded-tr-md bg-gradient-to-b from-kumo-brand to-kumo-brand-hover px-4 py-2.5 shadow-[0_1px_2px_rgba(0,0,0,0.12)]">
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
                      className="!ring-0 max-h-48 resize-none rounded-lg border-0 bg-transparent p-0 text-sm !leading-relaxed text-white outline-none placeholder:text-white/50"
                      style={{ maxHeight: 192, minWidth: 280 }}
                    />
                    <div className="flex items-center justify-end gap-1.5">
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => setEditing(null)}
                        className="!text-white/80 hover:!bg-white/15 hover:!text-white"
                      >
                        取消
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        onClick={saveEdit}
                        disabled={!editing.text.trim()}
                        className="!bg-white !text-kumo-brand hover:!bg-white/90"
                      >
                        发送
                      </Button>
                    </div>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    shape="square"
                    onClick={handleCopyEdit}
                    className="absolute -left-8 top-1/2 z-10 !h-6 !w-6 -translate-y-1/2 !rounded-full !text-kumo-subtle hover:!bg-kumo-tint hover:!text-kumo-default"
                    aria-label="复制消息"
                    title="复制消息内容"
                  >
                    {copiedEdit ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                  </Button>
                </div>
              ) : (
                <div className="group relative flex w-fit max-w-prose items-start gap-1.5">
                  <div className="rounded-2xl rounded-tr-md bg-gradient-to-b from-kumo-brand to-kumo-brand-hover px-4 py-2.5 text-sm !leading-relaxed text-white shadow-[0_1px_2px_rgba(0,0,0,0.12)]">
                    <TextBlock text={msg.content} />
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    shape="square"
                    onClick={() => setEditing({ id: msg.id, text: msg.content || '' })}
                    className="absolute -left-8 top-1/2 z-10 !h-6 !w-6 -translate-y-1/2 !rounded-full !text-kumo-subtle opacity-0 transition-all duration-200 group-hover:opacity-100 hover:!bg-kumo-tint hover:!text-kumo-default"
                    aria-label="编辑重发"
                  >
                    <Edit className="h-3 w-3" />
                  </Button>
                </div>
              )
            ) : (
              <div className="flex w-full flex-col gap-1">
                <ReasoningBlock text={msg.reasoning} summary={msg.reasoningSummary} streaming={streaming && !hasText} />
                <ThinkingBlock thinking={msg.thinking} streaming={streaming} />
                <div className="w-full">
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
                  {hasCardContent && (
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
                    ) : (
                      <div className="space-y-2">
                        {msg.blocks && msg.blocks.map((block, bi) => (
                          <MessageBlock
                            key={bi}
                            block={block}
                            streaming={streaming && block.type === 'text' && bi === lastTextIdx}
                            onResolveApproval={onResolveApproval}
                            onRetry={onRetry}
                          />
                        ))}
                        {!msg.blocks && msg.content && <TextBlock text={msg.content} />}
                      </div>
                    )}
                  </div>
                  </div>
                  )}
                </div>
              </div>
            )}
          </article>
          );
        })}
      </div>
    </div>
  );
}