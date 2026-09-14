import React from 'react';
import { LayerCard, Table } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Trash } from '../../components/Icons.jsx';
import { formatDate, statusVariant } from './utils.jsx';

function PagesDomainsDialog({
  pagesDomainState,
  setPagesDomainState,
  isArmed,
  onAddPagesDomain,
  onDeletePagesDomain,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">Pages 自定义域名：{pagesDomainState.project?.name}</Dialog.Title>
      <div className="grid grid-cols-1 gap-3 cq-md:grid-cols-[1fr_auto]">
        <Input size="sm" label="域名" value={pagesDomainState.domain} onChange={(event) => setPagesDomainState((prev) => ({ ...prev, domain: event.target.value }))} placeholder="www.example.com" />
        <div className="flex items-end"><Button size="sm" onClick={onAddPagesDomain}>添加域名</Button></div>
      </div>
      <LayerCard className="overflow-x-auto p-0">
        <Table>
          <Table.Header variant="compact">
            <Table.Row><Table.Head>域名</Table.Head><Table.Head>状态</Table.Head><Table.Head>验证状态</Table.Head><Table.Head>创建时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
          </Table.Header>
          <Table.Body>
            {pagesDomainState.domains.length === 0 ? (
              <Table.Row><Table.Cell colSpan={5} className="py-8 text-center text-kumo-subtle">没有自定义域名。</Table.Cell></Table.Row>
            ) : pagesDomainState.domains.map((domain) => (
              <Table.Row key={domain.id || domain.name}>
                <Table.Cell>{domain.name}</Table.Cell>
                <Table.Cell><Badge variant={statusVariant(domain.status)}>{domain.status || '未知'}</Badge></Table.Cell>
                <Table.Cell>{domain.validationStatus || '-'}</Table.Cell>
                <Table.Cell>{formatDate(domain.createdOn)}</Table.Cell>
                <Table.Cell className="text-right"><Button size="sm" shape="square" variant={isArmed(`pages-domain:${domain.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeletePagesDomain(domain)} aria-label={`删除 ${domain.name}`} title="删除" icon={<Trash className="h-4 w-4" />} /></Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </LayerCard>
    </div>
  );
}

export default PagesDomainsDialog;
