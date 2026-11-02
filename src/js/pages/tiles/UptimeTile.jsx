import React, { useMemo } from 'react';
import TileEntry from './TileEntry.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { navigateModule, widthTier } from './constants.js';

export default function UptimeTile({ data, density = 'full', w = 1 }) {
  const total = data?.total ?? 0;
  const up = data?.up ?? 0;
  const down = total - up;
  const upPct = total ? (up / total) * 100 : 0;
  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => (Array.isArray(data?.items) ? data.items : []), [data]);

  if (!data) return <TileSkeleton variant="list" rows={4} />;

  const head = (
    <>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{up}/{total}</span>
        {density !== 'compact' && <span className="text-sm font-medium text-kumo-subtle">在线 · {down} 离线</span>}
      </div>
      <div className="mt-1 flex h-2 w-full overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className="h-full rounded-full bg-kumo-success" style={{ width: `${upPct}%` }} />
      </div>
    </>
  );

  const entries = (
    <div className="min-h-0 flex-1 overflow-y-auto tile-scroll">
      <div className={`flex flex-col ${isHalf ? 'gap-1' : 'gap-1.5'}`}>
        {items.map((m, i) => {
          const ok = m.lastHeartbeat ? (m.lastHeartbeat.status === 1 || m.lastHeartbeat.status === 'up') : true;
          return (
            <TileEntry
              key={m.id ?? `${m.name ?? ''}-${i}`}
              name={m.name || m.id || ''}
              title={m.name || m.id || ''}
              onClick={() => navigateModule('uptime')}
              leading={
                <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${ok ? 'bg-kumo-success' : 'bg-kumo-danger'}`} />
              }
            />
          );
        })}
      </div>
    </div>
  );

  if (isHalf && tier === 'narrow') {
    // 1×1：不显示条目，数值 + 进度条居中
    return (
      <div className="flex h-full min-h-0 flex-col justify-center gap-1 overflow-hidden px-4 pb-1.5 pt-1">
        <div className="animate-tile-fade-up flex flex-col items-center gap-1">{head}</div>
      </div>
    );
  }
  if (isHalf) {
    // 2×1 / 4×1：左侧数据 + 右侧条目
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">{head}</div>
        </div>
        <div className={`${tier === 'wide' ? 'w-1/2' : 'w-1/3'} flex shrink-0 flex-col overflow-hidden`}>{entries}</div>
      </div>
    );
  }
  // 全高：数值 + 进度条 + 下方条目列表
  return (
    <div className="flex h-full min-h-0 flex-col gap-2 px-4 pb-1.5 pt-1">
      <div className="animate-tile-fade-up shrink-0">{head}</div>
      {density === 'rich' && (
        <div className="shrink-0 text-[10px] text-kumo-subtle">
          {down > 0 ? `${down} 台离线 · 请检查监控状态` : '全部监控在线'}
        </div>
      )}
      {entries}
    </div>
  );
}
