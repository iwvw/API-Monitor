import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { Toolbar } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Download, Edit, Key, Plus, Shield, Trash, Upload } from '../../components/Icons.jsx';
import { ACCOUNT_TABLE_COLUMNS } from './constants.js';
import { getOciStatusTone, getVerifyStatusLabel } from './utils.js';
import TableSkeletonRows from './TableSkeletonRows.jsx';

export default function AccountsPanel({ accounts, loadingAccounts, accountImportFileRef, onLoadAccountImportFile, onExportAccounts, onOpenAccountImportDialog, onOpenAccountDialog, onVerifyAccount, onDeleteAccount, isArmed }) {
  return (
    <SectionCard
      title="Oracle 账号"
      icon={<Key className="h-4 w-4 text-brand" />}
      description={accounts.length > 0 ? `${accounts.length} 个已配置账号` : '管理 OCI 凭证和默认 Compartment'}
      className="min-h-0 flex-1"
      bodyPadding="none"
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
      actions={(
        <>
          <input
            ref={accountImportFileRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={onLoadAccountImportFile}
          />
          <Toolbar size="sm" aria-label="导出导入账号" className="shrink-0">
            <Toolbar.Button type="button" onClick={onExportAccounts} aria-label="导出账号" title="导出账号" icon={<Upload className="h-3.5 w-3.5" />}>
              <span className="hidden cq-sm:inline">导出</span>
            </Toolbar.Button>
            <Toolbar.Button type="button" onClick={onOpenAccountImportDialog} aria-label="导入账号" title="导入账号" icon={<Download className="h-3.5 w-3.5" />}>
              <span className="hidden cq-sm:inline">导入</span>
            </Toolbar.Button>
          </Toolbar>
          <Button type="button" size="sm" shape="square" variant="primary" onClick={() => onOpenAccountDialog()} aria-label="添加账号" title="添加账号" icon={<Plus className="h-4 w-4" />} />
        </>
      )}
    >
      <DataTableFrame variant="embedded" density="dense" className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin">
        <AppTable tableId="oracle-accounts" columns={ACCOUNT_TABLE_COLUMNS}>
          <Table.Header variant="compact">
            <Table.Row>
              <Table.Head>名称</Table.Head>
              <Table.Head>Region</Table.Head>
              <Table.Head>默认 Compartment</Table.Head>
              <Table.Head>验证状态</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {loadingAccounts && accounts.length === 0 ? (
              <TableSkeletonRows columns={5} rows={5} />
            ) : accounts.length === 0 ? (
              <Table.Row>
                <Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">尚未配置 Oracle 账号。</Table.Cell>
              </Table.Row>
            ) : accounts.map((account) => (
              <Table.Row
                key={account.id}
                className="cursor-pointer"
                title="双击编辑账号"
                onDoubleClick={() => onOpenAccountDialog(account)}
              >
                <Table.Cell>
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold text-kumo-strong" title={account.name || '-'}>
                      {account.name}
                    </div>
                    <div className="truncate font-mono text-[11px] text-kumo-subtle" title={account.description || account.tenancyOcid || '-'}>
                      {account.description || account.tenancyOcid}
                    </div>
                  </div>
                </Table.Cell>
                <Table.Cell>{account.region}</Table.Cell>
                <Table.Cell><code className="block truncate text-xs">{account.defaultCompartmentId || '-'}</code></Table.Cell>
                <Table.Cell>
                  <StatusBadge tone={getOciStatusTone(account.lastVerifyStatus)}>
                    {getVerifyStatusLabel(account.lastVerifyStatus)}
                  </StatusBadge>
                </Table.Cell>
                <Table.Cell className="text-right">
                  <div className="inline-flex gap-2">
                    <Button type="button" size="sm" shape="square" variant="secondary" onClick={() => onVerifyAccount(account.id)} aria-label={`验证 ${account.name}`} title="验证" icon={<Shield className="h-4 w-4" />} />
                    <Button type="button" size="sm" shape="square" variant="secondary" onClick={() => onOpenAccountDialog(account)} aria-label={`编辑 ${account.name}`} title="编辑" icon={<Edit className="h-4 w-4" />} />
                    <Button type="button" size="sm" shape="square" variant={isArmed(`account:${account.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeleteAccount(account.id)} aria-label={`删除 ${account.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );
}
