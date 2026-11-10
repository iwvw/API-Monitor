import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';

export default function KoyebScaleDialog({ koyebScaleTarget, setKoyebScaleTarget, koyebScaleScope, setKoyebScaleScope, koyebScaleInstances, setKoyebScaleInstances, koyebScaleError, resetKoyebScale, saveKoyebScale, koyebScaleSaving }) {
  return (
      <LayerDialog.Root open={!!koyebScaleTarget} onOpenChange={(open) => { if (!open) setKoyebScaleTarget(null); }}>
        <LayerDialog.Content size="base">
          <LayerDialog.Title>手动扩容</LayerDialog.Title>
          <LayerDialog.Description>
            {koyebScaleTarget ? `${koyebScaleTarget.service.name} · 设置实例副本数（覆盖自动扩缩容策略）` : ''}
          </LayerDialog.Description>
          <LayerDialog.Body>
            <div className="space-y-3">
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">作用域（scopes，逗号分隔，通常为服务类型名）</label>
                <Input size="sm" aria-label="作用域" type="text" value={koyebScaleScope} onChange={(e) => setKoyebScaleScope(e.target.value)} placeholder="web" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-semibold text-kumo-subtle">实例数量</label>
                <Input size="sm" aria-label="实例数量" type="number" min="1" value={koyebScaleInstances} onChange={(e) => setKoyebScaleInstances(Number(e.target.value) || 1)} className="w-full text-xs" />
              </div>
              {koyebScaleError && (
                <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebScaleError}</div>
              )}
              <div className="flex justify-end">
                <Button size="sm" variant="secondary-destructive" onClick={resetKoyebScale} className="text-xs">重置为自动</Button>
              </div>
            </div>
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Action type="button" onClick={saveKoyebScale} disabled={koyebScaleSaving}>
              {koyebScaleSaving ? '保存中...' : '保存'}
            </LayerDialog.Action>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Root>
  );
}
