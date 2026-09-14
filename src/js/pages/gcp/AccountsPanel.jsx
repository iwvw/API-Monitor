import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppTable, DataTableFrame, EmptyState, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Key, Plus } from '../../components/Icons.jsx';
import { ACCOUNT_TABLE_COLUMNS } from './constants.js';
import { getGcpStatusTone, getVerifyStatusLabel } from './utils.jsx';
import { AccountActions } from './ActionsPanels.jsx';

export default function AccountsPanel({ loadingAccounts, accounts, onOpenCreateAccount, onOpenDetail, onVerifyAccount, onEditAccount, onDeleteAccount }) {
  return (
    <SectionCard
      title="GCP 账号"
      icon={<Key className="h-4 w-4" />}
      actions={(
        <Button type="button" size="sm" variant="primary" onClick={onOpenCreateAccount}>
          <Plus className="h-4 w-4" />新增账号
        </Button>
      )}
      bodyPadding="none"
    >
      {loadingAccounts ? (
        <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
          <AppTable tableId="gcp-accounts-loading" columns={ACCOUNT_TABLE_COLUMNS}>
            {[0, 1, 2].map((row) => (
              <Table.Row key={row}>
                {ACCOUNT_TABLE_COLUMNS.map((col) => (
                  <Table.Cell key={col.id}><SkeletonLine className="h-4" /></Table.Cell>
                ))}
              </Table.Row>
            ))}
          </AppTable>
        </DataTableFrame>
      ) : accounts.length === 0 ? (
        <EmptyState card={false} icon={Key} title="暂无 GCP 账号" description="新增账号并粘贴 Service Account JSON 开始使用" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
          <AppTable tableId="gcp-accounts" columns={ACCOUNT_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>Service Account</Table.Head>
                <Table.Head>默认项目</Table.Head>
                <Table.Head>验证状态</Table.Head>
                <Table.Head>操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {accounts.map((account) => (
                <Table.Row key={account.id}>
                  <Table.Cell><Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('account', account)} title={`${account.name} · 点击查看详情`}>{account.name}</Button></Table.Cell>
                  <Table.Cell className="truncate font-mono text-xs" title={account.clientEmail}>{account.clientEmail || '-'}</Table.Cell>
                  <Table.Cell className="text-sm text-kumo-strong">{account.defaultProjectId || '-'}</Table.Cell>
                  <Table.Cell>
                    <StatusBadge tone={getGcpStatusTone(account.lastVerifyStatus)}>{getVerifyStatusLabel(account.lastVerifyStatus)}</StatusBadge>
                  </Table.Cell>
                  <Table.Cell><AccountActions account={account} onVerify={onVerifyAccount} onEdit={onEditAccount} onDelete={onDeleteAccount} /></Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
