import React from 'react';
import { LayerCard, Table } from '@cloudflare/kumo';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { ExternalLink, Trash } from '../../components/Icons.jsx';
import { AppTable } from '../../components/ui/AppPrimitives.jsx';
import { formatDate, statusVariant } from './utils.jsx';

const PAGES_DEPLOYMENT_COLUMNS = [
  { id: 'url', role: 'primary', minWidth: 220, grow: 1 },
  { id: 'environment', role: 'type' },
  { id: 'status', role: 'status' },
  { id: 'createdOn', role: 'datetime' },
  { id: 'actions', role: 'actions-md' },
];

function PagesDeploymentsDialog({
  open,
  onOpenChange,
  pagesDeployState,
  isArmed,
  onDeletePagesDeployment,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>Pages 部署：{pagesDeployState.project?.name}</LayerDialog.Title>
        <LayerDialog.Body>
          <LayerCard className="overflow-x-auto p-0">
            <AppTable tableId="pages-deployments" columns={PAGES_DEPLOYMENT_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row><Table.Head>地址</Table.Head><Table.Head>环境</Table.Head><Table.Head>状态</Table.Head><Table.Head>创建时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
              </Table.Header>
              <Table.Body>
                {pagesDeployState.deployments.length === 0 ? (
                  <Table.Row><Table.Cell colSpan={5} className="py-8 text-center text-kumo-subtle">没有部署记录。</Table.Cell></Table.Row>
                ) : pagesDeployState.deployments.map((deployment) => (
                  <Table.Row key={deployment.id}>
                    <Table.Cell><div className="truncate" title={deployment.url || '-'}>{deployment.url || '-'}</div></Table.Cell>
                    <Table.Cell>{deployment.environment || '-'}</Table.Cell>
                    <Table.Cell><Badge variant={statusVariant(deployment.status)}>{deployment.status || '未知'}</Badge></Table.Cell>
                    <Table.Cell className="whitespace-nowrap">{formatDate(deployment.createdOn)}</Table.Cell>
                    <Table.Cell>
                      <div className="inline-flex gap-2">
                        {deployment.url && <LinkButton size="sm" shape="square" variant="secondary" href={deployment.url} external aria-label="打开部署地址" title="打开" icon={<ExternalLink className="h-4 w-4" />} />}
                        <Button size="sm" shape="square" variant={isArmed(`pages-deployment:${deployment.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDeletePagesDeployment(deployment)} aria-label="删除部署" title="删除" icon={<Trash className="h-4 w-4" />} />
                      </div>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </LayerCard>
        </LayerDialog.Body>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}

export default PagesDeploymentsDialog;
