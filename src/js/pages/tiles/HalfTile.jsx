// half 档内容布局（对齐 Cloudflare 官方半高卡形态）：
// narrow（1 列）：作为 1×2 的高度压缩版 = 数值行 + 压缩高度缩略图（无特殊三行样式）；
// medium（2 列）：左侧数据 + 右侧 mini 缩略趋势图；
// wide（≥4 列）：数值行 + 分数据同行，底部贴边矮图。
import React from 'react';

export default function HalfTile({ tier, stat, footnote, spark, isDarkMode }) {
  if (tier === 'narrow') {
    return (
      <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
        <div className="animate-tile-fade-up shrink-0">{stat}</div>
        {spark && (
          <div className="animate-tile-fade-up -mx-4 -mb-1.5 mt-0.5 min-h-0 flex-1 overflow-hidden">{spark}</div>
        )}
      </div>
    );
  }
  if (tier === 'medium') {
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">{stat}</div>
          {footnote && (
            <div className="animate-tile-fade-up mt-0.5 shrink-0 truncate text-[10px] text-kumo-subtle tabular-nums">{footnote}</div>
          )}
        </div>
        {spark && <div className="animate-tile-fade-up w-1/3 shrink-0 overflow-hidden">{spark}</div>}
      </div>
    );
  }
  return (
    <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1 pt-1">
      <div className="flex min-w-0 flex-1 flex-col justify-center">
        <div className="animate-tile-fade-up shrink-0">{stat}</div>
        {footnote && (
          <div className="animate-tile-fade-up mt-0.5 shrink-0 truncate text-[10px] text-kumo-subtle tabular-nums">{footnote}</div>
        )}
      </div>
      {spark && <div className="animate-tile-fade-up -mr-4 -mb-1 w-1/2 shrink-0 overflow-hidden">{spark}</div>}
    </div>
  );
}
