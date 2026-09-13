import React from 'react';
import { ClipboardText } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';

function TunnelTokenDialog({
  tunnelTokenState,
  loading,
  onCloseModal,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">Tunnel 令牌：{tunnelTokenState.tunnel?.name}</Dialog.Title>
      {loading.tunnelToken ? (
        <SkeletonLine className="h-28 w-full" />
      ) : (
        <>
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
        </>
      )}
    </div>
  );
}

export default TunnelTokenDialog;
