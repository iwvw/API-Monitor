import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function FlexusOpsDialog({ flexusOps, setFlexusOps, submitFlexusOps }) {
  return (
    <Dialog.Root open={flexusOps.open} onOpenChange={(open) => setFlexusOps((cur) => ({ ...cur, open }))}>
      <Dialog className="@container !w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
        <Dialog.Title className="mb-1 text-base font-semibold text-kumo-strong">{flexusOps.type === 'rename' ? '修改实例名称' : '重置实例密码'}</Dialog.Title>
        <Dialog.Description className="mb-4 text-xs text-kumo-subtle">Flexus L 实例「{flexusOps.instance?.name || '-'}」</Dialog.Description>
        <div className="flex flex-col gap-3">
          {flexusOps.type === 'rename' ? (
            <Input label="新名称" value={flexusOps.value} onChange={(e) => setFlexusOps((cur) => ({ ...cur, value: e.target.value }))} placeholder="请输入新名称" />
          ) : (
            <Input label="新密码" type="password" value={flexusOps.value} onChange={(e) => setFlexusOps((cur) => ({ ...cur, value: e.target.value }))} placeholder="请输入新密码" />
          )}
          <div className="mt-2 flex items-center justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => setFlexusOps((cur) => ({ ...cur, open: false }))}>取消</Button>
            <Button size="sm" onClick={submitFlexusOps}>确定</Button>
          </div>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
