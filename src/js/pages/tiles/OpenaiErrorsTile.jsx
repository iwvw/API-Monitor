import React, { useMemo } from 'react';
import { ChartPalette } from '@cloudflare/kumo';
import StatValue from './StatValue.jsx';
import TileChart from './TileChart.jsx';
import HalfTile from './HalfTile.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import ValueStatBar from './ValueStatBar.jsx';
import { widthTier } from './constants.js';
import { fmtCompact, pctDelta, seriesSummary, shortDay } from './utils.js';

export default function OpenaiErrorsTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const daily = useMemo(() => (Array.isArray(data?.daily) ? data.daily : []), [data]);
  const categories = useMemo(() => daily.map((p) => shortDay(p.day)), [daily]);
  const errors = useMemo(() => daily.map((p) => p.errors ?? 0), [daily]);
  const total = useMemo(() => errors.reduce((a, b) => a + b, 0), [errors]);
  const delta = useMemo(() => pctDelta(errors), [errors]);
  const summary = useMemo(() => seriesSummary(errors), [errors]);
  const color = useMemo(() => ChartPalette.semantic('Attention', isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => {
    if (tier === 'narrow') return [];
    const list = [];
    if (summary) list.push({ label: '日均', value: fmtCompact(summary.avg) });
    if (summary) list.push({ label: '峰值', value: fmtCompact(summary.max) });
    if (tier === 'wide') {
      const lastVal = errors[errors.length - 1];
      if (lastVal != null) list.push({ label: '今日', value: fmtCompact(lastVal) });
    }
    return list;
  }, [tier, summary, errors]);
  const footnote = useMemo(() => (summary ? `日均 ${fmtCompact(summary.avg)} · 峰值 ${fmtCompact(summary.max)}` : null), [summary]);
  const spark = isHalf ? (
    <TileChart
      series={[{ name: '错误', color, data: errors }]}
      categories={categories}
      type="bar"
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
    />
  ) : null;

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={fmtCompact(total)} delta={delta} />}
          footnote={footnote}
          spark={spark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <ValueStatBar value={fmtCompact(total)} delta={delta} items={items} tier={tier} />
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '错误', color, data: errors }]}
              categories={categories}
              type="bar"
              isDarkMode={isDarkMode}
              density={density}
              tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
            />
          </div>
        </>
      )}
    </div>
  );
}
