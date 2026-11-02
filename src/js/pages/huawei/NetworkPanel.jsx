import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import {
  AppTable,
  DataTableFrame,
  EmptyState,
  SectionCard,
  StatusBadge,
} from '../../components/ui/AppPrimitives.jsx';
import { Layers } from '../../components/Icons.jsx';
import SkeletonLines from './SkeletonLines.jsx';
import { EIP_TABLE_COLUMNS } from './constants.js';
import { getStatusTone } from './utils.js';

export default function NetworkPanel({ loadingScope, scopeReady, eips }) {
  return (
    <SectionCard title="弹性公网 IP" bodyPadding="none">
      {loadingScope ? (
        <SkeletonLines />
      ) : !scopeReady ? (
        <EmptyState card={false} icon={Layers} title="请选择账号与项目" description="选择华为云账号和区域项目后展示网络资源" className="min-h-64" />
      ) : eips.length === 0 ? (
        <EmptyState card={false} icon={Layers} title="暂无弹性公网 IP" description="该项目下没有公网 IP" className="min-h-64" />
      ) : (
        <DataTableFrame variant="embedded" density="dense" >
          <AppTable tableId="huawei-eips" columns={EIP_TABLE_COLUMNS}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>公网 IP</Table.Head>
                <Table.Head>状态</Table.Head>
                <Table.Head>带宽</Table.Head>
                <Table.Head>区域</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {eips.map((eip) => (
                <Table.Row key={eip.id}>
                  <Table.Cell>
                    <span className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong" title={eip.publicIp}>{eip.publicIp || '-'}</span>
                  </Table.Cell>
                  <Table.Cell><StatusBadge tone={getStatusTone(eip.status)}>{eip.status || '-'}</StatusBadge></Table.Cell>
                  <Table.Cell>{eip.bandwidth > 0 ? `${eip.bandwidth} Mbps` : '-'}</Table.Cell>
                  <Table.Cell className="truncate text-sm text-kumo-strong">{eip.region || '-'}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      )}
    </SectionCard>
  );
}
