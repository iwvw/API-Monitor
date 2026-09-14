import React from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Table } from '@cloudflare/kumo/components/table';
import { LayerCard } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { AppCard, FieldRow, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Activity, ChevronDown, ChevronUp, Columns, Database, Download, FileText, HardDrive, RefreshCw, Trash, Upload } from '../../components/Icons.jsx';
import { BackupPanel } from '../BackupPage.jsx';
import { formatFileSize, toInt } from './utils.js';

export function DatabasePanel({
  cleanupDeprecatedTables,
  commitDatabaseImport,
  databaseBusy,
  databaseSegments,
  databaseStorage,
  dbImportPreview,
  dbStats,
  dbTableDisplayRows,
  dbTablesExpanded,
  deprecatedTableItems,
  deprecatedTables,
  exportDatabase,
  fetchDbState,
  fileInputRef,
  formatTableMetricSize,
  formatTableRows,
  importDatabase,
  postSettingsAction,
  previewDatabaseImport,
  runDatabaseVacuum,
  setDbImportPreview,
  setDbTablesExpanded,
  tableRows,
}) {
  return (
        <div className="grid min-w-0 items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]">
          <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            className="min-w-0"
            title="数据库导入导出"
            icon={<Download className="h-4 w-4 text-brand" />}
            bodyPadding="sm"
            bodyClassName="space-y-3"
          >
            <Input
              ref={fileInputRef}
              type="file"
              accept=".db"
              aria-label="选择数据库文件"
              className="hidden"
              onChange={previewDatabaseImport}
            />
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={exportDatabase}
                aria-label="导出数据库"
                title="导出数据库"
                icon={<Upload className="h-3.5 w-3.5" />}
              >
                导出数据库
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={importDatabase}
                loading={databaseBusy}
                aria-label="导入数据库"
                title="导入数据库"
                icon={<Download className="h-3.5 w-3.5" />}
              >
                导入数据库
              </Button>
            </div>
            {dbImportPreview && (
              <AppCard padding="none" className="bg-kumo-recessed/40 p-3 text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold text-kumo-strong truncate">{dbImportPreview.originalName}</span>
                  <Badge variant={dbImportPreview.analysis?.integrity === 'ok' ? 'success' : 'warning'}>
                    {dbImportPreview.analysis?.integrity || 'unknown'}
                  </Badge>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <div className="rounded-md border border-kumo-line/70 bg-kumo-base px-3 py-2">
                    <div className="text-[10px] font-semibold uppercaser text-kumo-subtle">大小</div>
                    <div className="mt-1 font-mono text-kumo-strong">{formatFileSize(dbImportPreview.analysis?.sizeBytes)}</div>
                  </div>
                  <div className="rounded-md border border-kumo-line/70 bg-kumo-base px-3 py-2">
                    <div className="text-[10px] font-semibold uppercaser text-kumo-subtle">表数量</div>
                    <div className="mt-1 font-mono text-kumo-strong">{dbImportPreview.analysis?.tableCount || 0}</div>
                  </div>
                </div>
                {dbImportPreview.warnings?.length > 0 && (
                  <div className="mt-2 space-y-1 rounded border border-kumo-warning/30 bg-kumo-warning/10 p-2 text-[11px] text-kumo-warning">
                    {dbImportPreview.warnings.map((warning) => (
                      <div key={warning}>{warning}</div>
                    ))}
                  </div>
                )}
                <div className="mt-3 max-h-44 overflow-y-auto rounded border border-kumo-line bg-kumo-base">
                  <Table layout="fixed">
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>表名</Table.Head>
                        <Table.Head className="w-20">记录数</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {(dbImportPreview.analysis?.tables || []).slice(0, 20).map((row) => (
                        <Table.Row key={row.name}>
                          <Table.Cell className="truncate font-mono text-[11px]">{row.name}</Table.Cell>
                          <Table.Cell className="font-mono text-[11px]">{row.rows}</Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </Table>
                </div>
                <div className="mt-3 flex gap-2">
                  <Button size="sm" variant="primary" className="flex-1 justify-center" onClick={commitDatabaseImport} loading={databaseBusy}>
                    确认导入
                  </Button>
                  <Button size="sm" variant="secondary" className="flex-1 justify-center" onClick={() => setDbImportPreview(null)}>
                    取消
                  </Button>
                </div>
              </AppCard>
            )}
          </SectionCard>

          <BackupPanel embedded />

          </div>
          <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            title="维护操作"
              icon={<HardDrive className="h-4 w-4 text-brand" />}
              bodyPadding="none"
            >
              <FieldRow title="压缩数据库">
                <Button
                  size="sm"
                  onClick={() => runDatabaseVacuum()}
                  loading={databaseBusy}
                >
                  立即压缩
                </Button>
              </FieldRow>

              <FieldRow title="清理运行日志">
                <Button
                  size="sm"
                  variant="secondary-destructive"
                  onClick={() => postSettingsAction('/api/settings/clear-logs', '数据库日志已清理', fetchDbState)}
                  loading={databaseBusy}
                  icon={<Trash className="h-4 w-4" />}
                >
                  清理日志
                </Button>
              </FieldRow>

              <FieldRow title="清理废弃表">
                <div className="flex items-center gap-2">
                  <Badge variant={deprecatedTableItems.length > 0 ? 'warning' : 'secondary'}>{deprecatedTableItems.length} 张</Badge>
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    onClick={cleanupDeprecatedTables}
                    loading={databaseBusy}
                    disabled={deprecatedTableItems.length === 0}
                    icon={<Trash className="h-4 w-4" />}
                  >
                    清理废弃表
                  </Button>
                </div>
              </FieldRow>

              <div className="mx-4 mb-4 mt-3 rounded-lg border border-kumo-line/80 bg-kumo-base px-3 pt-3 pb-2">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-semibold text-kumo-strong">废弃表候选</div>
                  </div>
                  <Badge variant={deprecatedTableItems.length > 0 ? 'warning' : 'secondary'}>
                    {deprecatedTableItems.length} 张
                  </Badge>
                </div>

                <div className="mt-2 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-3 py-2.5">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
                    <span className="text-kumo-subtle">候选表 <span className="font-semibold text-kumo-strong">{deprecatedTableItems.length}</span></span>
                    <span className="text-kumo-subtle">记录数 <span className="font-semibold text-kumo-strong">{deprecatedTables?.totalRows || 0}</span></span>
                    <span className="text-kumo-subtle">占用 <span className="font-semibold text-kumo-strong">{formatFileSize(deprecatedTables?.totalSize)}</span></span>
                  </div>
                </div>

                {deprecatedTableItems.length > 0 && (
                  <div className="mt-3 max-h-40 overflow-y-auto divide-y divide-kumo-line rounded-md border border-kumo-line/70 bg-kumo-recessed/10 text-[11px]">
                    {deprecatedTableItems.slice(0, 8).map((item) => (
                      <div key={item.table} className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 px-3 py-2">
                        <div className="min-w-0">
                          <div className="truncate font-mono text-kumo-strong" title={item.table}>{item.table}</div>
                          <div className="mt-0.5 truncate text-kumo-subtle" title={item.reason}>{item.reason}</div>
                        </div>
                        <span className="font-mono text-kumo-subtle">{formatFileSize(item.sizeBytes)}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </SectionCard>

          <SectionCard
            title="数据库统计"
            description={dbStats?.dbPath || 'SQLite 数据文件'}
            icon={<Database className="h-4 w-4 text-brand" />}
            actions={
                <Button size="sm" onClick={() => fetchDbState().catch((error) => toast.error(error.message || '加载数据库统计失败'))} loading={databaseBusy} icon={<RefreshCw className="h-4 w-4" />}>刷新统计</Button>
            }
            bodyPadding="none"
            bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
          >
            {databaseStorage && (
              <div className="shrink-0 border-b border-kumo-line">
                <div className="p-3.5">
                  <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-4 cq-sm:gap-3">
                  {[
                    { title: '总占用', description: '主库 + WAL/SHM + 空闲页合计', value: formatFileSize(databaseStorage.totalSizeBytes), icon: <Database className="h-3.5 w-3.5 text-brand" />, valueClassName: 'text-brand' },
                    { title: '主库文件', description: 'SQLite 主数据库', value: formatFileSize(databaseStorage.mainSizeBytes), icon: <FileText className="h-3.5 w-3.5 text-kumo-strong" />, valueClassName: 'text-kumo-strong' },
                    { title: 'WAL / SHM', description: '预写日志与共享内存', value: formatFileSize((databaseStorage.walSizeBytes || 0) + (databaseStorage.shmSizeBytes || 0)), icon: <Activity className="h-3.5 w-3.5 text-kumo-warning" />, valueClassName: 'text-kumo-warning' },
                    { title: '空闲页', description: '可直接回收的空间', value: formatFileSize(databaseStorage.freePageBytes), icon: <Columns className="h-3.5 w-3.5 text-kumo-info" />, valueClassName: 'text-kumo-info' },
                  ].map((item) => (
                    <LayerCard key={item.title} className="min-w-0 p-2.5 cq-sm:p-3">
                      <div title={item.description} className="flex items-center justify-between gap-2 text-[11px] text-kumo-subtle cq-sm:gap-3 cq-sm:text-xs">
                        <span className="truncate">{item.title}</span>
                        <span className="shrink-0">{item.icon}</span>
                      </div>
                      <div className={`mt-1 truncate text-base font-semibold tabular-nums ${item.valueClassName}`} title={item.value}>{item.value}</div>
                    </LayerCard>
                  ))}
                  </div>
                </div>
                {databaseSegments.length > 0 && (
                  <div className="flex flex-col gap-2 border-t border-kumo-line bg-kumo-surface px-3.5 py-3">
                    <div className="flex h-1.5 w-full items-center overflow-hidden rounded-full bg-kumo-recessed">
                      {databaseSegments.map((s) => (
                        <div key={s.label} className={`h-full ${s.barClass}`} style={{ width: `${s.percent}%` }} title={`${s.label} ${formatFileSize(s.value)}`} />
                      ))}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                      {databaseSegments.map((s) => (
                        <span key={s.label} className="inline-flex items-center gap-1.5 text-[10px] text-kumo-subtle">
                          <span className={`h-2 w-2 rounded-full ${s.barClass}`} />
                          {s.label}
                          <span className="tabular-nums text-kumo-default">{formatFileSize(s.value)}</span>
                        </span>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
            {tableRows.length > 0 && (
              <div className="flex shrink-0 items-center justify-between border-b border-kumo-line bg-kumo-surface px-3.5 py-1.5">
                <span className="text-xs font-semibold text-kumo-strong">数据库表（{tableRows.length} 张）</span>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => setDbTablesExpanded((v) => !v)}
                  className="gap-1 text-xs font-medium text-brand"
                >
                  {dbTablesExpanded ? '收起' : '展开全部'}
                  {dbTablesExpanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                </Button>
              </div>
            )}
            <div className="min-h-0 flex-1 overflow-auto">
              <Table layout="fixed">
                <colgroup>
                  <col className="w-[28%]" />
                  <col className="w-[14%]" />
                  <col className="w-[19%]" />
                  <col className="w-[18%]" />
                  <col className="w-[21%]" />
                </colgroup>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>表名</Table.Head>
                    <Table.Head>记录数</Table.Head>
                    <Table.Head>占用</Table.Head>
                    <Table.Head>索引</Table.Head>
                    <Table.Head>行大小</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {tableRows.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={5} className="p-8 text-center text-kumo-subtle">
                        {databaseBusy ? '正在加载统计...' : '暂无统计数据'}
                      </Table.Cell>
                    </Table.Row>
                  ) : !dbTablesExpanded ? (
                    <Table.Row>
                      <Table.Cell colSpan={5} className="p-8 text-center text-kumo-subtle">
                        已折叠 {tableRows.length} 张表，点击上方展开全部
                      </Table.Cell>
                    </Table.Row>
                  ) : dbTableDisplayRows.map((row) => (
                    <Table.Row key={row.table}>
                      <Table.Cell className="truncate font-mono text-xs text-kumo-strong" title={row.table}>{row.table}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableRows(row.rows)}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableMetricSize(row.estimatedSizeBytes)}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableMetricSize(row.indexSizeBytes)}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableMetricSize(row.avgRowSizeBytes)}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          </SectionCard>
          </div>
        </div>
  );
}
