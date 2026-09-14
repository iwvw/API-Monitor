import React from 'react';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { LayerCard } from '@cloudflare/kumo';
import { sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Copy, Sparkle } from '../../components/Icons.jsx';
import { renderLogOutput } from './shared.jsx';
import { WorkflowCanvas } from './WorkflowCanvas.jsx';
import { formatOutputText, formatTimestamp, statusBadgeVariant, statusLabel } from './utils.js';

export function RunDetailDialog({ selectedRun, setSelectedRun, tasks }) {
  return (
    <Dialog.Root open={Boolean(selectedRun)} onOpenChange={(open) => !open && setSelectedRun(null)}>
      <Dialog className="@container scheduler-task-dialog flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden p-5 cq-sm:p-6">
        <Dialog.Title className="mb-4 shrink-0 text-base font-semibold text-kumo-strong">运行详情</Dialog.Title>
        {selectedRun && (
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
            {selectedRun.workflow && <WorkflowCanvas workflow={selectedRun.workflow} runs={[selectedRun]} tasks={tasks} />}
            <div className="grid gap-3">
              {(selectedRun.node_runs || []).map((nodeRun) => {
                const wfNode = (selectedRun.workflow?.nodes || []).find((n) => n.id === nodeRun.node_id || n.name === nodeRun.node_name);
                const linkedTask = tasks.find((t) => String(t.id) === String(wfNode?.task_id || nodeRun.task_id));
                const isAi = wfNode?.type === 'ai' || linkedTask?.type === 'ai';
                return (
                  <LayerCard key={nodeRun.id} className="flex flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                    <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0`}>
                      <div className="flex min-w-0 items-center gap-2.5">
                        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg ${isAi ? 'bg-brand/10 text-brand' : 'bg-kumo-fill text-brand'}`}>
                          {isAi ? <Sparkle className="h-4 w-4" /> : <Activity className="h-4 w-4" />}
                        </span>
                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <span className="truncate text-sm font-semibold text-kumo-strong">{nodeRun.node_name}</span>
                            {isAi && <Badge variant="purple">AI</Badge>}
                          </div>
                          <div className="text-xs text-kumo-subtle">{formatTimestamp(nodeRun.start_time)} / {nodeRun.duration ?? 0}s</div>
                        </div>
                      </div>
                      <Badge variant={statusBadgeVariant(nodeRun.status)} appearance="dot">{statusLabel(nodeRun.status)}</Badge>
                    </LayerCard.Secondary>
                    <LayerCard.Primary className="gap-0 overflow-visible bg-kumo-elevated px-4 py-3 ring-0">
                      {isAi ? (
                        <div className="max-h-80 overflow-auto">
                          {renderLogOutput(nodeRun.output, true)}
                        </div>
                      ) : (
                        <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-md bg-kumo-recessed p-3 text-xs text-kumo-default">{formatOutputText(nodeRun.output) || '无输出'}</pre>
                      )}
                      <Button size="sm" variant="secondary" className="mt-2" onClick={() => navigator.clipboard?.writeText(nodeRun.output || '')}><Copy className="h-3.5 w-3.5" />复制输出</Button>
                    </LayerCard.Primary>
                  </LayerCard>
                );
              })}
            </div>
          </div>
        )}
      </Dialog>
    </Dialog.Root>
  );
}
