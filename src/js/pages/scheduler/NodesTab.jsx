import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Empty } from '@cloudflare/kumo/components/empty';
import { AppTable, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Server } from '../../components/Icons.jsx';
import { statusBadgeVariant, statusLabel } from './utils.js';

const SCHEDULER_NODE_COLUMNS = [
  { id: 'node', role: 'primary', grow: 1, minWidth: 200 },
  { id: 'status', role: 'status' },
  { id: 'kind', role: 'type' },
  { id: 'concurrency', role: 'number' },
  { id: 'labels', role: 'content', grow: 1, minWidth: 176, verticalAlign: 'middle' },
  { id: 'note', role: 'content', grow: 2, minWidth: 240, verticalAlign: 'middle' },
];

export function NodesTab({ nodes }) {
  return (
    <SectionCard
      title="执行节点"
      icon={<Server className="h-4 w-4 text-brand" />}
      bodyPadding="none"
    >
      {nodes.length === 0 ? (
        <Empty size="sm" className="rounded-none border-0 bg-transparent" icon={<Server className="h-8 w-8 text-kumo-inactive" />} title="暂无执行节点" description="本机默认作为执行节点。" />
      ) : (
        <div className="overflow-x-auto">
          <AppTable tableId="scheduler-nodes" columns={SCHEDULER_NODE_COLUMNS} className="w-full min-w-[760px]">
            <Table.Header><Table.Row><Table.Head>节点</Table.Head><Table.Head>状态</Table.Head><Table.Head>类型</Table.Head><Table.Head>并发</Table.Head><Table.Head>标签</Table.Head><Table.Head>说明</Table.Head></Table.Row></Table.Header>
            <Table.Body>
              {nodes.map((node) => (
                <Table.Row key={node.id}>
                  <Table.Cell>
                    <div className="flex min-w-0 items-center gap-2">
                      <Server className="h-4 w-4 shrink-0 text-brand" />
                      <div className="min-w-0">
                        <div className="truncate font-semibold text-kumo-strong">{node.name}</div>
                        <div className="truncate text-xs text-kumo-subtle">{node.id}</div>
                      </div>
                    </div>
                  </Table.Cell>
                  <Table.Cell><Badge variant={statusBadgeVariant(node.status)} appearance="dot">{statusLabel(node.status)}</Badge></Table.Cell>
                  <Table.Cell className="text-xs text-kumo-default">{node.kind === 'local' ? '本机' : 'Agent'}</Table.Cell>
                  <Table.Cell className="font-mono text-xs text-kumo-strong">{node.active_runs ?? 0}/{node.max_concurrency ?? 0}</Table.Cell>
                  <Table.Cell>
                    <div className="flex flex-wrap gap-1">
                      {(node.labels || []).length === 0 ? <span className="text-xs text-kumo-subtle">无</span> : node.labels.map((label) => <Badge key={label} variant="secondary">{label}</Badge>)}
                    </div>
                  </Table.Cell>
                  <Table.Cell className="truncate text-xs text-kumo-subtle">{node.capability_note || '-'}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </div>
      )}
    </SectionCard>
  );
}
