import React from 'react';
import { Empty } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { AppTable, DataTableFrame, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Box, Edit, HardDrive, Plug, Plus, RefreshCw, Trash } from '../../components/Icons.jsx';
import { ASSET_COLUMNS, sourceModuleLabel } from './constants.js';
import { statusMeta, formatExpireAt, formatDaysLeft, daysTone, formatCost } from './utils.js';

export default function AssetTable({
  assets,
  loading,
  category,
  onEdit,
  onDelete,
  onCreate,
  onOpenAsset,
  onRefresh,
  refreshingId,
}) {
  if (!loading && assets.length === 0) {
    const isPhysical = category === 'physical';
    return (
      <Empty
        size="base"
        className="rounded-none border-0 bg-transparent"
        icon={isPhysical ? <HardDrive className="h-8 w-8 text-kumo-secondary" /> : <Box className="h-8 w-8 text-kumo-secondary" />}
        title={isPhysical ? '还没有实体资产' : '还没有虚拟资产'}
        description={
          isPhysical
            ? '登记服务器、网络设备、存储与终端，统一掌握它们的到期情况'
            : '登记域名、证书、订阅、许可证与密钥，统一掌握它们的到期与成本'
        }
      >
        <Button size="sm" variant="primary" icon={<Plus className="h-3.5 w-3.5" />} onClick={onCreate}>登记资产</Button>
      </Empty>
    );
  }

  const ColumnIcon = category === 'physical' ? HardDrive : Box;

  return (
    <DataTableFrame variant="embedded">
      <AppTable tableId={`assets-${category}`} columns={ASSET_COLUMNS}>
        <Table.Header sticky variant="compact">
          <Table.Row>
            <Table.Head>名称</Table.Head>
            <Table.Head>提供方</Table.Head>
            <Table.Head className="text-center">状态</Table.Head>
            <Table.Head>到期日</Table.Head>
            <Table.Head className="text-right">剩余</Table.Head>
            <Table.Head className="text-right">成本</Table.Head>
            <Table.Head>标签</Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {assets.map(asset => {
            const meta = statusMeta(asset.derived_status);
            return (
              <Table.Row
                key={asset.id}
                className="cursor-pointer hover:bg-kumo-recessed/25"
                onDoubleClick={() => onOpenAsset?.(asset)}
              >
                <Table.Cell>
                  <div className="flex min-w-0 items-center gap-2">
                    <ColumnIcon className="h-4 w-4 shrink-0 text-kumo-info" />
                    <div className="min-w-0">
                      <div className="truncate text-sm font-semibold text-kumo-strong" title={asset.name}>{asset.name}</div>
                      {asset.origin === 'linked' && (
                        <span className="mt-0.5 inline-flex items-center gap-1 text-[11px] text-kumo-subtle">
                          <Plug className="h-3 w-3 shrink-0" />
                          <span className="truncate" title={asset.source_module}>
                            纳管自 {sourceModuleLabel(asset.source_module)}
                          </span>
                        </span>
                      )}
                    </div>
                  </div>
                </Table.Cell>
                <Table.Cell className="truncate text-kumo-subtle" title={asset.provider}>{asset.provider || '--'}</Table.Cell>
                <Table.Cell className="text-center">
                  <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
                </Table.Cell>
                <Table.Cell className="whitespace-nowrap text-kumo-subtle">{formatExpireAt(asset.expire_at)}</Table.Cell>
                <Table.Cell className="whitespace-nowrap text-right">
                  <StatusBadge tone={daysTone(asset)}>{formatDaysLeft(asset)}</StatusBadge>
                </Table.Cell>
                <Table.Cell className="truncate whitespace-nowrap text-right text-kumo-subtle" title={formatCost(asset)}>{formatCost(asset)}</Table.Cell>
                <Table.Cell>
                  <span className="flex min-w-0 flex-wrap gap-1">
                    {(asset.tags || []).slice(0, 3).map(tag => (
                      <span key={tag} className="rounded border border-kumo-line bg-kumo-recessed px-1.5 py-0.5 text-[11px] text-kumo-subtle">{tag}</span>
                    ))}
                    {(asset.tags || []).length > 3 && (
                      <span className="text-[11px] text-kumo-subtle">+{asset.tags.length - 3}</span>
                    )}
                  </span>
                </Table.Cell>
                <Table.Cell className="text-right">
                  <div className="flex justify-end gap-1 whitespace-nowrap">
                    {asset.origin === 'linked' && (
                      <Button
                        size="sm"
                        shape="square"
                        variant="secondary"
                        icon={<RefreshCw className="h-3.5 w-3.5" />}
                        aria-label="刷新来源快照"
                        title="刷新来源快照"
                        loading={refreshingId === asset.id}
                        onClick={() => onRefresh?.(asset)}
                      />
                    )}
                    <Button
                      size="sm"
                      shape="square"
                      variant="secondary"
                      icon={<Edit className="h-3.5 w-3.5" />}
                      aria-label="编辑资产"
                      title="编辑资产"
                      onClick={() => onEdit?.(asset)}
                    />
                    <Button
                      size="sm"
                      shape="square"
                      variant="secondary-destructive"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      aria-label="删除资产"
                      title="删除资产"
                      onClick={() => onDelete?.(asset)}
                    />
                  </div>
                </Table.Cell>
              </Table.Row>
            );
          })}
        </Table.Body>
      </AppTable>
    </DataTableFrame>
  );
}
