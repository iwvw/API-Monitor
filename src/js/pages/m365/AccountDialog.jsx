import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
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
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>{editingAccount ? '编辑租户' : '新增租户'}</LayerDialog.Title>
        <LayerDialog.Body>
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
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            onClick={submitAccount}
            loading={submittingAccount}
          >
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
