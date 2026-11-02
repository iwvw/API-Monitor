import React from 'react';
import { Button } from '@cloudflare/kumo';
import { ChevronDown } from '../../Icons.jsx';

/* 时间线胶囊：推理 / 工具步骤组 / 助手消息头三处共用同一视觉。
 * 此前三处各自复制了一串 Tailwind 类，边框透明度已经开始漂移，统一收敛到这里。 */
const PILL_CLASS = [
  'flex w-max max-w-full cursor-pointer items-center gap-1.5 rounded-lg border',
  'bg-kumo-recessed/60 py-1 pl-1.5 pr-2 text-[11px] text-kumo-default',
  'hover:bg-kumo-recessed hover:text-kumo-strong',
].join(' ');

/* emphasis：助手消息头是消息级外壳，用实线边框与内联 trace 胶囊区分；
 * 其余为内联 trace，用半透明边框。 */
export function TracePill({
  icon,
  children,
  trailing,
  onClick,
  title,
  emphasis = false,
  className = '',
  ariaExpanded,
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      onClick={onClick}
      title={title}
      aria-expanded={ariaExpanded}
      className={`${PILL_CLASS} ${emphasis ? 'border-kumo-line' : 'border-kumo-line/60'} ${className}`}
    >
      {icon}
      {children}
      {trailing}
    </Button>
  );
}

/* 折叠箭头：-rotate-90 收起 / 0 展开。open=true 表示已展开（箭头朝下）。 */
export function TraceChevron({ open }) {
  return (
    <ChevronDown
      className={`h-3 w-3 shrink-0 text-kumo-subtle transition-transform duration-base ${open ? '' : '-rotate-90'}`}
    />
  );
}

/* 流式三点：思考中/等待中的脉冲指示。 */
export function TraceTypingDots({ className = '' }) {
  return (
    <span className={`flex items-center gap-0.5 ${className}`}>
      <span className="askai-typing-dot" />
      <span className="askai-typing-dot" />
      <span className="askai-typing-dot" />
    </span>
  );
}
