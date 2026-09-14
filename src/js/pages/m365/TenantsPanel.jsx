import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Toolbar } from '@cloudflare/kumo';
import { cx, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, Plus, RefreshCw, Trash, Upload, Download } from '../../components/Icons.jsx';
import {
  panelBodyClass,
  scrollViewportClass,
  tenantCardFrameClass,
  tenantGridClass,
} from './constants.js';
import { TenantGridSkeleton } from './Skeletons.jsx';
import { getDisplayText } from './utils.js';

export default function TenantsPanel({
  accounts,
  loadingAccounts,
  selectedAccountId,
  setSelectedAccountId,
  verifyingAccountId,
  exportAccounts,
  openImportAccounts,
  openCreateAccount,
  verifyAccount,
  openEditAccount,
  deleteAccount,
  isArmed,
}) {
  return (
    <SectionCard
      className="flex min-h-0 flex-1 flex-col"
      bodyClassName={panelBodyClass}
      title="租户管理"
      icon={<Cloud className="h-4 w-4" />}
      action={
        <div className="flex items-center gap-2">
          <Toolbar size="sm" aria-label="导出导入租户" className="shrink-0">
            <Toolbar.Button
              title="导出租户"
              aria-label="导出租户"
              icon={<Upload className="h-3.5 w-3.5" />}
              onClick={exportAccounts}
            >
              <span className="hidden cq-sm:inline">导出</span>
            </Toolbar.Button>
            <Toolbar.Button
              title="导入租户"
              aria-label="导入租户"
              icon={<Download className="h-3.5 w-3.5" />}
              onClick={openImportAccounts}
            >
              <span className="hidden cq-sm:inline">导入</span>
            </Toolbar.Button>
          </Toolbar>
          <Button
            size="sm"
            variant="primary"
            icon={<Plus className="h-3.5 w-3.5" />}
            onClick={openCreateAccount}
          >
            新增租户
          </Button>
        </div>
      }
    >
      {loadingAccounts ? (
        <TenantGridSkeleton />
      ) : (
        <div className={cx(scrollViewportClass, 'grid content-start gap-3 p-1', tenantGridClass)}>
          {accounts.map(account => {
            const active = String(account.id) === String(selectedAccountId);
            const verifying = String(account.id) === String(verifyingAccountId);
            return (
              <div
                key={account.id}
                role="button"
                tabIndex={0}
                className={cx(
                  tenantCardFrameClass,
                  'group flex h-full cursor-pointer flex-col justify-between border bg-kumo-base/95 text-left hover:border-brand/40 hover:bg-kumo-base focus:outline-none focus-visible:border-brand/70',
                  active ? 'border-brand/70 bg-brand/5' : 'border-kumo-line/80'
                )}
                onClick={() => setSelectedAccountId(String(account.id))}
                onKeyDown={event => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    setSelectedAccountId(String(account.id));
                  }
                }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-kumo-recessed/60 text-brand">
                        <Cloud className="h-4 w-4" />
                      </div>
                      <div className="min-w-0">
                        <div
                          className="truncate text-sm font-semibold text-kumo-strong"
                          title={getDisplayText(account.name)}
                        >
                          {getDisplayText(account.name)}
                        </div>
                        <div
                          className="truncate text-xs text-kumo-subtle"
                          title={getDisplayText(account.organization || '未校验')}
                        >
                          {getDisplayText(account.organization || '未校验')}
                        </div>
                      </div>
                    </div>
                  </div>
                  <StatusBadge
                    tone={account.lastVerifiedErr ? 'danger' : 'success'}
                    className="shrink-0"
                  >
                    {account.lastVerifiedErr ? '待修复' : '已连通'}
                  </StatusBadge>
                </div>

                <div className="mt-2.5 grid gap-2 rounded-lg border border-kumo-line/60 bg-kumo-recessed/20 p-2 text-xs">
                  <div className="flex items-center justify-between gap-3">
                    <span className="shrink-0 text-kumo-subtle">默认域</span>
                    <span
                      className="min-w-0 truncate font-medium text-kumo-strong"
                      title={getDisplayText(account.defaultDomain)}
                    >
                      {getDisplayText(account.defaultDomain)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="shrink-0 text-kumo-subtle">租户 ID</span>
                    <span
                      className="min-w-0 truncate font-mono text-[11px] text-kumo-subtle"
                      title={getDisplayText(account.tenantId)}
                    >
                      {getDisplayText(account.tenantId)}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-3">
                    <span className="shrink-0 text-kumo-subtle">客户端 ID</span>
                    <span
                      className="min-w-0 truncate font-mono text-[11px] text-kumo-subtle"
                      title={getDisplayText(account.clientId)}
                    >
                      {getDisplayText(account.clientId)}
                    </span>
                  </div>
                </div>

                <div className="mt-2.5 flex items-center justify-between gap-2">
                  <span
                    className={cx(
                      'text-[11px] font-medium',
                      active ? 'text-brand' : 'text-kumo-subtle'
                    )}
                  >
                    {active ? '已选中' : '选择'}
                  </span>
                  <div className="flex gap-2" onClick={event => event.stopPropagation()}>
                    <Button
                      size="sm"
                      variant="secondary"
                      shape="square"
                      title="校验"
                      aria-label="校验"
                      loading={verifying}
                      icon={<RefreshCw className="h-3.5 w-3.5" />}
                      onClick={() => verifyAccount(account)}
                    />
                    <Button size="sm" variant="secondary" onClick={() => openEditAccount(account)}>
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      variant={isArmed(`m365-account-delete:${account.id}`) ? 'destructive' : 'secondary-destructive'}
                      shape="square"
                      title="删除"
                      aria-label="删除"
                      icon={<Trash className="h-3.5 w-3.5" />}
                      onClick={() => deleteAccount(account)}
                    />
                  </div>
                </div>
              </div>
            );
          })}
          <Button
            key="tenant-placeholder"
            type="button"
            variant="secondary"
            className={cx(
              tenantCardFrameClass,
              'group !h-full w-full justify-center border-dashed border-kumo-line/80 bg-kumo-base/35 text-kumo-subtle hover:border-brand/45 hover:bg-brand/5 hover:text-brand'
            )}
            onClick={openCreateAccount}
            aria-label="添加新租户"
          >
            <div className="flex flex-col items-center gap-2">
              <div className="flex h-10 w-10 items-center justify-center rounded-full border border-dashed border-kumo-line/90 text-kumo-subtle group-hover:border-brand/50 group-hover:text-brand">
                <Plus className="h-5 w-5" />
              </div>
              <span className="text-xs font-medium opacity-0 transition group-hover:opacity-100">
                添加新租户
              </span>
            </div>
          </Button>
        </div>
      )}
    </SectionCard>
  );
}
