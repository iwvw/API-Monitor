import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { AppTable, DataTableFrame, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Copy } from '../../components/Icons.jsx';
import { columnLabel, getOciStatusTone, resourceColumnSpecs } from './utils.js';
import TableSkeletonRows from './TableSkeletonRows.jsx';

export default function ResourceList({ title, icon, items, columns, onCopy, embedded = false, loading = false, renderActions = null }) {
  const table = loading ? (
    <DataTableFrame
      variant="embedded"
      density="dense"
      className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin [&_td:first-child]:!pr-1.5 [&_td:last-child]:!pl-1.5"
    >
      <AppTable tableId={`oracle-${title}-loading`} columns={resourceColumnSpecs(columns, renderActions)}>
        <Table.Header variant="compact">
          <Table.Row>
            {columns.map((column) => <Table.Head key={column}>{columnLabel(column)}</Table.Head>)}
            {renderActions ? <Table.Head className="app-table-action">操作</Table.Head> : null}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          <TableSkeletonRows columns={columns.length + (renderActions ? 1 : 0)} rows={5} />
        </Table.Body>
      </AppTable>
    </DataTableFrame>
  ) : items.length === 0 ? (
    <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-kumo-subtle">暂无数据</div>
  ) : (
    <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
      <AppTable tableId={`oracle-${title}`} columns={resourceColumnSpecs(columns, renderActions)}>
        <Table.Header variant="compact">
          <Table.Row>
            {columns.map((column) => <Table.Head key={column}>{columnLabel(column)}</Table.Head>)}
            {renderActions ? <Table.Head className="app-table-action">操作</Table.Head> : null}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {items.map((item, index) => (
            <Table.Row key={item.id || item.attachmentId || item.volumeId || index}>
              {columns.map((column) => (
                <Table.Cell key={column}>
                  <div className="flex items-center gap-2">
                    {column === 'state' ? (
                      <StatusBadge tone={getOciStatusTone(item[column])}>{String(item[column] || '-')}</StatusBadge>
                    ) : (
                      <div className="min-w-0 truncate text-sm text-kumo-strong" title={String(item[column] || '-')}>
                        {String(item[column] || '-')}
                      </div>
                    )}
                    {item[column] && ['connectionString', 'volumeId', 'attachmentId', 'subnetId'].includes(column) && (
                      <Button type="button" size="sm" shape="square" variant="ghost" onClick={() => onCopy(item[column])} aria-label={`复制${columnLabel(column)}`} title="复制" icon={<Copy className="h-3.5 w-3.5" />} />
                    )}
                  </div>
                </Table.Cell>
              ))}
              {renderActions ? <Table.Cell className="text-right">{renderActions(item)}</Table.Cell> : null}
            </Table.Row>
          ))}
        </Table.Body>
      </AppTable>
    </DataTableFrame>
  );
  if (embedded) return table;
  return (
    <SectionCard
      title={title}
      icon={icon}
      className="min-h-0 flex-1"
      bodyPadding="none"
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
    >
      {table}
    </SectionCard>
  );
}
