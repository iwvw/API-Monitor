import React from 'react';
import { LayerCard, Table } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { ExternalLink, Trash } from '../../components/Icons.jsx';
import { formatDate, statusVariant } from './utils.jsx';

function PagesDeploymentsDialog({
  pagesDeployState,
  isArmed,
  onDeletePagesDeployment,
}) {
  return (
    <div className="flex flex-col gap-4">
      <Dialog.Title className="text-base font-semibold text-kumo-strong">Pages 部署：{pagesDeployState.project?.name}</Dialog.Title>
      <LayerCard className="overflow-x-auto p-0">
        <Table>
          <Table.Header variant="compact">
            <Table.Row><Table.Head>地址</Table.Head><Table.Head>环境</Table.Head><Table.Head>状态</Table.Head><Table.Head>创建时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
          </Table.Header>
          <Table.Body>
            {pagesDeployState.deployments.length === 0 ? (
              <Table.Row><Table.Cell colSpan={5} className="py-8 text-center text-kumo-subtle">没有部署记录。</Table.Cell></Table.Row>
            ) : pagesDeployState.deployments.map((deployment) => (
              <Table.Row key={deployment.id}>
                <Table.Cell><div className="truncate">{deployment.url || '-'}</div></Table.Cell>
                <Table.Cell>{deployment.environment || '-'}</Table.Cell>
                <Table.Cell><Badge variant={statusVariant(deployment.status)}>{deployment.status || '未知'}</Badge></Table.Cell>
                <Table.Cell>{formatDate(deployment.createdOn)}</Table.Cell>
                <Table.Cell className="text-right">
                  <div className="inline-flex gap-2">
                    {deployment.url && <LinkButton size="sm" shape="square" variant="secondary" href={deployment.url} external aria-label="打开部署地址" title="打开" icon={<ExternalLink className="h-4 w-4" />} />}
                    <Button size="sm" shape="square" variant={isArmed(`pages-deployment:${deployment.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeletePagesDeployment(deployment)} aria-label="删除部署" title="删除" icon={<Trash className="h-4 w-4" />} />
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

export default PagesDeploymentsDialog;
