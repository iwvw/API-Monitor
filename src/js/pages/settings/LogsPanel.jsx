import React from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Database, FileText, Save, Trash } from '../../components/Icons.jsx';
import { toInt } from './utils.js';

export function LogsPanel({ logSettings, logsBusy, operationLogs, runEnforceLogLimits, saveLogSettings, setLogSettings }) {
  return (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]">
          <SectionCard
            className="top-[calc(var(--app-header-height)+0.5rem)] z-20 min-w-0 cq-xl:sticky"
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
            <div className="p-5">
              <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2 cq-md:grid-cols-3">
                <Input size="sm" label="保留天数" type="number" min="0" value={logSettings.days} onChange={(e) => setLogSettings((prev) => ({ ...prev, days: Math.max(0, toInt(e.target.value, 0)) }))} />
                <Input size="sm" label="单表最大条数" type="number" min="0" value={logSettings.count} onChange={(e) => setLogSettings((prev) => ({ ...prev, count: Math.max(0, toInt(e.target.value, 0)) }))} />
                <Input size="sm" label="数据库最大 MB" type="number" min="0" value={logSettings.dbSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, dbSizeMB: Math.max(0, toInt(e.target.value, 0)) }))} />
                <Input size="sm" label="app.log 最大 MB" type="number" min="1" value={logSettings.logFileSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, logFileSizeMB: Math.max(1, toInt(e.target.value, 10)) }))} />
                <Input size="sm" label="执行间隔（小时）" type="number" min="1" value={logSettings.autoCleanupHours} onChange={(e) => setLogSettings((prev) => ({ ...prev, autoCleanupHours: Math.max(1, toInt(e.target.value, 24)) }))} disabled={!logSettings.autoCleanup} />
              </div>

                <div className="mt-4 flex justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={saveLogSettings} loading={logsBusy} icon={<Save className="h-4 w-4" />}>保存策略</Button>
                  <Button size="sm" onClick={runEnforceLogLimits} loading={logsBusy} icon={<Trash className="h-4 w-4" />}>执行保留限制</Button>
                </div>
              </div>
            </SectionCard>

          <div className="min-w-0">
            <SectionCard
              className="min-w-0"
              title="审计记录"
              description="最近 100 条记录"
              icon={<Database className="h-4 w-4 text-brand" />}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
              <Table layout="fixed" className="min-w-[700px]">
                <colgroup>
                  <col className="w-[170px]" />
                  <col className="w-[220px]" />
                  <col className="w-[130px]" />
                  <col />
                </colgroup>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>时间</Table.Head>
                    <Table.Head>操作</Table.Head>
                    <Table.Head>对象</Table.Head>
                    <Table.Head>Trace</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {operationLogs.slice(0, 100).map((log) => (
                    <Table.Row key={log.id}>
                      <Table.Cell className="font-mono text-xs">{log.created_at}</Table.Cell>
                      <Table.Cell><Badge variant="outline">{log.operation_type}</Badge></Table.Cell>
                      <Table.Cell className="font-mono text-xs">{log.table_name}</Table.Cell>
                      <Table.Cell className="truncate font-mono text-xs text-kumo-subtle">{log.trace_id || '-'}</Table.Cell>
                    </Table.Row>
                  ))}
                  {operationLogs.length === 0 && (
                    <Table.Row>
                      <Table.Cell colSpan={4} className="p-8 text-center text-kumo-subtle">暂无审计记录</Table.Cell>
                    </Table.Row>
                  )}
                </Table.Body>
              </Table>
            </SectionCard>
          </div>
        </div>
  );
}
