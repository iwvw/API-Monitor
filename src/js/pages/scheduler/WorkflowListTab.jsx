import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Bell, Edit, GitBranch, Play, Plus, Trash } from '../../components/Icons.jsx';
import { IconButton } from './shared.jsx';
import { WorkflowCanvas } from './WorkflowCanvas.jsx';

export function WorkflowListTab({ workflows, tasks, runs, isArmed, onNavigate, openCreateWorkflow, openEditWorkflow, runWorkflow, deleteWorkflow }) {
  return (
    <SectionCard
      title="工作流编排"
      icon={<GitBranch className="h-4 w-4 text-brand" />}
      bodyClassName={workflows.length === 0 ? '' : 'space-y-3'}
      bodyPadding={workflows.length === 0 ? 'none' : 'md'}
    >
      {workflows.length === 0 ? (
        <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<GitBranch className="h-8 w-8 text-kumo-inactive" />} title="暂无工作流" description="创建后可按 DAG 编排任务。" contents={<Button size="sm" variant="primary" onClick={openCreateWorkflow}><Plus className="h-3.5 w-3.5" />新建工作流</Button>} />
      ) : (
        <div className="grid gap-3 cq-lg:grid-cols-2">
          {workflows.map((workflow) => (
            <div key={workflow.id} className="scheduler-workflow-card flex h-full flex-col overflow-hidden rounded-xl border border-kumo-line bg-kumo-elevated p-3 shadow-none">
              <div className="grid min-h-0 flex-1 gap-3 cq-lg:grid-cols-[minmax(0,220px)_minmax(0,1fr)]">
                <div className="flex min-h-0 min-w-0 flex-col gap-2.5">
                  {/* 标题栏：标题居左，启用状态 pill 靠右 */}
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <h3 className="min-w-0 truncate text-sm font-semibold text-kumo-strong" title={workflow.name}>{workflow.name}</h3>
                    <Badge variant={workflow.enabled ? 'success' : 'neutral'} appearance="dot" className="shrink-0">{workflow.enabled ? '启用' : '停用'}</Badge>
                  </div>
                  {/* 元信息 */}
                  <div className="grid gap-2 cq-sm:grid-cols-2 cq-lg:grid-cols-1">
                    <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                      <div className="text-[11px] text-kumo-subtle">触发方式</div>
                      <div className="mt-1 truncate font-mono text-xs text-kumo-strong">{workflow.schedule ? `Cron ${workflow.schedule}` : '手动触发'}</div>
                    </div>
                    <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-2.5 py-2">
                      <div className="text-[11px] text-kumo-subtle">下次运行</div>
                      <div className="mt-1 truncate text-xs font-semibold text-kumo-strong">
                        {workflow.enabled && workflow.schedule ? (
                          workflow.next_run
                            ? new Date(workflow.next_run * 1000).toLocaleString()
                            : '等待调度'
                        ) : (
                          '手动触发'
                        )}
                      </div>
                    </div>
                  </div>
                  {/* 操作按钮单独一行 */}
                  <div className="mt-auto flex items-center gap-1">
                    <IconButton label="运行工作流" onClick={() => runWorkflow(workflow)} icon={<Play className="h-3.5 w-3.5" />} />
                    <IconButton label="配置通知规则" onClick={() => onNavigate('notification', { newRule: 'workflow.completed', workflowId: String(workflow.id), workflowName: workflow.name })} icon={<Bell className="h-3.5 w-3.5" />} />
                    <IconButton label="编辑工作流" onClick={() => openEditWorkflow(workflow)} icon={<Edit className="h-3.5 w-3.5" />} />
                    <IconButton label="删除工作流" variant={isArmed(`workflow:${workflow.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorkflow(workflow)} icon={<Trash className="h-3.5 w-3.5" />} />
                  </div>
                </div>

                <div className="flex min-h-0 min-w-0 overflow-hidden rounded-md border border-kumo-line">
                  <div className="relative flex min-w-0 flex-1 min-h-[10rem]">
                    <WorkflowCanvas workflow={workflow} runs={runs} tasks={tasks} size="compact" />
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </SectionCard>
  );
}
