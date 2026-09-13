import React from 'react';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { Toolbar } from '@cloudflare/kumo';
import { Download, Edit, Eye, EyeOff, Plus, Settings, Shield, Trash, Upload } from '../../components/Icons.jsx';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { formatDate } from './utils.jsx';

function AccountsPanel({
  accounts,
  accountTokens,
  accountColWidths,
  startAccountResize,
  isArmed,
  openImportModal,
  openAccountModal,
  exportAccounts,
  toggleAccountToken,
  verifyAccount,
  deleteAccount,
}) {
  return (
    <SectionCard
      title="Cloudflare 账号"
      icon={<Settings className="h-4 w-4 text-brand" />}
      actions={(
        <>
        <Toolbar size="sm" aria-label="导出导入账号" className="shrink-0">
          <Toolbar.Button onClick={exportAccounts} aria-label="导出账号" title="导出账号" icon={<Upload className="h-3.5 w-3.5" />}>
            <span className="hidden cq-sm:inline">导出</span>
          </Toolbar.Button>
          <Toolbar.Button onClick={() => openImportModal('accounts')} aria-label="导入账号" title="导入账号" icon={<Download className="h-3.5 w-3.5" />}>
            <span className="hidden cq-sm:inline">导入</span>
          </Toolbar.Button>
        </Toolbar>
        <Button size="sm" variant="primary" onClick={() => openAccountModal()} icon={<Plus className="h-4 w-4" />}>添加账号</Button>
        </>
      )}
      bodyPadding="none"
      bodyClassName="overflow-x-auto"
    >
        <Table layout="fixed">
          <colgroup>{accountColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
          <Table.Header variant="compact">
            <Table.Row>
              <Table.Head className="relative pr-6">备注名称<Table.ResizeHandle onMouseDown={(e) => startAccountResize(0, e)} onTouchStart={(e) => startAccountResize(0, e)} /></Table.Head>
              <Table.Head className="relative pr-6">邮箱<Table.ResizeHandle onMouseDown={(e) => startAccountResize(1, e)} onTouchStart={(e) => startAccountResize(1, e)} /></Table.Head>
              <Table.Head className="relative pr-6">Account ID<Table.ResizeHandle onMouseDown={(e) => startAccountResize(2, e)} onTouchStart={(e) => startAccountResize(2, e)} /></Table.Head>
              <Table.Head className="relative pr-6">令牌<Table.ResizeHandle onMouseDown={(e) => startAccountResize(3, e)} onTouchStart={(e) => startAccountResize(3, e)} /></Table.Head>
              <Table.Head className="relative pr-6">最后使用<Table.ResizeHandle onMouseDown={(e) => startAccountResize(4, e)} onTouchStart={(e) => startAccountResize(4, e)} /></Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {accounts.length === 0 ? (
              <Table.Row><Table.Cell colSpan={6} className="py-10 text-center text-kumo-subtle">暂无 Cloudflare 账号。</Table.Cell></Table.Row>
            ) : accounts.map((account) => (
              <Table.Row
                key={account.id}
                className="cursor-pointer"
                title="双击编辑账号"
                onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openAccountModal(account))}
              >
                <Table.Cell className="font-medium text-kumo-strong">{account.name}</Table.Cell>
                <Table.Cell>{account.userEmail || account.email || '-'}</Table.Cell>
                <Table.Cell><code className="block truncate text-xs">{account.cfAccountId || '-'}</code></Table.Cell>
                <Table.Cell>
                  <div className="flex items-center gap-2">
                    <code className="truncate text-xs">{accountTokens[account.id] || (account.hasToken ? '••••••••••••••••' : '-')}</code>
                    {account.hasToken && (
                      <Button size="sm" shape="square" variant="secondary" onClick={() => toggleAccountToken(account)} aria-label="显示或隐藏令牌">
                        {accountTokens[account.id] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                      </Button>
                    )}
                  </div>
                </Table.Cell>
                <Table.Cell>{formatDate(account.lastUsed)}</Table.Cell>
                <Table.Cell className="text-right">
                  <div className="inline-flex gap-2">
                    <Button size="sm" shape="square" variant="secondary" onClick={() => verifyAccount(account)} aria-label={`验证 ${account.name}`} title="验证" icon={<Shield className="h-4 w-4" />} />
                    <Button size="sm" shape="square" variant="secondary" onClick={() => openAccountModal(account)} aria-label={`编辑 ${account.name}`} title="编辑" icon={<Edit className="h-4 w-4" />} />
                    <Button size="sm" shape="square" variant={isArmed(`account:${account.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteAccount(account)} aria-label={`删除 ${account.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
    </SectionCard>
  );
}

export default AccountsPanel;
