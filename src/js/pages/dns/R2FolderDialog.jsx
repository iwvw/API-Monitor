import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

function R2FolderDialog({
  r2FolderForm,
  setR2FolderForm,
  r2CurrentPrefix,
  loading,
  onCloseModal,
  onCreateR2Folder,
}) {
  return (
    <div className="flex flex-col gap-4">
      <div>
        <Dialog.Title className="text-base font-semibold text-kumo-strong">新建文件夹</Dialog.Title>
        <Dialog.Description className="mt-1 text-xs text-kumo-subtle">
          在当前路径 {r2CurrentPrefix || '/'} 下创建文件夹。
        </Dialog.Description>
      </div>
      <Input
        size="sm"
        label="文件夹名称"
        value={r2FolderForm.name}
        onChange={(event) => setR2FolderForm({ name: event.target.value })}
        placeholder="assets"
        autoFocus
      />
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onCreateR2Folder} disabled={loading.createR2Folder}>创建</Button>
      </div>
    </div>
  );
}

export default R2FolderDialog;
