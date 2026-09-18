import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
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
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>导入华为云账号</LayerDialog.Title>
        <LayerDialog.Description>粘贴从「导出」得到的 JSON（含 AK/SK/SSH 凭据），或按 {`{"accounts":[...]}`} 格式。</LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-3">
            <Textarea value={importText} onChange={(e) => setImportText(e.target.value)} placeholder={'{"accounts":[{"name":"...","accessKeyId":"...","secretAccessKey":"...","sshUser":"root","sshPort":22}]}'} className="min-h-[11rem] font-mono text-xs" />
            <Switch size="sm" label="覆盖当前已有账号" controlFirst={false} checked={importOverwrite} onCheckedChange={(checked) => setImportOverwrite(Boolean(checked))} />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={submitImport} loading={importingAccounts} disabled={!importText.trim()}>
            导入
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
