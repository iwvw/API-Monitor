// 定时任务：任务条目列表（一列显示，右侧标注启用/停用状态），0.5×1 起显示具体条目。
import React from 'react';
import TileEntry from './TileEntry.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { navigateModule } from './constants.js';

export default function SchedulerTile({ dash, density = 'full', w = 1 }) {
  const list = Array.isArray(dash?.scheduler?.list) ? dash.scheduler.list : [];
  const total = dash?.scheduler?.total ?? 0;
  const enabled = dash?.scheduler?.enabled ?? 0;

  if (!dash) return <TileSkeleton variant="list" rows={4} />;
  if (!list.length) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 px-4 pb-1.5 pt-1">
        <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{enabled}/{total}</span>
        <span className="text-[10px] text-kumo-subtle">已启用的计划任务</span>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div className="flex min-h-0 flex-col gap-1.5 overflow-y-auto tile-scroll">
        {list.map((t, i) => (
          <TileEntry
            key={`${t.name}-${i}`}
            name={t.name || '未命名任务'}
            title={t.name}
            onClick={() => navigateModule('scheduler')}
            trailing={
              <span className={`shrink-0 text-[10px] ${t.enabled ? 'text-kumo-success' : 'text-kumo-subtle'}`}>
                {t.enabled ? '启用' : '停用'}
              </span>
            }
          />
        ))}
      </div>
    </div>
  );
}
