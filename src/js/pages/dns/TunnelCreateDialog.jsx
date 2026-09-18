import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

function TunnelCreateDialog({
  open,
  onOpenChange,
  tunnelForm,
  setTunnelForm,
  loading,
  onCreateTunnel,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>创建 Tunnel</LayerDialog.Title>
        <LayerDialog.Body>
          <Input size="sm" label="Tunnel 名称" value={tunnelForm.name} onChange={(event) => setTunnelForm({ name: event.target.value })} placeholder="my-tunnel" />
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onCreateTunnel} loading={loading.saveTunnel}>
            创建
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default TunnelCreateDialog;
