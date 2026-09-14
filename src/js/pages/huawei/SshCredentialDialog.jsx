import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';

export default function SshCredentialDialog({
  open,
  onOpenChange,
  sshCredAccount,
  sshCredForm,
  setSshCredForm,
  saveSshCred,
  savingSshCred,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">SSH 凭据 · {sshCredAccount?.name || ''}</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">用于面板内终端直连实例。私钥优先、密码兜底；编辑时留空表示不更换。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <div className="grid gap-3 cq-md:grid-cols-2">
            <Input label="SSH 用户" value={sshCredForm.sshUser} onChange={(e) => setSshCredForm({ ...sshCredForm, sshUser: e.target.value })} placeholder="默认 root" />
            <Input label="SSH 端口" type="number" value={sshCredForm.sshPort} onChange={(e) => setSshCredForm({ ...sshCredForm, sshPort: Number(e.target.value) })} placeholder="默认 22" />
          </div>
          <div className="grid gap-3 cq-md:grid-cols-2">
            <Textarea label="SSH 私钥（可选）" value={sshCredForm.sshPrivateKey} onChange={(e) => setSshCredForm({ ...sshCredForm, sshPrivateKey: e.target.value })} placeholder="粘贴 PEM 私钥，编辑时留空不更换" className="min-h-[7rem]" />
            <Input label="SSH 密码（可选）" type="password" value={sshCredForm.sshPassword} onChange={(e) => setSshCredForm({ ...sshCredForm, sshPassword: e.target.value })} placeholder="与私钥二选一" />
          </div>
          <div className="mt-2 flex items-center justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
            <Button size="sm" onClick={saveSshCred} disabled={savingSshCred}>{savingSshCred ? '保存中…' : '保存'}</Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
