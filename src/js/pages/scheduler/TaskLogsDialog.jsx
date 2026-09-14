import React from 'react';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Badge } from '@cloudflare/kumo/components/badge';
import { LayerCard } from '@cloudflare/kumo';
import { sectionCardHeaderClass } from '../../components/ui/AppPrimitives.jsx';
import { X } from '../../components/Icons.jsx';
import { renderLogOutput } from './shared.jsx';
import { formatTimestamp, statusBadgeVariant, statusLabel } from './utils.js';

export function TaskLogsDialog({ taskLogsTarget, setTaskLogsTarget, taskLogs, taskLogsLoading, taskLogsSelectedId, setTaskLogsSelectedId }) {
  return (
    <Dialog.Root open={Boolean(taskLogsTarget)} onOpenChange={(open) => !open && setTaskLogsTarget(null)}>
      <Dialog className="@container scheduler-task-dialog flex h-[min(680px,calc(100dvh-2rem))] w-[min(960px,calc(100vw-2rem))] flex-col overflow-hidden p-5 cq-sm:p-6">
        <Dialog.Title className="mb-4 shrink-0 text-base font-semibold text-kumo-strong">运行日志{taskLogsTarget ? `：${taskLogsTarget.name}` : ''}</Dialog.Title>
        <div className="grid min-h-0 flex-1 gap-0 cq-md:grid-cols-[minmax(0,240px)_minmax(0,1fr)]">
          <div className="flex min-h-0 flex-col gap-1 overflow-y-auto cq-md:border-r cq-md:border-kumo-line cq-md:pr-3">
            {taskLogsLoading ? (
              <div className="space-y-2">
                <SkeletonLine className="h-11" />
                <SkeletonLine className="h-11" />
                <SkeletonLine className="h-11" />
              </div>
            ) : taskLogs.length === 0 ? (
              <div className="rounded-lg border border-kumo-line p-6 text-center text-sm text-kumo-subtle">暂无运行日志</div>
            ) : (
              taskLogs.map((logItem) => {
                const isActive = logItem.id === taskLogsSelectedId;
                return (
                  <Button
                    key={logItem.id}
                    variant="ghost"
                    size="sm"
                    className={`h-10 w-full justify-start rounded-md px-3 ${isActive ? 'bg-brand/15' : ''}`}
                    onClick={() => setTaskLogsSelectedId(logItem.id)}
                  >
                    <span className="flex w-full min-w-0 items-center justify-between gap-2">
                      <span className="truncate font-mono text-xs text-kumo-strong">{formatTimestamp(logItem.start_time)}</span>
                      <Badge variant={statusBadgeVariant(logItem.status)} appearance="dot" className="shrink-0">{statusLabel(logItem.status)}</Badge>
                    </span>
                  </Button>
                );
              })
            )}
          </div>
          <div className="min-h-0 overflow-y-auto cq-md:pl-3">
            {(() => {
              const active = taskLogs.find((item) => item.id === taskLogsSelectedId);
              if (!active) {
                return <div className="flex h-full min-h-32 items-center justify-center rounded-lg border border-dashed border-kumo-line p-6 text-center text-sm text-kumo-subtle">选择左侧记录查看详情</div>;
              }
              return (
                <LayerCard className="flex h-full min-h-0 flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated shadow-none ring-0">
                  <LayerCard.Secondary className={`${sectionCardHeaderClass} my-0 shrink-0`}>
                    <div className="flex min-w-0 items-center gap-3 text-xs text-kumo-subtle">
                      <span className="whitespace-nowrap font-mono">{formatTimestamp(active.start_time)}</span>
                      {active.duration != null && <span className="whitespace-nowrap">｜耗时 {active.duration}s</span>}
                    </div>
                    <Badge variant={statusBadgeVariant(active.status)} appearance="dot" className="shrink-0">{statusLabel(active.status)}</Badge>
                  </LayerCard.Secondary>
                  <LayerCard.Primary className="flex-1 gap-0 overflow-auto bg-kumo-elevated px-4 py-3 ring-0">
                    {renderLogOutput(active.output, taskLogsTarget?.type === 'ai')}
                  </LayerCard.Primary>
                </LayerCard>
              );
            })()}
          </div>
        </div>
        <div className="mt-4 flex shrink-0 items-center justify-end gap-2 border-t border-kumo-line pt-3">
          <Button size="sm" variant="secondary" onClick={() => setTaskLogsTarget(null)}><X className="h-3.5 w-3.5" />关闭</Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
