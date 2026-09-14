import React from 'react';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Save } from '../../components/Icons.jsx';

function TemplateDialog({
  modal,
  templateForm,
  setTemplateForm,
  recordTypes,
  loading,
  onCloseModal,
  onSaveTemplate,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">
        {modal.data ? '编辑 DNS 模板' : '添加 DNS 模板'}
      </Dialog.Title>
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
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onSaveTemplate} disabled={loading.saveTemplate} icon={<Save className="h-4 w-4" />}>
          保存
        </Button>
      </div>
    </div>
  );
}

export default TemplateDialog;
