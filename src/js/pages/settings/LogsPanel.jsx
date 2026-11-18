import React from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { AppTable, EmptyState, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { MobileCard, MobileCardField, MobileCardFields, MobileCardList } from '../../components/ui/MobileTableCards.jsx';
import { Database, FileText, Save, Trash } from '../../components/Icons.jsx';
import { toInt } from './utils.js';

const FALLBACK_FAMILY = 'ui-sans-serif, system-ui, sans-serif';
const CELL_PADDING_WIDE = 20;
const CELL_PADDING_NARROW = 16;
const BADGE_BOX = 18;
const FALLBACK_WIDTHS = { time: 168, operation: 160, table: 176 };

function resolveFontFamily() {
  if (typeof window === 'undefined' || !document.body) return FALLBACK_FAMILY;
  const family = window.getComputedStyle(document.body).fontFamily;
  return family && family.trim() ? family : FALLBACK_FAMILY;
}

function resolveFontPx() {
  if (typeof window === 'undefined') return 12;
  const base = parseFloat(window.getComputedStyle(document.documentElement).fontSize);
  if (!Number.isFinite(base) || base <= 0) return 12;
  return 0.75 * base;
}

function measureAuditWidths(logs) {
  if (typeof document === 'undefined' || logs.length === 0) return FALLBACK_WIDTHS;
  const probe = document.createElement('canvas');
  const ctx = probe.getContext('2d');
  if (!ctx) return FALLBACK_WIDTHS;
  const family = resolveFontFamily();
  const fontPx = resolveFontPx();
  const measure = (values, weight) => {
    ctx.font = `${weight} ${fontPx}px ${family}`;
    return values.reduce((max, value) => Math.max(max, ctx.measureText(String(value || '')).width), 0);
  };
  const clamp = (value, min, max) => Math.min(max, Math.max(min, Math.ceil(value)));
  const widestOperation = measure(logs.map(log => log.operation_type), 500);
  return {
    time: clamp(measure(logs.map(log => log.created_at), 400) + CELL_PADDING_WIDE + 2, 132, 220),
    operation: clamp(widestOperation + BADGE_BOX + CELL_PADDING_NARROW + 6, 96, 300),
    table: clamp(measure(logs.map(log => log.table_name), 400) + CELL_PADDING_WIDE + 2, 104, 340),
  };
}

export function LogsPanel({ logSettings, logsBusy, operationLogs, runEnforceLogLimits, saveLogSettings, setLogSettings }) {
  const logs = React.useMemo(() => operationLogs.slice(0, 100), [operationLogs]);
  const widths = React.useMemo(() => measureAuditWidths(logs), [logs]);
  const columns = React.useMemo(() => [
    { id: 'time', role: 'datetime', width: widths.time, minWidth: 132, maxWidth: 220 },
    { id: 'operation', role: 'type', width: widths.operation, minWidth: 96, maxWidth: 300 },
    { id: 'table', role: 'identifier', width: widths.table, minWidth: 104, maxWidth: 340 },
    { id: 'trace', role: 'content', grow: 1, minWidth: 160, verticalAlign: 'middle' },
  ], [widths]);
  const intervalDisabled = !logSettings.autoCleanup;

  return (
    <div className="grid min-w-0 grid-cols-1 items-start gap-3 cq-lg:grid-cols-[minmax(0,24rem)_minmax(0,1fr)]">
      {/* 左列:保留策略,随页面自由滚动 */}
      <div className="flex min-w-0 flex-col gap-3 cq-lg:self-start">
        <SectionCard
          className="min-w-0"
          title="审计与保留"
          icon={<FileText className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          actions={
            <Switch
              label="自动执行保留限制"
              checked={logSettings.autoCleanup}
              onCheckedChange={(checked) => setLogSettings((prev) => ({ ...prev, autoCleanup: checked }))}
            />
          }
        >
          <div className="grid grid-cols-1 gap-x-4 gap-y-4 p-4 cq-sm:grid-cols-2 cq-sm:p-5">
            <Input size="sm" label="保留天数" type="number" min="0" value={logSettings.days} onChange={(e) => setLogSettings((prev) => ({ ...prev, days: Math.max(0, toInt(e.target.value, 0)) }))} />
            <Input size="sm" label="单表最大条数" type="number" min="0" value={logSettings.count} onChange={(e) => setLogSettings((prev) => ({ ...prev, count: Math.max(0, toInt(e.target.value, 0)) }))} />
            <Input size="sm" label="数据库最大 MB" type="number" min="0" value={logSettings.dbSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, dbSizeMB: Math.max(0, toInt(e.target.value, 0)) }))} />
            <Input size="sm" label="app.log 最大 MB" type="number" min="1" value={logSettings.logFileSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, logFileSizeMB: Math.max(1, toInt(e.target.value, 10)) }))} />
            <div className="cq-sm:col-span-2">
              <Input
                size="sm"
                label="执行间隔(小时)"
                type="number"
                min="1"
                value={logSettings.autoCleanupHours}
                onChange={(e) => setLogSettings((prev) => ({ ...prev, autoCleanupHours: Math.max(1, toInt(e.target.value, 24)) }))}
                disabled={intervalDisabled}
                description={intervalDisabled ? '未开启自动执行时不生效' : undefined}
              />
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2 border-t border-kumo-line px-4 py-3 cq-sm:px-5">
            <Button size="sm" variant="secondary" onClick={saveLogSettings} loading={logsBusy} icon={<Save className="h-4 w-4" />}>保存策略</Button>
            <Button size="sm" onClick={runEnforceLogLimits} loading={logsBusy} icon={<Trash className="h-4 w-4" />}>执行保留限制</Button>
          </div>
        </SectionCard>
      </div>

      {/* 右列:审计记录,桌面吸顶全高、列表内部滚动。吸顶偏移要让开顶栏(58px)与吸在其下沿的 Tab 行(58px)。 */}
      <section className="flex min-w-0 flex-col gap-2 cq-lg:sticky cq-lg:top-[132px] cq-lg:h-[calc(100dvh-144px)]">
        <SectionCard
          className="min-h-0 min-w-0 cq-lg:flex-1"
          title="审计记录"
          description="最近 100 条记录"
          icon={<Database className="h-4 w-4 text-brand" />}
          bodyPadding="none"
          bodyClassName="min-h-0 cq-lg:flex-1"
        >
          {/* 内部滚动只在桌面(cq-lg 右列固定高度)启用:移动端若也套 overflow-auto +
              overscroll-contain,手指落在列表上会被内层滚动容器吃掉,页面滚不动。 */}
          {logs.length ? (
            <div className="min-w-0 scrollbar-thin cq-lg:min-h-0 cq-lg:flex-1 cq-lg:overflow-auto cq-lg:overscroll-contain">
              <MobileCardList className="p-2">
                {logs.map((log) => (
                  <MobileCard key={log.id}>
                    <div className="flex min-w-0 items-start gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-mono text-[11px] text-kumo-subtle" title={log.created_at}>{log.created_at || '—'}</div>
                        <div className="mt-1 flex min-w-0 items-center gap-1.5">
                          <Badge variant="outline" className="shrink-0">{log.operation_type}</Badge>
                          <span className="min-w-0 truncate font-mono text-xs text-kumo-strong" title={log.table_name}>{log.table_name}</span>
                        </div>
                      </div>
                    </div>
                    <MobileCardFields className="grid-cols-1">
                      <MobileCardField label="Trace">
                        <span className="font-mono text-[11px]">{log.trace_id || '-'}</span>
                      </MobileCardField>
                    </MobileCardFields>
                  </MobileCard>
                ))}
              </MobileCardList>
              <div className="hidden min-w-0 overflow-x-auto overscroll-x-contain scrollbar-thin cq-md:block">
                <AppTable tableId="audit-logs" columns={columns} className="w-full text-xs">
                  <Table.Header sticky variant="compact">
                    <Table.Row className="h-8">
                      <Table.Head className="!px-2.5 !py-1.5">时间</Table.Head>
                      <Table.Head className="!px-2 !py-1.5 text-center">操作</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5">对象</Table.Head>
                      <Table.Head className="!px-2.5 !py-1.5">Trace</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {logs.map((log) => (
                      <Table.Row key={log.id} className="h-10">
                        <Table.Cell className="!px-2.5 !py-1.5 whitespace-nowrap font-mono text-xs text-kumo-subtle" title={log.created_at}>{log.created_at || '—'}</Table.Cell>
                        <Table.Cell className="!px-2 !py-1.5 text-center"><Badge variant="outline">{log.operation_type}</Badge></Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5"><span className="block truncate font-mono text-xs text-kumo-strong" title={log.table_name}>{log.table_name}</span></Table.Cell>
                        <Table.Cell className="!px-2.5 !py-1.5"><span className="block truncate font-mono text-[11px] text-kumo-subtle" title={log.trace_id || ''}>{log.trace_id || '-'}</span></Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </div>
            </div>
          ) : (
            <div className="p-4">
              <EmptyState card={false} title="暂无审计记录" description="执行敏感操作后,记录会汇总在这里。" />
            </div>
          )}
        </SectionCard>
      </section>
    </div>
  );
}
