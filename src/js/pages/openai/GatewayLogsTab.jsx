import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Loader, LayerCard, Pagination, Popover, Table } from '@cloudflare/kumo';
import { formatDateTime } from '../../modules/utils.js';
import { StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { X } from '../../components/Icons.jsx';
import {
  resultTone,
  ttfbTone,
  statusCodeTone,
  logOutputSpeedText,
  formatCostAmount,
  formatUnitPrice,
  costDetailsFor,
} from './utils.js';
import { FailoverPathBadge } from './FailoverPathBadge.jsx';
import { IpCell } from './IpCell.jsx';

export function GatewayLogsTab({ analytics, endpoints }) {
  const {
    analyticsLogs,
    analyticsLoading,
    analyticsPage, setAnalyticsPage,
    analyticsPageSize, setAnalyticsPageSize,
    analyticsTotal,
    logStatusFilter, setLogStatusFilter,
    logModelFilter, setLogModelFilter,
    logEndpointFilter, setLogEndpointFilter,
    setLogDetail,
    setLogDetailExpanded,
  } = analytics;

  return (
        <div className="flex w-full min-w-0 flex-col gap-3">
          {/* 日志筛选区：状态 / 模型 / 端点，均即时生效 */}
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <Select alignItemWithTrigger
              size="sm"
              className="w-28"
              value={logStatusFilter || undefined}
              onValueChange={value => {
                setLogStatusFilter(value || '');
                setAnalyticsPage(1);
              }}
              placeholder="全部状态"
              aria-label="状态筛选"
            >
              <Select.Option value="">全部状态</Select.Option>
              <Select.Option value="success">成功</Select.Option>
              <Select.Option value="error">失败 (≥400)</Select.Option>
              <Select.Option value="429">限流 429</Select.Option>
              <Select.Option value="5xx">服务端 5xx</Select.Option>
            </Select>
            <Input
              size="sm"
              className="w-52"
              value={logModelFilter}
              aria-label="按模型筛选"
              onChange={e => {
                setLogModelFilter(e.target.value);
                setAnalyticsPage(1);
              }}
              placeholder="按模型筛选，如 deepseek-flash"
              spellCheck={false}
            />
            <Input
              size="sm"
              className="w-52"
              value={logEndpointFilter}
              aria-label="按端点筛选"
              onChange={e => {
                setLogEndpointFilter(e.target.value);
                setAnalyticsPage(1);
              }}
              placeholder="端点名称或 ID"
              spellCheck={false}
            />
            {(logStatusFilter || logModelFilter || logEndpointFilter) && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setLogStatusFilter('');
                  setLogModelFilter('');
                  setLogEndpointFilter('');
                  setAnalyticsPage(1);
                }}
                icon={<X className="h-3.5 w-3.5" />}
              >
                清除筛选
              </Button>
            )}
          </div>
          {/* 表格视口高度：日志 tab 单独做行内部滚动，外层限高让表头吸顶、行在
              容器内滚动。偏移量 = 顶栏 58 + 吸顶 tab 栏 58 + PageStack 间距 16
              + 筛选行 28（sm 控件 h-7）+ 卡片间距 12 + 底部 gutter 12 = 184。
              该模块整体是整页滚动模式，父级没有确定高度，故此处按视口高度扣减。 */}
          <LayerCard className="flex h-[calc(100dvh-184px)] min-h-64 w-full min-w-0 flex-col overflow-hidden p-0 shadow-none">
            <div className="min-h-0 min-w-0 flex-1 overflow-auto scrollbar-thin">
              <Table layout="fixed" className="min-w-[1362px] [&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
