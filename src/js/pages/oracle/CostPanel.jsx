import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { AppTable, DataTableFrame, SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { PieChart, RefreshCw } from '../../components/Icons.jsx';
import CostSummaryCard from './CostSummaryCard.jsx';
import { formatCurrency } from './utils.js';

export default function CostPanel({ costOverview, loadingCost, onRefresh }) {
  return (
    <SectionCard
      title="成本监控"
      icon={<PieChart className="h-4 w-4 text-brand" />}
      description={costOverview?.currency ? `货币 ${costOverview.currency}` : 'OCI 成本与预算概览'}
      className="min-h-0 flex-1"
      bodyPadding="none"
      bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
      actions={(
        <Button type="button" size="sm" variant="secondary" onClick={onRefresh} loading={loadingCost} icon={<RefreshCw className="h-3.5 w-3.5" />}>
          刷新
        </Button>
      )}
    >
      {loadingCost && !costOverview ? (
        <div className="grid gap-4 p-4 cq-lg:grid-cols-2">
          {[0, 1, 2, 3].map((i) => <SkeletonLine key={i} className="h-28 w-full" />)}
        </div>
      ) : !costOverview ? (
        <div className="p-6 text-center text-sm text-kumo-subtle">暂无成本数据，请选择账号后刷新。</div>
      ) : (
        <div className="grid gap-4 overflow-auto p-4 cq-lg:grid-cols-2">
          <CostSummaryCard title="本月成本" amount={costOverview.currentMonth?.cost} month={costOverview.currentMonth?.month} />
          <CostSummaryCard title="上月成本" amount={costOverview.previousMonth?.cost} month={costOverview.previousMonth?.month} />
          <SectionCard title="按服务分解" className="min-h-0" bodyPadding="none" bodyClassName="overflow-auto">
            {costOverview.byService?.length ? (
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 overflow-auto">
                <AppTable tableId="oracle-cost-by-service" columns={[{ id: 'service', role: 'primary' }, { id: 'cost', role: 'number', width: 120 }]}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>服务</Table.Head>
                      <Table.Head>成本</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {costOverview.byService.map((item) => (
                      <Table.Row key={item.service}>
                        <Table.Cell>{item.service}</Table.Cell>
                        <Table.Cell>{formatCurrency(item.cost)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            ) : (
              <div className="p-4 text-center text-sm text-kumo-subtle">暂无按服务成本数据</div>
            )}
          </SectionCard>
          <SectionCard title="预算" className="min-h-0" bodyPadding="none" bodyClassName="overflow-auto">
            {costOverview.budgets?.length ? (
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 overflow-auto">
                <AppTable tableId="oracle-cost-budgets" columns={[{ id: 'name', role: 'primary' }, { id: 'amount', role: 'number', width: 120 }, { id: 'actual', role: 'number', width: 120 }]}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>预算</Table.Head>
                      <Table.Head>额度</Table.Head>
                      <Table.Head>已花费</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {costOverview.budgets.map((b) => (
                      <Table.Row key={b.id}>
                        <Table.Cell>{b.name}</Table.Cell>
                        <Table.Cell>{formatCurrency(b.amount)}</Table.Cell>
                        <Table.Cell>{formatCurrency(b.actualSpend)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            ) : (
              <div className="p-4 text-center text-sm text-kumo-subtle">未配置预算</div>
            )}
          </SectionCard>
          {costOverview.monthlyHistory?.length > 0 && (
            <SectionCard title="近 6 月成本趋势" className="min-h-0 cq-lg:col-span-2" bodyPadding="none" bodyClassName="overflow-auto">
              <DataTableFrame variant="embedded" density="dense" className="min-h-0 overflow-auto">
                <AppTable tableId="oracle-cost-history" columns={[{ id: 'month', role: 'primary' }, { id: 'cost', role: 'number', width: 140 }]}>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head>月份</Table.Head>
                      <Table.Head>成本</Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {costOverview.monthlyHistory.map((m) => (
                      <Table.Row key={m.month}>
                        <Table.Cell>{m.month}</Table.Cell>
                        <Table.Cell>{formatCurrency(m.cost)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </AppTable>
              </DataTableFrame>
            </SectionCard>
          )}
        </div>
      )}
    </SectionCard>
  );
}
