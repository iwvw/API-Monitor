import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { AppCard } from '../../components/ui/AppPrimitives.jsx';
import { Edit, FolderOpen, Trash } from '../../components/Icons.jsx';
import { BRAND_COLOR_FALLBACK } from '../../components/ui/BrandIcon.jsx';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';

const GroupsTab = ({
  totpGroups,
  handleOpenAddGroup,
  groupAccountCounts,
  isArmed,
  handleOpenEditGroup,
  handleDeleteGroup,
}) => {
  return (
    <AppCard padding="none" className="overflow-hidden">
      {totpGroups.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle">
          <FolderOpen className="w-12 h-12 opacity-30 mb-4" />
          <span>暂无分组</span>
          <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddGroup}>
            创建分组
          </Button>
        </div>
      ) : (
        <div className="w-full overflow-x-auto">
          <Table layout="fixed">
            <colgroup>
              <col className="w-20" />
              <col />
              <col className="w-28" />
              <col className="w-36" />
            </colgroup>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>颜色</Table.Head>
                <Table.Head>分组名称</Table.Head>
                <Table.Head className="text-center">账号数</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {totpGroups.map(group => (
                <Table.Row
                  key={group.id}
                  className="cursor-pointer"
                  title="双击编辑分组"
                  onDoubleClick={event =>
                    handleEditableRowDoubleClick(event, () => handleOpenEditGroup(group))
                  }
                >
                  <Table.Cell>
                    <div
                      className="h-4 w-4 rounded-full border border-kumo-line"
                      style={{ background: group.color || BRAND_COLOR_FALLBACK }}
                    />
                  </Table.Cell>
                  <Table.Cell className="font-semibold text-kumo-strong">
                    {group.name}
                  </Table.Cell>
                  <Table.Cell className="text-center tabular-nums text-kumo-default">
                    {groupAccountCounts[group.id] || 0}
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <div className="flex items-center justify-center gap-2">
                      <Button
                        shape="square"
                        size="sm"
                        variant="secondary"
                        aria-label="编辑分组"
                        onClick={() => handleOpenEditGroup(group)}
                        title="编辑"
                        icon={<Edit className="w-3.5 h-3.5" />}
                      />
                      <Button
                        shape="square"
                        size="sm"
                        variant={
                          isArmed(`totp-group-${group.id}`)
                            ? 'destructive'
                            : 'secondary-destructive'
                        }
                        aria-label="删除分组"
                        onClick={() => handleDeleteGroup(group)}
                        title={
                          isArmed(`totp-group-${group.id}`)
                            ? '再次点击确认删除'
                            : '删除'
                        }
                        icon={<Trash className="w-3.5 h-3.5" />}
                      />
                    </div>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
    </AppCard>
  );
};

export default GroupsTab;
