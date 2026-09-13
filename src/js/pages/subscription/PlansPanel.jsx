import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { Badge } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Edit, Plus, Trash } from '../../components/Icons.jsx';
import { PLAN_COLUMNS } from './constants.js';
import { formatBytes } from './utils.js';

export default function PlansPanel({ plans, nodes, internalNodes, isArmed, onCreate, onEdit, onToggleEnabled, onDelete }) {
  return (
    <SectionCard title={`套餐管理 (${plans.length})`} bodyPadding="none" actions={<Button size="sm" variant="primary" onClick={onCreate}><Plus className="h-3.5 w-3.5" />新建套餐</Button>}>
      <DataTableFrame variant="embedded">
        <AppTable tableId="subscription-plans" columns={PLAN_COLUMNS}>
          <Table.Header sticky variant="compact"><Table.Row><Table.Head className="text-center">启用</Table.Head><Table.Head>套餐</Table.Head><Table.Head className="text-center">状态</Table.Head><Table.Head className="text-center">单订阅额度</Table.Head><Table.Head className="text-center">重置</Table.Head><Table.Head className="text-center">节点范围</Table.Head><Table.Head className="text-center">订阅</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
          <Table.Body>
            {plans.map((plan) => {
			  const externalCount = plan.selection_mode === 'all' ? (plan.include_external_nodes ? nodes.length : 0) : (plan.node_ids || []).filter((id) => nodes.some((node) => node.id === id)).length;
			  const internalCount = plan.selection_mode === 'all' ? (plan.include_internal_nodes ? internalNodes.length : 0) : (plan.node_ids || []).filter((id) => internalNodes.some((node) => node.id === id)).length;
			  return <Table.Row key={plan.id} onDoubleClick={() => onEdit(plan)} className="cursor-pointer">
              <Table.Cell className="text-center"><Switch size="sm" aria-label={plan.enabled ? `停用套餐 ${plan.name}` : `启用套餐 ${plan.name}`} checked={!!plan.enabled} onCheckedChange={(checked) => onToggleEnabled(plan, checked)} /></Table.Cell>
              <Table.Cell><div className="font-semibold text-kumo-strong">{plan.name}</div>{plan.remark ? <div className="mt-1 truncate text-[11px] text-kumo-subtle">{plan.remark}</div> : null}</Table.Cell>
              <Table.Cell className="text-center"><Badge variant={plan.enabled ? 'success' : 'neutral'} appearance="dot">{plan.enabled ? '启用' : '停用'}</Badge></Table.Cell>
			  <Table.Cell><div>{plan.total_bytes > 0 ? formatBytes(plan.total_bytes) : '不限'}</div></Table.Cell>
              <Table.Cell className="text-center">{plan.cycle_type === 'monthly' ? `每月 ${plan.cycle_day} 日` : plan.cycle_type === 'custom' ? '自定义' : '不重置'}</Table.Cell>
				<Table.Cell><div className="text-xs font-semibold text-kumo-strong">内部 {internalCount} · 外部 {externalCount}</div></Table.Cell>
              <Table.Cell className="text-center">{plan.subscription_count || 0}</Table.Cell>
              <Table.Cell className="text-center"><div className="inline-flex justify-center gap-2"><Button size="sm" shape="square" variant="secondary" onClick={() => onEdit(plan)} icon={<Edit className="h-3.5 w-3.5" />} aria-label="编辑套餐" /><Button size="sm" shape="square" variant={isArmed(`plan-delete:${plan.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => onDelete(plan)} icon={<Trash className="h-3.5 w-3.5" />} aria-label="删除套餐" /></div></Table.Cell>
			</Table.Row>;})}
            {plans.length === 0 && <Table.Row><Table.Cell colSpan={8} className="p-8 text-center text-kumo-subtle">暂无套餐。统一定义节点范围、额度和重置规则。</Table.Cell></Table.Row>}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );
}
