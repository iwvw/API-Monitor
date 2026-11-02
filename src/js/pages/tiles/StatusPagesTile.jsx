// 状态页：公开状态页列表（点击打开）。1×2 默认布局 = 单列稍大卡片（左图标 + 名称 + 打开箭头），
// 更宽尺寸切多列网格；最小高度 1 行。
import React from 'react';
import TileEntry from './TileEntry.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { statusPageHref, widthTier } from './constants.js';
import { PublicPageBrandIcon } from '../../components/public/PublicPageIconPicker.jsx';
import { ArrowRight } from '../../components/Icons.jsx';

export default function StatusPagesTile({ dash, density = 'full', w = 1 }) {
  const list = Array.isArray(dash?.statusPages?.list) ? dash.statusPages.list : [];
  const total = dash?.statusPages?.total ?? list.length;
  const isHalf = density === 'half';
  const tier = widthTier(w);

  if (!dash) return <TileSkeleton variant="list" rows={3} />;
  if (!list.length && !total) {
    return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">暂无状态页</div>;
  }
  if (!list.length) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">共 {total} 个状态页</div>
    );
  }

  // 1 列（1×2 默认）= 单列大卡片；2/3 列 = 两/三列网格；宽半高/宽全高 = 更多列
  const cols = tier === 'narrow' ? 1 : isHalf ? (tier === 'wide' ? 4 : 3) : tier === 'wide' ? 4 : 3;
  const single = cols === 1;
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
      <div className={`grid content-start gap-1.5 ${single ? '' : 'auto-rows-fr'}`} style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {list.map((p, i) => {
          const href = p.url || statusPageHref(p);
          return (
            <TileEntry
              key={`${p.kind}-${p.id || i}`}
              name={p.name || '未命名状态页'}
              href={href || undefined}
              title={p.name || href || ''}
              pad={single ? 'py-2' : 'py-1.5'}
              leading={
                <PublicPageBrandIcon
                  pageKind={p.kind}
                  config={p.config}
                  iconClassName={`shrink-0 ${single ? 'h-4 w-4' : 'h-3.5 w-3.5'}`}
                  customIconClassName={`shrink-0 ${single ? 'h-4 w-4' : 'h-3.5 w-3.5'}`}
                />
              }
              trailing={href ? <ArrowRight className={`shrink-0 text-kumo-inactive ${single ? 'h-3.5 w-3.5' : 'h-3 w-3'}`} /> : undefined}
            />
          );
        })}
      </div>
    </div>
  );
}
