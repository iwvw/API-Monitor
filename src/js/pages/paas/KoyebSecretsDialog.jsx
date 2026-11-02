import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { SensitiveInput } from '@cloudflare/kumo/components/sensitive-input';
import { Badge, Loader } from '@cloudflare/kumo';
import { Plus, Settings, Trash } from '../../components/Icons.jsx';

export default function KoyebSecretsDialog({ koyebSecretsTarget, setKoyebSecretsTarget, koyebNewSecretName, setKoyebNewSecretName, koyebNewSecretValue, setKoyebNewSecretValue, addKoyebSecret, koyebSecretsLoading, koyebSecretsError, koyebSecrets, updateKoyebSecretValue, isArmed, deleteKoyebSecret }) {
  return (
      <LayerDialog.Root open={!!koyebSecretsTarget} onOpenChange={(open) => { if (!open) setKoyebSecretsTarget(null); }}>
        <LayerDialog.Content size="base">
          <LayerDialog.Title>Secrets 密钥管理</LayerDialog.Title>
          <LayerDialog.Description>{koyebSecretsTarget ? `${koyebSecretsTarget.name} · 组织级密钥，可在部署环境变量中以 secret 引用` : ''}</LayerDialog.Description>
          <LayerDialog.Body>
            <div className="flex flex-col gap-4">
              <div className="flex gap-2">
                <Input size="sm" aria-label="密钥名称" type="text" value={koyebNewSecretName} onChange={(e) => setKoyebNewSecretName(e.target.value)} placeholder="密钥名称（如 DB_PASSWORD）" className="w-48 text-xs" />
                <SensitiveInput size="sm" aria-label="密钥值" value={koyebNewSecretValue} onValueChange={setKoyebNewSecretValue} placeholder="密钥值" className="flex-1 text-xs" onKeyDown={(e) => { if (e.key === 'Enter') addKoyebSecret(); }} />
                <Button size="sm" onClick={addKoyebSecret} icon={<Plus className="h-3.5 w-3.5" />} className="text-xs">添加</Button>
              </div>
              <div className="min-h-0 flex-1">
                {koyebSecretsLoading ? (
                  <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载密钥...</div>
                ) : koyebSecretsError ? (
                  <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebSecretsError}</div>
                ) : koyebSecrets.length === 0 ? (
                  <div className="py-12 text-center text-kumo-subtle text-sm">暂无密钥</div>
                ) : (
                  <div className="space-y-2">
                    {koyebSecrets.map((secret) => (
                      <div key={secret.id} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-mono text-xs text-kumo-strong">{secret.name}</span>
                            <Badge variant="neutral">{secret.type || 'SIMPLE'}</Badge>
                          </div>
                          <div className="mt-1 text-[10px] text-kumo-subtle">
                            更新于 {secret.updated_at ? new Date(secret.updated_at).toLocaleString() : '-'}
                            {secret.project_id ? ' · 项目级' : ' · 组织级'}
                          </div>
                        </div>
                        <div className="flex shrink-0 items-center gap-1">
                          <Button shape="square" size="xs" variant="secondary" aria-label="更新值" title="更新值" onClick={() => updateKoyebSecretValue(secret)} icon={<Settings className="h-3 w-3" />} />
                          <Button shape="square" size="xs" variant={isArmed(`koyeb-secret:${secret.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除密钥" title="删除密钥" onClick={() => deleteKoyebSecret(secret)} icon={<Trash className="h-3 w-3" />} />
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
