import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';

export default function KoyebConfigDialog({ koyebConfigTarget, setKoyebConfigTarget, koyebConfigForm, setKoyebConfigForm, koyebConfigError, saveKoyebConfig, koyebConfigSaving }) {
  return (
      <LayerDialog.Root open={!!koyebConfigTarget} onOpenChange={(open) => { if (!open) setKoyebConfigTarget(null); }}>
        <LayerDialog.Content size="base">
          <LayerDialog.Title>编辑服务配置</LayerDialog.Title>
          <LayerDialog.Description>
            {koyebConfigTarget ? `${koyebConfigTarget.service.name} · 更新后将触发一次新部署` : ''}
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="space-y-3">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">镜像地址</label>
                <Input size="sm" aria-label="镜像地址" type="text" value={koyebConfigForm.image} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, image: e.target.value }))} placeholder="registry.hub.docker.com/xxx/app:latest" className="w-full text-xs" />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1">
                  <label className="text-[11px] font-semibold text-kumo-subtle">启动命令</label>
                  <Input size="sm" aria-label="启动命令" type="text" value={koyebConfigForm.command} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, command: e.target.value }))} placeholder="留空使用镜像默认" className="w-full text-xs" />
                </div>
                <div className="space-y-1">
                  <label className="text-[11px] font-semibold text-kumo-subtle">参数（逗号分隔）</label>
                  <Input size="sm" aria-label="启动参数" type="text" value={koyebConfigForm.args} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, args: e.target.value }))} placeholder="--port,3000" className="w-full text-xs" />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1">
                  <label className="text-[11px] font-semibold text-kumo-subtle">端口（port:protocol）</label>
                  <Input size="sm" aria-label="端口" type="text" value={koyebConfigForm.ports} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, ports: e.target.value }))} placeholder="8080:http, 3000:tcp" className="w-full text-xs" />
                </div>
                <div className="space-y-1">
                  <label className="text-[11px] font-semibold text-kumo-subtle">区域（逗号分隔）</label>
                  <Input size="sm" aria-label="区域" type="text" value={koyebConfigForm.regions} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, regions: e.target.value }))} placeholder="fra,sin,tok" className="w-full text-xs" />
                </div>
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">实例规格</label>
                <Select alignItemWithTrigger
                  aria-label="实例规格" size="sm"
                  value={koyebConfigForm.instanceType}
                  onValueChange={(value) => setKoyebConfigForm((f) => ({ ...f, instanceType: String(value) }))}
                  items={['nano', 'micro', 'small', 'medium', 'large', 'xlarge'].map((t) => ({ value: t, label: t }))}
                  className="w-full"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">环境变量（每行 K=V）</label>
                <Textarea size="sm" aria-label="环境变量" value={koyebConfigForm.env} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, env: e.target.value }))} placeholder={'DB_HOST=db.internal\nPORT=3000'} className="min-h-24 w-full text-xs font-mono" />
              </div>
              <Checkbox
                checked={koyebConfigForm.skipBuild}
                onCheckedChange={(checked) => setKoyebConfigForm((f) => ({ ...f, skipBuild: !!checked }))}
                label="跳过构建（复用上次构建镜像，仅改配置不重建）"
              />
              {koyebConfigError && (
                <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebConfigError}</div>
              )}
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary
              type="button"
              onClick={saveKoyebConfig}
              loading={koyebConfigSaving}
            >
              保存并部署
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
