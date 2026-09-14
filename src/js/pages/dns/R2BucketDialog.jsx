import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

function R2BucketDialog({
  r2BucketForm,
  setR2BucketForm,
  loading,
  onCloseModal,
  onCreateR2Bucket,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">创建 R2 存储桶</Dialog.Title>
      <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
        <Input size="sm" label="存储桶名称" value={r2BucketForm.name} onChange={(event) => setR2BucketForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="my-bucket" />
        <Input size="sm" label="位置" value={r2BucketForm.location} onChange={(event) => setR2BucketForm((prev) => ({ ...prev, location: event.target.value }))} placeholder="auto" />
      </div>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onCreateR2Bucket} disabled={loading.saveR2Bucket}>创建</Button>
      </div>
    </div>
  );
}

export default R2BucketDialog;
