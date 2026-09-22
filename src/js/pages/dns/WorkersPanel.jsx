import React from 'react';
import { AppTable, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Plus, Terminal, Trash } from '../../components/Icons.jsx';
import { handleEditableRowDoubleClick } from '../../modules/tableInteractions.js';
import { formatDate } from './utils.jsx';

const DNS_WORKER_COLUMNS = [
  { id: 'name', role: 'primary', grow: 1 },
  { id: 'createdOn', role: 'datetime' },
  { id: 'modifiedOn', role: 'datetime' },
  { id: 'actions', role: 'actions-xl' },
];

function WorkersPanel({
  loading,
  workers,
  workerSubdomain,
  workerColWidths,
  startWorkerResize,
  isArmed,
  openWorkerModal,
  openWorkerRoutesModal,
  openWorkerDomainsModal,
  openWorkerAnalyticsModal,
  toggleWorkerSubdomain,
  deleteWorker,
}) {
  return (
    <SectionCard
      title="Workers"
      description={workerSubdomain ? <>默认子域名：<span className="font-mono text-kumo-strong">{workerSubdomain}.workers.dev</span></> : '默认子域名未返回'}
      icon={<Terminal className="h-4 w-4 text-brand" />}
      action={(
        <Button size="sm" onClick={() => openWorkerModal()} icon={<Plus className="h-4 w-4" />}>
          新建 Worker
        </Button>
      )}
      bodyPadding="none"
      bodyClassName="overflow-x-auto"
    >
      <AppTable tableId="dns-workers" columns={DNS_WORKER_COLUMNS} columnWidths={workerColWidths}>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startWorkerResize(0, e)} onTouchStart={(e) => startWorkerResize(0, e)} /></Table.Head>
            <Table.Head className="relative pr-6">创建时间<Table.ResizeHandle onMouseDown={(e) => startWorkerResize(1, e)} onTouchStart={(e) => startWorkerResize(1, e)} /></Table.Head>
            <Table.Head className="relative pr-6">更新时间<Table.ResizeHandle onMouseDown={(e) => startWorkerResize(2, e)} onTouchStart={(e) => startWorkerResize(2, e)} /></Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {loading.workers ? (
            Array.from({ length: 4 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={4}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
          ) : workers.length === 0 ? (
            <Table.Row><Table.Cell colSpan={4} className="py-10 text-center text-kumo-subtle">没有 Workers。</Table.Cell></Table.Row>
          ) : workers.map((worker) => (
            <Table.Row
              key={worker.id || worker.name}
              className="cursor-pointer"
              title="双击编辑 Worker 代码"
              onDoubleClick={(event) => handleEditableRowDoubleClick(event, () => openWorkerModal(worker))}
            >
              <Table.Cell className="font-medium text-kumo-strong"><div className="truncate" title={worker.name}>{worker.name}</div></Table.Cell>
              <Table.Cell className="whitespace-nowrap">{formatDate(worker.createdOn)}</Table.Cell>
              <Table.Cell className="whitespace-nowrap">{formatDate(worker.modifiedOn)}</Table.Cell>
              <Table.Cell className="text-right">
                <div className="inline-flex flex-wrap justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={() => openWorkerModal(worker)}>代码</Button>
                  <Button size="sm" variant="secondary" onClick={() => openWorkerRoutesModal(worker)}>路由</Button>
                  <Button size="sm" variant="secondary" onClick={() => openWorkerDomainsModal(worker)}>域名</Button>
                  <Button size="sm" variant="secondary" onClick={() => openWorkerAnalyticsModal(worker)}>统计</Button>
                  <Button size="sm" variant="secondary" onClick={() => toggleWorkerSubdomain(worker, true)}>启用</Button>
                  <Button size="sm" variant={isArmed(`worker:${worker.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteWorker(worker)} aria-label={`删除 ${worker.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                </div>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </AppTable>
    </SectionCard>
  );
}

export default WorkersPanel;
