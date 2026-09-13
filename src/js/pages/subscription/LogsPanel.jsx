import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo';
import { AppTable, DataTableFrame } from '../../components/ui/AppPrimitives.jsx';
import { SUBSCRIPTION_LOG_COLUMNS } from './constants.js';
import { formatBytes, formatTime } from './utils.js';

export default function LogsPanel({ logs, subscriptions }) {
  return (
    <DataTableFrame>
      <AppTable tableId="subscription-access-logs" columns={SUBSCRIPTION_LOG_COLUMNS}>
          <Table.Header>
            <Table.Row>
              <Table.Head>时间</Table.Head>
              <Table.Head>对外订阅</Table.Head>
              <Table.Head>客户端</Table.Head>
              <Table.Head>格式</Table.Head>
              <Table.Head>结果</Table.Head>
              <Table.Head>节点</Table.Head>
              <Table.Head>流量快照</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {logs.map((log) => (
              <Table.Row key={log.id}>
                <Table.Cell className="text-xs">{formatTime(log.created_at)}</Table.Cell>
                <Table.Cell className="text-xs">{subscriptions.find((item) => item.id === log.subscription_id)?.name || log.subscription_id || '-'}</Table.Cell>
                <Table.Cell><div className="truncate font-mono text-[11px]">{log.ip_address}</div><div className="truncate text-[10px] text-kumo-subtle">{log.user_agent}</div></Table.Cell>
                <Table.Cell className="text-xs">{log.format || '-'}</Table.Cell>
                <Table.Cell><Badge variant={log.success ? 'success' : 'error'} appearance="dot">{log.success ? '成功' : log.error_message || log.status_code}</Badge></Table.Cell>
                <Table.Cell className="text-xs">{log.node_count}</Table.Cell>
                <Table.Cell className="text-xs">{formatBytes((log.upload_bytes || 0) + (log.download_bytes || 0))} / {formatBytes(log.total_bytes || 0)}</Table.Cell>
              </Table.Row>
            ))}
            {logs.length === 0 && (
              <Table.Row><Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">暂无访问日志。</Table.Cell></Table.Row>
            )}
          </Table.Body>
      </AppTable>
    </DataTableFrame>
  );
}
