// 服务器状态卡（与监控可用率同款样式）：数值 + 可用率条 + 服务器条目列表。
// 1×1 不显示条目；半高宽（2×1/4×1）左侧数据 + 右侧条目；全高条目在下方，溢出可滚动。
import React from 'react';
import TileEntry from './TileEntry.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { navigateModule, widthTier } from './constants.js';

export default function ServerStatusTile({ servers, density = 'full', w = 1 }) {
  const tier = widthTier(w);
  const isHalf = density === 'half';
  const total = servers?.total ?? 0;
  const online = servers?.online ?? 0;
  const error = servers?.error ?? 0;
  const offline = Math.max(0, total - online - error);
  const list = Array.isArray(servers?.list) ? servers.list : [];
  const upPct = total ? (online / total) * 100 : 0;

  if (!servers) return <TileSkeleton variant="list" rows={4} />;
  if (!total) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">暂无服务器</div>;

  const head = (
    <>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{online}/{total}</span>
        {density !== 'compact' && <span className="text-sm font-medium text-kumo-subtle">在线 · {offline} 离线</span>}
      </div>
      <div className="mt-1 flex h-2 w-full overflow-hidden rounded-full border border-kumo-line/70 bg-kumo-recessed">
        <div className="h-full rounded-full bg-kumo-success" style={{ width: `${upPct}%` }} />
      </div>
    </>
  );

  const entries = (cols) => (
    <div className="min-h-0 flex-1 overflow-y-auto tile-scroll">
      <div className="grid content-start gap-1.5" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {list.map((m, i) => (
          <TileEntry
            key={`${m.name || ''}-${i}`}
            name={m.name || '未命名'}
            title={m.name || m.host || ''}
            onClick={() => navigateModule('server')}
            leading={
              <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                m.status === 'online' ? 'bg-kumo-success' : m.status === 'error' ? 'bg-kumo-danger' : 'bg-kumo-fill'
              }`} />
            }
          />
        ))}
      </div>
    </div>
  );

  if (isHalf && tier === 'narrow') {
    // 1×1：不显示条目，数值 + 可用率条居中
    return (
      <div className="flex h-full min-h-0 flex-col justify-center gap-1 overflow-hidden px-4 pb-1.5 pt-1">
        <div className="animate-tile-fade-up flex flex-col items-center gap-1">{head}</div>
      </div>
    );
  }
  if (isHalf) {
    // 2×1 / 4×1：左侧数据 + 右侧条目（wide 右侧两列）
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">{head}</div>
        </div>
        <div className={`${tier === 'wide' ? 'w-1/2' : 'w-1/3'} flex shrink-0 flex-col overflow-hidden`}>
          {entries(tier === 'wide' ? 2 : 1)}
        </div>
      </div>
    );
  }
  // 全高：数值 + 可用率条 + 下方条目列表（多列）
  const fullCols = tier === 'wide' ? (density === 'rich' ? 3 : 4) : tier === 'medium' ? 2 : 1;
  return (
    <div className="flex h-full min-h-0 flex-col gap-2 px-4 pb-1.5 pt-1">
      <div className="animate-tile-fade-up shrink-0">{head}</div>
      {density === 'rich' && (
        <div className="shrink-0 text-[10px] text-kumo-subtle">
          {error > 0 ? `${error} 台异常 · ${offline} 台离线` : '全部服务器在线'}
        </div>
      )}
      {entries(fullCols)}
    </div>
  );
}
