import { forwardRef, useMemo, useRef } from 'react';
import { TimeseriesChart } from '@cloudflare/kumo';
import { createSiteFontEcharts } from '../chartFont.js';

// Kumo 的 TimeseriesChart 用 window mousemove 是否落在图表外来关闭 tooltip；
// 移动端触摸没有后续 mousemove，tooltip 会残留。这里包一层容器，在触摸结束
// （pointerup，仅 touch）时向 window 合成一个位于图表外的 mousemove，
// 复用 Kumo 自带的 closeWhenOutsideChart 逻辑关闭 tooltip，不改第三方包。
const SiteFontTimeseriesChart = forwardRef(function SiteFontTimeseriesChart({ echarts, ...props }, ref) {
  const siteFontEcharts = useMemo(() => createSiteFontEcharts(echarts), [echarts]);
  const containerRef = useRef(null);

  const handlePointerUp = (event) => {
    if (event.pointerType && event.pointerType !== 'touch') return;
    const el = containerRef.current;
    if (!el || typeof window === 'undefined') return;
    const rect = el.getBoundingClientRect();
    window.dispatchEvent(
      new MouseEvent('mousemove', {
        clientX: rect.right + 2,
        clientY: rect.bottom + 2,
        bubbles: true,
      })
    );
  };

  return (
    <div ref={containerRef} className="w-full" onPointerUp={handlePointerUp}>
      <TimeseriesChart ref={ref} {...props} echarts={siteFontEcharts} />
    </div>
  );
});

export default SiteFontTimeseriesChart;
