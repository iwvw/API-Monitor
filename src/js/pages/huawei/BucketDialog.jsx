import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function BucketDialog({ open, onOpenChange, bucketForm, setBucketForm, createBucket, savingBucket }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">新建 OBS 桶</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">桶名称全网唯一，创建后不可重名；将创建于当前所选区域。</Dialog.Description>
        <div className="flex flex-col gap-3">
          <Input label="桶名称" value={bucketForm.name} onChange={(e) => setBucketForm({ name: e.target.value })} placeholder="小写字母/数字/中划线" />
          <div className="mt-2 flex items-center justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
            <Button size="sm" onClick={createBucket} disabled={savingBucket}>{savingBucket ? '创建中…' : '创建'}</Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
