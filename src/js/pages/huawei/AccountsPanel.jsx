import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import {
  Download,
  Edit,
  Key,
  Plus,
  Shield,
  Terminal,
  Trash,
  Upload,
} from '../../components/Icons.jsx';
import { ACCOUNT_TABLE_COLUMNS } from './constants.js';

export default function AccountsPanel({
  loadingAccounts,
  accounts,
  exportAccountsAction,
  setImportDialogOpen,
  openCreateAccount,
  openSshDialog,
  verifyAccount,
  openEditAccount,
  deleteAccount,
}) {
  return (
    <SectionCard
      title="华为云账号"
      icon={<Key className="h-4 w-4" />}
      actions={(
        <>
          <Button type="button" size="sm" variant="secondary" onClick={exportAccountsAction} title="导出账号清单（含敏感凭据）"><Download className="h-4 w-4" />导出</Button>
          <Button type="button" size="sm" variant="secondary" onClick={() => setImportDialogOpen(true)}><Upload className="h-4 w-4" />导入</Button>
          <Button type="button" size="sm" variant="primary" onClick={openCreateAccount}>
            <Plus className="h-4 w-4" />新增账号
          </Button>
        </>
      )}
      bodyPadding="none"
    >
      {loadingAccounts ? (
        <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
          <AppTable tableId="huawei-accounts-loading" columns={ACCOUNT_TABLE_COLUMNS}>
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
        <EmptyState card={false} icon={Key} title="暂无华为云账号" description="新增账号并填写 AK/SK 开始使用" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
          <AppTable tableId="huawei-accounts" columns={ACCOUNT_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>名称</Table.Head>
                <Table.Head>站点</Table.Head>
                <Table.Head>AK</Table.Head>
                <Table.Head>默认区域</Table.Head>
                <Table.Head>验证状态</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {accounts.map((account) => (
                <Table.Row key={account.id}>
                  <Table.Cell>
                    <span className="block max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={account.name}>{account.name}</span>
                  </Table.Cell>
                  <Table.Cell className="text-sm text-kumo-strong">{account.site === 'intl' ? '国际站' : '国内站'}</Table.Cell>
                  <Table.Cell className="truncate font-mono text-xs" title={account.accessKeyId}>{account.accessKeyId || '-'}</Table.Cell>
                  <Table.Cell className="text-sm text-kumo-strong">{account.defaultRegion || '-'}</Table.Cell>
                  <Table.Cell>
                    <StatusBadge tone={account.lastVerifyStatus === 'success' ? 'success' : account.lastVerifyStatus === 'failed' ? 'danger' : 'neutral'}>
                      {account.lastVerifyStatus === 'success' ? '已验证' : account.lastVerifyStatus === 'failed' ? '验证失败' : '未验证'}
                    </StatusBadge>
                  </Table.Cell>
                  <Table.Cell>
                    <div className="inline-flex items-center gap-1">
                      <Button type="button" size="sm" shape="square" variant="secondary" title="SSH 凭据" aria-label="SSH 凭据" onClick={() => openSshDialog(account)}><Terminal className="h-4 w-4" /></Button>
                      <Button type="button" size="sm" shape="square" variant="secondary" title="验证" aria-label="验证" onClick={() => verifyAccount(account)}><Shield className="h-4 w-4" /></Button>
                      <Button type="button" size="sm" shape="square" variant="secondary" title="编辑" aria-label="编辑" onClick={() => openEditAccount(account)}><Edit className="h-4 w-4" /></Button>
                      <Button type="button" size="sm" shape="square" variant="destructive" title="删除" aria-label="删除" onClick={() => deleteAccount(account)}><Trash className="h-4 w-4" /></Button>
                    </div>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
