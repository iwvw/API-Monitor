import React from 'react';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Save } from '../../components/Icons.jsx';

function AccountDialog({
  open,
  onOpenChange,
  modal,
  accountForm,
  setAccountForm,
  loading,
  onSaveAccount,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>
          {modal.data ? '编辑 Cloudflare 账号' : '添加 Cloudflare 账号'}
        </LayerDialog.Title>
        <LayerDialog.Description>
          推荐使用 API Token，邮箱可留空；账户 Token（cfat_）需额外填 Account ID。Origin CA Key（v1.0-）已弃用。
        </LayerDialog.Description>
        <LayerDialog.Body>
          <div className="@container flex flex-col gap-4">
            <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
              <Input size="sm" label="备注名称" value={accountForm.name} onChange={(event) => setAccountForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="生产账号" />
              <Input size="sm" label="邮箱" type="email" value={accountForm.email} onChange={(event) => setAccountForm((prev) => ({ ...prev, email: event.target.value }))} placeholder="name@example.com" />
            </div>
            <Input size="sm"
              label="Account ID"
              value={accountForm.cfAccountId}
              onChange={(event) => setAccountForm((prev) => ({ ...prev, cfAccountId: event.target.value }))}
              className="font-mono"
            />
            <Input size="sm"
              label="API Token / 全局 API Key"
              type="text"
              name="cf_credential_value"
              value={accountForm.apiToken}
              onChange={(event) => setAccountForm((prev) => ({ ...prev, apiToken: event.target.value }))}
              placeholder={modal.data ? '不修改请留空' : 'cfat_... 或普通 API Token'}
              autoComplete="off"
              data-1p-ignore
              data-lpignore="true"
              data-bwignore="true"
              data-form-type="other"
              spellCheck={false}
              className="font-mono"
            />
            <Checkbox
              checked={accountForm.skipVerify}
              onCheckedChange={(checked) => setAccountForm((prev) => ({ ...prev, skipVerify: Boolean(checked) }))}
              label="跳过 API 验证"
            />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSaveAccount} loading={loading.saveAccount} icon={<Save className="h-4 w-4" />}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default AccountDialog;
