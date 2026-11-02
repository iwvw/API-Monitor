import React from 'react';

/* 消息气泡：只管气泡外观，不管行布局、对齐、状态与动作。
 * 语义参考 shadcn/ui Bubble（variant 决定视觉，align 决定对齐），实现用 Kumo + Tailwind。
 *
 * variant：
 *   user      —— 当前用户，品牌实底
 *   assistant —— 助手正文，中性卡片
 *   error     —— 失败/错误内容
 * align：start（助手）/ end（用户）
 */

const BUBBLE_VARIANTS = {
  user: 'bg-gradient-to-b from-brand to-brand-hover text-white shadow-[0_1px_2px_rgba(0,0,0,0.12)]',
  assistant: 'bg-kumo-base ring-1 ring-kumo-line',
  error: 'bg-kumo-danger/10 ring-1 ring-kumo-danger/30',
};

const BUBBLE_ALIGN = {
  start: 'items-start',
  end: 'items-end',
};

/* 气泡形状：
 *   card —— 助手正文卡片，四角同圆角、px-4 py-3、占满消息列
 *   chat —— 对话气泡，右上角收窄成小圆角、px-4 py-2.5、宽度随内容
 */
const BUBBLE_SHAPES = {
  card: 'w-full max-w-full rounded-xl px-4 py-3',
  chat: 'min-w-0 max-w-full rounded-2xl rounded-tr-md px-4 py-2.5',
};

export function MessageBubble({
  variant = 'assistant',
  shape = 'card',
  streaming = false,
  className = '',
  children,
  ...rest
}) {
  const base = BUBBLE_VARIANTS[variant] || BUBBLE_VARIANTS.assistant;
  const shapeClass = BUBBLE_SHAPES[shape] || BUBBLE_SHAPES.card;
  const streamRing = streaming && variant === 'assistant' ? 'ring-brand/30' : '';
  return (
    <div
      className={`text-sm !leading-relaxed ${shapeClass} ${base} ${streamRing} ${className}`}
      {...rest}
    >
      {children}
    </div>
  );
}

/* 消息行：负责对齐与槽位编排（header 状态条 / body 气泡 / footer 动作）。
 * 参考 shadcn/ui Message 的 MessageContent 结构：header 始终靠起始侧，
 * footer 跟随消息所在侧。 */
export function MessageRow({
  align = 'start',
  header = null,
  footer = null,
  className = '',
  children,
}) {
  const side = BUBBLE_ALIGN[align] || BUBBLE_ALIGN.start;
  return (
    <div className={`flex w-full flex-col gap-1 ${side} ${className}`}>
      {header}
      {children}
      {footer}
    </div>
  );
}

export default MessageBubble;
