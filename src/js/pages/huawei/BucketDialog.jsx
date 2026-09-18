import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function BucketDialog({ open, onOpenChange, bucketForm, setBucketForm, createBucket, savingBucket }) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>新建 OBS 桶</LayerDialog.Title>
        <LayerDialog.Description>桶名称全网唯一，创建后不可重名；将创建于当前所选区域。</LayerDialog.Description>
        <LayerDialog.Body>
          <Input label="桶名称" value={bucketForm.name} onChange={(e) => setBucketForm({ name: e.target.value })} placeholder="小写字母/数字/中划线" />
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={createBucket} loading={savingBucket}>
            创建
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
