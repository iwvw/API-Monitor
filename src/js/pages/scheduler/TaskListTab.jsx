import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { AppTable, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Check, Clock, Edit, Pause, Play, Plus, Terminal, Trash } from '../../components/Icons.jsx';
import { IconButton } from './shared.jsx';
import { formatTimestamp, statusBadgeVariant, statusLabel, taskTypeLabel } from './utils.js';

const SCHEDULER_TASK_COLUMNS = [
  { id: 'status', role: 'status' },
  { id: 'name', role: 'primary', grow: 1, minWidth: 200 },
  { id: 'type', role: 'type' },
  { id: 'schedule', role: 'content', grow: 1, minWidth: 176, verticalAlign: 'middle' },
  { id: 'nextRun', role: 'datetime' },
  { id: 'logs', role: 'content', grow: 1, minWidth: 176, verticalAlign: 'middle' },
  { id: 'actions', role: 'actions-lg' },
];

export function TaskListTab({ tasks, loading, isArmed, openCreateTask, openEditTask, openTaskLogs, runTask, toggleTask, deleteTask }) {
  return (
    <SectionCard
      title="任务列表"
      icon={<Clock className="h-4 w-4 text-brand" />}
      bodyPadding="none"
    >
      {loading ? (
        <div className="space-y-2 p-4">
          {Array.from({ length: 6 }).map((_, index) => <SkeletonLine key={index} className="h-11 w-full" />)}
        </div>
      ) : tasks.length === 0 ? (
        <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Clock className="h-8 w-8 text-kumo-inactive" />} title="暂无任务" description="创建后可被工作流引用。" contents={<Button size="sm" variant="primary" onClick={openCreateTask}><Plus className="h-3.5 w-3.5" />新建任务</Button>} />
      ) : (
        <div className="overflow-x-auto">
          <AppTable tableId="scheduler-tasks" columns={SCHEDULER_TASK_COLUMNS} className="min-w-[1080px]">
            <Table.Header><Table.Row><Table.Head>状态</Table.Head><Table.Head>任务</Table.Head><Table.Head>类型</Table.Head><Table.Head>周期</Table.Head><Table.Head>下次运行</Table.Head><Table.Head>日志</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
            <Table.Body>
              {tasks.map((task) => (
                <Table.Row key={task.id}>
                  <Table.Cell><Badge variant={task.enabled ? 'success' : 'neutral'} appearance="dot">{task.enabled ? '启用' : '停用'}</Badge></Table.Cell>
                  <Table.Cell><div className="font-semibold text-kumo-strong">{task.name}</div><div className="truncate text-xs text-kumo-subtle">{task.description || task.command}</div></Table.Cell>
                  <Table.Cell className="text-xs text-kumo-default">{taskTypeLabel(task.type)}</Table.Cell>
                  <Table.Cell><div className="font-mono text-xs text-kumo-default">{task.schedule || '手动'}</div><div className="text-xs text-kumo-subtle">{task.schedule_summary}</div></Table.Cell>
                  <Table.Cell className="text-xs text-kumo-subtle">{formatTimestamp(task.next_run)}</Table.Cell>
                  <Table.Cell>
                    <div className="flex items-center gap-2">
                      <IconButton label="查看日志" onClick={() => openTaskLogs(task)} icon={<Terminal className="h-3.5 w-3.5" />} />
                      {task.recent_status ? (
                        <Badge variant={statusBadgeVariant(task.recent_status)} appearance="dot">{statusLabel(task.recent_status)}</Badge>
                      ) : (
                        <span className="text-xs text-kumo-subtle">暂无运行</span>
                      )}
                    </div>
                  </Table.Cell>
                  <Table.Cell>
                    <div className="flex items-center justify-center gap-1">
                      <IconButton label="立即运行" onClick={() => runTask(task)} icon={<Play className="h-3.5 w-3.5" />} />
                      <IconButton label={task.enabled ? '停用' : '启用'} onClick={() => toggleTask(task)} icon={task.enabled ? <Pause className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />} />
                      <IconButton label="编辑" onClick={() => openEditTask(task)} icon={<Edit className="h-3.5 w-3.5" />} />
                      <IconButton label="删除" variant={isArmed(`task:${task.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTask(task)} icon={<Trash className="h-3.5 w-3.5" />} />
                    </div>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </div>
      )}
    </SectionCard>
  );
}
