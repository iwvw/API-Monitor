import React from 'react';
import { LayerCard } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { LayoutSidebar, X } from '../Icons.jsx';
import { iconButtonIconClass } from '../ui/AppPrimitives.jsx';

/**
 * 可配置的右侧面板容器。
 * 接受任意子组件作为面板内容，支持显隐切换。
 */
export default function DocumentSidebar({
  open = false,
  onToggle,
  title = '面板',
  width = 'w-72',
  children,
  className = '',
}) {
  return (
    <LayerCard
	  className={`hidden shrink-0 flex-col rounded-none transition-[width] duration-200 cq-xl:flex ${
        open ? width : 'w-0 overflow-hidden ring-0'
      } ${className}`.trim()}
    >
      {open && (
        <>
          <div className="flex shrink-0 items-center justify-between border-b border-kumo-line px-3 py-2">
            <span className="text-xs font-semibold text-kumo-strong">{title}</span>
			{onToggle && <Button
              type="button"
              size="sm"
              variant="ghost"
              shape="square"
              aria-label={`关闭${title}`}
              icon={<X className={iconButtonIconClass} />}
              onClick={onToggle}
			/>}
          </div>
          <div className="min-h-0 flex-1 overflow-auto">{children}</div>
        </>
      )}
    </LayerCard>
  );
}

/**
 * 右侧面板切换按钮。
 */
export function DocumentSidebarToggle({ open, onToggle, label = '切换面板' }) {
  return (
    <Button
      type="button"
      size="sm"
      variant={open ? 'primary' : 'secondary'}
      shape="square"
      aria-label={open ? `关闭${label}` : `打开${label}`}
      title={open ? `关闭${label}` : `打开${label}`}
      icon={<LayoutSidebar className={iconButtonIconClass} />}
      onClick={onToggle}
    />
  );
}
