import React, { useMemo, useState } from 'react';
import { Badge, Empty, Loader } from '@cloudflare/kumo';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Table } from '@cloudflare/kumo/components/table';
import { AppTable, DataTableFrame } from '../../components/ui/AppPrimitives.jsx';
import { Plug, RefreshCw, Plus } from '../../components/Icons.jsx';
import { CATEGORY_LABEL } from './constants.js';
import { formatExpireAt } from './utils.js';

const CANDIDATE_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'name', role: 'primary', minWidth: 200, maxWidth: 280, grow: 0 },
  { id: 'provider', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'expire', role: 'date', grow: 1, minWidth: 120 },
  { id: 'state', role: 'status' },
];

export default function SourcePickerPanel({ groups, loading, linking, onLink, onRefreshAll }) {
  const [selected, setSelected] = useState({});
  const [collapsed, setCollapsed] = useState({});

  const totalSelectable = useMemo(
    () => groups.reduce((sum, group) => sum + group.items.filter(item => !item.linked).length, 0),
    [groups],
  );

  const selectedCount = Object.keys(selected).length;

  const toggle = (module, refID) => {
    const key = `${module}\x00${refID}`;
    setSelected(prev => {
      const next = { ...prev };
      if (next[key]) delete next[key];
      else next[key] = { source_module: module, source_ref_id: refID };
      return next;
    });
  };

  const toggleGroup = (group, checked) => {
    setSelected(prev => {
      const next = { ...prev };
      group.items.forEach(item => {
        if (item.linked) return;
        const key = `${group.module}\x00${item.source_ref_id}`;
        if (checked) next[key] = { source_module: group.module, source_ref_id: item.source_ref_id };
        else delete next[key];
      });
      return next;
    });
  };

  const submit = async () => {
    const links = Object.values(selected);
    if (links.length === 0) return;
    const ok = await onLink(links);
    // 仅成功后清空选择：失败时保留用户勾选，避免白选一次。
    if (ok) setSelected({});
  };

  if (!loading && groups.length === 0) {
    return (
      <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
        <Empty
          size="base"
          className="rounded-none border-0 bg-transparent"
          icon={<Plug className="h-8 w-8 text-kumo-secondary" />}
          title="暂无可纳管的来源"
          description="面板中尚未有可纳管的对象，或来源模块数据为空"
        />
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3 cq-sm:gap-4">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <Plug className="h-4 w-4 shrink-0 text-kumo-info" />
          <span className="text-sm font-semibold text-kumo-strong">纳管来源</span>
          <span className="truncate text-xs text-kumo-subtle">从已有模块中勾选对象纳管为资产；纳管为引用，不复制来源数据</span>
        </div>
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
          <Badge variant="neutral">{totalSelectable} 个可纳管</Badge>
          <Button
            size="sm"
            variant="secondary"
            icon={<RefreshCw className="h-3.5 w-3.5" />}
            loading={linking === 'refresh'}
            onClick={onRefreshAll}
          >刷新纳管快照</Button>
          <Button
            size="sm"
            variant="primary"
            icon={<Plus className="h-3.5 w-3.5" />}
            disabled={selectedCount === 0}
            loading={linking === 'link'}
            onClick={submit}
          >纳管选中（{selectedCount}）</Button>
        </div>
      </div>
      {loading && groups.length === 0 ? (
        <div className="flex items-center justify-center overflow-hidden rounded-lg border border-kumo-line py-8">
          <Loader size={28} className="text-kumo-info" />
        </div>
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          {groups.map(group => {
            const isCollapsed = collapsed[group.module];
            const selectable = group.items.filter(item => !item.linked);
            const allSelected = selectable.length > 0
              && selectable.every(item => selected[`${group.module}\x00${item.source_ref_id}`]);
            return (
              <div key={group.module} className="min-w-0 overflow-hidden rounded-lg border border-kumo-line">
                <div className="flex min-w-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <Checkbox
                      size="sm"
                      aria-label={`全选 ${group.label}`}
                      checked={allSelected}
                      disabled={selectable.length === 0}
                      onCheckedChange={checked => toggleGroup(group, checked)}
                    />
                    <span className="text-sm font-semibold text-kumo-strong">{group.label}</span>
                    <Badge variant="neutral">{CATEGORY_LABEL[group.category] || group.category}</Badge>
                    <span className="text-xs text-kumo-subtle">{group.items.length} 个</span>
                  </div>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setCollapsed(prev => ({ ...prev, [group.module]: !prev[group.module] }))}
                  >{isCollapsed ? '展开' : '收起'}</Button>
                </div>
                {!isCollapsed && (
                  <DataTableFrame variant="embedded">
                    <AppTable tableId={`candidates-${group.module}`} columns={CANDIDATE_COLUMNS}>
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head className="text-center" />
                          <Table.Head>名称</Table.Head>
                          <Table.Head>提供方</Table.Head>
                          <Table.Head>到期日</Table.Head>
                          <Table.Head className="text-center">状态</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {group.items.length === 0 ? (
                          <Table.Row>
                            <Table.Cell colSpan={5} className="py-6 text-center text-kumo-subtle">该来源暂无对象</Table.Cell>
                          </Table.Row>
                        ) : (
                          group.items.map(item => {
                            const key = `${group.module}\x00${item.source_ref_id}`;
                            return (
                              <Table.Row key={key} className={item.linked ? 'opacity-60' : 'hover:bg-kumo-recessed/25'}>
                                <Table.Cell className="text-center">
                                  <Checkbox
                                    size="sm"
                                    aria-label={`纳管 ${item.name}`}
                                    checked={Boolean(selected[key])}
                                    disabled={item.linked}
                                    onCheckedChange={() => toggle(group.module, item.source_ref_id)}
                                  />
                                </Table.Cell>
                                <Table.Cell className="truncate text-sm font-medium text-kumo-strong" title={item.name}>{item.name || item.source_ref_id}</Table.Cell>
                                <Table.Cell className="truncate text-kumo-subtle" title={item.provider}>{item.provider || '--'}</Table.Cell>
                                <Table.Cell className="whitespace-nowrap text-kumo-subtle">{formatExpireAt(item.expire_at)}</Table.Cell>
                                <Table.Cell className="text-center">
                                  {item.linked
                                    ? <Badge variant="success">已纳管</Badge>
                                    : <Badge variant="neutral">可纳管</Badge>}
                                </Table.Cell>
                              </Table.Row>
                            );
                          })
                        )}
                      </Table.Body>
                    </AppTable>
                  </DataTableFrame>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
