import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

export default function BucketDialog({ open, onOpenChange, bucketForm, setBucketForm, submittingBucket, onSubmit }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(38rem,calc(100vw-2rem))] !max-w-[min(38rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">创建存储桶</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">存储桶名称全局唯一，创建后不可重名。</Dialog.Description>
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
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="button" size="sm" variant="primary" loading={submittingBucket} onClick={onSubmit}>创建</Button>
        </div>
      </div>
      </Dialog>
    </Dialog.Root>
  );
}
