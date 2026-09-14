// PaaS 实例：条目列表（平台图标 Koyeb/Fly + 名称 + 运行状态点），条目溢出可滚动。
import React from 'react';
import TileEntry from './TileEntry.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { navigateModule, widthTier } from './constants.js';
import { KoyebBrand, FlyIoBrand } from '../../components/Icons.jsx';

export default function PaasTile({ dash, density = 'full', w = 1 }) {
  const list = Array.isArray(dash?.paas?.list) ? dash.paas.list : [];
  const total = dash?.paas ? dash.paas.koyeb.total + dash.paas.fly.total : 0;
  const running = dash?.paas ? dash.paas.koyeb.running + dash.paas.fly.running : 0;
  const isHalf = density === 'half';
  const tier = widthTier(w);

  if (!dash || !dash.paas) return <TileSkeleton variant="list" rows={4} />;
  if (!list.length) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 px-4 pb-1.5 pt-1">
        <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{running}/{total}</span>
        <span className="text-[10px] text-kumo-subtle">运行中实例</span>
      </div>
    );
  }

  const cols = isHalf ? (tier === 'wide' ? 4 : 2) : tier === 'wide' ? 4 : 2;
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div
        className="grid min-h-0 flex-1 content-start gap-1.5 overflow-y-auto tile-scroll"
        style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
      >
        {list.map((p, i) => {
          const PlatformIcon = p.kind === 'koyeb' ? KoyebBrand : FlyIoBrand;
          return (
            <TileEntry
              key={`${p.kind}-${p.name}-${i}`}
              name={p.name || '未命名实例'}
              title={p.name}
              onClick={() => navigateModule('paas')}
              leading={<PlatformIcon className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />}
              trailing={
                <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${p.status === 'running' ? 'bg-kumo-success' : 'bg-kumo-fill'}`} />
              }
            />
          );
        })}
      </div>
    </div>
  );
}
