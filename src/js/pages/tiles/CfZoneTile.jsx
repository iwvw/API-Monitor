import React, { useMemo } from 'react';
import { ChartPalette } from '@cloudflare/kumo';
import StatValue from './StatValue.jsx';
import TileChart from './TileChart.jsx';
import HalfTile from './HalfTile.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { widthTier } from './constants.js';
import { fmtCompact, fmtCfAxisTime, fmtPercent, parseCfTime, seriesSummary } from './utils.js';

export default function CfZoneTile({ data, loading, isDarkMode, empty, range = '24h', density = 'full', w = 4 }) {
  const points = useMemo(
    () => (Array.isArray(data?.timeseries)
      ? data.timeseries
        .filter(Boolean)
        .map((p) => ({ ts: parseCfTime(p), requests: p.requests ?? 0 }))
        .filter((p) => p.ts != null)
      : []),
    [data],
  );
  const categories = useMemo(() => points.map((p) => fmtCfAxisTime(p.ts, range)), [points, range]);
  const values = useMemo(() => points.map((p) => p.requests), [points]);
  const summary = useMemo(() => seriesSummary(values), [values]);
  const color = useMemo(() => ChartPalette.categorical(6, isDarkMode), [isDarkMode]);

  const isHalf = density === 'half';
  const tier = widthTier(w);
  const showSummary = tier === 'wide' || density === 'rich';
  const halfFootnote = useMemo(() => {
    const parts = [];
    if (data?.cacheHitRate != null) parts.push(`缓存命中 ${fmtPercent(data?.cacheHitRate)}`);
    if (tier === 'wide' && summary) parts.push(`峰值 ${fmtCompact(summary.max)}`);
    if (tier === 'wide' && summary) parts.push(`均值 ${fmtCompact(summary.avg)}`);
    return parts.length ? parts.join(' · ') : null;
  }, [tier, data, summary]);
  const halfSpark = isHalf ? (
    <TileChart
      series={[{ name: '请求量', color, data: values }]}
      categories={categories}
      isDarkMode={isDarkMode}
      density="compact"
      tooltipValueFormat={(v) => `${fmtCompact(v)} 次`}
    />
  ) : null;

  if (empty) return <div className="flex h-full items-center justify-center px-4 text-center text-xs text-kumo-inactive">未配置 Cloudflare 账号</div>;
  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={<StatValue value={fmtCompact(data?.requests)} delta={null} />}
          footnote={halfFootnote}
          spark={halfSpark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="flex shrink-0 flex-wrap items-baseline gap-x-4 gap-y-0.5">
            <StatValue value={fmtCompact(data?.requests)} delta={null} />
            <span className="shrink-0 text-sm font-medium text-kumo-subtle">缓存命中 {fmtPercent(data?.cacheHitRate)}</span>
            {showSummary && summary && (
              <>
                <span className="shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  峰值 {fmtCompact(summary.max)}
                </span>
                <span className="shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  均值 {fmtCompact(summary.avg)}
                </span>
              </>
            )}
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={[{ name: '请求量', color, data: values }]}
              categories={categories}
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
