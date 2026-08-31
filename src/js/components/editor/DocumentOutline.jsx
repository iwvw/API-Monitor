import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { ChevronRight } from '../Icons.jsx';

/**
 * 文档大纲 — 基于 Markdown 标题树的导航侧栏。
 * 支持点击跳转、高亮当前标题段、折叠。
 */
export default function DocumentOutline({
  outline = [],
  activeHeadingId,
  onHeadingClick,
  className = '',
}) {
  if (!outline || outline.length === 0) {
    return (
      <div className={`p-3 text-xs text-kumo-subtle ${className}`.trim()}>
        暂无标题
      </div>
    );
  }

  return (
    <nav className={`overflow-auto p-2 ${className}`.trim()} aria-label="文档大纲">
      <div className="mb-2 px-2 text-[11px] font-semibold text-kumo-subtle uppercaser">
        大纲
      </div>
      <ul className="space-y-0.5">
        {outline.map((item) => {
          const isActive = activeHeadingId === item.id;
          const paddingLeft = `${(item.level - 1) * 12 + 8}px`;
          return (
            <li key={item.id}>
              <Button
                type="button"
                variant={isActive ? 'primary' : 'ghost'}
                size="sm"
                className={`w-full !justify-start truncate rounded !px-2 !py-1 text-left text-xs`}
                style={{ paddingLeft }}
                onClick={() => onHeadingClick?.(item)}
                title={item.text}
              >
                <span className="inline-flex items-center gap-1">
                  {isActive && <ChevronRight className="h-3 w-3 shrink-0" />}
                  <span className="truncate">{item.text}</span>
                </span>
              </Button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
