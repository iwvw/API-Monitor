import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function FlexusOpsDialog({ flexusOps, setFlexusOps, submitFlexusOps }) {
  return (
    <LayerDialog.Root open={flexusOps.open} onOpenChange={(open) => setFlexusOps((cur) => ({ ...cur, open }))}>
      <LayerDialog.Content size="sm">
        <LayerDialog.Title>{flexusOps.type === 'rename' ? '修改实例名称' : '重置实例密码'}</LayerDialog.Title>
        <LayerDialog.Description>Flexus L 实例「{flexusOps.instance?.name || '-'}」</LayerDialog.Description>
        <LayerDialog.Body>
          {flexusOps.type === 'rename' ? (
            <Input label="新名称" value={flexusOps.value} onChange={(e) => setFlexusOps((cur) => ({ ...cur, value: e.target.value }))} placeholder="请输入新名称" />
          ) : (
            <Input label="新密码" type="password" value={flexusOps.value} onChange={(e) => setFlexusOps((cur) => ({ ...cur, value: e.target.value }))} placeholder="请输入新密码" />
          )}
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={submitFlexusOps}>
            确定
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
