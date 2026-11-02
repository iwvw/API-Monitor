import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { AppCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Copy, Shield } from '../../components/Icons.jsx';

export default function PermissionDialog({
  open,
  onOpenChange,
  selectedAccount,
  selectedAccountId,
  permissionCheckLoading,
  detectPermissions,
  permissionCheckError,
  permissionItems,
  copyText,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="lg">
        <LayerDialog.Title>Graph 权限说明</LayerDialog.Title>

        <LayerDialog.Body>
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="text-xs text-kumo-subtle">
              {selectedAccount ? `当前租户：${selectedAccount.name}` : '请先选择租户'}
            </div>
            <Button
              size="sm"
              variant="secondary"
              icon={
                <Shield
                  className={`h-3.5 w-3.5 ${permissionCheckLoading ? 'animate-pulse' : ''}`}
                />
              }
              onClick={detectPermissions}
              disabled={!selectedAccountId || permissionCheckLoading}
            >
              {permissionCheckLoading ? '检测中...' : '一键检测'}
            </Button>
          </div>

          {permissionCheckError ? (
            <AppCard className="border-kumo-danger/20 bg-kumo-danger/5">
              <div className="text-xs text-kumo-danger">{permissionCheckError}</div>
            </AppCard>
          ) : null}

          <div className="grid gap-3">
            {permissionItems.map(permission => (
              <AppCard key={permission.name} className="p-0">
                <div className="flex items-center justify-between gap-3 px-4 py-3">
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <div className="font-mono text-sm font-semibold text-kumo-strong">
                        {permission.name}
                      </div>
                      {permission.granted === true ? (
                        <StatusBadge tone="success">已具备</StatusBadge>
                      ) : null}
                      {permission.granted === false ? (
                        <StatusBadge tone="danger">缺失</StatusBadge>
                      ) : null}
                    </div>
                    <div className="mt-1 text-xs text-kumo-subtle">{permission.note}</div>
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    shape="square"
                    aria-label={`复制 ${permission.name}`}
                    title="复制权限名"
                    icon={<Copy className="h-3.5 w-3.5" />}
                    onClick={() => copyText(permission.name, `${permission.name} 已复制`)}
                  />
                </div>
              </AppCard>
            ))}
          </div>
        </div>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
