import { ChartPalette, LayerCard, Popover, SkeletonLine, Tabs } from '@cloudflare/kumo';
import { AppCard, sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { ArrowDown, ArrowUp } from '@phosphor-icons/react';
import {
  Activity,
  AlertTriangle,
  Brain,
  Clock,
  Cpu,
  TrendingUp,
} from '../../components/Icons.jsx';
import { formatCompact, formatCostAmount, formatTokensZh } from './utils.js';
import { TrendBarChart } from './TrendBarChart.jsx';
import { ModelTrendChart } from './ModelTrendChart.jsx';

export function AnalyticsTab({ analytics, isDarkMode, trendSeries, byModelTrend }) {
  const {
    analyticsCharts,
    analyticsDays,
    analyticsLoading,
    analyticsMinutes,
    analyticsSummary,
    requestTrendMode, setRequestTrendMode,
    tokenTrendMode, setTokenTrendMode,
    latencyTrendMode, setLatencyTrendMode,
    errorTrendMode, setErrorTrendMode,
    modelTrendMode, setModelTrendMode,
    modelTrendMetric, setModelTrendMetric,
    modelTrendCache, setModelTrendCache,
    tokenShareMode, setTokenShareMode,
    countShareMode, setCountShareMode,
  } = analytics;

  return (
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-sm:gap-3 cq-xl:grid-cols-6">
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">网关请求</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Activity className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                {analyticsLoading ? (
                  <SkeletonLine className="h-6 w-20" />
                ) : (
                  <div className="flex min-w-0 items-baseline gap-1">
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看成功/失败详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-2xl font-semibold leading-none text-kumo-strong">
                            {String(analyticsSummary.totalRequests)}
                            <span className="ml-0.5 text-sm font-medium text-kumo-subtle">次</span>
                          </span>
                        }
                      />
                      <Popover.Content className="w-56 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          网关请求详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">成功</span>
                            <span className="font-mono">
                              {String(Math.max(0, analyticsSummary.totalRequests - (analyticsSummary.errorCount || 0)))}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">失败</span>
                            <span className="font-mono">
                              {String(analyticsSummary.errorCount || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 border-t border-kumo-line text-kumo-strong">
                            <span className="text-kumo-subtle">错误率</span>
                            <span className="font-mono">
                              {((analyticsSummary.errorRate || 0) * 100).toFixed(2)}%
                            </span>
                          </div>
                        </div>
                      </Popover.Content>
                    </Popover>
                  </div>
                )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">最近 {analyticsMinutes ? `${analyticsMinutes} 分钟` : `${analyticsDays} 天`}</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均端到端延迟</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-warning">
                    <Clock className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看首字/总耗时详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-2xl font-semibold leading-none text-kumo-warning">
                            {analyticsSummary.avgLatency ? (analyticsSummary.avgLatency / 1000).toFixed(2) : '0.00'}
                            <span className="ml-0.5 text-sm font-medium text-kumo-subtle">s</span>
                          </span>
                        }
                      />
                      <Popover.Content className="w-64 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          延迟详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">平均首字延迟</span>
                            <span className="font-mono">
                              {analyticsSummary.avgTtfbMs > 0
                                ? `${(analyticsSummary.avgTtfbMs / 1000).toFixed(2)}s`
                                : '—'}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">平均端到端耗时</span>
                            <span className="font-mono">
                              {(analyticsSummary.avgLatency / 1000).toFixed(2)}s
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 border-t border-kumo-line text-kumo-strong">
                            <span className="text-kumo-subtle">首字后耗时（输出+传输）</span>
                            <span className="font-mono">
                              {analyticsSummary.avgTtfbMs > 0 && analyticsSummary.avgLatency > 0
                                ? `${(Math.max(0, analyticsSummary.avgLatency - analyticsSummary.avgTtfbMs) / 1000).toFixed(2)}s`
                                : '—'}
                            </span>
                          </div>
                        </div>
                      </Popover.Content>
                    </Popover>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">最近 {analyticsMinutes ? `${analyticsMinutes} 分钟` : `${analyticsDays} 天`}</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">词元用量</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Brain className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                {analyticsLoading ? (
                  <SkeletonLine className="h-6 w-24" />
                ) : (
                  <div className="flex min-w-0 items-baseline gap-1">
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看输入/输出详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-2xl font-semibold leading-none text-brand">
                            {formatTokensZh(analyticsSummary.totalTokens)}
                          </span>
                        }
                      />
                      <Popover.Content className="w-80 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          词元用量详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">输入（含缓存）</span>
                            <span className="font-mono">
                              {formatTokensZh(analyticsSummary.totalPromptTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle" title="非缓存输入 = 输入（含缓存）− 缓存命中的词元">缓存命中</span>
                            <span className="font-mono">
                              {formatTokensZh(analyticsSummary.totalCachedTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle" title="非缓存输入 = 输入（含缓存）− 缓存命中的词元">未缓存输入</span>
                            <span className="font-mono">
                              {formatTokensZh(
                                Math.max(0, (analyticsSummary.totalPromptTokens || 0) - (analyticsSummary.totalCachedTokens || 0))
                              )}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">输出</span>
                            <span className="font-mono">
                              {formatTokensZh(analyticsSummary.totalCompletionTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 text-kumo-strong border-t border-kumo-line">
                            <span className="text-kumo-subtle">合计</span>
                            <span className="font-mono">
                              {formatTokensZh(analyticsSummary.totalTokens || 0)}
                            </span>
                          </div>
                          {(analyticsSummary.costs?.length > 0) && (
                            <div className="flex items-center justify-between gap-3 pt-1.5 text-kumo-strong border-t border-kumo-line">
                              <span className="text-kumo-subtle">预估费用</span>
                              <span className="flex flex-col items-end gap-0.5 font-mono text-kumo-success">
                                {analyticsSummary.costs.map(cs => (
                                  <span key={cs.currency}>
                                    {formatCostAmount(cs.amount, cs.currency)}
                                  </span>
                                ))}
                              </span>
                            </div>
                          )}
                          {analyticsSummary.costByEndpoint?.length > 0 && (
                            <>
                              <div className="border-t border-kumo-line pt-1.5">
                                <span className="text-kumo-subtle">费用构成（按端点 / Key）</span>
                              </div>
                              {analyticsSummary.costByEndpoint.map(es => (
                                <div key={es.endpointId || es.endpointName} className="flex flex-col gap-1">
                                  <div className="flex items-center justify-between gap-3">
                                    <span className="min-w-0 truncate" title={es.endpointName}>
                                      {es.endpointName || '—'}
                                    </span>
                                    <span className="shrink-0 font-mono text-kumo-success">
                                      {formatCostAmount(es.cost, es.currency)}
                                    </span>
                                  </div>
                                  {es.keys?.length > 0 && (
                                    <div className="flex flex-col gap-0.5 pl-3">
                                      {es.keys.map(ks => (
                                        <div
                                          key={`${es.endpointId}-${ks.keyName}`}
                                          className="flex items-center justify-between gap-3"
                                        >
                                          <span className="min-w-0 truncate text-[11px] text-kumo-subtle" title={ks.keyName}>
                                            {ks.keyName || '未识别密钥'}
                                          </span>
                                          <span className="shrink-0 font-mono text-[11px] text-kumo-success">
                                            {formatCostAmount(ks.cost, ks.currency)}
                                          </span>
                                        </div>
                                      ))}
                                    </div>
                                  )}
                                </div>
                              ))}
                            </>
                          )}
                        </div>
                      </Popover.Content>
                    </Popover>
                  </div>
                )}
                </div>
                <span
                  className="hidden truncate font-mono text-[11px] text-kumo-subtle cq-xl:block"
                  title="非缓存输入 = 输入（含缓存）− 缓存命中的词元"
                >
                  <ArrowDown
                    className="inline h-3 w-3 align-[-1px]"
                    aria-hidden="true"
                  />{' '}
                  {formatTokensZh(Math.max(0, analyticsSummary.totalPromptTokens - analyticsSummary.totalCachedTokens))}（
                  {analyticsSummary.totalPromptTokens > 0
                    ? `${(
                        (Math.max(0, analyticsSummary.totalPromptTokens - analyticsSummary.totalCachedTokens) /
                          analyticsSummary.totalPromptTokens) *
                        100
                      ).toFixed(1)}%`
                    : '0.0%'}
                  ） · <ArrowUp className="inline h-3 w-3 align-[-1px]" aria-hidden="true" />{' '}
                  {formatTokensZh(analyticsSummary.totalCompletionTokens || 0)}
                </span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均 TPM</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <Cpu className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-baseline">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-2xl font-semibold leading-none text-brand">
                        {((analyticsSummary.totalTokens || 0) / Math.max(1, analyticsMinutes || analyticsDays * 24 * 60)).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">/min</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">每分钟词元</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均 RPM</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-brand">
                    <TrendingUp className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-baseline">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-2xl font-semibold leading-none text-brand">
                        {((analyticsSummary.totalRequests || 0) / Math.max(1, analyticsMinutes || analyticsDays * 24 * 60)).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">/min</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">每分钟请求</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">上游错误率</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-danger">
                    <AlertTriangle className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex h-8 min-w-0 items-center">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看各渠道错误率"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-2xl font-semibold leading-none text-kumo-danger">
                            {(analyticsSummary.errorRate * 100).toFixed(1)}
                            <span className="ml-0.5 text-sm font-medium text-kumo-subtle">%</span>
                          </span>
                        }
                      />
                      <Popover.Content className="w-72 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          各渠道错误率
                        </Popover.Title>
                        {analyticsSummary.endpointErrorRates?.length ? (
                          <div className="mt-2 flex max-h-60 flex-col gap-1.5 overflow-y-auto pr-1 text-xs text-kumo-strong">
                            {analyticsSummary.endpointErrorRates.map((item) => (
                              <div
                                key={item.endpointId || item.endpointName}
                                className="flex items-center justify-between gap-3"
                              >
                                <span className="min-w-0 truncate text-kumo-subtle" title={item.endpointName}>
                                  {item.endpointName}
                                </span>
                                <span className="flex shrink-0 items-baseline gap-1.5 font-mono">
                                  <span className={item.errorRate > 0 ? 'text-kumo-danger' : 'text-kumo-strong'}>
                                    {((item.errorRate || 0) * 100).toFixed(1)}%
                                  </span>
                                  <span className="text-[10px] text-kumo-subtle">
                                    {item.errors}/{item.requests}
                                  </span>
                                </span>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div className="mt-2 text-xs text-kumo-subtle">暂无渠道数据</div>
                        )}
                      </Popover.Content>
                    </Popover>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">请求失败占比</span>
              </AppCard>
            </div>

            <div className="grid items-start gap-3 cq-xl:grid-cols-2">
            {[
              {
                key: 'requests',
                icon: <Activity className="h-4 w-4 text-brand" />,
                title: '请求量趋势',
                series: trendSeries.requests,
              },
              {
                key: 'tokens',
                icon: <Brain className="h-4 w-4 text-brand" />,
                title: '词元趋势',
                series: trendSeries.tokens,
              },
              {
                key: 'latency',
                icon: <Clock className="h-4 w-4 text-kumo-warning" />,
                title: '平均延迟趋势',
                series: trendSeries.latency,
              },
              {
                key: 'errors',
                icon: <AlertTriangle className="h-4 w-4 text-kumo-danger" />,
                title: '错误率趋势',
                series: trendSeries.errorRate,
              },
            ].map(card => {
              const series =
                card.key === 'requests'
                  ? requestTrendMode === 'success'
                    ? trendSeries.requestsSuccess
                    : requestTrendMode === 'failed'
                      ? trendSeries.requestsFailed
                      : trendSeries.requests
                  : card.key === 'tokens'
                    ? tokenTrendMode === 'uncached'
                      ? trendSeries.tokensUncached
                      : trendSeries.tokens
                    : card.key === 'latency'
                      ? latencyTrendMode === 'ttfb'
                        ? trendSeries.latencyTtfb
                        : trendSeries.latency
                      : card.key === 'errors'
                        ? errorTrendMode === 'count'
                          ? trendSeries.errorCount
                          : trendSeries.errorRate
                        : card.series;
              const toggleTabs =
                card.key === 'requests' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={requestTrendMode}
                    onValueChange={setRequestTrendMode}
                    tabs={[
                      { value: 'all', label: '全部' },
                      { value: 'success', label: '成功' },
                      { value: 'failed', label: '失败' },
                    ]}
                  />
                ) : card.key === 'tokens' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={tokenTrendMode}
                    onValueChange={setTokenTrendMode}
                    tabs={[
                      { value: 'all', label: '全部' },
                      { value: 'uncached', label: '未缓存' },
                    ]}
                  />
                ) : card.key === 'latency' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={latencyTrendMode}
                    onValueChange={setLatencyTrendMode}
                    tabs={[
                      { value: 'total', label: '总耗时' },
                      { value: 'ttfb', label: '首字' },
                    ]}
                  />
                ) : card.key === 'errors' ? (
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={errorTrendMode}
                    onValueChange={setErrorTrendMode}
                    tabs={[
                      { value: 'rate', label: '错误率' },
                      { value: 'count', label: '错误数' },
                    ]}
                  />
                ) : null;
              return (
              <LayerCard key={card.key} className="min-w-0 p-0">
                <LayerCard.Secondary className={sectionCardHeaderClass}>
                  {toggleTabs ? (
                    <div className="flex w-full items-center justify-between gap-2">
                      <span>{card.title}</span>
                      {toggleTabs}
                    </div>
                  ) : (
                    card.title
                  )}
                </LayerCard.Secondary>
                <LayerCard.Primary className="flex min-h-0 flex-col gap-2 !p-3">
                  <div className="min-h-0 w-full" style={{ height: 168 }}>
                    {series.labels.length === 0 && !analyticsLoading ? (
                      <div className="flex h-full items-center justify-center text-sm text-kumo-subtle">
                        暂无数据
                      </div>
                    ) : (
                      <TrendBarChart
                        labels={series.labels}
                        values={series.values}
                        color={series.color}
                        isDarkMode={isDarkMode}
                        loading={analyticsLoading}
                        formatValue={series.formatValue}
                        formatAxis={series.formatAxis}
                      />
                    )}
                  </div>
                </LayerCard.Primary>
              </LayerCard>
              );
            })}
          </div>

            <div className="grid">
            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex w-full items-center justify-between gap-2">
                  <span>模型调用趋势</span>
                  <div className="flex flex-wrap items-center justify-end gap-2">
                    {modelTrendMetric === 'tokens' && (
                      <Tabs
                        variant="segmented"
                        size="sm"
                        value={modelTrendCache}
                        onValueChange={setModelTrendCache}
                        tabs={[
                          { value: 'uncached', label: '未缓存' },
                          { value: 'all', label: '全部' },
                        ]}
                      />
                    )}
                    <Tabs
                      variant="segmented"
                      size="sm"
                      value={modelTrendMetric}
                      onValueChange={setModelTrendMetric}
                      tabs={[
                        { value: 'count', label: '调用量' },
                        { value: 'tokens', label: '词元' },
                      ]}
                    />
                    <Tabs
                      variant="segmented"
                      size="sm"
                      value={modelTrendMode}
                      onValueChange={setModelTrendMode}
                      tabs={[
                        { value: 'model', label: '按模型' },
                        { value: 'endpoint', label: '按站点' },
                      ]}
                    />
                  </div>
                </div>
              </LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
              {(!Array.isArray(byModelTrend.labels) || byModelTrend.labels.length === 0) && !analyticsLoading ? (
                <div className="flex h-[240px] items-center justify-center text-sm text-kumo-subtle">
                  暂无数据
                </div>
              ) : (
                <ModelTrendChart
                  labels={byModelTrend.labels}
                  series={modelTrendMode === 'endpoint' ? byModelTrend.endpoints : byModelTrend.models}
                  metric={
                    modelTrendMetric === 'tokens'
                      ? modelTrendCache === 'uncached'
                        ? 'tokensUncached'
                        : 'tokens'
                      : 'count'
                  }
                  isDarkMode={isDarkMode}
                  loading={analyticsLoading}
                />
              )}
              </LayerCard.Primary>
            </LayerCard>
          </div>

            <div className="grid items-start gap-3 cq-xl:grid-cols-2">
            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex w-full items-center justify-between gap-2">
                  <span>模型词元分布</span>
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={tokenShareMode}
                    onValueChange={setTokenShareMode}
                    tabs={[
                      { value: 'model', label: '按模型' },
                      { value: 'endpoint', label: '按站点' },
                    ]}
                  />
                </div>
              </LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
                <div className="min-h-0">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="w-full h-4" />
                    <SkeletonLine className="w-full h-4" />
                  </div>
                ) : (
                  (() => {
                    const shareData =
                      tokenShareMode === 'endpoint'
                        ? analyticsCharts.endpoints || []
                        : analyticsCharts.models || [];
                    if (shareData.length === 0) {
                      return <div className="py-16 text-center text-sm text-kumo-subtle">暂无数据</div>;
                    }
                    const totalTokens =
                      shareData.reduce(
                        (sum, model) => sum + (Number(model.tokens) || 0),
                        0
                      ) || 1;
                    const sorted = [...shareData]
                      .sort((a, b) => (Number(b.tokens) || 0) - (Number(a.tokens) || 0))
                      .slice(0, 20);
                    return (
                      <div className="flex flex-col gap-1.5">
                        {sorted.map((model, index) => {
                          const tokens = Number(model.tokens) || 0;
                          const percent = (tokens / totalTokens) * 100;
                          return (
                            <div
                              key={`${model.model}:${index}`}
                              className="rank-row-enter flex items-center gap-2 text-xs"
                              style={{ animationDelay: `${Math.min(index * 40, 400)}ms` }}
                            >
                              <span
                                className="w-40 shrink-0 truncate font-medium text-kumo-strong"
                                title={model.model}
                              >
                                {model.model}
                              </span>
                              <div className="h-3 min-w-0 flex-1 overflow-hidden rounded-full bg-kumo-recessed">
                                <div
                                  className="h-full rounded-full transition-[width] duration-500 ease-out"
                                  style={{
                                    width: `${Math.max(2, Math.min(100, percent))}%`,
                                    background: ChartPalette.categorical(index, isDarkMode),
                                  }}
                                />
                              </div>
                              <span className="w-16 shrink-0 text-right font-mono text-[11px] text-kumo-subtle">
                                {formatTokensZh(tokens)}
                              </span>
                              <span className="w-11 shrink-0 text-right font-mono text-[10px] text-kumo-subtle">
                                {percent.toFixed(1)}%
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    );
                  })()
                )}
              </div>
              </LayerCard.Primary>
            </LayerCard>

            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary className={sectionCardHeaderClass}>
                <div className="flex w-full items-center justify-between gap-2">
                  <span>模型调用次数</span>
                  <Tabs
                    variant="segmented"
                    size="sm"
                    value={countShareMode}
                    onValueChange={setCountShareMode}
                    tabs={[
                      { value: 'model', label: '按模型' },
                      { value: 'endpoint', label: '按站点' },
                    ]}
                  />
                </div>
              </LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
                <div className="min-h-0">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="h-4 w-full" />
                    <SkeletonLine className="h-4 w-full" />
                  </div>
                ) : (
                  (() => {
                    const shareData =
                      countShareMode === 'endpoint'
                        ? analyticsCharts.endpoints || []
                        : analyticsCharts.models || [];
                    if (shareData.length === 0) {
                      return <div className="py-16 text-center text-sm text-kumo-subtle">暂无数据</div>;
                    }
                    const totalCount =
                      shareData.reduce(
                        (sum, model) => sum + (Number(model.count) || 0),
                        0
                      ) || 1;
                    const sorted = [...shareData]
                      .sort((a, b) => (Number(b.count) || 0) - (Number(a.count) || 0))
                      .slice(0, 20);
                    return (
                      <div className="flex flex-col gap-1.5">
                        {sorted.map((model, index) => {
                          const count = Number(model.count) || 0;
                          const percent = (count / totalCount) * 100;
                          return (
                            <div
                              key={`${model.model}:${index}`}
                              className="rank-row-enter flex items-center gap-2 text-xs"
                              style={{ animationDelay: `${Math.min(index * 40, 400)}ms` }}
                            >
                              <span
                                className="w-40 shrink-0 truncate font-medium text-kumo-strong"
                                title={model.model}
                              >
                                {model.model}
                              </span>
                              <div className="h-3 min-w-0 flex-1 overflow-hidden rounded-full bg-kumo-recessed">
                                <div
                                  className="h-full rounded-full transition-[width] duration-500 ease-out"
                                  style={{
                                    width: `${Math.max(2, Math.min(100, percent))}%`,
                                    background: ChartPalette.categorical(index, isDarkMode),
                                  }}
                                />
                              </div>
                              <span className="w-14 shrink-0 text-right font-mono text-[11px] text-kumo-subtle">
                                {formatCompact(count, 0)}
                              </span>
                              <span className="w-11 shrink-0 text-right font-mono text-[10px] text-kumo-subtle">
                                {percent.toFixed(1)}%
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    );
                  })()
                )}
              </div>
              </LayerCard.Primary>
            </LayerCard>
          </div>
        </div>
  );
}
