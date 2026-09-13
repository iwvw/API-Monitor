import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { LayerCard } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { NODE_COLUMNS } from './constants.js';

export default function NodesSkeleton() {
  return (
    <div className="grid gap-3" aria-busy="true" aria-label="正在加载节点">
      <LayerCard className="flex flex-col overflow-hidden p-0 shadow-none">
        <LayerCard.Secondary className={sectionCardHeaderClass}>
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <SkeletonLine className="h-4 w-20" />
            <SkeletonLine className="h-3 w-72 max-w-[42vw]" />
          </div>
          <div className="hidden shrink-0 items-center gap-2 cq-sm:flex">
            <SkeletonLine className="h-8 w-24" />
            <SkeletonLine className="h-8 w-24" />
            <SkeletonLine className="h-8 w-28" />
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-4">
          <div className="grid gap-4 cq-lg:grid-cols-4">
            <div className="space-y-2">
              <SkeletonLine className="h-3 w-12" />
              <SkeletonLine className="h-8 w-full" />
            </div>
            <div className="space-y-2 cq-lg:col-span-2">
              <SkeletonLine className="h-3 w-20" />
              <SkeletonLine className="h-8 w-full" />
            </div>
            <div className="space-y-2">
              <SkeletonLine className="h-3 w-24" />
              <SkeletonLine className="h-8 w-full" />
            </div>
            <SkeletonLine className="h-8 w-32" />
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <DataTableFrame>
        <AppTable tableId="subscription-nodes-skeleton" columns={NODE_COLUMNS}>
          <Table.Header>
            <Table.Row>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head>节点名称</Table.Head>
              <Table.Head className="text-center">类型</Table.Head>
              <Table.Head className="text-center">连接</Table.Head>
              <Table.Head className="text-center">主机 / 延迟</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {Array.from({ length: 6 }).map((_, index) => (
              <Table.Row key={index}>
                <Table.Cell><SkeletonLine className="mx-auto h-5 w-9" /></Table.Cell>
                <Table.Cell>
                  <SkeletonLine className="h-4 w-32" />
                </Table.Cell>
                <Table.Cell><SkeletonLine className="h-5 w-16" /></Table.Cell>
                <Table.Cell><SkeletonLine className="h-3 w-44" /></Table.Cell>
                <Table.Cell><SkeletonLine className="h-3 w-16" /></Table.Cell>
                <Table.Cell>
                  <div className="flex gap-1">
                    <SkeletonLine className="h-8 w-8" />
                    <SkeletonLine className="h-8 w-8" />
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </div>
  );
}
