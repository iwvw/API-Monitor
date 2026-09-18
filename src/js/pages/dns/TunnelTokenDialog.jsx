import React from 'react';
import { ClipboardText } from '@cloudflare/kumo';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';

function TunnelTokenDialog({
  open,
  onOpenChange,
  tunnelTokenState,
  loading,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>Tunnel 令牌：{tunnelTokenState.tunnel?.name}</LayerDialog.Title>
        <LayerDialog.Body>
          {loading.tunnelToken ? (
            <SkeletonLine className="h-28 w-full" />
          ) : (
            <div className="flex flex-col gap-2">
              <ClipboardText
                size="sm"
                text={tunnelTokenState.token}
                className="w-full"
                tooltip={{ text: '复制令牌', copiedText: '令牌已复制', side: 'top' }}
                labels={{ copyAction: '复制 Tunnel 令牌' }}
              />
              <ClipboardText
                size="sm"
                text={`cloudflared tunnel run --token ${tunnelTokenState.token}`}
                className="w-full"
                tooltip={{ text: '复制命令', copiedText: '运行命令已复制', side: 'top' }}
                labels={{ copyAction: '复制 cloudflared 运行命令' }}
              />
            </div>
          )}
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default TunnelTokenDialog;
