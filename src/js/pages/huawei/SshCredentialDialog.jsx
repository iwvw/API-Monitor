import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { SensitiveInput } from '@cloudflare/kumo/components/sensitive-input';

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
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>SSH 凭据 · {sshCredAccount?.name || ''}</LayerDialog.Title>
        <LayerDialog.Description>用于面板内终端直连实例。私钥优先、密码兜底；编辑时留空表示不更换。</LayerDialog.Description>
        <LayerDialog.Body>
          <div className="@container flex flex-col gap-3">
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Input label="SSH 用户" value={sshCredForm.sshUser} onChange={(e) => setSshCredForm({ ...sshCredForm, sshUser: e.target.value })} placeholder="默认 root" />
              <Input label="SSH 端口" type="number" value={sshCredForm.sshPort} onChange={(e) => setSshCredForm({ ...sshCredForm, sshPort: Number(e.target.value) })} placeholder="默认 22" />
            </div>
            <div className="grid gap-3 cq-md:grid-cols-2">
              <Textarea label="SSH 私钥（可选）" value={sshCredForm.sshPrivateKey} onChange={(e) => setSshCredForm({ ...sshCredForm, sshPrivateKey: e.target.value })} placeholder="粘贴 PEM 私钥，编辑时留空不更换" className="min-h-[7rem]" />
              <SensitiveInput label="SSH 密码（可选）" value={sshCredForm.sshPassword} onValueChange={(sshPassword) => setSshCredForm({ ...sshCredForm, sshPassword })} placeholder="与私钥二选一" />
            </div>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={saveSshCred} loading={savingSshCred}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
