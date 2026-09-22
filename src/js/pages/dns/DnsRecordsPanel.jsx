import React from 'react';
import { LayerCard, Toolbar } from '@cloudflare/kumo';
import { AnimatedCollapse } from '../../components/AnimatedCollapse.jsx';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import SiteFontTimeseriesChart from '../../components/SiteFontTimeseriesChart.jsx';
import { ChevronDown, ChevronUp, Download, Edit, Globe, Plus, Trash, Upload } from '../../components/Icons.jsx';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { AppTable } from '../../components/ui/AppPrimitives.jsx';
import { SSL_MODES } from './constants.js';
import {
  DnsPanelCard,
  formatAnalyticsAxisTime,
  formatBytes,
  formatDate,
  formatNumber,
  formatPercent,
  recordShortName,
  recordTypeBadgeVariant,
  sslModeLabel,
} from './utils.jsx';

const DNS_RECORD_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'type', role: 'type' },
  { id: 'name', role: 'primary', grow: 1 },
  { id: 'content', role: 'content', minWidth: 200, verticalAlign: 'middle', grow: 1 },
  { id: 'ttl', role: 'count' },
  { id: 'proxied', role: 'status' },
  { id: 'modifiedOn', role: 'datetime' },
  { id: 'actions', role: 'actions-md' },
];

