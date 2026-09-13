import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Download } from '../../components/Icons.jsx';

export default function AccountImportDialog({
  open,
  onOpenChange,
  accountImportInputRef,
  importAccountsFromFile,
  accountImportState,
  setAccountImportState,
  submitImportAccounts,
  importingAccounts,
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] p-5 cq-sm:w-full cq-sm:max-w-2xl">
        <div className="space-y-4">
          <Dialog.Title>导入租户</Dialog.Title>
          <input
            ref={accountImportInputRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={importAccountsFromFile}
          />
          <div className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-kumo-line/80 bg-kumo-recessed/10 px-3 py-2.5">
              <div className="min-w-0">
                <div className="text-sm font-medium text-kumo-strong">上传文件或直接粘贴</div>
                <div className="text-xs text-kumo-subtle">
                  仅支持 .json 格式。
                </div>
              </div>
              <Button
                size="sm"
                variant="secondary"
                icon={<Download className="h-3.5 w-3.5" />}
                onClick={() => accountImportInputRef.current?.click()}
              >
                选择文件
              </Button>
            </div>
            {accountImportState.fileName ? (
              <div className="rounded-md border border-kumo-line/70 bg-kumo-recessed/10 px-3 py-2 text-xs text-kumo-subtle">
                当前文件：
                <span className="font-medium text-kumo-strong">
                  {accountImportState.fileName}
                </span>
              </div>
            ) : null}
          </div>
          <div className="space-y-2">
            <div className="text-xs font-medium text-kumo-subtle">JSON 内容</div>
            <CodeEditor
              label="租户 JSON"
              language="json"
              value={accountImportState.text}
              onChange={text =>
                setAccountImportState(current => ({ ...current, text }))
              }
              minHeight="18rem"
              placeholder='{"accounts":[{"name":"Contoso","tenantId":"tenant-id","clientId":"client-id","clientSecret":"client-secret"}]}'
            />
          </div>
          <label className="flex items-center gap-2 text-xs text-kumo-subtle">
            <Checkbox
              checked={accountImportState.overwrite}
              onCheckedChange={checked =>
                setAccountImportState(current => ({ ...current, overwrite: !!checked }))
              }
            />
            覆盖现有租户数据
          </label>
          <div className="flex justify-end gap-2">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
            <Button
              size="sm"
              variant="primary"
              onClick={submitImportAccounts}
              disabled={importingAccounts}
            >
              {importingAccounts ? '导入中...' : '导入'}
            </Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
