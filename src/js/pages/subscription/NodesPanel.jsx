import React from 'react';
import { Button, RefreshButton } from '@cloudflare/kumo/components/button';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge, Tabs } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { Download, Edit, Plus, Star, Trash } from '../../components/Icons.jsx';
import { NODE_COLUMNS } from './constants.js';
import { latencyChipClass, managedNodeState, nodeEndpoint, nodeNetworkTagClass, nodeNetworkTags, nodeTypeBadgeVariant } from './utils.js';
import { NodeFlag, NodeHostQuality } from './components.jsx';

export default function NodesPanel({
  internalNodes, runtimeReadyServers, servers, preferredAddresses, internalNodeActions, externalNodeActions,
  filteredNodes, visibleNodes, serverNameById, isArmed,
  protocolFilter, protocolItems, tagFilter, tagItems,
  onStartInternalDeployment, onEditInternalNode, onToggleInternalNodeEnabled, onReconcileInternalNode, onDeleteInternalNode,
  onProtocolFilterChange, onTagFilterChange, onOpenImportModal, onEditNode, onToggleNodeEnabled, onDeleteNode,
}) {
  return (
    <div className="grid gap-3">
    <SectionCard
      title={`本机节点 (${internalNodes.length})`}
      className="min-h-0"
      bodyPadding="none"
      bodyClassName="min-h-0"
      actions={<Button size="sm" variant="primary" disabled={runtimeReadyServers.length === 0} onClick={() => onStartInternalDeployment()}><Plus className="h-3.5 w-3.5" />生成节点</Button>}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="internal-proxy-nodes" columns={NODE_COLUMNS}>
          <Table.Header sticky variant="compact"><Table.Row><Table.Head className="text-center">状态</Table.Head><Table.Head>节点名称</Table.Head><Table.Head className="text-center">类型</Table.Head><Table.Head>连接</Table.Head><Table.Head>主机 / 延迟</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
          <Table.Body>
            {internalNodes.map((node) => {
              const server = servers.find((item) => item.id === node.server_id);
              const protocol = node.protocol === 'vless-reality' ? 'vless' : node.protocol;
              const isTunnelNode = node.access_mode === 'cloudflare_tunnel';
              const preferredAddress = preferredAddresses.find((item) => item.id === node.preferred_address_id && item.enabled !== false)
                || preferredAddresses.find((item) => item.is_default && item.enabled !== false);
              const displayHost = isTunnelNode
                ? (node.connect_address || preferredAddress?.address || node.tunnel_hostname || node.public_host || '-')
                : (node.public_host || '-');
              const displayPort = isTunnelNode
                ? (node.connect_port || preferredAddress?.port || 443)
                : (node.assigned_port || '-');
              const connectionTags = isTunnelNode
                ? ['ws', 'tls', 'tunnel', node.runtime].filter(Boolean)
                : [node.transport, node.protocol === 'vless-reality' ? 'reality' : (node.protocol === 'hysteria2' ? 'tls' : null), node.runtime].filter(Boolean);
              const reconciling = !!internalNodeActions[`${node.id}:reconcile`];
              const deleting = !!internalNodeActions[`${node.id}:delete`];
              const deleteConfirmKey = `internal-node-delete:${node.id}`;
              const confirmingDelete = isArmed(deleteConfirmKey);
              return <Table.Row key={node.id} onDoubleClick={() => onEditInternalNode(node)} className="cursor-pointer">
                <Table.Cell className="text-center"><Switch size="sm" aria-label={node.enabled ? '停用内部节点' : '启用内部节点'} checked={!!node.enabled} onCheckedChange={(checked) => onToggleInternalNodeEnabled(node, checked)} /></Table.Cell>
                <Table.Cell><div className="flex min-w-0 items-center gap-1.5 truncate text-sm font-semibold text-kumo-strong">{node.stable && <Star className="h-3.5 w-3.5 shrink-0 text-kumo-warning" />}{node.name}{node.stable && <Badge variant="success">稳定</Badge>}</div>{!node.publishable && (() => { const [stateLabel, stateVariant] = managedNodeState(node); return <div className="mt-1"><Badge variant={stateVariant}>{stateLabel}</Badge></div>; })()}</Table.Cell>
                <Table.Cell className="text-center"><Badge variant={nodeTypeBadgeVariant(protocol)} className="uppercase">{node.protocol === 'vless-reality' ? 'VLESS' : node.protocol === 'hysteria2' ? 'HYSTERIA2' : node.protocol === 'socks' ? 'SOCKS5' : node.protocol === 'http' ? 'HTTP' : String(node.protocol || 'UNKNOWN').toUpperCase()}</Badge></Table.Cell>
                <Table.Cell><div className="truncate font-mono text-xs text-kumo-strong">{displayHost}:{displayPort}</div><div className="mt-1 flex min-w-0 flex-nowrap items-center gap-1 overflow-x-auto scrollbar-none">{connectionTags.map((tag) => <span key={tag} className={`${tag === node.runtime ? 'hidden cq-sm:inline-flex' : 'inline-flex'} shrink-0 rounded-[3px] border px-1.5 py-0.5 font-mono text-[10px] leading-4 ${nodeNetworkTagClass({ key: tag === 'tls' ? 'tls' : 'network', tone: tag })}`}>{tag}</span>)}</div></Table.Cell>
                <Table.Cell>{server?.status === 'online' ? <NodeHostQuality node={{ ...node, traffic_server_id: node.server_id }} serverNameById={serverNameById} /> : <div className="flex min-w-0 flex-col items-start gap-1"><span className="inline-flex max-w-full rounded-[3px] border border-kumo-info/25 bg-kumo-info/10 px-1.5 py-0.5 text-[10px] font-semibold leading-4 text-kumo-info"><span className="truncate">{server?.name || node.server_name || node.server_id}</span></span><span className={`inline-flex rounded-[3px] border px-1.5 py-0.5 text-[10px] font-semibold leading-4 ${latencyChipClass(0)}`}>主机离线</span></div>}</Table.Cell>
                <Table.Cell className="text-center"><div className="inline-flex items-center justify-center gap-2"><Button size="sm" shape="square" variant="secondary" aria-label={`编辑 ${node.name}`} title={`编辑 ${node.name}`} disabled={reconciling || deleting} onClick={(event) => { event.stopPropagation(); onEditInternalNode(node); }} icon={<Edit className="h-3.5 w-3.5" />} /><RefreshButton size="sm" variant="secondary" loading={reconciling} aria-label={`重新部署 ${node.name}`} title={`重新部署 ${node.name}`} disabled={reconciling || deleting} onClick={(event) => { event.stopPropagation(); onReconcileInternalNode(node); }} /><Button size="sm" shape="square" variant={confirmingDelete ? 'destructive' : 'secondary-destructive'} aria-label={confirmingDelete ? `再次确认卸载 ${node.name}` : `卸载 ${node.name}`} title={confirmingDelete ? '再次点击确认卸载' : `卸载 ${node.name}`} disabled={reconciling} loading={deleting} onClick={(event) => { event.stopPropagation(); onDeleteInternalNode(node); }} icon={<Trash className="h-3.5 w-3.5" />} /></div></Table.Cell>
              </Table.Row>;
            })}
            {internalNodes.length === 0 && <Table.Row><Table.Cell colSpan={6} className="p-6 text-center text-kumo-subtle">暂无本机节点</Table.Cell></Table.Row>}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
    <SectionCard
      title={(
        <div className="flex flex-wrap items-center gap-2 cq-sm:gap-3">
          <span>节点列表 ({filteredNodes.length})</span>
          <Tabs
            {...TOOL_TABS_PROPS}
            value={protocolFilter}
            onValueChange={(value) => onProtocolFilterChange(String(value))}
            tabs={protocolItems}
            className="min-w-max shrink-0"
            listClassName="max-w-full overflow-x-auto whitespace-nowrap scrollbar-thin"
          />
          {tagItems.length > 1 && (
            <Select alignItemWithTrigger
              size="sm"
              aria-label="标签筛选"
              value={tagFilter}
              onValueChange={(value) => onTagFilterChange(String(value))}
              items={tagItems}
              className="w-36 shrink-0"
            />
          )}
        </div>
      )}
      className="min-h-0"
      bodyPadding="none"
      bodyClassName="min-h-0"
      actions={<Button size="sm" variant="primary" onClick={() => onOpenImportModal()} aria-label="导入外部节点" title="导入外部节点"><Download className="h-3.5 w-3.5" />导入外部节点</Button>}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="external-proxy-nodes" columns={NODE_COLUMNS}>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head>节点名称</Table.Head>
              <Table.Head className="text-center">类型</Table.Head>
              <Table.Head>连接</Table.Head>
              <Table.Head>主机 / 延迟</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {filteredNodes.map((node) => {
              const networkTags = nodeNetworkTags(node);
              const deleting = !!externalNodeActions[`${node.id}:delete`];
              const deleteConfirmKey = `external-node-delete:${node.id}`;
              const confirmingDelete = isArmed(deleteConfirmKey);
              return (
                <Table.Row key={node.id} onDoubleClick={() => onEditNode(node)} className="cursor-pointer">
                  <Table.Cell className="text-center">
                    <Switch
                      size="sm"
                      aria-label={node.enabled ? '停用节点' : '启用节点'}
                      checked={!!node.enabled}
                      onCheckedChange={(checked) => onToggleNodeEnabled(node, checked)}
                    />
                  </Table.Cell>
                  <Table.Cell>
                    <div className="flex min-w-0 items-center gap-1.5 truncate text-sm font-semibold text-kumo-strong">
                      <NodeFlag node={node} />
                      {node.stable && <Star className="h-3.5 w-3.5 shrink-0 text-kumo-warning" />}
                      <span className="truncate">{node.name}</span>
                      {node.stable && <Badge variant="success">稳定</Badge>}
                    </div>
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <Badge variant={nodeTypeBadgeVariant(node.type)} className="uppercase">{node.type || '-'}</Badge>
                  </Table.Cell>
                  <Table.Cell>
                    <div className="truncate font-mono text-xs text-kumo-strong">{nodeEndpoint(node)}</div>
                    <div className="mt-1 flex min-w-0 flex-nowrap items-center gap-1 overflow-x-auto scrollbar-none">
                      {networkTags.map((tag) => (
                        <span
                          key={tag.key}
                          className={`${['fingerprint', 'path', 'alpn'].includes(tag.key) ? 'hidden cq-sm:inline-flex' : 'inline-flex'} shrink-0 min-w-0 max-w-full truncate rounded-[3px] border px-1.5 py-0.5 font-mono text-[10px] leading-4 ${nodeNetworkTagClass(tag)}`}
                          title={tag.label}
                        >
                          {tag.label}
                        </span>
                      ))}
                      {networkTags.length === 0 && <span className="font-mono text-[11px] text-kumo-subtle">-</span>}
                    </div>
                  </Table.Cell>
                  <Table.Cell>
                    <NodeHostQuality node={node} serverNameById={serverNameById} />
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <div className="inline-flex items-center justify-center gap-2">
                      <Button size="sm" shape="square" variant="secondary" aria-label="编辑节点" title="编辑节点" onClick={() => onEditNode(node)} icon={<Edit className="h-3.5 w-3.5" />} />
                      <Button
                        size="sm"
                        shape="square"
                        variant={confirmingDelete ? 'destructive' : 'secondary-destructive'}
                        aria-label={confirmingDelete ? `再次确认删除 ${node.name}` : `删除 ${node.name}`}
                        title={confirmingDelete ? '再次点击确认删除' : `删除 ${node.name}`}
                        loading={deleting}
                        onClick={(event) => {
                          event.stopPropagation();
                          onDeleteNode(node);
                        }}
                        icon={<Trash className="h-3.5 w-3.5" />}
                      />
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })}
            {filteredNodes.length === 0 && (
              <Table.Row><Table.Cell colSpan={6} className="p-8 text-center text-kumo-subtle">{visibleNodes.length === 0 ? '暂无节点。' : '没有符合筛选条件的节点。'}</Table.Cell></Table.Row>
            )}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard></div>
  );
}
