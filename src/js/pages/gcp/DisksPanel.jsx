import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppTable, DataTableFrame, EmptyState, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, HardDrive } from '../../components/Icons.jsx';
import { DISK_TABLE_COLUMNS } from './constants.js';
import { formatGb, getGcpStatusTone } from './utils.jsx';
import { DiskActions } from './ActionsPanels.jsx';

export default function DisksPanel({ scopeReady, loadingProjectScope, disks, onOpenDetail, onResize, onSnapshot, onDelete }) {
  return (
    <SectionCard
      title="磁盘"
      icon={<HardDrive className="h-4 w-4" />}
      bodyPadding="none"
      bodyClassName="flex min-h-0 flex-1 flex-col"
    >
      {!scopeReady ? (
        <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-64" />
      ) : loadingProjectScope ? (
        <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
          <AppTable tableId="gcp-disks-loading" columns={DISK_TABLE_COLUMNS}>
            {[0, 1, 2].map((row) => (
              <Table.Row key={row}>
                {DISK_TABLE_COLUMNS.map((col) => (
                  <Table.Cell key={col.id}><SkeletonLine className="h-4" /></Table.Cell>
                ))}
              </Table.Row>
            ))}
          </AppTable>
        </DataTableFrame>
      ) : disks.length === 0 ? (
        <EmptyState card={false} icon={HardDrive} title="暂无磁盘" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
          <AppTable tableId="gcp-disks" columns={DISK_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>可用区</Table.Head>
                <Table.Head>类型</Table.Head>
                <Table.Head>大小</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {disks.map((disk) => (
                <Table.Row key={disk.id || disk.name}>
                  <Table.Cell>
                    <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('disk', disk)} title={`${disk.name} · 点击查看详情`}>{disk.name}</Button>
                  </Table.Cell>
                  <Table.Cell className="truncate text-sm text-kumo-strong" title={disk.zone}>{disk.zone || '-'}</Table.Cell>
                  <Table.Cell className="truncate text-sm text-kumo-strong" title={disk.type}>{disk.type || '-'}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap">{formatGb(disk.sizeGb)}</Table.Cell>
                  <Table.Cell><StatusBadge tone={getGcpStatusTone(disk.status)}>{disk.status || '-'}</StatusBadge></Table.Cell>
                  <Table.Cell><DiskActions disk={disk} onResize={onResize} onSnapshot={onSnapshot} onDelete={onDelete} /></Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
