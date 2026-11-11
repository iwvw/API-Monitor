import { memo, useEffect, useMemo, useRef } from 'react';
import { Chart } from '@cloudflare/kumo';
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
import { kumoHex } from './utils.js';

echarts.use([
  BarChart,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
  AriaComponent,
  CanvasRenderer,
]);
const siteFontEcharts = createSiteFontEcharts(echarts);

  // 时间序列（小时/天/周粒度）：为每根柱提供独立可对齐的类目轴。
// 后端每个桶返回 day(bucket label) + count/tokens/avgLatency/errors，仅用于柱状展示。
export const TrendBarChart = memo(function TrendBarChart({
  labels,
  values,
  color,
  isDarkMode,
  loading = false,
  formatValue = value => (Number.isFinite(Number(value)) ? String(Number(value)) : String(value)),
  formatAxis = formatValue,
}) {
  const chartRef = useRef(null);
  const options = useMemo(() => {
    if (!labels || labels.length === 0) return null;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    return {
      grid: { left: 8, right: 12, top: 10, bottom: 0, containLabel: true },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        appendTo: 'body',
        backgroundColor: kumoHex('--color-kumo-base'),
        textStyle: { color: axisColor, fontSize: 11 },
        valueFormatter: formatValue,
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
        axisLabel: { color: axisColor, fontSize: 10, formatter: formatAxis },
      },
      series: [
        {
          type: 'bar',
          data: values,
          barMaxWidth: 26,
          itemStyle: { color, borderRadius: [2, 2, 0, 0] },
        },
      ],
    };
  }, [labels, values, color, isDarkMode, formatValue, formatAxis]);

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

  const hasData = !!(labels && labels.length > 0);
  if (!hasData && !loading) return null;

  return <Chart ref={chartRef} echarts={siteFontEcharts} isDarkMode={isDarkMode} options={hasData ? options : {}} height={168} />;
});
