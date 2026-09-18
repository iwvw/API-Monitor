import React from 'react';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import CodeEditor from '../../components/ui/CodeEditor.jsx';

function WorkerAnalyticsDialog({
  open,
  onOpenChange,
  workerAnalyticsState,
  loading,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>Worker 统计：{workerAnalyticsState.worker?.name}</LayerDialog.Title>
        <LayerDialog.Body>
          {loading.workerAnalytics ? (
            <SkeletonLine className="h-64 w-full" />
          ) : (
            <CodeEditor label="统计数据" language="json" value={JSON.stringify(workerAnalyticsState.analytics || {}, null, 2)} readOnly minHeight="20rem" />
          )}
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default WorkerAnalyticsDialog;
