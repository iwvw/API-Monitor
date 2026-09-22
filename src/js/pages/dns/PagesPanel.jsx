import React from 'react';
import { AppTable, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button, LinkButton } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ExternalLink, Layers, Trash } from '../../components/Icons.jsx';
import { statusVariant } from './utils.jsx';

const DNS_PAGE_COLUMNS = [
  { id: 'name', role: 'primary', grow: 1 },
  { id: 'subdomain', role: 'identifier', grow: 1 },
  { id: 'productionBranch', role: 'meta' },
  { id: 'latestDeployment', role: 'status' },
  { id: 'actions', role: 'actions-lg' },
];

function PagesPanel({
  loading,
  pages,
  pageColWidths,
  startPageResize,
  isArmed,
  openPagesDeploymentsModal,
  openPagesDomainsModal,
  deletePagesProject,
}) {
  return (
    <SectionCard
      title="Pages 项目"
      icon={<Layers className="h-4 w-4 text-brand" />}
      bodyPadding="none"
      bodyClassName="overflow-x-auto"
    >
      <AppTable tableId="dns-pages" columns={DNS_PAGE_COLUMNS} columnWidths={pageColWidths}>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head className="relative pr-6">项目<Table.ResizeHandle onMouseDown={(e) => startPageResize(0, e)} onTouchStart={(e) => startPageResize(0, e)} /></Table.Head>
            <Table.Head className="relative pr-6">访问地址<Table.ResizeHandle onMouseDown={(e) => startPageResize(1, e)} onTouchStart={(e) => startPageResize(1, e)} /></Table.Head>
            <Table.Head className="relative pr-6">生产分支<Table.ResizeHandle onMouseDown={(e) => startPageResize(2, e)} onTouchStart={(e) => startPageResize(2, e)} /></Table.Head>
            <Table.Head className="relative pr-6">最新部署<Table.ResizeHandle onMouseDown={(e) => startPageResize(3, e)} onTouchStart={(e) => startPageResize(3, e)} /></Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {loading.pages ? (
            Array.from({ length: 4 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
          ) : pages.length === 0 ? (
            <Table.Row><Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">没有 Pages 项目。</Table.Cell></Table.Row>
          ) : pages.map((project) => (
            <Table.Row key={project.name}>
              <Table.Cell className="font-medium text-kumo-strong">{project.name}</Table.Cell>
              <Table.Cell>
                {project.subdomain ? (
                  <LinkButton size="sm" variant="secondary" href={`https://${project.subdomain}`} external icon={<ExternalLink className="h-4 w-4" />}>
                    打开
                  </LinkButton>
                ) : '-'}
              </Table.Cell>
              <Table.Cell>{project.productionBranch || '-'}</Table.Cell>
              <Table.Cell>
                <Badge variant={statusVariant(project.latestDeployment?.status)}>
                  {project.latestDeployment?.status || '未知'}
                </Badge>
              </Table.Cell>
              <Table.Cell className="text-right">
                <div className="inline-flex flex-wrap justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={() => openPagesDeploymentsModal(project)}>部署</Button>
                  <Button size="sm" variant="secondary" onClick={() => openPagesDomainsModal(project)}>域名</Button>
                  <Button size="sm" variant={isArmed(`pages-project:${project.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deletePagesProject(project)} aria-label={`删除 ${project.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                </div>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </AppTable>
    </SectionCard>
  );
}

export default PagesPanel;
