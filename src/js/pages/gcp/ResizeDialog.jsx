import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { formatGb } from './utils.jsx';

export default function ResizeDialog({ open, onOpenChange, resizeTarget, resizeSize, setResizeSize, onSubmit }) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>磁盘扩容</LayerDialog.Title>
        <LayerDialog.Description>磁盘容量只增不减，扩容为异步操作。</LayerDialog.Description>
        <LayerDialog.Body>
          <div className="flex flex-col gap-3">
            <p className="text-sm text-kumo-subtle">磁盘「{resizeTarget?.name}」当前大小为 {formatGb(resizeTarget?.sizeGb)}。</p>
            <label className="flex flex-col gap-1">
              <span className="text-sm text-kumo-subtle">目标大小（GB）</span>
              <Input size="sm" type="number" value={resizeSize} onChange={(event) => setResizeSize(event.target.value)} />
            </label>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSubmit}>
            扩容
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
