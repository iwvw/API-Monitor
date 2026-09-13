import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Save } from '../../components/Icons.jsx';

function RecordDialog({
  modal,
  recordForm,
  setRecordForm,
  recordTypes,
  loading,
  onCloseModal,
  onSaveRecord,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">
        {modal.data ? '编辑 DNS 记录' : '添加 DNS 记录'}
      </Dialog.Title>
      <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-3">
        <Select alignItemWithTrigger size="sm"
          label="类型"
          value={recordForm.type}
          onValueChange={(value) => setRecordForm((prev) => ({ ...prev, type: String(value) }))}
          items={recordTypes.map((type) => ({ value: type, label: type }))}
        />
        <Input size="sm" label="名称" value={recordForm.name} onChange={(event) => setRecordForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="@ 或 www" />
        <Input size="sm" label="TTL" type="number" value={String(recordForm.ttl)} onChange={(event) => setRecordForm((prev) => ({ ...prev, ttl: event.target.value }))} />
      </div>
      <Input size="sm" label="内容" value={recordForm.content} onChange={(event) => setRecordForm((prev) => ({ ...prev, content: event.target.value }))} placeholder="IP、域名或文本内容" />
      <div className="grid grid-cols-1 gap-4 cq-md:grid-cols-2">
        <Input size="sm" label="优先级" type="number" value={String(recordForm.priority)} onChange={(event) => setRecordForm((prev) => ({ ...prev, priority: event.target.value }))} />
        <div className="flex items-center justify-between rounded-md border border-kumo-line p-3">
          <div>
            <div className="text-sm font-medium text-kumo-strong">代理流量</div>
            <div className="text-xs text-kumo-subtle">开启 Cloudflare 橙云代理。</div>
          </div>
          <Switch checked={recordForm.proxied} onCheckedChange={(checked) => setRecordForm((prev) => ({ ...prev, proxied: Boolean(checked) }))} />
        </div>
      </div>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onSaveRecord} disabled={loading.saveRecord} icon={<Save className="h-4 w-4" />}>
          保存
        </Button>
      </div>
    </div>
  );
}

export default RecordDialog;
