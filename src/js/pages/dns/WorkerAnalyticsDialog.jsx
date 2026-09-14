import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import CodeEditor from '../../components/ui/CodeEditor.jsx';

function WorkerAnalyticsDialog({
  workerAnalyticsState,
  loading,
  onCloseModal,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">Worker 统计：{workerAnalyticsState.worker?.name}</Dialog.Title>
      {loading.workerAnalytics ? (
        <SkeletonLine className="h-64 w-full" />
      ) : (
        <CodeEditor label="统计数据" language="json" value={JSON.stringify(workerAnalyticsState.analytics || {}, null, 2)} readOnly minHeight="20rem" />
      )}
      <div className="flex justify-end"><Button size="sm" variant="secondary" onClick={onCloseModal}>关闭</Button></div>
    </div>
  );
}

export default WorkerAnalyticsDialog;
