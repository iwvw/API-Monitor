import React from 'react';
import { LayerCard, Table } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Trash } from '../../components/Icons.jsx';

function WorkerDomainsDialog({
  open,
  onOpenChange,
  workerDomainState,
  setWorkerDomainState,
  isArmed,
  onAddWorkerDomain,
  onDeleteWorkerDomain,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>Worker 自定义域名：{workerDomainState.worker?.name}</LayerDialog.Title>
        <LayerDialog.Body>
          <div className="@container flex flex-col gap-4">
            <div className="grid grid-cols-1 gap-3 cq-md:grid-cols-[1fr_180px_auto]">
              <Input size="sm" label="域名" value={workerDomainState.hostname} onChange={(event) => setWorkerDomainState((prev) => ({ ...prev, hostname: event.target.value }))} placeholder="worker.example.com" />
              <Input size="sm" label="环境" value={workerDomainState.environment} onChange={(event) => setWorkerDomainState((prev) => ({ ...prev, environment: event.target.value }))} />
              <div className="flex items-end"><Button size="sm" onClick={onAddWorkerDomain}>添加域名</Button></div>
            </div>
            <LayerCard className="overflow-x-auto p-0">
              <Table>
                <Table.Header variant="compact">
                  <Table.Row><Table.Head>域名</Table.Head><Table.Head>环境</Table.Head><Table.Head>Zone</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
                </Table.Header>
                <Table.Body>
                  {workerDomainState.domains.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={4} className="py-8 text-center text-kumo-subtle">没有自定义域名。</Table.Cell></Table.Row>
                  ) : workerDomainState.domains.map((domain) => (
                    <Table.Row key={domain.id}>
                      <Table.Cell>{domain.hostname}</Table.Cell>
                      <Table.Cell>{domain.environment || '-'}</Table.Cell>
                      <Table.Cell>{domain.zoneName || domain.zoneId || '-'}</Table.Cell>
                      <Table.Cell className="text-right"><Button size="sm" shape="square" variant={isArmed(`worker-domain:${domain.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeleteWorkerDomain(domain)} aria-label={`删除 ${domain.hostname}`} title="删除" icon={<Trash className="h-4 w-4" />} /></Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </LayerCard>
          </div>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default WorkerDomainsDialog;