<colgroup>
                  <col style={{ width: 140 }} />
                  <col style={{ width: 104 }} />
                  <col style={{ width: 140 }} />
                  <col style={{ width: 64 }} />
                  <col style={{ width: 100 }} />
                  <col style={{ width: 100 }} />
                  <col style={{ width: 160 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 150 }} />
                  <col style={{ width: 88 }} />
                </colgroup>
                <Table.Header sticky variant="compact">
                  <Table.Row>
                    <Table.Head className="text-left">时间</Table.Head>
                    <Table.Head className="text-left">端点</Table.Head>
                    <Table.Head className="text-left">模型</Table.Head>
                    <Table.Head className="text-center">状态</Table.Head>
                    <Table.Head className="text-left">出口 IP</Table.Head>
                    <Table.Head className="text-left">客户端 IP</Table.Head>
                    <Table.Head className="text-left">耗时/首字</Table.Head>
                    <Table.Head className="text-left">输入 / 输出</Table.Head>
                    <Table.Head className="text-left">缓存</Table.Head>
                    <Table.Head className="text-left">总消耗</Table.Head>
                    <Table.Head className="text-left" title="输出速度（输出词元/秒）">T/S</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {analyticsLoading && analyticsLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={11} className="text-center py-8">
                        <Loader size={20} className="mx-auto text-kumo-subtle" />
                      </Table.Cell>
                    </Table.Row>
                  ) : analyticsLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={11} className="text-center py-8 text-kumo-subtle text-sm">
                        暂无网关日志记录
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    analyticsLogs.map(log => {
                      const detail = costDetailsFor(log, endpoints);
                      return (
                        <Table.Row key={log.id} className="text-sm">
                          <Table.Cell className="truncate text-left font-mono text-kumo-subtle" title={formatDateTime(log.timestamp)}>
                            {formatDateTime(log.timestamp)}
                          </Table.Cell>
                          <Table.Cell
                            className="break-all text-left font-semibold text-kumo-strong"
                            title={log.endpointName}
                          >
                            <span className="inline-flex min-w-0 items-center gap-2">
                              <FailoverPathBadge path={log.failoverPath} endpointName={log.endpointName} />
                              {typeof log.keyIndex === 'number' && log.keyIndex >= 0 && (
                                <StatusBadge tone="info" title={`使用的 API Key 序号（0=主 key）`}>
                                  K{log.keyIndex + 1}
                                </StatusBadge>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell
                            className={`truncate text-left font-mono font-medium ${log.statusCode >= 400 && log.errorResponse ? 'cursor-pointer text-kumo-danger' : 'text-kumo-strong'}`}
                            title={log.statusCode >= 400 && log.errorResponse ? '点击查看报错详情' : log.model}
                            onClick={log.statusCode >= 400 && log.errorResponse ? () => {
                              setLogDetailExpanded(false);
                              setLogDetail(log);
                            } : undefined}
                          >
                            <span className="inline-flex min-w-0 items-center gap-1.5">
                              {log.realModel && log.realModel !== log.model ? (
                                <Popover>
                                  <Popover.Trigger
                                    nativeButton={false}
                                    render={
                                      <span
                                        className="cursor-pointer truncate text-kumo-info hover:text-kumo-strong"
                                        title="点击查看映射前实际模型"
                                        onClick={e => e.stopPropagation()}
                                      >
                                        {log.model}
                                      </span>
                                    }
                                  />
                                  <Popover.Content className="p-3 max-w-xs">
                                    <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                                      实际模型
                                    </Popover.Title>
                                    <div className="mt-2 grid gap-1.5 text-xs">
                                      <div className="flex items-center gap-2">
                                        <span className="shrink-0 text-kumo-subtle">对外名称</span>
                                        <code className="truncate rounded bg-kumo-surface-2 px-2 py-0.5 font-mono text-kumo-strong select-all">
                                          {log.model}
                                        </code>
                                      </div>
                                      <div className="flex items-center gap-2">
                                        <span className="shrink-0 text-kumo-subtle">实际模型</span>
                                        <code className="truncate rounded bg-kumo-surface-2 px-2 py-0.5 font-mono text-kumo-strong select-all">
                                          {log.realModel}
                                        </code>
                                      </div>
                                    </div>
                                  </Popover.Content>
                                </Popover>
                              ) : (
                                <span className="truncate">{log.model}</span>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell className="text-center">
                            <span className="inline-flex items-center gap-2">
                              <StatusBadge tone={statusCodeTone(log.statusCode)}>
                                {log.statusCode}
                              </StatusBadge>
                              {log.statusCode === 503 && (
                                <StatusBadge tone="warning" title="网关无可用渠道">
                                  无
                                </StatusBadge>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell
                            className="text-left font-mono text-kumo-subtle"
                            title={log.upstreamIp || '本机出口'}
                          >
                            <div
                              className="inline-flex items-center gap-2"
                              title="经代理池出口"
                            >
                              <IpCell value={log.upstreamIp} viaProxy={log.viaProxy} />
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-left font-mono text-kumo-subtle"
                            title={log.clientIp || '无客户端 IP'}
                          >
                            <IpCell value={log.clientIp} v6EdgeOnly />
                          </Table.Cell>
                          <Table.Cell className="text-left">
                            <div
                              className="inline-flex items-center gap-2"
                              title={log.stream ? '流式响应' : '非流式响应'}
                            >
                              <StatusBadge tone={resultTone(log.statusCode, log.completionTokens, log.latencyMs)}>
                                {(log.latencyMs / 1000).toFixed(1)}s
                              </StatusBadge>
                              <StatusBadge tone={ttfbTone(log.ttfbMs)}>
                                {log.ttfbMs > 0 ? (log.ttfbMs / 1000).toFixed(1) + 's' : '—'}
                              </StatusBadge>
                              <StatusBadge
                                tone={log.stream ? 'info' : 'warning'}
                                className="!px-1.5 !text-[10px]"
                              >
                                {log.stream ? '流' : '非流'}
                              </StatusBadge>
                            </div>
                          </Table.Cell>
                          <Table.Cell className="text-left font-mono">
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right text-kumo-strong">
                                {log.promptTokens}
                              </span>
                              <span className="shrink-0 px-0.5 text-kumo-subtle">/</span>
                              <span className="text-left text-kumo-strong">
                                {log.completionTokens}
                              </span>
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="text-left font-mono"
                            title="缓存命中 词元（占比 = 缓存 / 输入）"
                          >
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right text-kumo-strong">
                                {log.cachedTokens}
                              </span>
                              <span className="shrink-0 px-0.5 text-kumo-subtle">（</span>
                              <span className="text-left text-kumo-strong">
                                {log.promptTokens > 0
                                  ? ((log.cachedTokens / log.promptTokens) * 100).toFixed(1)
                                  : '0.0'}
                                %
                              </span>
                              <span className="shrink-0 text-kumo-subtle">）</span>
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="text-left font-mono"
                            title="总消耗（实际消耗 = 总消耗 − 缓存）"
                          >
                            {detail ? (
                              <Popover>
                                <Popover.Trigger
                                  nativeButton={false}
                                  title="点击查看费用详情"
                                  render={
                                    <div className="flex w-full cursor-pointer items-baseline justify-start whitespace-nowrap">
                                      <span className="text-right font-semibold leading-none text-kumo-success">
                                        {formatCostAmount(detail.cost, detail.currency)}
                                      </span>
                                      <span className="shrink-0 px-0.5 leading-none text-kumo-subtle">（</span>
                                      <span className="text-left font-mono leading-none text-kumo-subtle">
                                        {Math.max(0, log.totalTokens - log.cachedTokens)}
                                      </span>
                                      <span className="shrink-0 leading-none text-kumo-subtle">）</span>
                                    </div>
                                  }
                                />
                                <Popover.Content className="w-72 p-3">
                                  <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                                    费用详情
                                  </Popover.Title>
                                  <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                                    <div className="flex items-center justify-between gap-3">
                                      <span className="text-kumo-subtle">模型</span>
                                      <span className="max-w-44 truncate font-mono">{detail.model || '—'}</span>
                                    </div>
                                    <div className="flex items-center justify-between gap-3">
                                      <span className="text-kumo-subtle">端点</span>
                                      <span className="max-w-44 truncate">{detail.endpointName || '—'}</span>
                                    </div>
                                    <div className="flex items-center justify-between gap-3">
                                      <span className="text-kumo-subtle">货币</span>
                                      <span className="font-mono">{detail.currency}</span>
                                    </div>
                                    {detail.hasPricing ? (
                                      <>
                                        <div className="border-t border-kumo-line pt-1.5">
                                          <span className="text-kumo-subtle">单价（每百万词元）</span>
                                        </div>
                                        <div className="flex items-center justify-between gap-3">
                                          <span className="text-kumo-subtle">输入</span>
                                          <span className="font-mono">{formatUnitPrice(detail.inputUnit)}</span>
                                        </div>
                                        <div className="flex items-center justify-between gap-3">
                                          <span className="text-kumo-subtle">输出</span>
                                          <span className="font-mono">{formatUnitPrice(detail.outputUnit)}</span>
                                        </div>
                                        {detail.cacheUnit > 0 && (
                                          <div className="flex items-center justify-between gap-3">
                                            <span className="text-kumo-subtle">缓存</span>
                                            <span className="font-mono">{formatUnitPrice(detail.cacheUnit)}</span>
                                          </div>
                                        )}
                                        <div className="border-t border-kumo-line pt-1.5">
                                          <span className="text-kumo-subtle">用量分解</span>
                                        </div>
                                        <div className="flex items-center justify-between gap-3">
                                          <span className="text-kumo-subtle">输入（未缓存）</span>
                                          <span className="font-mono">{detail.input}</span>
                                        </div>
                                        {detail.cached > 0 && (
                                          <div className="flex items-center justify-between gap-3">
                                            <span className="text-kumo-subtle">缓存命中</span>
                                            <span className="font-mono">{detail.cached}</span>
                                          </div>
                                        )}
                                        <div className="flex items-center justify-between gap-3">
                                          <span className="text-kumo-subtle">输出</span>
                                          <span className="font-mono">{detail.completion}</span>
                                        </div>
                                        <div className="border-t border-kumo-line pt-1.5">
                                          <span className="text-kumo-subtle">费用分解</span>
                                        </div>
                                        {detail.inputCost > 0 && (
                                          <div className="flex items-center justify-between gap-3">
                                            <span className="text-kumo-subtle">输入费用</span>
                                            <span className="font-mono">{formatCostAmount(detail.inputCost, detail.currency)}</span>
                                          </div>
                                        )}
                                        {detail.cacheCost > 0 && (
                                          <div className="flex items-center justify-between gap-3">
                                            <span className="text-kumo-subtle">缓存费用</span>
                                            <span className="font-mono">{formatCostAmount(detail.cacheCost, detail.currency)}</span>
                                          </div>
                                        )}
                                        {detail.outputCost > 0 && (
                                          <div className="flex items-center justify-between gap-3">
                                            <span className="text-kumo-subtle">输出费用</span>
                                            <span className="font-mono">{formatCostAmount(detail.outputCost, detail.currency)}</span>
                                          </div>
                                        )}
                                        <div className="flex items-center justify-between gap-3 border-t border-kumo-line pt-1.5 font-semibold">
                                          <span className="text-kumo-subtle">合计</span>
                                          <span className="font-mono text-kumo-success">
                                            {formatCostAmount(detail.cost, detail.currency)}
                                          </span>
                                        </div>
                                      </>
                                    ) : (
                                      <div className="mt-1 text-xs text-kumo-subtle">
                                        端点未返回该模型的定价信息，仅展示已记录的费用金额。
                                      </div>
                                    )}
                                  </div>
                                </Popover.Content>
                              </Popover>
                            ) : (
                              <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                                <span className="text-right font-semibold leading-none text-brand">
                                  {log.totalTokens}
                                </span>
                                <span className="shrink-0 px-0.5 leading-none text-kumo-subtle">（</span>
                                <span className="text-left font-mono leading-none text-kumo-subtle">
                                  {Math.max(0, log.totalTokens - log.cachedTokens)}
                                </span>
                                <span className="shrink-0 leading-none text-kumo-subtle">）</span>
                              </div>
                            )}
                          </Table.Cell>
                          <Table.Cell
                            className="text-left font-mono text-kumo-strong"
                            title={
                              logOutputSpeedText(log) != null
                                ? (() => {
                                    const genSec = Math.max(0, (Number(log.latencyMs) || 0) - (Number(log.ttfbMs) || 0)) / 1000;
                                    return `输出速度 ${logOutputSpeedText(log)} T/S（输出词元 ${log.completionTokens} ÷ 输出耗时 ${genSec.toFixed(1)}s）`;
                                  })()
                                : '无输出或无法计时'
                            }
                          >
                            {logOutputSpeedText(log) || '—'}
                          </Table.Cell>
                        </Table.Row>
                      );
                    })
                  )}
                </Table.Body>
              </Table>
            </div>

            {analyticsTotal > 0 && (
              <Pagination
                page={analyticsPage}
                setPage={setAnalyticsPage}
                perPage={analyticsPageSize}
                totalCount={analyticsTotal}
                labels={{
                  navigation: '网关日志分页',
                  firstPage: '第一页',
                  previousPage: '上一页',
                  nextPage: '下一页',
                  lastPage: '最后一页',
                  pageNumber: '页码',
                  pageSize: '每页数量',
                }}
                className="shrink-0 flex-wrap gap-x-3 gap-y-1 border-x-0 border-b-0 border-t border-kumo-line bg-kumo-base px-3 py-2 text-sm shadow-none [&_[data-slot=pagination-controls]]:ml-auto [&_[data-slot=pagination-info]]:min-w-0 max-sm:[&_[data-slot=pagination-info]]:hidden max-sm:[&_[data-slot=pagination-page-size]]:hidden max-sm:[&_[data-slot=pagination-separator]]:hidden max-sm:[&_[data-slot=pagination-controls]]:m-auto"
              >
                <Pagination.Info>
                  {({ pageShowingRange, totalCount }) => (
                    <span className="text-kumo-subtle">
                      显示 {pageShowingRange}，共 {totalCount} 条
                    </span>
                  )}
                </Pagination.Info>
                <Pagination.Separator />
                <Pagination.PageSize
                  value={analyticsPageSize}
                  onChange={size => {
                    setAnalyticsPageSize(size);
                    setAnalyticsPage(1);
                  }}
                  options={[10, 20, 50, 100]}
                  label="每页"
                />
                <Pagination.Controls />
              </Pagination>
            )}
          </LayerCard>
        </div>
  );
}
