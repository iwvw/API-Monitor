import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

export default function BucketDialog({ open, onOpenChange, bucketForm, setBucketForm, submittingBucket, onSubmit }) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>创建存储桶</LayerDialog.Title>
        <LayerDialog.Description>存储桶名称全局唯一，创建后不可重名。</LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-3">
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">名称 *</span>
              <Input size="sm" value={bucketForm.name} onChange={(event) => setBucketForm({ ...bucketForm, name: event.target.value })} placeholder="全局唯一存储桶名称" />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">位置</span>
              <Input size="sm" value={bucketForm.location} onChange={(event) => setBucketForm({ ...bucketForm, location: event.target.value })} placeholder="例如 us-central1" />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">存储类别</span>
              <Select alignItemWithTrigger size="sm" value={bucketForm.storageClass} onValueChange={(value) => setBucketForm({ ...bucketForm, storageClass: value })} items={[
                { value: 'STANDARD', label: '标准（STANDARD）' },
                { value: 'NEARLINE', label: '近线（NEARLINE）' },
                { value: 'COLDLINE', label: '冷线（COLDLINE）' },
                { value: 'ARCHIVE', label: '归档（ARCHIVE）' },
              ]} />
            </label>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSubmit} loading={submittingBucket}>
            创建
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
