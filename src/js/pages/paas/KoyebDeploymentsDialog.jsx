import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Badge, ClipboardText, Loader } from '@cloudflare/kumo';
import { RefreshCw, X } from '../../components/Icons.jsx';

export default function KoyebDeploymentsDialog({ koyebDeployTarget, setKoyebDeployTarget, openKoyebDeployments, koyebDeployLoading, koyebDeployError, koyebDeployments, getKoyebStatusTone, getKoyebStatusText, cancelKoyebDeployment }) {
  return (
      <LayerDialog.Root open={!!koyebDeployTarget} onOpenChange={(open) => { if (!open) setKoyebDeployTarget(null); }}>
        <LayerDialog.Content size="lg">
          <LayerDialog.Title>部署历史</LayerDialog.Title>
          <LayerDialog.Description>{koyebDeployTarget?.service?.name || ''}</LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-4">
              {koyebDeployTarget && (
                <div className="flex justify-end">
                  <Button size="sm" variant="secondary" onClick={() => openKoyebDeployments(koyebDeployTarget.account, koyebDeployTarget.service)} icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
                </div>
              )}
              <div className="min-h-0 flex-1">
                {koyebDeployLoading ? (
                  <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载部署记录...</div>
                ) : koyebDeployError ? (
                  <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebDeployError}</div>
                ) : koyebDeployments.length === 0 ? (
                  <div className="py-12 text-center text-kumo-subtle text-sm">暂无部署记录</div>
                ) : (
                  <div className="space-y-2">
                    {koyebDeployments.map((dep) => {
                      const image = dep.definition?.docker?.image || '';
                      const status = String(dep.status || '').toUpperCase();
                      const cancellable = ['PENDING', 'PROVISIONING', 'SCHEDULED', 'ALLOCATING', 'STARTING'].includes(status);
                      return (
                        <div key={dep.id} className="space-y-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                          <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                            <div className="min-w-0">
                              <ClipboardText size="sm" text={dep.id} />
                              {image && <div className="mt-1 truncate font-mono text-[10px] text-kumo-subtle">{image}</div>}
                            </div>
                            <div className="flex shrink-0 items-center gap-2">
                              <Badge variant={getKoyebStatusTone(status)} appearance="dot">{getKoyebStatusText(status)}</Badge>
                              {dep.version ? <Badge variant="neutral">v{dep.version}</Badge> : null}
                              {cancellable && (
                                <Button shape="square" size="xs" variant="secondary-destructive" aria-label="取消部署" title="取消部署" onClick={() => cancelKoyebDeployment(dep)} icon={<X className="h-3 w-3" />} />
                              )}
                            </div>
                          </div>
                          <div className="flex flex-wrap items-center gap-2 text-[10px] text-kumo-subtle">
                            <span>创建于 {dep.created_at ? new Date(dep.created_at).toLocaleString() : '-'}</span>
                            {dep.metadata?.git?.sha ? <span>· SHA {String(dep.metadata.git.sha).slice(0, 12)}</span> : null}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
