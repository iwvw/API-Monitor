import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppTable, DataTableFrame, EmptyState, SectionCard, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Cloud, Globe, Plus, Shield } from '../../components/Icons.jsx';
import { ADDRESS_TABLE_COLUMNS, FIREWALL_TABLE_COLUMNS } from './constants.js';
import { getGcpStatusTone } from './utils.jsx';
import { FirewallActions } from './ActionsPanels.jsx';

export default function NetworkPanel({ scopeReady, loadingProjectScope, firewalls, addresses, onOpenFirewallDialog, onOpenDetail, onEditFirewall, onDeleteFirewall }) {
  return (
    <div className="columns-1 gap-3 lg:columns-2 [&>*]:mb-3 [&>*]:break-inside-avoid">
      <SectionCard
        title="防火墙规则"
        icon={<Shield className="h-4 w-4" />}
        actions={scopeReady && (
          <Button type="button" size="sm" variant="primary" onClick={() => onOpenFirewallDialog()}>
            <Plus className="h-4 w-4" />新建规则
          </Button>
        )}
        bodyPadding="none"
      >
        {!scopeReady ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
        ) : loadingProjectScope ? (
          <div className="p-4"><SkeletonLine className="h-5 w-full" /></div>
        ) : firewalls.length === 0 ? (
          <EmptyState card={false} icon={Shield} title="暂无防火墙规则" className="min-h-48" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="gcp-firewalls" columns={FIREWALL_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>方向</Table.Head>
                  <Table.Head>动作</Table.Head>
                  <Table.Head>优先级</Table.Head>
                  <Table.Head>网络</Table.Head>
                  <Table.Head>操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {firewalls.map((rule) => (
                  <Table.Row key={rule.name}>
                    <Table.Cell><Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('firewall', rule)} title={`${rule.name} · 点击查看详情`}>{rule.name}</Button></Table.Cell>
                    <Table.Cell className="text-sm text-kumo-strong">{rule.direction || '-'}</Table.Cell>
                    <Table.Cell><StatusBadge tone={rule.action === 'ALLOW' ? 'success' : 'danger'}>{rule.action || '-'}</StatusBadge></Table.Cell>
                    <Table.Cell>{rule.priority}</Table.Cell>
                    <Table.Cell className="truncate text-sm text-kumo-strong" title={rule.network}>{rule.network || '-'}</Table.Cell>
                    <Table.Cell><FirewallActions fw={rule} onEdit={onEditFirewall} onDelete={onDeleteFirewall} /></Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
      <SectionCard title="静态 IP" icon={<Globe className="h-4 w-4" />} bodyPadding="none">
        {!scopeReady ? (
          <EmptyState card={false} icon={Cloud} title="请选择账号与项目" className="min-h-48" />
        ) : addresses.length === 0 ? (
          <EmptyState card={false} icon={Globe} title="暂无静态 IP" className="min-h-48" />
        ) : (
          <DataTableFrame variant="embedded" density="dense" className="overflow-auto">
            <AppTable tableId="gcp-addresses" columns={ADDRESS_TABLE_COLUMNS}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head>名称</Table.Head>
                  <Table.Head>地址</Table.Head>
                  <Table.Head>区域</Table.Head>
                  <Table.Head>状态</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {addresses.map((address) => (
                  <Table.Row key={address.id || address.name}>
                    <Table.Cell><Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" onClick={() => onOpenDetail('address', address)} title={`${address.name} · 点击查看详情`}>{address.name}</Button></Table.Cell>
                    <Table.Cell className="font-mono text-xs">{address.address || '-'}</Table.Cell>
                    <Table.Cell className="text-sm text-kumo-strong">{address.region || '-'}</Table.Cell>
                    <Table.Cell><StatusBadge tone={getGcpStatusTone(address.status)}>{address.status || '-'}</StatusBadge></Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </AppTable>
          </DataTableFrame>
        )}
      </SectionCard>
    </div>
  );
}
