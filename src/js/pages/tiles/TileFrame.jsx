// TileFrame —— 图块卡片壳（复刻 Cloudflare DashboardChartShell）：
// 标题截断 + 内容区撑满（flex-1 min-h-0），悬停 ring 高亮，区域内防文字选中。
// 标题行固定 h-7 并垂直居中：无论右上角是否有 action（如下拉框），所有卡标题行严格等高，
// 内容区高度一致，避免 action 撑高导致同排卡底部元素错位。
import React from 'react';

export default function TileFrame({ title, action, children, className = '' }) {
  return (
    <div className={`flex h-full min-h-0 select-none flex-col overflow-hidden rounded-lg bg-kumo-base ring ring-kumo-line transition-colors hover:ring-kumo-fill ${className}`}>
      <div className="tile-header flex h-8 shrink-0 items-center gap-3 px-4">
        <div className="min-w-0 flex-1 truncate text-xs font-medium text-kumo-subtle" title={title}>
          {title}
        </div>
        {action}
      </div>
      <div className="min-h-0 flex-1 overflow-hidden p-0">{children}</div>
    </div>
  );
}