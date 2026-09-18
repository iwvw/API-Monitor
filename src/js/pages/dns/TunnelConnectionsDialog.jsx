import React from 'react';
import { LayerCard, Table } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { formatDate } from './utils.jsx';

function TunnelConnectionsDialog({
  open,
  onOpenChange,
  tunnelConnectionState,
  onCleanupTunnelConnections,
}) {
  return (
    <LayerDialog.Root open={open} onOpenChange={onOpenChange}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>Tunnel 连接：{tunnelConnectionState.tunnel?.name}</LayerDialog.Title>
        <LayerDialog.Body>
          <div className="flex flex-col gap-4">
            <div className="flex justify-end">
              <Button size="sm" variant="secondary-destructive" onClick={() => onCleanupTunnelConnections(tunnelConnectionState.tunnel)}>
                清理全部连接
              </Button>
            </div>
            <LayerCard className="overflow-x-auto p-0">
              <Table>
                <Table.Header variant="compact">
                  <Table.Row><Table.Head>客户端</Table.Head><Table.Head>版本</Table.Head><Table.Head>架构</Table.Head><Table.Head>边缘节点</Table.Head><Table.Head>连接时间</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row>
                </Table.Header>
                <Table.Body>
                  {tunnelConnectionState.connections.length === 0 ? (
                    <Table.Row><Table.Cell colSpan={6} className="py-8 text-center text-kumo-subtle">没有活动连接。</Table.Cell></Table.Row>
                  ) : tunnelConnectionState.connections.map((connection) => (
                    <Table.Row key={connection.id || connection.clientId}>
                      <Table.Cell><code className="text-xs">{connection.clientId || connection.id || '-'}</code></Table.Cell>
                      <Table.Cell>{connection.clientVersion || '-'}</Table.Cell>
                      <Table.Cell>{connection.arch || '-'}</Table.Cell>
                      <Table.Cell>{connection.coloName || '-'}</Table.Cell>
                      <Table.Cell>{formatDate(connection.connectedAt)}</Table.Cell>
                      <Table.Cell className="text-right">
                        <Button size="sm" variant="secondary-destructive" onClick={() => onCleanupTunnelConnections(tunnelConnectionState.tunnel, connection.clientId || connection.id)}>
                          清理
                        </Button>
                      </Table.Cell>
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

export default TunnelConnectionsDialog;
