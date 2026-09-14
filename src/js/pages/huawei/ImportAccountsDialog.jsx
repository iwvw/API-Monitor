import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Textarea } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';

export default function ImportAccountsDialog({
  open,
  onOpenChange,
  importText,
  setImportText,
  importOverwrite,
  setImportOverwrite,
  submitImport,
  importingAccounts,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">导入华为云账号</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">粘贴从「导出」得到的 JSON（含 AK/SK/SSH 凭据），或按 {`{"accounts":[...]}`} 格式。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <Textarea value={importText} onChange={(e) => setImportText(e.target.value)} placeholder={'{"accounts":[{"name":"...","accessKeyId":"...","secretAccessKey":"...","sshUser":"root","sshPort":22}]}'} className="min-h-[11rem] font-mono text-xs" />
          <Switch size="sm" label="覆盖当前已有账号" controlFirst={false} checked={importOverwrite} onCheckedChange={(checked) => setImportOverwrite(Boolean(checked))} />
          <div className="mt-2 flex items-center justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
            <Button size="sm" onClick={submitImport} disabled={importingAccounts || !importText.trim()}>{importingAccounts ? '导入中…' : '导入'}</Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
