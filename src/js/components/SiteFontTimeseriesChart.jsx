import { forwardRef, useMemo } from 'react';
import { TimeseriesChart } from '@cloudflare/kumo';
import { createSiteFontEcharts } from '../chartFont.js';

const SiteFontTimeseriesChart = forwardRef(function SiteFontTimeseriesChart({ echarts, ...props }, ref) {
  const siteFontEcharts = useMemo(() => createSiteFontEcharts(echarts), [echarts]);
  return <TimeseriesChart ref={ref} {...props} echarts={siteFontEcharts} />;
});

export default SiteFontTimeseriesChart;
