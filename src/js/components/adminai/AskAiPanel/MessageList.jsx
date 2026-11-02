import React, { useEffect, useRef, useState } from 'react';
import { Button, Loader, Textarea, Table } from '@cloudflare/kumo';
import { CodeHighlighted } from '@cloudflare/kumo/code';
import { Sparkle, Terminal, MessageSquare, Globe, Server, Cloud, Clock, Sliders, Bell, FlyIoBrand, KoyebBrand, Copy, Check, X, Edit, ChevronDown, RotateCw } from '../../Icons.jsx';
import ToolCallCard, { toolLabel, toolPathLabel, ToolSteps } from './ToolCallCard.jsx';
import ApprovalCard from './ApprovalCard.jsx';
import { AnimatedCollapse } from '../../AnimatedCollapse.jsx';
import { TracePill, TraceChevron, TraceTypingDots } from '../primitives/TracePill.jsx';
import { MessageBubble, MessageRow } from '../primitives/MessageBubble.jsx';
import { isStreaming } from '../../../modules/adminAiMessages.js';
import { streamPhaseLabel } from './phaseLabel.js';
import { typewriterFrame } from '../../../modules/typewriter.js';

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
      return <a key={i} href={href || undefined} target="_blank" rel="noopener noreferrer" className="text-brand underline">{linkMatch[1]}</a>;
    }
    return part;
  });
}

