// StatTileCard —— 通用统计卡：图标 + 数值 + 单行明细，全档位统一三行结构。
// 三行 = TileFrame 标题（名称）/ 大数值居中（图标同行）/ 底部单行明细。
// 不再单独渲染 label 行（与标题重复会导致 1×2 等尺寸变成 4 行）。
import React from 'react';

export default function StatTileCard({ icon: Icon, value, detail, tone = 'default', density = 'full' }) {
  const iconColor = {
    brand: 'text-brand',
    success: 'text-kumo-success',
    warning: 'text-kumo-warning',
    danger: 'text-kumo-danger',
    default: 'text-kumo-subtle',
  }[tone] || 'text-kumo-subtle';
  const rich = density === 'rich';
  const valueClass = density === 'half' ? 'text-xl' : rich ? 'text-3xl' : 'text-2xl';

  return (
    <div className="flex h-full min-h-0 flex-col justify-center gap-1 overflow-hidden px-4 pb-1.5 pt-1">
      <div className="flex items-center justify-center gap-1.5">
        {Icon && <Icon className={`h-4 w-4 shrink-0 ${iconColor}`} />}
        <span className={`truncate font-semibold leading-tight text-kumo-default tabular-nums ${valueClass}`}>
          {value}
        </span>
      </div>
      {detail ? (
        <div className="animate-tile-fade-up shrink-0 truncate text-center text-[10px] text-kumo-subtle" title={detail}>{detail}</div>
      ) : null}
    </div>
  );
}