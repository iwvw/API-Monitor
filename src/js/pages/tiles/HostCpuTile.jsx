// 主机性能：CPU/内存/磁盘 三进度条（hover 显示具体值）。半高 = 三行进度条（无右图）；
// 非半高 = CPU 实时数值 + 三进度条 + CPU 采样图（底部出血）。
import React, { useMemo } from 'react';
import { ChartPalette } from '@cloudflare/kumo';
import TileChart from './TileChart.jsx';
import TileSkeleton from './TileSkeleton.jsx';
import CompactMeter from './CompactMeter.jsx';
import { widthTier } from './constants.js';
import { fmtBytes } from './utils.js';

export default function HostCpuTile({ data, isDarkMode, density = 'full', w = 1 }) {
  const samples = useMemo(() => (Array.isArray(data?.samples) ? data.samples : []), [data]);
  const memSamples = useMemo(() => (Array.isArray(data?.memSamples) ? data.memSamples : []), [data]);
  const diskSamples = useMemo(() => (Array.isArray(data?.diskSamples) ? data.diskSamples : []), [data]);
  const gpuSamples = useMemo(() => (Array.isArray(data?.gpuSamples) ? data.gpuSamples : []), [data]);
  const current = data?.cpu?.usage;
  const isHalf = density === 'half';
  const tier = widthTier(w);
  const mem = data?.memory;
  const disk = data?.disk;
  const load1 = data?.cpu?.loadAverage?.[0];
  const cores = data?.cpu?.cores;
  // 1 分钟滚动窗口多折线：CPU / 内存 / 磁盘 / GPU（缺数据序列自动剔除）
  const perfSeries = useMemo(() => [
    { name: 'CPU', color: ChartPalette.categorical(5, isDarkMode), data: samples },
    { name: '内存', color: ChartPalette.categorical(1, isDarkMode), data: memSamples },
    { name: '磁盘', color: ChartPalette.categorical(2, isDarkMode), data: diskSamples },
    ...(gpuSamples.length ? [{ name: 'GPU', color: ChartPalette.categorical(3, isDarkMode), data: gpuSamples }] : []),
  ].filter((s) => s.data.length > 0), [samples, memSamples, diskSamples, gpuSamples, isDarkMode]);

  if (current == null) return <TileSkeleton variant={isHalf || tier === 'narrow' ? 'chart' : 'bars'} />;

  const bars = [
    {
      label: 'CPU',
      usage: Number(current) || 0,
      detail: [cores ? `${cores} 核` : '', load1 != null ? `负载 ${Number(load1).toFixed(2)}` : ''].filter(Boolean).join(' · ') || '—',
      tone: 'success',
    },
    {
      label: '内存',
      usage: Number(mem?.usage) || 0,
      detail: mem ? `${fmtBytes(mem.used)} / ${fmtBytes(mem.total)}` : '—',
      tone: 'info',
    },
    {
      label: '磁盘',
      usage: Number(disk?.usage) || 0,
      detail: disk ? `${fmtBytes(disk.used)} / ${fmtBytes(disk.total)}` : '—',
      tone: 'brand',
    },
  ];
  const barList = (
    <div className="flex min-w-0 flex-col gap-1">
      {bars.map((b) => (
        <CompactMeter
          key={b.label}
          label={b.label}
          usage={b.usage}
          tone={b.tone}
          detail={b.detail}
          title={b.detail}
          hideValue={isHalf && tier === 'narrow'}
        />
      ))}
    </div>
  );

  // 半高：全部用 1 分钟滚动多折线图（1×1 数值 + 压缩图；2×1/4×1 左数据右图）
  if (isHalf) {
    if (tier === 'narrow') {
      return (
        <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pb-1.5 pt-1">
          <div className="animate-tile-fade-up flex shrink-0 items-baseline gap-1.5">
            <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
            <span className="min-w-0 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</span>
          </div>
          <div className="animate-tile-fade-up -mx-4 -mb-1.5 mt-0.5 min-h-0 flex-1 overflow-hidden">
            <TileChart
              series={perfSeries}
              showSymbol={false}
              categories={samples.map(() => '')}
              isDarkMode={isDarkMode}
              density="compact"
              yAxisTickFormat={(v) => `${v}%`}
              tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
            />
          </div>
        </div>
      );
    }
    return (
      <div className="flex h-full min-h-0 items-stretch gap-3 px-4 pb-1.5 pt-1">
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <div className="animate-tile-fade-up shrink-0">
            <div className="flex items-baseline gap-1.5">
              <span className="text-xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
              <span className="text-[10px] text-kumo-subtle">CPU</span>
            </div>
            <div className="mt-0.5 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</div>
          </div>
        </div>
        <div className="animate-tile-fade-up -mr-4 -mb-1 w-1/2 shrink-0 overflow-hidden">
          <TileChart
            series={perfSeries}
            showSymbol={false}
            categories={samples.map(() => '')}
            isDarkMode={isDarkMode}
            density="compact"
            yAxisTickFormat={(v) => `${v}%`}
            tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
          />
        </div>
      </div>
    );
  }

  // 非半高：1×2（compact 窄卡）也用滚动多折线图（数值 + 缩略图，不含进度条）；
  // 2×2 及以上保留「顶部 CPU 数值 + 三进度条 + CPU 采样图」
  if (tier === 'narrow') {
    return (
      <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pt-1">
        <div className="animate-tile-fade-up flex shrink-0 items-baseline gap-1.5">
          <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
          <span className="min-w-0 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</span>
        </div>
        <div className="animate-tile-fade-up -mx-4 mt-1 min-h-0 flex-1 overflow-hidden">
          <TileChart
            series={perfSeries}
            showSymbol={false}
            categories={samples.map(() => '')}
            isDarkMode={isDarkMode}
            density="compact"
            yAxisTickFormat={(v) => `${v}%`}
            tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
          />
        </div>
      </div>
    );
  }
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden px-4 pt-1">
      <div className="flex shrink-0 items-baseline gap-1.5">
        <span className="text-2xl font-semibold leading-tight text-kumo-default tabular-nums">{current.toFixed(1)}%</span>
        <span className="min-w-0 truncate text-[10px] text-kumo-subtle">{data?.hostname || '未连接主机'}</span>
      </div>
      <div className="mt-1 shrink-0">{barList}</div>
      <div className="animate-tile-fade-up -mx-4 mt-1 min-h-0 flex-1 overflow-hidden">
        <TileChart
          series={perfSeries}
          showSymbol={false}
          categories={samples.map(() => '')}
          isDarkMode={isDarkMode}
          density={density}
          yMin={0}
          yMax={100}
          yInterval={25}
          yAxisTickFormat={(v) => `${v}%`}
          tooltipValueFormat={(v) => `${Number(v).toFixed(1)}%`}
        />
      </div>
    </div>
  );
}
