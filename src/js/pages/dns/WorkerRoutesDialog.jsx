import React from 'react';
import { LayerCard, Table } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Edit, Trash } from '../../components/Icons.jsx';

function WorkerRoutesDialog({
  workerRouteState,
  setWorkerRouteState,
  loading,
  isArmed,
  onSaveWorkerRoute,
  onDeleteWorkerRoute,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">Worker 路由：{workerRouteState.worker?.name}</Dialog.Title>
      <div className="grid grid-cols-1 gap-3 cq-md:grid-cols-[1fr_220px_auto]">
        <Input size="sm"
          label="路由规则"
          value={workerRouteState.form.pattern}
          onChange={(event) => setWorkerRouteState((prev) => ({ ...prev, form: { ...prev.form, pattern: event.target.value } }))}
          placeholder="example.com/*"
        />
        <Input size="sm"
          label="Worker"
          value={workerRouteState.form.script}
          onChange={(event) => setWorkerRouteState((prev) => ({ ...prev, form: { ...prev.form, script: event.target.value } }))}
        />
        <div className="flex items-end">
          <Button size="sm" onClick={onSaveWorkerRoute} disabled={loading.saveWorkerRoute}>
            {workerRouteState.form.id ? '更新路由' : '添加路由'}
          </Button>
        </div>
      </div>
      <LayerCard className="overflow-x-auto p-0">
        <Table>
          <Table.Header variant="compact">
            <Table.Row>
              <Table.Head>规则</Table.Head>
              <Table.Head>Worker</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {workerRouteState.routes.length === 0 ? (
              <Table.Row><Table.Cell colSpan={3} className="py-8 text-center text-kumo-subtle">没有 Worker 路由。</Table.Cell></Table.Row>
            ) : workerRouteState.routes.map((route) => (
              <Table.Row key={route.id}>
                <Table.Cell>{route.pattern}</Table.Cell>
                <Table.Cell>{route.script || '-'}</Table.Cell>
                <Table.Cell className="text-right">
                  <div className="inline-flex gap-2">
                    <Button size="sm" shape="square" variant="secondary" onClick={() => setWorkerRouteState((prev) => ({ ...prev, form: { id: route.id, pattern: route.pattern, script: route.script || workerRouteState.worker?.name || '' } }))} aria-label="编辑 Worker 路由" title="编辑" icon={<Edit className="h-4 w-4" />} />
                    <Button size="sm" shape="square" variant={isArmed(`worker-route:${route.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeleteWorkerRoute(route)} aria-label="删除 Worker 路由" title="删除" icon={<Trash className="h-4 w-4" />} />
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </LayerCard>
    </div>
  );
}

export default WorkerRoutesDialog;
