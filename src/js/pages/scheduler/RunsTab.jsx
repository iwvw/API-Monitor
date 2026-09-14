import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Eye, RefreshCw, Trash, X } from '../../components/Icons.jsx';
import { IconButton } from './shared.jsx';
import { formatTimestamp, statusBadgeVariant, statusLabel } from './utils.js';

export function RunsTab({ runs, isArmed, clearOldRuns, clearAllRuns, authHeaders, setSelectedRun, retryRun, cancelRun }) {
  return (
    <SectionCard
      title="运行记录"
      icon={<Activity className="h-4 w-4 text-brand" />}
      actions={(
        <>
          <Button size="sm" variant={isArmed('runs:clear-old') ? 'destructive' : 'secondary-destructive'} onClick={clearOldRuns}><Trash className="h-3.5 w-3.5" />清理 30 天前</Button>
          <Button size="sm" variant={isArmed('runs:clear-all') ? 'destructive' : 'secondary-destructive'} onClick={clearAllRuns}><Trash className="h-3.5 w-3.5" />清空全部</Button>
        </>
      )}
      bodyPadding="none"
    >
      {runs.length === 0 ? (
        <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Activity className="h-8 w-8 text-kumo-inactive" />} title="暂无运行记录" description="运行后显示结果" />
      ) : (
        <div className="overflow-x-auto">
          <Table layout="fixed" className="min-w-[920px]">
            <colgroup><col /><col className="w-[110px]" /><col className="w-[130px]" /><col className="w-[180px]" /><col className="w-[120px]" /><col className="w-[128px]" /></colgroup>
            <Table.Header><Table.Row><Table.Head>运行对象</Table.Head><Table.Head>状态</Table.Head><Table.Head>触发方式</Table.Head><Table.Head>开始时间</Table.Head><Table.Head>耗时</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
            <Table.Body>
              {runs.map((run) => (
                <Table.Row key={run.id}>
                  <Table.Cell><div className="font-semibold text-kumo-strong">{run.workflow_name}</div><div className="truncate text-xs text-kumo-subtle">{run.summary || '暂无摘要'}</div></Table.Cell>
                  <Table.Cell><Badge variant={statusBadgeVariant(run.status)} appearance="dot">{statusLabel(run.status)}</Badge></Table.Cell>
                  <Table.Cell className="text-xs text-kumo-subtle">{run.trigger_type === 'manual' ? '手动触发' : run.trigger_type}</Table.Cell>
                  <Table.Cell className="text-xs text-kumo-subtle">{formatTimestamp(run.start_time || run.created_at)}</Table.Cell>
                  <Table.Cell className="font-mono text-xs text-kumo-default">{run.duration ?? 0}s</Table.Cell>
                  <Table.Cell><div className="flex items-center justify-center gap-1"><IconButton label="查看详情" onClick={async () => {
                    const res = await fetch(`/api/scheduler/runs/${run.id}`, { headers: authHeaders() });
                    const data = await res.json();
                    if (data.success) setSelectedRun(data.data);
                  }} icon={<Eye className="h-3.5 w-3.5" />} />{run.status === 'failed' && <IconButton label="重试运行" onClick={() => retryRun(run)} icon={<RefreshCw className="h-3.5 w-3.5" />} />}{run.status === 'running' && <IconButton label="取消运行" variant="secondary-destructive" onClick={() => cancelRun(run)} icon={<X className="h-3.5 w-3.5" />} />}</div></Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
    </SectionCard>
  );
}
