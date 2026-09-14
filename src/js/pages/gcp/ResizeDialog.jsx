import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { formatGb } from './utils.jsx';

export default function ResizeDialog({ open, onOpenChange, resizeTarget, resizeSize, setResizeSize, onSubmit }) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog className="@container !w-[min(36rem,calc(100vw-2rem))] !max-w-[min(36rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">磁盘扩容</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">磁盘容量只增不减，扩容为异步操作。</Dialog.Description>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-kumo-subtle">磁盘「{resizeTarget?.name}」当前大小为 {formatGb(resizeTarget?.sizeGb)}。</p>
        <label className="flex flex-col gap-1">
          <span className="text-sm text-kumo-subtle">目标大小（GB）</span>
          <Input size="sm" type="number" value={resizeSize} onChange={(event) => setResizeSize(event.target.value)} />
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" size="sm" variant="secondary" onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="button" size="sm" variant="primary" onClick={onSubmit}>扩容</Button>
        </div>
      </div>
      </Dialog>
    </Dialog.Root>
  );
}
