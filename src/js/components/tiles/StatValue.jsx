// StatValue —— 图块数值行：大数字 + 环比（↗ 绿 / ↘ 红），对齐 Cloudflare 首页样式。
import React from 'react';

export default function StatValue({ value, delta }) {
  return (
    <div className="flex items-baseline gap-2">
      <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{value}</span>
      {delta != null && Number.isFinite(delta) && (
        <span className={`text-sm font-medium ${delta >= 0 ? 'text-kumo-success' : 'text-kumo-danger'}`}>
          {delta >= 0 ? '↗' : '↘'} {Math.abs(delta).toFixed(1)}%
        </span>
      )}
    </div>
  );
}