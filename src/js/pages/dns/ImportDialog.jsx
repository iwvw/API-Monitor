import React from 'react';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Upload } from '../../components/Icons.jsx';
import CodeEditor from '../../components/ui/CodeEditor.jsx';

function ImportDialog({
  importState,
  setImportState,
  onCloseModal,
  onSubmitImport,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">
        导入{importState.kind === 'accounts' ? '账号' : importState.kind === 'templates' ? '模板' : 'DNS 记录'}
      </Dialog.Title>
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
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onSubmitImport} icon={<Upload className="h-4 w-4" />}>导入</Button>
      </div>
    </div>
  );
}

export default ImportDialog;
