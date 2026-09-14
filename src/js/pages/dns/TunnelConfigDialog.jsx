import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Save } from '../../components/Icons.jsx';

function TunnelConfigDialog({
  tunnelConfigState,
  setTunnelConfigState,
  loading,
  onCloseModal,
  onSaveTunnelConfig,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">Tunnel 配置：{tunnelConfigState.tunnel?.name}</Dialog.Title>
      {loading.tunnelConfig ? (
        <SkeletonLine className="h-80 w-full" />
      ) : (
        <CodeEditor
          label="配置 JSON"
          language="json"
          value={tunnelConfigState.text}
          onChange={(text) => setTunnelConfigState((prev) => ({ ...prev, text }))}
          minHeight="24rem"
        />
      )}
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onSaveTunnelConfig} icon={<Save className="h-4 w-4" />}>保存</Button>
      </div>
    </div>
  );
}

export default TunnelConfigDialog;
