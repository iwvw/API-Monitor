import React from 'react';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Save } from '../../components/Icons.jsx';

function TemplateDialog({
  open,
  onOpenChange,
  modal,
  templateForm,
  setTemplateForm,
  recordTypes,
  loading,
  onSaveTemplate,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>
          {modal.data ? '编辑 DNS 模板' : '添加 DNS 模板'}
        </LayerDialog.Title>
        <LayerDialog.Body>
          <div className="@container flex flex-col gap-4">
            <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
              <Input size="sm" label="模板名称" value={templateForm.name} onChange={(event) => setTemplateForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="默认 A 记录" />
              <Input size="sm" label="描述" value={templateForm.description} onChange={(event) => setTemplateForm((prev) => ({ ...prev, description: event.target.value }))} placeholder="可选" />
            </div>
            <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-4">
              <Select alignItemWithTrigger size="sm"
                label="类型"
                value={templateForm.type}
                onValueChange={(value) => setTemplateForm((prev) => ({ ...prev, type: String(value) }))}
                items={recordTypes.map((type) => ({ value: type, label: type }))}
              />
              <Input size="sm" label="记录名称" value={templateForm.recordName} onChange={(event) => setTemplateForm((prev) => ({ ...prev, recordName: event.target.value }))} />
              <Input size="sm" label="TTL" type="number" value={String(templateForm.ttl)} onChange={(event) => setTemplateForm((prev) => ({ ...prev, ttl: event.target.value }))} />
              <Input size="sm" label="优先级" type="number" value={String(templateForm.priority)} onChange={(event) => setTemplateForm((prev) => ({ ...prev, priority: event.target.value }))} />
            </div>
            <Input size="sm" label="内容" value={templateForm.content} onChange={(event) => setTemplateForm((prev) => ({ ...prev, content: event.target.value }))} />
            <Checkbox
              checked={templateForm.proxied}
              onCheckedChange={(checked) => setTemplateForm((prev) => ({ ...prev, proxied: Boolean(checked) }))}
              label="默认开启代理"
            />
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSaveTemplate} loading={loading.saveTemplate} icon={<Save className="h-4 w-4" />}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default TemplateDialog;
