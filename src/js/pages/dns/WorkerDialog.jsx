import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import CodeEditor from '../../components/ui/CodeEditor.jsx';
import { Save } from '../../components/Icons.jsx';

function WorkerDialog({
  modal,
  workerForm,
  setWorkerForm,
  loading,
  onCloseModal,
  onSaveWorker,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">
        {modal.data ? `编辑 Worker：${modal.data.name}` : '新建 Worker'}
      </Dialog.Title>
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
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="secondary" onClick={onCloseModal}>取消</Button>
        <Button size="sm" onClick={onSaveWorker} disabled={loading.saveWorker} icon={<Save className="h-4 w-4" />}>
          保存
        </Button>
      </div>
    </div>
  );
}

export default WorkerDialog;
