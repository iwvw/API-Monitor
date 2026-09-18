import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Save } from '../../components/Icons.jsx';

function WorkerDialog({
  open,
  onOpenChange,
  modal,
  workerForm,
  setWorkerForm,
  loading,
  onSaveWorker,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>
          {modal.data ? `编辑 Worker：${modal.data.name}` : '新建 Worker'}
        </LayerDialog.Title>
        <LayerDialog.Body>
          <div className="flex flex-col gap-4">
            {!modal.data && (
              <Input size="sm" label="Worker 名称" value={workerForm.name} onChange={(event) => setWorkerForm((prev) => ({ ...prev, name: event.target.value }))} placeholder="my-worker" />
            )}
            {modal.data && <Input size="sm" label="Worker 名称" value={workerForm.name} readOnly />}
            {loading.workerScript ? (
              <SkeletonLine className="h-64 w-full" />
            ) : (
              <CodeEditor
                label="脚本内容"
                language="javascript"
                value={workerForm.script}
                onChange={(script) => setWorkerForm((prev) => ({ ...prev, script }))}
                minHeight="24rem"
              />
            )}
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="button" onClick={onSaveWorker} loading={loading.saveWorker} icon={<Save className="h-4 w-4" />}>
            保存
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default WorkerDialog;
