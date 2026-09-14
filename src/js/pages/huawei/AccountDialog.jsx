import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

export default function AccountDialog({ open, onOpenChange, editingAccount, accountForm, setAccountForm, saveAccount, savingAccount }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{editingAccount ? '编辑华为云账号' : '新增华为云账号'}</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">使用 AK/SK 接入华为云，SK 将加密存储，列表不回显。</Dialog.Description>
        <div className="flex flex-col gap-3">
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
            <Input label="Secret Access Key" type="password" value={accountForm.secretAccessKey} onChange={(e) => setAccountForm({ ...accountForm, secretAccessKey: e.target.value })} placeholder={editingAccount ? '留空表示不更换' : 'SK 加密存储'} />
          </div>
          <div className="grid gap-3 cq-md:grid-cols-2">
            <Input label="默认区域" value={accountForm.defaultRegion} onChange={(e) => setAccountForm({ ...accountForm, defaultRegion: e.target.value })} placeholder="如 cn-north-4，可留空" />
            <Input label="默认项目 ID" value={accountForm.defaultProjectId} onChange={(e) => setAccountForm({ ...accountForm, defaultProjectId: e.target.value })} placeholder="验证后自动发现，可留空" />
          </div>
          <Input label="备注" value={accountForm.description} onChange={(e) => setAccountForm({ ...accountForm, description: e.target.value })} placeholder="可选" />
          <div className="mt-2 flex items-center justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
            <Button size="sm" onClick={saveAccount} disabled={savingAccount}>{savingAccount ? '保存中…' : '保存'}</Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
