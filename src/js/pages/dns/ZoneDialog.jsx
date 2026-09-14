import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Plus } from '../../components/Icons.jsx';

function ZoneDialog({
  zoneForm,
  setZoneForm,
  loading,
  onCloseModal,
  onSaveZone,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">添加域名</Dialog.Title>
      <Input size="sm" label="域名" value={zoneForm.name} onChange={(event) => setZoneForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="example.com" />
      <div className="flex items-center justify-between rounded-md border border-kumo-line p-3">
        <div>
          <div className="text-sm font-medium text-kumo-strong">扫描现有 DNS 记录</div>
          <div className="text-xs text-kumo-subtle">对应 Cloudflare jump_start 参数。</div>
        </div>
        <Switch checked={zoneForm.jumpStart} onCheckedChange={(checked) => setZoneForm((prev) => ({ ...prev, jumpStart: Boolean(checked) }))} />
      </div>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onSaveZone} disabled={loading.saveZone} icon={<Plus className="h-4 w-4" />}>
          添加
        </Button>
      </div>
    </div>
  );
}

export default ZoneDialog;
