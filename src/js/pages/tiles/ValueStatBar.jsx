// 数值统计条：大数值 + 环比 + 按宽度档递增的附加统计项。
// half 档 1 列时垂直居中排列（避免窄卡横向溢出），其余横向 baseline 对齐。
import React from 'react';
import StatValue from './StatValue.jsx';

export default function ValueStatBar({ value, delta, items = [], half = false, tier = 'narrow' }) {
  const vertical = half && tier === 'narrow';
  return (
    <div className={vertical ? 'flex flex-col items-center gap-0.5' : 'flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-0.5'}>
      <StatValue value={value} delta={delta} />
      {items.map((it, i) => (
        <span key={i} className="animate-tile-fade-up shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
          {it.label} {it.value}
        </span>
      ))}
    </div>
  );
}
