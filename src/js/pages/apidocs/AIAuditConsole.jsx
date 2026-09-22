import React, { useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { LayerCard, Loader, Pagination, Table } from '@cloudflare/kumo';
import { AppCard, AppTable, EmptyState, StatusBadge } from '../../components/ui/AppPrimitives.jsx';
import { Activity, Eye, Search } from '../../components/Icons.jsx';
import { formatAuditDetails, formatDateTime } from './utils.js';

const AI_AUDIT_COLUMNS = [
  { id: 'time', role: 'datetime', align: 'center' },
  { id: 'agent', role: 'identifier', align: 'center' },
  { id: 'action', role: 'meta', align: 'center' },
  { id: 'target', role: 'content', minWidth: 190, grow: 1, align: 'center', verticalAlign: 'middle' },
  { id: 'status', role: 'status' },
  { id: 'latency', role: 'number' },
  { id: 'ip', role: 'identifier', align: 'center' },
  { id: 'details', role: 'content', minWidth: 240, grow: 1, align: 'center', verticalAlign: 'middle' },
];

export default function AIAuditConsole({
  records,
  total,
  page,
  pageSize,
  loading,
  error,
  actionFilter,
  searchText,
  onActionFilterChange,
  onSearchTextChange,
  onClearFilters,
  onPageChange,
  onPageSizeChange,
  onRefresh,
}) {
  const [selected, setSelected] = useState(null);

  if (loading && records.length === 0) {
    return (
      <AppCard padding="lg">
        <SkeletonLine className="h-5 w-36" />
        <SkeletonLine className="mt-4 h-80 w-full" />
      </AppCard>
    );
  }

  if (error) {
    return (
      <EmptyState
        icon={Activity}
        title="调用审计暂不可用"
        description={error}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            重试
          </Button>
        }
      />
    );
  }

  return (
    <>
    <LayerCard className="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden p-0 shadow-none">
      <div className="flex items-center gap-2 border-b border-kumo-line px-3 py-2">
        <Select alignItemWithTrigger
          value={actionFilter}
          onValueChange={onActionFilterChange}
          className="w-[140px]"
          size="sm"
          aria-label="操作类型"
          items={[
            { value: '', label: '全部操作' },
            { value: 'mcp.describe', label: 'mcp.describe' },
            { value: 'tools/call', label: 'tools/call' },
            { value: 'manifest', label: 'manifest' },
            { value: 'notifications/cancelled', label: 'notifications/cancelled' },
          ]}
        />
        <div className="relative flex-1 max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-kumo-subtle" />
          <Input
            value={searchText}
            onChange={e => onSearchTextChange(e.target.value)}
            placeholder="搜索时间、动作、目标、IP..."
            className="w-full pl-7"
            size="sm"
            aria-label="搜索调用日志"
          />
        </div>
        {searchText || actionFilter ? (
          <Button size="sm" variant="ghost" onClick={onClearFilters}>
            清除
          </Button>
        ) : null}
      </div>
      <div className="min-h-0 min-w-0 flex-1 overflow-auto scrollbar-thin">
        <AppTable tableId="ai-audit-records" columns={AI_AUDIT_COLUMNS} className="min-w-[1080px] [&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="text-center">时间</Table.Head>
              <Table.Head className="text-center">Agent</Table.Head>
              <Table.Head className="text-center">动作</Table.Head>
              <Table.Head className="text-center">目标</Table.Head>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head className="text-center">耗时</Table.Head>
              <Table.Head className="text-center">IP</Table.Head>
              <Table.Head className="text-center">详情</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {loading ? (
              <Table.Row>
                <Table.Cell colSpan={8} className="py-8 text-center">
                  <Loader size={20} className="mx-auto text-kumo-subtle" />
                </Table.Cell>
              </Table.Row>
            ) : records.length === 0 ? (
              <Table.Row>
                <Table.Cell colSpan={8} className="py-8 text-center text-sm text-kumo-subtle">
                  暂无审计记录
                </Table.Cell>
              </Table.Row>
            ) : (
              records.map(item => (
                <Table.Row key={item.id} className="text-sm">
                  <Table.Cell className="truncate text-center font-mono text-kumo-subtle">
                    {formatDateTime(item.createdAt)}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono text-kumo-subtle"
                    title={item.agentName}
                  >
                    {item.agentName || '-'}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono font-medium text-kumo-strong"
                    title={item.action}
                  >
                    {item.action}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono text-kumo-subtle"
                    title={item.target || item.details}
                  >
                    {item.target || item.details || '-'}
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <StatusBadge tone={item.status === 'success' ? 'success' : 'danger'}>
                      {item.status}
                    </StatusBadge>
                  </Table.Cell>
                  <Table.Cell className="text-center font-mono text-kumo-subtle">
                    {item.latencyMs != null ? `${item.latencyMs}ms` : '-'}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono text-kumo-subtle"
                    title={item.ipAddress}
                  >
                    {item.ipAddress || '—'}
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => setSelected(item)}
                      title="查看完整详情"
                      className="h-auto max-w-full gap-1 px-1.5 py-0.5 text-kumo-subtle hover:text-kumo-strong"
                    >
                      <span className="truncate">{item.details || '查看'}</span>
                      <Eye className="h-3.5 w-3.5 shrink-0" />
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))
            )}
          </Table.Body>
        </AppTable>
      </div>

      {total > 0 && (
        <Pagination
          page={page}
          setPage={onPageChange}
          perPage={pageSize}
          totalCount={total}
          labels={{
            navigation: '调用审计分页',
            firstPage: '第一页',
            previousPage: '上一页',
            nextPage: '下一页',
            lastPage: '最后一页',
            pageNumber: '页码',
            pageSize: '每页数量',
          }}
          className="shrink-0 flex-wrap gap-x-3 gap-y-1 border-x-0 border-b-0 border-t border-kumo-line bg-kumo-base px-3 py-2 text-sm shadow-none [&_[data-slot=pagination-controls]]:ml-auto [&_[data-slot=pagination-info]]:min-w-0 max-sm:[&_[data-slot=pagination-info]]:hidden max-sm:[&_[data-slot=pagination-page-size]]:hidden max-sm:[&_[data-slot=pagination-separator]]:hidden max-sm:[&_[data-slot=pagination-controls]]:m-auto"
        >
          <Pagination.Info>
            {({ pageShowingRange, totalCount }) => (
              <span className="text-kumo-subtle">
                显示 {pageShowingRange}，共 {totalCount} 条
              </span>
            )}
          </Pagination.Info>
          <Pagination.Separator />
          <Pagination.PageSize
            value={pageSize}
            onChange={size => onPageSizeChange(size)}
            options={[10, 20, 50, 100]}
            label="每页"
          />
          <Pagination.Controls />
        </Pagination>
      )}

      <LayerDialog.Root open={!!selected} onOpenChange={open => !open && setSelected(null)}>
        <LayerDialog.Content size="base">
          <LayerDialog.Title>审计详情</LayerDialog.Title>
          <LayerDialog.Description>
            {selected ? `#${selected.id} · ${formatDateTime(selected.createdAt)}` : ''}
          </LayerDialog.Description>
          <LayerDialog.Body>
          <div className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
            {[
              { label: 'Agent', value: selected?.agentName || '-' },
              { label: '动作', value: selected?.action || '-' },
              { label: '目标', value: selected?.target || '-' },
              {
                label: '状态',
                value: selected?.status || '-',
                pill: selected?.status === 'success' ? 'success' : 'danger',
              },
              { label: '耗时', value: selected?.latencyMs != null ? `${selected.latencyMs}ms` : '-' },
              { label: 'IP', value: selected?.ipAddress || '—' },
            ].map(field => (
              <div key={field.label}>
                <div className="mb-0.5 text-kumo-subtle">{field.label}</div>
                <div className="break-all font-mono text-kumo-strong">
                  {field.pill ? (
                    <StatusBadge tone={field.pill}>{field.value}</StatusBadge>
                  ) : (
                    field.value
                  )}
                </div>
              </div>
            ))}
            <div className="col-span-2">
              <div className="mb-0.5 text-kumo-subtle">User-Agent</div>
              <div className="break-all rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2 font-mono text-kumo-strong">
                {selected?.userAgent || '—'}
              </div>
            </div>
            <div className="col-span-2">
              <div className="mb-0.5 text-kumo-subtle">详情</div>
              <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-md border border-kumo-line bg-kumo-recessed/40 p-3 font-mono leading-relaxed text-kumo-strong">
                {selected ? formatAuditDetails(selected.details) : ''}
              </pre>
            </div>
          </div>
          </LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>
    </LayerCard>
    </>
  );
}
