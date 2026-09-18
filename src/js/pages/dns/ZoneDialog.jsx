import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Plus } from '../../components/Icons.jsx';

function ZoneDialog({
  open,
  onOpenChange,
  zoneForm,
  setZoneForm,
  loading,
  onSaveZone,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>添加域名</LayerDialog.Title>
        <LayerDialog.Body>
          <div className="@container flex flex-col gap-4">
            <Input size="sm" label="域名" value={zoneForm.name} onChange={(event) => setZoneForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="example.com" />
            <div className="flex items-center justify-between rounded-md border border-kumo-line p-3">
              <div>
                <div className="text-sm font-medium text-kumo-strong">扫描现有 DNS 记录</div>
                <div className="text-xs text-kumo-subtle">对应 Cloudflare jump_start 参数。</div>
              </div>
              <Switch checked={zoneForm.jumpStart} onCheckedChange={(checked) => setZoneForm((prev) => ({ ...prev, jumpStart: Boolean(checked) }))} />
            </div>
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSaveZone} loading={loading.saveZone} icon={<Plus className="h-4 w-4" />}>
            添加
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default ZoneDialog;
