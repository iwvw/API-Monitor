import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Switch } from '@cloudflare/kumo/components/switch';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Download } from '../../components/Icons.jsx';

export default function AccountImportDialog({ open, onOpenChange, accountImportFileRef, accountImportText, setAccountImportText, accountImportFileName, accountImportOverwrite, setAccountImportOverwrite, submitImportAccounts, importingAccounts }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="!w-[min(42rem,calc(100vw-2rem))] !max-w-[min(42rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">导入 Oracle 账号</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">
          支持导入本页导出的 Oracle 账号 JSON，可选文件或直接粘贴。
        </Dialog.Description>
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => accountImportFileRef.current?.click()}
              icon={<Download className="h-3.5 w-3.5" />}
            >
              选择 JSON 文件
            </Button>
            <div className="min-w-0 text-xs text-kumo-subtle">
              {accountImportFileName || '未选择文件'}
            </div>
          </div>
          <CodeEditor
            label="导入 Oracle 账号 JSON"
            language="json"
            value={accountImportText}
            onChange={setAccountImportText}
            minHeight="14rem"
            placeholder={`{\n  "version": "1.0",\n  "accounts": []\n}`}
          />
          <Switch
            size="sm"
            label="覆盖当前已有账号"
            controlFirst={false}
            checked={accountImportOverwrite}
            onCheckedChange={(checked) => setAccountImportOverwrite(Boolean(checked))}
          />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
            <Button type="button" onClick={submitImportAccounts} disabled={importingAccounts || !accountImportText.trim()}>
              {importingAccounts ? '导入中...' : '导入'}
            </Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
