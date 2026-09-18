import React from 'react';
import { Table } from '@cloudflare/kumo/components/table';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Trash } from '../../components/Icons.jsx';

export function LogPreviewDialog({ confirmEnforceLogLimits, logPreview, setLogPreview }) {
  return (
      <LayerDialog.Alert open={!!logPreview} onOpenChange={(open) => { if (!open) setLogPreview(null); }}>
        <LayerDialog.Content size="xl">
          <LayerDialog.Title>
            <span className="flex items-center gap-3">
              <Trash className="h-5 w-5 text-kumo-danger" />
              执行保留限制
            </span>
          </LayerDialog.Title>
          <LayerDialog.Description>
            将按当前保留策略清理以下{logPreview?.tables?.length || 0}张日志/自动生成表，预计删除 {logPreview?.totalDeleted ?? 0} 条记录。此操作不可恢复。
          </LayerDialog.Description>
          <LayerDialog.Body>
            {logPreview?.tables?.length > 0 && (
              <div className="max-h-64 overflow-auto rounded-lg border border-kumo-line">
                <Table layout="fixed">
                  <colgroup>
                    <col />
                    <col className="w-[96px]" />
                    <col className="w-[96px]" />
                    <col className="w-[96px]" />
                  </colgroup>
                  <Table.Header>
                    <Table.Row>
                      <Table.Head>表</Table.Head>
                      <Table.Head>当前行数</Table.Head>
                      <Table.Head>保留</Table.Head>
                      <Table.Head>删除</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {logPreview.tables.map((row) => (
                      <Table.Row key={row.table}>
                        <Table.Cell className="font-mono text-xs">{row.table}</Table.Cell>
                        <Table.Cell>{row.current}</Table.Cell>
                        <Table.Cell>{row.kept}</Table.Cell>
                        <Table.Cell className="text-kumo-danger">{row.deleted}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              </div>
            )}
            {logPreview?.sizeOverLimit && (
              <div className="mt-3 rounded-lg border border-kumo-warning/40 bg-kumo-warning/10 p-3 text-xs text-kumo-warning">
                数据库当前大小超过 {logPreview.dbSizeMB} MB 上限（当前 {logPreview.currentSizeMB}），还将循环删除各表最旧数据直至达标。
              </div>
            )}
            {logPreview?.tables?.some((row) => row.floor > 0) && (
              <div className="mt-3 rounded-lg border border-kumo-line bg-kumo-recessed p-3 text-xs text-kumo-subtle">
                已按「每个实体的最新记录」保底：单表条数低于实体数时，也不会删除各实体最新数据。
              </div>
            )}
          </LayerDialog.Body>
          <LayerDialog.Actions dismissLabel="取消">
            <LayerDialog.Actions.Primary type="button" variant="destructive" onClick={confirmEnforceLogLimits}>
              确认清理
            </LayerDialog.Actions.Primary>
          </LayerDialog.Actions>
        </LayerDialog.Content>
      </LayerDialog.Alert>
  );
}
