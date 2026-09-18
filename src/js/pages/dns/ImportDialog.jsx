import React from 'react';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Upload } from '../../components/Icons.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';

function ImportDialog({
  open,
  onOpenChange,
  importState,
  setImportState,
  onSubmitImport,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>
          导入{importState.kind === 'accounts' ? '账号' : importState.kind === 'templates' ? '模板' : 'DNS 记录'}
        </LayerDialog.Title>
        <LayerDialog.Body>
          <div className="flex flex-col gap-4">
            <CodeEditor
              label="JSON 内容"
              language="json"
              value={importState.text}
              onChange={(text) => setImportState((prev) => ({ ...prev, text }))}
              minHeight="20rem"
              placeholder='{"records":[{"type":"A","name":"@","content":"1.1.1.1","ttl":1,"proxied":false}]}'
            />
            {importState.kind !== 'records' && (
              <Checkbox
                checked={importState.overwrite}
                onCheckedChange={(checked) => setImportState((prev) => ({ ...prev, overwrite: Boolean(checked) }))}
                label="覆盖现有数据"
              />
            )}
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSubmitImport} icon={<Upload className="h-4 w-4" />}>
            导入
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default ImportDialog;
