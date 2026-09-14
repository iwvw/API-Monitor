import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Badge, Loader } from '@cloudflare/kumo';
import { Plus, Settings, Trash, X } from '../../components/Icons.jsx';

export default function KoyebSecretsDialog({ koyebSecretsTarget, setKoyebSecretsTarget, koyebNewSecretName, setKoyebNewSecretName, koyebNewSecretValue, setKoyebNewSecretValue, addKoyebSecret, koyebSecretsLoading, koyebSecretsError, koyebSecrets, updateKoyebSecretValue, isArmed, deleteKoyebSecret }) {
  return (
      <Dialog.Root open={!!koyebSecretsTarget} onOpenChange={(open) => { if (!open) setKoyebSecretsTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(44rem,calc(100vw-2rem))] !max-w-[min(44rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-semibold text-kumo-strong">Secrets 密钥管理</Dialog.Title>
              {koyebSecretsTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebSecretsTarget.name} · 组织级密钥，可在部署环境变量中以 secret 引用</p>}
            </div>
            <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebSecretsTarget(null)} icon={<X className="h-4 w-4" />} />
          </div>
          <div className="border-b border-kumo-line p-4">
            <div className="flex gap-2">
              <Input size="sm" aria-label="密钥名称" type="text" value={koyebNewSecretName} onChange={(e) => setKoyebNewSecretName(e.target.value)} placeholder="密钥名称（如 DB_PASSWORD）" className="w-48 text-xs" />
              <Input size="sm" aria-label="密钥值" type="password" value={koyebNewSecretValue} onChange={(e) => setKoyebNewSecretValue(e.target.value)} placeholder="密钥值" className="flex-1 text-xs" onKeyDown={(e) => { if (e.key === 'Enter') addKoyebSecret(); }} />
              <Button size="sm" onClick={addKoyebSecret} icon={<Plus className="h-3.5 w-3.5" />} className="text-xs">添加</Button>
            </div>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
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
        </Dialog>
      </Dialog.Root>
  );
}
