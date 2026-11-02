// TileSkeleton —— 卡片加载骨架：按卡片最终形态组合 SkeletonLine（数值行 + 图表块 / 进度条 / 条目行），
// 加载中替代「加载中…」文字占位，保持各卡内容区域的轮廓稳定。
import React from 'react';
import { SkeletonLine } from '@cloudflare/kumo';

export default function TileSkeleton({ variant = 'chart', rows = 0, className = '' }) {
  return (
    <div className={`flex h-full min-h-0 flex-col gap-2 overflow-hidden px-4 pb-2 pt-1 ${className}`} aria-hidden="true">
      <div className="flex shrink-0 items-baseline gap-2">
        <SkeletonLine className="h-5 w-14" />
        <SkeletonLine className="h-3.5 w-24" />
      </div>
      {variant === 'bars' && (
        <div className="flex shrink-0 flex-col gap-1.5">
          <SkeletonLine className="h-3.5 w-full" />
          <SkeletonLine className="h-3.5 w-full" />
          <SkeletonLine className="h-3.5 w-full" />
        </div>
      )}
      {variant === 'list' ? (
        <div className="flex min-h-0 flex-1 flex-col gap-1.5">
          {Array.from({ length: rows }).map((_, i) => (
            <SkeletonLine key={i} className="h-7 w-full shrink-0" />
          ))}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 items-stretch pb-1">
          <SkeletonLine className="h-full w-full" />
        </div>
      )}
    </div>
  );
}
