import { Button } from '@cloudflare/kumo/components/button';
import { ChartBoundaryBox, ChartWarmupSkeleton } from '../../components/ui/AppPrimitives.jsx';
import SiteFontTimeseriesChart from '../../components/SiteFontTimeseriesChart.jsx';
import { RefreshCw } from '../../components/Icons.jsx';
import { SERVER_NETWORK_QUALITY_CHART_UPDATE_BEHAVIOR } from './constants.js';
import { formatLatencyAxis, formatLatencyValue, formatNetworkQualityChartTime } from './utils.js';
import { NetworkQualitySummaryStrip } from './NetworkQualitySummaryStrip.jsx';

export function NetworkQualityPanel({
  serverName,
  quality = {},
  series = [],
  hasData = false,
  unsupported = false,
  chartHeight,
  isDarkMode,
  chartEcharts,
  isCompactViewport,
  onCollect,
  className = '',
  compact = false,
}) {
  const summary = Array.isArray(quality.summary) ? quality.summary : [];
  const networkQualityBodyHeight = chartHeight;

  return (
    <ChartBoundaryBox className={`min-w-0 overflow-hidden rounded-lg border border-kumo-line/90 bg-kumo-base p-1.5 shadow-none ${className}`}>
      {(tooltipBoundary) => (
        <div className="flex min-w-0 flex-col gap-1.5">
          <div className={`flex min-w-0 flex-wrap items-center justify-between gap-2 ${compact ? 'min-h-2' : ''}`}>
            <h4 className="flex min-w-0 items-center gap-1.5 text-xs font-semibold text-kumo-strong">
              <span className="h-3 w-1 shrink-0 rounded-full bg-brand"></span>
              <span className="truncate">网络波动 24h</span>
            </h4>
            <div className="flex shrink-0 items-center gap-2">
              {quality.updatedAt && (
                <span className="text-[10px] font-medium text-kumo-subtle">
                  {formatNetworkQualityChartTime(quality.updatedAt)} 更新
                </span>
              )}
              <Button
                shape="square"
                size="sm"
                variant="secondary"
                title="立即采样"
                aria-label="立即采样"
                icon={<RefreshCw className="h-3.5 w-3.5" />}
                className="h-7 w-7 p-0"
                disabled={!!quality.loading}
                onClick={(event) => {
                  event.stopPropagation();
                  onCollect?.();
                }}
              />
            </div>
          </div>

          {quality.error && !unsupported && (
            <div className="rounded-md border border-kumo-warning/30 bg-kumo-warning/10 px-2 py-1.5 text-[11px] font-medium text-kumo-warning">
              {quality.error}
            </div>
          )}

          {quality.loading && !hasData ? (
            <ChartWarmupSkeleton height={networkQualityBodyHeight} bars={3} />
          ) : (
            <>
              {summary.length > 0 && (
                <NetworkQualitySummaryStrip summary={summary} />
              )}

              {hasData ? (
                <SiteFontTimeseriesChart
                  echarts={chartEcharts}
                  data={series}
                  height={chartHeight}
                  isDarkMode={isDarkMode}
                  gradient
                  loading={!!quality.loading}
                  tooltipBoundary={tooltipBoundary ?? undefined}
                  xAxisTickCount={isCompactViewport ? 3 : 6}
                  yAxisTickCount={isCompactViewport ? 3 : 4}
                  xAxisTickFormat={formatNetworkQualityChartTime}
                  yAxisTickFormat={formatLatencyAxis}
                  tooltipValueFormat={formatLatencyValue}
                  optionUpdateBehavior={SERVER_NETWORK_QUALITY_CHART_UPDATE_BEHAVIOR}
                  ariaDescription={`${serverName} 24 小时网络延迟波动`}
                />
              ) : (
                <div
                  className="flex items-center justify-center rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-3 text-center text-xs font-medium text-kumo-subtle"
                  style={{ height: networkQualityBodyHeight }}
                >
                  {unsupported ? '当前 Agent 版本暂不支持，升级后显示 24h 网络波动' : '暂无 24h 网络质量采样'}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </ChartBoundaryBox>
  );
}
