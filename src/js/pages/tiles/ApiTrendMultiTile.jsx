// API 调用趋势（多系列）：与 OpenAI 延迟卡同款结构——half 走 HalfTile 三档（左数据右缩略图），
// 非 half 走「数值行（总请求 + 词元 + 三系列色点图例）+ 整宽出血图」。
// 已合并原「API 请求趋势」卡（请求次数 = 读取 + 变更）。
import React, { useMemo, useState } from 'react';
import { Button, ChartPalette } from '@cloudflare/kumo';
import TileChart from './TileChart.jsx';
import HalfTile from './HalfTile.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import { widthTier } from './constants.js';
import { fmtBytes, fmtCompact, shortDay } from './utils.js';
import { formatTokensZh } from '../openai/utils.js';

export default function ApiTrendMultiTile({ data, loading, isDarkMode, density = 'full', w = 1 }) {
  const trend = useMemo(() => (Array.isArray(data?.trend) ? data.trend : []), [data]);
  const categories = useMemo(() => trend.map((p) => shortDay(p.bucket)), [trend]);
  const [isolated, setIsolated] = useState(null);
  const configs = useMemo(() => [
    {
      key: 'requests',
      name: '请求次数',
      color: ChartPalette.categorical(0, isDarkMode),
      pick: (p) => (Number(p.audit) || 0) + (Number(p.ops) || 0),
    },
    {
      key: 'tokens',
      name: '词元用量',
      color: ChartPalette.categorical(1, isDarkMode),
      pick: (p) => Number(p.tokens) || 0,
    },
    {
      key: 'traffic',
      name: '订阅流量',
      color: ChartPalette.categorical(2, isDarkMode),
      pick: (p) => Number(p.traffic) || 0,
    },
  ], [isDarkMode]);
  const series = useMemo(() => configs.map((c) => {
    const raw = trend.map(c.pick);
    const max = Math.max(...raw, 1);
    return {
      ...c,
      max,
      raw,
      data: raw.map((v) => ({ value: (v / max) * 100, raw: v })),
      total: raw.reduce((a, b) => a + b, 0),
    };
  }), [configs, trend]);
  const totalRequests = series[0]?.total ?? 0;
  const tokensTotal = series[1]?.total ?? 0;
  const trafficTotal = series[2]?.total ?? 0;
  const isHalf = density === 'half';
  const tier = widthTier(w);
  const visible = isolated ? series.filter((s) => s.key === isolated) : series;
  const showLegend = tier !== 'narrow' && !isHalf;
  const summaryItems = useMemo(() => {
    if (tier !== 'wide' && density !== 'rich') return [];
    const req = series[0];
    if (!req || !trend.length) return [];
    return [
      { label: '请求日均', value: fmtCompact(req.total / trend.length) },
      { label: '请求峰值', value: fmtCompact(Math.max(...req.raw, 0)) },
    ];
  }, [tier, density, series, trend.length]);

  if (loading) return <TileSkeleton variant="chart" />;
  if (!categories.length) return <div className="flex h-full items-center justify-center text-xs text-kumo-inactive">无数据</div>;

  const chart = (chartDensity) => (
    <TileChart
      series={visible.map((s) => ({ name: s.name, color: s.color, data: s.data }))}
      categories={categories}
      isDarkMode={isDarkMode}
      density={chartDensity}
      yMin={0}
      yMax={100}
      yInterval={25}
      tooltipValueFormat={(v) => {
        const raw = v && typeof v === 'object' ? v.raw : v;
        return fmtCompact(Number(raw) || 0);
      }}
    />
  );

  const halfStat = (
    <div className="flex min-w-0 items-baseline gap-2.5">
      <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtCompact(totalRequests)}</span>
      {tier !== 'narrow' && (
        <span className="text-sm font-medium text-kumo-default/80 tabular-nums">{formatTokensZh(tokensTotal)}</span>
      )}
    </div>
  );
  const halfSpark = isHalf ? chart('compact') : null;

  return (
    <div className={`flex h-full min-h-0 flex-col overflow-hidden ${isHalf ? '' : 'px-4 pt-1'}`}>
      {isHalf ? (
        <HalfTile
          tier={tier}
          stat={halfStat}
          footnote={tier === 'narrow' ? `词元 ${formatTokensZh(tokensTotal)} · 流量 ${fmtBytes(trafficTotal)}` : `流量 ${fmtBytes(trafficTotal)}`}
          spark={halfSpark}
          isDarkMode={isDarkMode}
        />
      ) : (
        <>
          <div className="shrink-0">
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{fmtCompact(totalRequests)}</span>
              {tier !== 'narrow' && (
                <span className="text-base font-medium text-kumo-default/80 tabular-nums">{formatTokensZh(tokensTotal)}</span>
              )}
              {showLegend && series.map((s) => (
                <Button
                  key={s.key}
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => setIsolated((prev) => (prev === s.key ? null : s.key))}
                  title={isolated === s.key ? '取消隔离' : `只看 ${s.name}`}
                  className={`inline-flex min-w-0 items-center gap-1 text-[10px] text-kumo-subtle transition-opacity ${
                    isolated !== null && isolated !== s.key ? 'opacity-40 hover:opacity-70' : ''
                  }`}
                >
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: s.color }} />
                  <span className="shrink-0">{s.name}</span>
                  <span className="shrink-0 tabular-nums text-kumo-default/80">
                    {s.key === 'traffic' ? fmtBytes(s.total) : s.key === 'tokens' ? formatTokensZh(s.total) : fmtCompact(s.total)}
                  </span>
                </Button>
              ))}
              {summaryItems.map((it, i) => (
                <span key={i} className="shrink-0 whitespace-nowrap text-[10px] text-kumo-subtle tabular-nums">
                  {it.label} {it.value}
                </span>
              ))}
            </div>
          </div>
          <div className="animate-tile-fade-up -mx-4 mt-1.5 min-h-0 flex-1 overflow-hidden">
            {chart(tier === 'narrow' ? 'compact' : 'full')}
          </div>
        </>
      )}
    </div>
  );
}
