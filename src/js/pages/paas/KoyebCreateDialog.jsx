import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';

export default function KoyebCreateDialog({ koyebCreateTarget, setKoyebCreateTarget, koyebCreateForm, setKoyebCreateForm, koyebCatalogInstances, koyebCatalogRegions, koyebCreateError, createKoyebService, koyebCreateSaving }) {
  return (
      <Dialog.Root open={!!koyebCreateTarget} onOpenChange={(open) => { if (!open) setKoyebCreateTarget(null); }}>
        <Dialog className="flex max-h-[90vh] !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] flex-col p-6">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong mb-1">新建服务</Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            {koyebCreateTarget ? `在应用 ${koyebCreateTarget.app.name} 下创建服务` : ''}
          </Dialog.Description>
          <div className="space-y-3 overflow-y-auto">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">服务名称</label>
                <Input size="sm" aria-label="服务名称" type="text" value={koyebCreateForm.name} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, name: e.target.value }))} placeholder="web" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">类型</label>
                <Select alignItemWithTrigger
                  aria-label="服务类型" size="sm"
                  value={koyebCreateForm.type}
                  onValueChange={(value) => setKoyebCreateForm((f) => ({ ...f, type: String(value) }))}
                  items={[{ value: 'web', label: 'web' }, { value: 'worker', label: 'worker' }, { value: 'job', label: 'job' }]}
                  className="w-full"
                />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-semibold text-kumo-subtle">镜像地址</label>
              <Input size="sm" aria-label="镜像地址" type="text" value={koyebCreateForm.image} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, image: e.target.value }))} placeholder="registry.hub.docker.com/xxx/app:latest" className="w-full text-xs" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">启动命令</label>
                <Input size="sm" aria-label="启动命令" type="text" value={koyebCreateForm.command} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, command: e.target.value }))} placeholder="留空使用镜像默认" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">端口（port:protocol）</label>
                <Input size="sm" aria-label="端口" type="text" value={koyebCreateForm.ports} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, ports: e.target.value }))} placeholder="8080:http" className="w-full text-xs" />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">实例规格</label>
                <Select alignItemWithTrigger
                  aria-label="实例规格" size="sm"
                  value={koyebCreateForm.instanceType}
                  onValueChange={(value) => setKoyebCreateForm((f) => ({ ...f, instanceType: String(value) }))}
                  items={(koyebCatalogInstances.length > 0 ? koyebCatalogInstances : ['nano', 'micro', 'small', 'medium', 'large', 'xlarge']).map((t) => {
                    const id = typeof t === 'string' ? t : (t.id || t.name || '');
                    const label = typeof t === 'string' ? t : `${id}${t.memory ? ' · ' + t.memory : ''}${t.price_monthly ? ' · $' + t.price_monthly + '/月' : ''}`;
                    return { value: id, label };
                  })}
                  className="w-full"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">区域（逗号分隔）</label>
                <Input size="sm" aria-label="区域" type="text" value={koyebCreateForm.regions} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, regions: e.target.value }))} placeholder="fra,sin,tok" className="w-full text-xs" />
              </div>
            </div>
            {koyebCatalogRegions.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {koyebCatalogRegions.slice(0, 12).map((region) => {
                  const id = region.id || region.name || '';
                  return (
                    <Button key={id} size="xs" variant={koyebCreateForm.regions.split(',').map((s) => s.trim()).includes(id) ? 'primary' : 'secondary'} onClick={() => {
                      const current = koyebCreateForm.regions.split(',').map((s) => s.trim()).filter(Boolean);
                      const next = current.includes(id) ? current.filter((r) => r !== id) : [...current, id];
                      setKoyebCreateForm((f) => ({ ...f, regions: next.join(',') }));
                    }} className="text-[10px]">{region.name || id}</Button>
                  );
                })}
              </div>
            )}
            <div className="space-y-1">
              <label className="text-[11px] font-semibold text-kumo-subtle">环境变量（每行 K=V）</label>
              <Textarea size="sm" aria-label="环境变量" value={koyebCreateForm.env} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, env: e.target.value }))} placeholder={'DB_HOST=db.internal\nPORT=3000'} className="min-h-24 w-full text-xs font-mono" />
            </div>
            {koyebCreateError && (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebCreateError}</div>
            )}
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Dialog.Close render={(props) => <Button size="sm" {...props} variant="secondary" className="text-xs">取消</Button>} />
            <Button size="sm" onClick={createKoyebService} disabled={koyebCreateSaving} className="text-xs">
              {koyebCreateSaving ? '创建中...' : '创建并部署'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
  );
}
