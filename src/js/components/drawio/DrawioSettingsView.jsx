import React from 'react';
import { Empty, Field } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SectionCard } from '../ui/AppPrimitives.jsx';

export default function DrawioSettingsView({
  settings,
  onChange,
  onSave,
  onRebuildAll,
  saving = false,
}) {
  if (!settings) return <Empty size="sm" title="正在加载设置" />;
  const patch = value => onChange(value);

  return (
    <div className="min-h-0 flex-1 overflow-y-auto pr-1 scrollbar-thin">
      <SectionCard
        title="编辑器默认设置"
        className="max-w-4xl self-start"
        bodyClassName="grid gap-4"
      >
        <div className="grid gap-4 cq-sm:grid-cols-2">
          <Field label="默认导出格式" description="工具栏中的导出按钮优先使用此格式">
            <Select
              size="sm"
              value={settings.default_export_format}
              onValueChange={value => patch({ default_export_format: value })}
              renderValue={value => (value === 'drawio' ? 'draw.io' : value.toUpperCase())}
            >
              <Select.Option value="drawio">draw.io</Select.Option>
              <Select.Option value="xml">XML</Select.Option>
              <Select.Option value="svg">SVG</Select.Option>
            </Select>
          </Field>
          <Field label="编辑器主题" description="进入图表编辑器时采用的主题">
            <Select
              size="sm"
              value={settings.default_theme_mode}
              onValueChange={value => patch({ default_theme_mode: value })}
              renderValue={value =>
                ({ system: '跟随系统', light: '浅色', dark: '深色' })[value] || value
              }
            >
              <Select.Option value="system">跟随系统</Select.Option>
              <Select.Option value="light">浅色</Select.Option>
              <Select.Option value="dark">深色</Select.Option>
            </Select>
          </Field>
          <Field label="自动保存延迟" description="停止编辑多久后保存草稿，单位为毫秒">
            <Input
              size="sm"
              type="number"
              min="300"
              max="30000"
              value={String(settings.autosave_debounce_ms || 2000)}
              onChange={event =>
                patch({ autosave_debounce_ms: Math.max(300, Number(event.target.value) || 2000) })
              }
            />
          </Field>
          <Field label="版本软上限" description="超过该数量后提示清理历史版本">
            <Input
              size="sm"
              type="number"
              min="1"
              max="1000"
              value={String(settings.version_soft_limit || 50)}
              onChange={event =>
                patch({ version_soft_limit: Math.max(1, Number(event.target.value) || 50) })
              }
            />
          </Field>
          <Field label="文档大小上限" description="单个图表 XML 的最大体积，单位为 MB">
            <Input
              size="sm"
              type="number"
              min="1"
              max="100"
              value={String(
                Math.max(1, Math.round((settings.document_size_limit_bytes || 10485760) / 1048576))
              )}
              onChange={event =>
                patch({
                  document_size_limit_bytes:
                    Math.max(1, Number(event.target.value) || 10) * 1048576,
                })
              }
            />
          </Field>
        </div>

        <div className="grid gap-3 border-t border-kumo-line pt-4 cq-sm:grid-cols-3">
          <Switch
            size="sm"
            label="自动保存草稿"
            controlFirst={false}
            checked={settings.autosave_enabled}
            onCheckedChange={checked => patch({ autosave_enabled: checked })}
          />
          <Switch
            size="sm"
            label="允许外链资源"
            controlFirst={false}
            checked={settings.allow_external_assets}
            onCheckedChange={checked => patch({ allow_external_assets: checked })}
          />
          <Switch
            size="sm"
            label="阻止私网资源"
            controlFirst={false}
            checked={settings.block_private_network_assets}
            onCheckedChange={checked => patch({ block_private_network_assets: checked })}
          />
        </div>

        <div className="grid gap-4 border-t border-kumo-line pt-4 cq-sm:grid-cols-3">
          <Field label="预览格式">
            <Select
              size="sm"
              value={settings.thumbnail_format}
              onValueChange={value => patch({ thumbnail_format: value })}
              renderValue={value => value.toUpperCase()}
            >
              <Select.Option value="svg">SVG</Select.Option>
            </Select>
          </Field>
          <Field label="最大宽度">
            <Input
              size="sm"
              type="number"
              min="160"
              max="4096"
              value={String(settings.thumbnail_max_width || 640)}
              onChange={event =>
                patch({ thumbnail_max_width: Math.max(160, Number(event.target.value) || 640) })
              }
            />
          </Field>
          <Field label="最大高度">
            <Input
              size="sm"
              type="number"
              min="90"
              max="4096"
              value={String(settings.thumbnail_max_height || 360)}
              onChange={event =>
                patch({ thumbnail_max_height: Math.max(90, Number(event.target.value) || 360) })
              }
            />
          </Field>
        </div>

        <div className="flex flex-wrap justify-end gap-2 border-t border-kumo-line pt-4">
          <Button size="sm" variant="secondary" onClick={onRebuildAll}>
            重建全部预览
          </Button>
          <Button size="sm" variant="primary" loading={saving} onClick={onSave}>
            保存设置
          </Button>
        </div>
      </SectionCard>
    </div>
  );
}
