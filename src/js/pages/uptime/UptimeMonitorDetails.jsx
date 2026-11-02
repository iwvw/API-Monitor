import React, { useMemo } from 'react';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { Button } from '@cloudflare/kumo/components/button';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { DeferredRender } from '../../components/AnimatedCollapse.jsx';
import { ChartCard, ChartWarmupSkeleton } from '../../components/ui/AppPrimitives.jsx';
import SiteFontTimeseriesChart from '../../components/SiteFontTimeseriesChart.jsx';
import { Edit, Pause, Play, Trash, TrendingUp } from '../../components/Icons.jsx';
import SslCertificatePanel from './SslCertificatePanel.jsx';
import { formatLatencyAxis, formatUptimeChartTime, getUptimeChartColor, parseUptimeBeatTime } from './utils.js';

echarts.use([
  LineChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

// ==================== UptimeMonitorDetails 子组件 ====================
// 使用独立的子组件隔离 Kumo TimeseriesChart，在折叠/销毁时由组件自身清理 ECharts 实例
function UptimeMonitorDetails({
  monitor,
  heartbeats = [],
  loading = false,
  uptime24h,
  uptime30d,
  isDarkMode,
  onPauseResume,
  onEdit,
  onDelete,
  expanded = true,
}) {
  const { isArmed, confirmPress } = useConfirmPress();
  const chartData = useMemo(() => {
    return [{
      name: '响应时间',
      color: getUptimeChartColor(isDarkMode),
      data: [...heartbeats]
        .slice(0, 60)
        .reverse()
        .map((beat) => [beat.timestamp ?? parseUptimeBeatTime(beat.time), Number(beat.ping) || 0])
        .filter(([timestamp]) => Number.isFinite(timestamp)),
    }];
  }, [heartbeats, isDarkMode]);

  return (
    <div className="space-y-3 border-t border-kumo-interact/80 bg-kumo-recessed/40 p-3">
      {/* 头部操作栏 */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h5 className="flex items-center gap-1.5 text-xs font-semibold text-kumo-strong">
          <TrendingUp className="w-3.5 h-3.5" />
          监控详情
        </h5>
        <div className="flex items-center gap-1.5">
          <Button size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onPauseResume(monitor);
            }}
            icon={monitor.active ? <Pause className="w-3 h-3" /> : <Play className="w-3 h-3" />}
          >
            {monitor.active ? '暂停' : '启用'}
          </Button>
          <Button size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onEdit(monitor);
            }}
            icon={<Edit className="w-3 h-3" />}
          >
            编辑
          </Button>
          <Button size="sm"
            variant={isArmed(`monitor:${monitor.id}`) ? 'destructive' : 'secondary-destructive'}
            onClick={(e) => {
              e.stopPropagation();
              onDelete(monitor.id);
            }}
            icon={<Trash className="w-3 h-3" />}
          >
            删除
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 cq-lg:grid-cols-[minmax(0,1fr)_15rem]">
        {/* 图表主栏 (Span 3) */}
        <ChartCard className="relative h-36 !border-kumo-interact/90 !bg-kumo-base">
          {(tooltipBoundary) => (
            <DeferredRender open={expanded} fallback={<ChartWarmupSkeleton height={120} />}>
              <SiteFontTimeseriesChart
                echarts={echarts}
                data={chartData}
                height={120}
                yAxisName="ms"
                loading={loading}
                tooltipBoundary={tooltipBoundary ?? undefined}
                xAxisTickCount={3}
                yAxisTickCount={3}
                isDarkMode={isDarkMode}
                yAxisTickFormat={formatLatencyAxis}
                tooltipValueFormat={(value) => `${Math.round(value)} ms`}
                xAxisTickFormat={formatUptimeChartTime}
                tooltipMode="single"
                gradient
                ariaDescription="Uptime 监测响应时间历史"
              />
            </DeferredRender>
          )}
        </ChartCard>

        {/* 右侧可用率统计指标 */}
        <div className="grid grid-cols-2 gap-2 cq-lg:grid-cols-1">
          <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
            <span className="text-[10px] text-kumo-subtle select-none">24小时可用率</span>
            <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{uptime24h}%</span>
          </div>
          <div className="rounded-md border border-kumo-interact/85 bg-kumo-base p-2">
            <span className="text-[10px] text-kumo-subtle select-none">30天可用率</span>
            <span className="mt-1 block text-base font-semibold tabular-nums text-kumo-strong">{uptime30d}%</span>
          </div>
        </div>
      </div>

      {/* SSL 证书信息面板 */}
      {monitor.url && monitor.url.startsWith('https://') && (
        <SslCertificatePanel monitorId={monitor.id} />
      )}

    </div>
  );
}

export default UptimeMonitorDetails;
