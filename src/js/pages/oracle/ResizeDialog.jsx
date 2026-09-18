import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { InsetPanel, KeyValueGrid } from '../../components/ui/AppPrimitives.jsx';
import { formatShapeSummary } from './utils.js';

export default function ResizeDialog({
  open,
  onOpenChange,
  selectedInstance,
  resizeForm,
  setResizeForm,
  resizeShapeOptions,
  loadingResizeShapes,
  selectedResizeShape,
  applyResizeShape,
  saveResize,
  submittingResize,
  formatInstanceMetric,
  formatBaselineLabel,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="base">
        <LayerDialog.Title>实例升降配</LayerDialog.Title>
        <LayerDialog.Description>
          调整 shape 或 Flex 规格时，Oracle 可能重启实例，建议在低峰期执行。
        </LayerDialog.Description>
        <LayerDialog.Body>
        {selectedInstance ? (
          <div className="@container space-y-4">
            <InsetPanel tone="recessed">
              <KeyValueGrid
                items={[
                  {
                    label: '实例',
                    value: (
                      <div className="truncate font-semibold" title={selectedInstance.name || selectedInstance.id}>
                        {selectedInstance.name || selectedInstance.id}
                      </div>
                    ),
                  },
                  {
                    label: '当前规格',
                    value: <div className="font-mono">{selectedInstance.shape || '-'}</div>,
                  },
                  {
                    label: '当前 OCPU / 内存',
                    value: `${formatInstanceMetric(selectedInstance.ocpuCount)} / ${formatInstanceMetric(selectedInstance.memoryGb)} GB`,
                  },
                  {
                    label: '可用域',
                    value: selectedInstance.availabilityDomain || '-',
                  },
                ]}
              />
            </InsetPanel>

            <Select alignItemWithTrigger
              aria-label="目标规格"
              size="sm"
              className="w-full"
              value={resizeForm.shape}
              onValueChange={applyResizeShape}
              disabled={loadingResizeShapes || resizeShapeOptions.length === 0}
              items={resizeShapeOptions.map((shape) => ({
                value: shape.name,
                label: `${shape.name} · ${formatShapeSummary(shape)}`,
              }))}
            />

            {selectedResizeShape ? (
              <InsetPanel tone="surface">
                <div className="mb-1 text-sm font-semibold text-kumo-strong">{selectedResizeShape.name}</div>
                <div className="text-xs leading-5 text-kumo-subtle">
                  {selectedResizeShape.processorDescription || 'Oracle 计算实例规格'}
                </div>
                <KeyValueGrid
                  className="mt-3"
                  items={[
                    {
                      label: '默认规格',
                      value: formatShapeSummary(selectedResizeShape),
                    },
                    {
                      label: '计费',
                      value: selectedResizeShape.billingType || 'PAID',
                    },
                  ]}
                />
                {selectedResizeShape.isFlexible ? (
                  <div className="mt-4 grid gap-3 cq-md:grid-cols-2">
                    <Input
                      size="sm"
                      label={`OCPU${selectedResizeShape.ocpuOptions?.min || selectedResizeShape.ocpuOptions?.max ? ` (${formatInstanceMetric(selectedResizeShape.ocpuOptions?.min)} - ${formatInstanceMetric(selectedResizeShape.ocpuOptions?.max)})` : ''}`}
                      value={resizeForm.ocpuCount}
                      onChange={(event) => setResizeForm((current) => ({ ...current, ocpuCount: event.target.value }))}
                      placeholder={selectedResizeShape.ocpuCount ? String(selectedResizeShape.ocpuCount) : '例如 2'}
                    />
                    <Input
                      size="sm"
                      label={`内存 GB${selectedResizeShape.memoryOptions?.min || selectedResizeShape.memoryOptions?.max ? ` (${formatInstanceMetric(selectedResizeShape.memoryOptions?.min)} - ${formatInstanceMetric(selectedResizeShape.memoryOptions?.max)})` : ''}`}
                      value={resizeForm.memoryGb}
                      onChange={(event) => setResizeForm((current) => ({ ...current, memoryGb: event.target.value }))}
                      placeholder={selectedResizeShape.memoryGb ? String(selectedResizeShape.memoryGb) : '例如 12'}
                    />
                    {selectedResizeShape.baselineOcpuUtilizations?.length ? (
                      <Select alignItemWithTrigger
                        aria-label="baseline OCPU"
                        size="sm"
                        className="cq-md:col-span-2"
                        value={resizeForm.baselineOcpuUtilization}
                        onValueChange={(value) => setResizeForm((current) => ({ ...current, baselineOcpuUtilization: value }))}
                        items={[
                          { value: '', label: '使用默认基线' },
                          ...selectedResizeShape.baselineOcpuUtilizations.map((value) => ({
                            value,
                            label: formatBaselineLabel(value),
                          })),
                        ]}
                      />
                    ) : null}
                  </div>
                ) : (
                  <div className="mt-4 text-xs text-kumo-subtle">
                    固定规格，只需切换 shape，无需填写 OCPU / 内存。
                  </div>
                )}
              </InsetPanel>
            ) : (
              <InsetPanel tone="dashed" bodyClassName="py-6 text-center text-sm text-kumo-subtle">
                {loadingResizeShapes ? '正在加载可选规格...' : '当前实例没有可用的规格数据，请刷新后重试。'}
              </InsetPanel>
            )}

            <Switch
              size="sm"
              label="尽量避免停机更新"
              controlFirst={false}
              checked={resizeForm.avoidDowntime}
              onCheckedChange={(checked) => setResizeForm((current) => ({ ...current, avoidDowntime: Boolean(checked) }))}
            />
          </div>
        ) : (
          <div className="py-8 text-center text-sm text-kumo-subtle">请先选择一个实例。</div>
        )}
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary
            type="button"
            onClick={saveResize}
            loading={submittingResize}
            disabled={loadingResizeShapes}
          >
            提交变更
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
