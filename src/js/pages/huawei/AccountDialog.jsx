import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { SensitiveInput } from '@cloudflare/kumo/components/sensitive-input';
import { Select } from '@cloudflare/kumo/components/select';

export default function AccountDialog({ open, onOpenChange, editingAccount, accountForm, setAccountForm, saveAccount, savingAccount }) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>{editingAccount ? '编辑华为云账号' : '新增华为云账号'}</LayerDialog.Title>
        <LayerDialog.Description>使用 AK/SK 接入华为云，SK 将加密存储，列表不回显。</LayerDialog.Description>
        <LayerDialog.Body>
          <div className="@container flex flex-col gap-3">
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="账号名称" value={accountForm.name} onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })} placeholder="如：生产环境" />
              <Select
                alignItemWithTrigger
                aria-label="站点"
                value={accountForm.site}
                onValueChange={(v) => setAccountForm({ ...accountForm, site: v })}
                items={[
                  { value: 'cn', label: '国内站（myhuaweicloud.cn）' },
                  { value: 'intl', label: '国际站（myhuaweicloud.com）' },
                ]}
              />
            </div>
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="Access Key ID" value={accountForm.accessKeyId} onChange={(e) => setAccountForm({ ...accountForm, accessKeyId: e.target.value })} placeholder={editingAccount ? `当前：${editingAccount.accessKeyId}（留空不更换）` : 'AK 明文保存，列表脱敏显示'} />
              <SensitiveInput label="Secret Access Key" value={accountForm.secretAccessKey} onValueChange={(secretAccessKey) => setAccountForm({ ...accountForm, secretAccessKey })} placeholder={editingAccount ? '留空表示不更换' : 'SK 加密存储'} />
            </div>
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="默认区域" value={accountForm.defaultRegion} onChange={(e) => setAccountForm({ ...accountForm, defaultRegion: e.target.value })} placeholder="如 cn-north-4，可留空" />
              <Input label="默认项目 ID" value={accountForm.defaultProjectId} onChange={(e) => setAccountForm({ ...accountForm, defaultProjectId: e.target.value })} placeholder="验证后自动发现，可留空" />
            </div>
            <Input label="备注" value={accountForm.description} onChange={(e) => setAccountForm({ ...accountForm, description: e.target.value })} placeholder="可选" />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={saveAccount} loading={savingAccount}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
