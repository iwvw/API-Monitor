// TileEntry —— 所有卡片内条目行的统一样式：等宽圆角边框 + 悬停高亮。
// leading=行首图标/状态点，name=主文本（截断），desc=第二行说明（模块入口），badge=名称旁小标记，
// trailing=行尾徽标/箭头；onClick 渲染为按钮，href 渲染为外链（新标签页），两者都无则为静态行。
import React from 'react';

export default function TileEntry({ leading, name, desc, badge, trailing, onClick, href, title, pad = 'py-1', className = '' }) {
  const base = `animate-tile-fade-up flex min-w-0 items-center gap-1.5 rounded-md border border-kumo-line/60 px-1.5 ${pad} text-left transition-colors hover:border-brand/60 hover:bg-kumo-tint ${className}`;
  const body = (
    <>
      {leading}
      {desc ? (
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1">
            <span className="truncate text-[10px] text-kumo-default">{name}</span>
            {badge}
          </span>
          <span className="truncate text-[9px] text-kumo-subtle">{desc}</span>
        </span>
      ) : (
        <>
          <span className="min-w-0 flex-1 truncate text-[10px] text-kumo-default">{name}</span>
          {badge}
        </>
      )}
      {trailing}
    </>
  );
  if (href) {
    return (
      <a href={href} target="_blank" rel="noreferrer" title={title || name} className={base}>
        {body}
      </a>
    );
  }
  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        title={title || name}
        className={`animate-tile-fade-up flex min-w-0 items-center gap-1.5 rounded-md border border-kumo-line/60 px-1.5 ${pad} text-left transition-colors hover:border-brand/60 hover:bg-kumo-tint ${className}`}
      >
        {body}
      </button>
    );
  }
  return (
    <div title={title || name} className={base}>
      {body}
    </div>
  );
}
