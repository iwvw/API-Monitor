import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';

function TunnelCreateDialog({
  tunnelForm,
  setTunnelForm,
  loading,
  onCloseModal,
  onCreateTunnel,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">创建 Tunnel</Dialog.Title>
      <Input size="sm" label="Tunnel 名称" value={tunnelForm.name} onChange={(event) => setTunnelForm({ name: event.target.value })} placeholder="my-tunnel" />
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onCreateTunnel} disabled={loading.saveTunnel}>创建</Button>
      </div>
    </div>
  );
}

export default TunnelCreateDialog;
