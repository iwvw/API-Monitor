import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { AppCard, AppTable, DataTableFrame, EmptyState, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Plus, RotateCw, Shield } from '../../components/Icons.jsx';

const MAINTENANCE_WINDOW_COLUMNS = [
  { id: 'title', role: 'primary', minWidth: 208, grow: 1 },
  { id: 'status', role: 'status' },
  { id: 'window', role: 'content', minWidth: 224, grow: 1, verticalAlign: 'middle' },
  { id: 'strategy', role: 'meta' },
  { id: 'timezone', role: 'meta' },
  { id: 'updatedAt', role: 'datetime' },
];

function MaintenancePanel({
  maintenanceWindows,
  selectedMonitorIds,
  metaLoading,
  formatDateTime,
  onRefresh,
  onCreateQuick,
}) {
  return (
    <SectionCard
      title="维护窗口"
      description="维护期内抑制告警"
      icon={<Shield className="h-4 w-4 text-brand" />}
      actions={(
        <>
          <Button
            size="sm"
            variant="secondary"
            icon={<RotateCw className="w-3.5 h-3.5" />}
            onClick={onRefresh}
            disabled={metaLoading}
          >
            刷新
          </Button>
          <Button
            size="sm"
            variant="primary"
            icon={<Plus className="w-3.5 h-3.5" />}
            onClick={onCreateQuick}
            disabled={metaLoading}
          >
            创建 1 小时窗口
          </Button>
        </>
      )}
      bodyPadding="lg"
      bodyClassName="space-y-5"
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-kumo-subtle">
        <span>已选 <span className="font-semibold text-kumo-strong">{selectedMonitorIds.length}</span> 个监测目标，可直接用于快速维护窗口。</span>
        <span>当前共 <span className="font-semibold text-kumo-strong">{maintenanceWindows.length}</span> 个维护窗口。</span>
        <span>
          当前生效{' '}
          <span className={`font-semibold ${maintenanceWindows.filter((item) => {
            const start = item.startAt ? new Date(item.startAt).getTime() : null;
            const end = item.endAt ? new Date(item.endAt).getTime() : null;
            const now = Date.now();
            return item.active && Number.isFinite(start) && Number.isFinite(end) && start <= now && end >= now;
          }).length > 0 ? 'text-kumo-warning' : 'text-kumo-strong'}`}>
            {maintenanceWindows.filter((item) => {
              const start = item.startAt ? new Date(item.startAt).getTime() : null;
              const end = item.endAt ? new Date(item.endAt).getTime() : null;
              const now = Date.now();
              return item.active && Number.isFinite(start) && Number.isFinite(end) && start <= now && end >= now;
            }).length}
          </span>{' '}
          个。
        </span>
      </div>

      {metaLoading ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, index) => <SkeletonLine key={index} className="h-14 w-full" />)}
        </div>
      ) : maintenanceWindows.length === 0 ? (
        <EmptyState
          icon={Shield}
          title="暂无维护窗口"
          description="维护期内抑制告警"
          action={(
            <Button size="sm" variant="primary" icon={<Plus className="w-3.5 h-3.5" />} onClick={onCreateQuick}>
              创建 1 小时窗口
            </Button>
          )}
        />
      ) : (
        <AppCard padding="none" className="overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/20 px-4 py-3">
            <div>
              <div className="text-sm font-semibold text-kumo-strong">维护窗口列表</div>
              <div className="mt-1 text-xs text-kumo-subtle">查看维护计划。</div>
            </div>
            <div className="text-xs text-kumo-subtle">
              共 <span className="font-semibold text-kumo-strong">{maintenanceWindows.length}</span> 条记录
            </div>
          </div>

          <DataTableFrame variant="embedded">
            <AppTable tableId="maintenance-windows" columns={MAINTENANCE_WINDOW_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>标题</Table.Head>
                  <Table.Head className="text-center">状态</Table.Head>
                  <Table.Head>时间窗口</Table.Head>
                  <Table.Head>策略</Table.Head>
                  <Table.Head>时区</Table.Head>
                  <Table.Head>更新时间</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {maintenanceWindows.map((item) => {
                  const start = item.startAt ? new Date(item.startAt).getTime() : null;
                  const end = item.endAt ? new Date(item.endAt).getTime() : null;
                  const now = Date.now();
                  const isActiveNow = item.active && Number.isFinite(start) && Number.isFinite(end) && start <= now && end >= now;
                  const isUpcoming = item.active && Number.isFinite(start) && start > now;
                  return (
                    <Table.Row key={item.id}>
                      <Table.Cell className="font-semibold text-kumo-strong truncate">
                        {item.title}
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        {isActiveNow ? (
                          <StatusBadge tone="warning">生效中</StatusBadge>
                        ) : isUpcoming ? (
                          <StatusBadge tone="info">待开始</StatusBadge>
                        ) : item.active ? (
                          <StatusBadge tone="success">启用</StatusBadge>
                        ) : (
                          <StatusBadge tone="neutral">停用</StatusBadge>
                        )}
                      </Table.Cell>
                      <Table.Cell className="font-mono text-xs text-kumo-subtle truncate">
                        {formatDateTime(item.startAt)} - {formatDateTime(item.endAt)}
                      </Table.Cell>
                      <Table.Cell className="text-xs">
                        {item.strategy || 'manual'}
                      </Table.Cell>
                      <Table.Cell className="text-xs">
                        {item.timezone || 'UTC'}
                      </Table.Cell>
                      <Table.Cell className="text-xs text-kumo-subtle">
                        {formatDateTime(item.updatedAt || item.createdAt)}
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        </AppCard>
      )}
    </SectionCard>
  );
}

export default MaintenancePanel;
