import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import {
  AppCard,
  cx,
  DataTableFrame,
  EmptyState,
  PageStack,
  SectionCard,
} from '../../components/ui/AppPrimitives.jsx';
import { Folder, Plus, RefreshCw, Users } from '../../components/Icons.jsx';
import { panelBodyClass, scrollViewportClass, tableFrameClass } from './constants.js';
import { CardTableSkeleton, GroupsTabSkeleton } from './Skeletons.jsx';

export default function GroupsTab({
  selectedAccountId,
  groupsLoading,
  groups,
  loadGroups,
  setShowGroupDialog,
  selectedGroupId,
  setSelectedGroupId,
  selectedGroup,
  memberInput,
  setMemberInput,
  addGroupMember,
  groupMembersLoading,
  skuItems,
  groupLicenseSkuId,
  setGroupLicenseSkuId,
  assignGroupLicense,
  assigningGroupLicense,
  groupMembers,
  removeGroupMember,
  isArmed,
}) {
  return (
    <PageStack viewport className="min-h-0 flex-1">
      <SectionCard
        className="flex min-h-0 flex-1 flex-col"
        bodyClassName={panelBodyClass}
        title="组管理"
        icon={<Folder className="h-4 w-4" />}
        action={
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="secondary"
              icon={<RefreshCw className="h-3.5 w-3.5" />}
              onClick={loadGroups}
            >
              刷新
            </Button>
            <Button
              size="sm"
              variant="primary"
              icon={<Plus className="h-3.5 w-3.5" />}
              onClick={() => setShowGroupDialog(true)}
            >
              新建组
            </Button>
          </div>
        }
      >
        {!selectedAccountId ? (
          <EmptyState icon={Folder} title="请先选择租户" />
        ) : groupsLoading ? (
          <GroupsTabSkeleton />
        ) : groups.length === 0 ? (
          <EmptyState
            icon={Folder}
            title="暂无组"
            description="先创建一个组"
          />
        ) : (
          <div className="grid min-h-0 flex-1 gap-4 cq-lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
            <AppCard padding="none" className="flex min-h-0 flex-col">
              <DataTableFrame variant="embedded" density="compact" className={scrollViewportClass}>
                <Table layout="auto" className="[&_td]:py-3 [&_th]:py-3">
                  <Table.Header>
                    <Table.Row>
                      <Table.Head>组</Table.Head>
                      <Table.Head>邮件</Table.Head>
                      <Table.Head>类型</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {groups.map(group => (
                      <Table.Row
                        key={group.id}
                        className={
                          String(group.id) === String(selectedGroupId) ? 'bg-brand/5' : ''
                        }
                        onClick={() => setSelectedGroupId(String(group.id))}
                      >
                        <Table.Cell>
                          <div className="font-medium text-kumo-strong">
                            {group.displayName || '-'}
                          </div>
                          <div className="text-xs text-kumo-subtle">{group.id}</div>
                        </Table.Cell>
                        <Table.Cell>{group.mail || '-'}</Table.Cell>
                        <Table.Cell>{group.securityEnabled ? '安全组' : '协作组'}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </DataTableFrame>
            </AppCard>

            <SectionCard
              className="flex min-h-0 flex-col"
              bodyClassName={panelBodyClass}
              title={selectedGroup ? selectedGroup.displayName : '组成员'}
              description="输入成员对象 ID"
              icon={<Users className="h-4 w-4" />}
              bodyPadding="sm"
              action={
                <div className="flex items-center gap-2">
                  <Input
                    aria-label="成员对象 ID"
                    size="sm"
                    value={memberInput}
                    onChange={event => setMemberInput(event.target.value)}
                    placeholder="成员对象 ID"
                  />
                  <Button size="sm" variant="secondary" onClick={addGroupMember}>
                    添加成员
                  </Button>
                </div>
              }
            >
              {!selectedGroup ? (
                <EmptyState
                  icon={Users}
                  title="请选择一个组"
                  card={false}
                />
              ) : groupMembersLoading ? (
                <CardTableSkeleton rows={5} showToolbar />
              ) : (
                <div className="flex min-h-0 flex-1 flex-col gap-4">
                  <div className="flex items-center gap-2">
                    <Select alignItemWithTrigger
                      aria-label="组许可证 SKU"
                      size="sm"
                      value={groupLicenseSkuId}
                      onValueChange={setGroupLicenseSkuId}
                      items={skuItems}
                    />
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={assignGroupLicense}
                      disabled={assigningGroupLicense}
                    >
                      分配组许可证
                    </Button>
                  </div>
                  <div
                    className={cx(
                      tableFrameClass,
                      'rounded-lg border border-kumo-line/80 bg-kumo-base'
                    )}
                  >
                    <DataTableFrame
                      variant="embedded"
                      density="compact"
                      className={scrollViewportClass}
                    >
                      <Table layout="auto" className="[&_td]:py-3 [&_th]:py-3">
                        <Table.Header>
                          <Table.Row>
                            <Table.Head>成员</Table.Head>
                            <Table.Head>邮箱</Table.Head>
                            <Table.Head className="app-table-action">操作</Table.Head>
                          </Table.Row>
                        </Table.Header>
                        <Table.Body>
                          {groupMembers.map(member => (
                            <Table.Row key={member.id}>
                              <Table.Cell>
                                <div className="font-medium text-kumo-strong">
                                  {member.displayName || '-'}
                                </div>
                                <div className="text-xs text-kumo-subtle">{member.id}</div>
                              </Table.Cell>
                              <Table.Cell>
                                {member.userPrincipalName || member.mail || '-'}
                              </Table.Cell>
                              <Table.Cell>
                                <div className="flex justify-end">
                                  <Button
                                    size="sm"
                                    variant={isArmed(`m365-group-member-remove:${member.id}`) ? 'destructive' : 'secondary-destructive'}
                                    onClick={() => removeGroupMember(member)}
                                  >
                                    移除
                                  </Button>
                                </div>
                              </Table.Cell>
                            </Table.Row>
                          ))}
                        </Table.Body>
                      </Table>
                    </DataTableFrame>
                  </div>
                </div>
              )}
            </SectionCard>
          </div>
        )}
      </SectionCard>
    </PageStack>
  );
}
