import { memo, useEffect, useMemo, useRef, useState } from 'react';
import { ChartLegend, ChartPalette } from '@cloudflare/kumo';
import * as echarts from 'echarts/core';
import { BarChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { createSiteFontEcharts } from '../../chartFont.js';
import { kumoHex, formatCompact, formatTokensZh } from './utils.js';

echarts.use([
  BarChart,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
  AriaComponent,
  CanvasRenderer,
]);
const siteFontEcharts = createSiteFontEcharts(echarts);

// 全宽「模型 × 时间」折线趋势：类别轴（每桶唯一刻度），稀疏段断线成 Trend；
// 顶部图例按指标值降序，颜色与折线同一份映射，点击隔离/恢复。
// metric: 'count'（调用量）| 'tokens'（全部词元）| 'tokensUncached'（未缓存词元）。
export const ModelTrendChart = memo(function ModelTrendChart({ labels, series, isDarkMode, loading = false, metric = 'count' }) {
  const containerRef = useRef(null);
  const chartRef = useRef(null);
  const [hiddenSeries, setHiddenSeries] = useState({});
  const tokensMetric = metric !== 'count';

  const pickValues = item => {
    if (metric === 'tokens') return item.tokens || [];
    if (metric === 'tokensUncached') return item.tokensUncached || [];
    return item.data || [];
  };

  // 排序必须完全确定：指标值降序、相同值按模型名升序，避免图例顺序
  // 随接口返回顺序（后端 map 迭代随机）漂移；颜色在排序后按固定位次分配，
  // 保证同一模型始终同色。
  const ordered = useMemo(() => {
    const withMeta = (series || []).map(item => {
      const values = pickValues(item).map(value => Number(value) || 0);
      return {
        model: item.model,
        total: values.reduce((sum, value) => sum + value, 0),
        values,
      };
    });
    withMeta.sort((a, b) => b.total - a.total || (a.model < b.model ? -1 : a.model > b.model ? 1 : 0));
    return withMeta.map((item, index) => ({
      ...item,
      color: ChartPalette.categorical(index, isDarkMode),
    }));
  }, [series, isDarkMode, metric]);

  const visibleSeries = useMemo(
    () => ordered.filter(item => !hiddenSeries[item.model]),
    [ordered, hiddenSeries]
  );

  // 图表实例只在挂载时初始化一次；labels/系列的更新全部走 setOption，
  // 否则 30 秒自动刷新带来的新数组引用会触发 dispose+重建，造成闪烁与交互状态丢失。
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const chart = siteFontEcharts.init(el);
    chartRef.current = chart;
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(el);
    return () => {
      observer.disconnect();
      chart.dispose();
      chartRef.current = null;
    };
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart || !labels || labels.length === 0) return;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    chart.setOption(
      {
        grid: { left: 8, right: 12, top: 8, bottom: 0, containLabel: true },
        tooltip: {
          trigger: 'axis',
          traceHigh: true,
          // 挂到 body：LayerCard 自带 overflow-hidden，悬浮框默认渲染在图表
          // 容器内会被卡片裁剪遮挡。
          appendTo: 'body',
          backgroundColor: kumoHex('--color-kumo-base'),
          textStyle: { color: axisColor, fontSize: 11 },
          valueFormatter: tokensMetric
            ? value => `${(Number(value) / 1e6).toFixed(2)}M`
            : undefined,
        },
        xAxis: {
          type: 'category',
          data: labels,
          boundaryGap: false,
          axisLine: { lineStyle: { color: gridColor } },
          axisTick: { show: false },
          axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
        },
        yAxis: {
          type: 'value',
          splitLine: { lineStyle: { color: gridColor } },
          axisLabel: {
            color: axisColor,
            fontSize: 10,
            formatter: tokensMetric
              ? value => `${(Number(value) / 1e6).toFixed(1)}M`
              : value => formatCompact(value, 0),
          },
        },
        series: visibleSeries.map(item => ({
          type: 'line',
          name: item.model,
          data: item.values,
          smooth: true,
          connectNulls: false,
          showSymbol: true,
          symbolSize: 4,
          lineStyle: { width: 2, color: item.color },
          itemStyle: { color: item.color },
          areaStyle: { opacity: 0 },
        })),
      },
      // replaceMerge：隐藏/恢复系列时按 name 精确替换，避免默认 merge 模式下
      //「新 series 数组变短 → 按 index 合并」导致被隐藏的旧系列残留、颜色错位。
      { replaceMerge: ['series'] }
    );
  }, [labels, visibleSeries, isDarkMode, metric, tokensMetric]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    if (loading) {
      chart.showLoading({
        text: '',
        color: kumoHex('--color-brand'),
        maskColor: 'rgba(0,0,0,0)',
      });
    } else {
      chart.hideLoading();
    }
  }, [loading]);

  const handleClick = name => {
    setHiddenSeries(prev => {
      const isIsolated = ordered.every(
        item => (item.model === name ? !prev[item.model] : prev[item.model])
      );
      const next = {};
      for (const item of ordered) {
        next[item.model] = isIsolated ? false : item.model !== name;
      }
      return next;
    });
  };

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex flex-nowrap gap-x-4 overflow-x-auto overscroll-x-contain px-1 pb-1 touch-pan-x scrollbar-thin">
        {ordered.map(item => (
          <ChartLegend.LargeItem
            key={item.model}
            name={item.model}
            color={item.color}
            value={tokensMetric ? formatTokensZh(item.total) : formatCompact(item.total, 0)}
            unit={tokensMetric ? '' : '次'}
            inactive={hiddenSeries[item.model] ?? false}
            onClick={() => handleClick(item.model)}
          />
        ))}
      </div>
<div ref={containerRef} className="h-[240px] w-full" />
    </div>
  );
});
