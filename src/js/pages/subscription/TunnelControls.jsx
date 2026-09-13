import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { ClipboardText, LayerCard } from '@cloudflare/kumo';
import { sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { Plus } from '../../components/Icons.jsx';

export default function TunnelControls({ managedTunnels, preferredAddresses, onOpenPreferredModal }) {
  return (
    <LayerCard className="mb-3 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none ring-0">
      <LayerCard.Secondary className={sectionCardHeaderClass}>
        <div className="text-sm font-semibold text-kumo-strong">Tunnel 与优选地址</div>
        <div className="flex shrink-0 items-center gap-2">
          <Button className="cq-sm:hidden" size="sm" variant="secondary" onClick={() => onOpenPreferredModal(true)}><Plus className="h-3.5 w-3.5" />管理</Button>
          <Button className="hidden cq-sm:inline-flex" size="sm" variant="secondary" onClick={() => onOpenPreferredModal(true)}><Plus className="h-3.5 w-3.5" />优选地址</Button>
        </div>
      </LayerCard.Secondary>
      <LayerCard.Primary className="px-3 py-2.5 cq-sm:px-4">
        {managedTunnels.length > 0 || preferredAddresses.length > 0 ? (
          <div className="flex flex-col gap-2">
            {managedTunnels.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <span className="text-xs font-semibold text-kumo-subtle">Tunnel</span>
                {managedTunnels.map((item) => {
                  return (
                    <span key={item.server_id} className="inline-flex items-center gap-1.5">
                      <ClipboardText
                        size="sm"
                        text={item.server_name}
                        textToCopy={item.hostname}
                        className="min-w-0 max-w-64"
                        tooltip={{ text: item.last_error ? `${item.last_error}；复制 Tunnel 地址` : `复制 ${item.server_name} 的 Tunnel 地址`, copiedText: `已复制 ${item.server_name} 的 Tunnel 地址` }}
                        labels={{ copyAction: `复制 ${item.server_name} 的 Tunnel 地址` }}
                      />
                    </span>
                  );
                })}
              </div>
            )}
            {preferredAddresses.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <span className="text-xs font-semibold text-kumo-subtle">优选地址</span>
                {preferredAddresses.map((item) => (
                  <span key={item.id} className="inline-flex items-center gap-1.5">
                    <ClipboardText
                      size="sm"
                      text={`${item.address}:${item.port}`}
                      className={`min-w-0 max-w-64 ${item.enabled === false ? 'opacity-50' : ''}`}
                      tooltip={{ text: item.last_error ? `${item.last_error}；复制 ${item.name} 的地址` : `复制 ${item.name} 的地址`, copiedText: `已复制 ${item.name} 的地址` }}
                      labels={{ copyAction: `复制 ${item.name} 的地址` }}
                    />
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : (
          <span className="text-xs text-kumo-subtle">暂无 Tunnel 与优选地址</span>
        )}
      </LayerCard.Primary>
    </LayerCard>
  );
}
