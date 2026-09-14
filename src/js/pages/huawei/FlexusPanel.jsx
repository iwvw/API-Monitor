import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Cloud } from '../../components/Icons.jsx';
import SkeletonLines from './SkeletonLines.jsx';
import { FLEXUS_TABLE_COLUMNS } from './constants.js';
import { getStatusTone } from './utils.js';

export default function FlexusPanel({
  loadingScope,
  selectedAccountId,
  flexusInstances,
  formatExpire,
  formatTraffic,
  openFlexusDetail,
  renderFlexusActions,
}) {
  return (
    <SectionCard title="Flexus L 实例" bodyPadding="none">
      {loadingScope ? (
        <SkeletonLines />
      ) : !selectedAccountId ? (
        <EmptyState card={false} icon={Cloud} title="请选择账号" description="选择华为云账号后展示 Flexus L 实例" className="min-h-64" />
      ) : flexusInstances.length === 0 ? (
        <EmptyState card={false} icon={Cloud} title="暂无 Flexus L 实例" description="该账号下没有 Flexus L 实例" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" >
          <AppTable tableId="huawei-flexus" columns={FLEXUS_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>规格</Table.Head>
                <Table.Head>到期时间</Table.Head>
                <Table.Head>流量</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {flexusInstances.map((instance) => (
                <Table.Row key={instance.id}>
                  <Table.Cell>
                    <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={`${instance.name} · 点击查看详情`} onClick={() => openFlexusDetail(instance)}>{instance.name || '-'}</Button>
                  </Table.Cell>
                  <Table.Cell><StatusBadge tone={getStatusTone(instance.serverStatus)}>{instance.serverStatus || '-'}</StatusBadge></Table.Cell>
                  <Table.Cell>
                    <div className="min-w-0">
                      <div className="truncate text-sm text-kumo-strong" title={instance.specDescription || instance.specCode}>{instance.specDescription || instance.specCode || '-'}</div>
                      {instance.regionId && <div className="truncate text-[11px] text-kumo-subtle">{instance.regionId}</div>}
                    </div>
                  </Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{formatExpire(instance.expireAt)}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-xs">{formatTraffic(instance)}</Table.Cell>
                  <Table.Cell>{renderFlexusActions(instance)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
