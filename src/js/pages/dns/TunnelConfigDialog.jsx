import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Save } from '../../components/Icons.jsx';

function TunnelConfigDialog({
  open,
  onOpenChange,
  tunnelConfigState,
  setTunnelConfigState,
  loading,
  onSaveTunnelConfig,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>Tunnel 配置：{tunnelConfigState.tunnel?.name}</LayerDialog.Title>
        <LayerDialog.Body>
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
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSaveTunnelConfig} icon={<Save className="h-4 w-4" />}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default TunnelConfigDialog;
