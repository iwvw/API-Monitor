import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { getOSIconClass, getServerPlatformLabel } from '../../modules/osPlatform.js';
import CountryFlag from '../../components/CountryFlag.jsx';
import { Plus } from '../../components/Icons.jsx';
import { RUNTIME_HOST_COLUMNS } from './constants.js';
import { formatInstanceUptime, getInstanceCountryCode, getInstanceLocationLabel, nodeTypeBadgeVariant } from './utils.js';

export default function InstancesPanel({
  servers, selectedRuntimeHosts, setSelectedRuntimeHosts, runtimeLifecycleServers, saving,
  internalNodes, runtimeByServer, managedTunnels, isArmed,
  onDeployProxyRuntime, onUninstallProxyRuntime, onUninstallTunnel, onOpenTunnelDeployment,
}) {
  return (
    <SectionCard
      title={`Linux 主机 (${servers.length})`}
      bodyPadding="none"
      actions={<Button size="sm" variant="primary" loading={saving} disabled={selectedRuntimeHosts.size === 0} onClick={() => onDeployProxyRuntime([...selectedRuntimeHosts])}><Plus className="h-3.5 w-3.5" />批量部署程序 ({selectedRuntimeHosts.size})</Button>}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="runtime-hosts" columns={RUNTIME_HOST_COLUMNS} className="text-xs [&_td]:border-kumo-interact/45 [&_th]:border-kumo-interact/50">
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.CheckHead checked={runtimeLifecycleServers.length > 0 && selectedRuntimeHosts.size === runtimeLifecycleServers.length} indeterminate={selectedRuntimeHosts.size > 0 && selectedRuntimeHosts.size < runtimeLifecycleServers.length} onCheckedChange={(checked) => setSelectedRuntimeHosts(checked ? new Set(runtimeLifecycleServers.map((server) => server.id)) : new Set())} />
              <Table.Head className="text-left">名称</Table.Head>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head className="text-center">位置</Table.Head>
              <Table.Head className="text-center">在线</Table.Head>
              <Table.Head className="text-center">Agent 版本</Table.Head>
              <Table.Head className="text-center">代理服务</Table.Head>
              <Table.Head className="text-center">节点类型</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {servers.map((server) => {
              const managed = internalNodes.filter((node) => node.server_id === server.id);
              const runtime = runtimeByServer.get(String(server.id));
              const tunnel = managedTunnels.find((item) => item.server_id === server.id);
              const countryCode = getInstanceCountryCode(server);
              const locationLabel = getInstanceLocationLabel(server);
              const supportsRuntimeLifecycle = server.agent_capabilities?.proxy_runtime_lifecycle_v2 === true;
              return (
                <Table.Row key={server.id}>
                  <Table.CheckCell disabled={!supportsRuntimeLifecycle || server.status !== 'online'} checked={selectedRuntimeHosts.has(server.id)} onCheckedChange={(checked) => setSelectedRuntimeHosts((previous) => { const next = new Set(previous); if (checked) next.add(server.id); else next.delete(server.id); return next; })} />
<Table.Cell><div className="flex min-w-0 items-center gap-2"><i className={getOSIconClass(getServerPlatformLabel(server), { offline: server.status !== 'online' })} title={getServerPlatformLabel(server) || 'Linux'} /><span className={`truncate font-semibold ${server.status === 'online' ? 'text-kumo-strong' : 'text-kumo-subtle'}`} title={server.name}>{server.name}</span></div></Table.Cell>
						<Table.Cell className="text-center"><Badge variant={server.status === 'online' ? 'success' : server.status === 'offline' ? 'error' : 'neutral'} appearance="dot">{server.status === 'online' ? '在线' : server.status === 'offline' ? '离线' : '未知'}</Badge></Table.Cell>
                  <Table.Cell className="text-center"><div className="mx-auto flex w-[64px] items-center justify-center gap-1.5">{countryCode && <CountryFlag preferSvg countryCode={countryCode} className="h-3.5 w-5 shrink-0 !rounded-[2px] text-sm" />}<span className="truncate font-semibold uppercase text-kumo-strong" title={server.location || locationLabel}>{locationLabel}</span></div></Table.Cell>
                  <Table.Cell className="text-center"><span className="font-semibold tabular-nums text-kumo-strong">{formatInstanceUptime(server.uptime)}</span></Table.Cell>
                  <Table.Cell className="text-center"><span className="font-mono text-xs">{server.agent_version && server.agent_version !== '<nil>' ? server.agent_version : '未报告'}</span></Table.Cell>
					<Table.Cell className="text-center"><div className="flex min-w-0 flex-nowrap items-center justify-center gap-2 px-2">{runtime ? <Badge className="shrink-0" variant={runtime.apply_status === 'running' ? 'success' : ['failed', 'drifted'].includes(runtime.apply_status) ? 'error' : 'warning'}>{runtime.apply_status === 'running' ? `sing-box${runtime.version ? ` ${runtime.version}` : ''}` : runtime.apply_status === 'failed' ? '部署失败' : runtime.apply_status === 'drifted' ? '状态漂移' : '部署中'}</Badge> : <Badge variant="neutral" className="shrink-0">未安装</Badge>}{server.status === 'online' && !supportsRuntimeLifecycle && <Badge variant="warning" className="shrink-0">需升级 Agent</Badge>}{tunnel && <Badge className="shrink-0" variant={tunnel.apply_status === 'running' ? 'success' : tunnel.apply_status === 'failed' ? 'error' : 'warning'} title={tunnel.apply_status === 'disconnected' && tunnel.last_error ? tunnel.last_error : undefined}>Tunnel {tunnel.apply_status === 'running' ? '已连接' : tunnel.apply_status === 'disconnected' ? '已断开' : tunnel.apply_status}</Badge>}</div></Table.Cell>
					<Table.Cell className="text-center"><div className="flex flex-nowrap justify-center gap-1">{managed.map((node) => <Badge key={node.id} variant={nodeTypeBadgeVariant(node.protocol)}>{node.protocol === 'hysteria2' ? 'HY2' : node.protocol === 'socks' ? 'SOCKS5' : node.protocol === 'http' ? 'HTTP' : 'VLESS'}</Badge>)}{managed.length === 0 && <span className="text-xs text-kumo-subtle">—</span>}</div></Table.Cell>
                  <Table.Cell className="text-center"><div className="flex w-full flex-nowrap items-center justify-center gap-1">{runtime?.apply_status === 'running' ? <><Button size="sm" variant="secondary" onClick={() => onDeployProxyRuntime(server.id)} disabled={!supportsRuntimeLifecycle || saving} title={!supportsRuntimeLifecycle ? '请先升级 Agent' : undefined}>升级 / 重装</Button><Button size="sm" variant={isArmed(`runtime-uninstall:${server.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onUninstallProxyRuntime(server)} disabled={saving || managed.length > 0 || !supportsRuntimeLifecycle} title={managed.length > 0 ? '请先在节点管理中卸载该实例的全部节点' : !supportsRuntimeLifecycle ? '请先升级 Agent' : '卸载 sing-box'}>{isArmed(`runtime-uninstall:${server.id}`) ? '再次确认' : '卸载程序'}</Button></> : <Button size="sm" variant="secondary" onClick={() => onDeployProxyRuntime(server.id)} disabled={!supportsRuntimeLifecycle || saving} title={!supportsRuntimeLifecycle ? '请先升级 Agent' : undefined}>安装代理</Button>}{tunnel ? <Button size="sm" variant={isArmed(`managed-tunnel-delete:${server.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onUninstallTunnel(server)}>{isArmed(`managed-tunnel-delete:${server.id}`) ? '再次确认' : '卸载 Tunnel'}</Button> : <Button size="sm" variant="secondary" onClick={() => onOpenTunnelDeployment(server)} disabled={server.status !== 'online'}>部署 Tunnel</Button>}</div></Table.Cell>
                </Table.Row>
              );
            })}
            {servers.length === 0 && <Table.Row><Table.Cell colSpan={9} className="p-6 text-center text-kumo-subtle">暂无 Linux 主机</Table.Cell></Table.Row>}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );
}
