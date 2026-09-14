import React from 'react';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { AppTable, DataTableFrame, ResponsiveSearchInput, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Server } from '../../components/Icons.jsx';
import { INSTANCE_TABLE_COLUMNS, stateOptions } from './constants.js';
import { formatInstanceMetric, formatOciDate, getOciStatusTone } from './utils.js';
import TableSkeletonRows from './TableSkeletonRows.jsx';

export default function InstanceListPanel({ filteredInstances, loadingInstances, query, onQueryChange, stateFilter, onStateFilterChange, selectedInstanceId, onSelectInstance }) {
  return (
    <SectionCard
      title="实例列表"
      description={`${filteredInstances.length} 台实例`}
      className="min-h-0"
      bodyPadding="none"
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
      icon={<Server className="h-4 w-4 text-brand" />}
      actions={(
        <>
          <ResponsiveSearchInput
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder="搜索名称、ID、IP、shape"
            ariaLabel="搜索 Oracle 实例"
            className="w-40 cq-sm:w-52"
          />
          <Select alignItemWithTrigger
            aria-label="实例状态筛选"
            size="sm"
            className="w-32 cq-sm:w-36"
            value={stateFilter}
            onValueChange={onStateFilterChange}
            items={stateOptions}
          />
        </>
      )}
    >
      {loadingInstances ? (
        <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
          <AppTable tableId="oracle-instances-loading" columns={INSTANCE_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>公共 IP</Table.Head>
                <Table.Head>配置</Table.Head>
                <Table.Head>OCPU</Table.Head>
                <Table.Head>内存(GB)</Table.Head>
                <Table.Head>创建时间</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              <TableSkeletonRows columns={7} rows={6} />
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      ) : filteredInstances.length === 0 ? (
        <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-kumo-subtle">暂无实例</div>
      ) : (
        <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
          <AppTable tableId="oracle-instances" columns={INSTANCE_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>公共 IP</Table.Head>
                <Table.Head>配置</Table.Head>
                <Table.Head>OCPU</Table.Head>
                <Table.Head>内存(GB)</Table.Head>
                <Table.Head>创建时间</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filteredInstances.map((instance) => (
                <Table.Row
                  key={instance.id}
                  variant={selectedInstanceId === instance.id ? 'selected' : 'default'}
                  className="cursor-pointer"
                  onClick={() => onSelectInstance(instance.id)}
                >
                  <Table.Cell>
                    <div className="truncate text-sm font-semibold text-kumo-strong" title={instance.name || '-'}>
                      {instance.name || '-'}
                    </div>
                  </Table.Cell>
                  <Table.Cell>
                    <StatusBadge tone={getOciStatusTone(instance.state)}>{instance.state || '-'}</StatusBadge>
                  </Table.Cell>
                  <Table.Cell className="font-mono text-xs">{instance.primaryPublicIp || '-'}</Table.Cell>
                  <Table.Cell>
                    <div className="truncate text-sm text-kumo-strong" title={instance.shape || '-'}>
                      {instance.shape || '-'}
                    </div>
                  </Table.Cell>
                  <Table.Cell>{formatInstanceMetric(instance.ocpuCount)}</Table.Cell>
                  <Table.Cell>{formatInstanceMetric(instance.memoryGb)}</Table.Cell>
                  <Table.Cell className="text-xs text-kumo-subtle">{formatOciDate(instance.timeCreated)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
