import React, { useMemo } from 'react';
import { ChartPalette } from '@cloudflare/kumo';
import StatValue from './StatValue.jsx';
import TileChart from './TileChart.jsx';
import HalfTile from './HalfTile.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import ValueStatBar from './ValueStatBar.jsx';
import { widthTier } from './constants.js';
import { pctDelta, seriesSummary, shortDay } from './utils.js';
import { formatTokensAxis, formatTokensZh } from '../openai/utils.js';

export default function ApiTokensTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const trend = useMemo(() => (Array.isArray(data?.trend) ? data.trend : []), [data]);
  const categories = useMemo(() => trend.map((p) => shortDay(p.bucket)), [trend]);
  const values = useMemo(() => trend.map((p) => p.tokens ?? 0), [trend]);
  const delta = useMemo(() => pctDelta(values), [values]);
  const summary = useMemo(() => seriesSummary(values), [values]);
  const color = useMemo(() => ChartPalette.categorical(1, isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const items = useMemo(() => {
    if (tier === 'narrow') return [];
    const list = [];
    if (summary) list.push({ label: '日均', value: formatTokensZh(summary.avg) });
    if (summary) list.push({ label: '峰值', value: formatTokensZh(summary.max) });
    if (tier === 'wide') {
      const lastVal = values[values.length - 1];
      if (lastVal != null) list.push({ label: '今日', value: formatTokensZh(lastVal) });
    }
    return list;
  }, [tier, summary, values]);
  const footnote = useMemo(() => (summary ? `日均 ${formatTokensZh(summary.avg)} · 峰值 ${formatTokensZh(summary.max)}` : null), [summary]);
  const spark = isHalf ? (
    <TileChart
      series={[{ name: '词元用量', color, data: values }]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => formatTokensZh(v)}
    />
  ) : null;

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={formatTokensZh(data?.tokens ?? 0)} delta={delta} />}
          footnote={footnote}
          spark={spark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <ValueStatBar value={formatTokensZh(data?.tokens ?? 0)} delta={delta} items={items} tier={tier} />
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '词元用量', color, data: values }]}
              categories={categories}
              isDarkMode={isDarkMode}
              density={density}
              yAxisTickFormat={formatTokensAxis}
              tooltipValueFormat={(v) => formatTokensZh(v)}
            />
          </div>
        </>
      )}
    </div>
  );
}
