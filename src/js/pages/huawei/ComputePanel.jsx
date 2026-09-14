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
import { Server } from '../../components/Icons.jsx';
import SkeletonLines from './SkeletonLines.jsx';
import { INSTANCE_TABLE_COLUMNS } from './constants.js';
import { getStatusTone } from './utils.js';

export default function ComputePanel({ loadingScope, scopeReady, instances, renderInstanceActions }) {
  return (
    <SectionCard title="ECS 实例" bodyPadding="none">
      {loadingScope ? (
        <SkeletonLines />
      ) : !scopeReady ? (
        <EmptyState card={false} icon={Server} title="请选择账号与项目" description="选择华为云账号和区域项目后展示实例列表" className="min-h-64" />
      ) : instances.length === 0 ? (
        <EmptyState card={false} icon={Server} title="暂无实例" description="该项目下没有 ECS 实例" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" >
          <AppTable tableId="huawei-instances" columns={INSTANCE_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>公网 IP</Table.Head>
                <Table.Head>规格</Table.Head>
                <Table.Head>区域</Table.Head>
                <Table.Head>创建时间</Table.Head>
                <Table.Head>操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {instances.map((instance) => (
                <Table.Row key={instance.id}>
                  <Table.Cell>
                    <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={instance.name}>{instance.name || '-'}</Button>
                  </Table.Cell>
                  <Table.Cell><StatusBadge tone={getStatusTone(instance.status)}>{instance.status || '-'}</StatusBadge></Table.Cell>
                  <Table.Cell><span className="truncate font-mono text-xs" title={instance.publicIp || instance.privateIp}>{instance.publicIp || instance.privateIp || '-'}</span></Table.Cell>
                  <Table.Cell>
                    <span className="truncate text-sm text-kumo-strong" title={instance.flavorName}>{instance.flavorName || '-'}</span>
                    {instance.vcpus > 0 && <span className="ml-1.5 shrink-0 whitespace-nowrap text-xs text-kumo-subtle">{instance.vcpus} vCPU</span>}
                  </Table.Cell>
                  <Table.Cell className="truncate text-sm text-kumo-strong" title={instance.region}>{instance.region || '-'}</Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{instance.createdAt || '-'}</Table.Cell>
                  <Table.Cell>{renderInstanceActions(instance)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
