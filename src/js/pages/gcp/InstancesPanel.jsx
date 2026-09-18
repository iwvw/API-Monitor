import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { InlineCopyText } from '@cloudflare/kumo/components/inline-copy-text';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  ResponsiveSearchInput,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Cloud, Plus, Server } from '../../components/Icons.jsx';
import { INSTANCE_TABLE_COLUMNS, stateOptions } from './constants.js';
import { formatDate, formatMemoryGb, getGcpStatusTone } from './utils.jsx';
import { InstanceActions } from './ActionsPanels.jsx';

export default function InstancesPanel({
  scopeReady,
  loadingProjectScope,
  filteredInstances,
  query,
  onQueryChange,
  stateFilter,
  onStateFilterChange,
  onCreateInstance,
  onOpenDetail,
  onCopy,
  onRunAction,
}) {
  return (
    <div className="flex flex-col gap-3">
      <SectionCard
        title="实例"
        icon={<Server className="h-4 w-4" />}
        actions={scopeReady && (
          <>
            <ResponsiveSearchInput value={query} onChange={onQueryChange} placeholder="搜索名称 / IP / 机型" className="w-56" />
            <Select alignItemWithTrigger size="sm" aria-label="状态筛选" value={stateFilter} onValueChange={onStateFilterChange} className="w-32" items={stateOptions} />
            <Button type="button" size="sm" variant="primary" onClick={onCreateInstance}>
              <Plus className="h-4 w-4" />创建实例
            </Button>
          </>
        )}
        bodyPadding="none"
        bodyClassName="flex min-h-0 flex-1 flex-col"
      >
        {loadingProjectScope ? (
          <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
            <AppTable tableId="gcp-instances-loading" columns={INSTANCE_TABLE_COLUMNS}>
              {[0, 1, 2].map((row) => (
                <Table.Row key={row}>
                  {INSTANCE_TABLE_COLUMNS.map((col) => (
                    <Table.Cell key={col.id}><SkeletonLine className="h-4" /></Table.Cell>
                  ))}
                </Table.Row>
              ))}
            </AppTable>
          </DataTableFrame>
        ) : !scopeReady ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号与项目" description="选择 GCP 账号和项目后查看实例列表" className="min-h-64" />
        ) : filteredInstances.length === 0 ? (
          <EmptyState card={false} icon={Server} title="暂无实例" description="该项目下没有实例" className="min-h-64" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
            <AppTable tableId="gcp-instances" columns={INSTANCE_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>状态</Table.Head>
                  <Table.Head>公网 IP</Table.Head>
                  <Table.Head>规格</Table.Head>
                  <Table.Head>可用区</Table.Head>
                  <Table.Head>创建时间</Table.Head>
                  <Table.Head>操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {filteredInstances.map((instance) => (
                  <Table.Row key={instance.id || instance.name}>
                    <Table.Cell>
                      <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('instance', instance)} title={`${instance.name || '-'} · 点击查看详情`}>{instance.name || '-'}</Button>
                    </Table.Cell>
                    <Table.Cell><StatusBadge tone={getGcpStatusTone(instance.state)}>{instance.state || '-'}</StatusBadge></Table.Cell>
                    <Table.Cell>
                      <InlineCopyText
                        value={instance.publicIp || instance.privateIp || ''}
                        variant="mono-secondary"
                        size="lg"
                        truncate
                        className="max-w-full"
                        labels={{ copyAction: '复制 IP', copied: 'IP 已复制' }}
                      >
                        {instance.publicIp || instance.privateIp || '-'}
                      </InlineCopyText>
                    </Table.Cell>
                    <Table.Cell>
                      <div className="flex min-w-0 items-center gap-1.5">
                        <span className="truncate text-sm text-kumo-strong" title={instance.machineType}>{instance.machineType || '-'}</span>
                        {instance.guestCpus > 0 && (
                          <span className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle">{instance.guestCpus} vCPU · {formatMemoryGb(instance.memoryMb)}</span>
                        )}
                      </div>
                    </Table.Cell>
                    <Table.Cell className="truncate text-sm text-kumo-strong" title={instance.zone}>{instance.zone || '-'}</Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{formatDate(instance.creationTimestamp)}</Table.Cell>
                    <Table.Cell><InstanceActions instance={instance} onRunAction={onRunAction} /></Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
    </div>
  );
}
