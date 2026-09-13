import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Badge, Loader } from '@cloudflare/kumo';
import { Plus, RefreshCw, Trash, X } from '../../components/Icons.jsx';

export default function KoyebDomainsDialog({ koyebDomainsTarget, setKoyebDomainsTarget, koyebNewDomain, setKoyebNewDomain, addKoyebDomain, koyebDomainsLoading, koyebDomainsError, koyebDomains, getKoyebStatusText, refreshKoyebDomain, isArmed, deleteKoyebDomain }) {
  return (
      <Dialog.Root open={!!koyebDomainsTarget} onOpenChange={(open) => { if (!open) setKoyebDomainsTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(44rem,calc(100vw-2rem))] !max-w-[min(44rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-semibold text-kumo-strong">域名管理</Dialog.Title>
              {koyebDomainsTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebDomainsTarget.app ? `应用 ${koyebDomainsTarget.app.name}` : '组织全部域名'}</p>}
            </div>
            <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebDomainsTarget(null)} icon={<X className="h-4 w-4" />} />
          </div>
          <div className="border-b border-kumo-line p-4">
            <div className="flex gap-2">
              <Input size="sm" aria-label="新域名" type="text" value={koyebNewDomain} onChange={(e) => setKoyebNewDomain(e.target.value)} placeholder="app.example.com" className="flex-1 text-xs" onKeyDown={(e) => { if (e.key === 'Enter') addKoyebDomain(); }} />
              <Button size="sm" onClick={addKoyebDomain} icon={<Plus className="h-3.5 w-3.5" />} className="text-xs">添加</Button>
            </div>
            <p className="mt-1.5 text-[10px] text-kumo-subtle">绑定后按 Koyeb 提示配置 DNS 记录（CNAME/A 记录）。</p>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {koyebDomainsLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载域名...</div>
            ) : koyebDomainsError ? (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebDomainsError}</div>
            ) : koyebDomains.length === 0 ? (
              <div className="py-12 text-center text-kumo-subtle text-sm">暂无域名</div>
            ) : (
              <div className="space-y-2">
                {koyebDomains.map((domain) => (
                  <div key={domain.id} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-xs text-kumo-strong">{domain.name}</span>
                        <Badge variant={domain.type === 'AUTOASSIGNED' ? 'outline' : 'success'}>{domain.type === 'AUTOASSIGNED' ? '自动' : '自定义'}</Badge>
                      </div>
                      <div className="mt-1 text-[10px] text-kumo-subtle">
                        状态 {getKoyebStatusText(domain.status)}
                        {domain.verified_at ? ` · 已验证 ${new Date(domain.verified_at).toLocaleString()}` : ''}
                        {domain.intended_cname ? ` · CNAME: ${domain.intended_cname}` : ''}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button shape="square" size="xs" variant="secondary" aria-label="刷新校验" title="刷新校验" onClick={() => refreshKoyebDomain(domain)} icon={<RefreshCw className="h-3 w-3" />} />
                      <Button shape="square" size="xs" variant={isArmed(`koyeb-domain:${domain.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除域名" title="删除域名" onClick={() => deleteKoyebDomain(domain)} icon={<Trash className="h-3 w-3" />} />
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
