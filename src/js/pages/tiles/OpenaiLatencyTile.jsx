import React, { useMemo } from 'react';
import { ChartPalette } from '@cloudflare/kumo';
import TileChart from './TileChart.jsx';
import HalfTile from './HalfTile.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { widthTier } from './constants.js';
import { fmtMs, seriesSummary, shortDay } from './utils.js';

export default function OpenaiLatencyTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const daily = useMemo(() => (Array.isArray(data?.daily) ? data.daily : []), [data]);
  const categories = useMemo(() => daily.map((p) => shortDay(p.day)), [daily]);
  const latValues = useMemo(() => daily.map((p) => p.avgLatency ?? 0), [daily]);
  const ttfbValues = useMemo(() => daily.map((p) => p.avgTtfbMs ?? 0), [daily]);
  const latColor = useMemo(() => ChartPalette.categorical(3, isDarkMode), [isDarkMode]);
  const ttfbColor = useMemo(() => ChartPalette.categorical(4, isDarkMode), [isDarkMode]);
  const latest = daily[daily.length - 1];
  const summary = useMemo(() => seriesSummary([...latValues, ...ttfbValues]), [latValues, ttfbValues]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const showLegend = tier !== 'narrow' && !isHalf;
  const summaryItems = useMemo(() => {
    if (!summary || (tier !== 'wide' && density !== 'rich')) return [];
    return [
      { label: '延迟日均', value: fmtMs(summary.avg) },
      { label: '延迟峰值', value: fmtMs(summary.max) },
    ];
  }, [summary, tier, density]);

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  const halfStat = (
    <div className="flex min-w-0 items-baseline gap-2.5">
      <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtMs(latest?.avgLatency)}</span>
      {tier !== 'narrow' && (
        <span className="text-sm font-medium text-kumo-default/80 tabular-nums">{fmtMs(latest?.avgTtfbMs)}</span>
      )}
    </div>
  );
  const halfSpark = isHalf ? (
    <TileChart
      series={[
        { name: '延迟', color: latColor, data: latValues },
        { name: 'TTFB', color: ttfbColor, data: ttfbValues },
      ]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      yAxisTickFormat={(v) => `${(Number(v) / 1000).toFixed(1)}s`}
      tooltipValueFormat={(v) => fmtMs(v)}
    />
  ) : null;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={halfStat}
          footnote={tier === 'narrow' ? `TTFB ${fmtMs(latest?.avgTtfbMs)}` : null}
          spark={halfSpark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtMs(latest?.avgLatency)}</span>
              {tier !== 'narrow' && <span className="text-base font-medium text-kumo-default/80 tabular-nums">{fmtMs(latest?.avgTtfbMs)}</span>}
              {showLegend && (
                <>
                  <span className="animate-tile-fade-up inline-flex items-center gap-1 text-[10px] text-kumo-subtle">
                    <span className="h-2 w-2 rounded-full" style={{ background: latColor }} />延迟
                  </span>
                  <span className="animate-tile-fade-up inline-flex items-center gap-1 text-[10px] text-kumo-subtle">
                    <span className="h-2 w-2 rounded-full" style={{ background: ttfbColor }} />TTFB
                  </span>
                </>
              )}
              {summaryItems.map((it, i) => (
                <span key={i} className="animate-tile-fade-up shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  {it.label} {it.value}
                </span>
              ))}
            </div>
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[
                { name: '延迟', color: latColor, data: latValues },
                { name: 'TTFB', color: ttfbColor, data: ttfbValues },
              ]}
              categories={categories}
              isDarkMode={isDarkMode}
              density={density}
              yAxisTickFormat={(v) => `${(Number(v) / 1000).toFixed(1)}s`}
              tooltipValueFormat={(v) => fmtMs(v)}
            />
          </div>
        </>
      )}
    </div>
  );
}
