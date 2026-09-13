import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';

export default function AccountDialog({
  open,
  onOpenChange,
  editingAccount,
  accountForm,
  setAccountForm,
  submitAccount,
  submittingAccount,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-xl">
        <div className="space-y-4">
          <Dialog.Title>{editingAccount ? '编辑租户' : '新增租户'}</Dialog.Title>
          <div className="grid gap-3">
            <Input
              size="sm"
              aria-label="名称"
              value={accountForm.name}
              onChange={event =>
                setAccountForm(current => ({ ...current, name: event.target.value }))
              }
              placeholder="显示名称"
            />
            <Input
              size="sm"
              aria-label="租户 ID"
              value={accountForm.tenantId}
              onChange={event =>
                setAccountForm(current => ({ ...current, tenantId: event.target.value }))
              }
              placeholder="tenant_id"
            />
            <Input
              size="sm"
              aria-label="客户端 ID"
              value={accountForm.clientId}
              onChange={event =>
                setAccountForm(current => ({ ...current, clientId: event.target.value }))
              }
              placeholder="client_id"
            />
            <Input
              size="sm"
              aria-label="客户端密钥"
              value={accountForm.clientSecret}
              onChange={event =>
                setAccountForm(current => ({ ...current, clientSecret: event.target.value }))
              }
              placeholder={editingAccount ? '留空则保持原密钥' : 'client_secret'}
            />
            <Textarea
              aria-label="描述"
              value={accountForm.description}
              onChange={event =>
                setAccountForm(current => ({ ...current, description: event.target.value }))
              }
              placeholder="备注或租户说明"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={submitAccount}
              disabled={submittingAccount}
            >
              {submittingAccount ? '保存中...' : '保存'}
            </Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
