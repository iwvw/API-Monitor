import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Globe } from '../../components/Icons.jsx';
import SkeletonLines from './SkeletonLines.jsx';
import { DNS_TABLE_COLUMNS } from './constants.js';
import { getStatusTone } from './utils.js';

export default function DnsPanel({ loadingScope, scopeReady, zones }) {
  return (
    <SectionCard title="云解析 Zone" bodyPadding="none">
      {loadingScope ? (
        <SkeletonLines />
      ) : !scopeReady ? (
        <EmptyState card={false} icon={Globe} title="请选择账号与项目" description="选择华为云账号和区域项目后展示域名解析" className="min-h-64" />
      ) : zones.length === 0 ? (
        <EmptyState card={false} icon={Globe} title="暂无 Zone" description="该项目下没有 DNS zone" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" >
          <AppTable tableId="huawei-dns" columns={DNS_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>域名</Table.Head>
                <Table.Head>类型</Table.Head>
                <Table.Head>记录数</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>创建时间</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {zones.map((zone) => (
                <Table.Row key={zone.id}>
                  <Table.Cell>
                    <Button type="button" variant="ghost" size="xs" className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={zone.name}>{zone.name || '-'}</Button>
                  </Table.Cell>
                  <Table.Cell>{zone.type === 'public' ? '公网' : zone.type === 'private' ? '内网' : zone.type || '-'}</Table.Cell>
                  <Table.Cell>{zone.recordNum ?? '-'}</Table.Cell>
                  <Table.Cell><StatusBadge tone={getStatusTone(zone.status)}>{zone.status || '-'}</StatusBadge></Table.Cell>
                  <Table.Cell className="whitespace-nowrap text-xs text-kumo-subtle">{zone.createdAt || '-'}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
