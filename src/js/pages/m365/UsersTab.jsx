import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge, Meter } from '@cloudflare/kumo';
import {
  AppTable,
  cx,
  DataTableFrame,
  EmptyState,
  PageStack,
  ResponsiveSearchInput,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Database, Plus, RefreshCw, Trash, User, Users } from '../../components/Icons.jsx';
import { panelBodyClass, scrollViewportClass, USER_TABLE_COLUMN_WIDTHS } from './constants.js';
import { CardTableSkeleton, SkuGridSkeleton } from './Skeletons.jsx';
import {
  clampPercent,
  formatMetricNumber,
  getAssignedSkuLabels,
  getDisplayText,
  getSkuDisplayLabel,
  getSkuLifecycleText,
} from './utils.js';

export default function UsersTab({
  selectedAccountId,
  skuLoading,
  skus,
  loadSkus,
  userSearch,
  setUserSearch,
  loadUsers,
  openCreateUser,
  usersLoading,
  users,
  skuLabelLookup,
  selectedUserId,
  setSelectedUserId,
  togglingUserId,
  toggleUserEnabled,
  openEditUser,
  deleteUser,
  isArmed,
}) {
  return (
    <PageStack viewport>
      <SectionCard
        className="shrink-0"
        bodyClassName={panelBodyClass}
        title="SKU 库存"
        icon={<Database className="h-4 w-4" />}
        action={
          <Button
            size="sm"
            variant="secondary"
            icon={<RefreshCw className="h-3.5 w-3.5" />}
            onClick={loadSkus}
          >
            刷新
          </Button>
        }
      >
        {!selectedAccountId ? (
          <EmptyState icon={Database} title="请先选择租户" />
        ) : skuLoading ? (
          <SkuGridSkeleton />
        ) : skus.length === 0 ? (
          <EmptyState
            icon={Database}
            title="暂无 SKU 数据"
          />
        ) : (
          <div
            className={cx(scrollViewportClass, 'grid auto-rows-max content-start gap-2.5 pr-1')}
            style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}
          >
            {skus.map(sku => {
              const totalUnits = Number(sku.prepaidUnits?.enabled ?? 0);
              const warningUnits = Number(sku.prepaidUnits?.warning ?? 0);
              const consumedUnits = Number(sku.consumedUnits ?? 0);
              const availableUnits = Math.max(0, totalUnits - consumedUnits);
              const usagePct =
                totalUnits > 0 ? clampPercent((consumedUnits / totalUnits) * 100) : 0;
              const progressTone =
                usagePct >= 90
                  ? '!bg-kumo-danger'
                  : usagePct >= 70
                    ? '!bg-kumo-warning'
                    : '!bg-brand';
              const lifecycleText = getSkuLifecycleText(sku);
              return (
                <div
                  key={sku.skuId}
                  className="rounded-lg border border-kumo-line/70 bg-kumo-base/95 px-3 py-2.5"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0 flex-1">
                      <div
                        className="truncate text-[15px] font-semibold text-kumo-strong"
                        title={getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                      >
                        {getSkuDisplayLabel(sku.skuPartNumber, sku.skuId)}
                      </div>
                      <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-[11px]">
                        <span className="font-semibold text-kumo-strong">
                          {formatMetricNumber(consumedUnits)} / {formatMetricNumber(totalUnits)}
                        </span>
                        <span className="text-kumo-subtle">
                          剩余 {formatMetricNumber(availableUnits)}
                        </span>
                        {warningUnits > 0 ? (
                          <span className="text-kumo-subtle">
                            警告 {formatMetricNumber(warningUnits)}
                          </span>
                        ) : null}
                      </div>
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center justify-end gap-1">
                      {lifecycleText ? (
                        <Badge
                          variant="outline"
                          className="border-kumo-line/70 bg-kumo-recessed/30 !px-2 !py-0.5 !text-[11px] font-medium leading-5 !text-kumo-subtle"
                        >
                          {lifecycleText}
                        </Badge>
                      ) : null}
                      <StatusBadge
                        tone={usagePct >= 90 ? 'danger' : usagePct >= 70 ? 'warning' : 'success'}
                      >
                        {usagePct.toFixed(0)}%
                      </StatusBadge>
                    </div>
                  </div>

                  <Meter
                    label=""
                    value={usagePct}
                    max={100}
                    showValue={false}
                    className="mt-2.5"
                    trackClassName="!h-1.5 bg-kumo-recessed/80"
                    indicatorClassName={progressTone}
                  />
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>

      <SectionCard
        title="用户与许可证"
        icon={<Users className="h-4 w-4" />}
        bodyPadding="none"
        action={
          <div className="flex items-center gap-2">
            <ResponsiveSearchInput
              value={userSearch}
              onChange={event => setUserSearch(event.target.value)}
              onSearch={loadUsers}
              placeholder="搜索显示名或 UPN"
              ariaLabel="搜索用户"
              className="cq-sm:w-56"
            />
            <Button
              size="sm"
              variant="primary"
              icon={<Plus className="h-3.5 w-3.5" />}
              onClick={openCreateUser}
            >
              新增用户
            </Button>
          </div>
        }
      >
        {!selectedAccountId ? (
          <div className="p-4">
            <EmptyState
              icon={Users}
              title="请先选择租户"
            />
          </div>
        ) : usersLoading ? (
          <div className="p-4">
            <CardTableSkeleton rows={7} />
          </div>
        ) : users.length === 0 ? (
          <div className="p-4">
            <EmptyState icon={User} title="暂无用户" description="当前筛选无用户" />
          </div>
        ) : (
          <DataTableFrame
            variant="embedded"
            density="compact"
            className="overflow-auto scrollbar-thin"
          >
            <AppTable
              layout="fixed"
              widths={USER_TABLE_COLUMN_WIDTHS}
              className="w-full text-xs [&_td]:align-middle"
            >
              <colgroup>
                {USER_TABLE_COLUMN_WIDTHS.map((width, index) => (
                  <col key={index} style={{ width }} />
                ))}
              </colgroup>
              <Table.Header sticky variant="compact">
                <Table.Row>
                  <Table.Head className="!px-3 !py-2 text-center">状态</Table.Head>
                  <Table.Head className="!px-3 !py-2">显示名</Table.Head>
                  <Table.Head className="!px-3 !py-2">登录账号</Table.Head>
                  <Table.Head className="!px-3 !py-2">邮箱</Table.Head>
                  <Table.Head className="!px-3 !py-2">许可证</Table.Head>
                  <Table.Head className="app-table-action !px-3 !py-2">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {users.map(user => {
                  const assignedSkuLabels = getAssignedSkuLabels(
                    user.assignedLicenses,
                    skuLabelLookup
                  );
                  const assignedSkuSummary =
                    assignedSkuLabels.length <= 2
                      ? assignedSkuLabels.join('、') || '-'
                      : `${assignedSkuLabels.slice(0, 2).join('、')} +${assignedSkuLabels.length - 2}`;
                  const deleteArmed = isArmed(`m365-user-delete:${user.id}`);
                  return (
                    <Table.Row
                      key={user.id}
                      variant={
                        String(user.id) === String(selectedUserId) ? 'selected' : 'default'
                      }
                      className="h-10 cursor-pointer"
                      onClick={() => setSelectedUserId(String(user.id))}
                    >
                      <Table.Cell className="!px-3 !py-1.5 text-center">
                        <div
                          className="flex justify-center"
                          onClick={event => event.stopPropagation()}
                        >
                          <Switch
                            size="sm"
                            aria-label={`${getDisplayText(user.displayName)}状态开关`}
                            checked={user.accountEnabled !== false}
                            disabled={togglingUserId === String(user.id)}
                            onCheckedChange={checked => toggleUserEnabled(user, checked)}
                          />
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div
                          className="truncate font-medium text-kumo-strong"
                          title={getDisplayText(user.displayName)}
                        >
                          {getDisplayText(user.displayName)}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div className="truncate" title={getDisplayText(user.userPrincipalName)}>
                          {getDisplayText(user.userPrincipalName)}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div className="truncate" title={getDisplayText(user.mail)}>
                          {getDisplayText(user.mail)}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div
                          className="flex min-w-0 items-center gap-2"
                          title={assignedSkuLabels.join('、') || '-'}
                        >
                          <span className="min-w-0 flex-1 truncate">{assignedSkuSummary}</span>
                          {assignedSkuLabels.length > 1 ? (
                          <Badge
                            variant="outline"
                            className="shrink-0 border-kumo-line/70 bg-kumo-recessed/20 !px-2 !py-0.5 !text-[10px] !text-kumo-subtle"
                          >
                            {assignedSkuLabels.length} 项
                          </Badge>
                          ) : null}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="!px-3 !py-1.5">
                        <div
                          className="flex items-center justify-end gap-2 whitespace-nowrap"
                          onClick={event => event.stopPropagation()}
                        >
                          <Button
                            size="sm"
                            variant="secondary"
                            onClick={() => openEditUser(user)}
                          >
                            编辑
                          </Button>
                          <Button
                            size="sm"
                            variant={deleteArmed ? 'destructive' : 'secondary-destructive'}
                            title={deleteArmed ? '确认' : '删除用户'}
                            aria-label={deleteArmed ? '确认' : '删除用户'}
                            icon={<Trash className="h-3.5 w-3.5" />}
                            className={deleteArmed ? 'ring-1 ring-kumo-danger/50' : ''}
                            onClick={() => void deleteUser(user)}
                          >
                            {deleteArmed ? '确认' : '删除'}
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
    </PageStack>
  );
}