/* ---------- 代码块（行号仅在多行时显示，单行加了反而噪音） ---------- */
function CodeBlock({ code, language }) {
  const multiline = (code || '').includes('\n');
  return (
    <CodeHighlighted
      code={code}
      lang={language || 'bash'}
      variant="plain"
      showCopyButton
      showLineNumbers={multiline}
      className="my-2 rounded-lg border border-kumo-line"
    />
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
      <Table className="text-xs">
        <Table.Header>
          <Table.Row>
            {headerCells.map((c, i) => (
              <Table.Head key={i} className="text-left">{renderInline(c)}</Table.Head>
            ))}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {bodyRows.map((row, ri) => (
            <Table.Row key={ri}>
              {row.map((c, ci) => (
                <Table.Cell key={ci}>{renderInline(c)}</Table.Cell>
              ))}
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
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

/* ---------- 富文本渲染（标题/列表/代码块/表格/行内样式；折叠由外层 AnimatedCollapse 控制） ---------- */
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
  let codeLang = '';
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
      elements.push(<CodeBlock key={elements.length} code={codeLines.join('\n')} language={codeLang} />);
      codeLines = null;
      codeLang = '';
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
        codeLang = line.trimStart().slice(3).trim().split(/\s+/)[0] || '';
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
      const cls = level <= 1 ? 'text-base font-semibold' : level === 2 ? 'text-sm font-semibold' : level === 3 ? 'text-sm font-semibold' : 'text-[13px] font-semibold text-kumo-default';
      elements.push(<h4 key={elements.length} className={`${cls} text-kumo-strong`}>{renderInline(heading[2])}</h4>);
      continue;
    }
    const indent = line.match(/^\s*/)[0].length;
    const nestStyle = indent > 0 ? { paddingLeft: `${Math.min(indent, 6) * 10}px` } : undefined;
    const taskMatch = trimmed.match(/^[-*]\s+\[(x| )\]\s+(.*)$/i);
    if (taskMatch) {
      elements.push(
        <div key={elements.length} className="flex items-start gap-1.5" style={nestStyle}>
          <span className={`mt-0.5 flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border ${taskMatch[1].toLowerCase() === 'x' ? 'border-brand bg-brand/15 text-brand' : 'border-kumo-line text-transparent'}`}>
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

/* ---------- 资源引用 chips（user 消息下方：@ 选择的结构化引用） ---------- */
const MENTION_ICONS = {
  zone: Globe,
  host: Server,
  task: Clock,
  account: Cloud,
  flyio: FlyIoBrand,
  koyeb: KoyebBrand,
  node: Sliders,
  channel: Bell,
};
function MentionChips({ mentions }) {
  if (!mentions || mentions.length === 0) return null;
  return (
    <div className="mb-1 flex max-w-full flex-col items-end gap-1">
      {mentions.map((m, i) => {
        const Icon = MENTION_ICONS[m.type] || Globe;
        return (
          <span
            key={`${m.type}-${m.id}-${i}`}
            title={`${m.type}: ${m.id}`}
            className="flex max-w-[240px] select-none items-center gap-1 rounded-full border border-kumo-line/60 bg-kumo-recessed/60 px-2 py-0.5 text-[11px] text-kumo-default"
          >
            <Icon className="h-3 w-3 shrink-0 text-brand" />
            <span className="truncate">{m.name}</span>
          </span>
        );
      })}
    </div>
  );
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

// 摘要强制无标点：删除全部中文/英文标点与空白（后端生成时已清洗，这里兜底历史消息）。
const SUMMARY_PUNCT_RE = /[\u3000-\u303f\uff00-\uffef"'(),.;:!?/\\\s]/g;
function cleanSummaryText(text) {
  if (!text) return '';
  return text.replace(SUMMARY_PUNCT_RE, '');
}

function ReasoningPart({ part, streaming, isLastPart }) {
  const [open, setOpen] = useState(false);
  const [followOn, setFollowOn] = useState(true);
  const scrollRef = useRef(null);
  // 推理中：内容每次增量后自动滚动到底部（跟随最新进度）；点击药丸可暂停/恢复跟随
  useEffect(() => {
    if (followOn && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [part.text, followOn]);
  if (!part.text && !streaming) return null;
  const displaySummary = summarizeByPunctuation(cleanSummaryText(part.summary));
  return (
    <div className="flex flex-col gap-1">
      <TracePill
        onClick={() => (streaming ? setFollowOn(!followOn) : setOpen(!open))}
        title={streaming ? (followOn ? '暂停跟随推理' : '恢复跟随推理') : (open ? '收起推理' : '查看完整推理')}
        icon={<Sparkle weight="fill" className={`h-4 w-4 shrink-0 text-brand ${streaming && isLastPart ? 'askai-live-icon' : ''}`} />}
        trailing={!(streaming && isLastPart) && <TraceChevron open={open} />}
      >
        {streaming || !displaySummary ? <span className="shrink-0">推理</span> : null}
        {streaming && isLastPart ? (
          <TraceTypingDots className="ml-0.5 text-brand" />
        ) : (
          displaySummary && (
            <span className="askai-reason-fade max-w-[240px] min-w-0 truncate leading-4">
              {displaySummary}
            </span>
          )
        )}
      </TracePill>
      <AnimatedCollapse open={(streaming && isLastPart) || open}>
        <div
          ref={scrollRef}
          className="askai-reason-fade max-h-[220px] overflow-y-auto overscroll-contain border-l-2 border-kumo-line pl-3 pr-1"
          title={streaming ? '推理过程中（自动跟随最新内容）' : undefined}
        >
          <div className="text-xs !leading-relaxed text-kumo-subtle/90">
            <RenderLines text={part.text} />
          </div>
        </div>
      </AnimatedCollapse>
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
        {copied ? <Check className="h-3 w-3 shrink-0 text-brand" /> : <Copy className="h-3 w-3 shrink-0 text-transparent group-hover:text-kumo-subtle" />}
      </Button>
    </div>
  );
}

/* ---------- timeline part 分发（按时间序逐行渲染） ---------- */
function TimelinePart({ part, streaming, isLastPart, onResolveApproval, onRetry, className }) {
  switch (part.type) {
    case 'reasoning':
      return <ReasoningPart part={part} streaming={streaming} isLastPart={isLastPart} />;
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
          <Loader size={10} className="shrink-0 animate-spin text-brand" />
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

/* ---------- 助手消息 footer 动作（复制整条回复 / 重新生成） ----------
 * 仅在消息终态显示：流式中重新生成没有意义，且后端活跃 run 会 409。
 * 复制内容取该消息所有 text part 拼接，不含推理与工具结果。 */
function MessageActions({ msg, streaming, onRegenerate }) {
  const [copied, setCopied] = useState(false);
  const text = (msg.parts || [])
    .filter((p) => p.type === 'text' && p.text)
    .map((p) => p.text)
    .join('\n\n');
  const disabled = streaming || msg.status === 'cancelled';
  if (!text && !onRegenerate) return null;
  const handleCopy = async () => {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
    }
  };
  return (
    <div className="flex items-center gap-0.5 text-kumo-subtle">
      {text && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          shape="square"
          onClick={handleCopy}
          className="!h-6 !w-6 !rounded-md !p-0 text-kumo-subtle hover:!bg-kumo-tint hover:!text-kumo-default"
          aria-label="复制回复"
          title="复制回复"
        >
          {copied ? <Check className="h-3 w-3 text-brand" /> : <Copy className="h-3 w-3" />}
        </Button>
      )}
      {onRegenerate && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          shape="square"
          onClick={() => onRegenerate(msg.id)}
          disabled={disabled}
          className="!h-6 !w-6 !rounded-md !p-0 text-kumo-subtle hover:!bg-kumo-tint hover:!text-kumo-default disabled:opacity-40"
          aria-label="重新生成"
          title={disabled ? '流式进行中，无法重新生成' : '重新生成'}
        >
          <RotateCw className="h-3 w-3" />
        </Button>
      )}
    </div>
  );
}

/* ---------- 助手消息（timeline：推理/工具/正文按时间序） ---------- */
function AssistantMessage({ msg, streaming, live, onResolveApproval, onRetry, onEditResend, onRegenerate, isCollapsed, toggleCollapse, mode }) {
  const parts = msg.parts || [];
  // 仅本条消息处于流式/占位状态才打 live 徽章：markLiveMessage 只把目标消息标 STREAMING，
  // 否则多轮会话里每条历史 assistant 消息都会误显示「正在执行工具…」
  const liveActive = !!live && !!live.runId && isStreaming(msg.status);
  const pending = streaming && parts.length === 0;
  const hasText = parts.some((p) => p.type === 'text' && p.text);
  const textParts = parts.filter((p) => p.type === 'text');
  const lastTextIdx = textParts.length - 1;
  const lastText = textParts.length > 0 ? textParts[textParts.length - 1].text : '';
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
    <MessageRow
      header={(
        <div className="mb-1 flex items-center gap-1.5 text-xs text-kumo-subtle">
          <TracePill
            emphasis
            onClick={toggleCollapse}
            title={isCollapsed ? '展开回复' : '收起回复'}
            icon={mode === 'ask' ? (
              <MessageSquare className={`h-4 w-4 shrink-0 text-brand ${streaming || liveActive ? 'askai-live-icon' : ''}`} />
            ) : (
              <Terminal className={`h-4 w-4 shrink-0 text-brand ${streaming || liveActive ? 'askai-live-icon' : ''}`} />
            )}
            trailing={!streaming && <TraceChevron open={!isCollapsed} />}
          >
            {mode === 'ask' ? '询问' : '代理'}
          </TracePill>
          {(streaming && !hasText && !pending) || liveActive ? (
            <span className="flex items-center gap-1.5 text-brand">
              <TraceTypingDots />
              <span className="text-[10px]">
                {liveActive ? livePhaseLabel(live.phase) : streamPhaseLabel(parts)}
              </span>
            </span>
          ) : null}
          {msg.status === 'cancelled' && (
            <span className="rounded-full bg-kumo-tint px-1.5 py-0.5 text-[10px] text-kumo-subtle">已停止</span>
          )}
          {msg.status === 'error' && (
            <span className="rounded-full bg-kumo-danger/10 px-1.5 py-0.5 text-[10px] text-kumo-danger">出错了</span>
          )}
        </div>
      )}
      footer={!pending && !streaming && <MessageActions msg={msg} streaming={streaming} onRegenerate={onRegenerate} />}
    >
      <div className="w-full">
        <AnimatedCollapse open={!isCollapsed}>
        <MessageBubble variant="assistant" streaming={streaming}>
          {pending ? (
            <div className="flex flex-col gap-2 py-0.5">
              <div className="askai-skeleton-line w-11/12" />
              <div className="askai-skeleton-line w-2/3" />
            </div>
          ) : parts.length === 0 ? null : (
            <div className="flex flex-col gap-2">
              {segments.map((seg, si) => {
                if (Array.isArray(seg)) {
                  return <ToolSteps key={`tg-${si}`} items={seg} streaming={streaming} isLastPart={si === segments.length - 1} />;
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
                    isLastPart={si === segments.length - 1}
                    streaming={!liveActive && streaming && (isText ? (textSeq === lastTextIdx && part.text === lastText) : true)}
                    className={prevIsTool && isText ? 'mt-1' : undefined}
                    onResolveApproval={onResolveApproval}
                    onRetry={onRetry}
                  />
                );
              })}
            </div>
          )}
        </MessageBubble>
        </AnimatedCollapse>
      </div>
    </MessageRow>
  );
}

// 外部 run 阶段文案（live 模式头部状态指示）
function livePhaseLabel(phase) {
  if (phase === 'tooling') return '正在执行工具…';
  if (phase === 'thinking') return '正在思考…';
  return '正在回复…';
}

/* ---------- 消息列表 ---------- */
export default function MessageList({ messages, mode, live, hasMoreOlder, loadingOlder, onLoadOlder, onResolveApproval, onRetry, onEditResend, onRegenerate }) {
  const listRef = useRef(null);
  const userScrolledUp = useRef(false);
  const [collapsedIds, setCollapsedIds] = useState({});
  const [editing, setEditing] = useState(null);
  const [editWidth, setEditWidth] = useState(null);
  const [copiedEdit, setCopiedEdit] = useState(false);
  const [atBottom, setAtBottom] = useState(true);
  const editRef = useRef(null);
  // 向上懒加载时保留视口：记录本次渲染前的首条 id 与滚动高度，检测到「更早内容
  // 前插」后用高度差补偿 scrollTop，让用户正看着的消息不被顶走。
  const prevFirstIdRef = useRef(null);
  const prevScrollHeightRef = useRef(0);
  const loadingOlderRef = useRef(false);

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
    if (!el) return;
    const firstId = messages?.[0]?.id ?? null;
    const prevFirst = prevFirstIdRef.current;
    const prepended = prevFirst !== null && firstId !== prevFirst && messages?.some((m) => m.id === prevFirst);
    if (prepended) {
      // 更早消息前插：按新增高度补偿滚动位置，保持当前阅读锚点不跳。
      const delta = el.scrollHeight - prevScrollHeightRef.current;
      if (delta > 0) el.scrollTop += delta;
    } else if (!userScrolledUp.current) {
      el.scrollTop = el.scrollHeight;
    }
    prevFirstIdRef.current = firstId;
    prevScrollHeightRef.current = el.scrollHeight;
  }, [messages]);

  useEffect(() => { loadingOlderRef.current = !!loadingOlder; }, [loadingOlder]);

  const handleScroll = () => {
    const el = listRef.current;
    if (!el) return;
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    userScrolledUp.current = !bottom;
    setAtBottom(bottom);
    // 接近顶部且还有更早历史：触发懒加载（loadingOlder 防止并发重复请求）。
    if (el.scrollTop < 120 && hasMoreOlder && !loadingOlderRef.current) {
      onLoadOlder?.();
    }
  };

  const scrollToBottom = () => {
    const el = listRef.current;
    if (!el) return;
    userScrolledUp.current = false;
    setAtBottom(true);
    el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
  };

  if (!messages || messages.length === 0) return null;

  return (
    <div className="relative h-full">
      <div ref={listRef} onScroll={handleScroll} className="h-full overflow-y-auto overscroll-contain scrollbar-thin px-1.5 pt-4 pb-4">
      <div className="flex w-full flex-col gap-4">
        {(loadingOlder || hasMoreOlder) && (
          <div className="flex items-center justify-center py-1 text-[11px] text-kumo-subtle">
            {loadingOlder ? (
              <span className="flex items-center gap-1.5">
                <Loader size={10} className="animate-spin text-brand" />
                正在加载更早的消息…
              </span>
            ) : (
              <span className="text-kumo-subtle/60">向上滚动加载更早的消息</span>
            )}
          </div>
        )}
        {messages.map((msg, idx) => {
          const streaming = isStreaming(msg.status);
          const msgKey = msg.id || idx;
          const isCollapsed = !!collapsedIds[msgKey];
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
                    <MessageBubble variant="user" shape="chat">
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
                    </MessageBubble>
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
                <div className="group relative flex w-fit max-w-full flex-col items-end">
                  <MentionChips mentions={msg.mentions} />
                  <div className="flex items-end gap-1.5">
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
                    className="mb-0.5 !h-6 !w-6 shrink-0 !rounded-full !p-0 !text-kumo-subtle opacity-0 transition-opacity duration-base group-hover:opacity-100 hover:!bg-kumo-tint hover:!text-kumo-default"
                    aria-label="编辑重发"
                  >
                    <Edit className="h-3 w-3" />
                  </Button>
                  <MessageBubble variant="user" shape="chat" data-user-bubble>
                    <TextBlock text={msg.content} />
                  </MessageBubble>
                  </div>
                </div>
              )
            ) : (
              <AssistantMessage
                msg={msg}
                streaming={streaming}
                live={live}
                onResolveApproval={onResolveApproval}
                onRetry={onRetry}
                onRegenerate={onRegenerate}
                isCollapsed={isCollapsed}
                toggleCollapse={toggleCollapse}
                mode={mode}
              />
            )}
          </article>
          );
        })}
      </div>
      </div>
      {!atBottom && (
        <Button
          type="button"
          size="sm"
          variant="secondary"
          shape="circle"
          onClick={scrollToBottom}
          className="!absolute bottom-4 left-1/2 !h-8 !w-8 -translate-x-1/2 !p-0 shadow-md"
          aria-label="回到最新"
          title="回到最新"
        >
          <ChevronDown className="h-4 w-4" />
        </Button>
      )}
    </div>
  );
}
