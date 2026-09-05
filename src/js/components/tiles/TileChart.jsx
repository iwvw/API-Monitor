// TileChart —— 图块内嵌图表：echarts 直连 flex-1 容器 + ResizeObserver 自适应，
// 无固定高度，缩放/拖拽时图表实时重算；密度档 compact 隐藏坐标轴成为贴边 sparkline。
// 背景显式透明；x/y 轴线隐藏避免底部/侧边出现白线；setOption 用 notMerge 防档位切换残留。
import React, { useEffect, useRef } from 'react';
import * as echarts from 'echarts/core';
import { BarChart, LineChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { createSiteFontEcharts } from '../../chartFont.js';

echarts.use([
  BarChart,
  LineChart,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

const siteFontEcharts = createSiteFontEcharts(echarts);

// ChartPalette 返回运行时的主题色（hex），这里转 rgba 供 echarts 使用，避免源码硬编码颜色。
function withAlpha(hex, alpha) {
  const h = String(hex || '').replace('#', '');
  if (h.length !== 3 && h.length !== 6) return hex;
  const full = h.length === 3 ? h.split('').map((c) => c + c).join('') : h;
  const int = parseInt(full, 16);
  return `rgba(${(int >> 16) & 255}, ${(int >> 8) & 255}, ${int & 255}, ${alpha})`;
}

export default function TileChart({
  series = [],
  categories = [],
  type = 'line',
  isDarkMode = false,
  density = 'full',
  yAxisTickFormat,
  tooltipValueFormat,
  showSymbol,
}) {
  const boxRef = useRef(null);
  const chartRef = useRef(null);
  const compact = density === 'compact';

  useEffect(() => {
    const el = boxRef.current;
    if (!el) return undefined;
    const chart = siteFontEcharts.init(el, isDarkMode ? 'dark' : undefined);
    chartRef.current = chart;
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(el);
    return () => {
      observer.disconnect();
      chart.dispose();
      chartRef.current = null;
    };
  }, [isDarkMode]);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart || !series.length || !categories.length) return;
    chart.setOption({
      backgroundColor: 'transparent',
      animationDuration: 300,
      animationEasing: 'cubicOut',
      grid: compact
        ? { left: 0, right: 0, top: 2, bottom: 0 }
        : { left: 4, right: 8, top: 10, bottom: 2, containLabel: true },
      tooltip: {
        trigger: 'axis',
        confine: true,
        backgroundColor: isDarkMode ? 'rgba(24, 24, 27, 0.92)' : 'rgba(255, 255, 255, 0.95)',
        borderColor: isDarkMode ? 'rgba(255, 255, 255, 0.14)' : 'rgba(0, 0, 0, 0.08)',
        borderWidth: 1,
        padding: [8, 10],
        textStyle: { fontSize: 11, color: isDarkMode ? 'rgba(228, 228, 231, 0.95)' : 'rgba(24, 24, 27, 0.9)' },
        formatter: (params) => {
          const list = Array.isArray(params) ? params : [params];
          return list
            .map((p) => `${p.marker}${p.seriesName}: ${tooltipValueFormat ? tooltipValueFormat(p.value) : p.value}`)
            .join('<br/>');
        },
      },
      xAxis: {
        type: 'category',
        data: categories,
        boundaryGap: type === 'bar' && !compact,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: compact
          ? { show: false }
          : { color: 'rgba(128, 128, 128, 0.75)', fontSize: 10 },
      },
      yAxis: {
        type: 'value',
        axisLine: { show: false },
        splitLine: compact
          ? { show: false }
          : { lineStyle: { color: 'rgba(128, 128, 128, 0.16)' } },
        axisLabel: compact
          ? { show: false }
          : {
            color: 'rgba(128, 128, 128, 0.75)',
            fontSize: 10,
            formatter: yAxisTickFormat,
          },
      },
      series: series.map((s) => ({
        name: s.name,
        type,
        data: s.data,
        smooth: type === 'line' ? 0.35 : undefined,
        showSymbol: showSymbol !== undefined ? showSymbol : !compact,
        lineStyle: { width: compact ? 2 : 1.5, color: s.color },
        itemStyle: { color: s.color, borderRadius: type === 'bar' ? [2, 2, 0, 0] : 0 },
        barMaxWidth: compact ? 10 : 16,
        areaStyle: type === 'line' ? { color: withAlpha(s.color, 0.14) } : undefined,
        emphasis: { focus: 'series' },
      })),
    }, true);
  }, [series, categories, type, isDarkMode, density, compact, yAxisTickFormat, tooltipValueFormat]);

  return <div ref={boxRef} className="h-full w-full min-h-0" />;
}