function DnsRecordsPanel({
  loading,
  records,
  selectedZone,
  selectedZoneId,
  recordFilter,
  setRecordFilter,
  recordTypes,
  recordColWidths,
  startRecordResize,
  renderResizeHead,
  selectedRecordIds,
  setSelectedRecordIds,
  toggleRecordSelection,
  isArmed,
  analyticsRange,
  loadAnalytics,
  analyticsPoints,
  showAnalyticsCharts,
  setShowAnalyticsCharts,
  showAnalyticsPanel,
  analyticsChartCards,
  analyticsSummary,
  sslInfo,
  updateSslMode,
  echarts,
  isDarkMode,
  loadRecords,
  openRecordModal,
  deleteRecord,
  exportRecords,
  openImportModal,
  batchDeleteRecords,
}) {
  return (
    <>
      <div className="flex min-h-8 shrink-0 items-center justify-between gap-2 px-1">
        <div className="flex min-w-0 items-center gap-2 text-xs text-kumo-subtle">
          <Globe className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate font-medium text-kumo-strong">
            {selectedZone ? selectedZone.name : 'DNS 记录'}
          </span>
        </div>
        {selectedZone && (
          <div className="flex shrink-0 items-center gap-2">
            <Select alignItemWithTrigger size="sm"
              value={analyticsRange}
              onValueChange={(value) => loadAnalytics(String(value))}
              className="w-20 shrink-0"
              items={[
                { value: '24h', label: '24 小时' },
                { value: '7d', label: '7 天' },
                { value: '30d', label: '30 天' },
              ]}
            />
            {(analyticsPoints.length > 0 || loading.analytics) && (
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setShowAnalyticsCharts((value) => !value)}
                icon={showAnalyticsCharts ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                className="shrink-0 px-2"
              >
                {showAnalyticsCharts ? '收起趋势' : '展开趋势'}
              </Button>
            )}
            <div className="shrink-0 text-xs text-kumo-subtle">
              {records.length} 条记录
            </div>
          </div>
        )}
      </div>
      {selectedZone ? (
        <>
          <div className="dns-summary-grid order-3 grid shrink-0 gap-2 cq-md:order-none">
            <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
              <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">SSL 模式</div>
              <Select alignItemWithTrigger size="sm"
                value={sslInfo?.mode || null}
                onValueChange={(value) => updateSslMode(String(value))}
                placeholder="选择模式"
                loading={loading.ssl}
                renderValue={(value) => sslModeLabel(value)}
                className="w-18 shrink-0"
                items={SSL_MODES}
              />
            </DnsPanelCard>
            <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
              <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">唯一访问者</div>
              <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">{loading.analytics ? '加载中' : formatNumber(analyticsSummary.uniques)}</div>
            </DnsPanelCard>
            <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
              <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">请求量</div>
              <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">{loading.analytics ? '加载中' : formatNumber(analyticsSummary.requests)}</div>
            </DnsPanelCard>
            <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
              <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">带宽</div>
              <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">{loading.analytics ? '加载中' : formatBytes(analyticsSummary.bandwidth)}</div>
            </DnsPanelCard>
            <DnsPanelCard className="flex min-h-9 items-center justify-between gap-2 p-2">
              <div className="flex min-w-0 items-center gap-2">
                  <div className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">缓存命中率</div>
                  <div className="shrink-0 whitespace-nowrap text-base font-semibold leading-5 text-kumo-strong">
                    {loading.analytics ? '加载中' : formatPercent(analyticsSummary.cacheHitRate)}
                  </div>
                </div>
            </DnsPanelCard>
          </div>
          <AnimatedCollapse
            open={showAnalyticsPanel}
            className={showAnalyticsPanel ? 'order-5 shrink-0 cq-md:order-none' : 'contents'}
          >
            <div className="dns-chart-grid grid gap-2 pt-0.5">
            {analyticsChartCards.map((card) => (
              <DnsPanelCard key={card.key} className="min-w-0 overflow-hidden p-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="truncate text-xs font-medium text-kumo-strong">{card.label}</div>
                  <div className="shrink-0 text-xs font-semibold text-kumo-subtle">{loading.analytics ? '加载中' : card.value}</div>
                </div>
                <div className="mt-2 min-w-0 overflow-hidden" style={{ height: 108 }}>
                  <SiteFontTimeseriesChart
                    echarts={echarts}
                    data={card.data}
                    height={108}
                    isDarkMode={isDarkMode}
                    gradient
                    loading={loading.analytics && analyticsPoints.length === 0}
                    xAxisTickCount={3}
                    yAxisTickCount={2}
                    xAxisTickFormat={(timestamp) => formatAnalyticsAxisTime(timestamp, analyticsRange)}
                    yAxisTickFormat={card.yAxisTickFormat}
                    tooltipValueFormat={card.tooltipValueFormat}
                    tooltipFollowCursor="x"
                    ariaDescription={`Cloudflare ${card.label}`}
                  />
                </div>
              </DnsPanelCard>
            ))}
            </div>
          </AnimatedCollapse>
          <div className="dns-toolbar-frame order-1 flex shrink-0 flex-wrap items-center justify-between gap-2 p-2 cq-md:order-none">
            <div className="grid w-full grid-cols-[minmax(0,1fr)_minmax(7rem,0.7fr)_auto] gap-2 cq-sm:flex cq-sm:w-auto cq-sm:flex-wrap cq-sm:items-center">
              <Input size="sm"
                aria-label="按名称筛选 DNS 记录"
                value={recordFilter.name}
                onChange={(event) => setRecordFilter((prev) => ({ ...prev, name: event.target.value }))}
                placeholder="筛选名称"
                className="w-full cq-sm:w-48"
              />
              <Select alignItemWithTrigger size="sm"
                aria-label="按类型筛选 DNS 记录"
                value={recordFilter.type || null}
                onValueChange={(value) => setRecordFilter((prev) => ({ ...prev, type: value ? String(value) : '' }))}
                placeholder="全部类型"
                className="w-full cq-sm:w-36"
                items={recordTypes.map((type) => ({ value: type, label: type }))}
              />
              <Button size="sm" variant="secondary" onClick={() => loadRecords(selectedZoneId, recordFilter)}>
                查询
              </Button>
            </div>
            <div className="flex w-full flex-wrap items-center gap-2 cq-sm:w-auto">
              <Button size="sm" onClick={() => openRecordModal()} icon={<Plus className="h-4 w-4" />}>
                添加记录
              </Button>
              <Toolbar size="sm" aria-label="导出导入 DNS 记录" className="shrink-0">
                <Toolbar.Button onClick={exportRecords} aria-label="导出 DNS 记录" icon={<Upload className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导出</span>
                </Toolbar.Button>
                <Toolbar.Button onClick={() => openImportModal('records')} aria-label="导入 DNS 记录" icon={<Download className="h-3.5 w-3.5" />}>
                  <span className="hidden cq-sm:inline">导入</span>
                </Toolbar.Button>
              </Toolbar>
              {selectedRecordIds.length > 0 && (
                <Button size="sm" variant={isArmed('batch-records') ? 'destructive' : 'secondary-destructive'} onClick={batchDeleteRecords} icon={<Trash className="h-4 w-4" />}>
                  删除 {selectedRecordIds.length}
                </Button>
              )}
            </div>
          </div>
          <div className="order-2 grid gap-2 pb-3 cq-md:hidden">
            {loading.records ? (
              Array.from({ length: 5 }).map((_, index) => (
                <LayerCard key={index} className="p-3">
                  <SkeletonLine className="h-4 w-32" />
                  <SkeletonLine className="mt-2 h-3.5 w-full" />
                  <SkeletonLine className="mt-2 h-3.5 w-24" />
                </LayerCard>
              ))
            ) : records.length === 0 ? (
              <LayerCard className="p-8 text-center text-xs text-kumo-subtle">暂无匹配记录。</LayerCard>
            ) : records.map((record) => (
              <LayerCard
                key={record.id}
                className={`p-3 ${selectedRecordIds.includes(record.id) ? 'ring-1 ring-brand/35' : ''}`}
                onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openRecordModal(record))}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <Badge variant={recordTypeBadgeVariant(record.type)} className="min-w-10 justify-center text-[10px] leading-4">{record.type}</Badge>
                      <span className="min-w-0 truncate text-sm font-semibold text-kumo-strong" title={recordShortName(record.name, selectedZone.name)}>
                        {recordShortName(record.name, selectedZone.name)}
                      </span>
                      <Badge variant={record.proxied ? 'success' : 'outline'} className="text-[10px] leading-4">{record.proxied ? '代理' : '仅 DNS'}</Badge>
                    </div>
                    <div className="mt-2 break-all font-mono text-[11px] text-kumo-default" title={record.content}>{record.content}</div>
                    <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-kumo-subtle">
                      <span>TTL {record.ttl === 1 ? '自动' : record.ttl}</span>
                      <span>{formatDate(record.modifiedOn)}</span>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button size="sm" shape="square" variant="secondary" onClick={() => openRecordModal(record)} aria-label={`编辑 ${record.name}`} title="编辑" icon={<Edit className="h-3.5 w-3.5" />} />
                    <Button size="sm" shape="square" variant={isArmed(`record:${record.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteRecord(record)} aria-label={`删除 ${record.name}`} title="删除" icon={<Trash className="h-3.5 w-3.5" />} />
                  </div>
                </div>
              </LayerCard>
            ))}
          </div>
          <div className="dns-table-frame order-2 hidden max-w-full cq-md:flex cq-md:order-none">
            <div className="dns-table-scroll scrollbar-thin">
            <AppTable tableId="dns-records" columns={DNS_RECORD_COLUMNS} columnWidths={recordColWidths} className="w-full text-xs">
              <Table.Header sticky variant="compact">
                <Table.Row className="h-8">
                  <Table.CheckHead
                    checked={records.length > 0 && selectedRecordIds.length === records.length}
                    indeterminate={selectedRecordIds.length > 0 && selectedRecordIds.length < records.length}
                    onCheckedChange={(checked) => setSelectedRecordIds(checked ? records.map((record) => record.id) : [])}
                    aria-label="全选 DNS 记录"
                    className="!px-2 !py-1.5 text-center"
                  />
                  {renderResizeHead('类型', 1, startRecordResize, 'center')}
                  {renderResizeHead('名称', 2, startRecordResize)}
                  {renderResizeHead('内容', 3, startRecordResize)}
                  {renderResizeHead('TTL', 4, startRecordResize, 'center')}
                  {renderResizeHead('代理', 5, startRecordResize, 'center')}
                  {renderResizeHead('更新时间', 6, startRecordResize, 'center')}
                  <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {loading.records ? (
                  Array.from({ length: 5 }).map((_, index) => (
                    <Table.Row key={index} className="h-9">
                      <Table.Cell colSpan={8} className="!px-2.5 !py-1.5"><SkeletonLine className="mx-auto h-3.5 w-full max-w-2xl" /></Table.Cell>
                    </Table.Row>
                  ))
                ) : records.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={8} className="py-10 text-center text-kumo-subtle">
                      暂无匹配记录。
                    </Table.Cell>
                  </Table.Row>
                ) : records.map((record) => (
                  <Table.Row
                    key={record.id}
                    variant={selectedRecordIds.includes(record.id) ? 'selected' : 'default'}
                    className="h-9 cursor-pointer"
                    title="双击编辑记录"
                    onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openRecordModal(record))}
                  >
                    <Table.CheckCell
                      checked={selectedRecordIds.includes(record.id)}
                      onCheckedChange={(checked) => toggleRecordSelection(record.id, Boolean(checked))}
                      aria-label={`选择 ${record.name}`}
                      className="!px-2 !py-1.5 text-center"
                    />
                    <Table.Cell className="!px-2.5 !py-1.5 text-center">
                      <Badge variant={recordTypeBadgeVariant(record.type)} className="min-w-12 justify-center text-[10px] leading-4">
                        {record.type}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 text-left font-semibold text-kumo-strong">
                      <div className="truncate" title={recordShortName(record.name, selectedZone.name)}>{recordShortName(record.name, selectedZone.name)}</div>
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 text-left">
                      <div className="truncate font-mono text-[11px]" title={record.content}>{record.content}</div>
                    </Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 text-center">{record.ttl === 1 ? '自动' : record.ttl}</Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 text-center"><Badge variant={record.proxied ? 'success' : 'outline'} className="text-[10px] leading-4">{record.proxied ? '开启' : '关闭'}</Badge></Table.Cell>
                    <Table.Cell className="!px-2.5 !py-1.5 text-center">
                      <div className="truncate" title={formatDate(record.modifiedOn)}>{formatDate(record.modifiedOn)}</div>
                    </Table.Cell>
                    <Table.Cell className="!px-2 !py-1.5 text-center">
                      <div className="inline-flex gap-1">
                        <Button size="sm" shape="square" variant="secondary" onClick={() => openRecordModal(record)} aria-label={`编辑 ${record.name}`} title="编辑" icon={<Edit className="h-3.5 w-3.5" />} />
                        <Button size="sm" shape="square" variant={isArmed(`record:${record.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteRecord(record)} aria-label={`删除 ${record.name}`} title="删除" icon={<Trash className="h-3.5 w-3.5" />} />
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
            </div>
          </div>
        </>
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center rounded-lg border border-kumo-line bg-kumo-base p-8 shadow-none">
          <div className="flex flex-col items-center gap-3 text-center text-sm text-kumo-subtle">
            <Globe className="h-10 w-10 text-kumo-subtle" />
            <div>选择左侧域名后管理记录。</div>
          </div>
        </div>
      )}
    </>
  );
}

export default DnsRecordsPanel;
