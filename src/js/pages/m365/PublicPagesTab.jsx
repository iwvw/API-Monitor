import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Badge, Tabs } from '@cloudflare/kumo';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import {
  AppCard,
  AppTable,
  cx,
  DataTableFrame,
  EmptyState,
  PageStack,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Copy, Globe, Key, Plus, RefreshCw, Trash, Users } from '../../components/Icons.jsx';
import { formatDateTime } from '../../modules/utils.js';
import { M365_PUBLIC_TABS } from './tabs.jsx';
import {
  REGISTRATION_TABLE_COLUMNS,
  panelBodyClass,
  publicResourceCardActionBarClass,
  publicResourceCardClass,
  publicResourceCardFieldClass,
  publicResourceCardGridClass,
  publicResourceCardHeaderClass,
  scrollViewportClass,
} from './constants.js';
import { DomainListPopover, LicenseListPopover } from './ListPopovers.jsx';
import {
  getRegistrationResultText,
  getRegistrationStatusLabel,
  getRegistrationTone,
  getSkuDisplayLabel,
} from './utils.js';

export default function PublicPagesTab({
  publicTab,
  setPublicTab,
  loadPublicPages,
  loadInviteCodes,
  loadRegistrations,
  openCreatePublicPage,
  openInviteCodeGenerator,
  publicPagesLoading,
  filteredPublicPages,
  accountLookup,
  publicPageSkuLabelLookup,
  skuLabelLookup,
  copyText,
  togglingPublicPageId,
  togglePublicPageEnabled,
  openEditPublicPage,
  deletePublicPage,
  isArmed,
  inviteCodesLoading,
  groupedInviteCodeBatches,
  deleteInviteBatch,
  selectedRegistrationIds,
  deletingRegistrations,
  deleteSelectedRegistrations,
  registrationsLoading,
  filteredRegistrations,
  selectedRegistrationRecords,
  setSelectedRegistrationIds,
  toggleRegistrationSelection,
  setRegistrationDetail,
}) {
  return (
    <PageStack viewport className="min-h-0 flex-1">
      <SectionCard
        className="flex min-h-0 flex-1 flex-col"
        bodyClassName={panelBodyClass}
        title="公开页"
        icon={<Globe className="h-4 w-4" />}
        action={
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="secondary"
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={() => {
                loadPublicPages();
                loadInviteCodes();
                loadRegistrations();
              }}
            >
              刷新
            </Button>
            {publicTab === 'pages' ? (
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="h-3.5 w-3.5" />}
                onClick={openCreatePublicPage}
              >
                新建公开页
              </Button>
            ) : null}
            {publicTab === 'codes' ? (
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="h-3.5 w-3.5" />}
                onClick={() => openInviteCodeGenerator()}
              >
                生成邀请码
              </Button>
            ) : null}
          </div>
        }
      >
        <div className="flex min-h-0 flex-1 flex-col gap-4">
          <div className="shrink-0">
            <Tabs
              {...TOOL_TABS_PROPS}
              value={publicTab}
              onValueChange={setPublicTab}
              tabs={M365_PUBLIC_TABS}
              className="w-fit max-w-full"
              listClassName="w-fit max-w-full"
            />
          </div>

          {publicTab === 'pages' ? (
            <div className="flex min-h-0 flex-1 flex-col">
              {publicPagesLoading ? (
                <div className="grid auto-rows-fr gap-3 cq-md:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <SkeletonLine key={index} className="h-60 w-full" />
                  ))}
                </div>
              ) : filteredPublicPages.length === 0 ? (
                <EmptyState
                  icon={Globe}
                  title="还没有公开页"
                  description="点击右上角新建公开页"
                  card={false}
                />
              ) : (
                <div className={cx(scrollViewportClass, 'pr-1')}>
                  <div className="grid auto-rows-fr gap-3 cq-md:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                    {filteredPublicPages.map(page => {
                      const pageAccounts = (page.accountIds || [])
                        .map(accountId => accountLookup.get(String(accountId)))
                        .filter(Boolean);
                      const accountNames = pageAccounts
                        .map(account => account.name)
                        .filter(Boolean);
                      const domainList = Array.isArray(page.domains) ? page.domains : [];
                      const skuLabels = (page.skuIds || [])
                        .map(skuId => {
                          const normalizedSkuId = String(skuId || '').trim();
                          return (
                            publicPageSkuLabelLookup.get(normalizedSkuId) ||
                            skuLabelLookup.get(normalizedSkuId) ||
                            getSkuDisplayLabel('', normalizedSkuId)
                          );
                        })
                        .filter(Boolean);
                      const totalInviteCodeCount = page.inviteCodeCount || 0;
                      const usedInviteCodeCount = page.usedInviteCodeCount || 0;
                      const inviteCodeSummary = `邀请码 ${usedInviteCodeCount}/${totalInviteCodeCount}`;
                      const pageEnabled = page.enabled !== false;
                      const togglingCurrentPage = togglingPublicPageId === String(page.id);
                      return (
                        <div key={page.id} className={publicResourceCardClass}>
                          <div className={publicResourceCardHeaderClass}>
                            <div className="min-w-0 flex flex-1 items-center">
                              <div className="flex min-w-0 flex-wrap items-center gap-2">
                                <div
                                  className="truncate text-sm font-semibold text-kumo-strong"
                                  title={page.name}
                                >
                                  {page.name}
                                </div>
                                <StatusBadge tone={page.available ? 'success' : 'warning'}>
                                  {page.available ? '可用' : '停用'}
                                </StatusBadge>
                              </div>
                            </div>
                            <div className="flex shrink-0 items-center gap-3">
                              <span title={inviteCodeSummary}>
                                <Badge
                                  variant="outline"
                                  className="max-w-40 !px-2.5 !py-1 !text-[11px] font-medium !text-kumo-strong"
                                >
                                  <span className="truncate">{inviteCodeSummary}</span>
                                </Badge>
                              </span>
                              <div
                                className="flex items-center"
                                title={pageEnabled ? '已启用' : '已关闭'}
                              >
                                <Switch
                                  size="sm"
                                  aria-label={`${page.name} 启用开关`}
                                  checked={pageEnabled}
                                  disabled={togglingCurrentPage}
                                  onCheckedChange={checked =>
                                    togglePublicPageEnabled(page, checked)
                                  }
                                />
                              </div>
                            </div>
                          </div>

                          <div className={publicResourceCardGridClass}>
                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">目标租户</div>
                              <div
                                className="mt-2 truncate text-xs font-medium text-kumo-strong"
                                title={accountNames.join('、') || '-'}
                              >
                                {accountNames.join('、') || '-'}
                              </div>
                            </div>

                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">域名</div>
                              <div className="mt-2">
                                <DomainListPopover
                                  domains={domainList}
                                  copyText={copyText}
                                  label={`${page.name} 域名`}
                                />
                              </div>
                            </div>

                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">配置</div>
                              <div className="mt-2 text-xs font-medium text-kumo-strong">
                                {page.forceChangePasswordNextSignIn ? '首次改密' : '无需改密'}
                              </div>
                            </div>

                            <div className={publicResourceCardFieldClass}>
                              <div className="text-[11px] text-kumo-subtle">许可证</div>
                              <div className="mt-2">
                                <LicenseListPopover
                                  licenses={skuLabels}
                                  copyText={copyText}
                                  label={`${page.name} 许可证`}
                                />
                              </div>
                            </div>
                          </div>

                          <div className={publicResourceCardActionBarClass}>
                            <Button
                              size="sm"
                              variant="primary"
                              className="basis-0 !justify-center text-center"
                              style={{ flex: 2.6 }}
                              onClick={() => openInviteCodeGenerator(page.id)}
                            >
                              生成邀请码
                            </Button>
                            <Button
                              size="sm"
                              variant="secondary"
                              className="basis-0 !justify-center text-center"
                              style={{ flex: 1.2 }}
                              onClick={() => openEditPublicPage(page)}
                            >
                              编辑
                            </Button>
                            <Button
                              size="sm"
                              variant={isArmed(`m365-public-page-delete:${page.id}`) ? 'destructive' : 'secondary-destructive'}
                              className="basis-0 !justify-center text-center"
                              style={{ flex: 1 }}
                              onClick={() => deletePublicPage(page)}
                            >
                              删除
                            </Button>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          ) : null}

          {publicTab === 'codes' ? (
            <div className="flex min-h-0 flex-1 flex-col">
              {inviteCodesLoading ? (
                <div className="space-y-2.5">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <SkeletonLine key={index} className="h-20 w-full" />
                  ))}
                </div>
              ) : groupedInviteCodeBatches.length === 0 ? (
                <EmptyState
                  icon={Globe}
                  title="还没有邀请码"
                  description="先创建公开页再生成邀请码"
                  card={false}
                />
              ) : (
                <div className={cx(scrollViewportClass, 'pr-1')}>
                  <div className="grid auto-rows-fr gap-3 cq-md:grid-cols-2 cq-xl:grid-cols-3 cq-2xl:grid-cols-4">
                    {groupedInviteCodeBatches.map(group =>
                      (() => {
                        const totalCount = group.codes.length;
                        const remainingCount = Math.max(totalCount - group.usedCount, 0);
                        const exhausted = remainingCount <= 0;
                        const partiallyUsed = group.usedCount > 0 && !exhausted;
                        const statusTone = exhausted
                          ? 'danger'
                          : partiallyUsed
                            ? 'warning'
                            : 'success';
                        const batchLabel = group.batchId || '单个生成';
                        const fallbackCode = group.codes[0]?.code || '';
                        const publicRegisterPath = group.batchId
                          ? `/m365/register?batch=${encodeURIComponent(group.batchId)}`
                          : `/m365/register?code=${encodeURIComponent(fallbackCode)}`;
                        const publicRegisterUrl = `${window.location.origin}${publicRegisterPath}`;
                        return (
                          <div key={group.key} className={publicResourceCardClass}>
                            <div className={publicResourceCardHeaderClass}>
                              <div className="min-w-0 flex flex-1 items-center">
                                <div className="flex min-w-0 flex-wrap items-center gap-2">
                                  <div className="truncate text-sm font-semibold text-kumo-strong">
                                    {group.publicPageName}
                                  </div>
                                  <StatusBadge tone={statusTone}>
                                    已用 {group.usedCount}/{totalCount}
                                  </StatusBadge>
                                </div>
                              </div>
                              <span title={batchLabel}>
                                <Badge
                                  variant="outline"
                                  className="max-w-40 !px-2.5 !py-1 !text-[11px] font-medium !text-kumo-strong"
                                >
                                  <span className="truncate">{batchLabel}</span>
                                </Badge>
                              </span>
                            </div>

                            <div className={publicResourceCardGridClass}>
                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">域名</div>
                                <div className="mt-2">
                                  <DomainListPopover
                                    domains={group.domains}
                                    copyText={copyText}
                                    label={`${group.publicPageName} 域名`}
                                  />
                                </div>
                              </div>

                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">剩余可用</div>
                                <div
                                  className={cx(
                                    'mt-2 text-sm font-semibold',
                                    exhausted ? 'text-kumo-danger' : 'text-kumo-strong'
                                  )}
                                >
                                  {remainingCount} / {totalCount}
                                </div>
                              </div>

                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">创建时间</div>
                                <div
                                  className="mt-2 truncate text-xs font-medium text-kumo-strong"
                                  title={group.createdAt ? formatDateTime(group.createdAt) : '-'}
                                >
                                  {group.createdAt ? formatDateTime(group.createdAt) : '-'}
                                </div>
                              </div>

                              <div className={publicResourceCardFieldClass}>
                                <div className="text-[11px] text-kumo-subtle">最后使用</div>
                                <div
                                  className="mt-2 truncate text-xs font-medium text-kumo-strong"
                                  title={
                                    group.lastUsedAt ? formatDateTime(group.lastUsedAt) : '未使用'
                                  }
                                >
                                  {group.lastUsedAt ? formatDateTime(group.lastUsedAt) : '未使用'}
                                </div>
                              </div>
                            </div>

                            <div className={publicResourceCardActionBarClass}>
                              <Button
                                size="sm"
                                variant={exhausted ? 'secondary' : 'primary'}
                                className="basis-0 !justify-center text-center"
                                style={{ flex: 3 }}
                                icon={<Copy className="h-3.5 w-3.5" />}
                                onClick={() => copyText(publicRegisterUrl, '注册链接已复制')}
                                disabled={!publicRegisterUrl}
                              >
                                复制注册链接
                              </Button>
                              <Button
                                size="sm"
                                variant={isArmed(`m365-invite-batch:${group.key}`) ? 'destructive' : 'secondary-destructive'}
                                className="basis-0 !justify-center text-center"
                                style={{ flex: 1 }}
                                onClick={() => void deleteInviteBatch(group)}
                              >
                                {isArmed(`m365-invite-batch:${group.key}`) ? '确认' : '删除'}
                              </Button>
                            </div>
                          </div>
                        );
                      })()
                    )}
                  </div>
                </div>
              )}
            </div>
          ) : null}

          {publicTab === 'registrations' ? (
            <AppCard padding="none" className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <div className="flex items-center justify-between border-b border-kumo-line/60 px-4 py-3">
                <div>
                  <div className="text-sm font-semibold text-kumo-strong">注册记录</div>
                </div>
                <div className="flex items-center gap-2">
                  {selectedRegistrationIds.length > 0 ? (
                    <div className="text-xs text-kumo-subtle">
                      已选 {selectedRegistrationIds.length} 条
                    </div>
                  ) : null}
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    icon={<Trash className="h-3.5 w-3.5" />}
                    onClick={deleteSelectedRegistrations}
                    disabled={selectedRegistrationIds.length === 0 || deletingRegistrations}
                  >
                    {deletingRegistrations ? '删除中...' : '批量删除'}
                  </Button>
                </div>
              </div>
              {registrationsLoading ? (
                <div className="space-y-3 p-4">
                  {Array.from({ length: 5 }).map((_, index) => (
                    <SkeletonLine key={index} className="h-16 w-full" />
                  ))}
                </div>
              ) : filteredRegistrations.length === 0 ? (
                <EmptyState
                  icon={Users}
                  title="暂无注册记录"
                  card={false}
                />
              ) : (
                <DataTableFrame
                  variant="embedded"
                  density="dense"
                  className="min-h-0 flex-1 overflow-auto scrollbar-thin"
                >
                  <AppTable tableId="m365-registration-records" columns={REGISTRATION_TABLE_COLUMNS}>
                    <Table.Header sticky variant="compact">
                      <Table.Row>
                        <Table.CheckHead
                          checked={
                            filteredRegistrations.length > 0 &&
                            selectedRegistrationRecords.length === filteredRegistrations.length
                          }
                          indeterminate={
                            selectedRegistrationRecords.length > 0 &&
                            selectedRegistrationRecords.length < filteredRegistrations.length
                          }
                          onCheckedChange={checked =>
                            setSelectedRegistrationIds(
                              checked ? filteredRegistrations.map(record => record.id) : []
                            )
                          }
                          aria-label="全选注册记录"
                          className="!px-2 !py-1.5 text-center"
                        />
                        <Table.Head>账号</Table.Head>
                        <Table.Head>状态</Table.Head>
                        <Table.Head>来源</Table.Head>
                        <Table.Head>目标租户</Table.Head>
                        <Table.Head>Graph 用户 ID</Table.Head>
                        <Table.Head>创建时间</Table.Head>
                        <Table.Head>结果 / 错误</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {filteredRegistrations.map(record => (
                        <Table.Row
                          key={record.id}
                          variant={
                            selectedRegistrationIds.includes(record.id) ? 'selected' : 'default'
                          }
                        >
                          <Table.CheckCell
                            checked={selectedRegistrationIds.includes(record.id)}
                            onCheckedChange={checked =>
                              toggleRegistrationSelection(record.id, Boolean(checked))
                            }
                            aria-label={`选择注册记录 ${record.userPrincipalName || record.displayName || record.id}`}
                            className="!px-2 !py-1.5 text-center"
                          />
                          <Table.Cell>
                            <div className="min-w-0">
                              <div
                                className="truncate text-sm font-semibold text-kumo-strong"
                                title={record.displayName || record.userPrincipalName || '-'}
                              >
                                {record.displayName || record.userPrincipalName || '-'}
                              </div>
                              <div
                                className="truncate text-xs text-kumo-subtle"
                                title={record.userPrincipalName || '-'}
                              >
                                {record.userPrincipalName || '-'}
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <StatusBadge tone={getRegistrationTone(record.status)}>
                              {getRegistrationStatusLabel(record.status)}
                            </StatusBadge>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="min-w-0 space-y-1">
                              <div
                                className="truncate text-xs font-medium text-kumo-strong"
                                title={record.publicPageName || record.inviteName || '-'}
                              >
                                {record.publicPageName || record.inviteName || '-'}
                              </div>
                              <div
                                className="truncate font-mono text-[11px] text-kumo-subtle"
                                title={record.inviteCode || '-'}
                              >
                                {record.inviteCode || '-'}
                              </div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div
                              className="truncate text-xs font-medium text-kumo-strong"
                              title={record.accountName || '-'}
                            >
                              {record.accountName || '-'}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div
                              className="truncate font-mono text-[11px] text-kumo-subtle"
                              title={record.graphUserId || '-'}
                            >
                              {record.graphUserId || '-'}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div
                              className="truncate text-xs text-kumo-strong"
                              title={record.createdAt ? formatDateTime(record.createdAt) : '-'}
                            >
                              {record.createdAt ? formatDateTime(record.createdAt) : '-'}
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <Button
                              type="button"
                              variant="ghost"
                              title={getRegistrationResultText(record)}
                              onClick={() => setRegistrationDetail(record)}
                              className={cx(
                                '!h-auto w-full justify-start overflow-hidden px-2.5 py-1.5 text-left text-xs',
                                record.errorMessage
                                  ? 'border border-kumo-danger/20 bg-kumo-danger/5 text-kumo-danger hover:bg-kumo-danger/10'
                                  : 'text-kumo-subtle hover:bg-kumo-recessed/25'
                              )}
                            >
                              <span className="block truncate">
                                {getRegistrationResultText(record)}
                              </span>
                            </Button>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </AppTable>
                </DataTableFrame>
              )}
            </AppCard>
          ) : null}
        </div>
      </SectionCard>
    </PageStack>
  );
}
