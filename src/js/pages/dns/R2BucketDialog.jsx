import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

function R2BucketDialog({
  open,
  onOpenChange,
  r2BucketForm,
  setR2BucketForm,
  loading,
  onCreateR2Bucket,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>创建 R2 存储桶</LayerDialog.Title>
        <LayerDialog.Body>
          <div className="@container grid grid-cols-1 gap-4 cq-md:grid-cols-2">
            <Input size="sm" label="存储桶名称" value={r2BucketForm.name} onChange={(event) => setR2BucketForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="my-bucket" />
            <Input size="sm" label="位置" value={r2BucketForm.location} onChange={(event) => setR2BucketForm((prev) => ({ ...prev, location: event.target.value }))} placeholder="auto" />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onCreateR2Bucket} loading={loading.saveR2Bucket}>
            创建
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default R2BucketDialog;
