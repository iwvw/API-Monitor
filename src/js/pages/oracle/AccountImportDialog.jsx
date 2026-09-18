import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Switch } from '@cloudflare/kumo/components/switch';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Download } from '../../components/Icons.jsx';

export default function AccountImportDialog({ open, onOpenChange, accountImportFileRef, accountImportText, setAccountImportText, accountImportFileName, accountImportOverwrite, setAccountImportOverwrite, submitImportAccounts, importingAccounts }) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>导入 Oracle 账号</LayerDialog.Title>
        <LayerDialog.Description>
          支持导入本页导出的 Oracle 账号 JSON，可选文件或直接粘贴。
        </LayerDialog.Description>
        <LayerDialog.Body>
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
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            onClick={submitImportAccounts}
            disabled={!accountImportText.trim()}
            loading={importingAccounts}
          >
            导入
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
