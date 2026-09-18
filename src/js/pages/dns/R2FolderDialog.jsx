import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

function R2FolderDialog({
  open,
  onOpenChange,
  r2FolderForm,
  setR2FolderForm,
  r2CurrentPrefix,
  loading,
  onCreateR2Folder,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>新建文件夹</LayerDialog.Title>
        <LayerDialog.Description>
          在当前路径 {r2CurrentPrefix || '/'} 下创建文件夹。
        </LayerDialog.Description>
        <LayerDialog.Body>
          <Input
            size="sm"
            label="文件夹名称"
            value={r2FolderForm.name}
            onChange={(event) => setR2FolderForm({ name: event.target.value })}
            placeholder="assets"
            autoFocus
          />
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onCreateR2Folder} loading={loading.createR2Folder}>
            创建
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default R2FolderDialog;
