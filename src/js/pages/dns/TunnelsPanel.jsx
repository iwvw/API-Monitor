import React from 'react';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Edit, Lock, Plus, Trash } from '../../components/Icons.jsx';
import { tunnelStatusLabel, statusVariant, formatDate } from './utils.jsx';

function TunnelsPanel({
  loading,
  tunnels,
  tunnelColWidths,
  startTunnelResize,
  isArmed,
  setTunnelForm,
  setModal,
  openTunnelTokenModal,
  openTunnelConfigModal,
  openTunnelConnectionsModal,
  renameTunnel,
  deleteTunnel,
}) {
  return (
    <SectionCard
      title="Tunnel"
      icon={<Lock className="h-4 w-4 text-brand" />}
      action={(
        <Button size="sm" onClick={() => { setTunnelForm({ name: '' }); setModal({ type: 'tunnelCreate', data: null }); }} icon={<Plus className="h-4 w-4" />}>
          创建 Tunnel
        </Button>
      )}
      bodyPadding="none"
      bodyClassName="overflow-x-auto"
    >
      <Table layout="fixed">
        <colgroup>{tunnelColWidths.map((width, index) => <col key={index} style={{ width }} />)}</colgroup>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head className="relative pr-6">名称<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(0, e)} onTouchStart={(e) => startTunnelResize(0, e)} /></Table.Head>
            <Table.Head className="relative pr-6">状态<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(1, e)} onTouchStart={(e) => startTunnelResize(1, e)} /></Table.Head>
            <Table.Head className="relative pr-6">连接<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(2, e)} onTouchStart={(e) => startTunnelResize(2, e)} /></Table.Head>
            <Table.Head className="relative pr-6">创建时间<Table.ResizeHandle onMouseDown={(e) => startTunnelResize(3, e)} onTouchStart={(e) => startTunnelResize(3, e)} /></Table.Head>
            <Table.Head className="app-table-action">操作</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {loading.tunnels ? (
            Array.from({ length: 4 }).map((_, index) => <Table.Row key={index}><Table.Cell colSpan={5}><SkeletonLine className="h-4 w-full" /></Table.Cell></Table.Row>)
          ) : tunnels.length === 0 ? (
            <Table.Row><Table.Cell colSpan={5} className="py-10 text-center text-kumo-subtle">没有 Tunnel。</Table.Cell></Table.Row>
          ) : tunnels.map((tunnel) => (
            <Table.Row key={tunnel.id}>
              <Table.Cell>
                <div className="flex flex-col">
                  <span className="font-medium text-kumo-strong">{tunnel.name}</span>
                  <span className="text-xs text-kumo-subtle">{tunnel.id}</span>
                </div>
              </Table.Cell>
              <Table.Cell><Badge variant={statusVariant(tunnel.status)}>{tunnelStatusLabel(tunnel.status, tunnel.connections || [])}</Badge></Table.Cell>
              <Table.Cell>{tunnel.connections?.length || 0}</Table.Cell>
              <Table.Cell>{formatDate(tunnel.createdAt)}</Table.Cell>
              <Table.Cell className="text-right">
                <div className="inline-flex flex-wrap justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={() => openTunnelTokenModal(tunnel)}>令牌</Button>
                  <Button size="sm" variant="secondary" onClick={() => openTunnelConfigModal(tunnel)}>配置</Button>
                  <Button size="sm" variant="secondary" onClick={() => openTunnelConnectionsModal(tunnel)}>连接</Button>
                  <Button size="sm" shape="square" variant="secondary" onClick={() => renameTunnel(tunnel)} aria-label={`重命名 ${tunnel.name}`} title="重命名" icon={<Edit className="h-4 w-4" />} />
                  <Button size="sm" shape="square" variant={isArmed(`tunnel:${tunnel.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTunnel(tunnel)} aria-label={`删除 ${tunnel.name}`} title="删除" icon={<Trash className="h-4 w-4" />} />
                </div>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
    </SectionCard>
  );
}

export default TunnelsPanel;
